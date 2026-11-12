package clients

import (
	"fmt"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The API's own shapes, for the reason internal/dhcp/view.go gives: what the
// HTTP API returns should be a deliberate decision rather than a side effect of
// which fields happened to be exported.

// deviceView is one row of the client list.
type deviceView struct {
	MAC string `json:"mac"`

	// Name is already resolved — stored name, else observed hostname, else the
	// name the client last announced while it was here, else empty. NameOrigin
	// says which, because "we call it this", "it calls itself this" and "it
	// called itself this last week" are three different claims.
	Name       string `json:"name"`
	NameOrigin Origin `json:"name_origin,omitempty"`

	// NameAsOf is when a remembered name was heard, present only for
	// name_origin "remembered". A surface that shows the name without it would
	// be reporting last week's claim as this minute's.
	NameAsOf *time.Time `json:"name_as_of,omitempty"`

	Category       Category `json:"category"`
	CategoryOrigin Origin   `json:"category_origin,omitempty"`

	// DetectedCategory and DetectReason are what inference produced regardless
	// of whether it won, so a UI can offer the guess for confirmation on an
	// untouched device and can show an override *as* an override.
	DetectedCategory Category `json:"detected_category,omitempty"`
	DetectReason     string   `json:"detect_reason,omitempty"`
	Vendor           string   `json:"vendor,omitempty"`

	// VendorKey is the icon slug for Vendor, and is sent beside the name rather
	// than instead of it: the name is what a person reads, the key is what
	// selects a picture, and only a few dozen vendors have one. Absent for the
	// rest, which a client renders by falling through to the category — see
	// deviceIcon() in the SPA's features/clients/icons.ts.
	VendorKey VendorKey `json:"vendor_key,omitempty"`

	Model string `json:"model,omitempty"`

	// Icon is the operator's chosen picture, absent when the picture follows
	// category and vendor. See Client.Icon.
	Icon Icon `json:"icon,omitempty"`

	Notes string `json:"notes,omitempty"`

	// Group is the client's group, absent for none.
	Group string `json:"group,omitempty"`

	// Stored reports whether a human has described this client. False means it
	// is listed purely because it was seen, which is what the UI uses to offer
	// "name this client".
	Stored bool `json:"stored"`

	// Online is presence, computed against the response's as_of so a client
	// never has to guess which clock to compare against.
	Online bool `json:"online"`

	// Seen is false for a stored device that no source has observed — the
	// powered-off printer. Distinguished from Online because "we have never
	// seen this" and "we saw this and it is away" are different facts, and
	// collapsing them would make a typo'd MAC look like an offline device.
	Seen bool `json:"seen"`

	IPs      []string `json:"ips,omitempty"`
	Hostname string   `json:"hostname,omitempty"`
	Sources  []Source `json:"sources,omitempty"`

	// Expires is null for a client with no lease, and for a lease that never
	// expires. A pointer rather than a zero time, for the reason leaseView
	// gives in internal/dhcp/view.go.
	Expires *time.Time `json:"expires"`

	// FixedIP is the reserved address. Owned by dhcp; joined here so that no
	// client has to reimplement the join (§11.1).
	FixedIP string `json:"fixed_ip,omitempty"`

	// Network is which of this router's networks the client is on, and
	// NetworkOrigin is how we know — "observed" when the neighbour table said
	// so, "detected" when it was placed by which range its address falls in.
	//
	// Empty means it could not be placed, and a client has to render that as its
	// own answer: a client with a hand-set address outside every range is
	// somewhere on the network, and filing it under a plausible guess would be
	// worse than saying we do not know.
	Network       string `json:"network,omitempty"`
	NetworkOrigin Origin `json:"network_origin,omitempty"`

	// LastSeen is when the router last heard from the client: the neighbour
	// table confirming it answers, which any traffic through the router keeps
	// doing. Absent when it has not been heard since the record began. Unlike
	// Online it moves only on evidence, so a client that left an hour ago
	// reads as an hour ago even while a stale entry still lists it.
	LastSeen *time.Time `json:"last_seen,omitempty"`
}

func viewDevice(r Resolved) deviceView {
	v := deviceView{
		MAC:              r.MAC,
		Name:             r.Name,
		NameOrigin:       r.NameOrigin,
		NameAsOf:         r.NameAsOf,
		Category:         r.Category,
		CategoryOrigin:   r.CategoryOrigin,
		DetectedCategory: r.Detected.Category,
		DetectReason:     r.Detected.Reason,
		Vendor:           r.Detected.Vendor,
		VendorKey:        r.Detected.VendorKey,
		Model:            r.Model,
		Icon:             r.Icon,
		Notes:            r.Notes,
		Group:            r.Group,
		Stored:           r.Stored,
		Online:           r.Online(),
		Seen:             r.Presence != nil,
		FixedIP:          r.FixedIP,
		Network:          r.Network,
		NetworkOrigin:    r.NetworkOrigin,
		LastSeen:         r.LastSeen,
	}
	if r.Presence != nil {
		v.IPs = r.Presence.IPs
		v.Hostname = r.Presence.Hostname
		v.Sources = r.Presence.Sources
		v.Expires = r.Presence.Expires
	}
	return v
}

type listResponse struct {
	Clients []deviceView `json:"clients"`

	// Problems are the things that went wrong without taking the answer down:
	// an unreadable ARP table, a garbled lease line. Reported rather than
	// dropped so a partial list is visible as partial.
	Problems []core.Problem `json:"problems,omitempty"`

	// AsOf stamps the whole reply, because every observed object carries its
	// freshness (§4.5).
	AsOf time.Time `json:"as_of"`
}

// locationResponse is what the equipment said about one client.
type locationResponse struct {
	MAC       string         `json:"mac"`
	Locations []locationView `json:"locations"`

	// Problems are the devices that could not answer: no forwarding table, or
	// no reply. An empty Locations with no Problems means every device answered
	// and none of them has the client.
	Problems []core.Problem `json:"problems,omitempty"`

	AsOf time.Time `json:"as_of"`
}

// locationView is one device's answer, kept verbatim for the reason
// Location.Port gives: "5" and "lan2" are what the labels on the hardware say.
type locationView struct {
	DeviceID   string    `json:"device_id"`
	Device     string    `json:"device,omitempty"`
	Port       string    `json:"port"`
	VLAN       int       `json:"vlan,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
}

func viewLocation(l Location) locationView {
	return locationView{
		DeviceID:   l.DeviceID,
		Device:     l.Device,
		Port:       l.Port,
		VLAN:       l.VLAN,
		ObservedAt: l.ObservedAt,
	}
}

// --- plan ------------------------------------------------------------------

// planView mirrors internal/dhcp's plan shape on purpose.
//
// The field names are identical so that a client's Plan type and its plan
// preview render either module's answer without a second implementation. The
// values are what they honestly are for a module with no backend: no service
// action, and an impact of none — storing a name cannot drop a client.
type planView struct {
	Backend  string         `json:"backend"`
	Changes  []changeView   `json:"changes"`
	Action   string         `json:"action"`
	Impact   string         `json:"impact"`
	Empty    bool           `json:"empty"`
	Warnings []core.Problem `json:"warnings,omitempty"`
}

type changeView struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Impact string `json:"impact"`
	Diff   string `json:"diff"`
}

const (
	impactNone = "none"
	actionNone = "none"

	kindCreate = "create"
	kindUpdate = "update"
	kindDelete = "delete"
)

// buildPlan diffs stored intent against a proposal.
//
// A plan for this module is not about the system — nothing on the box changes —
// it is about the document. It exists so `olr --dry-run` and an agent proposing
// a change for a human to review (§6.4) work here the same way they work
// everywhere else, and so the UI can skip a write that would do nothing.
func buildPlan(stored, desired Config) planView {
	stored.Normalize()
	desired.Normalize()

	res := Validate(desired)
	plan := planView{
		Backend:  "",
		Changes:  []changeView{},
		Action:   actionNone,
		Impact:   impactNone,
		Warnings: problems(res.Warnings),
	}

	before := map[string]Client{}
	for _, d := range stored.Clients {
		before[d.MAC] = d
	}
	after := map[string]Client{}
	for _, d := range desired.Clients {
		after[d.MAC] = d
	}

	// Iterated over the sorted desired list, then the sorted stored list, so
	// the change order is deterministic rather than map-order.
	for _, d := range desired.Clients {
		old, existed := before[d.MAC]
		switch {
		case !existed:
			plan.Changes = append(plan.Changes, changeView{
				Path:   path(d.MAC),
				Kind:   kindCreate,
				Impact: impactNone,
				Diff:   describe(d, "+"),
			})
		case old != d:
			plan.Changes = append(plan.Changes, changeView{
				Path:   path(d.MAC),
				Kind:   kindUpdate,
				Impact: impactNone,
				Diff:   describe(old, "-") + describe(d, "+"),
			})
		}
	}
	for _, d := range stored.Clients {
		if _, kept := after[d.MAC]; !kept {
			plan.Changes = append(plan.Changes, changeView{
				Path:   path(d.MAC),
				Kind:   kindDelete,
				Impact: impactNone,
				Diff:   describe(d, "-"),
			})
		}
	}

	// A group is keyed by name, so a rename reads as one removed and one
	// added, with its members' updates listed above; a move is an update.
	for _, g := range desired.Groups {
		old, ok := stored.FindGroup(g.Name)
		switch {
		case !ok:
			plan.Changes = append(plan.Changes, changeView{
				Path: groupPath(g.Name), Kind: kindCreate, Impact: impactNone,
				Diff: describeGroup(g, "+"),
			})
		case old.Parent != g.Parent:
			plan.Changes = append(plan.Changes, changeView{
				Path: groupPath(g.Name), Kind: kindUpdate, Impact: impactNone,
				Diff: describeGroup(old, "-") + describeGroup(g, "+"),
			})
		}
	}
	for _, g := range stored.Groups {
		if _, ok := desired.FindGroup(g.Name); !ok {
			plan.Changes = append(plan.Changes, changeView{
				Path: groupPath(g.Name), Kind: kindDelete, Impact: impactNone,
				Diff: describeGroup(g, "-"),
			})
		}
	}

	plan.Empty = len(plan.Changes) == 0
	return plan
}

func groupPath(name string) string { return fmt.Sprintf("groups[%s]", name) }

func describeGroup(g Group, prefix string) string {
	out := fmt.Sprintf("%s group: %s\n", prefix, g.Name)
	if g.Parent != "" {
		out += fmt.Sprintf("%s parent: %s\n", prefix, g.Parent)
	}
	return out
}

func path(mac string) string { return fmt.Sprintf("clients[%s]", mac) }

// describe renders a client as prefixed lines, omitting fields that are unset
// so a diff shows what was said rather than a form full of blanks.
func describe(d Client, prefix string) string {
	var b strings.Builder
	line := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "%s %s: %s\n", prefix, k, v)
		}
	}
	line("mac", d.MAC)
	line("name", d.Name)
	line("category", string(d.Category))
	line("model", d.Model)
	line("icon", string(d.Icon))
	line("notes", d.Notes)
	line("group", d.Group)
	return b.String()
}
