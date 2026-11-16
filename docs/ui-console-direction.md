# Console direction: the operator's network, not our modules

Status: **adopted**. This is the standing direction for the console, not a
proposal to revisit. §2 to §4 are the rules to design against; the rest is how they
are applied, and §9 is the order the work is done in. Read it with the
[visual guide](ui-style-guide.md), the [content guide](ui-content-guide.md) and
the [semantic component contracts](ui-semantic-components.md). The
[interactive mockup](ui-console-mockup.html) is the reference implementation of
the visuals; stills are in [`ui-console-shots/`](ui-console-shots/).

## 1. Why the console is changing

The UI is currently a mirror of §4 of `design.md`. Gateway holds the interfaces,
the subnets and DNS; Wireless holds one access point; Devices holds an inventory
that is only configuration; Access and Advanced hold everything else. That is a
faithful map of the daemon and a poor map of a house.

A person does not arrive with "the DHCP scope". They arrive with "my wifi is
slow", "what is this box", "is anything broken". Answering those today means
walking three pages and assembling the box yourself: the same switch is a row
under Devices, a card under Wireless and a member of a subnet under Gateway. No
single page can answer "is this switch healthy", because no single page knows
that a switch is a thing.

Nothing in that is a bug in a page. It is the shape, and the shape is the thing
this document changes.

**These are not new ideas.** Every console that manages a fleet of appliances
converges on roughly this shape, for the same reason: a network is a set of
objects, and an operator moves between objects, not between features. We are
adopting it because it survives a heterogeneous network — three vendors, four
device families, one browser tab — not because it is novel.

The surfaces such a console needs, and the question each one answers:

| Surface | The question it answers |
| --- | --- |
| Shell | Which router am I looking at, and is it alive? |
| Overview | Is anything wrong right now? |
| Topology | What is wired to what, and how do I know? |
| Devices | What do I manage, and which one needs me? |
| Device page | Is this box healthy, and what is it doing? |
| Port view | Which port is that, and what is plugged into it? |
| Client list | Who is on the network? |

## 2. The standing rule

**The console is organised around what the operator is looking at, not around how
OLR is built.**

Our modules are a good shape for the daemon — `link`, `dhcp`, `dns`, `firewall`,
`wireless`, `devices` — and a bad shape for a person. A module-first UI answers
questions in the order the code is written, which is how one switch ends up in
three different places.

Three tests for any screen:

1. **Would a reader need to know our package layout to find this?** If the answer
   is yes, the page is organised for us.
2. **Does every object have one home?** A box, a client, a cable, a subnet, a
   rule. Settings live with the object they change, not in a settings drawer.
3. **Is anything on this page only because a module needed somewhere to put it?**
   Remove it, or give it a home in the object it belongs to.

The consequence is a two-layer console: **Network** (the objects) and
**Configuration** (the knobs). The second layer is not a second product and does
not get to grow into one — it is where a feature that only makes sense for one
vendor lives. Getting the first layer right is most of this document; the second
is §6.1 and then stops.

## 3. Five principles

**1. Navigation by object, not by module.** The primary axis is *what kind of
thing is this* — device, client, network — not *which feature configures it*.
That is the only structure that scales from one router to a rack: a switch and an
access point differ in their body, not in their place in the product. It prevents
the failure we have now, where the console is a table of contents for `internal/`.

**2. One page frame, a body per device type.** Every box opens the same header,
the same status, the same stat strip, the same Details tab. What differs is the
body: a switch answers "which VLAN is this port in", an ONT answers "what is my
optical budget". `web/src/features/devices/device-page.tsx` already records why
the bodies stay separate — a shared tab strip carried the union of all three
device families and hid the impossible ones behind `unsupported` capabilities,
which is a type distinction wearing a capability list's clothes. Keep the bodies;
unify the frame.

**3. The front panel is the device.** Nine numbered squares with a link LED, a
speed and a PoE mark say more about a switch in one glance than any table. Every
port is a pointer target; selecting one inspects it in place instead of
navigating away. A port is the smallest thing an operator has a question about,
and a table makes them find it twice: once as a row, once in their head.

**4. Topology is a destination, with its evidence.** Today the overview ends in a
vertical tree of device groups, which answers "what have I grouped", not "what is
wired to what". A page of its own can carry edge evidence, speeds and the
unknowns — and it is the only screen that can show a network with holes in it
without pretending the holes are not there.

**5. Density with one accent.** A 13px base, hairline dividers, one interaction
colour and a separate semantic set for state. Density is not the enemy of calm;
inconsistent meaning is. The console should feel like a console at 9am on a
Monday, which means it has to say a lot without raising its voice.

## 4. Five failure modes we refuse

**Vendor-shaped identity.** The tempting shortcut is a table of models, each with
its own ports, radios and firmware quirks baked into the UI. We cannot do that: a
Mercury SE109 Pro, an OpenWrt AP and a Huawei ONT answer different questions
through different adapters, and the next box is one we have never seen. Identity
stays `manufacturer + model + adapter`, and capability stays an observation with
`supported / unsupported / unknown`, never an inference from a name.

**Filling in blanks.** The tempting shortcut is to render the last known value,
the derived value and the measured value the same way, because the screen looks
finished. Ours must not: missing means unknown, a snapshot is a dated
observation, and a failed read is not an offline device. A dash with a reason
beats a plausible zero, every time.

**One dot for everything.** A single green dot collapses four different facts we
can tell apart, and the operator cannot act on any of them:

| What happened | Word we already have | Tone |
| --- | --- | --- |
| We read it and it is working | `Connected` | ok |
| The device is up but did not answer us | `Not answering` | warn |
| The link is down at both ends | `Down` | fault |
| We have never read this section | `Unknown` | unknown (ring) |

The fourth is the one that matters: an unread section is not an empty one.
`web/src/lib/state.ts` owns this vocabulary and its tones; the console work is
mostly *spending* it.

**The settings labyrinth.** Every setting that is not obviously at home drifts
into a global Settings tree, and a year later nobody can find it. Our module pages
put a setting next to the module that owns it, one level down, with its
explanation. Keep that.

**Rented identity.** The fastest way to look finished is to paint the interface in
someone else's palette, or in ours at full strength. Both are a costume: the
first makes a router look like a product it is not, the second makes every
surface shout the brand and nothing shout the state. §5 is the whole of our
answer, and it is deliberately small.

## 5. Two colours, one moment

The mark carries OLR's two colours: **`#13a7f5`** and **`#fd605e`**. They are
identity, not a theme.

- **They are not tokens with roles.** There is no `--brand` on a button, no
  `--brand` on a selected row, no `--brand` for a chart series. The interaction
  colour stays `--primary` (`#1d4ed8` / `#8ab4ff`), which is chosen for contrast;
  the brand pair never has to be readable as text, so it can stay at full
  saturation.
- **They appear in exactly three places.** The mark itself; a 34 × 2px gradient
  rule under the wordmark in the rail; the same rule on the setup and sign-in
  card. That is one instance per surface, and no surface gets a second.
- **They are identical in both themes.** Like the mark, they do not have a dark
  variant. A 2px rule is not text and does not need a contrast pair.
- **Never**: a status tone, a data series, a chart, a filled button, a body text
  colour, an icon tint, a focus ring, a selected state, a badge, a hover. If a
  colour is carrying information, it is not allowed to be brand.

The reason is the same reason the status vocabulary is closed: **colour here
means something**. `#fd605e` is a hair away from the destructive tone, and a
product that sprays its logo across controls reads as a skin rather than a
console. One gradient hairline says "this is OLR" and then gets out of the way,
which is what the operator asked for.

```css
--brand-a: #13a7f5;  /* mark only */
--brand-b: #fd605e;  /* mark only */
/* the one legal drawing of them */
background: linear-gradient(90deg, var(--brand-a), var(--brand-b));
```

## 6. The design

### 6.1 Two layers, one rail

```
Network        Overview · Topology · Devices
Configuration  Gateway · Wireless · Access · Advanced
```

Operations pages are read-mostly and share one shell across every box.
Configuration pages are what exists today, unchanged in URL and behaviour.

A left rail is a consequence of that split, not a fashion: seven destinations in
two named groups do not fit a 64px top bar without becoming the "cramped
six-section navigation" the visual guide already recorded as a finding. The rail
also gives the console a place to say which router you are looking at and whether
the connection to it is live — two facts a top bar cannot hold.

Phones keep the bottom tab bar, reduced to the operations group plus an overflow
for configuration.

### 6.2 One frame, three bodies

```
← Devices
[portrait]  Mercury SE109 Pro            [Connected]   [Read again]
            switch · 192.168.1.2 · mercury adapter · read 14 s ago

9 ports · 6 up      PoE 14.3 / 60 W      184 ↓ / 96 ↑ Mbps      31d 2h
─────────────────────────────────────────────────────────────────────
Ports   VLANs   MAC lookup   Details
```

- **The frame is shared** and is what §3.2 asks for: portrait, name, roles,
  status, the stat strip, one read action, and a Details tab that always exists.
- **The body belongs to the device type.** `switch-page`, `ont-page` and
  `openwrt-page` keep their own tabs and vocabulary. This is the existing
  decision in `device-page.tsx`, and it is right.
- **Capability decides inside a body, never which body.** A collection that
  cannot be read renders its own state — `Unknown` with a reason, or
  `Unsupported by the mercury adapter` — rather than a tab that vanishes. A tab
  that disappears silently teaches the operator nothing; a section that says
  "this model does not report PoE" teaches them the model.
- **Contact is explicit.** "Read again" is the only way a device page talks to
  hardware, because a read is an on-demand HTTP conversation on a serialized
  lane, not a background poll. The timestamp of the last read belongs in the
  frame, not in a footer disclaimer.
- The stat strip is four facts, not a card grid. A device page is not a
  dashboard.

### 6.3 The port tile

A tile may carry: its number, link state, speed, medium (`copper`, `fiber`,
`lag`) and PoE. Nothing else. Specifically:

- **No colour for VLAN.** A port has one PVID and may carry many tagged VLANs; a
  single hue would have to lie about one of them. VLANs get a column and a tab.
- **Down is one word, because the snapshot has one bit.** An adapter reports
  whether the link is up, not whether there is a cable in the socket. A switch
  port therefore says `Down`; `No cable` belongs to a local interface, where the
  kernel knows the carrier and the operator can act on the difference.
- **PoE speaks only when the read did.** A tile draws a PoE mark when the snapshot
  carried one for that port. When the read carried PoE for no port at all — the
  common case, since the Mercury adapter does not implement it — the panel says so
  once beneath the tiles. Nine identical "unknown" bolts would turn one sentence
  about the adapter into nine glyphs about the ports.
- **Unknown still stays a ring.** Where the read reported PoE for some ports and
  not others, the ones it skipped show the ring rather than a silent zero.
- **Selecting is not navigating.** The inspector opens beside the panel; the
  panel keeps its layout, so the operator's eye does not move.
- **The table stays.** Nine tiles at 44px do not fit a phone, and a 24-port switch
  does not fit any phone. The tile panel is the desktop reading; the table below
  it is the same data and the honest fallback.

### 6.4 Topology evidence

Every edge says how we know it:

| Line | Evidence |
| --- | --- |
| Solid | Reported by both ends (LLDP neighbours) |
| Dashed | Inferred from a forwarding table or an address pair |
| Dotted | Wireless association |
| Ring node | Reachable, but no adapter can describe it |

Manual links drawn by an operator are a separate style and are never merged with
evidence. This follows [the equipment contract](network-equipment.md) — "it does
not fabricate inferred direct cables". It is the one place where the console
deliberately draws less than it could: a confident picture is easy to fake and
worth nothing when the cable is the thing that is broken.

### 6.5 Devices, including this one

The router running OLR is a device too. It appears in the same list with the same
row shape, marked as local, because the operator's question — "is everything here
accounted for" — does not have a special case for the box under the desk.

Clients do not get a section. The current reasoning stands: the client list is a
body, and a statically-addressed printer that has never held a lease does not
belong on a page named for DHCP. It gets a tab on the device it hangs off and a
row in Overview.

### 6.6 Status on a list nobody has read

The inventory is configuration; it never contacts equipment. So on a fresh load
the device list knows name, roles, address and adapter, and **nothing about
whether any of them is answering**. The list must say that rather than imply it:

- Every device starts at `Not checked` (unknown, ring) and moves to a real state
  only after this session reads it, either by opening it or by "Read all".
- The list says **once**, under the rows, that read times belong to this session;
  each row then carries only how long ago. The scope is a property of the column,
  not of every cell — four repetitions of "in this session" bury the one thing
  that changes. The daemon keeps no snapshot history, so a reload starts over.
- If we later cache reads in `olrd`, the wording changes to a dated timestamp and
  this paragraph changes with it.

A console that polls in the background gets to show a state column for free; one
that reads on demand has to earn it. This is the cost of the read-on-demand
promise, and it is the easiest thing in this document to get wrong by being
helpful.

## 7. The artifact

[`ui-console-mockup.html`](ui-console-mockup.html) is one self-contained file
(no build step, no network): four screens — Overview, Devices, a device page, and
Topology — a light/dark switch, working tabs, and a port that can be selected to
move the inspector. It reuses the real mark, the real device portraits from
`web/src/assets/device-icons`, and the tokens in `web/src/index.css`. Phone and
tablet layouts are in the same file.

## 8. What this would touch

| Piece | Change |
| --- | --- |
| `web/src/index.css` | `--brand-a`, `--brand-b`; no role mapping |
| `components/layout/app-shell.tsx` | Top bar → rail on `lg`; keep the bottom bar |
| `components/layout/sections.ts` | Two groups; add Topology; every existing URL kept |
| `components/layout/logo.tsx` | Unchanged; the hairline lives beside it in the rail |
| `features/devices/devices-page.tsx` | Portraits, roles, adapter, per-session read state |
| `features/devices/device-page.tsx` | The shared frame: portrait, stat strip, read action |
| `features/devices/switch-page.tsx` | Front panel above the existing `PortsTable` |
| new `features/devices/port-panel.tsx` | Tiles, selection, inspector |
| `routes/overview.tsx` → new `routes/topology.tsx` | The device tree leaves Overview for a page of its own; the queries they share move into one hook |
| `features/topology/links.tsx` | Edge style from the evidence class |
| `components/ui/status.tsx` | No new tones; `deviceHealth()` beside `linkHealth()` |

Nothing in the backend moves for slices 0–4. The capability matrix, port rows and
per-section status already exist in `GET /api/devices/{id}/snapshot` and
`/capabilities`.

## 9. Implementation plan

Six slices, each independently shippable. Nothing here writes to hardware.
**Shipped: 0 (the rail), 1 (the object index) and 3 (the port panel). Open: 2,
4, 5 and 6.**

**Slice 0 — Tokens and shell.** *(small; no behaviour change)*
Add the two brand values to `index.css` and the one legal drawing to the rail
wordmark and the auth card. Move `AppShell` from a top bar to a rail at `lg`, keep
the bottom bar, and put `sections.ts` in the two groups. The Network group ships
Overview and Devices; Topology joins it in slice 4, when there is a page to open.
*Done when:* every existing page renders inside the rail, no URL changes, the
mobile bar is unchanged, and `npm run test` (control-language) is green.

**Slice 1 — Devices is the object index.** *(medium)*
Rows gain the portrait, roles as tags, the adapter, and the §6.6 status model.
Add `deviceHealth()` to `lib/state.ts` beside `linkHealth()`; add `Read all` with
a sequential queue (the service serializes reads anyway).
*Done when:* 0, 1 and 5 devices all read correctly; a device that has never been
read shows `Not checked`, not `Connected`; a failed read shows `Not answering`,
never `Unreachable`.

**Slice 2 — The shared device frame.** *(medium)*
`device-page.tsx` renders header + stat strip + read action, and hands the body to
the existing type pages. Move the Details identity and capability matrix out of
the per-type pages into one shared tab.
*Done when:* a device whose snapshot is entirely `unsupported` still has a
coherent page: identity from the inventory, one line saying nothing could be read,
and the capability matrix explaining why.

**Slice 3 — Ports: panel and inspector.** *(large; the visual payoff)*
`port-panel.tsx` with the tile contract from §6.3, selection state, and the
inspector. `switch-page.tsx` puts it above the table on `lg` and hides it below.
PoE, speed, medium and link come from the existing port rows; nothing is derived
that the adapter did not report.
*Done when:* a 9-port and a 24-port switch both lay out without horizontal
scrolling; a port with `link: unknown` renders the ring; keyboard selection works
and the inspector is announced.

**Slice 4 — Topology with evidence.** *(medium)*
`/map` is the world-traffic map and stays where it is; the device tree has no URL
of its own — it is a component inside Overview. Give it one: lift its query
plumbing into a hook both pages use, render it full width on `/topology`, add the
legend, and have `links.tsx` pick line style from the evidence class rather than
from depth. Keep the vertical grouping on phones.
*Done when:* a network with one LLDP link, one MAC-table inference, one wireless
client and one manual link draws four distinguishable edges, and the legend names
all four.

**Slice 5 — Overview rebuilt.** *(medium)*
Attention strip, four facts, 24h throughput, networks, events. Keep the existing
setup checklist above it for a router that is not configured yet.
*Done when:* a configured router shows no checklist and a fresh one shows only the
checklist.

**Slice 6 — Write it down.** *(small)*
Update the visual guide and content guide for the rail, the brand rule and the
device vocabulary; delete the old navigation; extend the control-language test
with the new device words.

Order matters in one place: **slice 1 before slice 2**. A device page is only
worth building once the list can tell you which device to open.

## 10. Open questions

- **Does Topology earn a top-level slot?** It costs a route and a page, and it is
  the only view that can draw a network with holes in it. If a household has one
  switch and one AP, the answer may be "keep it in Overview, full width, with a
  link" — slice 4 can end either way.
- **A read that takes 30 seconds.** The service budget is 30 s per device. The
  page needs a skeleton per section, not a spinner over the whole header, so
  identity renders from the inventory while the hardware answers.
- **Per-port rate.** The adapters report counters; a rate needs two samples and a
  clock. If we show one, the page says "over the last 30 s" rather than implying
  the switch told us.
- **Server-side read cache.** §6.6's session-scoped status is honest but forgetful.
  Caching the last read per device in `olrd` would let a reload show real states;
  it also starts a history we do not currently keep.
