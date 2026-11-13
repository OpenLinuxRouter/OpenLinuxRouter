# Serving DHCP for a network you already have

The shortest useful thing olr can do: leave the router you already own doing the
routing, and move **DHCP** onto a Linux box. You get a device list worth reading,
fixed addresses that are easy to edit, and a configuration file you can open.
Nothing about how traffic reaches the internet changes.

This is written for that arrangement specifically, because it is the one with a
trap in it (§4, below). If the box *is* your gateway, everything here still
applies except the parts about pointing clients at the old router — and the
address rules below have a version for that case too.

---

## What you need

- A Linux box on the network, with systemd and a **static address**, set with
  whatever your distribution uses (`systemd-networkd`, NetworkManager,
  `/etc/network/interfaces`) and outside the range you are about to hand out.
  In step 3 you give olr the same address, and from then on olr owns it — read
  the next section before you get there.
- Access to your existing router's settings, to turn its DHCP server off.
- Ten minutes, and preferably not while anybody is on a call.

### Who owns the box's address

Putting an interface in a network (step 3) hands its IPv4 addressing to olr.
olr writes the network's router address to it, puts it back after every reboot,
and when you apply a change **removes any other IPv4 address it finds there**.

What olr does not do yet is take the interface away from your distribution's
network configuration. Until it does, two things manage that interface and
neither knows about the other. Two rules keep that safe:

- **The same address in both places, and static.** If your distribution also
  configures the interface, it configures exactly the address you give the
  network — never DHCP. A leased address that differs is removed the next time
  you apply anything, and the default route goes with it. On a box with one
  network interface this is the only arrangement that works: something outside
  olr keeps providing the default route, and it and olr agree on the address.
- **An interface that gets its address by DHCP is never in a network.** On a box
  that *is* your gateway, that is the uplink to your modem: make it the uplink
  (below), and put only the LAN side in a network. Where the distribution runs
  dhcpcd (Debian 13, Raspberry Pi OS), olr stops it doing IPv4 on every
  interface olr addresses and leaves its IPv6 alone. Where it runs
  NetworkManager, systemd-networkd or dhclient, olr says so on the Networks page
  and you take the interface out of that configuration yourself.

#### If this box is your gateway: the uplink is not a network

A **network** is a subnet this router *serves* — it hands out addresses there
and answers names. The interface facing your modem is the opposite of that, and
it has its own place in olr: **Gateway → Networks → Internet uplink**, or `olr dial set
uplink`.

```sh
sudo olr adopt enp2s0
sudo olr dial set uplink --interface enp2s0 \
  --address 192.168.2.9/24 --gateway 192.168.2.1 --dry-run
```

The address keeps its host bits — `192.168.2.9/24` is this box's address on the
link to the modem, not the subnet. From then on olr owns that interface's IPv4
and this router's default route, and puts both back after a reboot. Add
`--dns 192.168.2.1` — usually the modem — and this router looks names up
through it too: olr writes `/etc/resolv.conf`, or gives systemd-resolved the
address where the box resolves through it. docs/dial.md §4 has exactly what is
taken from the distribution, and how it is given back.

**One interface cannot be both.** olr refuses an uplink on an interface a
network already carries, and says which network. If you set your WAN NIC up as a
network before this existed — which was the only thing olr would accept — the
easiest migration is the web UI: under **Gateway → Networks → Internet uplink**, choose
that interface. It says it carries the network, removes the network while
keeping its address on the interface, and sets the uplink on that same address,
so a session arriving through it stays up.

From the CLI it is `sudo olr net rm wan`, then the two commands above — and
`net rm` takes the network's address off the interface, so do that from the
console, or over the *other* NIC.

**Your networks come with it.** Once there is an uplink, olr masquerades
traffic leaving it from the subnets you declared, so the devices on your
networks reach the internet without anything further being configured. No
`gateway` exit is needed for that — an exit is for sending a network out a
*different* way, through a VPN or another box. `docs/gateway.md` §3.9 has the
rule and the switch that turns it off.

Throughout, the example network is `192.168.1.0/24`, the existing router is
`192.168.1.1`, and the olr box is `192.168.1.2` on interface `enp1s0`.

---

## 1. Install

```sh
sudo apt install ./olr_<version>_<arch>.deb
```

This starts `olrd`, serves its web UI, and changes nothing else — no DHCP
server, no resolver, no nftables rule, no interface touched. It prints the
address to open:

```
Open the web UI to finish setting up:

  http://192.168.1.2:8080
```

## 2. Set the router up

Browse to that address. The first screen asks one question, because **a box
nobody has set up refuses to be configured over the network** — the page loads
and every other request is turned away. That is what makes it safe for the UI to
be open from the moment you installed it.

Clicking through records that **there is no password**: anyone who can reach this
router on your network will be able to configure it. That is usually what you
want on a home network and not what you want anywhere else. Requiring a password
instead arrives with the login screen in the next release.

If you only ever reach this box over SSH, `sudo olr claim --no-password` does
the same thing, and `olr system show` says where you stand.

Setting up from a machine *outside* your own network is refused on purpose, so
that a box with a routable address is not claimed by whoever finds the port
first.

The rest of this document gives both the UI and the CLI; they are the same API.

## 3. Hand the interface to olr, and name the network on it

**Web UI:** both halves are on one page, in the order this heading gives them.
Gateway → Networks → Interfaces → switch on `enp1s0`; then, below it, Add: subnet
`192.168.1.0/24`, this box's address `192.168.1.2`, on `enp1s0`. (A box with
nothing adopted says so on the Overview, and that link goes to the same page.)

Adoption used to live under DHCP → Interfaces, and that address still
redirects here — an older copy of this document strands nobody.

**CLI:**

```sh
olr link show interfaces
sudo olr adopt enp1s0
sudo olr net add lan --member enp1s0 \
  --subnet 192.168.1.0/24 --router 192.168.1.2 --dry-run
```

olr refuses to serve anything on an interface nobody gave it, so without the
adoption every range you try to add is rejected. Adopting sets no address and
starts no service; it grants permission.

The network is the step that changes the box, which is why the command above is
a dry run. `--router` is not optional in this arrangement: left out, it defaults
to the first address in the subnet, which here is **your existing router's**.
Give it the static address the box already has, and the dry run should show no
address being added or taken off. If it shows one coming off, that is the
address you are connected over, and applying would end your session. When it
looks right, run the same command without `--dry-run`.

## 4. Add the address range — and read this part twice

**Web UI:** DHCP → Address ranges → Add. Choosing the network fills in a range
inside its subnet.

**CLI:**

```sh
sudo olr dhcp add pool lan \
  --range 192.168.1.100-192.168.1.200 \
  --gateway 192.168.1.1 \
  --dns 192.168.1.1
```

`--gateway` and `--dns` are the trap. Left unset they both mean *this router*,
which is right when the olr box is your gateway and **wrong here** — your
existing router still carries the traffic and answers the names. Set to blank in
this arrangement, every device would be handed a default route to a machine that
is not routing and a resolver that is not answering, and the symptom is "the new
router broke the internet".

Point them at whatever does that job today. Usually that is the old router,
`192.168.1.1`. If you would rather hand out a public resolver, `--dns 1.1.1.1`
is equally fine — what matters is that it is something that answers.

Then turn the server on:

```sh
sudo olr dhcp enable
olr dhcp status
```

## 5. Turn off the old DHCP server

Now, and not before. Find it in your existing router's admin page — usually
LAN → DHCP Server → off.

**Two DHCP servers on one network is a race, and olr cannot see the other one.**
The preflight check only looks at UDP/67 on *this* box; a second server in a
plastic box across the room is invisible to it. Whichever answers a client first
wins, so leaving both on gives you a network that works and is confusing, which
is worse than one that plainly does not.

## 6. Watch devices arrive

Devices do not move over immediately. They keep the address the old server gave
them until that lease expires — often 12 or 24 hours — and only then ask again.
To hurry a device along, turn its Wi-Fi off and on.

**Web UI:** the Overview page. **CLI:**

```sh
olr dhcp show leases
```

## 7. Pin the things that should not move

Printers, NAS boxes, anything you reach by address:

**Web UI:** Overview → click the device → give it a fixed address.

**CLI:**

```sh
sudo olr dhcp add reservation aa:bb:cc:dd:ee:ff --ip 192.168.1.50
```

Reservations are applied by reloading dnsmasq rather than restarting it, so
adding one does not interrupt anybody.

---

## What the device list can and cannot see

It is a join of three sources, and knowing which is which saves an argument with
the screen:

- **DHCP leases.** Everything that asked this box for an address. Complete for
  anything using DHCP, and blind to anything that does not.
- **The kernel's neighbour table** (`/proc/net/arp`), which catches the
  statically-addressed printer that has never taken a lease. It only knows
  devices *this box* has recently exchanged packets with — and in this
  arrangement traffic goes through your old router, not through here, so it will
  be sparse. Expect the list to be mostly lease-derived.
- **Your reservations**, so a device you have configured appears even when it is
  switched off.

Two known gaps, both stated rather than hidden: `/proc/net/arp` is IPv4-only, and
a DHCPv6 lease often carries no hardware address to join on — so a v6-only client
is invisible to both sources. A device that neither takes a lease nor talks to
this box has to be added by hand.

## Where things live

| | |
|---|---|
| `/etc/open-linux-router/olr.json` | everything you configured, one file |
| `/etc/open-linux-router/olrd.env` | whether the web UI listens, and on what |
| `/etc/open-linux-router/api-token` | the API token, used only with `--auth` |
| `/etc/open-linux-router/rendered/` | what olr generated for its backends — never edit |
| `/etc/unbound/open-linux-router/` | the resolver's rendered config — unbound may read nowhere else |
| `/var/lib/unbound/open-linux-router/` | the resolver's DNSSEC anchor, rewritten as the root key rolls |
| `/var/lib/open-linux-router/dhcp/` | the lease database |
| `/var/lib/open-linux-router/audit/` | every change made through the API — the one log olr keeps ([audit.md](audit.md)) |
| `journalctl -u olrd -u olr-dhcp` | logs; apart from the audit record, olr keeps no log files of its own |

The document in `olr.json` is meant to be read. If you ever need to recover a
box, SSH in and open it — that is the point of intent being a plain file.

## Undoing it

```sh
sudo olr dhcp disable        # stop serving, keep the configuration
sudo olr dhcp rm pool lan    # forget the range
sudo olr net rm lan          # forget the network — takes its address off enp1s0
sudo olr release enp1s0      # take the interface back
sudo apt remove olr          # or remove it entirely
```

The order is not arbitrary: an interface cannot be released while it is still in
a network.

`olr net rm` takes the router's address off the interface, and your distribution
does not notice it has gone. On a box you reach through that interface, that is
your session. Run it from the console, or have the distribution put its own
address straight back in the same breath, so the second half runs on the box
whether or not your session survives the first:

```sh
sudo sh -c 'olr net rm lan && systemctl restart networking'   # ifupdown; use your distribution's equivalent
```

Turn your old router's DHCP server back on before or shortly after, or nothing
new joining the network will get an address.

## When something is wrong

**Nothing gets an address.** Check the range is inside the network's subnet
(`olr net show`) and that the interface is up with a cable in it (`olr link show
interfaces`). Then `olr dhcp status`, then `journalctl -u olr-dhcp -n 50`.

**olr refuses to start the server.** Something already holds UDP/67 on this box,
and the refusal names it — process, pid and systemd unit — along with the
command that stands it down. olr runs its own dnsmasq instance and will not stop
a daemon it did not start *unless you ask it to*: `sudo olr dhcp fix` runs that
command for you, and the DHCP page has a button that does the same thing.
(`--dry-run` shows exactly what it would run first.) The usual culprit is your
distribution's own dnsmasq: `apt install dnsmasq` installs a service alongside
the binary, where `dnsmasq-base` is just the binary olr drives.

`fix` only touches a second copy of a daemon olr runs itself. It never stops an
OS component — `systemd-resolved` is asked to give up port 53 through a drop-in
and keeps running, because this box resolves names through it.

**A range is refused as "not adopted".** Step 3.

**Devices get an address but no internet.** Step 4 — the gateway is almost
certainly pointing at this box instead of your real router. `olr dhcp show pool
lan` shows what is being advertised.

**Your session dropped in step 3.** The network's router address was not the one
you were connected over, and olr took yours off — and the default route with it.
From the console, `sudo olr net set lan --router <the address you had>` makes the
two agree again, and `sudo systemctl restart networking` (or your distribution's
equivalent) puts the route back.

**olr refuses the uplink: "enp2s0 carries the network …".** One interface cannot
be both a network this router serves and the way out. That is the migration in
"If this box is your gateway", above: remove the network, then set the uplink.

**The box reaches the internet and the LAN does not.** Check that `olr gateway`
is switched on — the masquerade that gives your networks a return path lives
there, and turning the module off closes it along with any port forwards.
`olr gateway status` says whether it is in force.

**`olr dial show uplink` says the default route goes somewhere else.** Something
outside olr replaced it — most often the distribution still configuring that
interface, or a DHCP client on it. `olr dial set uplink --interface <name>` with
no other flags re-applies what is stored, and takes the interface from dhcpcd
if that is what was on it; anything else olr names in the same output, and you
take the interface out of its configuration, or the two will keep overwriting
each other.

**This router reaches the internet by address but cannot resolve names.** The
uplink has no resolvers: set `--dns` (the Networks page offers the gateway with
one click). The uplink card's "looks names up through" line shows what the box
is really using.

**The web UI cannot be reached.** `olr listen` with no listener set is off
by default; re-run step 2. If it is set, check nothing between you and the box is
filtering the port.
