# `firewall` module — nftables rules at the boundary

Status: **built.** Section references are to `design.md` unless prefixed.

The automatically generated default is:

> **Nothing outside your networks may start a connection to this router, or to
> anything behind it, unless something here asked for it.**

## 1. Why it exists now

Until IPv6, the networks behind an olr box were unreachable from outside by
accident: their addresses were private, and the egress masquerade gave nothing
outside a way in. That is not a firewall, and it stops being true the moment a
device has a global IPv6 address — every printer, NAS and camera on the network
becomes directly addressable from the internet. The router itself was never
covered: the web UI listens on `0.0.0.0:8080` and answers on the uplink too.

So filtering stopped being optional. What it did not need to become is
complicated.

## 2. Automatic defaults and native rules

```json
"firewall": { "enabled": true }
```

That is the stored configuration, all of it. **Off by default** — installing olr
changes nothing until somebody says so (§7) — and an ordinary switch in both
directions. `olr firewall enable|disable|show|status`, `/api/firewall/…`, and a
Firewall section in the web UI.

The Firewall landing page shows measured traffic by semantic category **and
incoming source**. One vertical Sankey has exactly three visual rows: external
semantic categories at the top, one virtual **OLR / Firewall** node in the
middle, and LAN/trusted and router-local semantic categories at the bottom.
Both outer rows are incoming traffic: external ribbons run down to OLR and
trusted/local ribbons run up to OLR. These are one flat set of sourced decision
counters arranged by origin, not sequential processing stages or traffic
forwarded from the upper row to the lower row. Category labels retain incoming
source and explicit Allowed/Blocked decisions; no separate source or decision
rows are added. Only categories with nonzero packet counts appear. Every bar
and ribbon shares one packet scale; the virtual node spans the sum of all
arrivals, with separate portions for each side. The chart fits the
available width when labels permit; otherwise it scrolls from the first node,
without automatically centering and hiding the leading categories. The matching-condition
disclosure includes packet and byte totals and distinguishes traffic to the
router from forwarded traffic.

Automatic categories retain established/related, inside-initiated, loopback,
ICMP, ICMPv6, DHCP, DHCPv6, service openings, DNAT, other forwarding, IPTV and
input/forward drops. Each decision rule is partitioned into disjoint sources
(outside, inside excluding loopback, and loopback) and counts packets/bytes
immediately before its original verdict. Filtering order and permissions are
preserved. Attribution uses the **first matching rule**: for example, an inside
ping is normally inside-initiated access, and later packets of an opened service
are established traffic. These are decision counts, not per-service connection
totals. Input and forward counters for the same source/category/verdict may be
summed because a packet traverses only one of those hooks.

`/api/firewall/status` supplies `traffic` rows and `traffic_known`, alongside the
legacy aggregate `blocked_input`/`blocked_forward`. Counts are cumulative since
the rules were last written and reset on table replacement. Old automatic tables
are replaced on the next apply/startup to install counters; incomplete counters
are unavailable, not known zero. Native, disabled or unverified rules are not
inferred from automatic defaults. The switch and status warnings stay here.

The `/firewall/rules` subpage reads the real ruleset using `nft -j list ruleset`. Tables, chains,
hooks, priorities, policies, rule order, handles, expressions, sets and counters
come from nftables itself. Other tables are visible but read-only.

Selecting **Edit olr_filter** copies the current `inet olr_filter` table (or the
saved table when disabled) into a native JSON editor. Individual rules can be
added, edited, removed and reordered; the full document editor handles chains,
sets and complex expressions. Draft edits do not change the kernel.

**Validate and preview** runs `nft -j -c -f -`. Saving requires an explicit
confirmation because arbitrary input rules can lock the operator out and there
is no automatic rollback. The saved `firewall.ruleset` is nft's native JSON
listing, not an OLR policy language. Application converts listing entries to
native `add` commands and deletes/recreates only `inet olr_filter` in one
transaction. Kernel handles are stripped and counters start at zero. No shell
is involved, and `flush ruleset` or commands addressing other tables are refused.

Once saved, the native table replaces automatic generation. Service changes
do not regenerate it or automatically repair externally changed rules. Startup
restores it; explicit apply restores the saved table. The UI uses `If-Match`
against the saved configuration and observed table to reject stale edits;
counter movement is excluded. Direct external `nft` changes are not persisted
until the operator copies and saves the live table in the editor.

The automatic mode remains the default for existing installations. Clearing
`ruleset` through the config API returns to automatic generation; this
deliberately replaces user rules. The inside/opening behavior below describes
automatic mode only. Native mode is governed by its actual chains and policies.

### 2.1 Inside and outside

**Inside** is every member interface of a `link` network, plus the remote-access
WireGuard interface when it is enabled — a dialled-in phone is meant to be on
the network. **Outside is everything else**, including any interface that
appears later: a second NIC, a PPPoE session, a USB modem. Defined by what is
trusted rather than by what is not, so a misclassification fails closed.

Enabling with no networks at all is refused: nothing would be inside, and the
ruleset would drop the operator's own connection.

### 2.2 Openings are derived, never typed

What stays reachable from outside comes from the objects that need it:

| Opening | From |
|---|---|
| udp WireGuard port | `remote.wireguard`, when enabled |
| tcp SOCKS5 port | `remote.socks5`, only with `listen: internet` |
| tcp 80, tcp 443, udp 443 | `ingress`, when enabled (443/udp is HTTP/3) |
| IP protocol 41, **from the broker only** | `dial`'s IPv6 tunnel (`uplink.ipv6`), when set |
| a port forward's traffic | conntrack's DNAT status — the forward *is* the permission |

Removing the object closes its port on the next apply. There is no rule anybody
can forget to take out. olrd re-applies the firewall whenever `dial`, `link`,
`remote` or `ingress` announces a change (`internal/daemon/firewall.go`).

The tunnel's opening is the one that is not a port. 6in4 is its own IP
protocol, and it is limited to the broker's address because protocol 41 from
anywhere would let anybody inject IPv6 into the tunnel. It is needed even though
conntrack lets replies in: the tracking entry for protocol 41 lapses after ten
minutes of silence, and the first inbound IPv6 connection to a quiet tunnel
would be dropped. The tunnel device itself is outside, like every interface that
appears later, so IPv6 arriving through it gets the same stance as IPv4 arriving
on the uplink.

## 3. The ruleset

One table, `inet olr_filter`, so IPv4 and IPv6 are one policy. Two chains:

```
input   (to this router)            forward (through it)
  established,related  accept         mss clamp             (no verdict)
  from lo              accept         established,related  accept
  from <inside>        accept         from <inside>         accept
  icmp, icmpv6         accept         to not <inside>       accept
  dhcp, dhcpv6 client  accept         port forwards (dnat)  accept
  each opening         accept         icmp, icmpv6          accept
  drop (counted)                      drop (counted)
```

The choices that are not obvious:

- **ICMP is not optional.** Neighbour discovery and router advertisements are
  ICMPv6, and dropping "packet too big" breaks path MTU discovery in the way that
  reads as "ping works, pages hang". The accepted types are RFC 4890's, and the
  same errors are let through the forward chain in both families — an IPv4
  "fragmentation needed" for a forwarded connection is not always `related`
  conntrack state, and without the rule it is dropped.
- **The forward chain only guards traffic headed into our networks** (`to not
  <inside> accept`). Forwarding between interfaces olr does not own — Docker,
  libvirt — is not this module's to judge, so a box running containers keeps
  working. The input chain has no such carve-out: the router itself is
  protected from every non-inside interface, which includes a container bridge
  reaching a service on the host.
- **Explicit counted drop under an `accept` policy**, not `policy drop`. The
  verdict is identical; named counters retain aggregate blocked totals, while
  every sourced rule has an anonymous packet/byte counter for traffic reporting.
- **Other tables cannot loosen this.** In nftables a `drop` in any base chain is
  final and an `accept` is not, so rules added by hand, by wg-quick or by Docker
  can narrow what this table allows but never widen it.
- **Atomic.** Programmed over netlink in-process like `olr_nat` (no `nft`
  binary): delete-and-recreate in one batch, so there is never a half-built
  ruleset. Each rule carries its canonical line as its nft comment, which is
  also the diff basis for drift.

### 3.1 Path MTU and MSS clamping

The ruleset also clamps the TCP maximum segment size when the way out is
narrower than the networks behind it.

A client on an ordinary 1500-byte LAN advertises MSS 1460 in every SYN. When
the uplink is PPPoE at 1492, the first full-size packet of every new connection
is discarded by the first router on the way, and the endpoints have to wait for
an ICMP "fragmentation needed" before they retry at 1452. That is a visible
stall on the first packet of every connection; where the ICMP is filtered
anywhere along the path, the connection can hang instead of retrying.

The clamp is the fix, and changing the interface MTU is not: 1500 is true of
the LAN. Lowering it would fragment or reset local traffic for a limit that
exists only on the way out, and would claim the LAN is smaller than it is. The
MSS option is the one place a host can be told the path's limit without changing
the link.

**Detection is not configuration.** The module looks up the route the box's own
traffic takes, reads the kernel's cached path MTU for that destination, and
when there is none sends one DF-set echo at the uplink's MTU. The first router
with a smaller link answers "fragmentation needed" and the kernel caches the
number, which the next route lookup returns. The finding is kept for a few
minutes, so a status page or an apply is not a probe. `olr firewall status`
reports it with its evidence — `path MTU: 1492 via ens18 (probed)` — and `link`
lists every interface's own MTU beside it.

Rules are rendered **only when a path is narrower than a trusted interface**:

```
nft forward oifname <uplink> tcp flags syn tcp option maxseg size set <path mtu - 40>
nft forward oifname <inside> tcp flags syn tcp option maxseg size set <path mtu - 40>
nft forward oifname olr-6in4 tcp flags syn tcp option maxseg size set <tunnel mtu - 40>
```

40 is the IPv4 and TCP header the MSS option does not include. They sit before
every verdict rule because they carry no verdict: they rewrite the option and
evaluation continues, so a rule placed after any `accept` would never see a SYN.
Matching the SYN bit covers both the SYN and the SYN-ACK, on the way out and on
the way back into each trusted interface. The 6in4 device is a second path, and
traffic into it and back into the networks is clamped to the narrower of the
two.

Turning the firewall off, or saving a native ruleset, removes the clamp. That
is reported as a problem against `mss_clamp` in `olr firewall status`, because
it is the state that pays the per-connection cost described above.

## 4. Lockout

Switching the firewall on while connected from outside — the olr box sits
behind an existing home router, and the operator is on that router's network —
blocks their next connection to the UI. The established one survives; the page
stops working a moment later and reads as the box having died.

design.md §5.5's guard, which would revert an unconfirmed change, is not built.
What stands in for it: the plan names the interface the request arrived on, and
when that interface is outside it carries a warning and `impact: disruptive`.
The UI stops and asks; the API returns 409 without `confirm=true`. The CLI over
the unix socket is never affected and never warned.

A private-source exception ("allow the UI from RFC 1918 addresses on the
uplink") was considered and left out to keep the stance simple. It is one rule
if it turns out to be wanted.

### 4.1 Web flow view

The complete rule list is authoritative. The optional flow view projects only
rules composed of recognized native matches, counters and a terminal
accept/drop/reject. Each equal-width ribbon links to its actual rule handle in
the list. Unsupported expressions, jumps, logging, NAT and non-terminal rules
remain fully visible in the list and are counted as not representable. No
business categories are assigned. These ribbons describe individual rule
matches, not simulated packet paths or final verdicts across all base chains.

## 5. Limits, stated

- **Native editing needs `nft` and CAP_NET_ADMIN.** The packaged dependency
  already includes nftables; manual binary installations must provide it.
- **Native edits take ownership of the table.** Enabling remote access or
  publishing a service afterwards requires an explicit native rule change.

- **Boot window.** The table is programmed when olrd starts, first of all the
  restores. Between the kernel bringing interfaces up and olrd starting, the
  box is unfiltered.
- **No per-device IPv6 openings.** Letting the internet reach one device's port
  over IPv6 is a filtering permission rather than a translation, and there is no
  object for it yet (docs/port-forwarding.md §7).
- **Drift detection reads comments**, with the gap `olr_nat` documents: a rule
  whose expressions were hand-edited under an intact comment is not reported. It
  is corrected on the next apply regardless.
- **Unverified on a real box** at the time of writing. The kernel half is
  typechecked for Linux and has not yet run against a live nftables.
