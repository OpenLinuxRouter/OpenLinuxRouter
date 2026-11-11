package dhcp

import (
	"fmt"
	"io/fs"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Paths locates everything the backend writes or tells the daemon about.
//
// It is a struct rather than a set of constants so tests can render into a
// temporary directory and still get a config whose internal references are
// self-consistent — the rendered file names these paths, so overriding them
// halfway would produce a config that pointed at the real system.
type Paths struct {
	// Conf is the daemon's main configuration file.
	Conf string
	// HostsDir holds one file per reservation.
	HostsDir string
	// OptsDir holds one file of DHCP options per pool.
	OptsDir string
	// LeaseFile is the lease database.
	LeaseFile string
	// PIDFile is where the daemon writes its pid.
	//
	// It lives in the module's own runtime subdirectory, not directly in
	// /run/olr. The unit declares that subdirectory with RuntimeDirectory=, and
	// systemd deletes a RuntimeDirectory when its unit stops — so naming
	// /run/olr itself would make stopping the DHCP backend delete olrd's
	// control socket and the §5.5 guard snapshots along with it.
	PIDFile string
}

// DefaultPaths is the on-disk layout for a real install.
//
// Generated files sit under our own directory with an ownership header
// (design.md §3.4/§7), never in the daemon's own configuration directories.
func DefaultPaths() Paths {
	return RootedPaths("")
}

// RootedPaths is the same layout relocated under root.
//
// An empty root gives the real one. A non-empty root is for development: it
// lets olrd run as an ordinary user against a scratch directory, so the whole
// render-plan-apply path — the part the WebUI drives — can be exercised without
// root, systemd, or writing into a real /etc. The paths stay internally
// consistent because the rendered config names them, which is the same reason
// Paths is a struct rather than a set of constants.
func RootedPaths(root string) Paths {
	rendered := filepath.Join(root, "/etc/open-linux-router/rendered/dhcp")
	return Paths{
		Conf:      filepath.Join(rendered, "dnsmasq.conf"),
		HostsDir:  filepath.Join(rendered, "hosts.d"),
		OptsDir:   filepath.Join(rendered, "opts.d"),
		LeaseFile: filepath.Join(root, "/var/lib/open-linux-router/dhcp/leases"),
		PIDFile:   filepath.Join(root, "/run/olr/dhcp/dhcp.pid"),
	}
}

// File is one rendered file.
type File struct {
	// Path is absolute.
	Path string
	// Mode is the file's permissions.
	Mode fs.FileMode
	// Data is the full contents.
	Data []byte
	// Reloadable reports whether the daemon picks up a change to this file
	// without a restart. See Rendered.
	Reloadable bool
}

// Rendered is the complete set of files a config produces. Always sorted by
// path, because drift detection compares rendered output against what is on
// disk and a stable order is what makes that comparison meaningful.
//
// The Reloadable split is the load-bearing part of the layout, not tidiness.
// dnsmasq re-reads its hosts and options directories on SIGHUP but never
// re-reads its configuration file, so putting reservations and per-pool options
// in those directories is exactly what turns the most common edit — adding a
// reservation — into a reload instead of a restart.
type Rendered struct {
	Files []File
}

// Get returns a file by path.
func (r Rendered) Get(path string) (File, bool) {
	i := slices.IndexFunc(r.Files, func(f File) bool { return f.Path == path })
	if i < 0 {
		return File{}, false
	}
	return r.Files[i], true
}

// Paths lists the rendered paths, in order.
func (r Rendered) Paths() []string {
	out := make([]string, len(r.Files))
	for i, f := range r.Files {
		out[i] = f.Path
	}
	return out
}

func (r *Rendered) add(f File) {
	r.Files = append(r.Files, f)
}

func (r *Rendered) sort() {
	slices.SortFunc(r.Files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
}

// Dnsmasq renders a config into dnsmasq's files and names the unit that runs
// it.
//
// There is deliberately no Backend interface here. design.md §10 commits to
// dnsmasq permanently — it serves DHCPv4, DHCPv6 and RA in one daemon where Kea
// would need radvd as a third — so by §3.1's own test there is nothing to
// iterate over and exactly one implementation. An interface would also have to
// carry reloadable(), which is a question about *dnsmasq's* directory-reload
// behaviour and means nothing to another server.
type Dnsmasq struct {
	Paths Paths

	// Source is the intent file named in every generated file's ownership
	// header. An operator who finds one of these files needs to be pointed at
	// the file that actually produced it, not at where it usually lives.
	Source string
}

// NewDnsmasq returns a backend writing to the given layout.
func NewDnsmasq(p Paths) Dnsmasq { return Dnsmasq{Paths: p, Source: core.ConfigPath} }

// WithSource names the intent file in generated headers.
func (d Dnsmasq) WithSource(path string) Dnsmasq {
	if path != "" {
		d.Source = path
	}
	return d
}

// Name identifies the backend in status output.
func (Dnsmasq) Name() string { return "dnsmasq" }

// Unit is the systemd unit that supervises it.
//
// Deliberately not the distro's dnsmasq.service. That unit and
// /etc/dnsmasq.d belong to whatever the operator installed dnsmasq for;
// taking them over would be squatting shared state (design.md §3.4). Running
// our own instance is the only way to own a DHCP server without taking the
// system's dnsmasq away from its owner.
func (Dnsmasq) Unit() string { return "olr-dhcp.service" }

// header is the ownership banner every generated file carries (design.md §7).
func (d Dnsmasq) header(what string) string {
	source := d.Source
	if source == "" {
		source = core.ConfigPath
	}
	return fmt.Sprintf(`# %s
#
# Generated by open-linux-router from
#   %s
# Do not edit: this file is rewritten on every apply and your changes will be
# lost. Use `+"`olr dhcp set`"+`, or the extra_dnsmasq_conf field for settings
# olr does not model.
`, what, source)
}

// Canonical reduces a rendered file to what dnsmasq actually reads: comments
// and blank lines removed.
//
// This is what stops a comment from being a config change. Roughly half of what
// we render is the ownership header and the explanations around each directive,
// all of it string literals in this file — so without normalisation, editing
// one of those comments in a new olr release would mark every deployed box as
// drifted and schedule a restart of its DHCP server. The file still gets
// rewritten; it just stops being a reason to signal the daemon.
//
// Normalisation belongs to the backend rather than to the planner because it is
// a statement about *this* file format. A future nftables or unbound backend
// brings its own; the planner only ever calls this.
func (Dnsmasq) Canonical(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	kept := lines[:0]
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		kept = append(kept, trimmed)
	}
	return []byte(strings.Join(kept, "\n"))
}

// reloadable reports whether a path sits in one of the directories dnsmasq
// re-reads on SIGHUP. Used by the planner to tell a reload from a restart,
// including for stray files no longer rendered by the current config.
func (d Dnsmasq) reloadable(path string) bool {
	return strings.HasPrefix(path, d.Paths.HostsDir+"/") ||
		strings.HasPrefix(path, d.Paths.OptsDir+"/")
}

// Render is pure: same config and same link facts, same bytes.
func (d Dnsmasq) Render(c Config, networks NetworkView) (Rendered, error) {
	c = c.Clone()
	c.Normalize()

	var out Rendered

	main, err := d.renderMain(c, networks)
	if err != nil {
		return Rendered{}, err
	}
	out.add(File{Path: d.Paths.Conf, Mode: 0o644, Data: main})

	for _, p := range c.Pools {
		opts, err := d.renderOptions(p)
		if err != nil {
			return Rendered{}, err
		}
		if opts == nil {
			continue
		}
		out.add(File{
			Path:       d.Paths.OptsDir + "/" + p.Network + ".conf",
			Mode:       0o644,
			Data:       opts,
			Reloadable: true,
		})
	}

	for _, r := range c.Reservations {
		out.add(File{
			Path: d.Paths.HostsDir + "/" + reservationFile(r.MAC),
			Mode: 0o644,
			Data: d.renderReservation(r),
			// dnsmasq adds new host records from hostsdir automatically, but a
			// changed or deleted one only takes effect on SIGHUP. So every
			// change here is classified as a reload rather than special-casing
			// additions: the saved signal is negligible and a delete that
			// silently did not apply would be a genuine bug.
			Reloadable: true,
		})
	}

	out.sort()
	return out, nil
}

func (d Dnsmasq) renderMain(c Config, networks NetworkView) ([]byte, error) {
	var b strings.Builder
	b.WriteString(d.header("dnsmasq configuration for the olr dhcp module"))

	b.WriteString(`
# DHCP only. DNS belongs to the dns module and unbound (design.md §4.2), and
# without port=0 this dnsmasq would silently become a second resolver on the
# box, answering queries nobody configured it to answer.
port=0

# Bind only the interfaces that have a pool, so another DHCP or DNS server on
# this machine keeps its own sockets (design.md §3.4). bind-dynamic and not
# bind-interfaces: both allow a second instance, but bind-interfaces binds only
# what exists at startup, and a router creates bridges and VLANs after boot.
bind-dynamic
except-interface=lo

`)

	fmt.Fprintf(&b, "dhcp-leasefile=%s\n", d.Paths.LeaseFile)
	fmt.Fprintf(&b, "pid-file=%s\n", d.Paths.PIDFile)

	b.WriteString(`
# Reservations and per-pool options live in directories because dnsmasq re-reads
# those on SIGHUP but never re-reads this file. That is what makes adding a
# reservation a reload rather than a restart.
`)
	fmt.Fprintf(&b, "dhcp-hostsdir=%s\n", d.Paths.HostsDir)
	fmt.Fprintf(&b, "dhcp-optsdir=%s\n", d.Paths.OptsDir)

	b.WriteString(`
# We refuse to start when anything else holds UDP/67, so dnsmasq genuinely is
# the only DHCP server here and the precondition for this directive holds.
#
# It is what makes "why did this client not get an address" stop being the
# common support question. Without it dnsmasq ignores a DHCPREQUEST for a lease
# it has no record of — a client returning after a range change, or after the
# lease database was lost — and that client waits out a timeout instead of
# being told to ask again.
dhcp-authoritative
`)

	fmt.Fprintf(&b, `
# dnsmasq's own default is 1000 leases, which a larger pool would hit silently:
# it would simply stop answering while every olr surface still reported free
# addresses. Sized from the configured pools instead, and never lowered below
# dnsmasq's default so this can only ever raise the ceiling.
dhcp-lease-max=%d
`, leaseMax(c, networks))

	if !c.Enabled {
		b.WriteString(`
# NOTE: the dhcp module is disabled. The pools below are rendered so that
# enabling is a service start rather than a re-render, but the unit is stopped.
`)
	}

	wantsRA := false
	for _, p := range c.Pools {
		info, err := networks.Network(p.Network)
		if err != nil {
			return nil, fmt.Errorf("rendering pool %q: %w", p.Network, err)
		}

		fmt.Fprintf(&b, "\n# network: %s\n", p.Network)
		for _, member := range info.Members {
			fmt.Fprintf(&b, "interface=%s\n", member)
		}

		// The dnsmasq tag is the network's name, so a tagged option reads as
		// belonging to the thing the operator named rather than to whichever
		// NIC it happens to sit on this week.
		if p.ServesIPv4() {
			start, end, ok := p.Range(info)
			if !ok {
				return nil, fmt.Errorf("rendering pool %q: no IPv4 range, and none could be derived from %s",
					p.Network, info.Subnet)
			}
			derived := ""
			if !p.IPv4.Explicit() {
				derived = "  # derived from the network's subnet"
			}
			// The netmask is optional for a directly connected network, but
			// stating it removes dnsmasq's class-based guess as a failure mode.
			fmt.Fprintf(&b, "dhcp-range=set:%s,%s,%s,%s,%s%s\n",
				p.Network, start, end, netmaskOf(info.Subnet), p.LeaseTimeOrDefault().Seconds(), derived)
		}

		if ra := p.RA(); ra != RAOff {
			wantsRA = true
			b.WriteString(raComment(ra))
			// One IPv6 range per member: `constructor:` names an interface, so a
			// network on two of them needs one line each.
			for _, member := range info.Members {
				fmt.Fprintf(&b, "dhcp-range=set:%s,%s\n", p.Network, raRange(ra, member, p))
			}
			if p.IPv6.MTU > 0 {
				// The trailing 0 is the RA interval, and zero means dnsmasq's
				// default; the syntax has no way to set the MTU alone.
				for _, member := range info.Members {
					fmt.Fprintf(&b, "ra-param=%s,mtu:%d,0\n", member, p.IPv6.MTU)
				}
			}
		}
	}

	if wantsRA {
		b.WriteString(`
# Required for any of the IPv6 ranges above to be advertised.
enable-ra
`)
	}

	if extra := strings.TrimRight(c.ExtraConf, "\n"); extra != "" {
		b.WriteString(`
# --- extra_dnsmasq_conf ---
# Passed through verbatim from the module config (design.md §3.2 rule 5). It is
# revisioned with everything else, which is the point: unusual settings stay
# visible to olr instead of being hand-edited into a file olr does not read.
`)
		b.WriteString(extra)
		b.WriteString("\n")
	}

	return []byte(b.String()), nil
}

// DefaultLeaseMax is dnsmasq's own ceiling on concurrent leases.
//
// It exists to stop a host inventing thousands of leases and exhausting the
// daemon's memory, so it is a real defence and not merely a default to
// override. leaseMax only ever raises it.
const DefaultLeaseMax = 1000

// StatefulRASize is how many addresses the stateful DHCPv6 range hands out.
// It follows from the ::100–::1ff bounds raRange writes and is stated here so
// the lease ceiling and the range cannot drift apart.
const StatefulRASize = 0x100

// leaseMax sizes dnsmasq's lease ceiling to what the config could actually hand
// out.
//
// Every address in a pool, every reservation — which may legitimately sit
// outside its pool's range and so cost an extra lease — and every stateful
// DHCPv6 range. That total is a true upper bound: dnsmasq reuses a lease record
// when an expired one is handed out again, so nothing accumulates beyond it.
func leaseMax(c Config, networks NetworkView) int {
	total := len(c.Reservations)
	for _, p := range c.Pools {
		if info, err := networks.Network(p.Network); err == nil {
			if start, end, ok := p.Range(info); ok {
				total += core.RangeSize(start, end)
			}
		}
		if p.RA() == RAStateful {
			total += StatefulRASize
		}
	}
	return max(total, DefaultLeaseMax)
}

// raRange builds the IPv6 dhcp-range for a mode.
//
// constructor: makes dnsmasq derive the prefix from the interface's current
// address, which is why prefix delegation needs no plumbing here — a WAN prefix
// that changes is followed by the daemon, not by a re-render.
func raRange(mode RAMode, member string, p Pool) string {
	switch mode {
	case RAStateful:
		// A stateful range needs real bounds. These are host identifiers; the
		// network part comes from the interface.
		return fmt.Sprintf("::100,::1ff,constructor:%s,slaac,%s",
			member, p.LeaseTimeOrDefault().Seconds())
	default:
		// ra-stateless: the O and A bits. Clients self-assign an address and
		// ask us for the rest. No range bounds are needed because we assign no
		// addresses, which is what "::" means here.
		return fmt.Sprintf("::,constructor:%s,ra-stateless", member)
	}
}

func raComment(mode RAMode) string {
	switch mode {
	case RAStateful:
		return "# IPv6: advertise the prefix and hand out DHCPv6 addresses from it.\n"
	default:
		return "# IPv6: advertise the prefix for self-assignment, answer DHCPv6\n" +
			"# information requests for DNS and NTP.\n"
	}
}

// renderOptions writes a pool's DHCP options, or nil if it has none.
//
// The file holds the text that would follow "dhcp-option=", one per line, which
// is the format dnsmasq's optsdir expects.
//
// It takes no view of the networks. It used to, and only to read the interface
// name for its tag — the tag is the network's name now, which the pool already
// carries.
func (d Dnsmasq) renderOptions(p Pool) ([]byte, error) {
	var lines []string
	tag := "tag:" + p.Network

	// A nil Gateway or DNS renders nothing on purpose. dnsmasq's own default
	// for both is the address of the machine it runs on, which is exactly what
	// "the router itself" means — so the default is expressed by saying
	// nothing, rather than by us restating an address we would then have to
	// keep in sync with the link module.
	if p.Gateway != nil {
		lines = append(lines, fmt.Sprintf("%s,option:router,%s", tag, *p.Gateway))
	}
	if v4, v6 := splitFamilies(p.DNS); len(v4) > 0 || len(v6) > 0 {
		if len(v4) > 0 {
			lines = append(lines, fmt.Sprintf("%s,option:dns-server,%s", tag, joinAddrs(v4)))
		}
		if len(v6) > 0 {
			lines = append(lines, fmt.Sprintf("%s,option6:dns-server,%s", tag, joinBracketed(v6)))
		}
	}
	if p.Domain != "" {
		lines = append(lines, fmt.Sprintf("%s,option:domain-name,%s", tag, p.Domain))
	}
	if v4, v6 := splitFamilies(p.NTP); len(v4) > 0 || len(v6) > 0 {
		if len(v4) > 0 {
			lines = append(lines, fmt.Sprintf("%s,option:ntp-server,%s", tag, joinAddrs(v4)))
		}
		if len(v6) > 0 {
			lines = append(lines, fmt.Sprintf("%s,option6:ntp-server,%s", tag, joinBracketed(v6)))
		}
	}
	for _, o := range p.Options {
		lines = append(lines, fmt.Sprintf("%s,%s,%s", tag, o.Option, o.Value))
	}

	if len(lines) == 0 {
		return nil, nil
	}

	var b strings.Builder
	b.WriteString(d.header("DHCP options for " + p.Network))
	b.WriteString("\n")
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\n")
	}
	return []byte(b.String()), nil
}

// renderReservation writes one dhcp-host line's worth of file.
//
// One file per reservation rather than one file for all of them: dnsmasq reads
// the whole directory, and per-file granularity means adding a reservation
// touches exactly one path, which keeps the diff in `olr dhcp diff` honest
// about what changed.
func (d Dnsmasq) renderReservation(r Reservation) []byte {
	fields := []string{r.MAC}
	if r.Hostname != "" {
		fields = append(fields, r.Hostname)
	}
	fields = append(fields, r.IP.String())
	if r.LeaseTime > 0 {
		fields = append(fields, r.LeaseTime.Seconds())
	}

	var b strings.Builder
	b.WriteString(d.header("DHCP reservation for " + r.MAC))
	b.WriteString("\n")
	b.WriteString(strings.Join(fields, ","))
	b.WriteString("\n")
	return []byte(b.String())
}

// reservationFile turns a MAC into a stable filename. dnsmasq reads a directory
// in unspecified order, so the name only has to be unique and predictable.
func reservationFile(mac string) string {
	return strings.ReplaceAll(mac, ":", "") + ".conf"
}

// netmaskOf renders a prefix length as a dotted-quad mask.
func netmaskOf(p netip.Prefix) string {
	bits := p.Bits()
	if !p.Addr().Is4() || bits < 0 {
		return ""
	}
	var mask [4]byte
	for i := 0; i < 4; i++ {
		switch {
		case bits >= 8:
			mask[i] = 0xff
			bits -= 8
		case bits > 0:
			mask[i] = ^byte(0) << (8 - bits)
			bits = 0
		}
	}
	return netip.AddrFrom4(mask).String()
}

func splitFamilies(addrs []netip.Addr) (v4, v6 []netip.Addr) {
	for _, a := range addrs {
		if a.Is4() {
			v4 = append(v4, a)
		} else if a.IsValid() {
			v6 = append(v6, a)
		}
	}
	return v4, v6
}

func joinAddrs(addrs []netip.Addr) string {
	parts := make([]string, len(addrs))
	for i, a := range addrs {
		parts[i] = a.String()
	}
	return strings.Join(parts, ",")
}

// joinBracketed formats IPv6 addresses the way dnsmasq requires in an option
// value: square-bracketed.
func joinBracketed(addrs []netip.Addr) string {
	parts := make([]string, len(addrs))
	for i, a := range addrs {
		parts[i] = "[" + a.String() + "]"
	}
	return strings.Join(parts, ",")
}
