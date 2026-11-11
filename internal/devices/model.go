// Package devices owns managed network equipment, separate from the MAC-keyed
// client inventory. Adapters translate vendor protocols into these observations.
package devices

import (
	"context"
	"errors"
	"time"
)

const ModuleName = "devices"

// Device identity survives address changes. A credential is either stored here
// or named by PasswordEnv, which points at an environment variable in the
// daemon; a stored Password wins when both are set. Reads replace a stored
// password with RedactedSecret, and sending that mask back means "unchanged".
type Device struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Adapter  string `json:"adapter" jsonschema:"enum=mercury,enum=openwrt"`
	Address  string `json:"address"`
	Username string `json:"username,omitempty"`
	// Password is the management credential stored in the inventory. Every read
	// surface redacts it; it never appears in responses, events or errors.
	Password    string `json:"password,omitempty" jsonschema:"description=Stored management password, redacted on every read. Sending the mask back means unchanged."`
	PasswordEnv string `json:"password_env,omitempty"`
	Roles       []Role `json:"roles,omitempty"`
}

// RedactedSecret stands in for a stored credential on a read surface. A fixed
// string rather than a length-preserving mask: it gives nothing away and cannot
// be mistaken for the real value.
const RedactedSecret = "********"

// Redacted is the inventory as a reader may see it: stored passwords replaced by
// the mask, everything else untouched. The revision still describes the stored
// document, so a concurrent credential change is still detected.
func (c Config) Redacted() Config {
	out := Config{Devices: append([]Device(nil), c.Devices...)}
	for i := range out.Devices {
		if out.Devices[i].Password != "" {
			out.Devices[i].Password = RedactedSecret
		}
	}
	return out
}

// Credential prefers a stored password and falls back to the environment
// variable named by PasswordEnv. A resolver is only needed for the fallback.
func (d Device) Credential(resolve func(string) (string, error)) (string, error) {
	if d.Password != "" {
		return d.Password, nil
	}
	if resolve == nil {
		return "", errors.New("device credential is unavailable")
	}
	return resolve(d.PasswordEnv)
}

type Role string

const (
	Router Role = "router"
	Switch Role = "switch"
	AP     Role = "ap"
)

type Config struct {
	Devices []Device `json:"devices,omitempty"`
}

type Identity struct {
	Manufacturer  string   `json:"manufacturer,omitempty"`
	Model         string   `json:"model,omitempty"`
	Serial        string   `json:"serial,omitempty"`
	Firmware      string   `json:"firmware,omitempty"`
	Hostname      string   `json:"hostname,omitempty"`
	MACs          []string `json:"macs,omitempty"`
	Roles         []Role   `json:"roles,omitempty"`
	UptimeSeconds *uint64  `json:"uptime_seconds,omitempty"`
}
type Collection struct {
	State      string    `json:"state" jsonschema:"enum=ok,enum=unsupported,enum=error"`
	ObservedAt time.Time `json:"observed_at"`
	Error      string    `json:"error,omitempty"`
}
type Capability struct {
	State  string `json:"state" jsonschema:"enum=supported,enum=unsupported,enum=unknown"`
	Reason string `json:"reason,omitempty"`
}
type Capabilities map[string]Capability

func (c Capabilities) Supports(name string) bool { return c[name].State == "supported" }
func Supported() Capability                      { return Capability{State: "supported"} }
func Unsupported(reason string) Capability       { return Capability{State: "unsupported", Reason: reason} }

type Port struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind" jsonschema:"enum=ethernet,enum=fiber,enum=bridge,enum=vlan,enum=lag,enum=wireless,enum=other"`
	MAC       string   `json:"mac,omitempty"`
	ParentID  string   `json:"parent_id,omitempty"`
	Members   []string `json:"members,omitempty"`
	Addresses []string `json:"addresses,omitempty"`
	Enabled   *bool    `json:"enabled,omitempty"`
	LinkUp    *bool    `json:"link_up,omitempty"`
	SpeedMbps *uint64  `json:"speed_mbps,omitempty"`
	Duplex    string   `json:"duplex,omitempty"`
	RXBytes   *uint64  `json:"rx_bytes,omitempty"`
	TXBytes   *uint64  `json:"tx_bytes,omitempty"`
	RXPackets *uint64  `json:"rx_packets,omitempty"`
	TXPackets *uint64  `json:"tx_packets,omitempty"`
	RXErrors  *uint64  `json:"rx_errors,omitempty"`
	TXErrors  *uint64  `json:"tx_errors,omitempty"`
	RXMbps    *float64 `json:"rx_mbps,omitempty"`
	TXMbps    *float64 `json:"tx_mbps,omitempty"`
	PVID      *int     `json:"pvid,omitempty"`
	PoE       *PoE     `json:"poe,omitempty"`
}
type PoE struct {
	Enabled    *bool    `json:"enabled,omitempty"`
	Delivering *bool    `json:"delivering,omitempty"`
	PowerWatts *float64 `json:"power_watts,omitempty"`
}
type VLAN struct {
	ID       int      `json:"id"`
	Name     string   `json:"name,omitempty"`
	Tagged   []string `json:"tagged_ports"`
	Untagged []string `json:"untagged_ports"`
}
type FDBEntry struct {
	MAC    string `json:"mac"`
	VLAN   *int   `json:"vlan,omitempty"`
	PortID string `json:"port_id"`
	Type   string `json:"type,omitempty"`
}
type Neighbor struct {
	PortID    string     `json:"port_id"`
	ChassisID string     `json:"chassis_id"`
	Port      string     `json:"remote_port,omitempty"`
	Name      string     `json:"name,omitempty"`
	Source    string     `json:"source"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}
type Radio struct {
	ID                 string   `json:"id"`
	Enabled            *bool    `json:"enabled,omitempty"`
	Band               string   `json:"band,omitempty"`
	Channel            *int     `json:"channel,omitempty"`
	WidthMHz           *int     `json:"width_mhz,omitempty"`
	ConfiguredWidthMHz *int     `json:"configured_width_mhz,omitempty"`
	Protocols          []string `json:"protocols,omitempty"`
	NoiseDBm           *int     `json:"noise_dbm,omitempty"`
	TxPowerDBm         *int     `json:"txpower_dbm,omitempty"`
	Country            string   `json:"country,omitempty"`
	UtilizationPercent *float64 `json:"utilization_percent,omitempty"`
}
type WirelessNetwork struct {
	ID           string   `json:"id"`
	RadioID      string   `json:"radio_id"`
	InterfaceID  string   `json:"interface_id"`
	SSID         string   `json:"ssid,omitempty"`
	BSSID        string   `json:"bssid,omitempty"`
	Mode         string   `json:"mode,omitempty"`
	Encryption   string   `json:"encryption,omitempty"`
	NetworkNames []string `json:"network_names,omitempty"`
}
type Association struct {
	MAC              string   `json:"mac"`
	NetworkID        string   `json:"network_id"`
	RadioID          string   `json:"radio_id,omitempty"`
	SignalDBm        *int     `json:"signal_dbm,omitempty"`
	SignalAvgDBm     *int     `json:"signal_avg_dbm,omitempty"`
	NoiseDBm         *int     `json:"noise_dbm,omitempty"`
	ConnectedSeconds *uint64  `json:"connected_seconds,omitempty"`
	InactiveMS       *uint64  `json:"inactive_ms,omitempty"`
	RXRateMbps       *float64 `json:"rx_rate_mbps,omitempty"`
	TXRateMbps       *float64 `json:"tx_rate_mbps,omitempty"`
	RXMCS            *int     `json:"rx_mcs,omitempty"`
	TXMCS            *int     `json:"tx_mcs,omitempty"`
	RXWidthMHz       *int     `json:"rx_width_mhz,omitempty"`
	TXWidthMHz       *int     `json:"tx_width_mhz,omitempty"`
	ExpectedMbps     *float64 `json:"expected_mbps,omitempty"`
	Retries          *uint64  `json:"retries,omitempty"`
	Failed           *uint64  `json:"failed,omitempty"`
	RXBytes          *uint64  `json:"rx_bytes,omitempty"`
	TXBytes          *uint64  `json:"tx_bytes,omitempty"`
	RXPackets        *uint64  `json:"rx_packets,omitempty"`
	TXPackets        *uint64  `json:"tx_packets,omitempty"`
	TXAirtimeUS      *uint64  `json:"tx_airtime_us,omitempty"`
	MFP              *bool    `json:"mfp,omitempty"`
	WMM              *bool    `json:"wmm,omitempty"`
}

// A Neighbour is another BSS heard during an explicit scan, not a live view.
// Scanning moves the radio off its channel for a few seconds, so every answer
// is a dated sample and the page says so.
type Neighbour struct {
	BSSID      string `json:"bssid,omitempty"`
	SSID       string `json:"ssid,omitempty"`
	Channel    *int   `json:"channel,omitempty"`
	WidthMHz   *int   `json:"width_mhz,omitempty"`
	SignalDBm  *int   `json:"signal_dbm,omitempty"`
	Encryption string `json:"encryption,omitempty"`
	// Open is nil when the beacon carried no usable privacy information, which
	// is not the same as an open network.
	Open      *bool  `json:"open,omitempty"`
	Transport string `json:"transport,omitempty"`
}
type ScanRequest struct {
	RadioID string `json:"radio_id"`
}
type ScanResult struct {
	DeviceID   string      `json:"device_id"`
	RadioID    string      `json:"radio_id"`
	ObservedAt time.Time   `json:"observed_at"`
	Neighbours []Neighbour `json:"neighbours"`
}
type Snapshot struct {
	DeviceID     string                `json:"device_id"`
	ObservedAt   time.Time             `json:"observed_at"`
	Identity     Identity              `json:"identity"`
	Capabilities Capabilities          `json:"capabilities"`
	Collections  map[string]Collection `json:"collections"`
	Ports        []Port                `json:"ports,omitempty"`
	VLANEnabled  *bool                 `json:"vlan_enabled,omitempty"`
	VLANs        []VLAN                `json:"vlans,omitempty"`
	FDB          []FDBEntry            `json:"fdb,omitempty"`
	Neighbors    []Neighbor            `json:"neighbors,omitempty"`
	Radios       []Radio               `json:"radios,omitempty"`
	Wireless     []WirelessNetwork     `json:"wireless,omitempty"`
	Associations []Association         `json:"associations,omitempty"`
}

// Lookup results describe reachability, not proof of a direct cable.
type FDBQuery struct {
	MAC  string `json:"mac"`
	VLAN *int   `json:"vlan,omitempty"`
}
type FDBResult struct {
	DeviceID   string     `json:"device_id"`
	ObservedAt time.Time  `json:"observed_at"`
	Entries    []FDBEntry `json:"entries"`
}
type Operation struct {
	Kind      string `json:"kind" jsonschema:"enum=client.disconnect"`
	Target    string `json:"target"`
	ClientMAC string `json:"client_mac"`
}
type OperationPlan struct {
	DeviceID     string     `json:"device_id"`
	Operation    Operation  `json:"operation"`
	Supported    bool       `json:"supported"`
	Impact       string     `json:"impact"`
	Confirmation string     `json:"confirmation,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}
type OperationResult struct {
	DeviceID   string    `json:"device_id"`
	Operation  Operation `json:"operation"`
	State      string    `json:"state" jsonschema:"enum=accepted,enum=verified,enum=unknown"`
	ObservedAt time.Time `json:"observed_at"`
}

var ErrUnsupported = errors.New("device capability is not supported")
var ErrInvalid = errors.New("invalid network device request")

// Adapter has one mandatory read contract and optional narrow abilities.
type Adapter interface {
	Read(context.Context, Device) (Snapshot, error)
}

// Scanner hears neighbouring BSS on one radio. It disturbs the radio while it
// runs, so callers trigger it deliberately and never as part of a snapshot.
type Scanner interface {
	Scan(context.Context, Device, string) (ScanResult, error)
}
type FDBLookup interface {
	LookupFDB(context.Context, Device, FDBQuery) (FDBResult, error)
}
type Operator interface {
	Plan(context.Context, Device, Operation) (OperationPlan, error)
	Execute(context.Context, Device, Operation) (OperationResult, error)
}

func Ptr[T any](v T) *T { return &v }
