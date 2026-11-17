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

**5. Density with one accent.** A 14px base with a 13px step for supporting text, hairline dividers, one interaction
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
is shadcn's Sidebar, and it collapses to its icons (⌘B / Ctrl+B, remembered in a
cookie) with each name in a tooltip.

Under the wordmark it prints nothing. It used to name the host the console was
talking to, and to print `Not answering` whenever the five-second metrics read
stopped getting an HTTP answer. Both are gone: the console is served by the
daemon at that address, so the host repeats what the reader typed or the bar
still shows, and a label that only ever qualifies itself is a permanent address
line priced at a poll on every page. Liveness keeps the answer it always had — a
read that does not complete says `Not answering` on the card that needed it, and
a page that renders at all is the daemon answering. A rail that holds navigation
holds no status.

The rail's foot is what lets the page column end with content instead of a
footer. It holds two things: the version, as a button that opens the console's
own menu, and the collapse toggle. The menu carries theme, language, GitHub,
Credits, the API token when one is set, and the copyright, and it opens upward so
it never covers the navigation it sits under. Collapsed, the foot keeps the
toggle alone: the rail is 48px wide and has one job then, which is bringing the
names back. Those items are the console's, not a page's, and they are set once: a
row of five icons at the bottom of the rail would spend the rail's most useful
strip on what an operator touches least. A phone has no rail, so below `lg` the
same items are one centred row above the tab bar instead.

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
- **The strip is four facts, per device family.** A switch is asked about ports
  and PoE, an ONT about its optical budget, an AP about radios and clients; the
  frame owns the row and each family says what belongs in it. It is not a card
  grid, because a device page is not a dashboard.
- **The pill reports the read, not the socket.** Reaching a box and getting
  nothing usable out of it is not `Connected`: it is `Unknown`, with one line
  above the tabs saying nothing could be read and the capability matrix below
  saying why. A page that stayed coherent — identity, one sentence, one matrix —
  is the difference between a broken device and a broken adapter.

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

### 6.4 The map is logical, and says so

The overview ends with a picture: the router at the top, the operator's groups
below it, the devices inside them. That picture lives there and nowhere else. A
page named for the network's shape is not a second copy of a page about groups,
so `/topology` is the other half of the same question: the hardware this router
can actually observe.

**A line is membership, not cabling.** It joins a group to the router; its
thickness is the group's traffic. Nothing on the drawing claims that two boxes
are plugged into each other, because nothing we can read establishes that:

| Evidence | Do we have it? |
| --- | --- |
| LLDP neighbours, both ends reporting a port | No — no adapter populates `neighbors` |
| The forwarding table placing a MAC on a port | One MAC at a time. The firmware serves `mac_address_search.cgi` and a search form; it has no table page to export (§10) |
| Wireless association | No — the association list is per access point, not drawn here |
| The interface a device was heard answering on (`network_origin: observed`) | **Yes** |
| The address range a device falls into (`network_origin: detected`) | **Yes** |

The last two rows are the evidence we actually hold, and they are about
*placement*, not wiring: "this device answered on lan0" and "this device's
address falls inside lan0's range" are different claims with different
strengths. The limit is a sentence on the topology page rather than a legend
invented for a drawing that cannot support it.

**Measured, not assumed.** On 16 November 2026 the LAN was listened to for 75
seconds: zero LLDP frames arrived on the segment while 193,000 frames passed in
a 20-second sample, so the capture works and nothing on this network advertises.
The switch is silent, and no access point speaks it either. LLDP is not a
setting we forgot to read; there is nothing on the wire to read.

The four line styles sketched earlier — solid for both ends reporting, dashed for
a forwarding table, dotted for a wireless association, a ring for a box we can
reach but cannot describe — are therefore **withdrawn, not postponed**. Drawing
them would be a guess in a line's clothing, and there is nothing on this hardware
to guess from.

**`/topology` is the boxes.** The page is the equipment inventory read out loud:
a card per box, the network its management address falls inside, its ports that
have a link, and the clients it reports, by name and signal. A switch is not a
client in anybody's group, and a card between the router and the group columns
would need every group's line to run through it or squeeze past it — the one
thing that layout refuses to do (`features/topology/layout.ts`). So the two live
on different pages instead of one drawing pretending to be both.

**The card draws the box.** A reading says how many ports have a carrier, and
a sentence cannot say *which* — which is the whole of what somebody standing in
front of the switch is asking. So a card draws its own front panel from the
ports the answer listed: one shield per RJ45 port, a cage for each fiber port,
a lamp above every one, lit only where the box reported a carrier. Ports
without a connector are not drawn at all — a bridge, a VLAN and a radio are
interfaces the firmware lists, not holes a cable goes into — so the row on the
screen is the row in the answer, and a box that has not answered has no panel
rather than an empty one.

The geometry is traced once, in the reference switch's own units — a 146 × 120
port, measured pixel by pixel off a reference picture of that model — and drawn
as vector paths. That is what
makes it worth doing in code instead of shipping a picture per model: the same
markup is a thumbnail at card width and a legible port at 4×, the lamp, the
shield count and the cage are the reading rather than a decoration on top of it,
and a model nobody has drawn yet still gets a panel that is honest about what it
reported. There is no per-model art to keep, and nothing to re-shoot when a
switch is added.

It is drawn flat: two greys, a dark opening, one green accent, and no gradients,
shadows or reflections. Shading a picture this small reads as dirt, and what the
panel has to say is *which* port — a rendered material competes with that
instead of carrying it.

Placement in that list is a **join, not a claim**: a box carries the network
whose range holds its management address. That is where it is managed from, not
where its cables are, and the page says so once instead of repeating a caveat
per card. A box reached by name, or from a segment this router does not serve,
says it has no network here rather than being filed somewhere plausible.

**Opening the page reads each box, once.** The read is the page's subject, so it
is not deferred to a button per card: somebody asking what is plugged into what
will not click four times to assemble the answer. What the page will not do is
read the same box twice — an answer this session already holds is reused, with
the time it arrived on it, and a box that does not answer says why instead of
retrying behind the reader's back. That is also the difference between this page
and the overview's map, which costs nothing to draw because it draws only what
the router already knows.

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
move the inspector. It is the sketch the implementation was built against, and it
stays because a drawing can try a layout the app has not committed to yet.

The stills in [`ui-console-shots/`](ui-console-shots/) are the running app, not
the sketch: the rail, the object index, the switch's front panel with a port
selected, and the map. Replace them when a slice changes what one of them shows.

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

Six slices, each independently shippable. Nothing here writes to hardware — the
one backend addition (`/api/gateway/activity/series`) is a read.

| # | Slice | State | Commit |
| --- | --- | --- | --- |
| 0 | Tokens and the rail | shipped | `9ac45e7` |
| 1 | Devices as the object index | shipped | `e4c18bd` |
| 2 | One device frame, three bodies | shipped | `8fdc963` |
| 3 | The switch's front panel | shipped | `654ae36` |
| 4 | The map as a page | shipped | `afbf54c` |
| 5 | Overview: counts and the day's traffic | shipped, except its events feed | `c0c7d57`, `31f614a` |
| 6 | Docs, stills and the cleanup pass | shipped in part: the stills are the app, the open list is written | `2db497f`, `88e8ac0` |

Two things went in beside the slices, both from testing against the real switch:
the "plugged into" lookup, which the Mercury adapter could already answer
(`d15218d`), and one health rule shared by the list and the page (`2db497f`).

What is left is in §10, and it is not UI work: LLDP was measured and closed, the
events feed needs a definition, the rest need adapter or sampling work.

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

**Slice 4 — The map gets a page.** *(medium)*
`/map` is the world-traffic map and stays where it is; the device tree had no URL
of its own. Lift its wiring into `NetworkMapPanel` so both pages use one copy,
add `/topology` with the legend, and keep the vertical grouping on phones.
The evidence line styles wait for an adapter that reports neighbours (§6.4).
*Done when:* the page renders the same picture as Overview, the legend names what
a line is and what it is not, and neither page can drift from the other.

**Slice 5 — Overview rebuilt.** *(medium; in part)*
Shipped, then revised: one verdict card, with the faults inside it and the
evidence for it behind one disclosure; five readouts; the chart; and the map
under a header that counts what it draws. The counts live on the readouts and
that header, each still linking to the page that owns the number. The separate
count strip, the radar and the card that flipped to the map are gone — the
chart links to /map, and the verdict and faults answer "is anything wrong" in
the one place a reader already looks.

Two pieces are still open, and neither is a UI job:

- ~~A 24-hour throughput chart~~ — **shipped as "traffic counted"**. The daemon
  keeps half-hour samples (`/api/gateway/activity`) per client or per address;
  `/api/gateway/activity/series` adds them up in one place. It is deliberately
  not called throughput: the sampler stores nothing it cannot attribute, so the
  card says "counted for clients this router can name" and that the link carried
  at least this much.
- **An events feed.** Nothing exposes one. "An event" is undefined here — the
  audit log records mutations, not network happenings — so this needs a decision
  about what an event is before it can have a list.

**Slice 6 — Write it down.** *(small)*
Update the visual guide and content guide for the rail, the brand rule and the
device vocabulary; delete the old navigation; extend the control-language test
with the new device words.

Order matters in one place: **slice 1 before slice 2**. A device page is only
worth building once the list can tell you which device to open.

## 10. What is still open

Nothing below is waiting on a design decision in the UI. Each is waiting on a
fact about the hardware, a definition, or a deliberate trade — and each says
which.

| Item | Blocked on | What it would take |
| --- | --- | --- |
| ~~**LLDP neighbours**, and the four evidence line styles of §6.4~~ | **Closed** (§6.4): measured on the live LAN — no device transmits it, and the adapter's firmware pages contain none. Reopening it means turning LLDP on at every box first, which is deployment, not design. | — |
| **An events feed** *(deferred — see below)* | A definition. "An event" is undefined here: the audit log records mutations, not network happenings. | Agree on one, e.g. "a state change nobody asked for" — port link changes, a device flipping to `Not answering`, a service dropping. Then keep a bounded ring buffer of them. |
| ~~**`fdb.list`** (the whole forwarding table)~~ | **Closed**: the switch's UI has no table page. Probing the live firmware found six pages — Main, MAC search, port statistics, 802.1Q, PVID, port mirror — and the only forwarding-table surface is a form that requires one MAC. A bulk read would need SNMP or a firmware change, not a parser. | — |
| **Per-port rates** | Arithmetic. Adapters report counters; a rate needs two samples and a clock. | A sampler beside the activity one, or a second read on the open page. Must say "over the last N s", never imply the switch reported a rate. |
| **A read that takes 30 s** | Nothing — it is work. | Per-section skeletons on the device page so identity renders from the inventory while the hardware answers. |
| **Server-side read cache** | A trade. §6.6's session-scoped status is honest but forgetful. | Caching the last read per device in `olrd` would let a reload show real states, and would start a history we do not currently keep. |
| **Artwork for Mercury** | An asset. `web/src/assets/device-icons` has no Mercury product shot. | One illustration in the existing style; until then the role-based generic switch picture is used and the model name carries the identity. |

### Deferred on purpose

**The events feed is a TODO, not a plan item.** It was scoped here, agreed to be
worth having, and deliberately left for later: it needs a definition before it
needs code, and none of the console work above is waiting on it. Recorded so the
idea is not lost and not mistaken for something that is half-built.

The other two closed items — LLDP and `fdb.list` — were closed by measurement on
the live hardware, not by judgement. Neither is a "not yet"; both are "not from
this switch as it ships".
