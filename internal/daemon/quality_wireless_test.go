package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/devices"
)

type readFunc func(context.Context, devices.Device) (devices.Snapshot, error)

func (f readFunc) Read(ctx context.Context, d devices.Device) (devices.Snapshot, error) {
	return f(ctx, d)
}

func qualityService(t *testing.T, configured ...devices.Device) *devices.Service {
	t.Helper()
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), devices.ModuleName)
	if _, err := devices.Save(store, devices.Config{Devices: configured}); err != nil {
		t.Fatal(err)
	}
	return devices.NewService(store)
}

func linkInt(v int) *int { return &v }

// The reader joins each association to its radio's band and keeps the access
// point that did not answer, so a partial read is visible as a partial read.
func TestQualityWirelessReadsEveryAccessPoint(t *testing.T) {
	service := qualityService(t,
		devices.Device{ID: "ap-5g", Name: "Study AP", Adapter: "openwrt", Address: "http://192.0.2.1", Roles: []devices.Role{devices.AP}},
		devices.Device{ID: "ap-silent", Name: "Hall AP", Adapter: "openwrt", Address: "http://192.0.2.2", Roles: []devices.Role{devices.AP}},
		devices.Device{ID: "switch", Adapter: "mercury", Address: "http://192.0.2.3", Roles: []devices.Role{devices.Switch}},
	)
	service.Adapters["openwrt"] = readFunc(func(_ context.Context, d devices.Device) (devices.Snapshot, error) {
		if d.ID == "ap-silent" {
			return devices.Snapshot{}, errors.New("dial tcp 192.0.2.2:80: connection refused")
		}
		return devices.Snapshot{
			DeviceID: d.ID,
			Radios:   []devices.Radio{{ID: "radio1", Band: "5g"}},
			Associations: []devices.Association{
				{MAC: "aa:bb:cc:dd:ee:01", RadioID: "radio1", SignalDBm: linkInt(-52), NoiseDBm: linkInt(-100), RXRateMbps: valueOf(866.7), TXRateMbps: valueOf(720.6)},
				{MAC: "aa:bb:cc:dd:ee:02", RadioID: "radio1", SignalDBm: linkInt(-71)},
			},
		}, nil
	})

	report, err := qualityWireless(service)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.APs) != 2 || report.APs[0].State != "ok" || report.APs[0].Links != 2 {
		t.Fatalf("access points: %+v", report.APs)
	}
	if report.APs[1].State != "error" || !strings.Contains(report.APs[1].Error, "connection refused") {
		t.Fatalf("a silent access point must be named: %+v", report.APs[1])
	}
	if len(report.Links) != 2 {
		t.Fatalf("links: %+v", report.Links)
	}
	link := report.Links[0]
	if link.AP != "ap-5g" || link.APName != "Study AP" || link.Band != "5g" || link.SNRDb == nil || *link.SNRDb != 48 {
		t.Fatalf("link metadata: %+v", link)
	}
	if link.RXRateMbps == nil || *link.RXRateMbps != 866.7 {
		t.Fatalf("rate: %+v", link.RXRateMbps)
	}
	// A signal without a noise floor has no measured SNR.
	if second := report.Links[1]; second.SNRDb != nil || second.SignalDBm == nil || *second.SignalDBm != -71 {
		t.Fatalf("noise floor guess: %+v", second)
	}
}

func TestQualityWirelessReportsNoAccessPoint(t *testing.T) {
	service := qualityService(t,
		devices.Device{ID: "switch", Adapter: "mercury", Address: "http://192.0.2.3", Roles: []devices.Role{devices.Switch}},
	)
	if _, err := qualityWireless(service)(context.Background()); err == nil || !strings.Contains(err.Error(), "no access point") {
		t.Fatalf("err=%v", err)
	}

	silent := qualityService(t,
		devices.Device{ID: "ap", Adapter: "openwrt", Address: "http://192.0.2.1", Roles: []devices.Role{devices.AP}},
	)
	silent.Adapters["openwrt"] = readFunc(func(context.Context, devices.Device) (devices.Snapshot, error) {
		return devices.Snapshot{}, errors.New("timeout")
	})
	if _, err := qualityWireless(silent)(context.Background()); err == nil || !strings.Contains(err.Error(), "no access point answered") {
		t.Fatalf("err=%v", err)
	}
}

func valueOf(v float64) *float64 { return &v }
