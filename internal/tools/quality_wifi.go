package tools

import "time"

// WirelessLink is one client association as a managed access point reports it.
// This router has no radio, so the only wireless evidence it can collect by
// itself is what its access points say about the clients attached to them: how
// well each device hears its access point, not how fast that link carries
// bytes. Retries, airtime and channel utilization are absent on the firmware
// this was written against, so no field here is guessed from what is missing.
type WirelessLink struct {
	AP        string `json:"ap_id"`
	APName    string `json:"ap_name,omitempty"`
	MAC       string `json:"mac"`
	Radio     string `json:"radio_id,omitempty"`
	Band      string `json:"band,omitempty"`
	SignalDBm *int   `json:"signal_dbm,omitempty"`
	NoiseDBm  *int   `json:"noise_dbm,omitempty"`
	// SNRDb is signal minus noise, present only when the access point reported
	// both. A missing noise floor stays missing rather than becoming zero.
	SNRDb            *int     `json:"snr_db,omitempty"`
	RXRateMbps       *float64 `json:"rx_rate_mbps,omitempty"`
	TXRateMbps       *float64 `json:"tx_rate_mbps,omitempty"`
	ConnectedSeconds *uint64  `json:"connected_seconds,omitempty"`
}

// WirelessAP records whether one access point answered. Two access points with
// one silent is a partial read, not one access point with fewer clients, and
// the difference is what sends an operator to the right box.
type WirelessAP struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
	Links int    `json:"links"`
}

// QualityWireless is one dated read of every managed access point. A client can
// move between radios between two reads, so this is evidence with a time
// attached, never current truth.
type QualityWireless struct {
	ObservedAt time.Time      `json:"observed_at"`
	APs        []WirelessAP   `json:"aps"`
	Links      []WirelessLink `json:"links"`
}
