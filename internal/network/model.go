// Package network owns managed network equipment, separate from the MAC-keyed
// client inventory. Adapters translate vendor protocols into these observations.
package network

import (
	"context"
	"errors"
	"time"
)

const ModuleName = "network"

// Device identity survives address changes. PasswordEnv names an environment
// variable containing the password; secrets never enter inventory responses.
type Device struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Adapter     string `json:"adapter" jsonschema:"enum=mercury,enum=openwrt"`
	Address     string `json:"address"`
	Username    string `json:"username,omitempty"`
	PasswordEnv string `json:"password_env,omitempty"`
	Roles       []Role `json:"roles,omitempty"`
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
	ID                 string `json:"id"`
	Enabled            *bool  `json:"enabled,omitempty"`
	Band               string `json:"band,omitempty"`
	Channel            *int   `json:"channel,omitempty"`
	WidthMHz           *int   `json:"width_mhz,omitempty"`
	ConfiguredWidthMHz *int   `json:"configured_width_mhz,omitempty"`
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
	SignalDBm        *int     `json:"signal_dbm,omitempty"`
	ConnectedSeconds *uint64  `json:"connected_seconds,omitempty"`
	RXRateMbps       *float64 `json:"rx_rate_mbps,omitempty"`
	TXRateMbps       *float64 `json:"tx_rate_mbps,omitempty"`
	RXBytes          *uint64  `json:"rx_bytes,omitempty"`
	TXBytes          *uint64  `json:"tx_bytes,omitempty"`
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
type FDBLookup interface {
	LookupFDB(context.Context, Device, FDBQuery) (FDBResult, error)
}
type Operator interface {
	Plan(context.Context, Device, Operation) (OperationPlan, error)
	Execute(context.Context, Device, Operation) (OperationResult, error)
}

func Ptr[T any](v T) *T { return &v }
