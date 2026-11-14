# open-linux-router

Router software for personal, homelab and small-org networks — installed as a
package on an ordinary Linux box, not flashed as a system image.

`apt install`, and the Debian machine you already have becomes a router with a
web UI, a CLI, a REST API and an MCP server, all speaking to the same
configuration. It stays a normal Linux machine the whole time.

**Status: early.** The features below are implemented, but this is not yet a
finished router. Interfaces must be handed to olr explicitly; it does not
create bridges or VLANs. QoS policies can be saved but do not shape traffic.
[docs/install.md](docs/install.md) covers a household DHCP/DNS deployment.

---

## What it believes

**A package, not an operating system.** No custom ISO, no reformatting, no
image to flash. You keep your distro, your packages, your SSH keys — and
`apt remove` gives the box back.

**Drive the daemons; don't reimplement them.** dnsmasq, unbound and nftables
have decades of correctness in them that nobody should rewrite. olr renders
their configuration, supervises them through systemd, and puts one schema in
front. It is a control plane, not a network stack.

**Nothing happens until you say so.** Installing olr starts a control plane,
serves its own web UI, and changes *nothing else* — no DHCP server appears, no
resolver takes port 53, no interface is touched. You hand olr an interface
explicitly (`olr adopt`) before any module will serve on it. Installing a
router's control plane on a box you reach over SSH must never be able to
disconnect you from it.

The UI is there immediately because a box nobody has set up **refuses to be
configured over the network** — so there is nothing to reach through it before
you arrive. That's the one thing that makes the convenience safe, and it's
[docs/system.md](docs/system.md).

**Hide complexity, never capability.** The default surface speaks your
vocabulary — networks, devices, fixed addresses — not the daemon's. But every
module also has a declared escape hatch that passes raw backend config through,
versioned and single-source like everything else. We make the common 95%
pleasant. We do not hide Linux.

**One schema, four surfaces.** Each module's config is a Go struct; the JSON
Schema derived from it generates the CLI flags, the REST body, the web form and
the MCP tool definition. The web UI is not privileged — it is one client of the
same HTTP API the CLI and your agent use.

**Never store a fact twice.** olr keeps your *intent*. The kernel keeps the
interfaces, dnsmasq keeps the leases, nftables keeps the rules. Nothing is
cached, so nothing can drift.

**Linux is the extension surface.** Need something we don't ship? You don't
write an olr plugin — you `apt install` it, drop in a systemd unit, or add your
own nftables table. The distro is a better plugin system than any API we could
design, and our job is to stay out of its way.

## Features and backends

| Feature | Underlying library or service |
|---|---|
| Interfaces and networks | [netlink](https://github.com/vishvananda/netlink) |
| DHCP and IPv6 router advertisements | [dnsmasq](https://thekelleys.org.uk/dnsmasq/doc.html) |
| DNS | [unbound](https://nlnetlabs.nl/projects/unbound/about/), [dnsmessage](https://pkg.go.dev/golang.org/x/net/dns/dnsmessage) (olr relay) |
| Device inventory | dnsmasq leases, kernel neighbours, IEEE OUI registry |
| Static uplink and IPv6 tunnel | [netlink](https://github.com/vishvananda/netlink) |
| IPv6 prefix delegation | In-tree DHCPv6 client |
| DDNS | [ddns-go](https://github.com/jeessy2/ddns-go) (ported provider code) |
| Exits and egress NAT | [nftables](https://github.com/google/nftables), [netlink](https://github.com/vishvananda/netlink) |
| Port forwarding and firewall | [nftables](https://github.com/google/nftables) |
| QoS policy (storage only) | In-tree config/API; no packet shaping yet |
| WireGuard | [WireGuard](https://www.wireguard.com/), `wg`, [netlink](https://github.com/vishvananda/netlink) |
| Shadowsocks | [shadowsocks-rust](https://github.com/shadowsocks/shadowsocks-rust) |
| SOCKS5 | [3proxy](https://3proxy.org/) |
| HTTPS ingress | [Caddy](https://caddyserver.com/) |
| Request inspection | [mitmproxy](https://mitmproxy.org/), `nft` |
| IP location | [ip.sb](https://ip.sb) (city lookup with a persistent SQLite LRU cache) |
| CLI | [Cobra](https://github.com/spf13/cobra), [pflag](https://github.com/spf13/pflag) |
| API and MCP | Go `net/http`, [jsonschema](https://github.com/invopop/jsonschema) (in-tree MCP server) |
| Web UI | [React](https://react.dev/), [Vite](https://vite.dev/) |
| Service supervision | [systemd](https://systemd.io/), [go-systemd](https://github.com/coreos/go-systemd) |

QoS stores policy but does not shape traffic yet; see [docs/qos.md](docs/qos.md).
Wi-Fi, bridges/VLANs, IPv4 DHCP-client and PPPoE uplinks, multi-WAN and TPROXY
exits are not implemented. Optional backends are not necessarily bundled.

## Getting started

Debian 13 (trixie) and Ubuntu are the tested targets; anything with systemd and
nftables should work.

```sh
sudo apt install ./olr_<version>_<arch>.deb
```

apt resolves `dnsmasq-base`, `nftables` and `unbound-anchor` before any of olr's
code runs. This starts the control plane and touches nothing else on the
machine.

`unbound-anchor` is in that list and `unbound` is not, which looks inconsistent
and is not: the anchor package is one binary that fetches the root DNSSEC key,
starts nothing and takes no port, while unbound without that key refuses to
start at all. Carrying the small half means turning DNS on asks you for one
thing instead of two.

Not `unbound`, deliberately. Installing it would start a resolver on
`127.0.0.1:53` on every box — including ones that will only ever hand out
addresses — and olr would then report that resolver as something holding the
port it wants. So DNS asks for it when you turn DNS on, and the page tells you
the command for the distribution you're actually running. Every backend works
this way: nothing is installed for a feature you haven't used.

For anything the `.deb` doesn't cover, the tarball holds a single binary that
installs itself:

```sh
tar xzf olr-<version>-linux-<arch>.tar.gz
sudo ./olr enable
```

`olr enable` no longer refuses over a missing backend — a router that never
resolves a name shouldn't have to fetch a resolver to get installed. Each
section reports what it needs, when you open it.

If you're installing dnsmasq by hand: `dnsmasq-base`, not `dnsmasq`. The full
package also ships a system dnsmasq service that binds `:53` as soon as it's
installed, and olr runs its own instance rather than taking somebody else's
daemon over. If you already have the full package, olr will say so and tell you
how to stand it down.

`olr enable` writes the systemd units, puts the binary in `/usr/local/bin`,
corrects the units' paths if your distribution doesn't keep dnsmasq where
Debian does, and starts the service. It prints every file it writes, and
`--dry-run` shows the list without touching anything. `olr disable` undoes the
boot entry and leaves the files in place.

Hand it an interface, name the network on it, then give that network a job:

```sh
olr link show interfaces                                          # what this box has
sudo olr adopt enp1s0                                             # grant permission
sudo olr net add lan --member enp1s0 \
  --subnet 192.168.1.0/24 --router 192.168.1.2                    # this router's address on it
sudo olr dhcp add pool lan --range 192.168.1.100-192.168.1.200
sudo olr dhcp enable
olr dhcp show leases                                              # who took an address
```

`olr adopt` is the step people miss. Every module refuses to serve on an
interface nobody handed it. Adopting sets no address and starts no service — it
only grants permission.

`olr net add` is the step that changes the box. It writes the router's address
to the interface, takes any other IPv4 address there off it, and olr puts it
back after every reboot. If you are connected over that interface, give the
network the address you are connected to, and run it with `--dry-run` first — it
prints exactly what would change. [docs/install.md](docs/install.md) has the two
rules that keep olr and your distribution's network configuration from fighting
over the same interface.

The web UI is already open — installing printed its address — and the first
screen asks one question. **A box nobody has set up refuses to be configured
over the network:** the page loads, and every other request is turned away until
somebody says how they want to reach it. That's what lets the UI be there from
the first second without leaving an unprotected admin API on your LAN.

Set an administrator password, or deliberately continue without one. The latter
lets anyone who can reach this router configure it. `sudo olr claim --no-password`
records that choice from a shell; `sudo olr claim --password` reads a password
from redirected stdin. `olr system show` reports the choice, and
`sudo olr listen --off` closes the listener entirely.

Claiming works once, and only from your own network or the box itself — so olr
on a machine with a public address isn't set up by whoever finds the port first.

The **Administrator access** link in the footer changes the password and signs
out browsers. See [administrator access](docs/administrator-access.md) for
HTTPS, API tokens, management source restrictions and recovery over SSH.

**The most useful thing this can do today:** leave your existing router in place
doing the routing, and move DHCP onto a Linux box — so you get a real device
list, fixed addresses that are easy to edit, and a config file you can read.
[docs/install.md](docs/install.md) walks through it, including the two things
that go wrong.

---

| Docs | |
|---|---|
| [docs/backup.md](docs/backup.md) | Back up and restore olr.json with connectivity recovery |
| [docs/audit.md](docs/audit.md) | Every change made through the API, and how to read it |
| [docs/public-access.md](docs/public-access.md) | Exposing one service to the internet, and what keeps the rest private |
| [docs/install.md](docs/install.md) | Getting a box serving DHCP for a real network |
| [docs/system.md](docs/system.md) | First install, and who may configure the box |
| [docs/cli.md](docs/cli.md) | `olr` command conventions, enforced by tests |
| [docs/dns.md](docs/dns.md) | What the DNS module does, and refuses to do |
| [docs/dial.md](docs/dial.md) | The box's own way out, and why it is not a network |
| [docs/gateway.md](docs/gateway.md) | Exits, and which networks use them |
| [docs/port-forwarding.md](docs/port-forwarding.md) | Port forwarding, and why there is no firewall |
| [docs/remote-access.md](docs/remote-access.md) | Dialling in from outside, and why not `wg-quick` |
| [docs/mcp.md](docs/mcp.md) | The agent surface |
| [design.md](design.md) | Architecture, and the decisions behind it |

Building: `make all` (SPA + binary), `make check` (vet + tests), `make package`
(`.deb` for amd64 and arm64). Go builds without Node — a binary built that way
serves an explanatory page instead of the UI. `make web` needs Node 24.

MIT licensed.
