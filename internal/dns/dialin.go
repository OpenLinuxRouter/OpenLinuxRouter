package dns

import (
	"net/netip"
	"slices"
)

// The address a client that dials in resolves through.
//
// `remote` writes `DNS = <this box's address on the dial-in network>` into
// every client configuration it prints, and prints it once — the file lives on
// a phone afterwards and cannot be reissued (docs/remote-access.md §6.3). So
// answering on that address is not a convenience an operator can be asked to
// arrange; it is the other half of a promise `remote` has already made.
//
// It used to be neither module's job, and that is what "connected but offline"
// is: a listen pin of 172.16.1.1 answers at home, the tunnel's 10.6.0.1 answers
// nowhere, and every lookup from the phone gets ICMP port-unreachable while the
// tunnel itself looks perfectly healthy. Both modules were internally
// consistent; nobody owned the intersection.
//
// The direction is `remote` publishes and `dns` subscribes — the same shape as
// ingress→dns in design.md §4.1, and for the same reason: the fact has one
// owner, and the module that has to act on it reads it rather than keeping a
// copy. The reverse direction is not available even though it looks tidier,
// because a client configuration naming an address we decided to stop serving
// cannot be repaired from here.
//
// This is a structural rule, not an `auto` mode (design.md §5.6): the address
// is part of the tunnel's declared intent, so answering on it is a
// consequence of role, like "never serve DHCP on a WAN interface". There is no
// switch, because a switch would be a way to break the promise again.

// DialInView is this module's read-only window onto the remote-access module.
type DialInView interface {
	// DialIn reports whether clients may dial in and, if so, this box's own
	// address on that network.
	DialIn() (DialInInfo, error)
}

// DialInInfo is the one fact this module needs from the tunnel.
type DialInInfo struct {
	// Enabled reports whether the tunnel exists at all.
	Enabled bool `json:"enabled"`

	// Addr is this box's address on the dial-in network. Invalid when there is
	// no tunnel.
	Addr netip.Addr `json:"addr,omitempty"`
}

// DialInAddr reads the view, folding "no view", "off" and "no address" into one
// answer: there is no address clients are told to resolve through.
func DialInAddr(v DialInView) (netip.Addr, bool, error) {
	if v == nil {
		return netip.Addr{}, false, nil
	}
	info, err := v.DialIn()
	if err != nil {
		return netip.Addr{}, false, err
	}
	if !info.Enabled || !info.Addr.IsValid() || info.Addr.IsUnspecified() {
		return netip.Addr{}, false, nil
	}
	return info.Addr, true, nil
}

// ServedListen is every address the relay answers on: the operator's pins plus
// the ones olr itself promised to clients.
//
// Empty still means the wildcard, and stays empty when the operator pinned
// nothing — the wildcard already covers every address this box holds, including
// ones that do not exist yet, and a second, pinned copy would be exactly the
// copy of a kernel fact docs/dns.md §4.8 warns about.
func ServedListen(c Config, dialIn DialInView) ([]netip.AddrPort, error) {
	service, err := ServiceListen(c, dialIn)
	if err != nil {
		return nil, err
	}
	if len(c.Listen) == 0 {
		return nil, nil
	}
	out := slices.Clone(c.Listen)
	for _, addr := range service {
		if !slices.Contains(out, addr) {
			out = append(out, addr)
		}
	}
	slices.SortStableFunc(out, compareAddrPort)
	return out, nil
}

// ServiceListen is the part of ServedListen that olr supplies rather than the
// operator: addresses the relay must answer on once they exist, and retries
// rather than requires (dnsrelay.Config.AlsoListen).
//
// Returned even under the wildcard, where rendering drops them as redundant:
// the question "which addresses does olr owe a client" has the same answer
// either way, and the two callers that ask it — the renderer and the derived
// note below — should not each decide.
func ServiceListen(c Config, dialIn DialInView) ([]netip.AddrPort, error) {
	addr, ok, err := DialInAddr(dialIn)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return []netip.AddrPort{netip.AddrPortFrom(addr, c.RelayPort())}, nil
}
