package clients

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ARP reads the kernel's IPv4 neighbour table.
//
// This is the answer to design.md §10 decision 7. A lease-derived list silently
// omits a statically-addressed printer, and operators notice — so either the
// neighbour table is read, or the screen has to be called "DHCP clients" and
// admit it is not an inventory. Reading it is the better trade, and it is what
// lets the screen honestly be called Clients.
//
// /proc/net/arp rather than netlink, deliberately. go.mod has three direct
// dependencies and that restraint is a stated project value; pulling in a
// netlink library to read a table the kernel already formats as text would be
// the wrong first reach. The cost is that this is IPv4 only — /proc/net/arp has
// no IPv6 half, and ND needs netlink, which is `link`'s problem to solve in
// milestone 1. Because both live behind PresenceSource, ND lands later as a
// third implementation and nothing else moves.
type ARP struct {
	// Path is the table to read. Empty means ARPPath. Injectable so the parser
	// is testable against fixtures — including malformed ones — on a machine
	// with no /proc at all.
	Path string
}

// ARPPath is the kernel's IPv4 neighbour table.
const ARPPath = "/proc/net/arp"

// atfCom is ATF_COM, the flag marking a neighbour entry as complete: the kernel
// has a hardware address for it and considers it resolved. An incomplete entry
// is an unanswered request, which is evidence of an address being *looked for*,
// not of a client being present.
const atfCom = 0x02

// incompleteMAC is what the kernel prints for an unresolved entry.
const incompleteMAC = "00:00:00:00:00:00"

// Name identifies the source in problem messages.
func (a ARP) Name() Source { return SourceARP }

func (a ARP) path() string {
	if a.Path != "" {
		return a.Path
	}
	return ARPPath
}

// Presence reads and parses the table.
//
// A missing file is reported as a problem rather than an error, and says what
// the consequence is. That case is every non-Linux developer box, and it is the
// one situation where silence would be actively misleading: the list would look
// complete while quietly being lease-only.
func (a ARP) Presence(_ context.Context) ([]Sighting, []Problem, error) {
	path := a.path()

	f, err := os.Open(path)
	switch {
	case os.IsNotExist(err):
		return nil, []Problem{{
			Path: string(SourceARP),
			Message: fmt.Sprintf(
				"%s is not available on this system, so devices that never request "+
					"a DHCP lease will not appear until they are added by hand", path),
		}}, nil
	case err != nil:
		return nil, nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	var (
		out      []Sighting
		problems []Problem
	)

	scanner := bufio.NewScanner(f)
	for line := 0; scanner.Scan(); line++ {
		text := scanner.Text()

		// The first line is a header, and it is a header rather than data on
		// every kernel that has ever shipped this file.
		if line == 0 {
			continue
		}
		if strings.TrimSpace(text) == "" {
			continue
		}

		fields := strings.Fields(text)
		// IP, HW type, Flags, HW address, Mask, Client.
		if len(fields) < 6 {
			problems = append(problems, Problem{
				Path:    string(SourceARP),
				Message: fmt.Sprintf("%s line %d has %d fields, want 6", path, line+1, len(fields)),
			})
			continue
		}

		// Field 5 is the interface the neighbour was seen on.
		//
		// This was deliberately dropped until now, on the grounds that §4.1
		// makes the network the operator-facing object and "enp3s0" exactly the
		// implementation detail a network exists to hide. It is carried again
		// because a map of which network a client is on has no other way to
		// place a statically-addressed one: an address can be matched against a
		// pool's range, but a reservation and a hand-set address both sit
		// outside it — which is precisely the printer this source exists for.
		//
		// The name still does not reach an operator raw. It is a key the UI
		// joins against networks it already names, and it goes back to being an
		// implementation detail as soon as `link` can name them itself.
		ip, rawFlags, mac, iface := fields[0], fields[2], fields[3], fields[5]

		// An unresolved entry names no hardware, so there is no device to
		// report. Skipped silently: it is a normal transient state, not a fault.
		if mac == incompleteMAC {
			continue
		}

		flags, err := strconv.ParseUint(strings.TrimPrefix(rawFlags, "0x"), 16, 64)
		if err != nil {
			problems = append(problems, Problem{
				Path:    string(SourceARP),
				Message: fmt.Sprintf("%s line %d has unreadable flags %q", path, line+1, rawFlags),
			})
			continue
		}

		out = append(out, Sighting{
			MAC:       mac,
			IP:        ip,
			Interface: iface,
			Source:    SourceARP,
			Active:    flags&atfCom != 0,
			// No hostname: ARP carries none. Leaving it empty rather than
			// substituting something keeps Merge's "a lease heard the client's
			// own name" rule meaningful.
			Hostname: "",
		})
	}
	if err := scanner.Err(); err != nil {
		return out, problems, fmt.Errorf("reading %s: %w", path, err)
	}

	return out, problems, nil
}
