package qos

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const ModuleName = "qos"

type Priority string

const (
	High   Priority = "high"
	Normal Priority = "normal"
	Low    Priority = "low"
)

type Client struct {
	MAC          string   `json:"mac"`
	Priority     Priority `json:"priority,omitempty"`
	DownloadMbps float64  `json:"download_mbps,omitempty"`
	UploadMbps   float64  `json:"upload_mbps,omitempty"`
}

type Config struct {
	Enabled      bool     `json:"enabled,omitempty"`
	DownloadMbps float64  `json:"download_mbps,omitempty"`
	UploadMbps   float64  `json:"upload_mbps,omitempty"`
	Clients      []Client `json:"clients,omitempty"`

	// Legacy records that a stored document still called its list `clients`,
	// the word this module used before the two inventories were told apart. It
	// is never serialised, so one save writes the current shape.
	Legacy bool `json:"-"`
}

// legacyConfig is the pre-rename shape of this module's subtree. The two are
// told apart by shape as well as by key: a client policy carries a MAC, an
// equipment entry an adapter and an address.
type legacyConfig struct {
	Enabled      bool     `json:"enabled,omitempty"`
	DownloadMbps float64  `json:"download_mbps,omitempty"`
	UploadMbps   float64  `json:"upload_mbps,omitempty"`
	Clients      []Client `json:"clients,omitempty"`
	Devices      []Client `json:"devices,omitempty"`
}

// HasLegacyKeys reports whether a stored subtree still uses the old `devices`
// key, so the daemon can rewrite it once.
func HasLegacyKeys(raw []byte) bool {
	var probe struct {
		Clients json.RawMessage `json:"clients"`
		Devices json.RawMessage `json:"devices"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return probe.Devices != nil && probe.Clients == nil
}

func (c *Config) Normalize() {
	c.Enabled = false // Legacy compatibility only; policies activate themselves.
	for i := range c.Clients {
		if mac, err := core.NormalizeMAC(c.Clients[i].MAC); err == nil {
			c.Clients[i].MAC = mac
		}
		if c.Clients[i].Priority == "" {
			c.Clients[i].Priority = Normal
		}
	}
	slices.SortFunc(c.Clients, func(a, b Client) int {
		if a.MAC < b.MAC {
			return -1
		}
		if a.MAC > b.MAC {
			return 1
		}
		return 0
	})
}
func validRate(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && (v == 0 || v >= 0.001) && v <= 100000
}
func (c Config) Validate() error {
	if len(c.Clients) > 4096 {
		return fmt.Errorf("at most 4096 client policies are supported")
	}
	if !validRate(c.DownloadMbps) || !validRate(c.UploadMbps) {
		return fmt.Errorf("link rates must be between 0 and 100000 Mbps")
	}
	// Enabled is retained only for compatibility with older documents. Policies
	// take effect whenever they exist; there is no separate enable switch.
	seen := map[string]bool{}
	for _, d := range c.Clients {
		mac, err := core.NormalizeMAC(d.MAC)
		if err != nil {
			return fmt.Errorf("client MAC %q: %w", d.MAC, err)
		}
		if seen[mac] {
			return fmt.Errorf("client %s appears twice", mac)
		}
		seen[mac] = true
		if d.Priority != "" && d.Priority != High && d.Priority != Normal && d.Priority != Low {
			return fmt.Errorf("client %s: unknown priority %q", mac, d.Priority)
		}
		if !validRate(d.DownloadMbps) || !validRate(d.UploadMbps) {
			return fmt.Errorf("client %s: limits must be between 0 and 100000 Mbps", mac)
		}
	}
	return nil
}

func (c Config) Requested() bool {
	return slices.ContainsFunc(c.Clients, func(d Client) bool { return d.Requested() })
}

func (d Client) Requested() bool {
	return (d.Priority != "" && d.Priority != Normal) || d.DownloadMbps > 0 || d.UploadMbps > 0
}
func Parse(b []byte) (Config, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var l legacyConfig
	if err := dec.Decode(&l); err != nil {
		return Config{}, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return Config{}, fmt.Errorf("extra JSON after configuration: %v", err)
	}
	c := Config{Enabled: l.Enabled, DownloadMbps: l.DownloadMbps, UploadMbps: l.UploadMbps, Clients: l.Clients}
	switch {
	case l.Clients != nil && l.Devices != nil:
		return Config{}, fmt.Errorf("both clients and clients are present")
	case l.Devices != nil:
		c.Clients = l.Devices
		c.Legacy = true
	}
	c.Normalize()
	return c, c.Validate()
}

// MarshalConfig renders intent for the store and for migrations.
func MarshalConfig(c Config) ([]byte, error) {
	c.Normalize()
	return json.Marshal(c)
}

func FromDocument(doc core.Document) (Config, error) {
	b, ok := doc.Raw(ModuleName)
	if !ok {
		return Config{}, nil
	}
	return Parse(b)
}
func (c Config) Client(mac string) Client {
	if normalized, err := core.NormalizeMAC(mac); err == nil {
		mac = normalized
	}
	for _, d := range c.Clients {
		if d.MAC == mac {
			return d
		}
	}
	return Client{MAC: mac, Priority: Normal}
}
func (c *Config) SetClient(d Client) {
	if mac, err := core.NormalizeMAC(d.MAC); err == nil {
		d.MAC = mac
	}
	c.RemoveClient(d.MAC)
	if (d.Priority != "" && d.Priority != Normal) || d.DownloadMbps > 0 || d.UploadMbps > 0 {
		c.Clients = append(c.Clients, d)
	}
	c.Normalize()
}
func (c *Config) RemoveClient(mac string) {
	if normalized, err := core.NormalizeMAC(mac); err == nil {
		mac = normalized
	}
	c.Clients = slices.DeleteFunc(c.Clients, func(d Client) bool { return d.MAC == mac })
}
