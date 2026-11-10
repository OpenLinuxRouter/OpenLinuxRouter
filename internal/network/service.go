package network

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

var ErrNotFound = errors.New("network device not found")
var ErrConfirmation = errors.New("operation confirmation is missing, expired, already used or no longer matches the device")

type pendingOperation struct {
	Device    Device
	Operation Operation
	Expires   time.Time
}

// Service serializes access to equipment using bounded, context-aware lanes.
// It stores pending user decisions, never remote state as current truth.
type Service struct {
	Store     *core.Store
	Adapters  map[string]Adapter
	lanes     [16]*core.Lock
	pendingMu sync.Mutex
	pending   map[string]pendingOperation
}

func NewService(store *core.Store) *Service {
	password := func(name string) (string, error) {
		value, ok := os.LookupEnv(name)
		if name == "" || !ok || value == "" {
			return "", errors.New("device credential is unavailable")
		}
		return value, nil
	}
	s := &Service{Store: store, Adapters: map[string]Adapter{"mercury": &Mercury{Password: password}, "openwrt": &OpenWrt{Password: password}}, pending: map[string]pendingOperation{}}
	for i := range s.lanes {
		s.lanes[i] = core.NewLock()
	}
	return s
}
func (s *Service) Device(id string) (Device, Adapter, error) {
	c, err := Load(s.Store)
	if err != nil {
		return Device{}, nil, err
	}
	for _, d := range c.Devices {
		if d.ID == id {
			a := s.Adapters[d.Adapter]
			if a == nil {
				return d, nil, ErrUnsupported
			}
			return d, a, nil
		}
	}
	return Device{}, nil, ErrNotFound
}
func (s *Service) withDevice(ctx context.Context, id string, fn func(context.Context, Device, Adapter) error) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(id))
	lane := s.lanes[hash.Sum32()%uint32(len(s.lanes))]
	return lane.Do(ctx, func() error {
		d, a, err := s.Device(id)
		if err != nil {
			return err
		}
		return fn(ctx, d, a)
	})
}
func (s *Service) Read(ctx context.Context, id string) (Snapshot, error) {
	var snapshot Snapshot
	err := s.withDevice(ctx, id, func(ctx context.Context, d Device, a Adapter) error {
		var err error
		snapshot, err = a.Read(ctx, d)
		return err
	})
	return snapshot, err
}

// Scan asks one radio what else it can hear. It is separate from Read because
// a scan interrupts the radio; a page that only wanted a snapshot must not pay
// for it.
func (s *Service) Scan(ctx context.Context, id, radioID string) (ScanResult, error) {
	if radioID == "" || len(radioID) > 128 {
		return ScanResult{}, fmt.Errorf("%w: a radio ID is required", ErrInvalid)
	}
	var result ScanResult
	err := s.withDevice(ctx, id, func(ctx context.Context, d Device, a Adapter) error {
		scanner, ok := a.(Scanner)
		if !ok {
			return ErrUnsupported
		}
		var err error
		result, err = scanner.Scan(ctx, d, radioID)
		return err
	})
	return result, err
}
func (s *Service) Lookup(ctx context.Context, id string, q FDBQuery) (FDBResult, error) {
	mac, err := core.NormalizeMAC(q.MAC)
	if err != nil {
		return FDBResult{}, fmt.Errorf("%w: invalid MAC address", ErrInvalid)
	}
	q.MAC = mac
	if q.VLAN != nil && (*q.VLAN < 1 || *q.VLAN > 4094) {
		return FDBResult{}, fmt.Errorf("%w: VLAN must be between 1 and 4094", ErrInvalid)
	}
	var result FDBResult
	err = s.withDevice(ctx, id, func(ctx context.Context, d Device, a Adapter) error {
		lookup, ok := a.(FDBLookup)
		if !ok {
			return ErrUnsupported
		}
		var err error
		result, err = lookup.LookupFDB(ctx, d, q)
		return err
	})
	return result, err
}
func validateOperation(op Operation) error {
	if op.Kind != "client.disconnect" {
		return ErrUnsupported
	}
	if op.Target == "" || len(op.Target) > 128 {
		return fmt.Errorf("%w: a wireless network target is required", ErrInvalid)
	}
	if _, err := core.NormalizeMAC(op.ClientMAC); err != nil {
		return fmt.Errorf("%w: invalid client MAC", ErrInvalid)
	}
	return nil
}
func (s *Service) Plan(ctx context.Context, id string, op Operation) (OperationPlan, error) {
	if err := validateOperation(op); err != nil {
		return OperationPlan{}, err
	}
	op.ClientMAC, _ = core.NormalizeMAC(op.ClientMAC)
	var plan OperationPlan
	err := s.withDevice(ctx, id, func(ctx context.Context, d Device, a Adapter) error {
		operator, ok := a.(Operator)
		if !ok {
			return ErrUnsupported
		}
		var err error
		plan, err = operator.Plan(ctx, d, op)
		if err != nil || !plan.Supported {
			return err
		}
		token := make([]byte, 32)
		if _, err = rand.Read(token); err != nil {
			return errors.New("could not create operation confirmation")
		}
		expires := time.Now().Add(5 * time.Minute)
		plan.Confirmation = hex.EncodeToString(token)
		plan.ExpiresAt = &expires
		s.pendingMu.Lock()
		defer s.pendingMu.Unlock()
		for key, p := range s.pending {
			if time.Now().After(p.Expires) {
				delete(s.pending, key)
			}
		}
		if len(s.pending) >= 256 {
			return errors.New("too many pending network operations")
		}
		s.pending[plan.Confirmation] = pendingOperation{Device: d, Operation: op, Expires: expires}
		return nil
	})
	return plan, err
}

// Execute consumes confirmation before calling a device. Lost replies and retries
// cannot replay an operation; a new user decision needs a fresh plan.
func (s *Service) Execute(ctx context.Context, id string, op Operation, confirmation string) (OperationResult, error) {
	if err := validateOperation(op); err != nil {
		return OperationResult{}, err
	}
	op.ClientMAC, _ = core.NormalizeMAC(op.ClientMAC)
	var result OperationResult
	err := s.withDevice(ctx, id, func(ctx context.Context, d Device, a Adapter) error {
		s.pendingMu.Lock()
		p, ok := s.pending[confirmation]
		delete(s.pending, confirmation)
		s.pendingMu.Unlock()
		if !ok || time.Now().After(p.Expires) || !reflect.DeepEqual(d, p.Device) || !reflect.DeepEqual(op, p.Operation) {
			return ErrConfirmation
		}
		operator, ok := a.(Operator)
		if !ok {
			return ErrUnsupported
		}
		var err error
		result, err = operator.Execute(ctx, d, op)
		return err
	})
	return result, err
}
