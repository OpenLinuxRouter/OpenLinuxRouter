package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/devices"
)

// readOnlyAdapter is a device with no forwarding table: the OpenWrt case today.
type readOnlyAdapter struct{}

func (readOnlyAdapter) Read(context.Context, devices.Device) (devices.Snapshot, error) {
	return devices.Snapshot{}, nil
}

type fdbTestAdapter struct {
	entries []devices.FDBEntry
	err     error
}

func (fdbTestAdapter) Read(context.Context, devices.Device) (devices.Snapshot, error) {
	return devices.Snapshot{}, nil
}

func (a fdbTestAdapter) LookupFDB(_ context.Context, d devices.Device, _ devices.FDBQuery) (devices.FDBResult, error) {
	if a.err != nil {
		return devices.FDBResult{}, a.err
	}
	return devices.FDBResult{
		DeviceID:   d.ID,
		ObservedAt: time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC),
		Entries:    a.entries,
	}, nil
}

// Three devices, three different answers: a switch that knows the port, an AP
// that has no forwarding table to report, and one that cannot be reached.
//
// The three must not collapse into one: "port 5", "this device cannot say" and
// "we could not ask" are different facts, and a client that showed the last two
// as "not there" would send the operator looking in the wrong place.
func TestDeviceLocationsKeepsAnswersAndFailuresApart(t *testing.T) {
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), devices.ModuleName)
	if _, err := devices.Save(store, devices.Config{Devices: []devices.Device{
		{ID: "switch", Name: "MercurySwitch", Adapter: "mercury", Address: "http://192.0.2.2"},
		{ID: "ap", Name: "XiaomiBE7000", Adapter: "openwrt", Address: "http://192.0.2.3"},
		{ID: "ont", Name: "HuaweiModem", Adapter: "huawei", Address: "http://192.0.2.4"},
	}}); err != nil {
		t.Fatal(err)
	}

	service := devices.NewService(store)
	service.Adapters["mercury"] = fdbTestAdapter{entries: []devices.FDBEntry{
		{MAC: "aa:bb:cc:dd:ee:ff", PortID: "5"},
	}}
	service.Adapters["openwrt"] = readOnlyAdapter{}
	service.Adapters["huawei"] = fdbTestAdapter{err: errors.New("no route to host")}

	locations, problems, err := deviceLocations{service: service}.Locations(context.Background(), "aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatal(err)
	}

	if len(locations) != 1 {
		t.Fatalf("locations = %+v", locations)
	}
	got := locations[0]
	if got.DeviceID != "switch" || got.Device != "MercurySwitch" || got.Port != "5" {
		t.Errorf("location = %+v", got)
	}

	if len(problems) != 2 {
		t.Fatalf("problems = %+v", problems)
	}
	// Sorted by device ID, so two reads of an unchanged inventory agree.
	if problems[0].Path != "ap" || !strings.Contains(problems[0].Message, "cannot report") {
		t.Errorf("first problem = %+v, want the AP's missing forwarding table", problems[0])
	}
	if problems[1].Path != "ont" || !strings.Contains(problems[1].Message, "could not ask") {
		t.Errorf("second problem = %+v, want the unreachable device", problems[1])
	}
	if !strings.Contains(problems[1].Message, "no route to host") {
		t.Errorf("message = %q, want the device's error", problems[1].Message)
	}
}

// No inventory at all is a valid, empty answer: a router with no managed switch
// in front of it honestly has nothing to say about ports.
func TestDeviceLocationsWithoutEquipmentIsEmpty(t *testing.T) {
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), devices.ModuleName)
	service := devices.NewService(store)

	locations, problems, err := deviceLocations{service: service}.Locations(context.Background(), "aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatal(err)
	}
	if len(locations) != 0 || len(problems) != 0 {
		t.Errorf("locations = %+v, problems = %+v", locations, problems)
	}
}
