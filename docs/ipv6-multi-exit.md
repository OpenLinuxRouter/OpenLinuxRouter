# IPv6 multi-exit: what is possible, what is not, and what not to build

Status: **research.** Nothing described here is built, and §7 argues most of it
should stay that way. It is written down because the question is reasonable, the
answer is not obvious, and re-deriving it costs an afternoon of reading
specifications that mostly say no.

The question: a network advertises two IPv6 prefixes — an ISP's
provider-aggregated prefix and a tunnel broker's routed prefix — and one device
on it should send *some* destinations out of the first and the rest out of the
second. Can this router be told to do that?

Short answer: the router can choose an exit **by destination** today. It cannot
choose a **source address** by destination, and the second one is what the
request actually needs. Widening the first to a device is a small gap. Closing
the second is not a feature; it is a different design.

---

## 1. What `gateway` does today

`internal/gateway` renders an RPDB mark plus one route table per exit, and a
classifier that stamps the mark. The relevant facts, because each one is a wall
this discussion hits:

- An exit has a per-exit IPv6 answer: `via`, `block` (the default) or `direct`
  (`config.go`, `IPv6Mode`). `via` installs a rule and a default route for v6 as
  well as v4; `block` installs an `unreachable` default; `direct` installs
  neither (the §5.4 leak, available and never default).
- `Exit.Carries` is per form: an `interface` exit carries both families, a
  `next_hop` exit carries only the family of its own address. There is no such
  thing as reaching a v6 destination through a v4 gateway.
- The classifier runs `restore → destination → device → source → account →
  unpoliced` (`render.go`). Destination rules come first and longest-prefix
  first; the device and source rules only fire when our mark bits are still
  zero, which is how a destination override wins over both.
- A **destination** rule is scoped to a source *network* — `SourceRule` is
  interface plus prefix, with no MAC. A **device** rule is one exit per MAC.
  So "this device, these destinations" is not a shape the schema has.
- IPv6 forwarding is a separate switch (`gateway.IPv6Forwarding`), off means olr
  does not touch the sysctl, and turning it on drops every RA-learned default
  route on an interface whose `accept_ra` is not 2.
- Port forwarding has no v6 form, deliberately: `docs/port-forwarding.md` §7.
  A v6 "forward" would be a filtering decision, and olr has no filtering policy.

## 2. Two mappings, and the one that does not exist

There are three pairwise relationships here, and only two of them are routing:

| Map | Who decides | Router can do it |
|---|---|---|
| destination prefix → exit | the operator | yes — mark + table |
| source prefix → exit | the operator | yes — `ip -6 rule from`; olr does not emit it |
| destination → source address | the **host**, RFC 6724 | no |

The third one is the request. "Some destinations go out the tunnel" is not a
routing statement once the two exits carry different prefixes: what it really
asks is that the packet leaving by the tunnel *carry the tunnel's prefix as its
source*, so the reply comes back the same way. That is source address selection,
and IPv6 gives it to the host.

Nothing in RA or DHCPv6 carries "for this destination, use this source". The
network's only lever is to make the source and the path agree, and it has
exactly two ways to do that: deprecate a prefix globally (not per destination),
or rewrite the source.

## 3. Translation exists, and is Experimental

Rewriting the source between two prefixes has a name: NPTv6, RFC 6296, June
2011, and its status is **Experimental** — never advanced in fourteen years.
The mechanism is a 1:1 prefix mapping with no port translation, designed to be
checksum-neutral so the transport checksum need not be touched.

Two details matter more than the RFC text:

- **Linux does not implement the stateless algorithm.** `snat ip6 prefix to
  <prefix>/<bits>` lives in a `nat` chain, so it is conntrack-backed prefix
  SNAT: the first packet of a flow establishes the binding, later packets reuse
  it, and checksums are recomputed rather than kept neutral. Behaviourally close
  to NPTv6; structurally stateful.
- **Therefore the shadow address exists only for flows.** An unsolicited
  inbound connection to the translated address has no conntrack entry to
  reverse, so it needs an explicit DNAT — and olr's v6 forwards are exactly the
  thing that does not translate. A translated exit and the inbound story would
  have to be designed together.

The RFC's own multihoming model (§2.4) is **source-driven**: the host picks a
prefix, the matching translator picks the uplink. What this discussion kept
arriving at — pick the exit by destination, then rewrite the source to match —
is not a deployment the RFC describes. It is a composition of allowed
mechanisms that nobody blessed.

## 4. Nothing on the wire changes

The strongest thing that can be said for all of this: no packet format changes,
no extension header, no RA option, no DHCPv6 option, no port. The peer cannot
tell. The invention, to the extent there is one, is entirely inside a local
policy engine — where inventions are allowed, and where they are only visible to
the operator.

What is being violated is not a protocol but a layering decision: v4's NAT put
the identity choice in the router, and v6 deliberately took it away. Rebuilding
that control surface with prefix translation is not inventing a protocol; it is
undoing a design decision, locally.

The self-check: if the answer to "what address does this device have" needs the
qualifier "depends on the destination", the network has left IPv6's model. That
is not a bug, but it is the tax: every later question — why does this peer see
the wrong address, why does this service never connect, why does accounting show
half the traffic — has to be re-answered with that qualifier in hand.

## 5. Three shapes, three sets of costs

| Shape | Translation | Destination controllable | Symmetric | Inbound | Client cooperation |
|---|---|---|---|---|---|
| Destination mark → tunnel exit, source stays ISP | none | yes (per network today) | **no** — half the RTT win | fine | none |
| One ISP prefix + per-exit prefix translation | NPTv6 | yes | yes | needs DNAT | none |
| Two prefixes on the LAN | none | **no** — RFC 6724 decides | yes | yes | the host must be configured |

The first is the only one olr can do today, and the only change it needs is
device scoping on destination rules. It is also the weakest: outbound goes the
tunnel's way, the reply comes back the ISP's way, so a latency win is halved and
a source-filtered destination still sees the ISP prefix.

The second is the one that fully answers the request and the one that costs the
most.

The third is the most faithful to v6 and the least controllable: advertising
both prefixes gives every SLAAC host an address under each, and which one it
sends from is its own decision. `internal/dhcp` cannot even advertise two today
— `raRange` derives the prefix from the interface through dnsmasq's
`constructor:`, one prefix per range, and the stateful range is `::100–::1ff`
from that one prefix. Adding a second range is small; the control problem is
not.

## 6. The two prefixes are not symmetrical

Which of the three shapes is reasonable depends entirely on where the second
prefix comes from, and this is the part that is easy to get backwards.

**Two ISP prefixes.** Both uplinks apply ingress filtering. A packet leaving
uplink A must carry A's prefix or it is dropped, so the source and the path are
forced to agree, and any destination-driven split needs translation on at least
one side. Not worth building.

**An ISP prefix and a tunnel broker prefix.** The tunnel's /48 is *routed* to
the box, not filtered by source, so:

- the hard requirement is one-directional: a source inside the broker prefix
  must leave by the tunnel, or the reply cannot find its way back;
- a packet sourced from the ISP prefix *may* leave by the tunnel and come back
  natively. Asymmetric, functional, and the reason the first shape in §5 is
  enough for reachability;
- for latency, or for destinations that filter by source prefix, the broker
  prefix has to be the source — which is the second shape, and the whole cost
  of §3.

**Two tunnels.** Both sides are paths without source filtering, so the
destination split works with no translation at all, and the only missing piece
is per-device scoping.

## 7. Decision

Not building destination-driven prefix translation now. In order of weight:

1. It is a deployment pattern with no precedent: nothing to interoperate with,
   no second implementation to compare against, and no field experience to say
   which of its failure modes show up first.
2. It gives one device two identities at the same time, chosen per flow. Every
   support question about that device becomes conditional on a rule the person
   asking cannot see.
3. The honest cases are covered by the cheap half — device-scoped destination
   rules for tunnels, and a `from`-prefix rule for the one hard requirement —
   while the case that needs full translation (two ISP prefixes) is the one
   where the cost is highest and the benefit thinnest.

What stays allowed, unchanged by this note:

- **device-scoped destination rules** — a destination rule that names a MAC, so
  one device's traffic can take another path without a VLAN;
- **source-prefix rules** — `ip -6 rule from <prefix> lookup <table>` for an
  exit that is tied to a prefix, which is the only way the broker-prefix hard
  requirement can be met. It has to live inside olr: a hand-rolled version that
  selects a table carrying a default route is exactly what §6 of
  `docs/gateway.md` reports as a foreign rule, and it blocks apply.

If translation is ever built, the boundaries this discussion settled on:

- it is an **exit property**, never a module default, so the default path can
  never be translated by accident;
- **1:1 prefix mapping only** — never a pool, never a single shared address,
  because that is the difference between a device with a second identity and a
  whole network collapsed into one;
- the UI names the shadow address on the device's own page, so "why does this
  peer see 2a01:…" has an answer where the device is;
- it is a last resort with its costs printed next to it, not a recommended path.

## 8. What would have to be settled first

- **Per-prefix router advertisement.** `raRange` gives one prefix per range, and
  Android ignores stateful DHCPv6 entirely (`internal/dhcp/validate.go` warns
  about this in those terms), so a deterministic address per device per prefix
  exists only for non-Android clients.
- **Accounting.** Counters are keyed by address. A translated flow counts bytes
  against an address the device does not hold, and the NDP inventory
  (`internal/clients/nd_linux.go`) can name the ISP address but not the shadow
  one. Per-client numbers would need a second keying or a mapping table.
- **Inbound.** As §3 says, a translated exit makes "the device is reachable
  under the broker prefix" true only for flows it started. Either the UI says
  that, or v6 forwarding grows a translation form — which contradicts the §7
  position in `docs/port-forwarding.md` and would need that argument re-opened.
- **The box's own traffic.** With a second prefix the router itself has two
  addresses, and its own outbound — health probes, DDNS, updates — picks a source
  by the same host rules everything else does. A probe that crosses one path
  while the exit's traffic uses the other reports "up" about a path nobody is
  using, and DDNS publishes whichever address the peer happened to see. This one
  is testable today and is where the ambiguity reaches olr's own behaviour.

## 9. Facts worth not re-deriving

- `Exit.IPv6` is `via | block | direct`; block is the default because a v4-only
  exit that lets v6 leak is the quietest failure in `docs/gateway.md` §5.4.
- The classifier's order is destination, device, source, and destination wins by
  running first — the device and source rules test that our mark bits are zero.
- RPDB has two rule kinds in olr: `local` and `mark`. There is no `from`.
- `Carries` is what makes "one exit, two prefixes" possible for `interface`
  exits and impossible for `next_hop` exits.
- A foreign rule that selects a table with a default route blocks apply
  (`plan.Blocked`), which is why a source-prefix rule cannot be added by hand
  beside olr.
- RFC 6296 is Experimental; Linux's implementation of it is stateful in a `nat`
  chain; the RFC's multihoming model is source-driven.
- Doing nothing is usually right: one device, one path, and a health check that
  re-points it when the path dies.
