package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/devices"
	"github.com/open-linux-router/open-linux-router/internal/tools"
)

// qualityWireless returns the reader behind the Wi-Fi category of the network
// quality score. The score runs on this router, which has no radio, so the only
// wireless it can observe is what its access points report about the clients
// attached to them. That is why the evidence is a list of links with a time on
// it rather than a throughput number: no one ran a test.
func qualityWireless(service *devices.Service) func(context.Context) (tools.QualityWireless, error) {
	return func(ctx context.Context) (tools.QualityWireless, error) {
		cfg, err := devices.Load(service.Store)
		if err != nil {
			return tools.QualityWireless{}, err
		}
		report := tools.QualityWireless{ObservedAt: time.Now().UTC()}
		var failures []string
		for _, device := range cfg.Devices {
			if !slices.Contains(device.Roles, devices.AP) {
				continue
			}
			access := tools.WirelessAP{ID: device.ID, Name: device.Name}
			// One silent access point must not hold the whole assessment for
			// the management timeout.
			bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
			snapshot, readErr := service.Read(bounded, device.ID)
			cancel()
			if ctx.Err() != nil {
				return tools.QualityWireless{}, ctx.Err()
			}
			if readErr != nil {
				access.State, access.Error = "error", readErr.Error()
				failures = append(failures, fmt.Sprintf("%s: %v", accessName(access), readErr))
				report.APs = append(report.APs, access)
				continue
			}
			access.State = "ok"
			bands := make(map[string]string, len(snapshot.Radios))
			for _, radio := range snapshot.Radios {
				bands[radio.ID] = radio.Band
			}
			for _, association := range snapshot.Associations {
				report.Links = append(report.Links, wirelessLink(device, bands, association))
				access.Links++
			}
			report.APs = append(report.APs, access)
		}
		if len(report.APs) == 0 {
			return tools.QualityWireless{}, errors.New("no access point is configured in the network equipment inventory")
		}
		// One access point answering is enough to score a link; silently
		// dropping a second one would look like fewer clients in the house.
		if len(failures) == len(report.APs) {
			return tools.QualityWireless{}, fmt.Errorf("no access point answered: %s", strings.Join(failures, "; "))
		}
		return report, nil
	}
}

func accessName(access tools.WirelessAP) string {
	if access.Name != "" {
		return access.Name
	}
	return access.ID
}

func wirelessLink(device devices.Device, bands map[string]string, association devices.Association) tools.WirelessLink {
	link := tools.WirelessLink{
		AP:               device.ID,
		APName:           device.Name,
		MAC:              association.MAC,
		Radio:            association.RadioID,
		Band:             bands[association.RadioID],
		SignalDBm:        association.SignalDBm,
		NoiseDBm:         association.NoiseDBm,
		RXRateMbps:       association.RXRateMbps,
		TXRateMbps:       association.TXRateMbps,
		ConnectedSeconds: association.ConnectedSeconds,
	}
	// A signal without a noise floor has no SNR; zero would be a measurement
	// nobody made.
	if association.SignalDBm != nil && association.NoiseDBm != nil {
		snr := *association.SignalDBm - *association.NoiseDBm
		link.SNRDb = &snr
	}
	return link
}
