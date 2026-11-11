# `ingress` module design

Status: **built, unproven.** `internal/ingress` is the module, the proxy runs as
`olr-caddy.service`, and there is a WebUI page. What has not happened is a
certificate: §11.8 records exactly how far a real Caddy has been driven and
where that stops. Bare section references are to this document; references to
`design.md` name it.

Five things were decided before this was written, and the rest of the document
is mostly their consequences: the backend is **Caddy**; it **runs as its own
binary and unit, never inside `olrd`**; Caddy **owns :80 and :443**; the
first-boot path stays **IP + port**; and certificates are Caddy's own job rather
than a separate ACME client's.

**§5 has since reversed on where the binary comes from.** It said we would ship
one; olr now drives whichever the operator supplies, the same way every other
module treats its backend. What changed the answer was not the packaging effort
but the provider list — a shipped build makes the set of available DNS providers
a fact we assert in three places, and asking the binary makes it a fact we read.
§11.6 and §11.7 close as a result.

Building it changed §3, twice. The first version had the module hold no domain
of its own: published names were written into the resolver's zone and answered
by this box. Using it then meant renaming every client in the house, because no
CA issues for `home.arpa` — so a published name is now a record in public DNS,
in a zone the operator owns, and this module holds that domain as its own fact.
§3 is rewritten around the split, §11.2 is closed twice, and **§4.2** — the most
expensive failure in the file, absent from the first draft — survives in a
narrower form.

One thing was *not* decided and is the largest question here: whether olr should
have this module at all. §2 is that argument, and it is a positioning question
(§2 of `design.md`), not a technical one.

---

## 1. The object: a published service

An operator has something running on their network — Home Assistant on a NUC,
Grafana in a container on the router, a NAS web UI — and wants to reach it at
`https://grafana.home.example.com` from a phone, without a port number and
without a certificate warning.

Today that costs four separate pieces of work: a DNS record, a wildcard
certificate, a reverse-proxy stanza, and a redirect from `:80`. Three of the
four are set up **once** and never thought about again. Only one of them varies
per service. So the object the operator creates has two fields:

| Field | Example | |
|---|---|---|
| `name` | `grafana` | the label under the network's local domain |
| `upstream` | NUC : 3000 | where the request goes |

An optional `link_path` (for example `/ui`) is only for the Overview
shortcut and icon discovery. It opens that page under the published name;
it does not rewrite requests or alter the Caddyfile. If the homepage redirects
to a page with an icon, icon discovery follows same-origin HTTPS redirects.
The WebUI can instead save a custom PNG/JPEG/WebP logo (resized to 128 px)
or a theSVG icon ID for each service, which names a mark bundled in `olrd`
rather than fetching one (`internal/brandlogo`). Clearing it restores automatic favicon
discovery; this changes only the Overview shortcut, not proxy routing.

Each upstream may also set or remove request headers sent to that service.
By default Caddy forwards the public `Host`; some appliances redirect requests
for unfamiliar hostnames back to their own login page (or into a loop). Set
`Host` to the appliance's LAN address in that case, for example
`olr ingress add mi --host 172.16.1.163 --port 80 --header 'Host: 172.16.1.163'`.
Use repeated `--header 'Name: value'` and `--remove-header Name` flags, or the
service editor's advanced section. This changes neither the browser's response
headers nor TLS SNI when connecting to an HTTPS upstream. Header names must be
unique (case-insensitively); values may be empty but not contain control
characters.

Everything else is derived and never asked:

- the certificate — one wildcard covers every name that will ever exist
- the name's answer — `dns` already serves this suffix (§3)
- the Caddyfile stanza
- the `:80` → `:443` redirect

That is the whole claim of this module. **Adding a service is one name and one
target**, because the expensive parts were paid once when the module was
enabled.

One constraint rides along with the wildcard and has to be enforced at write
time, because its symptom appears a long way from its cause. **A wildcard
covers exactly one label.** `*.home.example.com` covers
`grafana.home.example.com` and does not cover `grafana.lab.home.example.com`, so
a nested name renders into a syntactically perfect Caddyfile and then fails TLS
at the first request. The validator refuses the dot.

### 1.1 Module-level config, set once

The domain, the DNS provider and its credential, and an ACME contact address.
Entered at enable time, never revisited.

**The domain is this module's own** (`ingress.domain`): it is the zone the
operator adds records to, and the zone the wildcard is for. It is deliberately
not the resolver's local domain, which names the clients in the house and never
needs a certificate — §3 is the whole of that argument. The only thing read from
`dns` is that local domain, so a published domain falling inside it can be
refused rather than silently unresolved.

### 1.2 The upstream is a client, not an address

When several stored services are invalid, repairing one can be saved without
applying while the others still need repair. This requires fully fixing that
service, reducing existing validation errors, and introducing no new errors.
The response reports `pending` and remaining `problems`; status also reports
problems after refreshing the page. Caddy files and runtime stay untouched.
The final repair follows the normal validation and apply flow automatically.

Per `design.md` §4.1 a module reads another module's facts through that
module's API and never keeps a copy. The upstream should therefore reference a
**client** (`clients` owns identity, `design.md` §4.4) plus a port — not an IP
address copied into our config where it will rot.

Client references are stored as MAC addresses; names are display labels and
renaming a client does not change its published URLs or their targets. Legacy
name references are resolved on read and converted when saving ingress or
changing the client inventory. An already-unresolvable old name requires an
explicit target selection; it is never guessed from an address or URL.

This surfaces a constraint the operator has to be told about rather than
discover:

> **A published service needs a stable address.** A client whose address comes
> from a DHCP pool can move, and the rendered stanza then points at whatever
> took its place.

`dhcp` already owns fixed addresses. So selecting a client with no fixed
address is **refused, carrying the command that fixes it**, rather than
rendered into a stanza that works today and proxies to a stranger's laptop next
week.

Refusal rather than a warning, because of how that failure is distributed in
time: the config is correct for days, and then a lease turns over and a
published name starts answering with somebody else's machine. Nothing about
that moment points back at the decision that caused it. A warning is not
proportionate to landing there by default.

And a refusal rather than olr quietly reserving the address itself, because
that would be inferring a change to *another module's* configuration —
precisely what `design.md` §5.6 forbids. The remedy is named in the error and
the operator runs it.

A raw `host:port` form stays available for targets that are not clients: a
container on the router itself, `127.0.0.1:3000`, something behind another
router.

---

## 2. Why this is in a router at all

It is worth being honest that this module is not networking. It is hosting.
Every other module in §4 manages how packets move; this one manages how an
application is reached. It is the first feature that makes olr look less like a
router and more like a home server platform, and that is a change to §2's
positioning, not an addition to it.

The case for:

- It is the **always-on box with the LAN address**. The reverse proxy has to
  live somewhere, and every other candidate is a box that might be off.
- The recipe is **identical for everyone** and annoying for everyone. Wildcard
  cert, DNS-01, split horizon or a public record, one stanza per service. This
  is exactly the shape of thing olr exists to collapse into a form.
- It is **separable**. Unlike `gateway` or `dial`, nothing else depends on it,
  it ships as its own package, and removing it removes nothing from the router.
  If the positioning bet is wrong, the cost of unwinding it is one package.

The case against is one sentence and it is not weak: **there is no natural
stopping point after this.** Once olr publishes services it will be asked to
run them, back them up, and watch them. The boundary has to be drawn in this
document or it will not be drawn at all. §9 draws it.

---

## 3. The name is a record you own

`dns` answers a local zone authoritatively — `local-zone "<local domain>"
static`, so every name under it comes from this box and nothing under it is ever
forwarded. That zone is for clients: `sony-tv.home.arpa`, no certificate, never
visible outside.

A published service is the other kind of name. It lives in public DNS, in a zone
the operator owns, and the record is theirs to create. This module obtains the
certificate for that zone and proxies the name. It does not write the record,
and it does not need the resolver's help to resolve it.

| | client names | published names |
|---|---|---|
| zone | `dns.local_domain`, `home.arpa` by default | `ingress.domain`, a domain you own |
| answered by | this box, authoritatively | your DNS provider, publicly |
| certificate | none | one wildcard for `*.<domain>` |
| created by | `olr dns add host` | you, in your provider's zone |

### 3.1 Why not one domain

The first version had no domain field at all: published names were rendered into
the resolver's zone and answered by this box, under the same suffix as the
clients. One namespace for the whole box sounds like one less thing to keep in
step, and costs three things an operator can see:

- **Publishing forced a rename.** No CA issues for `home.arpa` (§4), so using
  this module at all meant moving every client's name to a domain the operator
  owned — one HTTPS service, and every bookmark in the house breaks.
- **The dialog could not offer to change it**, correctly, because the field
  belonged to another module; the operator had to read this document to find out
  where their domain came from.
- **The resolver answered two kinds of name in one zone.** A client name means
  "this client's fixed address"; a published name meant "this box, as seen from
  where you asked". The second had to be served by the relay, before unbound,
  because `local-data` cannot express it.

None of that is necessary when the record lives in public DNS. The two suffixes
are separate facts with separate owners, and nothing has to be kept in step
because nothing is copied.

### 3.2 What it costs

- **One record per service, created by hand.** `grafana.<domain>` has to point
  at this router in the operator's zone. The UI says what the name will be and
  never claims the record exists.
- **A private answer needs `dns`'s permission.** Pointing that record at a LAN
  address is legitimate and common (§3.3), but it is a public record holding a
  private address, and unbound strips exactly those by default as
  anti-rebinding. `dns.private_domains` is where the operator says which zones
  are theirs; without it the record works for anyone on a public resolver and
  silently does not exist for anyone using this one.
- **The published domain may not live inside the resolver's zone.** Validation
  refuses it: the resolver would answer NXDOMAIN for a name that exists, and
  nothing would say why. A sibling is the natural answer — `home.arpa` for
  clients, `pub.example.com` for services.
- **It is still not exposure.** A record that points at the router does not make
  the router reachable from outside; that is a port forward (§9).

### 3.3 Public DNS with a private answer

Worth naming, because it looks like a mistake and is not. `grafana.<domain>` →
`192.168.1.1` in a public zone means: the name resolves for everyone, and
reaches nobody without a route to this LAN — from the LAN, and over a VPN that
routes the LAN, it works; from the internet it is an unroutable address. The
operator has chosen "reachable where I am, not where I am not" without
split-horizon DNS, and this module neither knows nor cares which address the
record holds.

What it does depend on is `dns.private_domains` (§3.2) for every client that
resolves through this box — including each WireGuard peer, whose configuration
tells it `DNS = <router>`.

---

## 4. Certificates

Three ways to get a browser-trusted certificate for an internal name. Two of
them are dead on arrival and it is worth recording why, so they are not
re-proposed.

| | Works for an internal-only name? | Cost |
|---|---|---|
| HTTP-01 | **No** — the name must be publicly reachable on `:80` | would mean exposing the router to get a cert for something that is not exposed |
| Local CA (Caddy's internal issuer, mkcert) | Yes | a root certificate installed on **every** client — fine for two laptops, fatal for phones, TVs, consoles and guests |
| **DNS-01, wildcard** | **Yes** | an API token for your DNS provider, stored on the router |

So DNS-01, and a wildcard. The wildcard is what makes §1's claim true: one
certificate covers every service that will ever be published, so adding a
service involves no certificate step whatsoever.

### 4.1 The token has no proper home, and we should say so

olr has no secrets store. The provider credential lives in the config document
like any other field and is rendered into an **environment file** the unit
reads, mode `0600`, owned by root — never into the Caddyfile. That split is not
fastidiousness: the Caddyfile is the file an operator reads when something is
wrong, the file `olr ingress plan` diffs into a terminal, and the file that ends
up pasted into a forum post. The credential is in none of those because it was
never written there. The rendered secret is additionally marked so that every
surface which displays a file — the plan diff, the drift report, the logs —
withholds its contents while still comparing them byte for byte.

That leaves the real boundary, which is not nothing: **anyone with root on the
router can edit your DNS zone.**

Two mitigations that cost nothing and belong in the setup copy:

- **Scope the token to the single zone** if the provider supports it (Cloudflare
  does). A token that can write `home.example.com` and nothing else has a much
  smaller blast radius than an account key.
- Use a **dedicated zone or subdomain** delegated for this purpose, rather than
  the domain that also serves your mail.

This is a stopgap and the document should keep calling it one, rather than
implying a key-management story that does not exist.

### 4.2 The propagation check must not use this box's resolver

The first version of this section described a death trap, and it is worth
keeping the shape of it because the fix is one line that looks arbitrary. When
this module had no domain of its own (§3), the published suffix *was* the local
zone, which `dns` answers authoritatively and forwards nowhere. Caddy would ask
the provider API to create `_acme-challenge.<domain> TXT`, the record would
exist, and the system resolver — this box — would return a confident NXDOMAIN
for it. Issuance blocks on a propagation check that can never succeed, nothing
is misconfigured, and the symptom is "certificates just don't work".

Separate suffixes (§3) remove the contradiction: the resolver is not
authoritative for the published zone. What remains is the reason the field
survives — the box's resolver applies this network's blocking policies and
whatever upstream it forwards to, and the one lookup that decides whether a
certificate is issued should not be able to fail because of a filter we
rendered. So the check is pinned to public resolvers, two of them, and **two is
not redundancy**: Caddy asks each in turn and an unreachable one fails the
check, so every entry has to be reachable from the box. The defaults are 1.1.1.1
and 8.8.8.8, chosen for reachability: the original 9.9.9.9 is dropped by at
least one ISP, and a box behind it timed out on every attempt and never got a
certificate.

It leaks nothing worth protecting: the only names asked are `_acme-challenge`
records the operator is deliberately publishing, which the CA is about to query
from the outside anyway.

---

## 5. The backend: Caddy, supplied by the operator

olr does not ship a proxy. It drives the one that is on the box, which is the
same relationship `dhcp` has with dnsmasq and `dns` has with unbound.

That sounds unremarkable and is a reversal. This section used to say **bundled**,
and the argument for bundling was sound as far as it went; what follows is why
it stopped going far enough.

### 5.1 Why this module nearly became the exception

No official Caddy package includes DNS providers — not Debian's, and not
Caddy's own apt repository. That is architectural rather than an oversight:
providers are compiled-in modules and Caddy has no runtime plugin loading. And
DNS-01 is the only challenge that works for a name which does not resolve from
the internet (§4). So *somebody* has to produce a binary with the right module
in it, and for a while the answer was us.

### 5.2 What bundling would have cost

Two things, and only the second one is fatal.

> **Security updates become our obligation.** When Caddy patches a
> vulnerability, `apt` updates the distro's copy and does nothing for ours. Our
> users are patched when we cut a release.

That one is merely expensive — a standing CI job and a promise to keep. The
second is structural:

> **The provider list becomes a fact we assert rather than one we read.**

A bundled build fixes the provider set at our build time. Everything downstream
then has to *restate* that set — the validator, the schema's enum, the CLI's
completion — and every restatement is a copy that can disagree with the binary.
The failure that produces is the worst available: a provider the operator picks
from our own published list, accepted by our own validator, and then rejected at
runtime by a Caddy that never had it. The earlier draft's answer was a build-time
check comparing our list against `caddy list-modules`, which is a real fix for a
problem that did not need to exist.

### 5.3 What supplying it buys

Both costs go away, and the second one goes away *by construction*:

- **The binary is authoritative and is simply asked.** `caddy list-modules` says
  what it has. There is no list in olr to be stale — `olr ingress show
  providers` is a question, the config schema publishes no enum, and shell
  completion is fetched rather than compiled in.
- **The validator got smaller and more honest.** It checks that a provider was
  named; it does not judge the name. Whether that module exists is a question
  about a file on disk, and the validator is pure by design. The real check was
  already in the apply path — `caddy validate` on the rendered file, which
  rejects an unlinked `dns <provider>` before anything is written, in the
  binary's own words.
- **CVEs are the operator's distro's problem again**, like every other backend.
- **The module stops being special.** olr drives backends; it does not package
  them.

### 5.4 What it costs, which is a real barrier

The operator has to obtain a Caddy with their DNS provider compiled in. That is
a step "`apt install` turns this box into a router" (§1 of `design.md`) does not
otherwise have, and it is the honest price of this section.

It is smaller than it sounds, and the difference is entirely in whether we say
so at the right moment:

- <https://caddyserver.com/download> builds one with the providers ticked. No Go
  toolchain, no command line — a download.
- `xcaddy build --with github.com/caddy-dns/<provider>` for anyone who prefers it.

**So every failure along this path owes the operator both of those sentences.**
A missing binary is not "olr-caddy.service failed"; it is a refusal that names
the path olr looked in, says why no packaged Caddy will do, and gives the two
ways to get one. That message is the feature. `internal/ingress/binary.go` is
mostly that message, deliberately.

### 5.5 Where it goes, and living beside a distro Caddy

- Looked for at **`/usr/lib/open-linux-router/caddy`** first, then `caddy` on
  `PATH`. A binary at the first path is unambiguously one an operator put there
  for olr; the `PATH` fallback will usually be the distro's package, which works
  for everything except obtaining a certificate — and fails at that with Caddy's
  own message rather than a guess of ours.
- Supervised as **`olr-caddy.service`**, configured under
  `/etc/open-linux-router/`. Unit name and config path both differ from the
  distro package's, so a box already running Caddy has nothing to resolve. This
  is not optional politeness; it is the difference between working and not on
  any box that has ever served a web page.
- The shipped unit names the first path. A binary found anywhere else gets a
  **drop-in** from `olr enable`, never an edited unit — an edited unit is
  overwritten by the next upgrade and a drop-in is not. This is the same
  mechanism `dhcp` and `dns` use for a non-Debian layout.
- Reloads are `caddy reload --force`. Without `--force` Caddy skips a reload
  whose configuration matches what it is already running, which would make a
  reload issued to recover from a hand-edited running state do nothing at all.
- **`status` reports which binary was found**, because a box can have two and
  "which providers are available" is meaningless without saying which one was
  asked.

### 5.6 Bundling is not refuted, only deferred

Nothing above says a shipped binary is wrong — it says it is not worth its
second cost *yet*. If this module is ever expected to work with no steps at all,
the way back is open, and the thing that would make it affordable is the same
thing that makes it wanted: a build we run often enough that the provider list
and the CVE queue are already somebody's routine. §9 keeps it as a v2 row rather
than a rejected idea.

---

## 6. Ports, and the one rule that must not be optimised away

Caddy owns `:443`, and `:80` for the redirect. Consequences:

**Enabling must preflight both ports** and refuse with a message that names the
process holding them. An existing nginx or Apache is the single most likely
install failure for this module, and "Caddy failed to start" is not an
acceptable way to learn it.

**Bind to LAN interfaces only.** `dns.md` §5 makes this point about the
resolver and it applies here with more force: a reverse proxy reachable from the
WAN is a way to publish internal services to the internet by accident. Exposure
to the internet is a port forward's decision, made explicitly, with a different
conversation attached — see §9.

**The `:8080` API listener is never turned off by this module.** Ever.

> When Caddy is misconfigured, its certificate has expired, or `:443` is held by
> something else, `http://<router-ip>:8080` is the only remaining way in to fix
> it.

This is `design.md` §5.5's lockout guard in a new costume, and worse than the
original because the breaking configuration is one the operator typed. What
needs to change is that listener's *status*, from "a step during setup" to **a
permanent recovery path**. Written down here because the temptation to retire it
once a nice domain exists is obvious, and will recur.

This paragraph used to describe first boot as "`olr listen 0.0.0.0:8080` plus a
token, per `docs/install.md`", and concluded that nothing needed to change.
Neither half survived a real install. The token went in v0.1.6, because the page
that asks for it could not load while it was enforced; and `olr listen` stops
being a step at all under `docs/system.md` — olrd listens on `:8080` from the
moment it is installed. The recovery-path argument gets *stronger* for it: the
listener is no longer something the operator might or might not have switched on
before ingress broke, it is simply always there, which is what a recovery path
has to be.

The WebUI **may** be published through Caddy — `router.<domain>` →
`127.0.0.1:8080` — as an ordinary entry with no special case in core. That is a
nicety, it is opt-in, and it changes nothing above.

---

## 7. Mechanism

Intent in `/etc/open-linux-router/ingress.json` (`design.md` §3.2 rule 1); a
rendered Caddyfile; a unit we supervise over D-Bus. Nothing novel — the point
is that it is the same shape as `dhcp` and `dns`.

### 7.1 Reload, not restart, and validate before either

Two ways this differs from dnsmasq and both matter:

- **A restart is visible to users.** Reloading dnsmasq costs a DHCP request that
  the client retries. Restarting the proxy drops connections in the middle of
  somebody's video call. `caddy reload` replaces config without dropping
  listeners and is the only acceptable apply path.
- **A bad config takes down every service at once**, not just the one being
  edited. So the render must be run through `caddy validate` *before* it is
  applied, and a failure must be reported as a rejected change rather than a
  dead proxy. This is the `plan`/`validate` step `design.md` §3.2 already
  expects, with unusually high stakes.

### 7.2 We do not use Caddy's admin API

Caddy exposes an admin API that can mutate config without touching a file, and
it is tempting. It is rejected in §10: config that lives only in a running
process is not revisioned, not readable over SSH during an outage, and not
single-source. File plus reload keeps this module identical to every other one.

It cannot be turned **off**, though, and an earlier version of this section
said it should. `caddy reload` is a client of the admin endpoint — it reads
the file, adapts it, and POSTs the result to the running process — so a
rendered `admin off` left the unit's `ExecReload` with nothing to talk to.
The first start worked, and every edit after it failed at "reload", which on a
real box was the second service anyone tried to publish.

So the endpoint stays, used for exactly that and nothing else, and it listens
on a unix socket (`/run/olr/ingress/admin.sock`) instead of Caddy's default
`localhost:2019`. A loopback TCP port is reachable by every local user; the
socket sits in the unit's own root-only runtime directory and Caddy creates it
owner-only. That keeps the property the original decision was after — no
local control surface for anybody but the unit — without breaking the reload
that §5's impact ladder is built on.

Moving the endpoint is the one Caddyfile change a reload cannot deliver, since
the reload has to reach the process at the old address. The plan treats a
change to the `admin` line as a restart for that reason, which is what takes a
box upgraded from `admin off` across the gap without anyone restarting the unit
by hand.

### 7.2a Comments are not changes

Most of the rendered Caddyfile is explanation — the ownership header, and the
reasoning around the `tls` block in particular. Drift detection compares what we
render against what is on disk, so without normalising comments away, **rewording
a sentence in a future olr release would mark every deployed box as drifted and
schedule a proxy reload.** Here that means dropping every connection through it,
for a comment nobody read. The file is still rewritten; it just stops being a
reason to signal the daemon.

### 7.3 Escape hatch

`ingress.raw_caddyfile`, per `design.md` §3.2 rule 5 — passed through verbatim,
declared in our config, rendered by us. Caddy can do vastly more than publish
an internal service, and the answer to all of it is this field plus the
documentation of a tool we did not write.

---

## 8. Failure modes

**This is the only module in olr with state that decays on a clock.** Everything
else is either applied or not; a certificate is applied and then, ninety days
later, stops being valid. That difference drives most of this section.

It also comes with a grace period that is a trap. Caddy renews well before
expiry, so the first failed renewal costs nothing and the tenth costs nothing —
there are weeks of successful serving between the thing breaking and the thing
being visible. By the time symptoms appear the cause is a month old.

| | What happens | What `status` must say |
|---|---|---|
| Renewal fails (token revoked, provider API down, zone moved) | Nothing, visibly. The existing certificate keeps serving until it expires, then every service breaks at once | Days remaining and last successful renewal, as an **active check** — not a log line nobody reads |
| Rendered config rejected | Caught by §7.1's validate; the change is refused | The refusal, with Caddy's own message |
| Upstream client down | 502 | Distinguish *configured but unreachable* from *not configured* |
| Upstream address moved | Stanza proxies to whoever took the address | Prevented at write time by §1.2, not detected after |
| `:80`/`:443` taken | Unit fails to start | Caught at preflight (§6), by the process name |
| Issuance blocked by our own resolver | Nothing ever gets a certificate, with no failing component | Prevented by §4.2's pinned resolvers; it must not be possible to render the file without them |
| A pinned resolver is unreachable from this network | Nothing ever gets a certificate; the status page says "normal for a few minutes" indefinitely, and only the journal names the resolver (`dial tcp …:53: i/o timeout`) | Not prevented. Defaults chosen for reachability (§4.2); the remedy is replacing the entry in `resolvers` |
| A published name is also a client's name | **Silent.** One of the two answers and the other is unreachable by name — today the published name, since the relay answers it before unbound sees the query. Nothing errors either way | Prevented at write time — the two live in one namespace |

The first row is the one that will actually bite people, and it is why
certificate expiry deserves a place in the module's status output rather than
being left to Caddy's logs. The last two share a shape worth naming: **both are
invisible at the layer where they are configured**, and both are cheap to
refuse at write time and near-impossible to diagnose afterwards. That is the
argument for spending validation on them rather than status.

---

## 9. Scope

The boundary §2 said had to be drawn here.

| | | |
|---|---|---|
| **v1** | published services: name + upstream, one wildcard certificate via DNS-01 | the whole product claim of §1 |
| | `:80` → `:443` redirect, LAN-only bind, port preflight | §6 |
| | certificate expiry and renewal state in `status` | §8; the one clock-dependent thing we own |
| | `raw_caddyfile` escape hatch | §7.3 |
| | `olr-caddy.service`, and a drop-in when the binary is elsewhere | §5.5 |
| | names resolved by your own DNS records | §3 — the record is the operator's; we obtain the certificate and proxy the name |
| **v2** | publishing the WebUI itself | §6 |
| | shipping a proxy build, so there is no download step | §5.6 — deferred, not rejected |
| | per-service access control by group or client | the thing a router can do here that a standalone Caddy cannot — and the only entry on this list that justifies the module living in olr rather than in a README |
| **Never** | serving static files, PHP, a general web server | escape hatch, permanently |
| | **exposing a published service to the internet** | that is a port forward: `gateway`'s object and its risk conversation (docs/port-forwarding.md), and it must not become a side effect of publishing something internally |
| | running, updating or backing up the services themselves | §2's boundary. We make an application reachable. We do not manage applications |

---

## 10. Considered and rejected

- **Caddy as an embedded library.** §5.1. Every cost of embedding is real and
  none of them were costs of bundling; the objection was aimed at the wrong
  target.
- **A distro or official-repo Caddy, with `lego` obtaining the certificate.**
  Preserves a stock Caddy at the cost of two backends, two configs, a
  certificate handoff and a renewal status we would reconstruct from another
  tool. The separation of failure domains it buys is real but is not worth three
  new moving parts, and Caddy's certificate automation is the most mature part
  of Caddy. Note this is *not* what §5 now does: we still use one Caddy doing
  its own ACME — it is simply not one we built.
- **Shipping our own xcaddy build** (§5.2). Rejected on the second cost rather
  than the first: the CVE queue was affordable, and a fixed provider list was
  not. It is deferred rather than refuted — §5.6.
- **`caddy add-package` / `caddy upgrade --plugin`.** Fetches a rebuilt binary
  from a third-party build service at runtime and replaces itself in place —
  which `apt` will then overwrite on the next upgrade. Unverified whether it
  even works unattended; rejected regardless, on both counts.
- **HTTP-01, and a local CA.** §4. One requires exposing the router, the other
  requires touching every client the operator owns.
- **Caddy's admin API instead of a rendered file.** §7.2.
- **Naming this module `proxy` or `gateway`.** Both words are taken and mean
  the opposite direction of travel — `gateway.md` uses them for egress and for
  the operator's proxy box. A third word was needed.
- **A Caddy site block per published name**, which is the obvious rendering and
  reads better. Each block would request its own certificate, which is exactly
  the per-service certificate step §1 promises does not exist. One wildcard site
  block with a host matcher per service keeps that promise. The rendered file is
  slightly less readable and no name is ever requested.

---

## 11. Open

1. **Should olr have this module at all** (§2). The positioning question, and
   the only one on this list that can cancel the others. It is not a technical
   decision and should not be settled by a technical argument.
2. ~~**Local DNS override versus a public wildcard record.**~~ **Closed twice,
   the second time the other way.** It was first "closed by discovery" — `dns`
   already served the local suffix from a static zone, so published names were
   written into it and no public record existed at all. That answer required one
   domain for the whole box, which forced a network-wide rename on anyone who
   wanted one HTTPS service (§3.1). A published name is now a record in the
   operator's own public zone: no override, no automatic write into the
   resolver, and no shared suffix. §3 is rewritten around the split.
3. **Where the DNS provider token lives** (§4.1). A rendered `0600` environment
   file is the answer for now, and it is the first time olr has held a
   credential for a third-party account. If a secrets story is ever going to
   exist, this is the workload that starts it.
4. **The `clients` dependency** (§1.2). Referencing a client is right by
   `design.md` §4.1 but pulls `ingress → clients`, and forces the interaction at
   write time. **Built as a refusal carrying the remedy**, not as an offer — a
   dynamic address is refused with the `olr dhcp` command that fixes it,
   because `design.md` §5.6 forbids inferring a change to another module's
   config. Confirm that is the wanted shape before the UI is built on it.
5. ~~**Ordering.**~~ **Answered, and not the way it was asked.** The worry was
   that publishing services before the box had a firewall is hard to defend,
   and that §9's "never expose to the internet" row was a promise a `firewall`
   module would have to keep. There is no such module and there will not be one
   for now (docs/port-forwarding.md §0), so the row is kept by something
   simpler and more robust: olr publishes on a private address and nothing
   routes to it from outside unless somebody writes a port forward down. That
   is one deliberate act by the operator, in a different object, and it is
   exactly the line ingress must not cross on its own.
6. ~~**Who watches Caddy releases, and how fast.**~~ **Closed by not taking the
   obligation.** olr ships no proxy, so Caddy's security updates are the
   operator's distro's problem, as every other backend's already were. It
   reopens the day §5.6 is taken.
7. ~~**The provider list is a second copy.**~~ **Closed by deletion, which is
   the only clean way a question like this closes.** It asked how to keep our
   list in step with the binary's; §5 removed our list. `caddy list-modules` is
   asked directly, the schema publishes no enum, completion is fetched, and the
   validator no longer judges a provider name it cannot verify. This is the
   argument that actually decided §5 — the packaging burden was the visible
   cost and this was the load-bearing one.

8. **Mostly run against a real Caddy now; one thing is not.** Verified against
   caddy v2.9.1: `caddy validate` accepts the rendered Caddyfile in full — the
   wildcard site block, `admin off`, `tls { resolvers … }`, the per-service
   `host` matcher, `handle`, `abort`, and the `transport http { tls
   tls_insecure_skip_verify }` form for an https upstream — and the output of
   `caddy list-modules` parses as expected. What that did not exercise was
   `caddy reload`, and `admin off` — which validates fine — broke it on every
   box (§7.2). Reload through the admin socket was since verified against
   caddy v2.11.4. The refusal path was exercised for real,
   not simulated: the check ran, rejected the config for the right reason, wrote
   nothing to the live path and asked systemd for nothing.

   Two things came out of doing it, both now fixed and neither predictable from
   reading: Caddy prefixes its complaint with a JSON log line, and names the
   temporary file the check was given rather than the file an operator can open.

   **What remains unverified is the part that needs a provider module**: the
   `dns <provider>` line itself, and therefore an actual issuance. A stock Caddy
   has none (§5.4), so this cannot be exercised without building one — which is
   the operator's step by design. Everything up to that line is now known to
   parse.

9. **Nothing checks that the record exists.** §3 makes the name the
   operator's, and the UI says what it will be — but a published service whose
   record was never created, or points elsewhere, looks identical in `status`
   to a working one until somebody opens the URL. A check is cheap in
   principle (ask two public resolvers for the name, compare against this box's
   addresses) and would turn a silent miss into a warning on the page. Open
   because it is a network call on a status path, and because a record that
   legitimately points at a VPN address rather than this box would have to be
   allowed without a complaint.

10. **The UI has not been used against a working certificate either.** The page,
   both dialogs and the confirmation gate render and behave on a real daemon,
   and the disruptive-409 path is covered by `http_test.go` rather than by hand.
   What no one has seen is the screen with a valid certificate on it: every
   state below "issued" has been looked at, and the ordinary one has not.
