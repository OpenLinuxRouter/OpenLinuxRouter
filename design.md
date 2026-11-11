# open-linux-router — Design

Router software for personal, homelab, and small-org networks. Installed as
**packages on an ordinary Linux distro**, not flashed as a system image.

Status: architecture settled. `dhcp` is the first module under construction; its
design is §11. Open decisions at the bottom.

The [network quality design](docs/network-quality.md) describes the proposed
five-category assessment, measurement catalog, data model and staged delivery.

---

## 1. Goals

- **Package-level.** `apt install` turns an existing Debian box into a router.
  No custom ISO, no image flashing, no reformatting. The box stays a normal
  Linux machine you can put other things on.
- **UX first.** UniFi-grade or better. Simplicity over feature count.
  Explicitly *not* chasing enterprise capability.
- **Progressive disclosure.** The default surface speaks the operator's
  vocabulary — networks, clients, fixed addresses — not the daemon's. Every
  mechanism underneath stays reachable, through an advanced view, a declared
  escape hatch (§3.2 rule 5), or plain Linux. **We hide complexity; we never
  hide capability.** This is the rule that reconciles "UX first" with "we do not
  hide Linux": those are not in tension, they describe the default and the floor.
  Corollary — the name of the backend we drive is an implementation detail, and
  belongs in `status`, `logs`, and the escape hatch, not on the common path.
- **API-first, genuinely open.** The HTTP API is the substrate, not a wrapper.
  WebUI, CLI, and MCP are all equal clients of it.
- **Agent-manageable.** MCP server plus skills, so an agent can inspect and
  change the network through the same contract a human uses.
- **Open hardware.** Any x86/arm64 box with two NICs. No vendor lock.
- **Linux is the extension surface.** If you need something we don't ship, you
  don't write an `olr` plugin — you `apt install` it, drop in a systemd unit, or
  add your own nftables table. The distro *is* the plugin system, and it is
  better than any plugin API we could design. Our job is to stay out of its way.

### Non-goals

- Carrier or enterprise features (MPLS, VRF at scale, HA clustering). **HA is
  now structural, not merely descoped:** §10 commits to dnsmasq permanently, and
  dnsmasq has no lease synchronisation, so two boxes can never share DHCP state.
  The same choice caps us at hundreds of clients per network, not thousands.
  Both ceilings are accepted deliberately; neither is a gap to be closed later.
- Fleet-wide provisioning and arbitrary vendor configuration remain outside v1.
  Network equipment inventory and observation support Mercury SE109 Pro and
  OpenWrt, with capability-gated operations and a read-only page per device; see
  [network equipment](docs/network-equipment.md).
- A plugin API or third-party module ecosystem — see the extension surface
  above. Programmatic access is the HTTP API and MCP.
- Reimplementing what the distro already does well (supervision, logging,
  updates, DHCP/DNS/routing daemons). Wrap or defer.

---

## 2. Positioning

Everything adjacent is one of three things:

| Category | Examples | Why it isn't this |
|---|---|---|
| Image-level distros | OpenWrt, VyOS, OPNsense, pfSense, IPFire | You install *their* OS. VyOS is Debian-based but still ships as an ISO with an engineer-facing CLI. |
| Controllers for other vendors' hardware | OpenWISP, RadiusDesk, UniFi Network | Package-level with decent UX, but they manage *remote APs*, not the box they run on. |
| Blog-post DIY | nftables+dnsmasq guides | Hand-edited configs. No UI, no API, no safety net. |

The unoccupied position: *"`apt install` makes this Debian box a real router,
with consumer-grade UX and an API-first control plane."*

---

## 3. Architecture

Core is thin. It is a route table, a schema aggregator, auth, config storage,
and a lockout guard. Nothing more.

```
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│   Web UI     │  │   olr CLI    │  │  MCP server  │   all generated from
│    (SPA)     │  │              │  │   + skills   │   the same schema
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘
       └─────────────────┼─────────────────┘
                         │  HTTP/JSON, unix socket + optional TLS
                  ┌──────▼──────────────────────────┐
                  │  olrd  — routes, schema, auth,   │
                  │        config store, guard       │
                  └──────┬──────────────────────────┘
        ┌────────────┬───┴────────┬────────────┬───────────┐
     ┌──▼──┐     ┌───▼──┐    ┌────▼───┐   ┌────▼───┐  ┌────▼───┐
     │link │     │ dial │    │  dhcp  │   │  dns   │  │gateway │  …
     └──┬──┘     └───┬──┘    └────┬───┘   └────┬───┘  └────┬───┘
     netlink     pppd/dhcpcd   dnsmasq      unbound   nftables +
                                                       netlink
```

### 3.1 The critical inversion: library, not framework

Modules call core helpers. **Core does not call into modules through an
abstraction.** There is no `Module` interface.

This was tested against one question — *does anything actually iterate over
modules?* — and the answer is essentially no:

| Candidate iterator | What it really needs |
|---|---|
| `olr --help` module list | a list of strings |
| OpenAPI spec | concatenation of per-module schema fragments |
| MCP tool definitions | generated from that document |
| UI forms | consumes the document over HTTP; never sees a Go type |
| dependency-ordered apply | not needed — see §5, no cross-module transactions |
| `olr status` | fan-out over a route table |

So the uniformity lives in a **schema document**, not in Go types.

### 3.2 Module conventions

Five rules. No abstraction, no registry, no codegen framework.

1. A module owns a config namespace and its own file under
   `/etc/open-linux-router/`.
2. A module registers HTTP routes under `/api/<module>/`.
3. A module's config is a **tagged Go struct**; JSON Schema is derived from it
   by reflection. That struct is the single source for CLI flags, REST body,
   UI form, and MCP tool definition.

   **A type whose Go shape differs from its JSON shape must say so, or the
   schema publishes a lie.** `netip.Addr` is a struct of unexported fields that
   marshals as a *string*; reflected naively it becomes
   `{"type":"object","properties":{}}`, and every surface derived from that
   document is then wrong about the wire format — while the code keeps working,
   so nothing fails until a UI form or an MCP tool is built on it. Core maps the
   stdlib cases centrally and applies one general rule — *a type that marshals
   as text is a JSON string* — so a module only needs its own
   `JSONSchema()` when it knows something the rule cannot: which strings are
   legal, or that a field is an enum. This is the same trap §10 names for
   durations, one level up: a wrapper that fixes the *encoding* still publishes
   `integer` unless the schema is fixed too.
4. Shared verb vocabulary so the CLI stays predictable:
   `show`, `set`, `add`, `rm`, `status`, `logs`, `enable`, `disable`.
5. Every module exposes a **declared escape hatch** — a config field passed
   through to the underlying backend or ruleset verbatim
   (`dns.extra_unbound_conf`, `gateway.raw_nft`). Declared in our config and
   rendered by us, so it stays single-source and revisioned, unlike editing the
   file out of band. We make the common 95% pleasant; we do not hide Linux.

Modules are mounted explicitly — the list is bounded, so it is a literal list:

```go
// internal/daemon/daemon.go
core.Mount("link",     link.Handler(),     link.Schema)
core.Mount("dial",     dial.Handler(),     dial.Schema)
core.Mount("dhcp",     dhcp.Handler(),     dhcp.Schema)
// …
```

Because the contract is *HTTP routes + a schema fragment*, a module's control
code could in principle be proxied to another process (`core.Proxy("dns",
"/run/olr/dns.sock", …)`) rather than mounted. **This hatch exists and is
deliberately unused** — §3.5 argues against ever taking it, and it is *not* the
mechanism by which backends run out of process. Backends were never in process
to begin with. The two are easy to confuse and mean opposite things:

| | Lives where | Why |
|---|---|---|
| Module control code (render, plan, validate) | **in `olrd`** | §3.5 |
| Backend (dnsmasq, unbound, ours) | **its own unit, always** | §3.5 |

### 3.3 Shared library

What core offers modules (they call it, not the reverse):

- config file read/write with per-module revision history
- config templating and atomic file replacement
- systemd unit management for **backends** (§3.5)
- nftables table helpers (own table per module, documented hook priorities)
- netlink helpers
- structured logging and event publication
- the global apply lock (§3.6) — modules do not take their own

### 3.4 Good citizen rules

Because Linux is the extension surface (§1), the box must remain an ordinary
Linux machine. These are hard rules, not preferences.

**Read broadly, write narrowly.** Observe the whole system honestly — every
interface, route, and nftables table, including ones we don't manage. Write only
to what we own. `olr status` reporting *"3 foreign nftables tables present"* is
a feature; pretending we are the only actor is a bug.

- **`nft flush ruleset` is banned.** It silently kills Docker, podman, libvirt,
  and k8s networking. We create, replace, and delete *our own tables by name*,
  never the ruleset.
- **Documented hook priorities**, so a user can deliberately insert their own
  table before or after ours.
- **Adopt-only.** We never touch an interface that wasn't explicitly adopted
  (§7).
- **No machine-wide service disabling**, with one exception, taken on request
  and never on apply. Disable NetworkManager *on adopted interfaces*, not
  globally. The exception is a **shadow instance of a backend olr itself runs** —
  Debian's `dnsmasq.service` or `unbound.service`, which are second copies of
  daemons olr drives from its own units, competing for one port. Those olr will
  stop and disable, because the operator pressed the button that says so
  (`core.Action`, `olr dns fix`). An **OS component is never stopped**:
  `systemd-resolved` is asked to give up the socket through a drop-in and keeps
  running, because the box resolves names through it — and NetworkManager stays
  interface-scoped as above. The distinction that makes this a rule rather than
  a loophole is *whose daemon it is*: olr will only stand down a copy of
  something it is about to run itself, and only when asked.
- **Generated files are additive** — our own directory, `include`-d into the
  real daemon config, never replacing user files. Ownership header on each.
- **Don't squat shared state.** `/etc/resolv.conf`, the main route table, and
  sysctls are touched only when a module explicitly owns that concern. `dial`
  owns the main table's default route and, when the uplink is static and names
  resolvers, `/etc/resolv.conf` (or systemd-resolved's upstreams): a static
  uplink replaces the DHCP client that would otherwise have supplied both
  (docs/dial.md §4).

**Reuse the distro instead of rebuilding it.** This deletes real scope:

| We don't build | Because Linux has |
|---|---|
| a process supervisor | systemd |
| a logging system | journald |
| an updater | apt |
| a plugin runtime | apt + systemd units |
| DHCP / DNS / routing implementations | dnsmasq, unbound, bird |

**Everything is data, and the data is the operator's.** olr has no secrets
store and is not going to grow one. A DNS provider API token is data, the same
as an adopted interface name or a DHCP reservation, and it lives in the config
document beside them.

The alternative was considered when `ingress` became the first module to hold a
third-party credential, and it loses on the property §10 #5 exists to protect. A
secrets sidecar turns backup from *one file you can read* into a file plus a
second thing that can be restored separately and one day will not be, and a
reference (`provider_token_ref` pointing at an entry elsewhere) introduces a
broken-link failure that a literal value cannot have. Neither cost buys anything
on a single-admin box whose control plane already runs as root.

Two things follow:

- **The document is owner-only.** `core.Store` writes `olr.json` `0600` on
  every save. Startup also tightens existing intent and legacy config files;
  loading the API token tightens its permissions too. Nothing reads the intent
  unprivileged — `olrd` runs as root and the CLI is an API client over the
  `0660` socket. Backups contain credentials and need the same protection.
- **Redaction stays, and is a different concern.** A token must not appear in
  `plan` output, a `show`, or a log line, because those get screenshotted and
  pasted into issues. That is about not broadcasting the operator's data on
  their behalf and has nothing to do with file modes; "no secrets store" does
  not mean "print it anywhere".

### 3.5 Process model

How many processes are **ours**, and what is allowed to live inside them. The
two get conflated easily, because most of the processes on a running box are
not ours at all.

```
  olrd ─────────────────────────────── 1 process, ours, resident
   │
   ├─ link      → netlink               (no process)
   ├─ gateway   → netlink + nftables    (no process)
   ├─ qos       → tc netlink            (no process)
   ├─ clients   → reads                 (no process)
   ├─ system    → D-Bus to systemd      (no process)
   │
   ├─ dhcp      → dnsmasq  ──────────── 1  ours to supervise,
   ├─ dns       → unbound  ──────────── 1  not ours to write
   ├─ dial      → pppd / dhcpcd ─────── 0–1 per WAN
   ├─ wifi      → hostapd  ──────────── 1 per radio (later)
   └─ gateway   → bird     ──────────── 1 (later)

  transient:   olr guard expire   armed only during the §5.5 window
               dhcp-script        per lease event, forked by dnsmasq
```

`dhcp` and `dns` already have their own processes. **The module is not the
server** — it is the controller that renders config and reloads the backend.

**The invariant that decides everything else:**

> **`systemctl restart olrd` must never drop a packet, expire a lease, or break
> a session.**

Which gives a decidable test for where any new code goes:

**Does it have to keep running while `olrd` is stopped?** Yes → its own process
and its own unit. No → a package inside `olrd`.

*Process*, not *binary*, and the distinction is load-bearing rather than
pedantic. Every role ships in one executable — `olr`, which dispatches
`olr internal daemon` and `olr internal dns-relay` for the unit files to invoke
— so the file count says nothing about the process count. What this rule asks
for is an address space `systemctl restart olrd` cannot reach into, and two
units running one binary give exactly that.

The test discriminates correctly on the awkward cases. The netlink watcher that
feeds §6.3's live UI runs continuously inside `olrd`, and that is fine: if
`olrd` dies you lose live updates, not connectivity.

Generalised, this is the same rule as §5.5: **nothing that must survive `olrd`'s
death may live in `olrd`.** Data plane → its own unit. Guard state → a disk
snapshot plus a transient timer. One principle, two mechanisms.

**Modules stay in `olrd`.** Splitting module *control* code into per-module
processes was considered and rejected on three counts:

1. **It breaks §5.3.1, the highest-value mechanism in the design.** "Check the
   pool falls inside `link`'s subnet" is a function call on a pure read. Across
   processes it becomes an HTTP round trip that can fail, plus a TOCTOU window
   in which `link` can change between validate and apply.
2. **The lease→DNS path becomes two hops per lease.** §4.2 gives up dnsmasq's
   built-in fusion and rebuilds it over `dhcp-script`; that fires on every
   renewal.
3. **Startup ordering becomes a systemd graph.** In-process, `link` before
   `dial` before `dns` is the literal list in §3.2. Across processes it is
   `After=`/`Requires=` over six units — and §3.1 found that *nothing iterates
   over modules*. Splitting manufactures exactly the iteration requirement that
   finding removed.

The usual argument for splitting — fault isolation — does not apply. The risky
code is in the backends, and those are **already** separate processes **already**
supervised by systemd.

**Backends are separate processes even when we write them.** The rule is not
"reuse the distro"; it is stronger, and survives us writing our own
implementation. If `olr-dhcpd` ever exists it is a process and a unit — one
more `olr internal …` entry point — never a goroutine, because:

- **A goroutine server means building a process supervisor inside `olrd`** —
  restart policy, backoff, liveness — which is precisely what §3.4 refuses to
  build. As a unit it is free, and §5.4's "is the backend alive" stays one
  uniform D-Bus query with no special case.
- **It is the most dangerous code on the box.** A DHCP server binds `:67` and
  parses unauthenticated packets from every client on the LAN. `olrd` holds
  `CAP_NET_ADMIN` and the admin API. They must not share an address space.
- **§3.2's module rules stay uniform** — render config, reload unit, read
  status. One shape, no exceptions.

**Corollary, and the easy mistake:** we drive our own backend exactly the way we
drive dnsmasq — a rendered config file and a signal — **not** a private RPC
channel just because we happen to own both ends. A private channel reintroduces
two module shapes through the back door, and it costs two things for free: the
backend stops being independently runnable and debuggable, and §3.2 rule 5's
escape hatch stops working for it.

**Privileges: every unit runs as root, with no sandbox** (decided 2026-09-16).

olr's five units used to carry per-unit systemd hardening — `DynamicUser=`,
`ProtectSystem=strict`, capability bounding sets, `RestrictAddressFamilies=`,
the rest. They no longer carry any of it. Each unit runs as root with the
privileges systemd gives a system service by default.

This is a deliberate trade and not a gap nobody noticed:

- **Every failure it produced was ours, and none of them said "sandbox".**
  unbound would not start on a valid config, because resolving `interface:`
  lines calls `getifaddrs()` and the address-family filter had no `AF_NETLINK`.
  Caddy started, bound its ports, and could not reach its certificate
  authority, for the same reason one layer along. `core.holderOf` could never
  name the process holding `:53` because that needs `CAP_SYS_PTRACE` against a
  daemon running as its own user, so every port conflict fell through to a
  guess. The cost was paid in debugging sessions, in features that silently did
  not work, and in one shipped release where DNS could not start at all.
- **The strongest boundary was already gone.** `olrd` holds a root D-Bus
  connection to PID 1 — that is how it drives the backends — and
  `StartTransientUnit` over that bus runs anything with any capability. A
  bounding set on `olrd` could not contain anyone who had reached its address
  space; it only contained us.
- **Getting it exactly right is not today's problem.** Least privilege is worth
  having, and it is worth doing once, deliberately, with a way to test that a
  unit still works under it — not as a directive added per unit from memory and
  discovered to be wrong months later by an operator whose DNS is down.

What this costs, stated plainly so it is not rediscovered as a surprise:
`olr-dnsd` parses unauthenticated packets from every client on the LAN and now
does so as root. That is the unit to harden first when this is revisited, and
the one that should get a real test when it is.

`internal/packaging/embed_test.go` holds the units to this, so a directive added
back in isolation fails the build rather than the box.

### 3.6 Concurrency and privilege inside `olrd`

**One global apply lock.** Every config write across every module takes it. Not
per-module.

The simplicity argument is obvious; the correctness one is better. A global lock
is what makes §5.3.1 sound — validate `dhcp`'s pool against `link`'s subnet,
then apply, with nothing able to move `link` in between. Per-module locks
reintroduce the TOCTOU that validation exists to prevent. The contention budget
is a human clicking a toggle; there is no throughput requirement to trade
against.

**Reads never take it.** §5.4 already requires reads to hit actual system state
— netlink, nftables, the lease file, D-Bus. There is nothing to serialise.

**Apply must be bounded, and this is what keeps the global lock viable.** Apply
is render, reload, return. It **never** waits for convergence: `dial` returns
when `pppd` is started, not when the session is up. Convergence is observed
state (§6.2), streamed over SSE. Without this rule one 30-second PPPoE dial
freezes every other module.

This does **not** forbid post-apply verification (§11.5). "Did the unit come up
and bind?" is bounded and belongs inside apply; "did a client get a lease?" is
convergence and does not. The line is whether the answer depends on some other
client on the network deciding to speak.

**The `dhcp-script` callback bypasses the lock.** It writes *observed* state —
not config, not revisioned (§4.5) — so it lands on a separate endpoint and
cannot deadlock against a running apply.

**Most kernel operations execute no subprocesses.** Per §8's stack, `google/nftables` and
`vishvananda/netlink` are direct netlink and `coreos/go-systemd/dbus` is D-Bus.
Native firewall inspection and editing use `nft` with its standard JSON format
for lossless expression support and validation (docs/firewall.md). This bounded
subprocess receives JSON on stdin without a shell and only replaces the owned
filter table. Automatic generation still uses netlink directly. There is no
`exec.Command("systemctl")`. The sandbox remains:

```ini
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/etc/open-linux-router /var/lib/open-linux-router /run/olr
                /etc/unbound/open-linux-router /var/lib/unbound/open-linux-router
```

Worth stating as a rule, because the first `exec.Command` silently costs all of
it.

The last two are not olr's own directories, and that is the point of listing
them: `dns` renders the resolver's config and trust anchor inside `/etc/unbound`
and `/var/lib/unbound`, because Debian confines `/usr/sbin/unbound` to those
trees and a config outside them is one the daemon may not open (§7, and
`internal/dns.Paths`). A `ReadWritePaths=` built from olr's own directories
alone would fail `olr dns apply` on its first `mkdir` — which is the kind of
thing a block like this is copied out of a document without anyone re-deriving.

Written as the shape the sandbox would take, not as a unit that exists: no
shipped unit carries these directives today, for the reason the units themselves
record.

**Two exceptions exist, and the second one is the shape any future one must
take.** `ingress` runs `caddy validate` on every apply (§7.1 of docs/ingress.md):
a read-only subprocess that writes nothing, so the sandbox above contains it
unchanged. Clearing a missing-backend blocker runs the distribution's **package
manager**, which writes everywhere and cannot run inside this namespace at all —
`ProtectSystem=strict` would fail it on `/var/lib/dpkg` for reasons that read
like a bug in olr. So it is handed to systemd as a **transient unit**, which PID
1 spawns in the host namespace with full root, and the directives above are left
exactly as they are. The rule generalises: *a subprocess that needs more than
olrd's sandbox allows is a subprocess systemd should be starting, not olrd.*
Relaxing the block to fit one command would make the hole permanent and would
apply to every bug in olrd forever; a transient unit is scoped to the one
operation the operator asked for.

**There is deliberately no `CapabilityBoundingSet`, and the reason generalises.**
It used to pin olrd to `CAP_NET_ADMIN CAP_NET_RAW`. That never bought what it
appeared to: olrd holds a root connection to systemd's private bus — it must, it
is how backends are driven — and `StartTransientUnit` over that bus runs
arbitrary code with whatever capabilities systemd grants. The restriction could
not contain anyone who had already reached olrd's address space.

It did contain us. `core.holderOf` answers *"who is already serving DNS"* by
matching a socket inode against `/proc/<pid>/fd`, which needs `CAP_SYS_PTRACE`
as soon as the holder runs as its own user — and dnsmasq, unbound and
systemd-resolved all do. The branch that names the incumbent was therefore
unreachable on every real box, and operators hit a fallback that guesses
systemd-resolved and sends them to edit a file for a package they may not have
installed. Restoring it via `CAP_SYS_PTRACE` would have been theatre twice over,
since that capability is root in all but name.

The directives kept above are a different bet and survive it: they bound what a
*bug* can reach, not what an attacker can. `ProtectSystem=strict` turning a
mistyped path into a loud failure is worth having precisely because olrd never
legitimately wants to write there. **The test is whether a restriction would
ever stop code we intend to write.** One that cannot is cheap; one that can —
and is escapable anyway — costs real functionality for the appearance of rigour. **One exception is already visible and should be taken deliberately rather
than by accident:** `olr logs` (§3.2 rule 4) currently execs `journalctl` in the
*CLI* process, which is outside `olrd` and therefore fine. The moment the WebUI
wants logs, `olrd` needs journal access — and `go-systemd/sdjournal` is cgo,
which §8's `CGO_ENABLED=0` forbids. So the sanctioned form is `journalctl -o
json`, read-only, and it is the only exec `olrd` may ever hold.

**Privilege separation is deferred, not foreclosed.** The one split that could
ever be justified is not per-module but privsep —
network-facing HTTP/TLS unprivileged, netlink and file writes behind a socket.
Skipped for v1: Go is memory-safe, so the classic pre-auth memory-corruption
motivation is weak, and an authenticated admin can legitimately do everything
anyway.

The seam costs nothing to keep, because it already exists for another reason.
§10's test-hardware entry requires isolating netlink, nftables and systemd
behind an interface so the rest is unit-testable off-Linux. **That testability
seam is the privsep seam.** Keeping it clean for tests keeps privsep a cheap
option nobody has to plan for.

---

## 4. Modules

Bounded list. Not expected to grow much. The object they all key off is the
**network** (§4.4), not the kernel interface.

| | Module | Owns | v1 |
|---|---|---|---|
| **Foundation** | `link` | NICs, bridges, VLANs, bonds, addresses (netlink); **networks** (§4.4) | ✅ |
| | `dial` | WAN: DHCP client, PPPoE, static, LTE; IPv6 PD. **Part built:** the static uplink — interface, address, default route in the main table — IPv6 by prefix delegation or a 6in4 tunnel, and dynamic DNS; no DHCP client, PPPoE or LTE (docs/dial.md) | ✅ |
| | `clients` | **client identity** (§4.4); joins presence from leases + ARP | ✅ |
| | `devices` | **managed network equipment**: switches, access points and routers olr reads but never configures; brand adapters, capability-gated observations (docs/network-equipment.md) | ✅ |
| **Services** | `dhcp` | dnsmasq — DHCPv4, DHCPv6 **and RA** (§4.2) | ✅ |
| | `dns` | unbound — **DNS only** | ✅ |
| | `qos` | tc — per-client priority and upload/download limits; see docs/qos.md | |
| | `gateway` | **the boundary between the networks this box serves and everything else, in both directions**: static + policy routes and exits (`olr_route`), egress NAT and port forwarding (`olr_nat`), byte accounting (`olr_stat`); later bird (BGP/OSPF). Absorbed the deleted `firewall` module (docs/gateway.md §0, docs/port-forwarding.md) | ✅ |
| | `firewall` | **native nftables rules**: inspect tables/chains/ordered expressions; edit and persist `inet olr_filter`. Automatic default generation remains available until native rules are saved. The old module of this name was NAT only and moved to `gateway` (docs/firewall.md, docs/port-forwarding.md §0) | ✅ |
| | `remote` | getting back in from outside: **WireGuard and SOCKS5** built as parallel objects (docs/remote-access.md) | ✅ |
| | `wifi` | hostapd — only if the box has radios | |
| **Operational** | `system` | hostname, time, admin users, updates, backup, logs | ✅ |

### 4.1 Dependencies form a DAG

`link` is the foundation; everything references it. A module reads another
module's state **through that module's public API** — it never keeps its own
copy. Single ownership of every fact, so drift is structurally impossible.

Concretely, what the dependents read from `link` is a **network** (§4.4): a
named object with a subnet and a router address. They do not read interface
names, and they do not restate a subnet `link` already owns.

```
link ──┬─→ dial ──┬─→ dns      (upstream resolvers)
       │          ├─→ qos      (WAN iface + rate)
       │          └─→ gateway  (default route, PD prefix, NAT egress iface)
       ├─→ dhcp ──┬─→ dns      (lease hostnames, one-way publish)
       │          ├─→ clients  (leases as presence; identity flows the other way)
       │          └─→ qos
       ├─→ gateway
       └─→ wifi

ingress ─→ dns     (only: the zone the resolver answers, so a published
                   domain inside it is refused)
remote ──┬─→ dns     (the dial-in address peers are told to resolve through)
         └─→ gateway (the dial-in subnet, so peers reach the networks this box
                      serves and the internet behind it)
```

**When a cycle appears, invert one side into a fact subscription.** The real
case: `dhcp` wants to register lease hostnames in `dns`, and `dns` wants lease
data. Direction is fixed — `dhcp` *publishes* leases, `dns` *subscribes*.
(This is precisely why dnsmasq fuses the two; see §4.2.)

`ingress` and `dns` used to be the second such pair, in both directions: names
were published into the resolver's zone, and the suffix was read from it. That
is gone (`docs/ingress.md` §3). A published name is a record in public DNS, in a
zone the operator owns, and the resolver is not involved in answering it — so
there is nothing to subscribe to and nothing to re-apply after an ingress
change. What is left is one read: the zone `dns` answers authoritatively, so
that a published domain falling *inside* it is refused rather than silently
answering NXDOMAIN for a name that exists. That is an invariant about two facts
in one namespace, not a copy of either.

**An arrow is walked when the fact at its tail changes.** Reading through an
API keeps two copies of a fact from existing; it does not keep a *programmed*
one from going stale. dnsmasq is handed an interface name and nftables a set
of subnets, and neither module can notice when those move, because nothing in
its own intent changed — a network moved to another interface leaves DHCP
answering on the old one, and the egress masquerade translating out of it.
So a module whose apply changes what a dependent was built from re-applies
that dependent, in the same operation and under the same lock: one no-op
apply per arrow, and a step reported for the ones that were not. §5.2 still
holds — this is a sequence of independent applies, not a transaction, and a
half-applied change follows nothing until it is re-run. The dependents a
change reaches are `dhcp` and `gateway` (from `link` and `dial`), `dns` and
`gateway` (from `remote`), and `dns` (from `ingress`); internal/daemon's
`dependents.go` is the list.

The `remote → dns` arrow is the one worth spelling out, because the fact at its
tail is not something `dns` could have inferred. `remote` writes this box's
dial-in address into every client configuration it prints, and prints it once —
the file lives on a phone afterwards. A resolver that does not answer there is a
client that reports "connected" and resolves nothing: both modules internally
consistent, and nobody owning the intersection. So `dns` subscribes to the
tunnel's address and answers on it, derived rather than pinned, because the
address is olr's to create and not the operator's to keep correct
(internal/dns/dialin.go).

The `remote → gateway` arrow is the same shape on the data path. A peer's
address is on no network `link` serves, so a client on one of them has no route
back and its reply leaves by whatever gateway that client has. `gateway` reads
the dial-in subnet and rewrites it to this router's address on each network it
serves, because a tunnel that connects and reaches nothing is the DNS arrow's
failure met again — both modules internally consistent, and nobody owning the
intersection (internal/gateway/link.go's DialInView, docs/remote-access.md §6.2).

### 4.2 One backend, one owner

Throughout this document **daemon** means `olrd`, ours, one of; **backend** means
a supervised service it drives — dnsmasq, unbound, `pppd`, hostapd, bird — of
which there are many and none are ours to write (§3.5). The distinction matters
most here, where the two would otherwise both be called "the daemon."

Exactly one module owns any given backend or config file. dnsmasq can serve both
DHCP and DNS — if `dhcp` and `dns` both wrote `dnsmasq.conf`, module isolation
would break at the worst possible place. So:

- **dnsmasq** → `dhcp` only
- **unbound** → `dns` only
- **nftables** → each module writes its *own table*, never a shared ruleset

**`dhcp` owns router advertisement, exclusively.** This is not obvious from the
module's name and is easy to violate by accident, so it is stated here rather
than left implied. dnsmasq's `enable-ra` means the DHCP module is what announces
the default route on every IPv6 network. No other module may run radvd, CoreRAD,
or `IPv6SendRA` — a second RA source on one network is the same class of failure
as two modules writing one config file, except it presents as intermittent
IPv6 breakage rather than as a merge conflict.

The module is therefore misnamed: it is really *LAN client provisioning*
(DHCPv4 + DHCPv6 + RA). The name stays `dhcp` because that is what an operator
will look for, which is itself an application of the progressive disclosure
rule in §1.

### 4.3 IPv6 is not a module

It is a dimension cutting through `dial` (prefix delegation, tunnels), `link`
(a network's static /64), `dhcp` (DHCPv6, RA), `gateway` (forwarding), `dns`
and `firewall`. Modeling it as a module would be a mistake — and so would a
page or a wizard that sets it across modules at once. **Each module owns its
own IPv6 setting, explicitly, on its own page**: `gateway` does not turn
forwarding on because a network gained a prefix, and a network gaining one does
not turn anything else on. Where one module's setting costs another's, the plan
says so as a warning; it never writes the other module's half (decided
2026-09-30, while adding tunnelled IPv6).

**A family-wide *view* is not a family-wide setting.** The Gateway landing page
shows two stacks, IPv4 and IPv6, each listing the same stages — upstream, LAN,
DHCP, forwarding, exit, DNS — with that family's answer at each one. The reading
order is shared; the labels are not required to be. A card is named for the
mechanism its family actually uses: DHCP on IPv4, the module's own name and the
word an operator arrives with (§4.2), and on IPv6 the protocol that is running —
SLAAC, DHCPv6, or both when networks differ. Those are the words an operator
already has; symmetry for its own sake would put a name on the card that the
reader has to translate first. Each card opens the page that changes its
family's half — the IPv4
card the DHCP page, the IPv6 card the address ranges that carry each network's
advertisement mode. This is deliberately the opposite of the wizard above: a
stack card renders another module's state and links to that module's own editor,
so the stacks hold no configuration, produce no plan and apply nothing. The test
is mechanical — if a card needed a control it would have to reuse that module's
editor rather than grow one, and if it needed a draft of its own it would be a
second owner and would not be allowed. The rule stays where a setting lives;
the stacks decide only how it is found (decided 2026-10-21).

**The route table closes the stack, because it is per family too.** Each stack
ends with a Routes card drawn like the six above it: the route count, whether
that family has a default route, and how many of its tables belong to exits or to
somebody else. It is not one of them — every module above writes into the table
and the kernel consults it after all of them — but it is per family, so it closes
the family's stack rather than sitting in a block under both that named IPv4 and
IPv6 a second time and asked the reader to hold two places at once for one
family's routing. The card counts; the rows live on a read-only page
(docs/gateway.md §2.4.1), because a static route is an object of its own and a
page that reported the table must not become a second place to write it (decided
2026-10-22).

**v1 serves RA with SLAAC + RDNSS, and no DHCPv6 at all.** Two facts decide
this, and both are structural rather than matters of taste:

- **DHCPv6 has no default-gateway option.** There is no equivalent of DHCPv4's
  option 3 and there never was; it was deliberately left out. IPv6 hosts learn
  their next hop from RA and nowhere else. So RA with no DHCPv6 is a fully
  working network, while DHCPv6 with no RA is a network where every client has
  an address and cannot route. RA is not the IPv6 analogue of DHCP — it is more
  fundamental than DHCPv6.
- **Android has never implemented DHCPv6, and Google closed the request as
  "Won't Fix (Intended Behavior)."** Any network that relies on stateful DHCPv6
  silently loses every Android client on it. For our audience that is most of
  the handsets on the LAN.

So the smallest correct thing is also the most compatible thing. Stateful
DHCPv6 becomes an advanced mode later, labelled with the Android consequence.

### 4.4 The shared objects: network and client

Two objects are referenced by many modules and owned by one. They are the
reason the module list stays orthogonal without the product feeling like a pile
of daemons.

**A network is one LAN this box serves, by name:** name, VLAN, bridge/members,
subnet, router address. `link` owns it, because `link` already owns addresses
and a second owner of the subnet would be exactly the private copy §4.1 forbids.

Modules key off the **network**, never the kernel interface. `vlan30` is an
implementation detail; `guest` is the thing the operator named, and the thing
that survives being moved to a different bridge. This is what makes a "guest
network" an object with a lifecycle — creatable, renamable, inspectable,
deletable — rather than a write-only recipe whose output is scattered across
four modules. §6.3 composite operations compose *over* networks; they are not a
substitute for having them.

**A client is a thing on the network,** keyed by MAC. It is a **join of two
halves**, and keeping them distinct is the whole trick:

| Half | Example | Owned how |
|---|---|---|
| **Identity** — intent | user-given name, fixed address, blocked | ours, stored, revisioned |
| **Presence** — observed | online, current address, last seen | read-through, never stored as truth |

The exception is what no source remembers once the client has gone: *when it
was last heard* and *the name it last called itself*. Both are sampled in the
background — the time from the neighbour table (REACHABLE entries only), the
name from the DHCP lease — and kept per MAC in the runtime database's
`device_seen` table, apart from olr.json because it is history, not intent. It
can be deleted at any time and costs only that history. See
internal/clients/seen.go.

The remembered name is why a phone left in a bag for two days keeps its name
instead of becoming a MAC: a client's hostname arrives with its lease, and the
lease is twelve hours by default. It is *displayed* while no source can say
anything better, marked `remembered` and stamped with when it was heard, and it
is **never promoted into intent** — the operator's own name stays distinguishable
from the client's claim, and the client's current claim distinguishable from
its history. Detection reads it too, so the picture does not fall back to a
vendor default merely because the lease lapsed.

Clients are a *foundation* object, not an operational one: `dhcp` references
them for fixed addresses, `gateway` for per-client routing and forwards, `qos`
for shaping, `dns` for names. `gateway` must not have to depend on `dhcp` to
write a rule about a laptop. The apparent cycle — `dhcp` needs identity, the inventory needs
lease data — resolves with §4.1's own rule: identity flows one way as config,
lease facts flow the other way as a subscription. Which module owns the
inventory is **open** (§10).

**Naming.** The object is called a **network** at every layer: this document,
the schema (`link.networks`, a pool's `network`), the API, the CLI (`olr net add
iot`) and the UI ("guest network"). It used to be `group` everywhere below the
UI, with *network* as the label on the common path — and a word the operator
never sees is still a word every reader of the schema, the API and the code has
to translate. One object, one owner, one word.

**"Group" is reserved for *client groups*** — operator-named sets of clients
("Serving", "Personal", "IoT") that carry policy: the exit they leave by, and
later parental controls and QoS classes. A group is **exclusive**: every client
is in exactly one, so a policy set on a group never has to be reconciled against
another group's. Groups cut across networks and are a different concept
entirely. They need their own word: using one word for both forecloses the
other, and a rule that applies to "the kids' iPads wherever they connect" is not
a rule about a network. This word used to be "tag", and exclusive membership is
what "group" says that "tag" did not.

Groups **nest**: a group may sit inside one other group ("VMs" inside
"Serving"), up to four levels, and a client may sit in a group that also holds
subgroups. Nesting is cheap *because* membership is exclusive — a client has
one group and a group one parent, so from any client there is exactly one chain
upward, and a policy resolves by walking it. Overlapping sets would have needed
a tie-break at every level; a chain needs none. Built in `clients` as a name
and a parent on each group and a group name on each client; no group carries a
policy yet.

### 4.5 Own the model, never the state

olr's model is canonical and daemon configuration is a **projection of it**. An
operator meets a *client*, not dnsmasq's idea of a host, unbound's idea of a
name and nftables' idea of an address. Parsing a daemon's format dies at the
module boundary; nothing above it sees "column 4 of the lease file."

That applies in both directions, but with one asymmetry that has to stay sharp:

- **The model and the query interface are ours, always** — including for
  observed things. A lease is an olr type.
- **Caching observed state is fine**, and event-fed caching (`dhcp-script`) is
  better than polling.
- **The cache is never the source of truth.** Upstream is.
- **Every observed object carries `as_of`,** so no surface can imply a freshness
  it does not have.
- **The plan/drift path always reads fresh.** This is the one hard exception.
  §5.4 works because planning compares intent against *reality*; comparing it
  against our own cache would make drift undetectable precisely when it matters.

**The guardrail on all of this:** the model's size is bounded by the *product
surface*, not by the union of the daemons' capabilities. If we do not expose it,
we do not model it — it goes through the declared escape hatch (§3.2 rule 5).
Without that discipline, "olr models everything" becomes a second, worse
configuration language for every daemon we drive.

---

## 5. Apply semantics

### 5.1 Immediate apply

`olr dns set upstream=1.1.1.1` takes effect on return — like `ip` and `nft`,
not like VyOS `commit`. No staged commit, no global apply. The GUI follows:
toggling a switch applies instantly, no "Apply changes" bar.

### 5.2 No cross-module atomicity

Each module applies independently and keeps its own revision history
(`olr dhcp rollback`). There is no two-phase commit.

Rationale — the kernel already provides atomicity where risk is highest. An
nftables ruleset load is a single transactional netlink batch: all-or-nothing,
no partial ruleset ever visible. Individual netlink ops are atomic per-op. A
userspace 2PC layer would mostly protect the *low*-risk seams while the
high-risk one is already covered. Bad trade for the complexity.

### 5.3 What replaces it — three cheap mechanisms

1. **Cross-module validation before apply.** Atomic *validation* is far weaker
   than atomic *apply* — pure reads, no coordination. Before `dhcp` changes a
   pool, check it falls inside `link`'s subnet for that interface. Catches most
   real breakage (typos, half-finished renumbering) before anything changes.
   Highest value per line in the whole design.
2. **Idempotent re-apply instead of rollback.** If a multi-step change fails
   halfway, don't unwind — leave it partial, report exactly which steps landed,
   and let a re-run finish the job. More honest and more debuggable than a
   silent revert.
3. **Impact classification.** Every diff reports
   `none | reload | restart | disruptive`, so the UI can say *"this will drop
   all LAN connections"* instead of showing a spinner. Cheap now, impossible to
   retrofit.

### 5.4 Drift is free

Each module's plan step is pure and reads **actual system state**, not a cached
copy of intent. So "have we drifted?" is just: plan against unchanged intent and
see if the diff is empty. `olr status` is that, run across modules. No separate
health machinery.

Two things were hiding under the word "health":

- **drift** — applies to every module, derived as above
- **is the backend alive** — only backend-backed modules (`dhcp`, `dns`,
  `dial`, `wifi`, dynamic `gateway`); mostly a systemd query (§3.5)

### 5.5 Lockout guard

Not a transaction — a dead-man's switch. If admin reachability is lost within 90
seconds of a change and nobody confirms, the change is reverted. One **global**
reachability probe, not per-module health.

The failure this prevents is not an enterprise failure. You are on your laptop,
on the LAN, changing the LAN. Your session dies mid-sequence and now you cannot
run the command that would fix it. Every homelab router does this eventually.

**The guard must not live in `olrd`'s memory.** This is the §3.5 invariant
applied: state that has to survive `olrd`'s death cannot be held by `olrd`. An
in-process 90-second timer dies with the process — and a change catastrophic
enough to need reverting is exactly the kind that takes `olrd` down or reboots
the box. The mechanism would evaporate in precisely the case it exists for.

So the guard is on disk and the timer belongs to systemd:

```
apply    → write the revert snapshot to /run/olr/guard/<id>.json
         → systemd-run --on-active=90s olr guard expire <id>
confirm  → stop the transient unit, drop the snapshot
timeout  → olr guard expire probes reachability, reverts from the snapshot
```

Three properties fall out. It survives `olrd` crashing; it survives `olrd` never
having started; and `olr guard expire` is an ordinary CLI path, so the revert is
inspectable and testable without simulating a wedged daemon.

`/run` rather than `/etc` is deliberate: the snapshot must not outlive a reboot.
A box that came back up is reachable by definition, so an expired guard replayed
after boot would revert a change that turned out to be fine.

### 5.6 Automatic behaviour must be declared, never inferred

We observe foreign state broadly (§3.4) and we may act on it — but only where
the configuration *says so*. The distinction is not pedantry: it is what keeps
§5.4 working. Drift is "plan the stored intent against reality and see if the
diff is empty," so behaviour that changes without appearing in intent makes
drift undecidable — `olr status` can no longer tell "the operator chose this"
from "we drifted into it."

The pattern, using the DHCP server as the worked example:

```
auto   serve unless another server is already answering on this network
on     always serve; refuse to start into a detected conflict, loudly
off    never serve
```

`auto` is a declared value, so intent still predicts behaviour and drift stays
decidable. Silently toggling between on and off without a config value that says
to is what is forbidden. Three obligations come with any `auto`:

1. **Hysteresis, one-way.** Once serving, keep serving until told otherwise.
   A foreign server that appears for ten minutes must not make us flap; flapping
   is worse than either steady state. Log every transition.
2. **Effective state is first-class.** `auto` is not sufficient for status — the
   surfaces must report `effective: serving | standing-by` *and the reason*.
   This is a real cost: a new dimension on the CLI, API and UI. It is also
   better than the boolean it replaces.
3. **Faults must not hide inside it.** "Standing by because a peer exists" is
   `auto` working. "Not running because the backend died" is a fault. If those
   collapse into one status, `auto` becomes the place failures go to hide.

**Refuse, do not disable.** When `on` meets a conflict, we decline to start and
say why. Refusal demands action and leaves intent untouched; auto-disabling
would rewrite intent behind the operator's back and hide a real misconfiguration
— a second DHCP server handing out an unknown gateway is a problem to surface,
not to paper over.

**Be smart at setup, not at runtime.** The moment to resolve a conflict is when
a network is created, as a question the operator has the context to answer
("something is already serving DHCP here — serve anyway, or leave it to them?").
The probe decides what we *suggest*; `auto` is what they pick for the ongoing
behaviour. The two compose. This is also the mode that fits §7's promise that
installing changes nothing: `auto` is the right answer when olr is being added
to a network that already works.

Structural exceptions do not need a config value because they follow from role
rather than from observation: we never serve DHCP on a WAN interface, and never
on an unadopted one.

---

## 6. Surfaces

All four derive from the same per-module schema. **Derive, not fetch** — the
schema is a build-time source for the CLI and a runtime document for the REST,
UI, and MCP surfaces. See §10 resolved, *static command tree*.

### 6.1 CLI — `olr`, an `aws`-style hub

```
olr <module> <verb> [args]        olr dns show
                                  olr dhcp add reservation --mac … --ip …
                                  olr gateway rm forward web

olr status                        aggregate: drift + backend liveness
olr diff                          pending/drifted, per module
olr history | rollback            per-module revisions
olr adopt <iface> | release       take/hand back interface ownership

olr start | stop | restart        manage the service itself — see below
olr listen <addr>                 open the web UI on the network
```

The command tree is registered in Go, so `olr --help` is truthful with `olrd`
stopped and there is no schema to fetch, cache, or invalidate.

**Two tiers.** Most of `olr` is a client of `olrd` on equal footing with the
WebUI and MCP (§1). But `olr start`, `olr stop`, `olr restart` and `olr listen`
manage `olrd` itself, so they cannot be — starting a daemon cannot be an HTTP
call to that daemon, and `status` has to answer when it is wedged. Those
commands talk to systemd directly. The "equal clients" rule in §1 governs
*configuration*; daemon lifecycle is not configuration.

The tier is marked by the `Service:` heading in `--help`, not by a word in the
command path. They lived under an `olr daemon` group until the group was
flattened away: an operator has one program installed and thinks of it as one
program, so `olr start` is what they reach for. Grouping carries the boundary
as well as a prefix did, and asks nobody to learn the word "daemon" first.

The shared verb vocabulary (§3.2 rule 4) is enforced in code, not documented:
constructing a command with a verb outside the vocabulary panics at startup.
Verb drift across modules is invisible in review and obvious at startup.

### 6.2 REST API

`/api/<module>/…` over a unix socket, optional TLS for remote. OpenAPI derived
from the config structs. Read surface includes **observed resources** (leases,
WAN state, stations) declared alongside config but never stored or revisioned.

**Lists get a route per item; scalars do not.** A merge patch (RFC 7386) merges
an object key by key but replaces an array wholesale, so `PATCH /config` serves
a scalar field correctly and cannot serve a list at all — a patch meaning to edit
one pool, policy or exit would drop the others. That is the whole test for
whether a module needs `PUT`/`DELETE` on `…/exits/{name}` and the like.

It matters more than a spelling. Without item routes the *edit itself* happens in
the client: every caller loads the document, splices the list, and sends the whole
thing back. That is two requests where the apply lock covers only the second —
§3.6's lock cannot close a window that spans two requests — and it is one rule
reimplemented per client, because only a Go client can call the module's own
methods. `internal/gateway` is the worked example; the other modules still owe it.

**One request, and the plan decides whether it lands.** A mutating route plans
before it writes. `?dry_run=true` answers with the plan and writes nothing;
without `?confirm=true` a `disruptive` plan is refused with `409` and the plan,
so §5.3.3's warning costs a second round trip only in the case that earns one.
Everything else applies on the first.

**Two listeners, authenticated differently, because they are different things.**
The unix socket is the local admin path and its access control is the socket's
mode and group — filesystem permissions *are* its authentication, and a token
would add nothing a local caller has not already proven. TCP has no such
property. Wrapping both in one scheme would break `olr` over the socket for no
gain.

**The TCP rule is `docs/system.md` §3, and it is about capability rather than
credentials:**

> An unclaimed box cannot be configured over the network.

Until an administrator has recorded how they want to reach this box, TCP serves
the SPA and the single route that records that choice, and answers `409` for
every other API route. Afterwards it requires what they chose — a password, or
deliberately none.

This paragraph used to say something else, and the old text is worth keeping
rather than overwriting, because its instinct is still load-bearing. It said the
TCP scheme was a **single bearer token** in `/etc/open-linux-router/api-token`,
that an unauthenticated listener existed only for development, and that it
refused to bind anything but loopback, because "an open admin API on a router's
LAN address is not a convenience".

The instinct was right and the mechanism was wrong, in a way only a real box
showed. The token could not be *entered*: `BearerAuth` wrapped the whole mux
including `/`, so a browser got a JSON authentication error and never loaded the
page that asks for the token. The documented flow was impossible from a browser
for as long as it was documented. Meanwhile the loopback restriction meant the
only honest options on a LAN were a credential nobody could supply, or nothing.

What replaces it keeps the instinct exactly — an open admin API on a LAN is
still not a convenience — by making the open state **useless** rather than
making it loopback-only. A box nobody has claimed cannot be configured over TCP
at all, so the interval between `apt install` and a human decision is not an
exposure. `docs/system.md` carries the full argument, the source-range rule for
who may claim, and what it deliberately does not defend against.

`auth` therefore lands in `system` as its `access` slice (§10 #1), and is
deliberately not a user model: one box, one optional password, no accounts and
no roles, shaped so that a later identity model replaces it rather than
inheriting from it.

Password authentication is implemented: PBKDF2-SHA256 hashes, temporary
HttpOnly/SameSite browser sessions, login/logout and immediate revocation on
password changes. The TCP handler also validates Host, Origin and JSON content
type. HTTPS and client CIDR restrictions are daemon options. `--auth` retains
the separate bearer token for API clients; the Unix socket remains the local
recovery path. See [administrator access](docs/administrator-access.md) for the
current operator contract and transport requirements.

**Observed resources are not schema-published yet, and that is a gap.** Config
structs are reflected; the read and plan shapes are not, so a typed client has
to hand-write them and finds out at runtime when one drifts. Publishing response
schemas beside the config schema is what closes it, and it is worth doing before
a second module doubles the hand-written surface.

### 6.3 WebUI

SPA embedded in the binary, a pure client of the API.

**Reachable only when asked.** `olrd` binds its control socket always and a TCP
address never, unless `--listen` says otherwise — so a fresh install has a
working `olr` and a UI nothing can reach. That follows from §7: `apt install`
must not put an admin port on a LAN uninvited, and a router is exactly the box
where that matters. The cost is a step nobody guesses, so it is a command rather
than a documented file edit — `olr listen 0.0.0.0:8080` writes
`/etc/open-linux-router/olrd.env`, restarts the unit, and prints where the token
is. Making it *easy* to say yes is a different thing from saying yes on the
operator's behalf, and only the second one is a surprise.

**Composite tasks must live in core, not the UI.** "Set up a guest network"
touches `link` (VLAN), `dhcp` (pool), `dns` (scope), `gateway` (isolation),
`wifi` (SSID). If that orchestration lives in the WebUI, the CLI, the API, and
agents cannot do it — which rebuilds the exact UniFi limitation we're trying to
beat. Composite operations are first-class core operations with their own
routes; recipes state their own step order explicitly.

**Instant, except when it would disconnect you.** §5.1 says toggling a switch
applies instantly, with no "Apply changes" bar; §5.3.3 says the UI should warn
*"this will drop all LAN connections"* instead of showing a spinner. Those only
look contradictory. The rule is: every change is planned first, applied
immediately, and **held for confirmation only when the plan comes back
`disruptive`** — at which point the diff and the affected clients are shown.
The extra round trip buys the one outcome an operator cannot cheaply recover
from, which is being disconnected from the router by their own click. Nothing
else interrupts them.

This is also why `disruptive` has to be a *fact* rather than a guess (§5.3.3):
a classification that cried wolf would train the operator to click through the
one dialog that matters.

Live data (throughput, station lists, DNS query log) streams over SSE.

**The browser cannot authenticate an `EventSource`** — it sends no
`Authorization` header — so consuming the stream from the SPA needs either a
cookie session or a fetch-based reader. Deferred rather than chosen: while the
only event is "something was applied", refetching after a mutation covers it,
and the decision is better made against real live data than in advance of it.

### 6.4 MCP + skills

MCP tools generated from the same schema. Skills shipped as markdown in-repo.
Because plans are pure and diffs are inspectable, an agent can propose a change,
a human can review the diff, and it applies without a bespoke code path.

**Built, read-only, at `POST /api/mcp`.** See `docs/mcp.md`. Three things about
it are worth stating here because they changed this section's assumptions.

*"Generated from the same schema" needed a second half.* The schema says what a
config document looks like; it does not say which routes exist to send one to,
and `http.ServeMux` cannot be asked. Modules now declare their surface as data
(`core.Route`), core builds the handler *from* that table rather than beside it,
and `/api/routes` publishes it. A module cannot serve a route it did not
declare, which is what makes the list safe to generate from — and it is the
OpenAPI document of §9 milestone 5, joined to `/schema`.

*The MCP server is an API client structurally, not by convention.*
`internal/mcp` takes an `http.Handler` and imports no module, so a tool call
cannot skip validation, the §3.6 lock, or the change event. The import graph
enforces it; a test fails the build otherwise.

*Writes wait for §6.2's gate.* "An agent proposes, a human reviews the diff" needs
the disruptive refusal, and only `gateway` implements it — `dhcp`, `dns` and
`clients` still apply on the first request. Until they do not, publishing a write
tool would mean an agent could drop the LAN with nothing to show the approving
human that it would. The read and plan tools are the half that works today.

---

## 7. Adoption and reversibility

**Installing the package must never break your SSH.** Install alone changes
nothing — with one clause, added once a first install was watched rather than
imagined: **install serves olr's own UI on olr's own port, and changes nothing
else.** No interface, no other daemon, no shared port, no route, no resolver, no
DHCP server.

The clause is not a weakening. Opening `:8080` was never able to disconnect an
SSH session, and the reason to require a command for it turned out to be an
over-application of this section to the one surface it does not govern: the
management interface is not shared state the box was already using, so "do not
touch what is already running" has nothing to say about it. Every comparable
management UI — Cockpit, Proxmox, Home Assistant, Grafana — listens on install,
and none of them is thereby a bad citizen.

What makes it safe rather than merely convenient is `docs/system.md` §3: an
unclaimed box cannot be configured over the network, so serving the page costs
nothing that could be taken advantage of before its owner arrives.

- `olr adopt <iface>` — take ownership from NetworkManager/systemd-networkd,
  recording prior state
- `olr release <iface>` — hand it back

  **Built, except the half that takes the interface away.** `olr adopt` and
  `olr release` record consent in the `link` section of the document, and
  `dhcp`, `dns` and `gateway` each refuse an interface that is not in it.
  Adopting is still consent and nothing more: it sets no address and starts no
  service, which is what keeps the first line of this section true.

  Addressing arrived with the network (§4.4), as a second and separate act.
  Putting an adopted interface in a network writes this router's address to
  it, brings it up, and takes every other IPv4 address on it off — a member's
  v4 addressing is olr's (internal/link `PlanAddrs`). olrd puts that address
  back when it starts, because the kernel forgets it on a reboot and nothing
  else on the box knows it.

  Taking the interface from the distribution is **built for dhcpcd** — Debian
  13's ifupdown and Raspberry Pi OS — and only for what olr writes. At the
  moment olr writes an interface's IPv4 (a network member with a subnet, or a
  static uplink), the distribution's DHCP client stops doing IPv4 there and
  keeps doing IPv6, through an `ipv6only` block olr adds to `/etc/dhcpcd.conf`
  between markers and removes again when it lets go (internal/host,
  docs/dial.md §4). Adoption alone still takes nothing.

  What is still owed is the same for NetworkManager, systemd-networkd and
  dhclient. Until that lands, an interface one of them manages has **two
  managers** — olr, and whatever the distribution was told about it — and olr
  reports it, with the step that stops the other one, rather than changing a
  file it has not been tested against.

  That is also why the start-up restore only adds. Enforcing ownership at boot
  would race the distribution's own address sources, and a box that broke the
  first rule would lose its uplink address on every boot, with the way back
  being physical. Too many addresses is a state an operator can fix from a
  shell they can still reach; removal stays with an operator who asked for it.
- generated files live under `/etc/open-linux-router/rendered/`, included into
  the real daemons' configs; user files are never hand-edited. The resolver's
  config and trust anchor are the two exceptions, and they are not a preference:
  a distribution that confines `unbound` by path decides where the daemon may
  read a config from and write a key to, and on Debian that is a directory of
  ours inside `/etc/unbound` and `/var/lib/unbound` and nowhere else. Nothing
  else of the distribution's is touched, and `internal/dns/confinement_test.go`
  holds the split
- every generated file carries an ownership header

---

## 8. Packaging

- **Debian 13 (trixie)** primary, amd64 + arm64. Own apt repo.
- Ubuntu LTS next. Any systemd + nftables distro as a secondary goal.
- `open-linux-router` (core + v1 modules) with optional
  `olr-module-{qos,vpn,wifi,gateway}` splits, since selective install is a real
  payoff of package-level over image-level.

**Stack:** Go for core and modules — static binary, good netlink/nftables
libraries, trivial `.deb`, clean arm64 cross-compile. TypeScript SPA embedded
in the binary.

| | Choice | Note |
|---|---|---|
| Language | Go, `go.mod` floor **1.27** | `CGO_ENABLED=0`; the floor is the current release, not the oldest that compiles |
| HTTP | stdlib `net/http` + 1.22 `ServeMux` | core is thin (§3); a framework would leak into the API contract |
| CLI | `spf13/cobra` | static tree, §6.1 |
| Schema | `invopop/jsonschema`, draft 2020-12 | OpenAPI 3.1 is a superset, so one dialect serves REST, MCP, and UI |
| netlink | `vishvananda/netlink` | |
| nftables | `google/nftables` | direct netlink, no `nft` binary |
| systemd | `coreos/go-systemd/dbus` | §5.4 backend liveness |
| Logging | `log/slog` | stdlib, structured (§3.3) |
| `.deb` | `nfpm` | single binary, cross-arch, no Ruby |

Six direct dependencies for v1. For a router that is a feature.

**The floor is a decision, not a residue.** It sat at 1.23 for as long as
nothing needed more, and the note in the table said exactly that. Tracking the
current release instead is a different stance, and it costs something worth
naming: a clone built with `GOTOOLCHAIN=local` on an older toolchain now
refuses rather than downgrades, so rebuilding olr needs either a current Go or
the default `GOTOOLCHAIN=auto` that fetches one. What it buys is that the
language version stops being the thing that vetoes a dependency on its own —
which is a reason to weigh a dependency, never the whole of one. `make tidy`
still pins the directive afterwards, because a floor that drifts upward every
time a test-only dependency is refreshed is not a floor.

The SPA is a separate budget, spent where the product is judged (§1, "UX first"):

| | Choice | Note |
|---|---|---|
| Build | Vite, TypeScript | output is static files; no Node on the router |
| UI | React + `react-router` | declarative routing, no SSR — the daemon serves files |
| Styling | Tailwind v4 + shadcn/ui | components are vendored into the repo, not a dependency to track |
| Server state | TanStack Query | the entire UI is server state; hand-rolling the cache is the one place real code would be written for nothing |
| Types | generated from the published schema | §3.2 rule 3 — a field added in Go appears in TypeScript |

The asymmetry is deliberate. Node is a build-time dependency only: `go build`
works on a clone with no npm installed, and a binary built that way serves an
explanatory page instead of a UI. Nothing about the router's runtime depends on
this column.

**The binary carries its own installation.** `internal/packaging` embeds the
four unit files and `olrd.env`, and `olr enable` writes them out, so the
standalone download is one file rather than a directory of parts. systemd reads
units from disk and offers no way around that — the question was never whether
they land on disk, only who puts them there.

The `.deb` still installs them as real files rather than calling `olr enable`
from its postinstall, because dpkg has to *own* them: otherwise `apt remove`
leaves four units behind pointing at a binary that is gone. So there is one copy
in the repo with two consumers — `packaging/nfpm.yaml` names those paths, and
the embed reads them — and a test asserts the two lists match, since a unit
added to one and forgotten in the other surfaces months later as a missing
service.

`olr enable` executes no subprocesses either. §3.6's constraint is on `olrd`,
but the mechanism it forced — systemd over D-Bus, in `core.Unit` — is already
there, and `Enable` ends with the daemon-reload that makes the units just
written visible to the manager. A second mechanism would only be a second thing
to keep correct.

**The version is a file, not a tag.** `VERSION` at the repo root holds a bare
number — `0.1.0`, no leading `v` — and the Makefile derives everything else from
it: what `olr version` prints, what dpkg sorts on, what the tarballs are
called, what a release publishes. A tag records only *which commit claims that
number*, so cutting a release is a reviewable commit followed by a matching
`v0.1.0` tag, and CI refuses to publish when the two disagree (`make
version-check`).

The alternative — deriving the version from `git describe` — was what this
replaced. It makes the number appear for the first time in a command typed at a
shell, where a typo becomes a published release with no diff to review, and it
gives a clone that fetched no tags a different answer than CI. The one thing it
did buy is kept: a build that is not the tagged commit reports `0.1.0-dev`
(and `-dirty` for an edited tree), so a working-tree binary can never claim to
be the released artefact.

---

## 9. Milestones

1. **Get a box online.** `link` + `dial` + minimal core (config store, routes,
   `olr` skeleton). Success: WAN up via DHCP and PPPoE, `olr status` truthful.
   **`link` must land networks (§4.4) here**, not later — every module after this
   keys off them, so retrofitting the primary key is the one sequencing mistake
   that would be expensive.
   Partly done: `link` has landed networks and owns the router's address on each
   network, restored when olrd starts, and `dial` has landed the **uplink** —
   an interface, a static address and the default route in the main table,
   likewise restored when olrd starts. The two together are the first
   arrangement in which a box with one NIC facing a modem and one facing a LAN
   can be brought to a working state entirely inside olr.
   Still owed: taking an interface from the distribution's network manager (§7),
   bridges and VLANs, and three of `dial`'s four WAN forms — the DHCP client,
   PPPoE and LTE, each of which brings a backend to supervise and an address
   discovered rather than typed. IPv6 and prefix delegation are owed on both
   sides. So "WAN up via DHCP and PPPoE" is not met; WAN up via a static
   address is (docs/dial.md).
2. **Make it a router.** NAT + `dhcp` + `dns`. Success: a client on LAN reaches
   the internet with no hand-edited config.
   Done, and by a different route than this assumed: NAT arrived as `gateway`'s
   egress masquerade on `dial`'s uplink (docs/gateway.md §3.9) rather than as a
   `firewall` module's zones, which are not being built (docs/port-forwarding.md
   §0).
3. **Make it safe.** Lockout guard, per-module revisions, impact classification.
4. **Make it visible.** WebUI shell, `clients` inventory, live throughput.
   The inventory did move earlier: identity landed with the client list, which
   is what unblocked the §11.1 fixed-address surface.
5. **Make it programmable.** OpenAPI publication, MCP server, skills.
   Partly done: the MCP server reads (§6.4, `docs/mcp.md`), and `/api/routes`
   plus `/api/schema` are the two halves an OpenAPI document is a join of.
   Still owed: writes, which wait on §6.2's disruptive gate reaching `dhcp`,
   `dns` and `clients`; the document itself; skills.
6. **Then:** `qos`, `remote`, `gateway`, `wifi`.

**`remote` was called `vpn` here until it was built**, and the rename is
recorded rather than applied silently because the reserved name was the narrower
one. Two of the three objects that module is meant to hold are not VPNs — a
SOCKS proxy borrows an exit and gives no access to the network at all — so `vpn`
would have described a third of it and mis-described the rest. `remote` is what
the operator is doing. docs/remote-access.md §10 has the argument; the package
split named in §8 keeps the old spelling until there is a split to name.

**Order taken so far, and why it departs from the list.** `olrd`, the core
control plane and the WebUI shell landed before milestone 1, against a single
module (`dhcp`) driven over HTTP. The reason is that the load-bearing bet of
this whole architecture is §3.2 rule 3 — *one tagged struct drives CLI, REST, UI
and MCP* — and nothing tests that bet until one struct has actually reached a
browser. It paid immediately: reflecting `netip.Addr` and a duration wrapper
published the wrong types on every surface, a defect invisible from Go and
uncorrectable later without breaking clients.

What that jump did **not** buy is milestone 1. Pools were keyed by kernel
interface name, so the DHCP screen was keyed on something §4.4 says is an
implementation detail, and it was scaffolding until `link` landed networks. They
are keyed by network now, which is the retrofit this paragraph was warning
about.

**A minimal `link` landed early, and it was not milestone 1 either.** At first it
owned one field — the list of adopted interface names — plus a kernel reader for
the observed half. It existed because the adopt-only rule (§3.4, §7) was being
enforced by three modules against a fact with nowhere to live: `dhcp`, `dns` and
`gateway` each refused an interface nobody had handed over, `olr adopt` was a
stub, and the flag was reachable only by hand-writing a JSON file that olrd's
own systemd unit never passed. The result was that a packaged install could not
configure DHCP at all — every pool failed validation with "no such interface". A
safety rule nobody can satisfy is not a safety rule.

It has since grown the network. `link` lands networks, pools are keyed by them,
and applying one writes the router's address to its member, brings it up and
removes any other IPv4 address there, with olrd putting the address back when
it starts. What it still does not do is bring up the WAN or take an interface
from NetworkManager, systemd-networkd or ifupdown — §7 has what that leaves the
operator to do in the meantime — and milestone 1 still owes both.

Two things about it are worth keeping when the real module arrives. The observed
half — addresses, up, carrier — is read from the kernel per request and never
stored, so §4.5 holds by construction rather than by discipline; the file it
replaced was a second copy that could and did disagree with the machine. And
adoption stays a stored half of its own, apart from the network, which is what
keeps `olr adopt` a one-sentence write that touches nothing on the box — the
property §7 needs when it promises that installing changes nothing. Addressing
belongs to the network, a second and deliberate act.

---

## 10. Open decisions

1. **`access` module split.** Assumed here as three concerns —
   `firewall` (zones/rules/NAT/forwards), `clients` (inventory, blocking,
   parental), `auth` (who may log into olr, folded into `system`).
   Confirm this matches the intent.

   **The first third is answered, and not as assumed.** NAT and forwards are
   `gateway`'s (§4); zones and rules are not being built at all for now. So
   there is no `firewall` concern left in this split — see
   docs/port-forwarding.md §0.

   **The `auth` third is answered: `docs/system.md`.** It lands in `system` as
   the `access` slice, exactly as assumed here, and is scoped to one box with
   one optional password — no accounts, no roles. The rest of the split is
   still an assumption awaiting confirmation.
2. **Immediate apply** (§5.1) — assumed yes; confirm.
3. **Single box or multi-node?** v1 is single box either way. But if managing
   APs/switches is the destination, the schema needs a node dimension now
   rather than later.
4. **Wi-Fi on-box or bring-your-own AP?** On-box hostapd is a driver and
   regulatory rabbit hole. Assuming wired router + separate APs on a VLAN trunk
   is far cheaper and matches most homelab setups.
5. **Storage boundary: JSON intent, SQLite runtime data.** Operator intent
   remains in `/etc/open-linux-router/olr.json`. OLR-owned runtime state and
   bounded observation history use `/var/lib/open-linux-router/runtime.db`.
   See `docs/storage.md` for schema, migration, recovery and backup behavior.
   The previous no-database decision is superseded: future analysis
   needs queryable runtime data, while raw-event retention remains an explicit
   feature decision rather than a consequence of choosing SQLite.

   nftables remains the source for current byte counters, dnsmasq for leases,
   and the DNS relay's current query ring remains in memory. SQLite stores
   retained samples and OLR's own recovery/session records. Flash wear and
   privacy still require bounded retention and batched writes; selecting a
   database does not itself justify writing every DNS query or flow to disk.
   Explicitly enabled client inspection is a separate, bounded archive described
   below; ordinary traffic is not archived by enabling SQLite.

   **Client activity exception:** a bounded sampler now retains up
   to 90 days of half-hour counter differences for the client-card dot matrix
   and the total beneath it, which is that matrix added up rather than the
   kernel's cumulative counter.
   It samples every ten seconds independently of browsers, respects traffic
   counting being off, caps retained address/bucket pairs at 250,000 and never
   stores raw flows. History is atomically checkpointed every minute and on
   graceful shutdown to SQLite (0600). Only changed buckets are checkpointed;
   the old `activity.json` is imported once.
   Missing intervals, resets and initial baselines are
   not counted as zero traffic. Samples crossing half-hour boundaries are
   omitted rather than attributed to the wrong interval. The display joins
   current client addresses, so address changes can hide earlier samples and
   reused addresses can include another client's activity. These are sampled
   volumes, not exact accounting totals. Restart restores retained buckets but
   starts new counter baselines; abrupt termination may lose the latest minute.

   **Continuous client inspection is an explicit retention exception.** Its
   per-client switch survives stop/start of the control daemon, deployments and
   reboots. On packaged Linux systems the proxy is a separate systemd service,
   so an OLR daemon restart can reattach to it without interrupting capture.
   The live mitmweb viewer keeps only its latest 1000 flows in memory, while a
   separate archive writes full native `.mitm` files after each captured HTTP
   request completes or fails. Saved content survives turning inspection off
   and proxy/control-daemon restarts. A still-running request is not yet a
   completed archive and may be lost if the proxy terminates; archive errors
   are reported separately from live interception state.

   Inspection defaults to 30 days and a **5 GiB total router-wide archive cap
   shared across all clients**, with oldest-first removal. Operators can set
   1–365 days and 1 MiB–1 TiB. The UI always exposes retained per-client history,
   including when another client is active, with search, cursor pagination,
   inline headers/body previews and full native-flow downloads. Clearing a
   client's saved requests requires confirmation and leaves site activity and
   other clients' archives intact. Storage-limit controls explicitly name their
   global scope. Inspection site activity retains per-client samples with
   4-hour, 24-hour, 7-day and 30-day views after interception stops or restarts;
   control-daemon downtime remains an unsampled gap. Site activity has its own
   30-day/64-MiB bound across clients, separate from request archive limits.
   Filesystem overhead and transient files can need additional disk space.
   This does not create a complete browsing history or change the DNS relay's
   recent-memory log.
   See `docs/client-inspection.md` for setup, API and capture boundaries.

   Configuration revision history and rollback remain separate, unimplemented
   features. **Intent stays a JSON file** (§3.2 rule 1): "SSH in and read the
   config" remains the router's configuration recovery path.
6. **Does creating a network default DHCP on?** Leaning yes — a LAN without
   DHCP is the unusual case, and §5.1 immediate-apply makes it cheap to undo.
7. **What is a network, exactly?** (§4.4) Its field set is now the most
   load-bearing schema in the project: `link`, `dhcp`, `dns`, `gateway` and
   later `wifi` all key off it. Worth designing deliberately rather than
   growing.

### Resolved

- **`clients` owns the inventory, and it is a module of its own.** (was open
  decision 6, §4.4) `clients` is promoted to v1, renamed to `clients` after the
  object it owns, and grows the intent half: MAC, name, category, model, notes.
  The alternatives both failed on §4.1. Identity in `dhcp` would force
  `gateway` to depend on `dhcp` to write a rule about a laptop, which §4.4
  forbids in as many words. A separate identity module *beside* a `clients`
  reader would split one object across two owners and leave the join homeless.

  Presence stays read-through and arrives via a consumer-declared interface, the
  way `dhcp` reads `link` through `LinkView`: `clients` never imports `dhcp`, and
  the lease adapter is wired in `internal/daemon`. What was going to be the `clients`
  module — conntrack, wifi stations — becomes further presence sources behind
  that same interface rather than a second module with a second name for one
  object.

  **The fixed address does not move.** `dhcp` keeps owning it; the client list
  joins against it per request. §11.1 asked that the *workflow* start from the
  client, not that the fact change hands — and moving it would have been the
  private copy §4.1 exists to prevent. This unblocks that surface.

- **The client list reads ARP, from `/proc/net/arp`.** (was open decision 7)
  The screen is called Clients, so it has to be one: a lease-derived list omits
  the statically-addressed printer, and operators notice. Reading the kernel's
  neighbour table costs no new dependency, which is why it beat the netlink
  library — `go.mod` has three direct dependencies and that restraint is worth
  more than IPv6 coverage in v1.

  The cost is stated rather than hidden. `/proc/net/arp` is IPv4-only, so ND
  waits for `link` to bring netlink in properly (milestone 1), and a v6-only
  client is invisible to both sources until then. A client that neither takes a
  lease nor answers ARP is absent until added by hand, and the screen says so
  next to its `as_of` rather than implying the list is complete. On a machine
  with no `/proc/net/arp` at all — every non-Linux developer box — that is
  reported as a problem on the response, never swallowed.

- **HTTP stays stdlib; no web framework.** Reaffirmed against a concrete
  proposal to adopt `gin`, and the deciding argument was not taste. A framework's
  binding tags (`binding:"required"`) are a *second* source of requiredness on
  the same structs that this document already derives `required` from — the
  absence of `omitempty` (see *Config format: JSON* below). Two sources for one
  fact is what §4.1 forbids, and here they would disagree silently, in the
  document every other surface is generated from. What remains of the framework's
  value is routing that 1.22 `ServeMux` already does, middleware that is thirty
  lines, and SSE that is `http.Flusher` either way — against a dozen transitive
  dependencies inside the process holding `CAP_NET_ADMIN`.
- **No database in the control plane, for now.** Also reaffirmed against a
  concrete proposal. Config is a file per module (§3.2 rule 1) and stays one;
  SQLite's real candidacy is revision history and throughput series, which is
  open decision 5 above rather than settled here. The distinction worth keeping
  is *which data* — intent, which must survive being read by a human with a
  broken box, versus derived history, which never has to.
- **The API's auth floor is a bearer token** (§6.2), scoped to the TCP listener
  only, with the unix socket relying on its file mode. Chosen because the
  alternative was not "wait for `auth`" but "ship an unauthenticated admin API",
  and because a token is the one credential shape that does not prejudge what
  `auth` will be.
- **Coexistence stance.** Previously open. Settled by §1: if the answer to
  *"I need X"* is *"install X the Linux way,"* then never breaking X is a
  requirement, not a courtesy. We take exclusive ownership of a narrow, declared
  scope and are rigorously non-invasive outside it (§3.4).
- **Third-party modules.** Not needed as a plugin API — the extension surface is
  the distro. Modules stay a bounded in-tree list, mounted explicitly (§3.2).
- **Language: Go.** Confirmed against Rust rather than assumed. This project is
  ~90% config rendering, schema plumbing, HTTP, and process supervision and ~10%
  netlink — Rust's advantages land on the 10% that §3.4 gives away to existing
  daemons. Two things decided it. Runtime reflection makes one tagged struct
  drive CLI, REST, UI, and MCP (§3.2 rule 3); `schemars` is compile-time, so the
  surfaces would need a hand-written interpreter. And `google/nftables` is pure
  Go over netlink, where Rust's `rustables` wraps libnftnl — a C dependency that
  would compromise both the static binary and the arm64 cross-build.
- **Static command tree.** The CLI does not fetch a schema from `olrd` at
  runtime. Alternative considered and rejected: fetch-and-cache, which would let
  `olr` drive a newer or remote daemon but makes `olr --help` depend on the
  daemon being reachable. See §6.1.
- **CLI is two-tier.** The `Service:` commands — `start`, `stop`, `restart`,
  `listen` — are below the API; everything else is a client of it (§6.1). §1's
  "equal clients" governs configuration, not lifecycle. The tier used to be a
  command prefix (`olr daemon …`) and is now a heading, which changed where it
  is visible and nothing about what it is.
- **Process model: one resident `olrd`; modules in, backends out** (§3.5).
  Settled by the invariant that restarting `olrd` must never disturb traffic.
  Two alternatives were considered and rejected:
  - **Module-per-process.** Rejected on §5.3.1 (cross-module validation becomes
    a fallible round trip with a TOCTOU window), on the per-lease `dhcp-script`
    path becoming two hops, and on startup order degenerating into a six-unit
    systemd graph — reintroducing the module iteration §3.1 had eliminated.
  - **Socket activation with idle exit.** Rejected: it optimises idle RSS, the
    cheapest resource on the box, and pays for it where it hurts. A monitoring
    loop calling `olr status` becomes a cold spawn per poll, each re-reflecting
    the schema, so the access pattern a router actually sees is the one it
    handles worst. It also makes `inactive` the healthy state in
    `systemctl status`, adds a second unit, and adds an idle timeout with no
    principled value. Keeping `olrd` resident also preserves §6.1's two-tier
    rationale unchanged, since `olr start` remains a real operation.
- **`olrd` is a control plane, not a worker.** It may cache only what is derived
  and cheap to rebuild — the reflected schema, the route table, the OpenAPI
  document. Never config, never observed state, never revision history. The test:
  `kill -9 olrd` followed by a restart must not change a single answer the API
  gives. Residency is what makes this rule necessary — a long-lived process is
  somewhere to put state, and §5.4's drift detection silently stops being true
  the moment intent is compared against memory instead of reality (§4.5).
- **One global apply lock, and apply is bounded** (§3.6). Not per-module: a
  single lock is what makes §5.3.1's validate-then-apply free of a TOCTOU
  window. Viable only because apply never waits for convergence — it renders,
  reloads and returns, leaving "is the WAN actually up" to observed state.
- **Vocabulary: `daemon` is `olrd`, `backend` is what it drives** (§4.2). The
  document previously used "daemon" for both, which made the old
  `olr daemon status` read ambiguously against "is dnsmasq alive" — two
  different questions with two different answers. The word survives in this
  document and in the code, where the distinction is load-bearing; it no longer
  appears in the CLI, where it only ever asked the operator to care.
- **Config format: JSON.** Round-trips through the same struct tags as the REST
  body and the reflected schema, so there is no second dialect and no mapping
  layer. TOML would be friendlier to read and was rejected for that duplication.
  Three consequences:
  - `time.Duration` needs a wrapper type, or a 12h lease serialises as
    `43200000000000`.
  - JSON has no comments, so the §3.4 ownership header becomes a `"$schema"`
    key pointing at the published schema — which also makes the file
    editor-validatable. Generated *daemon* configs keep a real comment header;
    dnsmasq and nftables formats both support one.
  - Reflection derives `required` from the absence of `omitempty`, not from a
    tag. So core publishes two projections of every module schema: full for
    PUT, relaxed for PATCH and `olr set`. Without this a single-field update
    fails validation against its own schema.
- **DHCP backend: dnsmasq. Permanently — not "for v1".** dnsmasq serves DHCPv4,
  DHCPv6 **and RA** in one daemon; Kea does no RA at all and would need radvd as
  a third daemon — decisive given IPv6 is a dimension, not a module (§4.3).
  Two obligations follow: the module **must** render `port=0` or dnsmasq
  silently becomes a second resolver and violates §4.2; and the lease-to-DNS
  publish path §4.2 gives up has to be rebuilt over `dhcp-script`.

  Three alternatives were considered properly and are now closed:

  - **Kea.** The original objection was licensing — trixie ships Kea 2.6.3,
    predating the July 2025 Kea 3.0 hook relicensing. **That objection has since
    expired: trixie-backports carries Kea 3.0.3.** The decision stands anyway,
    on daemon count (Kea is two or three processes to dnsmasq's one) and on
    still needing a separate RA source. Recorded so the next reader does not
    re-derive it from the stale premise.
  - **A DHCP library in-process** (`insomniacslk/dhcp`). Real prize — it would
    delete the entire render/plan/apply file machinery, the reload-vs-restart
    distinction, and lease-file parsing. Rejected on scope and blast radius:
    the library is packet marshalling plus a handler signature, so allocation,
    lease persistence, conflict detection and the DHCPv4 interop tail are all
    ours, and **RA is not in it at all**. A bug takes the LAN down. Our
    differentiation is UX, API coherence and package-level install — not
    protocol implementation.
  - **Owning RA alone** (`mdlayher/ndp`, ~1.2k lines, stateless, bounded blast
    radius — a broken RA loses IPv6 while IPv4 keeps routing). Genuinely the
    better half to own if we owned one, and the inverse of what the
    "use a DHCP library" framing suggests. Declined under the same "no custom
    protocol code" line; dnsmasq's `constructor:` handles prefix-following and
    the renumbering deprecation dance, which is the hard part.

  **Consequences of "permanently":** the `Backend` interface is deleted — by
  §3.1's own test, nothing iterates over backends and there is exactly one — and
  we lean *into* dnsmasq's idioms (tags, `dhcp-hostsdir`, `ra-param`,
  `dhcp-script`) rather than hedging toward a portable abstraction. HA and the
  thousands-of-clients ceiling become permanent product boundaries (§1
  non-goals), not gaps to close.

- **The network is its own object** (§4.4), owned by `link`, and modules
  key off it rather than off kernel interface names. Considered and rejected: a
  separate `network` module, which would own a name and a subnet that `link`
  must know anyway — two sources for one fact, which §4.1 forbids. Consequence
  for `dhcp`: the pool's primary key is a network, its range is derived from the
  network's prefix by default, and most of the cross-module validation §5.3.1
  celebrates stops being *needed* for the common case, because the subnet is
  given rather than asserted and re-checked.

- **The object is called a `network`, at every layer.** It was first a
  `segment`, renamed on comprehension: "segment" is a network engineer's word
  for a broadcast domain, and the audience for this product is not reliably a
  network engineer. It was then a `group` in the schema, the API and the code
  while the operator always saw a *network* — and "group" turned out to be
  unclear for this object to everybody who met it below the UI. So the schema
  says what the page says: `link.networks`, a pool's `network`. An `olr.json`
  written with the old keys is read as-is and rewritten once when olrd starts.

  One consequence, and it is the reason this entry exists rather than being a
  silent find-and-replace: **the word "group" goes back to sets of clients**,
  which is what it was reserved for before the network borrowed it (the concept
  was called a **tag** in between). A client group is exclusive — each client in
  exactly one — and carries policy such as the exit (§4.4). Recorded so the next
  reader does not find "group" in an old commit and assume it means a network.

- **Progressive disclosure** (§1) as the rule reconciling "UX first" with "we do
  not hide Linux." They describe the default and the floor, not a tension.
- **Test hardware.** Previously open. Resolved by workflow rather than
  purchase: code is authored and built on x86_64 Linux, cross-checked for
  arm64, and end-to-end tested by hand on real hardware. Consequence for
  layout — keep renderers, lease parsing, schema, config store, and validation
  free of Linux-only imports so they are unit-testable anywhere; isolate
  netlink, nftables, and systemd behind an interface.

---

## 11. `dhcp` module design

First module under construction. It drives dnsmasq (§10) and owns DHCPv4,
DHCPv6 and RA (§4.2).

### 11.1 Object model

Two nouns, both defined in §4.4, neither of them owned here:

- **Network settings** attach to a **network**. `link` owns the network; `dhcp`
  owns what it serves there.
- **A fixed address** is a property **of a client**, not a standalone row. It is
  set from the client list, not by typing a MAC into a form. The difference
  between those two sentences is the difference between the OPNsense experience
  and the UniFi one.

Modelling fixed addresses as a flat MAC→IP table is the failure mode to avoid;
it is what forces every workflow to start from a hardware address the operator
has to go and find. `clients` now owns identity (§10, resolved), so this surface
is unblocked: the client list is where a fixed address is set, with the MAC
already known, and `dhcp` still owns the reservation itself.

### 11.2 Per-network settings

| Field | Default | Note |
|---|---|---|
| `dhcp` | `on` | `auto \| on \| off` per §5.6 |
| `range` | derived | from the network prefix; explicit overrides |
| `lease` | 12h | |
| `dns` | the router | |
| `domain` | the base domain | |
| `ipv6` | `on` | SLAAC + RDNSS (§4.3); `stateful` is advanced and labelled |

**The acceptance criterion for the UX goal is that creating a network takes only
a name:**

```
olr net add iot
```

deriving the next free /24, gateway `.1`, DHCP on, a range that leaves a low
block free for statics, DNS = the router, IPv6 on, lease 12h. Everything
overridable, nothing required. If that command needs four flags, §1's "UX first"
has not been met — this is a test, not an aspiration.

The derived range matters for a second reason: the real collision hazard is not
a fixed address inside the dynamic range, it is a **statically-configured client**
inside it, which DHCP will hand to someone else. dnsmasq has no exclusion
primitive, so the only defence is a range that deliberately leaves a static block
free. Deriving it gives that for free; asking the operator to type it does not.

### 11.3 The guarantee the file layout exists to buy

dnsmasq re-reads its hosts and options directories on SIGHUP but never re-reads
its configuration file. Reservations and per-network options therefore live in
directories, and that asymmetry is the whole reason for the layout. Stated as a
promise, because it is user-facing and testable:

> Adding, changing or removing a fixed address **never interrupts service**.
> Changing a range restarts the server, but clients keep their addresses.
> Disabling drops clients.

Mapped onto the §5.3.3 impact vocabulary, in client terms rather than daemon
terms:

| Impact | What a client on the LAN experiences |
|---|---|
| `none` | nothing |
| `reload` | nothing |
| `restart` | nothing; a request arriving mid-restart is retried |
| `disruptive` | loses the address it holds |

`disruptive` is computed from the **live lease database**, not from comparing
config fields — a range change is disruptive exactly when someone currently
holds an address the new range no longer covers. That is what makes it a fact
rather than a guess.

### 11.4 Scope

| | | |
|---|---|---|
| **v1** | per-network `auto\|on\|off`, derived range, fixed addresses, lease/DNS/domain, IPv6 SLAAC+RDNSS | |
| | pool usage, foreign-DHCP-server detection, recent DHCP events | "why did this client not get an address" is the single most common support question; these three answer it |
| **v2** | PXE / netboot (`dhcp-boot`) | high demand in *this* audience — Proxmox, netboot.xyz |
| | deny DHCP to a client (`dhcp-ignore`) | belongs with inventory blocking, not here |
| | option 121 static routes, stateful DHCPv6 | escape hatch until then |
| **Never** | DHCP relay | different object shape; settled once, not revisited |
| | per-vendor-class options, multiple pools per network | escape hatch, permanently |

### 11.5 Known gaps

- **Post-apply verification — closed.** A dead DHCP server is invisible for
  hours and then breaks everything at once when leases expire, and §5.5's
  lockout guard does not cover it: the operator keeps their own session
  throughout. Apply now watches the unit for a bounded window after the service
  action and fails if it does not stay active, which §3.6 permits explicitly.

  The window is the point, not the check. The unit is `Type=simple`, so systemd
  reports the start job `done` the moment the process is forked — before
  dnsmasq has read a config file or bound a socket. A single sample straight
  after the job therefore passes for a backend that is about to exit. Asserting
  on UDP/67 instead was considered and rejected: a network serving only RA never
  binds it, so that check would fail exactly where IPv6 is configured
  correctly. Staying alive is the stronger signal anyway, because dnsmasq exits
  on a config it rejects rather than idling.
- **`dhcp-script` is not wired up.** It is the only real IPC dnsmasq offers, and
  it is what turns leases from a polled file into an event stream — the live
  client list (§6.3 SSE), the §4.1 lease publish to `dns`, and the event history
  above all depend on it. Highest-leverage single item in the module.
- **Drift is byte-comparison against rendered files,** so changing a comment in
  the renderer marks every deployed install as drifted and schedules a restart on
  next apply. Inherent to the approach; needs a deliberate answer before there
  are installs in the field.
