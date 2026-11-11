package devices

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/invopop/jsonschema"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

var deviceID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
var envName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)

func (Role) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{Type: "string", Enum: []any{string(Router), string(Switch), string(AP)}}
}

func UnmarshalConfig(data []byte) (Config, error) {
	var c Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return c, fmt.Errorf("invalid network configuration: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return c, fmt.Errorf("invalid network configuration: trailing JSON")
	}
	c.Normalize()
	return c, c.Validate()
}
func (c *Config) Normalize() {
	for i := range c.Devices {
		d := &c.Devices[i]
		d.Name = strings.TrimSpace(d.Name)
		d.Address = strings.TrimRight(d.Address, "/")
		slices.Sort(d.Roles)
		d.Roles = slices.Compact(d.Roles)
	}
	slices.SortFunc(c.Devices, func(a, b Device) int { return strings.Compare(a.ID, b.ID) })
}
func (c Config) Validate() error {
	if len(c.Devices) > 256 {
		return fmt.Errorf("%w: at most 256 network devices", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, d := range c.Devices {
		if !deviceID.MatchString(d.ID) || seen[d.ID] {
			return fmt.Errorf("%w: device IDs must be unique and contain 1–64 letters, digits, underscores or hyphens", ErrInvalid)
		}
		seen[d.ID] = true
		if d.Adapter != "mercury" && d.Adapter != "openwrt" {
			return fmt.Errorf("%w: unknown adapter for device %s", ErrInvalid, d.ID)
		}
		u, err := url.Parse(d.Address)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("%w: device %s requires an HTTP(S) origin without credentials, path, query or fragment", ErrInvalid, d.ID)
		}
		if len(d.Address) > 2048 || len(d.Name) > 128 || len(d.Username) > 128 {
			return fmt.Errorf("%w: device %s field too long", ErrInvalid, d.ID)
		}
		if d.PasswordEnv != "" && !envName.MatchString(d.PasswordEnv) {
			return fmt.Errorf("%w: device %s has an invalid password environment reference", ErrInvalid, d.ID)
		}
		if d.Password == RedactedSecret {
			return fmt.Errorf("%w: device %s stored the redacted mask as its password", ErrInvalid, d.ID)
		}
		if len(d.Password) > 1024 {
			return fmt.Errorf("%w: device %s credential is too long", ErrInvalid, d.ID)
		}
		for _, r := range d.Roles {
			if r != Router && r != Switch && r != AP {
				return fmt.Errorf("%w: unknown role for device %s", ErrInvalid, d.ID)
			}
		}
	}
	return nil
}
func FromDocument(doc core.Document) (Config, error) {
	raw, ok := doc.Raw(ModuleName)
	if !ok {
		return Config{}, nil
	}
	return UnmarshalConfig(raw)
}
func Load(store *core.Store) (Config, error) {
	doc, err := store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(doc)
}
func Save(store *core.Store, c Config) (Config, error) {
	c.Normalize()
	if err := c.Validate(); err != nil {
		return c, err
	}
	doc, err := store.Load()
	if err != nil {
		return c, err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return c, err
	}
	doc.Set(ModuleName, data)
	return c, store.Save(doc)
}

// restoreSecrets turns the read-surface mask back into the stored credential.
// A form that showed the mask and sent it back must not overwrite the real
// password with eight asterisks.
func restoreSecrets(next, previous Config) Config {
	stored := make(map[string]string, len(previous.Devices))
	for _, d := range previous.Devices {
		stored[d.ID] = d.Password
	}
	for i := range next.Devices {
		if next.Devices[i].Password != RedactedSecret {
			continue
		}
		// A mask for a device that was not there to mask is not "unchanged"; it
		// stays a literal and fails validation rather than storing no credential.
		if password, ok := stored[next.Devices[i].ID]; ok {
			next.Devices[i].Password = password
		}
	}
	return next
}
