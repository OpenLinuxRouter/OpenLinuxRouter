# `dns` module design

Status: **§6's v1 row is built.** `internal/dns` is the module, `internal/dnsrelay`
and `internal/dnsd` are the relay, and the packaging carries `olr-dns.service`
(unbound, on loopback:5353) and `olr-dnsd.service` (ours, on :53). Section
references are to `design.md`.

Three things this document argued about have since been settled, and each is
recorded where it arises: §7.1 turned out to have been decided already by
`design.md` §3.5; §6's "blocking is native and cheap" does not survive the relay
(§4.4); and **olr no longer routes by domain name at all**, which closes §7.3 and
takes fake-IP out of the design entirely — `docs/gateway.md` §4 has that
argument, and §2 and §4.3 here are trimmed to match.

This document exists because DNS turned out not to be a service olr configures.
It is the layer that decides which traffic can be policed at all, and that
makes it a routing concern wearing a resolver's clothes.

---

## 1. The topology this has to survive

A real deployment is not one router. It is several boxes on one L2, each
willing to act as a gateway, and clients free to pick any of them:

| | Role | Has the WAN uplink |
|---|---|---|
| 1 | Modem / ISP router — dials, NATs, serves DNS, usually serves DHCP too | yes |
| 2 | The olr box — DHCP, DNS, routing | no |
| 3 | A proxy box — DNS + routing | no |
| 4 | Clients | |

Boxes 2 and 3 are side routers: they hang off box 1 and forward to it. A client
sets a default gateway and a DNS server, **independently**, and each may point
at any of the three. That is not a configuration option we offer. It is a
property of the wire, and it has to be treated as a fault mode.

### 1.1 The rule

> **DHCP hands out olr, and only olr, for both the gateway and the resolver.
> Boxes 1 and 3 are exits, never client-facing.**

Everything below is a consequence. The proxy box is a **next hop** exit
(`via 192.168.1.x`) in the §12 sense, and the modem is another one. Neither is
something a client is ever told about.

### 1.2 Why — two failures that have no fix at the client layer

**A client pointed elsewhere is invisible, not merely unaccounted.** Traffic
another gateway carries never reaches olr's counters at all — not the
`ipv4_addr . mark` sets (docs/gateway.md §7.1), and not the residual either,
because nothing knows it should be there. This is a different class of problem from unattributed bytes, and it
defeats the "always show what you cannot account for" rule by construction —
there is no counter to show. Detection is limited to the heuristic *has a lease
and answers ARP but has produced no traffic*.

**IPv4 has no default-gateway failover.** One gateway, no liveness check, no
second choice. If the proxy box is a client's gateway and it dies, that client
is offline until a human intervenes. The workarounds are all bad, and it is
worth writing down why so they are not re-proposed:

| Approach | Why not |
|---|---|
| Short lease, rewrite option 3 | Half the lease time to converge; many clients ignore a changed gateway on renew |
| VRRP between olr and the proxy box | Needs keepalived *on the proxy box* — a box we neither own nor version |
| olr gratuitous-ARPs the proxy's address | Races the box if it half-recovers |
| IPv6 RA router lifetime 0 | Genuinely works, for v6 only, which is not the problem |

One layer up it is not a problem at all: olr health-probes the exit and
re-points it. No client knows anything happened, so there is nothing to
converge. The probe must be the **through-path probe**, not a ping — a crashed
proxy daemon on a live box answers ARP and ICMP indefinitely while forwarding
nothing.

### 1.3 The limits of the rule, stated

On a shared L2 an operator with admin on their own laptop can always
static-configure their way onto box 1 or 3. Port isolation on a managed switch
is the only real answer, and we do not require one. We can make it *detectable*
and we can make it *inconvenient* — block client→proxy-box traffic except from
olr — and then we document it rather than implying coverage we do not have.

---

## 2. The two legs

Pointing a client at olr means two independent things, and they fail
differently.

| | Gateway leg | Resolver leg |
|---|---|---|
| Buys | Packets reach the box at all: per-source `Internet via`, IP/GEOIP rules, byte accounting | Blocklists, per-group blocking, the query log, and exact names for the proxy's own domain rules |
| Missing | Nothing works; the box never sees the traffic | Domain policy silently does not apply |
| Fails | Loudly | **Silently** |

The reverse mismatch is the milder one: resolver-here, gateway-elsewhere costs
the policy but not the connectivity — the client still receives a routable
address and still reaches the internet, and only the name-dependent half goes
unapplied. So the dangerous misconfiguration is gateway-yes / resolver-no, which
is exactly what §1.1 forecloses.

One constraint that is easy to miss, and which — since gateway:§4 — applies to
the *operator's* proxy rather than to us: whatever resolver a proxy derives its
domain rules from has to be the one its clients actually used, or it is matching
on names nobody asked it for. olr's part is to make the recommended arrangement
expressible — point `upstream` at the proxy's resolver, so one answer serves
both — not to model the mapping itself.

### 2.1 Gateway leg

nftables marks forwarded traffic by source; RPDB routes the mark to a table
whose default is the proxy box. Standard §12 next-hop exit. Two things the
proxy box does that have to be absorbed:

- When its rules say DIRECT it forwards back out the same interface and sends
  olr an **ICMP redirect**. `accept_redirects=0` on olr, or the table quietly
  acquires routes we did not choose. This is the mirror of the `send_redirects=0`
  obligation already recorded for LAN-side next hops.
- Its reply goes straight back to the client over the shared L2, so olr sees one
  direction only — byte counts halve and `ct mark` restore misses. Fix by SNAT
  at olr toward the next hop, or by giving the proxy box a segment of its own.
  SNAT is cheaper and costs the proxy its own per-source rules; a dedicated
  segment is correct and costs a VLAN. **Open.**

### 2.2 Resolver leg, and why a proxy's DNS matters

A proxy matches domain rules by one of three means. Only one of them survives a
client that did not use our resolver:

| Source of the domain | Survives? |
|---|---|
| Fake-IP reverse map (`198.18.0.0/15` → name) | No — the client never received a fake IP |
| A real-address mode's ip→domain cache | No — same dependency |
| Sniffing TLS SNI / HTTP Host / QUIC ClientHello | **Yes**, for TLS and HTTP |

So sniffing rescues rule matching *inside* the proxy, and nothing rescues a
routing decision that was supposed to be triggered by an address the client
never got. It also degrades: ECH encrypts the SNI, and non-TLS protocols have
nothing to sniff. Treat it as a fallback, not a design.

This is what makes **DoT/DoH blocking load-bearing** rather than hygiene, and it
ranks by cost to defeat: plaintext :53 to a hardcoded server (cheap — DNAT
hijack), DoT on :853 (cheap — drop by port, do not reject), DoH on :443
(expensive — IP/SNI blocklist, kept current, TCP *and* UDP or it moves to
HTTP/3). Browsers and Apple clients auto-upgrade to DoH by default, so this is
the common case rather than the adversarial one.

---

## 3. Why we do not use unbound's forwarding

`forward-zone` is instance-global. Views carry `local-zone`, so per-client
*blocking* is native and per-group parental controls are nearly free; views do not
carry forwarding, so **per-client upstream selection is not expressible in
unbound**. (Established from documentation, not verified against the build in
trixie. It is load-bearing — confirm before relying on it.)

> **Built:** the view half of this stopped mattering. Once the relay owns `:53`,
> every query reaches unbound from `127.0.0.1` and all views collapse into one,
> so blocking moved into the relay (§4.4) and unbound is configured with no
> views at all. §7.4's verification is therefore no longer blocking — it becomes
> load-bearing only if per-client selection moves into unbound; it currently
> stays in the relay, before unbound sees a query.

Unbound's forwarding half is still global. Its `upstream` setting remains one
default resolver for OLR, but the relay now selects a different resolver when
the client's effective gateway exit names one (gateway:§2.8). Unbound itself
does not make that per-client choice.

Pointing the *global* upstream at a fake-IP proxy remains unsafe for mixed
exits: every client would receive its synthetic addresses, including direct
ones. Instead, gateway attaches an optional DNS server to each exit. The relay
identifies the asking client by DHCPv4 lease or neighbour entry, selects its
effective exit, then forwards permitted public questions to that server.
Local names and blocking still run before the selection. The client always
sees OLR as its DNS server. The relay does not rewrite fake answers, and an
unidentified client on a subnet with conflicting per-client DNS choices gets
SERVFAIL rather than a possibly incompatible answer.

The existing `:53` hijack is a separate enforcement feature: DHCP option 6 is
advice, and a client using hardcoded plaintext DNS needs interception to stay
under OLR policy. Encrypted DoH is outside this feature.

---

## 4. The decision: a splitter, not a resolver

Writing a resolver is the wrong project. The hard parts — recursion and referral
chasing, DNSSEC validation and trust-anchor rollover, NSEC3 nonexistence proofs,
cache-poisoning defence, twenty years of accumulated edge cases — are all parts
we do not want and would get subtly wrong for years.

What we want is three things, and all three sit at the *front* of the pipeline:

1. See every query, with the client's address.
2. Decide, per client, whether to answer it ourselves.
3. Decide, per client, **which upstream answers it**.

So olr listens on :53, applies policy, and relays to unbound — which keeps doing
recursion, DNSSEC and caching. This does not remove unbound. The box ends up
running dnsmasq for leases, unbound for resolution, and olr for policy and
observation. That is a layer, not a simplification, and pricing it as one is the
mistake to avoid.

`internal/dhcp` already renders `port=0` into dnsmasq's config, with a test
enforcing it — dnsmasq is deliberately confined to DHCP and :53 left free. The
shape below is what claims it.

### 4.1 The invariant

> **Parse a copy for observation. Forward the original bytes untouched.**

Never re-serialise a response. Re-serialising breaks DNSSEC signatures, drops
EDNS options we did not model, and mangles record types we have never heard of.
Relaying the original means a parse failure costs a statistics entry and nothing
else — resolution still works. That makes the parser **best-effort by
construction**: it never has to be right, only useful. This property, not the
absence of parsing, is what keeps this out of resolver territory.

The one place it bends: returning NXDOMAIN or an override requires synthesising
a response. Small, and the only code path that must be exactly right.

### 4.2 Passthrough on the fast path, observation on a tee

```go
// fast path: serve the client first, observe second
rn, _ := upstream.Read(rbuf)
conn.WriteToUDP(rbuf[:rn], src)          // client is done here

select {
case tee <- obs{src: src.IP, msg: clone(rbuf[:rn])}:
default:
    dropped.Add(1)                        // statistics lag; DNS never does
}
```

The tee buys **isolation**, not speed. Three obligations come with it:

1. **Copy the bytes, never send the slice.** Handing `buf[:n]` to a channel
   while the read loop reuses `buf` misattributes answers to the wrong client,
   intermittently, under load only.
2. **`default:` on the send, always.** A channel that blocks when full turns a
   slow parser into DNS latency — the exact failure the tee exists to prevent.
   Drop, and count the drops: the gap must be visible, same rule as the
   `unpoliced` counter.
3. **`recover()` in the parser.** Malformed responses are routine and
   compression-pointer handling is where they bite. On the fast path a panic
   ends DNS for the house; on the tee it increments a counter.

A convenience worth exploiting: the response echoes the question section, so
`{srcIP, responseBytes}` is fully self-describing. No correlation table, no
pending-query map, no timeout sweeper. The gap is queries that never get an
answer, which produce no observation — keep a counter and accept that v1's query
log is a log of *answered* queries.

Once the relay is `read → policy → forward → write → try-send` it is **done**.
Every later feature lands on the far side of the channel and cannot regress DNS.
That is the line to hold in review: anything that influences the response belongs
in the policy lookup before the forward; anything that merely observes goes over
the channel.

### 4.3 The domain→IP map

The one thing that does require reading responses, and the reason 4.1 is phrased
as it is.

- **Compression pointers** must be resolved, with a loop guard, or a malformed
  response hangs the parser. `golang.org/x/net/dns/dnsmessage` handles this
  correctly and is most of the argument for using it over hand-rolling.
- **Attribute to the QNAME, not the record owner.** `www.example.com` commonly
  answers as a CNAME to `example.cdn.net` with the A record owned by the latter.
  Mapping owner→IP naively bills the client for a CDN hostname it never asked
  for. Keep the chain as well — its tail is the organisation signal §12's
  statistics model groups by.
- **It is many-to-many.** One name resolves to several addresses; one CDN
  address serves dozens of names. This is precisely the case where a per-domain
  byte split must not be fabricated.
- **Bound the map by capacity and recency, not by the answer's TTL.** A TTL says
  how long a resolver may cache an answer, not how long the pairing stays true:
  a connection routinely outlives the lookup that opened it, and a name dropped
  on the resolver's clock takes attribution with it. The map keeps the most
  recent 20,000 `(client, address)` pairs and, when full, evicts the pair no
  answer has confirmed for longest. That bounds an address reassigned to another
  tenant the same way it bounds everything else — the stale pair is the one
  nothing refreshed, so it is the first to go — rather than by a timer that
  happens to be right for neither short-TTL nor long-TTL answers.
- **Key by `(client, IP)`.** The source address is free and global keying
  cross-attributes when two clients reach one CDN address by different names.

Skip authority and additional sections, RRSIG/NSEC (relay, never validate), and
everything non-address. Log the qtype; map only A and AAAA.

The many-to-many applies to everything, with no exception for proxy-routed
traffic. An earlier draft had one — fake IPs are 1:1 with names, so anything
routed by name attributed exactly — but gateway:§4 removed the mechanism that
produced them, and one honest story about accuracy beats two. IP→ASN stays the
fallback for what this can never cover: clients that resolved before olr
started, connected by address, or asked somewhere we cannot see.

### 4.4 Blocking lives in the relay, not in unbound's views

A correction to §3 and to §6's "blocking is native and cheap", found while
building and forced by the rest of this document rather than chosen.

unbound selects a view by the **client's netblock**. The moment the relay owns
`:53`, every query reaches unbound from `127.0.0.1`, so every view matches the
same netblock and the whole mechanism collapses into one. The relay is the only
thing left that still knows who asked.

So the relay answers blocked names itself, which §4.1 already anticipated as the
one place the invariant bends — "returning NXDOMAIN or an override requires
synthesising a response. Small, and the only code path that must be exactly
right." It is about a hundred lines, and it buys two things beyond making the
feature work at all: unbound stays a plain recursive resolver with no views, so
the escape hatch stays a plain passthrough; and the answer is exact about the
client rather than about a netblock the resolver had to be told about twice.

The synthesised response echoes the query's ID and question section, because a
client matches on both and silently discards anything else — a blocked name that
produced a discarded answer would present as a timeout, which is the failure
blocking exists to avoid.

### 4.5 Where the observations are read from

The query log and the name map live in the relay's memory and are served over a
read-only unix socket at `/run/olr/dns/observe.sock`, which `olrd` reads through
on every request.

This is the *read* direction only. §3.5's "not a private RPC channel" governs
how we **drive** a backend, and that stays rendered files plus SIGHUP: the relay
starts from `relay.json` and `policy.d/` with nothing else alive on the box. A
read-only socket costs neither of the two things that corollary names — the
relay is still independently runnable, and `curl --unix-socket` on it is a
better debugging story than parsing a lease file, not a worse one.

A socket rather than a state file because the alternative is a line per query
appended to disk, and a great many olr boxes boot from an SD card. It also
leaves §7.5 genuinely open instead of quietly answering it.

`/queries` and `/names` take the same optional `?limit=` the API does, and the
bound travels all the way down rather than being applied to what comes back.
A polling page asks for the newest two hundred of five thousand; trimming the
reply in `olrd` would have meant the relay encoded five thousand entries and
`olrd` parsed them, to show two hundred. `stats.held` still reports the true
total, so a caller that asked for fewer can say how many it is not showing, and
an absent `limit` still means everything — `olr dns queries` reads the log in
full and must not start truncating because a page wanted a screenful.

Reading `/stats` gets the counters without the log. The status endpoint used to
reach them through `/queries` and discard the rows, which put the whole ring on
the socket every five seconds to report six integers.

### 4.6 Local names live in unbound, not in the relay

A client on this network should be reachable by name. `hosts` in the module
config is a name and its addresses; each is published under `local_domain`,
default `home.arpa`, and the set renders into unbound's config as a
`local-zone ... static` with one `local-data` per address.

**unbound and not the relay, which is the opposite of where blocking went**
(§4.4). Blocking had to move because it is *per client*, and every query reaches
unbound from 127.0.0.1, so the relay is the only thing that still knows who
asked. A local name is the same answer for everybody, so that argument does not
apply — and what is left is a list of edges unbound already has right and we
would have to get right again. The sharpest: a name with only an A record must
answer **NODATA** and not NXDOMAIN when asked for AAAA, or the client concludes
the name does not exist and stops asking for either family. Writing that in the
relay would be §3.5's mistake, the module taking on a backend's job for no gain.

Three choices inside that, each with a reason:

- **`static`, not `transparent`.** A name under the local domain that we do not
  publish gets an authoritative NXDOMAIN rather than being forwarded. Both
  halves matter: local names resolve without asking anybody, and the names of
  the clients in a house do not leak to whichever resolver is upstream.
- **Reverse only for private addresses.** `local-data-ptr` is rendered when the
  address is RFC 1918 or ULA. Pointing a local name at a public address is
  legitimate — it may be a host across a VPN — but claiming its PTR would make
  this box authoritative for a reverse name it does not own, and the override
  would be invisible to whoever later wondered why that address resolves oddly.
- **Names are stored relative to the domain.** `sony-tv`, not
  `sony-tv.home.arpa`. Typing it in full is what an operator does, so both
  spellings normalise to one entry rather than two that diff against each other
  forever, and changing the domain moves every name under it at once.

This is not the authoritative service §6 rules out permanently. There is no zone
to transfer, no delegation pointing here, and nothing off this network can ask:
it is a local override on the resolver a household already runs, which is what
`local-zone` is for.

**The bare name is a DHCP fact, not a DNS one.** `sony-tv:5006` works only on a
client that was handed the domain as its search domain — DHCP option 15, which
is `domain` on the pool in `internal/dhcp`. `sony-tv.home.arpa:5006` works
everywhere. olr cannot make the short form universal and does not pretend to.

### 4.6a Anti-rebinding, and the zones that are yours

unbound is rendered with `private-address` for `10/8`, `172.16/12`,
`192.168/16`, `169.254/16`, `fd00::/8` and `fe80::/10` — the anti-rebinding
list in `render.go`. A *public* name whose answer holds one of those addresses
is stripped. That is a real attack being closed: a page on the internet that
rebinds its own domain to `192.168.1.1` can talk to whatever is on the LAN from
the browser, and the clients that answer there — cameras, NAS boxes, printers —
are the ones nobody updates.

It is also exactly what an operator's own record looks like when they publish
`grafana.home.example.com → 192.168.1.1` in their provider's DNS, and the rule
cannot tell the two apart. **`private_domains`** is where the operator says
which zones are theirs; each entry renders as unbound's `private-domain`, which
allows private addresses in answers for that zone and its subdomains.

Three things about it that matter:

- **It is per zone, deliberately not a global off switch.** The protection
  stays on for every zone nobody claimed, which is every zone that could be
  somebody else's.
- **It only affects clients that ask this resolver.** A client configured with a
  public resolver gets the record directly. This matters most for WireGuard
  peers, whose configuration tells them `DNS = <router>` — every one of them
  uses this path.
- **It is not a way to publish a private name.** `local-zone ... static` (§4.6)
  answers the local domain itself; a zone inside it is answered there, never
  forwarded, so an exemption for it does nothing. Validation says so.

### 4.7 `dhcp` is subscribed to for a warning, and nothing else

`design.md` §4.1 names `dhcp`↔`dns` as the cycle that has to be broken by
inverting one side, and fixes which: `dhcp` publishes, `dns` subscribes. The
subscription exists — `ReservationView` in `internal/dns/dhcp.go`, adapted in
`internal/daemon` the same way the three `LinkView`s are — and it reaches
`Validate` only. It cannot change a rendered byte.

That restriction is the design, not an unfinished edge. Two things break if a
local name is *rendered* from a reservation:

- **Drift stops meaning anything.** Rendered bytes are what drift is measured
  against (§5.4). A reservation edit would leave this module drifted until
  somebody applied it, and drift is supposed to mean a human edited the box.
- **The apply promise breaks.** Every mutating route in olr is one request whose
  plan decides whether it lands (§5.3.3). Re-applying `dns` when `dhcp` applies
  means a dhcp request restarts the resolver without the dhcp plan ever saying
  so — exactly the surprise that promise exists to prevent. And `core.Events` is
  a notification bus, not a data channel, so there is no cross-module apply to
  reach for anyway.

What the subscription buys instead is the one failure that is otherwise silent:
the reservation was renumbered, DHCP moved the client, and the name goes on
answering where it used to be. That is a warning on the name, naming both
addresses, and it costs nothing — an unreadable dhcp config costs the check and
not the config.

**Not typing the address twice is the client's job**, which is where a
cross-module convenience belongs when there is no cross-module transaction.
`olr dns add host sony-tv` with no address reads dhcp's config over the API and
takes it from the matching reservation; the WebUI offers the reserved clients
that are not published yet and fills both fields from one. Two modules' config,
two writes, each with its own plan — and the join in the one place that is
allowed to know about both.

### 4.8 DNS answers; the firewall controls access

An empty `listen` with `enabled: true` means the relay binds port 53 on all
addresses. Pinning a specific address remains possible, but an address copied
into intent can go stale when a network is renumbered. An explicitly pinned address must be kept
current: if it cannot be bound, the relay fails rather than widening its
listen address silently.

Pins are not the whole listen set. The relay also answers on addresses olr
itself has promised to a client — today the dial-in address every WireGuard
configuration names — and those are derived from the module that owns them
rather than pinned. They are bound leniently: the tunnel may come up after the
relay does, and a missing service address is retried rather than allowed to fail
the whole listener, because "the tunnel is down" must not become "the house has
no DNS". `olr dns status` prints the resulting set and where each address came
from (internal/dns/dialin.go, internal/dnsrelay).

DNS does not maintain a second source-access list. In particular, delegated IPv6
prefix changes no longer rewrite `relay.json` or ask the operator to apply DNS.
The firewall owns which networks may reach the resolver. With the firewall off
or misconfigured, a wildcard-bound DNS relay can answer external queries; this
is not mitigated by the DNS module. Legacy stored `allow_from` is discarded when
read and omitted on the next save. New requests containing it are rejected.

---

## 5. The risk, which is availability

Perf is not a concern: a relay plus a memcpy against a house doing tens of
queries per second. Parser risk is contained by §4.2. The real cost is
different.

Today a DNS bug would produce a bad config file and a daemon that refuses to
start — loud, contained, and the thing answering queries is twenty years
hardened. Owning :53 means any olr crash, bad apply or OOM takes DNS down for
the whole building at once, and DNS-down does not read as DNS-down; it reads to
every occupant as *the internet is broken*. That is wider than §5.5's lockout
guard was scoped for, which strands only the operator.

Two things make it manageable:

**It is stateless.** No cache to warm, so a restart costs milliseconds and
clients retry unprompted. A crash loop is survivable in a way it would not be
for a real resolver.

**Which makes the process boundary the decision that matters.** In-process with
`olrd`, every config apply and upgrade blips DNS for the house, and an unrelated
panic in an HTTP handler takes the LAN offline. A separate process with
`Restart=always`, reading policy from `olrd` over the existing socket, is the
shape to argue for — at the cost of the single-daemon simplicity that is
currently a property of the design. **Open, and it determines whether the risk
above stays manageable.**

> **Built, and it was never open:** `design.md` §3.5 had already decided this.
> "Backends are separate processes even when we write them… if `olr-dhcpd` ever
> exists it is a process and a unit, never a goroutine", and its test — *does it
> have to keep running while `olrd` is stopped?* — DNS fails plainly. So the
> relay is `internal/dnsd`, `Type=notify`, `Restart=always`.
>
> It shares an executable with `olrd` and does not share an address space: one
> `olr` binary carries every role, and `olr-dnsd.service` invokes it as
> `olr internal dns-relay`. §3.5 asks for the second, never the first.
>
> One correction to the paragraph above: it is *not* configured by reading from
> `olrd` over a socket. §3.5's corollary is that we drive our own backend the way
> we drive dnsmasq — rendered files and a signal — because a private channel
> costs the backend its ability to be run and debugged on its own. `relay.json`
> is a restart; `policy.d/` is a SIGHUP.
>
> The observation direction is a socket; see §4.5 for why that is not the same
> thing.

A network-facing UDP listener needs an input boundary. The relay does not
implement one: DNS owns resolution, while the firewall owns access to the
router's port 53. A disabled firewall can expose the resolver to the internet;
this deployment risk must be addressed in firewall configuration.

---

## 6. Scope

| | | |
|---|---|---|
| **v1** | resolver leg: DNAT hijack of forwarded `:53`, upstream = unbound, per-network policy | built; policy keys off client prefixes until `link` lands networks |
| | passthrough relay with tee, query log, domain→IP map | the observability case is the whole reason to own :53 |
| | per-client blocking, DoT `:853` drop | built **in the relay**, not in unbound views — §4.4; the block is what protects everything else |
| | local names under a local domain | built **in unbound**, not the relay — §4.6, which is the same question answered the other way |
| | `private_domains`: zones whose public records may hold private answers | §4.6a — anti-rebinding stays on for everything nobody claimed |
| | `dhcp` reservations read for a cross-check | §4.7 — the subscription design.md §4.1 asked for, restricted to validation so it cannot drift |
| **v2** | per-client upstream selection (proxy vs direct) | built in the relay; gateway:§2.8 |
| | DoH `:443` blocklist, TCP and UDP | ongoing maintenance, not a one-off |
| **Never** | recursion, DNSSEC validation, our own cache | the moment this process needs a cache, it is a design change and not a refactor |
| | authoritative service, zone transfers | escape hatch, permanently |
| | minting or routing fake IPs | gateway:§4 — routing by name is the proxy's job |

---

## 7. Open

1. ~~**Separate process or inside `olrd`** (§5).~~ **Closed** — `design.md` §3.5
   had already decided it. `internal/dnsd`, its own unit, `Restart=always`,
   driven by rendered files and a signal.
2. **SNAT toward next-hop exits, or a dedicated segment for the proxy box**
   (§2.1). Determines whether byte accounting sees both directions.
3. ~~**Fake-IP destination routing versus per-source assignment — which wins.**~~
   **Closed by deletion**, which is the only way a question like this closes
   cleanly: it asked which of two things wins, and `gateway.md` §4 removed one of
   them. olr does not route by name, so there is no global domain rule to
   compete with `Internet via`, and the §5.6 surprise it worried about cannot
   arise. Routing by name stays the proxy's job.
4. **Whether unbound views really cannot carry `forward-zone`** (§3). Verify
   against trixie before the ladder in §3 is relied upon. **No longer blocking:**
   blocking moved into the relay (§4.4) and unbound is rendered with no views at
   all, so this only matters again if v2's per-client upstream selection is
   attempted inside unbound rather than with a second resolver process.
5. ~~**Where the query log lives.**~~ **Closed — it stays where it is.** It was
   the larger of two workloads voting in §10's revision-storage decision, and
   that decision came back "no store at all" (§10 #5). So the ring in the
   relay's memory, served over a socket (§4.5), is not a placeholder any more:
   it is the answer. The log starts empty after a restart and the API says so
   rather than implying a history it lacks.

   The argument that decided it came from this module and was only ever written
   in `internal/dnsrelay/log.go`: **a line per query appended to disk is
   continuous flash wear on the many olr boxes that boot from an SD card.** It
   is now recorded at §10 #5 too, because it generalises past the query log and
   was too load-bearing to live in one comment.

---

## 8. Known gaps in what is built

Stated here rather than left to be discovered, per the rule that we document
what we do not cover instead of implying coverage we do not have.

- **A redirect that failed to load is not detected.** `olr-dnsd.service` loads
  `hijack.nft` in `ExecStartPost` with a leading `-`, so a failure is logged and
  does not take DNS down with it — the right trade, since a broken redirect
  costs the redirect and a failed unit costs the building its name resolution.
  But nothing reads the ruleset back, so `olr dns status` cannot say the
  redirect is missing. Reading nftables back for the whole box belongs to
  `gateway` (docs/port-forwarding.md §8); this closes when that lands.
- **IPv6 is captured only if there is an IPv6 listen address.** The renderer
  emits per-family redirect rules and warns when one family has none, which is
  honest but not the same as covering it. A client resolving over IPv6 on a
  dual-stack network bypasses the redirect entirely — the same failure the
  routing model records for a v4-only exit.
- **A local name and a DHCP reservation remain two stored facts**, by the
  decision in §4.7 rather than by omission. They are told apart when they
  disagree and filled in together when they are created, and nothing keeps them
  in step after that. A reservation renumbered from a script, with nobody
  looking at a plan afterwards, still leaves the name answering the old address
  until the next time anything validates.
- **Adding a local name restarts unbound.** It lands in `unbound.conf`, which is
  not reloadable, so the plan is `ImpactRestart` — milliseconds, and the cache
  flushed. Acceptable for the field it is, and the one thing to revisit if
  naming clients turns out to be a weekly activity: a separate included file
  would make it a reload, at the cost of a mechanism nothing else needs.
- **Policies key off client prefixes, not networks.** `link` has not landed, so
  this is the same stand-in `internal/dhcp` makes by keying pools off kernel
  interface names, and it changes shape at the same time.
- **The query log is of *answered* queries.** A query that never gets an answer
  produces no observation, by §4.2's design — there is no pending-query map and
  no timeout sweeper. The `failed` counter is where those show up.
- **Not verified end to end.** The renderers, the planner, the policy matcher
  and the relay's own request path are unit-tested, including byte-for-byte
  passthrough against a fake upstream. Real `:53` capture, `nft -f` loading and
  unbound interop need Linux with `CAP_NET_ADMIN` and have not been exercised.
