package clients

import (
	"context"
	"fmt"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Applier is the read and write path onto stored identity.
//
// It is much thinner than internal/dhcp's, and the reason is worth stating: this
// module has no backend. There is nothing to render, no daemon to reload and no
// port to check, because a client's name and category are facts about how the
// network is described rather than how it is configured. Applying is therefore
// exactly one step — store the document — and the impact is always none.
//
// That is what makes the client list instant-apply under §5.1 with no
// confirmation: renaming a laptop cannot drop a client. The one action on the
// screen that *can* — giving a client a fixed address — belongs to `dhcp` and
// goes through `dhcp`'s plan and its impact gate, which is the correct place
// for it.
type Applier struct {
	// Store is core's configuration document, which owns this module's intent
	// alongside every other module's.
	Store *core.Store

	// BeforeSave lets the composition root preserve references to the old
	// inventory in the same atomic document write as an identity change.
	BeforeSave func(*core.Document) error

	// Presence is where observed sightings come from. Empty is legal and means
	// the list shows stored identity only.
	Presence []PresenceSource

	// Fixed is the window onto whoever owns fixed addresses. Nil means the list
	// omits them rather than claiming there are none.
	Fixed FixedAddressView

	// Networks is the window onto whoever owns the address ranges. Nil means a
	// device is placed only when a source saw which interface it was on, which
	// is a smaller answer rather than a wrong one.
	Networks NetworkView

	// Seen is what the router remembers about devices that are not here: when
	// each was last heard from, and the name it last announced. Nil leaves both
	// out rather than claiming the client was never heard or never named.
	Seen *Seen
}

// Load reads stored intent out of the configuration document.
func (a Applier) Load() (Config, error) {
	doc, err := a.Store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(doc)
}

// Save validates and stores intent, returning the stored form.
//
// Read-modify-write on the shared document, so a save here cannot drop another
// module's configuration. It is safe without further locking because every
// config write in the process holds the one global apply lock (§3.6) — the
// caller takes it.
func (a Applier) Save(cfg Config) (Config, error) {
	cfg.Normalize()
	if res := Validate(cfg); !res.OK() {
		return cfg, res.Err()
	}

	doc, err := a.Store.Load()
	if err != nil {
		return cfg, err
	}
	data, err := MarshalConfig(cfg)
	if err != nil {
		return cfg, err
	}
	if a.BeforeSave != nil {
		if err := a.BeforeSave(&doc); err != nil {
			return cfg, err
		}
	}
	doc.Set(ModuleName, data)
	if err := a.Store.Save(doc); err != nil {
		return cfg, fmt.Errorf("storing configuration in %s: %w", a.Store.Path(), err)
	}
	return cfg, nil
}

// List reads everything and joins it.
//
// Failures degrade rather than propagate: a presence source that cannot be read
// contributes a problem and no sightings, and an unreadable fixed-address view
// does the same. Only stored intent failing is fatal, because without it there
// is no list to speak of. This mirrors getStatus in internal/dhcp/http.go, where
// each half of the answer is reported independently and neither can suppress
// the other.
func (a Applier) List(ctx context.Context) ([]Resolved, []Problem, error) {
	cfg, err := a.Load()
	if err != nil {
		return nil, nil, err
	}

	sightings, problems := Gather(ctx, a.Presence...)

	fixed := map[string]string{}
	if a.Fixed != nil {
		got, err := a.Fixed.FixedAddresses(ctx)
		if err != nil {
			problems = append(problems, Problem{
				Message: fmt.Sprintf("could not read fixed addresses: %v", err),
			})
		} else {
			fixed = got
		}
	}

	var networks []Network
	if a.Networks != nil {
		got, err := a.Networks.Networks(ctx)
		if err != nil {
			problems = append(problems, Problem{
				Message: fmt.Sprintf("could not read the address ranges: %v", err),
			})
		} else {
			networks = got
		}
	}

	// A nil *Seen is a nil History here: nothing is remembered, and every device
	// answers from what a source can say now.
	var history History
	if a.Seen != nil {
		history = a.Seen
	}
	list, joinProblems := Build(cfg, sightings, fixed, networks, history)
	return list, append(problems, joinProblems...), nil
}
