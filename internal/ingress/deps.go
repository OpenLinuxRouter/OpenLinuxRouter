package ingress

import (
	"errors"
	"fmt"
	"net/netip"
)

// This module's read-only windows onto the two modules it depends on.
//
// Declared here by the consumer, and adapted in internal/daemon, which is the
// pattern dhcp uses for LinkView: `ingress` imports neither `dns` nor
// `clients`, states exactly the facts it needs, and never keeps a copy of
// either (design.md §4.1).

// DNSView is the window onto the `dns` module.
//
// One fact, read for one reason: the suffix the resolver answers
// authoritatively. A published domain that falls inside it would never be
// asked of public DNS at all — the resolver would answer NXDOMAIN for a name
// that exists, and nothing would say why. validate.go refuses that, and this
// is the only thing it needs to know.
//
// Note what is *not* here any more: the published names. They are the
// operator's records in their own zone (docs/ingress.md §3), so this module
// neither writes them into the resolver nor reads the resolver's names back.
type DNSView interface {
	// LocalDomain returns the suffix local names live under — `dns`'s
	// LocalDomain, resolved through its own default.
	LocalDomain() string
}

// ClientView is the window onto the `clients` module.
//
// Address resolution is read-through per request rather than cached, for the
// same reason dhcp reads link per request: a cached address is a copy, and a
// copy of something that changes is drift waiting for a lease to expire.
type ClientView interface {
	// Client returns what is known about a client, or ErrNoSuchClient.
	Client(name string) (ClientInfo, error)
}

// ClientInfo is the subset of a client's state that publishing depends on.
type ClientInfo struct {
	// MAC is the stable identity; Name is only a label.
	MAC string `json:"mac,omitempty"`
	// Name is the client's name in the `clients` module.
	Name string `json:"name,omitempty"`

	// Addr is where the client is right now.
	Addr netip.Addr `json:"address,omitempty"`

	// Fixed reports whether Addr is a fixed address owned by `dhcp`, as
	// opposed to whatever the client happens to hold from a dynamic pool.
	//
	// This is the difference between a published service that keeps working and
	// one that quietly proxies to a stranger's laptop after a lease turns over,
	// so validate.go refuses the dynamic case rather than warning about it.
	// docs/ingress.md §1.2 has the argument, and §5.6's rule that automatic
	// behaviour is declared rather than inferred is why the answer is a refusal
	// carrying a remedy instead of olr silently pinning the address itself.
	Fixed bool `json:"fixed"`
}

// ResolveClientReferences upgrades legacy names when they still identify a
// client. Unknown references stay visible so they can be repaired explicitly.
func (c *Config) ResolveClientReferences(clients ClientView) {
	if clients == nil {
		return
	}
	for i := range c.Services {
		u := &c.Services[i].Upstream
		if u.Client == "" {
			continue
		}
		if info, err := clients.Client(u.Client); err == nil && info.MAC != "" {
			u.Client = info.MAC
		}
	}
}

// ErrNoSuchClient is returned by ClientView.Client for an unknown name.
var ErrNoSuchClient = errors.New("no such client")

// StaticDNS is a DNSView backed by literal values, for tests.
type StaticDNS struct {
	Domain string
}

// LocalDomain implements DNSView.
func (s StaticDNS) LocalDomain() string { return s.Domain }

// StaticClients is a ClientView backed by a map, for tests.
//
// The validation rules are the largest thing in this module and none of them
// need a network to exercise, which is what §5.3.1 is for.
type StaticClients map[string]ClientInfo

// Client implements ClientView.
func (s StaticClients) Client(name string) (ClientInfo, error) {
	info, ok := s[name]
	if !ok {
		return ClientInfo{}, fmt.Errorf("%q: %w", name, ErrNoSuchClient)
	}
	if info.Name == "" {
		info.Name = name
	}
	return info, nil
}
