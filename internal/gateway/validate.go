package gateway

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/socksout"
)

// Validation is pure: no files, no netlink, no root. That is what lets the
// whole rule set be tested without a network (design.md §5.3.1), and it is why
// every check that needs the system reads it through LinkView rather than
// looking for itself.
//
// The rules here are docs/gateway.md §5's failure modes, which are all the same
// sentence from the operator's side — *"I set an exit and the internet
// stopped"*. That is why they are checks and declared behaviour rather than
// troubleshooting notes: each one is cheap to answer before applying and
// expensive to diagnose after.

// Problem is one validation finding, addressed by a JSON-ish path so a UI can
// attach it to the field that caused it. Mirrors internal/dhcp's and
// internal/dns's shape, and is converted to core.Problem at the view boundary,
// so a UI needs one renderer for every module's complaints rather than one per
// module.
type Problem struct {
	Path    string
	Message string
}

func (p Problem) String() string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}

// Result separates the fatal from the merely suspect.
type Result struct {
	Errors   []Problem
	Warnings []Problem
}

func (r *Result) errorf(path, format string, args ...any) {
	r.Errors = append(r.Errors, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
}

func (r *Result) warnf(path, format string, args ...any) {
	r.Warnings = append(r.Warnings, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
}

// OK reports whether the config can be applied.
func (r Result) OK() bool { return len(r.Errors) == 0 }

// Err collapses the errors into one, or nil.
func (r Result) Err() error {
	if r.OK() {
		return nil
	}
	msgs := make([]error, len(r.Errors))
	for i, p := range r.Errors {
		msgs[i] = errors.New(p.String())
	}
	return fmt.Errorf("invalid gateway configuration:\n  %w", errors.Join(msgs...))
}

// Validate checks intent against itself and against link's facts.
func Validate(c Config, links LinkView) Result {
	var r Result
	if !validLatencyTarget(c.LatencyTarget) {
		r.errorf("latency_target", "choose a listed Internet target")
	}
	if err := validateCustomSites(c.LatencySites); err != nil {
		r.errorf("latency_sites", "%s", err)
	}
	for _, site := range c.LatencySites {
		if site.Exit != "" {
			if _, ok := c.Find(site.Exit); !ok {
				r.errorf("latency_sites", "%s: unknown exit %s", site.Name, site.Exit)
			}
		}
	}
	seenCommon := map[string]bool{}
	for _, site := range c.LatencyCommon {
		if _, ok := commonLatencySite(site.Name); !ok {
			r.errorf("latency_common", "%q is not a common site", site.Name)
			continue
		}
		if seenCommon[site.Name] {
			r.errorf("latency_common", "duplicate common site %s", site.Name)
			continue
		}
		seenCommon[site.Name] = true
		if site.Exit != "" {
			if _, ok := c.Find(site.Exit); !ok {
				r.errorf("latency_common", "%s: unknown exit %s", site.Name, site.Exit)
			}
		}
	}

	validateExits(&r, c, links)
	validateDefault(&r, c)
	validateAssignments(&r, c, links)
	validateDevices(&r, c, links)
	validateDestinations(&r, c)

	return r
}

func validateDevices(r *Result, c Config, links LinkView) {
	infos, err := links.Interfaces()
	usable := false
	if err == nil {
		for _, info := range infos {
			if info.Adopted && len(info.Prefixes) > 0 {
				usable = true
				break
			}
		}
	}
	seen := map[string]int{}
	for i, a := range c.Clients {
		path := fmt.Sprintf("devices[%d]", i)
		mac, err := core.NormalizeMAC(a.MAC)
		if err != nil {
			r.errorf(path+".mac", "invalid client MAC %q: %v", a.MAC, err)
		} else if len(strings.Split(mac, ":")) != 6 {
			r.errorf(path+".mac", "a client override needs a 6-byte Ethernet MAC address")
		} else if first, ok := seen[mac]; ok {
			r.errorf(path+".mac", "%s is already assigned at devices[%d]", mac, first)
		} else {
			seen[mac] = i
		}
		if a.Exit != "" {
			if _, ok := c.Find(a.Exit); !ok {
				r.errorf(path+".exit", "there is no exit called %q", a.Exit)
			}
		}
		if !usable {
			r.warnf(path, "no adopted interface has a source prefix; this client override will not match traffic yet")
		}
	}
}

func validateExits(r *Result, c Config, links LinkView) {
	seen := map[string]int{}
	slots := map[int]int{}

	for i, e := range c.Exits {
		path := fmt.Sprintf("exits[%d]", i)

		switch {
		case e.Name == "":
			r.errorf(path+".name", "an exit needs a name; it is how everything else refers to it")
		case utf8.RuneCountInString(e.Name) > MaxNameLen:
			r.errorf(path+".name", "name is %d characters; the limit is %d",
				utf8.RuneCountInString(e.Name), MaxNameLen)
		default:
			if bad, ok := hasControlChar(e.Name); ok {
				// A newline in a name would break every single-line rendering
				// of it, including the CLI's table and the nftables comment we
				// render it into.
				r.errorf(path+".name", "name contains a control character (%q)", bad)
			}
			if first, dup := seen[e.Name]; dup {
				r.errorf(path+".name",
					"%q is already an exit at exits[%d]; exits are referred to by name, so each appears once",
					e.Name, first)
			} else {
				seen[e.Name] = i
			}
		}

		switch {
		case e.Slot == 0:
			r.errorf(path+".slot",
				"no slot could be allocated; the box is at its limit of %d exits", MaxSlot)
		case !Slot(e.Slot).Valid():
			r.errorf(path+".slot",
				"slot %d is outside the range 1–%d that this module owns", e.Slot, MaxSlot)
		default:
			if first, dup := slots[e.Slot]; dup {
				// Only reachable from a hand-edited file — allocateSlots clears
				// duplicates — but worth a real message, because two exits
				// sharing a slot means two exits sharing a route table, and the
				// symptom is that one of them silently carries the other's
				// traffic.
				r.errorf(path+".slot",
					"slot %d is already used by exits[%d]; each exit needs its own route table",
					e.Slot, first)
			} else {
				slots[e.Slot] = i
			}
		}

		validateVia(r, path+".via", e, links)
		if e.DNS != nil {
			if e.Via.Kind == ViaBlocked {
				r.errorf(path+".dns", "a blocked exit cannot resolve DNS")
			}
			switch e.DNS.Mode {
			case "olr":
				if e.DNS.Server.IsValid() {
					r.errorf(path+".dns.server", "OLR DNS does not take a server")
				}
			case "exit":
				if e.Via.Kind != ViaNextHop {
					r.errorf(path+".dns.mode", "exit DNS needs a next-hop exit")
				}
				if e.DNS.Server.IsValid() {
					r.errorf(path+".dns.server", "exit DNS uses the next hop on port 53")
				}
				if e.Via.NextHop != nil && e.Via.NextHop.Is6() {
					r.errorf(path+".dns.mode", "IPv6 next-hop DNS is not supported for device selection yet")
				}
				if e.Via.NextHop != nil {
					if infos, err := links.Interfaces(); err == nil {
						for _, info := range infos {
							for _, prefix := range info.Prefixes {
								if prefix.Addr() == *e.Via.NextHop {
									r.errorf(path+".dns.mode", "the next hop is this router; DNS forwarding would loop")
								}
							}
						}
					}
				}
			case "custom":
				if !e.DNS.Server.IsValid() || e.DNS.Server.Port() == 0 ||
					e.DNS.Server.Addr().IsUnspecified() || e.DNS.Server.Addr().IsMulticast() ||
					e.DNS.Server.Addr().IsLoopback() || e.DNS.Server.Addr().Is6() {
					r.errorf(path+".dns.server", "custom DNS needs an IPv4 unicast IP and port")
				} else if infos, err := links.Interfaces(); err == nil {
					for _, info := range infos {
						for _, prefix := range info.Prefixes {
							if prefix.Addr() == e.DNS.Server.Addr() {
								r.errorf(path+".dns.server", "%s belongs to this router; forwarding DNS to itself would loop", e.DNS.Server.Addr())
							}
						}
					}
				}
			default:
				r.errorf(path+".dns.mode", "unknown DNS mode %q (want olr, exit or custom)", e.DNS.Mode)
			}
		}

		if !e.IPv6.Valid() {
			r.errorf(path+".ipv6", "unknown IPv6 handling %q; valid values are %s",
				e.IPv6, join(IPv6Modes()))
		}
		if !e.OnFailure.Valid() {
			r.errorf(path+".on_failure", "unknown failure behaviour %q; valid values are %s",
				e.OnFailure, join(FailureModes()))
		}

		// §5.5 is emphatic that failing open is available and never silent, so
		// the operator gets told what they chose rather than only what they
		// typed.
		if e.Via.Kind == ViaInterface && e.Via.Interface == socksout.Interface && e.IPv6OrDefault() == IPv6Direct {
			r.errorf(path+".ipv6", "the managed SOCKS5 TUN is IPv4-only; direct IPv6 would bypass the proxy")
		}
		if e.Via.Kind == ViaInterface && e.Via.Interface == socksout.Interface && e.OnFailure.OrDefault() == FailDirect {
			r.errorf(path+".on_failure", "the managed SOCKS5 TUN must fail closed; direct fallback would bypass the proxy")
		}
		if e.OnFailure.OrDefault() == FailDirect && c.InUse(e.Name) {
			r.warnf(path+".on_failure",
				"when %q is down its traffic will take the box's normal path instead of stopping, "+
					"which sends exactly the traffic you asked to route out unrouted",
				e.Name)
		}

		if e.SNAT != nil && e.Via.Kind != ViaNextHop {
			r.warnf(path+".snat",
				"snat only applies to a next_hop exit and is ignored for a %s one", e.Via.Kind)
		}

		validateProbe(r, path+".probe", e, c)
		validateIPv6(r, path+".ipv6", e)
	}
}

func validateVia(r *Result, path string, e Exit, links LinkView) {
	if !e.Via.Kind.Valid() {
		if e.Via.Kind == "" {
			r.errorf(path+".kind", "an exit needs a form; valid values are %s", join(ViaKinds()))
		} else {
			r.errorf(path+".kind", "unknown exit form %q; valid values are %s",
				e.Via.Kind, join(ViaKinds()))
		}
		return
	}

	// Exactly the fields for this form, and nothing else. Ignoring a stray
	// field would mean an operator who switched an exit from next_hop to
	// interface leaves an address behind that reads like it is still in force.
	switch e.Via.Kind {
	case ViaInterface:
		if e.Via.Interface == "" {
			r.errorf(path+".interface", "an interface exit needs an interface name")
		} else if _, err := links.Interface(e.Via.Interface); err != nil && e.Via.Interface != socksout.Interface {
			// A warning, not an error, and the difference matters: WireGuard,
			// PPPoE and proxy TUN devices are all created by something other
			// than us and routinely do not exist yet when the exit is
			// configured. Refusing would make the correct order of operations
			// "start the tunnel, then tell olr about it", which is backwards.
			r.warnf(path+".interface",
				"%q is not an interface this box knows about yet; "+
					"the exit will not carry traffic until it appears",
				e.Via.Interface)
		}
		if e.Via.NextHop != nil {
			r.errorf(path+".next_hop", "an interface exit does not take a next hop")
		}
		if e.Via.Dev != "" {
			r.errorf(path+".dev", "an interface exit names its device in `interface`, not in `dev`")
		}

	case ViaNextHop:
		switch {
		case e.Via.NextHop == nil:
			r.errorf(path+".next_hop", "a next_hop exit needs the address of the box that takes the traffic")
		case !e.Via.NextHop.IsValid():
			r.errorf(path+".next_hop", "%q is not a valid address", e.Via.NextHop)
		case e.Via.NextHop.IsUnspecified():
			r.errorf(path+".next_hop", "the unspecified address is not a next hop")
		case e.Via.NextHop.IsLoopback():
			r.errorf(path+".next_hop",
				"%s is this box's own loopback; a next hop is another machine", e.Via.NextHop)
		default:
			validateNextHopReachable(r, path, e, links)
		}
		if e.Via.Interface != "" {
			r.errorf(path+".interface", "a next_hop exit names its device in `dev`, not in `interface`")
		}

	case ViaBlocked:
		if e.Via.Interface != "" || e.Via.NextHop != nil || e.Via.Dev != "" {
			r.errorf(path, "a blocked exit takes no interface, next hop or device")
		}
	}
}

// validateNextHopReachable is §5.1's second row.
//
// A next hop has to fall inside a prefix we hold, or the kernel refuses the
// route. The mistake this catches is specific and common: entering the proxy's
// *public* address instead of its LAN one, which looks entirely reasonable and
// produces an error message from netlink that names neither the exit nor the
// address.
func validateNextHopReachable(r *Result, path string, e Exit, links LinkView) {
	hop := *e.Via.NextHop

	if e.Via.Dev != "" {
		info, err := links.Interface(e.Via.Dev)
		if err != nil {
			r.errorf(path+".dev", "%q is not an interface this box knows about", e.Via.Dev)
			return
		}
		if !info.Adopted {
			r.errorf(path+".dev",
				"%q has not been adopted; run `olr adopt %s` before routing through it",
				e.Via.Dev, e.Via.Dev)
		}
		if !info.Contains(hop) {
			r.errorf(path+".next_hop",
				"%s is not on any subnet configured on %s, so it cannot be reached directly; "+
					"a next hop is a box on a network this one is attached to",
				hop, e.Via.Dev)
		}
		return
	}

	// No dev given: the kernel will resolve it from the main table, which is
	// right whenever the next hop is on a network we hold an address on. Check
	// that it is, and name the interface we found so the operator can confirm.
	if _, ok := InterfaceWithPrefixContaining(links, hop); !ok {
		r.errorf(path+".next_hop",
			"%s is not on any subnet this box has an address on, so it cannot be reached directly. "+
				"If this is a proxy box, use its address on your own network rather than its public one",
			hop)
	}
}

// validateIPv6 is §5.4, which is not optional: it is the difference between
// working and appearing to work.
func validateIPv6(r *Result, path string, e Exit) {
	if e.Via.Kind == ViaBlocked {
		return
	}
	if e.IPv6OrDefault() != IPv6Direct {
		return
	}
	r.warnf(path,
		"IPv6 from sources using %q will take the box's normal path rather than this exit, "+
			"so every site with an AAAA record bypasses it at full speed and nothing looks wrong",
		e.Name)
}

func validateProbe(r *Result, path string, e Exit, c Config) {
	if e.Probe == nil {
		// docs/dns.md §1.2 makes this more than a nicety: the topology rule
		// that DHCP hands out olr and only olr is *paid for* by olr being able
		// to re-point a dead exit, because IPv4 has no gateway failover at the
		// device layer. An unprobed exit in use is a single point of failure
		// with no detection.
		if c.InUse(e.Name) && e.Via.Kind != ViaBlocked {
			r.warnf(path,
				"%q carries traffic but is never health-checked, so if it stops forwarding "+
					"the devices using it are offline until somebody notices",
				e.Name)
		}
		return
	}

	if e.Via.Kind == ViaBlocked {
		r.warnf(path, "a blocked exit is never down, so its health check does nothing")
	}

	p := *e.Probe
	if !p.Target.IsValid() || !p.Target.Addr().IsValid() {
		r.errorf(path+".target",
			"a health check needs something to connect to, as address:port — for example 1.1.1.1:443")
		return
	}
	if p.Target.Port() == 0 {
		r.errorf(path+".target", "a health check target needs a port, for example %s:443", p.Target.Addr())
	}
	if p.Target.Addr().IsUnspecified() || p.Target.Addr().IsLoopback() {
		// A probe that never leaves the box passes while the exit is dead,
		// which is worse than no probe at all: it reports health it did not
		// measure. §5.5's whole argument is that the check has to traverse the
		// path it claims to be checking.
		r.errorf(path+".target",
			"%s is on this box, so connecting to it would succeed whether or not %q is working; "+
				"use an address on the far side of the exit",
			p.Target.Addr(), e.Name)
	}

	resolved := p.Resolved()
	if p.Interval != 0 && resolved.Interval < MinProbeInterval {
		r.errorf(path+".interval",
			"a health check every %s is a connection flood against somebody else's server; the floor is %s",
			resolved.Interval, MinProbeInterval)
	}
	if resolved.Timeout >= resolved.Interval {
		r.errorf(path+".timeout",
			"a timeout of %s with an interval of %s means attempts overlap; the timeout has to be shorter",
			resolved.Timeout, resolved.Interval)
	}
	if p.Failures < 0 || p.Successes < 0 {
		r.errorf(path, "failure and success counts cannot be negative")
	}
}

func validateDefault(r *Result, c Config) {
	if c.Default == "" {
		return
	}
	if _, ok := c.Find(c.Default); !ok {
		r.errorf("default",
			"there is no exit called %q; the box-wide setting has to name one of %s, "+
				"or be empty for the box's own default route",
			c.Default, quoteNames(c))
	}
}

func validateAssignments(r *Result, c Config, links LinkView) {
	seen := map[string]int{}

	for i, a := range c.Interfaces {
		path := fmt.Sprintf("interfaces[%d]", i)

		switch {
		case a.Interface == "":
			r.errorf(path+".interface", "an assignment needs the network it applies to")
		default:
			if first, dup := seen[a.Interface]; dup {
				// §2.3, one rung down. A source with two answers is genuinely
				// ambiguous, and inventing a tie-break produces behaviour
				// nobody can predict from the screen — so we refuse instead.
				r.errorf(path+".interface",
					"%q is already assigned at interfaces[%d]; pick one exit for it",
					a.Interface, first)
			} else {
				seen[a.Interface] = i
			}

			info, err := links.Interface(a.Interface)
			switch {
			case err != nil:
				r.errorf(path+".interface", "%q is not an interface this box knows about", a.Interface)
			case !info.Adopted:
				r.errorf(path+".interface",
					"%q has not been adopted; run `olr adopt %s` before routing its traffic",
					a.Interface, a.Interface)
			case len(info.Prefixes) == 0:
				r.errorf(path+".interface",
					"%q has no addresses, so there is no source range to match traffic on",
					a.Interface)
			case !info.Up:
				r.warnf(path+".interface", "%q is down; the assignment will take effect when it comes up",
					a.Interface)
			}
		}

		if a.Exit == "" {
			continue
		}
		if _, ok := c.Find(a.Exit); !ok {
			r.errorf(path+".exit",
				"there is no exit called %q; %s has to name one of %s, or be empty to use the box-wide setting",
				a.Exit, a.Interface, quoteNames(c))
		}
	}
}

func hasControlChar(s string) (rune, bool) {
	for _, r := range s {
		// Tab included: it is a control character that would misalign a table.
		if unicode.IsControl(r) {
			return r, true
		}
	}
	return 0, false
}

// join renders a vocabulary for an error message. Generic so that adding a
// value to any of the three enums cannot leave one message behind.
func join[T ~string](values []T) string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, string(v))
	}
	return strings.Join(out, ", ")
}

// quoteNames lists the exits that do exist, because "there is no exit called
// X" is only half an answer.
func quoteNames(c Config) string {
	if len(c.Exits) == 0 {
		return "(no exits are configured yet)"
	}
	out := make([]string, 0, len(c.Exits))
	for _, e := range c.Exits {
		out = append(out, `"`+e.Name+`"`)
	}
	return strings.Join(out, ", ")
}

// problems converts this module's findings into core's wire shape, so that
// every module reports a bad field the same way (see internal/dhcp/view.go).
func problems(in []Problem) []core.Problem {
	if len(in) == 0 {
		return nil
	}
	out := make([]core.Problem, 0, len(in))
	for _, p := range in {
		out = append(out, core.Problem{Path: p.Path, Message: p.Message})
	}
	return out
}
