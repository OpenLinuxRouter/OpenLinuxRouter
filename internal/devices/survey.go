package devices

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Survey is the last full look at the equipment: every box's reading and, for
// each switch, which port each address this router knows was learned on.
//
// It exists so a page can show the network at once. Reading the boxes takes
// seconds, and placing every client on a switch port takes a lookup per address
// — tens of seconds on firmware that searches one MAC at a time — so the router
// keeps the last answer and refreshes it in the background. A request is served
// the stored answer immediately and, when that answer is older than MaxAge,
// starts a new survey; the caller sees `running` and asks again.
//
// A refresh never blanks what is known. A box that does not answer keeps its
// last reading with the error beside it, and a switch's table is replaced only
// when its new sweep is complete, so the map does not lose cables half-way.
type Survey struct {
	Running    bool                    `json:"running"`
	StartedAt  *time.Time              `json:"started_at,omitempty"`
	FinishedAt *time.Time              `json:"finished_at,omitempty"`
	Devices    map[string]SurveyDevice `json:"devices"`
}

// SurveyDevice is what the last survey learned about one box.
type SurveyDevice struct {
	Snapshot  *Snapshot  `json:"snapshot,omitempty"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	ReadError string     `json:"read_error,omitempty"`

	// FDB is the last complete sweep: each known address the box placed on a
	// port. Absent for a box that cannot be asked.
	FDB   []FDBEntry `json:"fdb,omitempty"`
	FDBAt *time.Time `json:"fdb_at,omitempty"`
	// Asked and Total are the sweep in progress, when one is.
	Asked int `json:"fdb_asked,omitempty"`
	Total int `json:"fdb_total,omitempty"`
}

// Surveyor runs surveys and keeps the last one.
type Surveyor struct {
	Service *Service
	// Addresses is the MACs worth asking a switch about: this router's own
	// interfaces, the managed boxes, and whoever is online.
	Addresses func(context.Context) ([]string, error)
	// MaxAge is how old a survey may be before a request starts another.
	MaxAge time.Duration
	// Now is the clock; tests replace it.
	Now func() time.Time

	mu   sync.Mutex
	last Survey
}

const defaultSurveyAge = 2 * time.Minute

// Current returns the stored survey, starting a refresh when it is stale and
// none is running. It never waits for the equipment.
func (s *Surveyor) Current() Survey {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	maxAge := s.MaxAge
	if maxAge <= 0 {
		maxAge = defaultSurveyAge
	}
	if !s.last.Running && (s.last.FinishedAt == nil || now.Sub(*s.last.FinishedAt) > maxAge) {
		s.last.Running = true
		s.last.StartedAt = &now
		go s.run(context.Background())
	}
	return s.copyLocked()
}

func (s *Surveyor) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Surveyor) copyLocked() Survey {
	out := s.last
	out.Devices = make(map[string]SurveyDevice, len(s.last.Devices))
	for id, d := range s.last.Devices {
		out.Devices[id] = d
	}
	return out
}

func (s *Surveyor) update(id string, edit func(*SurveyDevice)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last.Devices == nil {
		s.last.Devices = map[string]SurveyDevice{}
	}
	d := s.last.Devices[id]
	edit(&d)
	s.last.Devices[id] = d
}

// run reads every box side by side, then sweeps every box that can be asked,
// side by side; each box is asked one address at a time, because that is all
// its firmware takes, and the service's per-device lanes would serialize it
// anyway.
func (s *Surveyor) run(ctx context.Context) {
	defer func() {
		s.mu.Lock()
		now := s.now()
		s.last.Running = false
		s.last.FinishedAt = &now
		s.mu.Unlock()
	}()
	cfg, err := Load(s.Service.Store)
	if err != nil {
		return
	}
	s.mu.Lock()
	// Forget boxes that left the inventory.
	known := map[string]bool{}
	for _, d := range cfg.Devices {
		known[d.ID] = true
	}
	for id := range s.last.Devices {
		if !known[id] {
			delete(s.last.Devices, id)
		}
	}
	s.mu.Unlock()

	var wg sync.WaitGroup
	for _, d := range cfg.Devices {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			snapshot, err := s.Service.Read(ctx, id)
			at := s.now()
			s.update(id, func(d *SurveyDevice) {
				if err != nil {
					d.ReadError = err.Error()
					return
				}
				d.Snapshot = &snapshot
				d.ReadAt = &at
				d.ReadError = ""
			})
		}(d.ID)
	}
	wg.Wait()

	var macs []string
	if s.Addresses != nil {
		macs, _ = s.Addresses(ctx)
	}
	macs = normalizeMACs(macs)
	if len(macs) == 0 {
		return
	}
	for _, d := range cfg.Devices {
		if _, ok := s.Service.Adapters[d.Adapter].(FDBLookup); !ok {
			continue
		}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			s.sweep(ctx, id, macs)
		}(d.ID)
	}
	wg.Wait()
}

func (s *Surveyor) sweep(ctx context.Context, id string, macs []string) {
	s.update(id, func(d *SurveyDevice) { d.Asked, d.Total = 0, len(macs) })
	var learned []FDBEntry
	answered := false
	for i, mac := range macs {
		result, err := s.Service.Lookup(ctx, id, FDBQuery{MAC: mac})
		if errors.Is(err, ErrUnsupported) {
			s.update(id, func(d *SurveyDevice) { d.Asked, d.Total = 0, 0 })
			return
		}
		if err == nil {
			answered = true
			learned = append(learned, result.Entries...)
		}
		s.update(id, func(d *SurveyDevice) { d.Asked = i + 1 })
	}
	at := s.now()
	s.update(id, func(d *SurveyDevice) {
		d.Asked, d.Total = 0, 0
		// A sweep where nothing answered is a box that was unreachable, not an
		// empty table; the last one that answered stands.
		if answered {
			d.FDB = learned
			d.FDBAt = &at
		}
	})
}

func normalizeMACs(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		mac, err := core.NormalizeMAC(raw)
		if err != nil || mac == "00:00:00:00:00:00" || seen[mac] {
			continue
		}
		seen[mac] = true
		out = append(out, mac)
	}
	return out
}
