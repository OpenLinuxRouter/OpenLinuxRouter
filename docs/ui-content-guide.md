# UI content and presentation standards

The visual direction is a precise, calm network console. Content must support
that direction: users should identify the important information before reading
supporting text. This guide complements [the visual guide](ui-style-guide.md).

## Vocabulary

Two words are kept apart, in English and in Chinese:

| Word | Means | Does not mean |
| --- | --- | --- |
| **device** / 设备 | Network equipment olr manages: switches, access points and routers. `/devices`, `olr devices`, the device inventory. | A laptop, phone or printer on the LAN. |
| **client** / 客户端 | A thing on the LAN, keyed by MAC: identity, presence, groups, the target of a policy. `/clients`, the client inventory. | The equipment olr reaches over a management interface. |

Both used to be called "device", which is why a page that says "device" no
longer said which one. **Network** is a third word and always means a subnet
Gateway serves (`olr net`, `link.networks`), never this inventory. Where a
sentence is about hardware in general — "the device's own management interface",
"connect it to the device" — leave it alone; the rule names the two inventories,
not every use of the word.

## Start with the user's decision

Advanced's System group includes Backup. The backup page distinguishes a
credential-bearing configuration export from runtime or whole-machine backups.
Selecting a restore file validates it first; replacement requires a separate
explicit action showing scope, credential changes and the recovery deadline.
Preserve the original JSON bytes in both directions, including uint64 values.

Disruptive network changes require a second decision after application. A global
recovery notice shows the remaining confirmation time, Keep changes and Revert
now. Never confirm automatically on a successful API response or page load.
Keep failed recovery and lost connectivity visible; explain how to reconnect to
the previous address after the countdown.

Client priority and speed limits apply automatically, without an enable switch.
Use “applied” only after observing matching runtime state. Show pending device
placement and apply failures explicitly. Blank means unlimited. Keep the user
workflow focused on per-client priority and speed limits; do not add aggregate
QoS settings to Gateway or imply they are required for client limits.

Before adding content, write down the page's primary task and what the user needs
to decide next. Give each visible element a purpose in that decision. Remove
repeated or unrelated information; move useful later-stage information to the
stage where it is needed.

| Page type | Primary question | Lead with | Supporting content |
| --- | --- | --- | --- |
| Directory / entry | Which capability do I need? | Recognizable names and distinct imagery | A short distinction only if names are ambiguous |
| Monitoring | What is happening, and what needs attention? | Current state, important changes, key measurements | Units, freshness and scope beside the data |
| Configuration | What am I changing? | Current value and its control | Necessary constraints, consequences and apply action |
| Diagnostic operation | What should I run? | Target, essential options and run action | Requirements and measurement scope |
| Diagnostic result | What happened, and what next? | Outcome and decisive measurements | Evidence, limitations and a relevant next action |

Apply these rules to existing pages as well as new features. The page-by-page
[review record](ui-page-review.md) documents the current baseline.

## Simplification preserves data and consistency

Reducing clutter does not mean reducing every data display. First remove repeated
introductions, obvious instructions and duplicated labels. Then clarify grouping,
ordering and state. Do not shrink metric cards, value typography or chart space
to make a page appear quieter. Overview cards may reduce padding and row height
to improve density while preserving values, units and mobile interaction targets.
Section landing pages omit titles duplicated by navigation, retaining a visually
hidden page title. Detail and operation pages keep their identifying titles.

- Overview shows Status, Traffic, Latency and Quality cards. Traffic's
  title-right map icon toggles statistics and map views in place. Its pressed
  state identifies the active map, and its accessible label names the next view.
  The detailed map has its own /map address. The
  quality radar shows Speed, Latency, Stability, Wireless and Functionality
  on a 0–100 higher-is-better scale. Wireless defaults to 80 when unmeasured or
  stale; explain this display-only default in details and the accessible chart
  description. Other missing or stale axes show a dash. Draw a polygon when all
  five axes have display scores. Omit the card total and measured-category caption.
  Details show the total out of 100 using normalized weights of current scored
  categories and components. Missing tests never reduce the score; measured
  failures still count. Clicking Quality or its chart links to the assessment page at /quality. It leads
  with the overall score, one display grade, the count of scored categories and
  the measured coverage, with the reference transfer's download and upload rates
  beside them: the numbers a reader can check lead, and the score stands next to
  what produced it. Categories follow in one panel; each names the score in a
  fixed column and states what it measured in that category's own units —
  throughput, median and variation, expected responses, reachable services.
  Never repeat a category state as a word on every row: "partial" describes the
  unfinished half of the catalogue, not this connection, and the coverage bar
  already says how much was scored. Only a state the operator can act on, such
  as an outdated result, earns a label. Expanding a row lists its tests; a
  service test item can rerun just that service, and a category heading can
  rerun every test behind that category. Unmeasured categories are excluded from
  the total. Raw service and NAT evidence and the scoring explanation stay in
  collapsed Measurement details. The page always includes the reference transfer, so replace the old
  optional-bandwidth switch with one short line at the run action; keep freshness
  and the router-default-route scope visible without restating the workflow.
  The view has its own /quality address. Snapshot reads never start
  tests. Clicking the Latency title opens the /latency test page, without a
  separate plus button. Internet target selection remains available through the
  API and CLI; failure never silently switches the chosen target. Show the latest
  single HTTPS latency sample, with `<1 ms` for sub-millisecond values; keep
  DNS lookup simple and omit bandwidth tests. Overview's site grid contains only
  pinned sites, with none selected by default; Internet and DNS remain visible.
  Clicking the Latency title opens /latency to manage pins. The pin is a small
  button before the site mark; an unpinned pin is hidden until its row is hovered
  or the button is focused, while a coarse pointer keeps it visible. Only the pin
  toggles Overview visibility — the mark and name are not a control. It indicates
  saved state, and its accessible action names the site and whether it will be
  pinned or unpinned. Save immediately to router configuration, retain choices
  across sessions and restarts, and show a failed save explicitly. A pin change
  is serialized, with progress on its row. Pinning changes presentation without
  starting probes or changing measurements.
  Show all monitored sites in one table, divided into the catalog's category
  sections (Communication, Social, Entertainment, News, Shopping, Tools, then
  Custom). A category heading is a plain semibold line; rows sit indented beneath
  it. Each site occupies one row: pin slot, bare icon, name, 24 fixed squares for
  the router-local day's hours, and a right-aligned daily average. Omit the
  visible hour axis, router date/offset and scheduling copy, and per-row test
  controls. Each tooltip gives the exact date, hour range and result. Missing and
  future hours stay neutral, and a failed attempt uses the same neutral fill;
  green is below 100 ms, amber from 100 to 499 ms and red from 500 ms. A retest
  replaces the current hour rather than appending another sample or changing
  other hours. Automatic tests run at about :30; the page's Test all measures one
  site at a time in reading order and updates only the current hour. It names the
  progress position and draws the current hour's probe as a spinner that settles
  into its square; it does not scroll or highlight the active row. A site that
  fails or times out does not stop the rest. Retain history through daemon and
  router restarts without inventing measurements for downtime. The row's average
  is the mean of that day's successful samples, in the same colour bands, and is a
  dash when none succeeded. Prefer matching bundled theSVG marks, then favicons
  and bare initials; add no icon frame. The catalog cannot be added to, removed
  or edited here. Keep completed results visible while Test all is running. Say
  when a request to measure fails; reading the page never starts tests.
  `Up` and elapsed uptime
  lead Status above system health. Click uptime for a dialog with CPU, memory and root disk
  used/total values; omit the separate System card.
  Click system health for a dialog with DHCP, DNS and Gateway states, with links to their
  pages. Distinguish checking, unavailable, disabled, needs attention and OK;
  retain the visible fault alerts below the cards.
  Latency and Map have no information icons. Traffic flips to reveal its map
  on the back, without a Map details row;
  clicking the grayscale map links to the color map at /map. All three detail
  pages use the shared header, PageHeading, content area and footer, with Overview
  selected in navigation. Country colors distinguish geography,
  while download/upload retain their semantic colors.
  omit the inline direction legend and top-ten summary.
- Overview Status shows one public-network summary using IPv4's country flag,
  city and ISP. Its row opens a dialog showing IPv4, IPv6 when available, ASN and ASN
  Organization from the IPv4 lookup, in that order; label the organization “Org”.
- Keep download and upload limits beside their respective measurements; separate
  controls serve separate decisions and are not automatically redundant.
- Overview cumulative traffic uses Download then Upload labels, each with
  its byte unit. Preserve unavailable states rather than
  displaying zero when counting is disabled or a read fails.
- Group Overview live traffic and utilization separately from cumulative traffic,
  site probes. Align section starts across desktop cards while keeping
  each group's internal spacing suited to its content; Status remains one list.
- Keep common configuration fields, addresses, counters and useful evidence
  visible. Do not move them into disclosures solely to shorten a page.
- Reuse existing headings, Card, ListRow, StatusStrip, fields and Dialog. Do not
  invent a compact card, new density tier or special layout for each feature.
  Choose shared size variants: MetricValue sm for inline measurements, md for
  summaries and lg for diagnostic results; Card's sm/default spacing is separate
  from Heading's sm/md/lg emphasis. Unavailable latency is never a zero result.
- Disclose lengthy technical explanations and infrequently used advanced options.
  Keep prerequisites and consequences beside the action they affect.
- A page can pass review without a visible redesign. Preserve a coherent existing
  presentation when its content already supports the user's task.

## State and data contracts

Quality assessments run on demand from the /quality page reached through
the card heading or chart. The page always includes the reference transfer. A
full run replaces the snapshot; rerunning one category or service test reuses
the other evidence, merges the fresh result and recalculates every affected
score. While a rerun is in progress, keep the last scores visible, disable the
other rerun controls and say which test is updating. Measured scores do not
expire; results survive daemon and router restarts. The result view labels the
same reference score Excellent 90–100, Good 75–89, Fair 60–74 or Poor below 60;
the label is presentation, not another calculation. Not tested, cancelled,
timed-out and unscored states replace the number with a dash, and a running
assessment shows progress there instead; each carries one sentence saying what
happened and what to do next. Missing data is never shown as zero.

A speed test runs as one request that stays open until the measurement ends, so
its page reads the run's stage on a second request rather than waiting on a
spinner. The measurement is shown the way LibreSpeed's page shows its own: two
dials, download and upload, on a logarithmic 0–10 Gbps scale. The direction
being measured carries the rate it has reached, the other holds its last
reading, and both settle on the result, with latency and jitter above them. A
dial that has not measured shows a dash rather than a zero, and a service that
reports only a finished result says it is measuring instead of inventing numbers
it cannot see yet. The controls stay to one row — way out, test server, start —
and carry no explanatory copy; controls, start button and dials sit in one card,
the way the embedded LibreSpeed page is one panel.

LibreSpeed is the exception among the tools: its page frames LibreSpeed's own
document unchanged, because that page is the measurement, and OLR supplies only
the viewer credential and the frame around it. Every other speed test borrows
that page's dials for its own result rather than shipping its own charts.

World traffic combines IPs at the same remote coordinates into one map connection.
Line width represents that connection's combined upload and download relative
to the busiest one; per-IP rates stay in details. Arcs are one amber
hue, outside the map's country colours, and each connection has one neutral dot
that travels out and back, illustrating both directions without selecting a
direction based on traffic volume.

Ingress distinguishes saved repairs from applied settings. When other existing
errors prevent application, say the repair was saved, list the remaining service
names and problems, and explain that the running proxy stays unchanged until
the final repair applies automatically. Retain this warning after refresh.

World traffic uses two-second sampling and UI refresh. Smooth map transitions communicate
continuity between samples, not individual packets or exact connection lifetimes;
the details ranking and rates continue to show the latest observed snapshot.
World traffic ranks remote IPs over a rolling thirty-second observed window,
then locates only the top forty using cached ip.sb city lookups. Details show
city (or region) and country; arcs use only valid returned coordinates,
omitting IPs without coordinates. State that IP locations are approximate in the
details dialog, which also carries the sampling caveats and ip.sb attribution. The detailed
administrative map adds country, province/state and major-city names progressively
on zoom, stopping at regional detail because IP locations are approximate; it
sits in a bounded panel below the page heading, with no text overlay or close button.
Country fill colors distinguish geography and carry no traffic or status meaning.
Traffic has no GeoIP city, region or country labels. Map labels use bundled
geography and local fonts, without fetching an external basemap.
Show their share of observed traffic in the details dialog,
and retain unknown locations in that denominator. Never imply
zero activity is a failed read, or sampled conntrack
bytes equal whole-router totals. Details name short-connection and bypass
limitations, warm-up, counting-disabled and unavailable states.

Distinguish loading, read failure, unknown, empty, disabled and healthy states.
No response is not an empty rule set; saved configuration is not proof of a
running service or end-to-end connectivity. Use neutral pending/unknown styling,
an explicit error and a retry where useful. Name configuration counts as such.

Firewall monitoring retains semantic permission categories and adds incoming
source as a separate dimension. Use one vertical diagram with three visual
rows: external semantic categories at the top, one virtual OLR / Firewall node
in the middle, and LAN/trusted and local categories at the bottom. Both outer
rows show arrivals at OLR, not an external-to-LAN forwarding path. Each category
shows its name and packet count; position conveys source and color conveys the
Allowed/Blocked decision. Keep full source and decision text in tooltips, without
repeating them on each node. Omit explanatory paragraphs and the matching-conditions
disclosure below the diagram. Hide zero-traffic categories and
sources. Path widths use log(1 + packet count) weights normalized separately
for each side, with a 4px minimum so small categories remain visible. Retain exact
packet labels and byte totals and matching conditions in tooltips.
Keep unavailable counters distinct from a verified zero-traffic state.

Show QoS as saved configuration until enforcement exists.
Device details group stable identity and saved settings on the left, live
uplink, priority and live traffic on the right. Reuse Overview rate meters with
Download and Upload labels beside narrower bars, without arrows.
Total below Upload shows download then upload with the Overview arrows, added up
over the same retained history the title matrix draws, so a reader can check it
against the cells above it; it states no window of its own, because the matrix
beside the title already is that window's picture. Unavailable history shows
dashes. Services below Group lists matching configured services as icons that
open their webpages, with an equally sized add action that opens Ingress and
prefills the current device. Empty lists show only the add action.
Devices without a reserved name/address prefill their observed host instead.
A device whose source of a name is gone keeps the name it last announced: rows
and lists show it as the device's name, and its page says under the title when
that name was last heard. A name field offers it as the last known name rather
than as what the device calls itself, and nothing is saved as the operator's
name unless the operator saves it.
A device that dialed in draws the same row, matched by its tunnel address, and
its add action prefills that address rather than a device reference.
Group is a tag that opens its edit dialog.
Compact Uplink and Priority segmented selectors lead the right column, in that
order. Uplink selections retain network-change confirmation and apply feedback.
On desktop, pair Vendor with Uplink, MAC with Priority, IP with Download,
Group with Upload and Services with Total in shared rows. Place read errors and
traffic availability messages after those rows so they do not shift the
corresponding values.
The title's activity matrix shows one day per column, newest
on the right, and six four-hour periods from top to bottom. The displayed date
range adapts to the available width, with fixed square cells; dates without
retained history remain explicitly missing data. Leave future periods
blank and show download/upload totals immediately on hover or focus. Format the
time as `10-14 4:00-8:00`, followed by traffic only; omit sampled/progress wording.
A device page reports identity and settings, and leaves presence to the lists and
the map. It draws no Last heard and no Way in row, and the hardware address is a
fact row rather than a chip beside the title. Lists, the network map and the
device dialog still report when a device was last heard, never whether it is
online: a DHCP lease outlives presence by hours and the kernel keeps a stale
neighbour entry looking complete, so Online would be a claim this box cannot
support; Last heard is the fact it can. The one exception is a device that dialed
in: every configuration this router renders carries a keepalive, so three minutes
without a handshake really is a broken tunnel, and that page alone says
Connected; the map draws the same fact as a dot rather than a word.
Its Last heard (the handshake) and Way in (the tunnel interface and subnet) stay,
because the tunnel is the only thing that can answer them, and its configured
name is a fact row in the same place the device page puts its MAC.
That page's two actions, handing over a new setup file and editing the device,
are one pair in the lower right corner, in the space the counters leave beside
the taller facts column; no rule separates them from the facts they belong to.
Name the file the button hands over, rather than a category of configuration:
the file exists only after the click, and why it cannot be downloaded again
belongs to its tooltip and to the confirm dialog that decides the loss.
Speed bars open download/upload limit dialogs; keep the
configuration-only caveat in those dialogs and save feedback, without a status row.
Omit Notes and the QoS heading.
Device activity sits at the right of the title bar, without visible
explanatory text. Only the current period breathes with live combined download/upload
speed: faster traffic means faster breathing; idle or unavailable rates stay steady.
Rate updates smoothly change playback speed without resetting the breath.
On becoming idle, complete the current breath before settling at full brightness.
Historical colors continue to represent accumulated traffic.
The lowest nonzero tier (below 10 MiB combined upload/download per four-hour
period) uses a stronger neutral gray than zero/missing data; green starts at
10 MiB. Four green levels use the device's retained 90-day four-hour totals at or
above 10 MiB, split at 25%, 50% and 75% of the greater of the maximum total
and 1 GiB, independently of
screen width. These divide the byte range, not the number of cells.
Values equal to a boundary use the lighter tier; identical totals share a color,
and a flat or single-sample history below 1 GiB follows the same minimum reference. Different devices'
greens represent their own relative traffic, not comparable byte quantities.
This distinguishes low traffic without implying whether a person is
actively using the device.
Immediate four-hour hover/focus hints show sampled download and upload
or explicitly report missing/unavailable data. Partial samples are not exact
four-hour totals. History belongs to the client, not to an address: every address
it used, including one it has since dropped, stays part of its matrix and of the
total beneath it. A peer, which has no hardware address, is the one card whose
history is looked up by address, and the one kind of traffic kept without a
client behind it: nothing else is stored under an address, so no total counts
traffic that no screen could show.
It begins accumulating after daemon startup. Sampling runs every ten seconds and
the matrix refreshes every minute; the first reading establishes a baseline.
History retains at most 90 days and is restored from
SQLite's `activity` table in `/var/lib/open-linux-router/runtime.db` after
restarts/upgrades. Silently dropped history is not an error state: when the
table's address/bucket ceiling is reached the oldest periods go and the matrix
simply begins later, so an operator sees a shorter left edge rather than an
empty current column. Transactional
checkpoints run every minute and on normal shutdown; an abrupt power loss may
lose the latest minute. It is not reconstructed from cumulative counters. Changing
device addresses can hide samples associated with its previous addresses.

Device Uplink offers Default (inherit interface/system routing) and named
exits only. Existing explicit router-connection overrides remain accurately
displayed until the user selects Default or a named exit.
Use label/value pairs within each device-detail half. Keep units with their inputs and chevrons beside selected values. Lead identity rows with Vendor. Edit addresses and their fixed toggle
in a dialog opened by clicking the IP. Show a Fixed tag beside reserved addresses;
keep the hover hint limited to DHCP lease information and omit the hostname field.
Preserve diagnostic scope: router-originated tests do not measure a downstream device's path.
The Inspection switch starts and stops interception directly, without confirmation.
Explain credentials, certificate trust and cleanup in the setup guide. It is the
device's setting, not a session: it stays where the operator left it across
reloads, daemon restarts and upgrades, so no copy may promise a countdown or a
temporary window. When the proxy cannot start, the card keeps the switch on, says
why beside it and offers Retry instead of showing the switch off.
Device Inspection is one card, and its title names the switch in the title bar:
no second "Inspection" label sits beside it. The setup gear remains available
whether the switch is on or off; QR code, guide link and global retention controls
belong in that dialog. The panel remains visible with Inspection off or another
device active: Activity shows retained samples and Requests shows saved history.
Current DNS queries appear while this device is active. Requests defaults to
Saved; its Live choice opens the embedded mitmproxy viewer while active, without
adding another card or duplicating its request list. Certificate installation,
trust and cleanup belong in the public five-step guide, with the certificate
at step two.
Saved requests include complete native flows after a captured request completes
or fails. Keep the unfinished-request boundary visible: in-flight content may be
lost if the proxy stops. Expose `history_error` as an archiving failure, rather
than implying a live response proves it was saved. Saved rows open request and
response headers/body previews; identify binary content and truncated previews,
and offer the full `.mitm` download. Search and pagination work independently of
current inspection state. Clear saved requests requires an explicit confirmation
that names this device and explains that activity and other devices are kept.
Activity offers 4-hour, 24-hour, 7-day and 30-day ranges; the total reads "in
selected range". Retained samples survive stopping inspection and control-daemon
restarts. Sampling occurs while inspection is on; downtime is not reconstructed.
Saved-request retention defaults to 30 days and 5 GiB total across all devices
on the router; sampled site activity has its own 30-day/64-MiB bound.
Always make the shared router scope explicit beside the storage limit and usage;
never imply a per-device allowance. Global settings accept 1–365 days and
1 MiB–1 TiB. Lowering limits can remove older saved requests immediately.
Sites are named by
registrable domain under the public suffix list, so a multi-label suffix such as
`com.cn` is never shown as a site.
The DNS section lists Time, Record, Type and right-aligned Answer values without
a visible header row and without a switch of its own: the panel's switch already
opened it. An accessible search icon in the section heading reveals a compact
filter that opens to the left of itself; closing clears the filter. The panel
keeps its title bar fixed and expands content downward under one divider below
that bar; its sections are separated by headings and spacing rather than by
a rule each, and the DNS rows carry no rules. The switch keeps its place at the
trailing edge as content changes.
WireGuard devices keep DNS as its own card with its own Inspection switch,
because their tunnel traffic cannot be inspected; there the card starts closed.
Do not promise encrypted DNS blocking beyond the supported protocol and port.

Use download then upload consistently. Device names may supplement an address
only when the address uniquely identifies a device. Keep long domains, IPv6
addresses and provider errors readable rather than silently truncating them.

## Disclosure and hierarchy

Access groups technology tiles under “Networking”, “Add a way out” and “Add a way
in”. Keep WireGuard, Tailscale, NetBird, ZeroTier, Headscale, SSH, SOCKS5,
Gateway and HE Tunnel Broker in the directory. Shadowsocks, Trojan, VLESS, VMess,
Hysteria, TUIC, OpenVPN and IPsec are outside the product scope. Do not label the
directory as proof of legal compliance or describe removed technologies as a
statutory list of prohibited products.
A technology can appear in both directions; its tile goes straight to the
configuration owner for that direction, and a technology OLR does not configure
opens its project documentation instead. Remove System default and purpose
filters from this page. Use app-style icons above names without enclosing cards.
Use project logos for branded technologies, including frp's official icon.
SOCKS5 and Port forwarding use reviewed original app artwork with simple flat
functional shapes and no lettering; the name remains below each icon. Other
unbranded entries retain a capital initial on a tinted tile. The approved HTTP
and HTTPS artwork does not introduce directory entries or replace Caddy's mark.
Port forwarding is a forwarding rule rather than a remote-access technology,
so it is not a tile
here; it lives under Advanced with the firewall whose ruleset it shares.
Functions and state appear on hover/focus and in the tile's accessible name.
External technologies remain explicitly “Not integrated”; broader ecosystem uses
are not promises of OLR configuration support. Existing service states remain
authoritative.

Tunnel Broker keeps provider details together in one saved assignment, with
activation and LAN assignment as separate decisions. Saving defaults to off.
Show five sections on one page with sticky step navigation; omit the repeated
page introduction and back-link heading. Keep endpoint requirements and change
consequences visible; disclose server comparisons and technical explanations.

Overview group headers open the group's detail page directly on desktop and
mobile. The unnamed device bucket retains its in-place disclosure.
Keep service icons beside the compact activity column, leaving the remaining
row width for the device name.
Group details lead with parent, device counts and subgroup links beside bulk
uplink and priority controls in the device summary layout. Keep current-member
scope, replacement behavior and the priority enforcement caveat beside the
explicit apply actions; selected values are batch choices, not observed shared
settings. List member devices below the summary.

Gateway's landing page is a directory of choices, not a status report. It has
five groups, each naming a part of the router and each holding the ways of
building that part: Internet (DHCP, PPPoE, a static address), Interfaces &
routing (the interfaces, the route table, external rules), DHCP (dnsmasq, and
Kea when it lands), DNS (unbound, and the resolvers OLR will offer beside it)
and Firewall (nftables, port forwarding, external filtering). The order runs
from the two parts an operator sets up once to the three whose shipped defaults
most installs keep.

A tile names a choice, never a module and never a mechanism: the word on it is
the one an operator would use to pick it, and the sentence that explains it is
the tooltip. A choice OLR cannot configure yet keeps its tile, muted and inert,
so a group shows what it will contain instead of growing a tile at a time; it is
not an external setup and the tooltip says so plainly. A tile that is a link
opens the one page that configures the choice, where the switches and the live
answer both live — the directory itself holds no switch, no status strip and no
draft, and no tile opens a dialog.

Open newly navigated pages at the top so their title and primary information are
visible. Preserve section targeting for hash links and scroll for query-only changes.

Default to two content levels: the main information and necessary context.
A third level needs a specific reason, such as a consequential safety condition.
An explanation is optional, including under a page title. Do not fill every
available subtitle or helper slot just because the component supports it.

Entry pages help selection. Operation pages explain execution. Result pages
explain outcomes. Do not put protocol implementation details, setup instructions
or descriptions of the next screen on every directory card.

Keep information that affects safety or interpretation visible at the decision
point: disruptive network changes, authentication consequences, unavailable
data, stale measurements and test scope must not disappear as part of simplification.
Secondary diagnostics and raw technical evidence can be disclosed separately
with clear labels.

## Languages and shared terminology

The UI supports English and Simplified Chinese. Keep English source messages
and their Chinese translations together through `t()` and
`web/src/lib/locales/zh-CN.ts`; reuse messages rather than inventing synonyms.
Use 网关 for Gateway, 出口 for Uplink / Way out, 接入 for Access, 地址池 for
Address ranges and 地址预留 for Reserved addresses. Translate status and actions
without changing protocol identifiers, addresses, commands, user names or API data.
Keep technical backend errors verbatim so operators can diagnose them.

Language selection is browser-local, persists across reloads and synchronizes
between tabs. On first visit, use Chinese for a Chinese browser preference and
English otherwise; unsupported languages fall back to English. Switching languages
updates the document language and visible controls without resetting forms or
restarting queries. Keep the switch available before login and initial setup.
New messages without a translation fall back to English. Translate complete
sentences with named placeholders when values are embedded; do not assemble new
sentences from translated fragments. Keep state decisions independent of copy.

## Naming and writing

Use familiar tool names and concise task labels. A diagnostic that is a named
upstream project takes that project's full name, which is why the tools
directory reads Speedtest by Ookla, Cloudflare Speed Test, Fast.com, LibreSpeed
and iperf3 rather than one "speed test" and a service picker: the service is the
distinction that helps an operator select one, and each measures a different
path. A shortened name would make two entries ambiguous — "Cloudflare" alone
also names the tunnel on Access — so the tile carries the same name as its
page. Preserve established names and their own spelling, such as Ping,
NatTypeTester, Traceroute and iperf3 (all lowercase). Do not invent broad
category labels for a single item.

Name a state for what it means for the user, not for how it is carried out. The
IPv6 DNS card on Gateway reads IPv6 disabled, not AAAA filtered: dropping AAAA
records is one way to reach that state, and the mechanism only answers the
question for a reader who already knows the record type. The setting's own page
may keep the mechanism's name, because there the operator is choosing it; a
status that reports the outcome names the outcome. The Forwarding card follows
the same rule: devices reach the internet or they do not, and which sysctl was
written is not something the operator can see or act on. The card's title stays
the module's own word, because the switch it reports is the one in the Gateway
routing card at interfaces#forwarding.

Avoid repeating the title in a description, stating obvious click behavior or
adding a generic next-step sentence. Use one short sentence when an explanation
is necessary. Keep essential text readable; reducing font size is not a remedy
for excessive content.

Keep the footer to the product copyright, Credits, version and the icon-only GitHub,
theme and language controls. On desktop, bottom-align the visible content with 12px
bottom padding. The row stays on one line, so phones keep the year alone in the
copyright instead of the product name the header already carries. Describe the
current and next theme, and the language in use and the one a click switches to, in
their accessible labels and tooltips, and name GitHub in the accessible label and
tooltip of its mark.

## Capability and richness

Device icon selection supports the same theSVG library as site logos. Choosing
a library logo changes only the picture, preserving the device category. Keep
automatic detection available and show a clear empty result for unmatched searches.

Show the real capability set together with consistent visual weight. Keep entries
compact enough to scan across the set. Richness comes from recognizable, distinct
capabilities, useful data and dependable interactions. Do not inflate it with
oversized panels, redundant labels, decorative badges or unavailable features.

Use a consistent ordering. Add grouping when the collection is large enough that
groups reduce search effort; add search when scanning the collection becomes
burdensome. Five tools need neither filters nor one category per card.

## Advanced directory reference

Advanced's goals are quick selection, a current answer and a calm presentation.
Its contract is:

- Three groups with charters: System (the machine and what may reach it),
  Services (background work for the network), Diagnostics (measurements).
  Place a feature by its charter, not by which module happens to own it.
- One tile per destination. Most tiles wear a capital initial on a 64px tinted
  tile: the tiles of a group share one hue, and the letter carries identity.
- A tile that names an upstream tool or service takes that name and its mark
  instead — filling the tile when the mark carries its own backing (NextTrace)
  and on white when it is a plain glyph (the three internet speed tests). A
  tile that is a protocol or OLR's own measurement keeps its initial. One
  destination measures one thing, so the three internet speed tests are three
  tiles rather than one tile and a service picker.
- The name below the tile is the whole name, nothing printed under it, and it
  wraps to a second line rather than being cut off at an ellipsis: two names in
  one row that both read "Cloudflare…" would defeat the tile. Beyond two lines
  the name is too long for the directory; the page may carry a longer title
  than the tile (Admin access opens Administrator access).
- Function and state appear on hover/focus and in the tile's accessible name.
- The whole tile is a keyboard-accessible link; no dialog opens on click.
- There is no intermediate "Tools" page: a diagnostic tile opens the tool.
- Keep explanations, requirements and results on the individual pages.

## Review before shipping

- Can the primary information be identified without reading every paragraph?
- Does each supporting line change a decision or explain a necessary constraint?
- Is any information repeated across title, description, badge and action?
- Are instructions shown at the step where they are used?
- Does simplifying preserve warnings, measurement scope and unavailable states?
- Can users scan the capability set on desktop and find entries on a phone?
- Do imagery, whitespace and type reinforce the same information hierarchy?

Update both guides when changing a shared presentation contract.
