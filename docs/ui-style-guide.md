# UI visual style guide

This is the shared reference for future UI changes. The direction is **a precise,
calm network console with lightly tangible surfaces**. Clarity of network state
comes first; polish comes from alignment, typography, restrained depth and
consistent interaction. Keep the existing Geist typeface, router identity and
device imagery.

For information hierarchy, wording and progressive disclosure, follow the
[content and presentation standards](ui-content-guide.md).

For the cross-page component inventory, typography roles, surface interactions
and migration targets, follow the [semantic component contracts](ui-semantic-components.md).
That document defines shared size variants and records the implementation audit;
route-specific behavior below remains applicable.

## Findings and decisions

The audit found inconsistent content widths and headings, oversized tool
illustrations, repeated borders, scattered save buttons, values hidden on
phones, a cramped six-section navigation and inconsistent color meanings.
The shared shell, heading, list and control primitives now define the baseline.
Use them before creating page-specific styling.

Content cleanup must preserve existing data space and component consistency.
Remove redundant wording before changing density. Keep established metric cards,
charts and common fields; avoid introducing extra presentation variants for
secondary information. See the [page review record](ui-page-review.md).

## Color and surfaces

Configuration backup reuses PageHeading, Card, Input and Button. Use a shared
Alert for the validated restore review. Keep file selection and restore actions
reachable on phones; wrap module names, errors and long addresses.

Connectivity recovery uses the shared Alert and Button components above the
application shell. Match shell width/gutters, wrap actions on phones, and retain
visible errors and remaining confirmation time on every page and after reload.

World traffic is the one map where hue belongs to data rather than to status.
Every location gets one translucent amber arc of the same weight: 2px where the
map fits the world into a laptop or a large window, about 1px in the card
preview and on a phone, where the same world is only a few hundred pixels wide.
Amber sits outside the country palette, so traffic reads as an overlay on
geography; the arc fades in at the router, and a blurred halo underneath
accumulates where arcs bunch up so busy junctions read as light rather than as
spaghetti. An arc holds its weight for as long as its location reports traffic
in the sampled window and fades only when the location leaves it: a line that
dimmed while the flow continued would claim a connection had ended, and the
sampler cannot know that — it differences conntrack counters and reports a rate
over a rolling window, with no connection start or end. Width does not carry
volume for the same reason it was flattened: four thicknesses compared badly
across dozens of arcs, and the ranked rates live in the details dialog, so the
map stays a presence and direction picture. A few arcs at a time — never more
than five, and fewer when the map is sparse — carry a soft warm highlight out
and back along the line, four seconds each way, drawn as a short blurred stroke
with a feathered tail and faded ends. A new location takes the next free turn
and the rest queue by fewest turns and longest wait, so every arc gets its
light within a minute and the map never becomes a field of moving sparks. Each
arc keeps its own phase, so the lights that are lit sit at different points.
Motion, not colour, carries the direction, and the pale warm light keeps amber
as the map's only hue. There are no endpoint dots — nearby destinations would
turn them into a cluster of specks at the hub, and the converging arcs already
mark both ends. The glow is a map effect only; it does not introduce glow
elsewhere.

Use semantic Tailwind tokens from `web/src/index.css`, never page-local gray or
blue approximations. Light and dark themes are designed as separate contrast
pairs.

| Role | Light | Dark |
| --- | --- | --- |
| Canvas / background | #f6f7f9 | #111318 |
| Card / popover | #ffffff | #1a1d24 |
| Main text | #171b22 | #f1f3f7 |
| Secondary text | #5b6472 | #abb4c3 |
| Subtle surface | #f0f2f5 | #232731 |
| Divider | #e2e6ec | #343b48 |
| Input border | #a4adbb | #67758b |
| Primary text / focus | #1d4ed8 | #8ab4ff |
| Solid primary action | #2563eb | #2563eb |
| Text on solid primary | #ffffff | #ffffff |
| Receive / download | #1d4ed8 | #8ab4ff |
| Transmit / upload | #0f766e | #6bd4c6 |
| Success text | #167447 | #70d8a5 |
| Warning text | #935a10 | #f1c071 |
| Destructive text | #b42335 | #ff9daa |

Use `primary-fill` with `primary-foreground` for filled actions. Use `primary`
for links and selected navigation. The selected section in desktop navigation is
a 40px `primary/10` tinted pill rather than a rule under the label; the tint is
a shape, so the selection survives without reading the hue. Use
`success-foreground` and `warning-foreground` for readable status text. Color
accompanies a label or icon; it never replaces one. A normally blocked DNS
request is neutral activity, not an incident. Reserve destructive emphasis for
errors and destructive actions.

Cards use a one-pixel border and shadow-xs. Shadows clarify elevation rather
than decorate every nested row. A list inside CardContent does not need a second
border. Use muted fills for small metrics, not another elevated card.
Use `panel-surface` for custom panels and `interactive-surface` for actual
link/button surfaces. Hover changes color, never shadow depth. Radius tokens
are 8px (`rounded-lg`), 12px (`rounded-xl`) and 16px (`rounded-2xl`). Dialogs
use 16px and shadow-md. Card spacing and title size are independent variants.

## Layout and hierarchy

Desktop and mobile navigation use Overview, Gateway, Wireless, Access and
Advanced, in that order. Wireless shows a managed access point's radios,
findings and associated clients; it is read-only in this increment.
Firewall, port forwarding, administrator access and the five diagnostic pages
are reached through Advanced and highlight Advanced in navigation, including on
their detail pages. Firewall and the tool pages keep their addresses; port
forwarding and administrator access moved from Access, so their old addresses
redirect rather than disappear.

Firewall's arrival diagram uses a centered firewall boundary spanning 40% of
the chart width inside its 24px gutters. Pack incoming bands contiguously along
each edge, with logarithmic widths normalized separately for each side and a
4px minimum. Keep each curved path region about 88px tall and packet values
with their units on one line. Reserve separate label space so small traffic
categories remain readable.

Access has three app-launcher grids headed “Networking”, “Add a way out” and
“Add a way in”, in that order. Networking contains WireGuard, Tailscale, NetBird,
ZeroTier and Headscale, shown once rather than repeated in the directional
groups. Keep their existing configuration actions available. Do not show System
default or purpose filters. Each tile shows a locally bundled
project logo above a short name, without an enclosing card, border, shadow or
corner arrow. Preserve brand colors on a white 64px rounded icon backing in
both themes. SOCKS5 and Advanced's Port forwarding use the reviewed original
app icons in `web/public/feature-icons`: simple flat color blocks on a softly
colored rounded tile, without 3D or material effects. These fill the 64px backing
without a second white inset; remove the generated masters' outside white field
before bundling. Use frp's official website icon as project artwork. Other
protocols without approved artwork retain a capital initial on a tinted tile;
the hue keeps shared initials apart. Use three columns on phones, five
from 640px, seven from 1024px and eight from 1280px, with 12px horizontal and
24px vertical gaps. Keep names below icons with an 8px gap and visible keyboard
focus around the whole tile. Group headings use 24px/32px semibold text. Each
tile is a link with one destination: the configuration page for the direction it
belongs to, or the project's documentation in a new tab when OLR does not
configure it. Do not open a dialog on click. Hover/focus explains function and
state, and the accessible name repeats that state for touch and screen readers.
External setup stays explicit.

Advanced is the same launcher, with three grids headed “System”, “Services” and
“Diagnostics”, in that order. System holds the machine and what may reach it
(firewall, administrator access, port forwarding); Services holds the background
jobs it runs for the network (DDNS, IPTV multicast); Diagnostics holds the
five tools. Each tile wears a capital initial on a tinted tile, and the tiles of
one group share a single hue, so the tint groups them without a legend. The one
exception is a diagnostic that names an upstream tool: it wears that tool's own
app icon. NextTrace's artwork carries its own rounded background, so it fills
the 64px tile instead of sitting inside a white or tinted one. Keep such artwork
bundled under `web/public/tool-logos` with its provenance recorded there, and
never borrow the mark of a protocol or of a service a test merely contacts — a
speed test keeps its initial, because the measurement is OLR's, not Ookla's.

A tile is a mark and one line of name. No name wraps and nothing prints under
it: a row of two-line labels makes the next group's heading look misplaced, and
a directory that reports nine statuses has stopped being a directory. Keep the
tile's name short enough for one line — the page behind it may carry the longer
name, as Admin access opens Administrator access. The state still travels with
the tile: hover/focus explains the function and the current answer, and the
accessible name repeats it for touch and screen readers. Keep the directory's
tiles and the shared launcher in `components/layout/launcher.tsx`; a tool tile
links to its diagnostic page, and there is no Tools directory page in between.

Credits opens with its title and then one ungrouped table: no visible header row,
no section rows, and no website column. The first column is the destination —
mark and name together as one link — followed by the version and the feature the
project carries. List every project that carries a feature, and a Go or web
library only where it is that feature's supplier; the complete dependency and
license lists stay linked from `go.mod` and `web/package.json`. Project marks are
bundled under `web/public/credit-logos` (or reused from the feature directories)
and sit on a white tile in both themes; a row without approved artwork keeps a
neutral initial. Below 768px the rows become stacked records with the same three
fields, rather than a horizontally scrolling table.

Tunnel Broker uses one scrollable page with five numbered sections and a sticky
step navigation immediately below the 64px shell header. Keep section anchors
clear of both bars and scroll the navigation locally on narrow screens. The
page title is accessible but visually hidden; avoid a repeated introductory
heading above the step navigation.

Gateway's landing page is a directory, in the launcher Access and Advanced
already use: five groups in the order an operator arrives at them — Internet,
Interfaces & routing, DHCP, DNS and Firewall — each holding one tile per way of
building that part. Use three columns on phones, five from 640px, seven from
1024px and eight from 1280px, with the group's 24px/32px heading above it and
each tile a letter in the group's hue, so the tint groups the tiles without a
legend. Names stay on one line and nothing prints under them.

A choice OLR cannot configure yet keeps its tile at 55% opacity, with no pointer
and no destination: a group states what it will contain instead of growing a
tile at a time. It is not an external setup, so it never borrows that state's
language, and its tooltip says plainly that nothing opens. A tile that is a link
opens the one page that configures the choice; the directory has no status
strip, no switch, no draft, and no tile that opens a dialog.

The interface cards and the two stacks, IPv4 and IPv6, that used to be this
page are not on it any more: a stack card's answer and the switch that changes
it were always one page away, and links that used to target
`gateway#interfaces` now target `gateway/interfaces`.

The shell has a 1280px maximum width, a 64px header and horizontal gutters of
16px on phones, 24px from 640px, and 32px from 1280px. Content starts with
24px vertical space on phones and 32px on larger screens.

All page containers use the available shell width, centered in the viewport.
Header, page headings, cards and footer share the same outer boundaries. Do not
apply route-dependent page widths. Individual fields and explanatory paragraphs
may constrain their own reading width without narrowing the entire page.
Section landing pages omit the visible heading already named in navigation;
retain an `sr-only` h1 for assistive technology, with no heading gap. Detail and
operation pages use `PageHeading` and `SubPage`: a visible title, an explanation
only when needed, and an optional primary action. Sub-pages do not repeat the
section navigation, because the shell's section tabs already name the section
they are under. Gateway's nested DNS and DHCP settings are the one exception:
those pages keep a Gateway › DNS breadcrumb, since the top bar can only name
Gateway, while the page that carries its section's own name (details) shows
none. Preserve existing redirects and deep links when moving settings.

The spacing vocabulary is 4, 8, 12, 16, 24 and 32px. Use 32px between major
sections, 16–24px inside cards and 8px between label and control. Basic corner
radii are 8px for controls/small inset surfaces, 12px for panels and 16px for
larger enclosing surfaces. Avoid arbitrary one-off radii. Overview metric cards
use 12px padding on phones and 16px on larger screens, an 8px heading gap,
36px metric rows and 4px between rows. Interactive rows retain 44px targets on
phones; site latency items use unfilled 32px rows with 6px vertical gaps.
Only download/upload rates use inset meter surfaces
in Overview cards. Status, public addresses, latency, totals and uptime use plain
rows; monitored sites retain their icons without capsule backgrounds.
Cumulative traffic rows use Download and Upload labels with plain byte values.
On desktop, Overview cards share title and primary metric boundaries. Totals
and site probes follow their primary metrics without divider lines. Status
flows naturally. Below desktop, cards use their own content height.
Site probes use two columns on phones and three from 640px. Overview shows only
sites pinned from /latency; no site is pinned by default. Internet and DNS keep
their existing rows. With no pins, omit the site grid and its padding. Base the
card's expanded height on the number of pinned sites rather than the full catalog.
Internet target selection and monitored-site configuration remain API/CLI
operations; /latency presents measurements, test actions and Overview pin toggles.
The /latency page is a test view, not an editor. Show all sites in one list
panel, divided into plain 16px semibold category headings: no band, rule or
count, a 40px heading row, and 16px between one group and the next. The heading
stays at the left when the panel scrolls horizontally, and the rows beneath it
are indented by the pin slot.

Each row has a 24px pin slot before a bare icon and name, 24 aligned 20px square
samples with 4px visible gaps, and a right-aligned average. Keep the name column
fixed at 176px on phones and 192px from 640px, with an 8px/12px gap to the
sample grid, rather than letting the name column absorb available width. The
sample band is 24 slots of 24px (576px), left-aligned; it does not stretch to
fill the row. The samples retain fixed router-local hours from 00:00–01:00 to
23:00–24:00; do not slide or reorder them as measurements arrive. Missing,
future and skipped hours stay neutral gray, and a failed attempt uses the same
neutral fill because it is not a value on the day's scale. Use success below
100 ms, warning from 100 to 499 ms and destructive at 500 ms and above. Retests
replace the current hour. Tooltips and accessible names identify each exact
date, range and outcome. While a site's on-demand probe runs, its current-hour
square becomes a primary spinner and crossfades and scales into the result over
200ms. The average is the arithmetic mean of that day's successful samples,
right-aligned in its own column, with the same colour bands and an em dash when
none succeeded. Keep columns aligned across sites, and scroll the panel
horizontally on narrow screens while keeping site identities pinned at the left.

Prefer the locally bundled theSVG mark for built-in sites, then favicon discovery
and bare initials, without added backgrounds, borders or clipping. Adapt black
marks for contrast in dark mode without a backing.

The pin is a 24px button before the mark, not the whole identity. An unpinned
pin is transparent until its row is hovered or the button is focused, and stays
visible on coarse pointers; a pinned pin remains visible in primary. Hover
changes only the pin's background and icon colour; the mark and name do not
react. During a save, show progress on that row and serialize pin changes.
Commit the visible state only after a successful save, and expose failures
without dropping measurements. Pin choices persist in router configuration and
do not start a measurement. There is no add, remove or site-edit control.

Test all sits beside the page heading and updates only the current hour. It asks
for sites one at a time in reading order and names the progress position on the
button. It does not highlight or scroll the active row; a failed or timed-out
site does not stop the rest, and completed results stay visible. There are no
category or row test controls.
The public-network summary uses a locally bundled flag-icons country flag.
Clicking any Status row opens its details in a shared dialog, without chevrons.
Public addresses use monospace and wrap within the dialog.
Status rows fill the card width. Give flags and health icons the same 16px
leading slot so text starts align.
System health uses the same dialog interaction as uptime, with module names on
the left and status values aligned right. Keep module details as plain rows.

## Typography and imagery

Use Geist Variable with system sans-serif fallback. Page titles are 24px/32px,
weight 600. Panel titles are generally 16px, weight 600. Body and controls are
14px; descriptions and technical metadata are 13–14px with relaxed line height.
Use 12px only for compact auxiliary metadata. Mobile text inputs are 16px to
avoid browser zoom. Use tabular numerals for live rates and counters, and
monospace for addresses, commands and configuration.

Device imagery identifies device categories. Device icon selection is one search
box over two catalogues — the pictures bundled in the binary, matched on labels,
makers, categories and tags, and theSVG's marks, matched on the daemon
(`internal/brandlogo`: exactly, then by prefix and substring, then on a near
miss, so a dropped or transposed letter still finds the mark) — so it works on a
router with no internet and draws no third-party image. The box sits above a
list that scrolls on its own, so it is still there when the list is long. Below
it, two blocks: brand pictures grouped one labelled section per maker — Apple,
Samsung, Huawei — with makers holding one picture sharing a trailing *Other
brands* section, then one *Common* grid mixing every unbranded picture and its
variants. A query replaces both blocks with results, bundled pictures first and
theSVG's marks under their own heading. Operating-system marks are no longer
offered there; stored marks still draw. See
`web/src/assets/device-icons/ICONS.md`. Render chosen logos in the same muted
tile as operating-system marks throughout device views. A device that dialed in
wears the Way in badge in the lower-right corner of that picture — the same
badge in the map's band card and on its own page — because the picture is the
same in both places and the badge is the only thing that says the tunnel brought
it.

Tool directory entries use Lucide
outline icons with a 1.75px stroke in a themed 56px surface (48px on phones),
an 18px name and one directional chevron. Avoid raster illustrations with baked-in
backgrounds. Overview metric headings have the same 24px line box; auxiliary
icons extend their hit area without increasing the heading height. Descriptions and instructions
belong on the individual tool page. No decorative glows or additional gradients are needed.
Use Lucide consistently for functional icons, normally 16–20px. The two-color
brand mark retains its own colors in both themes.

## Controls, lists and dialogs

Ingress pending repairs reuse Alert above the settings and a warning status dot.
List remaining errors with service names and wrapping text; keep the existing
single-service edit dialog and save action.

Use the shared Button, Input, Select, Textarea, Switch, Card and Dialog components.
Regular controls are 36px high on desktop, `sm` is 32px, and mobile
button/input/select targets are at least 44px high. Switch visuals are 44 × 24px
with an expanded mobile hit area; `busy` keeps an applying switch at full
contrast and `disabled` is reserved for a change the router will refuse. Focus
uses the primary ring and must remain visible. Hover and active changes use color;
avoid positional movement.

State, switch placement, button rank and row glyphs follow the
[control language contract](ui-control-language.md). Five tones are shared —
`ok`, `warn`, `fault`, `off`, `unknown` — and a word's tone is fixed in
`lib/state.ts`, never chosen per page. A state is drawn in one of three shapes:
`StatusPill` for the subject of a card, `StatusInline` (dot and word) for an item
in a list, and the status strip for a module's verdict. Measurements stay plain
values; only a grade is a pill. A row or card header shows at most one
control: a chevron means "this row leads somewhere", a pencil means "this row can
be changed", an ellipsis opens a menu, and a rotating chevron expands in place.
Editing and removing live on the page the row leads to, not beside its link; a
switch displaces the chevron rather than sitting next to it.

Inline editable fields keep the input, explanation and save action within the
same 720px field area. Show saving, error and completion where the action occurs.
Device titles are edited by clicking the name, with the displayed value prefilled.
Blur or Enter saves; Escape discards. No pencil or save/cancel controls accompany
the title. The name input is 320px wide, capped by available space.
Device traffic activity sits behind the title, aligned right and fading to
transparent toward the overlapping name and icon. Omit the visible presence chip:
lists and the map say when a device was last heard, and the device page does not
repeat it. Each day is one column, newest on the right, with
six four-hour dots from midnight downward. Future periods stay blank, and dot
tooltips appear immediately. A divider separates the
title from two columns of 36px desktop / 44px mobile minimum-height label/value rows: stable identity
and settings on the left, routing, priority and traffic on the right. Group uses
a clickable tag with a muted surface that opens its edit dialog.
Device traffic reuses Overview meters with visible direction labels.
At desktop widths both halves share five grid rows: Vendor/Uplink,
MAC/Priority, IP/Download, Group/Upload and Services/Total. The hardware address
is a mono fact row, not a chip beside the title; the dialed-in page puts its
configured name in the same place. Services shows matching configured
services as 28px icon tiles linking directly to their published webpages. A matching
28px outlined add button opens Ingress with the device prefilled. An empty list
shows only the add button. Total uses the Overview Traffic meter's neutral text
arrows (↓/↑), with the same size and weight, and byte values, with no caption
after them: it sums the title matrix rather than a counter, and that matrix is
where the window is read.
Match row padding and let wrapped content grow both sides together; feedback follows the aligned rows.
Device speed bars use half their available value width from 640px, aligned right;
on phones they retain the full width so rates and limit actions remain readable.
The device title contains a right-aligned traffic matrix occupying two thirds
of the title width: one column per day and six four-hour periods per column.
Cells stay 14px square with 3px corners. Derive the row gap from the 96px
matrix height, six cells and 1px vertical padding; use that same gap horizontally.
Fit as many complete date columns as the available width allows; wider layouts
show more dates rather than stretching cells. Keep today at the right edge,
the current-period hint and 1px inner padding.
Fade only the leftmost 20% toward overlapping identity content; keep the rest
fully visible, including zero/unknown dots. Use the shared opaque activity
palette: zero/unknown uses the empty gray, low nonzero traffic uses a stronger
neutral gray (#cfd4db light / #48515e dark), and the remaining four levels use
green intensities, using 25%, 50% and 75% of the greater of the retained
90-day four-hour peak and 1 GiB as boundaries. Green starts at 10 MiB;
boundary values keep the lighter tier. Use no decorative borders or inset rings;
cell fills and spacing define the grid. Retain visible keyboard focus. No axes,
legend, heading or helper text; tooltips provide dates, units and sample state.
Uplink and Priority lead the right column with compact segmented selectors.
Clicking each labeled speed bar opens its limit dialog; omit separate limit
fields and the QoS divider/status row.
Device policies apply automatically, without a switch. Keep QoS controls on
devices; Gateway does not expose aggregate link-rate settings. Blank device
limits mean unlimited; pending placement must remain distinct from active state.
Preserve existing confirmation and apply-preview behavior for network changes.
Group details reuse the device summary card: a 64px icon and title above a
divider, then two columns with a 64px desktop gutter and 32px stacked spacing.
Use the shared device label/value rows for parent, member counts and subgroup
tags on the left, with matching Uplink and Priority segmented controls on the
right. Keep batch apply actions and scope beside those controls, followed by
the device list in its own panel. Use the device page's 16px panel spacing.
Device detail rows use two zones: left-aligned labels and right-aligned values
or controls, with 36px desktop / 44px mobile minimum row height. Keep disclosure chevrons beside their
values rather than in a separate action column. Clicking the IP opens a dialog
with an address and fixed toggle, without a pencil. A Fixed badge follows reserved
addresses. Device traffic rates have Download and Upload labels beside narrower,
arrow-free inset meters; the recorded total sits below Upload. Uplink sits
above Priority in the right column. Both use segmented controls with 28px desktop
buttons, 2px container padding and 44px mobile targets. Uplink applies through
the existing network-change confirmation flow. Separate the
desktop identity/settings and traffic halves with a 64px gutter; stack them with
32px spacing below desktop.
Never style a pending or failed read as a disabled service.
Device inspection uses one panel below a fixed 72px title bar, separated by one
full-width divider. Keep Activity and saved Requests visible with inspection off,
including when another device is active. Current DNS queries appear while active.
Use headings and spacing between sections, without a rule per section or DNS row.
The title names the only Inspection switch; keep the setup gear at its left in
every state. QR code, certificate guide and global retention settings live in the
setup dialog. Disable scroll anchoring on the panel and reserve the document
scrollbar gutter to prevent horizontal movement.
Activity's range controls wrap on phones and identify the selected range with a
shared secondary button and `aria-pressed`. Requests uses the same controls for
Saved and Live while active. Saved history is the default, with a wrapping search
and clear action above plain rows; long URLs wrap and each row has a separate
44px download target. A saved row opens the shared dialog for request/response
headers and bounded, scrollable body previews. Show binary/truncated content as
text status with full native download access. Clear opens a separate confirmation
dialog. Live retains the embedded mitmproxy viewer at its existing height.
Keep device request storage, router-wide request storage and age retention in
one auxiliary line;
retention fields explicitly name the router total and preserve units on phones.
DNS uses the shared table without a visible header, with Time, Record, Type and
right-aligned Answer columns, and without a switch of its own: the panel's
switch already opened it. Its search icon reveals a compact filter in the
section heading, opening to the left of that icon with a 200ms width/opacity
transition that honors reduced motion. The switch stays pinned to the right edge,
so opening the filter adds controls to its left instead of moving it. Keep long
records and answers readable.

List rows have a minimum height of 64px. On phones, trailing values reflow below
the title instead of disappearing. Technical values and important descriptions
must wrap or have an accessible path to their complete value. A secondary
action is a sibling via ListRow's `action` prop, outside the row link/button.
For Button rendered as a Link, set `nativeButton={false}`.

Dialogs use a restrained scrim, a 480px default maximum width and 16/24px
padding. Long forms must scroll within the viewport and retain a reachable
close and submit action. Empty states use a normal surface, a short explanation
and a relevant next action where one exists.

## Responsive behavior and motion

Overview has four desktop cards: Status, Traffic, Latency and Quality.
Clicking the Latency heading links to /latency; omit an add icon.
Map and Quality chart content use half the normal card side and bottom gutters
(6px on phones, 8px on larger screens), while headings retain their normal inset.
Desktop cards target 4:3 proportions with a 240px minimum height and grow for
disclosures or extra probes. Use one column on phones, two from 640px and
four from 1280px so values and units remain on one line.
Traffic's title-right map icon toggles its statistics and embedded map views.
Keep both mounted as the front and back of one card to preserve height and map
continuity. Rotate the whole card 180 degrees around its vertical axis over
600ms, with 1200px perspective and hidden backfaces. Each face has its own
title and toggle so the map appears on the back of the card. Reduced motion
switches faces immediately. Make the inactive face inert and hidden to assistive
technology. Do not add a Map details row; clicking the map links to /map.
The Quality heading and radar link to /quality; omit a
separate Test button on the card. Retain measured scores without age-based fading or hiding.
Network quality uses a five-axis radar with theme-aware primary fill and border
grid lines. It occupies one card at every breakpoint. Only measured, current
axes have points; missing/stale axes show dashes and prevent a filled polygon,
except Wireless, which defaults to 80 for display. Omit the title-right total
and below-plot scope caption. /latency, /quality and /map are normal detail pages
inside AppShell, with its header, footer, content width and gutters. All three
keep Overview selected in desktop and mobile navigation. Use PageHeading for
their visible h1; they have no fullscreen dialog or floating close control. The page header
names the view, dates the snapshot and carries the run action with the one line
naming the included bandwidth test. A four-column summary then sits beside an
eight-column category panel on desktop and stacks in that order on phones. The
summary holds the score ring, the display grade, the count of scored categories
and a measured-coverage bar, with the transfer panel beneath it leading with the
download and upload rates as metric values; the score itself stays in foreground
colour while only the ring arc takes the grade hue, so a run of Fair categories
does not paint the page as a field of warnings. Categories are hairlined rows of
one panel: a 16px semibold heading, the 0–100 score in a fixed
right-aligned column, an action slot that stays reserved when a category has no
rerun control, and a bar whose track is 160px in every row so fills compare down
the list. Each row states its evidence in its own units beside the bar —
throughput, median and variation, response counts, reachable services — and
repeats no state word from row to row. Expanding a row indents its tests to the
category title, and each test shows its own medians with its score landing in the
category score column. A service probe is one test item because one run of six fresh connections
produces its timing, stability and access measurements; its rerun control updates
that service everywhere. Unimplemented items stay listed without a rerun control.
Scoring formulas, per-service samples and NAT observations sit in collapsed
Measurement details disclosures, so raw evidence never pushes the result below
the fold. Keep long errors and addresses wrapping inside the view.
Accessible chart descriptions identify partial and missing measurements.
Uptime is
the first plain disclosure row in Status, above system health. Both rows use
aligned 16px Lucide CircleCheck icons in success color when healthy. Uptime
opens a dialog with CPU, memory and root disk used/total values. Map has no information icon,
direction legend or top-ten summary. The
grayscale preview fills Traffic's remaining content area. /map contains a
pastel country map (muted colors in dark mode) in a bounded responsive panel
below the page heading, with pan/zoom. Keep direction labels, status, credit and
other text outside the map. The local
detailed map reveals province/state boundaries, their names and major cities
on zoom, stopping at regional detail (zoom 6). State names and capital/world-city
names begin at zoom 3; other major cities appear at zooms 4–5. Country fill
indices are stable and checked against neighboring countries, with separate
light/dark palettes. Keep administrative labels neutral and secondary
to traffic; traffic carries no GeoIP text labels.
Pan, pinch, scroll and double-click zoom stand alone, so place no zoom or
whole-world reset buttons over the map. Resizing must preserve the current
map view. Links and bookmarks open each detail page directly, and the Overview
navigation returns to the overview.
On phones each card uses the full column. Its local MapLibre map draws one amber
arc per remote location at a single weight — about 1px where the world is fitted
into a few hundred pixels, 2px at the world view of the detailed map — and the
arcs thin further as the map zooms in, so a regional view is not buried under
globe-width ribbons and a small map is not buried under a bundle of them. An arc
stays at full weight while its location is in the window and fades out when the
location leaves; nothing periodic touches it. Card preview and detailed map
show the same rotation: only a few arcs hold a highlight at a time, each a short
blurred warm stroke that travels out and back along the line, four seconds each
way, with a feathered tail and faded ends, so the light never lands at the hub or
on a destination. It carries direction and stops for reduced motion. Keep
highlight phases stable across ranking updates
and fade arriving/departing arcs over a fixed 450ms eased curve, which always
completes. When a snapshot brings or drops several locations at once, give each
one a slot in a window that ends before the next snapshot and jitter it inside
that slot, so the batch reads as an irregular sequence rather than a step or a
metronome; a lone change still fades immediately, and the whole batch has
settled by the time the next poll lands. Reduced motion drops the highlight, the
stagger and interpolation; hidden tabs pause updates. Arcs use valid returned coordinates without
country-center fallbacks; omit locations without coordinates. Draw flows between
different locations even within one country. The details dialog supplies
city/country, rates and ip.sb attribution without crowding the map.

Overview group headers navigate directly to group details at every screen size;
do not expand or focus named groups in place.
Device list rows reserve space for up to three 24px service icons with 4px gaps.
Use only the space needed by the device's services; additional icons scroll horizontally.
Keep the trailing activity column at 48px so service icons sit close to rates
and last-seen values without taking space from device names. A device row's
trailing value is when it was last heard — an elapsed time, a dash for a device
that answered nothing, or never seen — never an online or offline state, which
only a dialed-in peer has evidence for.

Network-map device ordering samples current traffic ranks every five seconds.
Pause reordering while the pointer is over the map, keyboard focus is inside it,
or a touch is active. Resume at the next scheduled update rather than immediately
on leaving. Rates and counters keep their normal live refresh; do not smooth them.
New devices appear after existing rows until the next reorder; removed devices
disappear immediately. Groups retain their name order.

The band above the router holds the only thing outside it: the devices dialed
in over WireGuard, one card each, drawn side by side and joined to the router by
their own line. A dial-in card is a device node, drawn in the band: the peer's
own picture from the same picker a device uses, its label over its tunnel
address, and the state in the same narrow figure column a device row keeps for
its rate or its last-heard time, with the endpoint and totals on hover. The way
in is a 14px badge on the corner of the peer's picture: the WireGuard mark at
full brand colour on the same white disc a protocol gets on the Access page, with
a 2px ring in the card's colour so it separates from the device tile underneath.
A badge on the picture is not a picture in place of the machine, and it costs the
card no width; the method also stays in the tooltip ("over wg0"). Connected is a
filled 8px success dot whose word lives in the accessible name and the card's
title; the other three states (an elapsed time, Never connected, Status
unavailable) stay words, since an absence of evidence must not be a status
light. It carries no rate, which is why the band's lines stay the thin ones.
Never mark an absent peer as a fault. WireGuard off, or a tunnel with
no peers, puts no band there at all: the map starts at the router, and with no
groups either it is one box.
Observed default routes are not cards on the map. A next hop is one line of
Link's status — address, interface, family — and drawing each one was a row of
boxes above the router that said less than that line while pushing every group a
third of a screen down. The device a next hop names is a device on this network,
so it is drawn in its group like any other, not lifted out of it. A way out that
has stopped answering is red where it can be acted on: the uplink card under
Gateway, which also names the interface the next hop does answer on, and a
network chip on the router when the gateway exit behind it is down.

Firewall uses one vertical Sankey with three visual rows: external semantic
categories above, one virtual OLR / Firewall node in the middle, and trusted/LAN
and router-local semantic categories below. Both sides flow toward OLR. These
positions distinguish origins, not successive processing stages. Keep the
semantic category and packet count on each outer node; omit repeated source and
Allowed/Blocked labels. Color conveys the decision, with full text in tooltips. Use one measured
packet scale and semantic receive/transmit and success/warning colors. Reserve
label space for small nonzero paths without inflating their widths. Provide an
accessible tooltip details; narrow screens scroll inside the diagram without
causing page-wide overflow. Adapt to available width and wrap labels. When the
chart must scroll, start at its leading edge; never
automatically center it and hide the first category.

Page navigation resets document scroll to the top before paint. Hash links retain
their section-target behavior; query-only changes preserve the current scroll.

Desktop section navigation begins at 1024px. Below that, show all five sections
in a five-column bottom navigation; do not require horizontal scrolling. The
bottom bar marks the current section with `text-primary` on an icon and label
alone: it has no room for the desktop pill, and a fill behind a 20px icon reads
as a pressed button rather than as a location.
Account for the device safe area and bottom navigation when laying out content
and the footer. At 375px, keep labels, statuses, addresses and action buttons
usable. Monitoring statistics use two columns on phones.

The footer is one line at every width: it centres its row and thins the gap to 12px
below 360px rather than wrapping. On desktop it bottom-aligns its text and icons with
12px bottom padding, keeping the visible content close to the viewport edge. Its items
run in one order — copyright, Credits, version, then the three controls — with one
visible 16px gap: each control spends 8px of side padding on a 44px-high hit target, so
the footer cancels that padding rather than letting the controls sit in visibly wider
intervals than the text. Below 640px the copyright keeps the year alone and drops the
product name, which the header already carries; the version stays at every width,
because the footer is the only place that reports it. The repository link is GitHub's
mark alone and the theme control a single icon; clicking the theme cycles auto, light
and dark, and both name themselves in their accessible label and tooltip. Both are
14px, the row's one icon size: an icon's class is its optical size, so artwork with no
inset of its own brings that inset with it instead of the call site asking for a
smaller box. The language
control shows only the language in use, in its native writing (English / 中文): footer
text size and weight, no icon, no separate "Language" label, and no filled hover
background, so it reads as footer text rather than as a button, with visible keyboard
focus. Like the theme control, it reports the state it is in, and its accessible label
and tooltip name the language a click switches to. Place the same control below the
sign-in and initial-setup forms.
Language changes preserve layout, current navigation and in-progress form values.

Use 150–200ms color/opacity transitions. Motion should explain a state change;
no ambient animation except observed world traffic and the current device-activity block's traffic indicator.
Breathe faster as combined live download/upload increases, with a logarithmic
4s–1.4s cycle capped at 100 MiB/s. Use opacity 1 → 0.45 over 55% of the
cycle, rest dim for 10%, then return to full opacity over 35%. Each changing
segment uses cubic-bezier(0.37, 0, 0.63, 1), with zero endpoint slopes for
gentle reversals. Keep the cell size and palette; no glow or scale animation.
Keep a fixed animation timeline and ease playback speed toward each new target
with a 350ms exponential time constant (about one second to settle), preserving
the current phase. When idle or unavailable, finish the current cycle and pause
at full opacity; resume from that phase when traffic returns.
Idle or unavailable rates stay steady;
historical cells retain their traffic fills. Honor prefers-reduced-motion in the global stylesheet.

## Page coverage and review checklist

Review Overview; device and group details; Gateway and interface details; exits,
usage and unmanaged settings; every DNS and DHCP detail/settings page; native
Firewall; Access and SOCKS5, remote and WireGuard; Advanced with its Caddy, port
forwarding, external filtering, DDNS, IPTV, administrator access and all five
diagnostic pages; Credits; not-found; token/password sign-in; initial setup and
first-run states.
Legacy URLs should still redirect to the appropriate current page.

For every iteration:

- Check 375px, 768px and 1440px layouts, light and dark themes.
- Check long device names, IPv6 addresses, empty collections and populated data.
- Verify keyboard focus, a single clear page title and semantic navigation.
- Verify loading, unavailable and error states cannot be mistaken for healthy data.
- Open affected dialogs, verify scroll/close/submit access and avoid nested buttons.
- Keep current value, units and action visible on mobile; avoid page-wide overflow.
- Run web lint, build and tests; run the repository's Go checks.
- Capture screenshots of affected pages and states for review.

The stylesheet and shared components are the implementation source of truth.
Update this guide alongside deliberate changes to these contracts.
