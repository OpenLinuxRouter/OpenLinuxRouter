# Public access — exposing a service, on purpose

Status: **built.** `expose` and `allow_from` on a service, `public_domain` on the
module, a second site block in the rendered Caddyfile, and an access log.

> One service at a time, off by default, with the private names still answered
> only to this network.

## 1. The object: exposure

Every published service has answered only on this network since the module
existed. That was never a setting — it was the shape of the thing. This adds the
setting, and gives it three values:

| `expose` | Name | Who can reach it |
|---|---|---|
| absent / `lan` | private | this network only — the LAN, the dial-in tunnel, the box itself |
| `public` | public | anyone, at `<name>.<public_domain>` |
| `restricted` | restricted | anyone who connects, but only the sources in `allow_from` get an answer |

The default is `lan`, and it stays `lan` unless somebody says otherwise. A
service that becomes public without being asked is the one outcome this feature
must never produce, so there is no module-level "expose everything" and no
inference from the upstream being a client or a host.

## 2. Two suffixes

`public_domain` is a zone of its own, e.g. `example.com` beside
`home.example.com`. The reason is the one thing DNS cannot do: a name that must
answer `172.16.1.1` inside the house and a public address outside is two answers
for one name, and that is split-horizon DNS — a resolver feature this module
neither owns nor wants. Two suffixes give the two answers without one.

What that buys, beyond simplicity:

- **Private names stay out of public DNS entirely.** `*.home.example.com` is
  never published for an exposed service; the exposed one gets its own name.
- **The internal answer cannot be the thing that leaks.** There is no world in
  which a public client resolves a private name.

What it costs:

- **A second wildcard certificate**, for `*.<public_domain>`, in a second zone.
  The DNS provider credential has to be allowed to write both. If it is not, the
  public certificate never issues while the private one renews perfectly — which
  is why `status` reports the two separately.
- **The public name is not usable from the LAN** unless the router in front
  supports hairpin NAT. Use the private name inside; that is what it is for.
- **Nesting is allowed but sharp.** `public_domain = example.com` with
  `domain = home.example.com` is legal: Caddy prefers the more specific site, so
  routing is right. The trap is a wildcard DNS record at the parent — it would
  resolve every private name to this box from outside, and only the per-service
  source rule would be between them and the internet. The validator warns about
  this when it sees the nesting.

## 3. What actually makes a name private

Not DNS. A resolver answer is a convenience for browsers; it is not a gate. The
published certificate is a wildcard, so a TLS handshake for **any** name under
it succeeds, and Caddy routes on the `Host` it is given. Anyone on the internet
can send `rustfs.home.example.com` as SNI to :443 and see what happens. The
certificate's existence is public in Certificate Transparency logs, so "nobody
knows the name" is not a position to defend.

The gate is a matcher:

```
@svc_rustfs {
	host rustfs.home.example.com
	remote_ip private_ranges
}
handle @svc_rustfs { … }
handle { abort }
```

`private_ranges` covers the RFC 1918 networks (the LAN), `fc00::/7`, loopback,
and therefore the dial-in tunnel's `10.x`. A request from anywhere else matches
nothing and falls through to the block's `abort`, which closes the connection
without a body. It does not matter what name it asked for, or how that name
resolved.

### 3.1 The case this cannot distinguish

If the router in front of this box rewrites the source address of forwarded
traffic (SNAT, which some CPEs do on port mappings), every external request
arrives looking like `192.168.1.1` — a private address — and the guard lets it
through. The guard fails open, silently.

A port forward that only translates the destination preserves the source, and
that is the ordinary case; this box's WireGuard port works that way. It is still
worth one check rather than an assumption:

1. expose a service, follow a request from outside (a phone on cellular), then
2. read the access log's `remote_ip` for that request.

A public address means the guard is doing its job. `192.168.1.1` means it is not,
and the honest fixes are a router that preserves the source, or the port split
in §6.

## 4. What each state renders

```
*.home.example.com {            # every service, always
	@svc_a {
		host a.home.example.com
		remote_ip private_ranges
	}
	handle @svc_a { reverse_proxy … }
	handle { abort }
}

*.example.com {                 # only `public` and `restricted` services
	log { … }
	@pub_a host a.example.com
	handle @pub_a { reverse_proxy … }

	@pub_b {
		host b.example.com
		remote_ip 203.0.113.0/24 198.51.100.7
	}
	handle @pub_b { reverse_proxy … }
	handle { abort }
}
```

Exposing a service **adds** a name. It does not move the private one, and a
service that is exposed is still reachable at its private name from inside.

### 4.1 Restricted is not hidden

Be clear about what `restricted` buys on port 443: a connection from a source
that is not in the list completes the TCP and TLS handshakes and is then closed.
The port is visibly open to a scanner; the *service* is not.

Looking closed requires the boundary to be a socket or a firewall rule rather
than a matcher — either the public names on their own port (with the forward
mapping 443 to it), or an nftables rule, which cannot see a `Host` header and so
cannot be per-service. That is a different design and it has not been taken here.

## 5. The firewall

The proxy's ports are opened at the boundary **only while something is exposed**.
A box where nothing is exposed is indistinguishable from outside from one where
this module is switched off, even if a port forward in front of it still exists.
Private names keep working: traffic from inside is accepted by the firewall's
inside rule, which does not depend on this opening.

## 6. Certificates

One wildcard per suffix, both obtained by DNS-01 through the same provider and
credential. `status` reports them separately: `certificate` and
`public_certificate`. The second one has its own failure mode — a credential
scoped to the private zone — and it is quiet, because the private certificate
keeps renewing.

## 7. The access log

Who reached an exposed service is a different record from who changed the box
(`audit.md`): higher volume, different retention, different privacy.

```
/var/lib/open-linux-router/ingress/logs/public.log
```

JSON, written by the proxy, rotated at 10 MiB with five kept. The path is inside
the unit's `StateDirectory`, so the sandbox and the backup story already cover
it. Two deliberate edits to Caddy's default access log:

- **The query string is removed**, and the path is re-added as its own field. An
  S3 request carries its signature in the query; a log that keeps it is a log
  that leaks temporary credentials to whoever reads the file later. Caddy's
  default already redacts `Authorization` and `Cookie`.
- **Only the public block logs.** The private block is on somebody's LAN and its
  traffic is not this feature's business.

## 8. What olr does not do

- **Create the DNS record.** It is in the operator's zone, as ever.
- **Forward the port.** The router in front of this box is the operator's device;
  olr may not be able to reach it at all.
- **Verify that the record points here.** Still the open item in
  `ingress.md` §11, and more consequential now: an exposed name whose record is
  wrong looks identical in `status` to a working one until somebody opens the
  URL.
- **Filter attacks.** No WAF, no rate limiting, no ban list. A service exposed
  here is exposed exactly as its own software handles the internet.

## 9. Turning it off

Remove `expose` from the service and apply: the next render has no public
matcher for it, and the name stops answering. The certificate stays on disk (it
is shared by every exposed name), and the DNS record is still the operator's to
remove — a record pointing at a box that closes the connection is not an
exposure, but it is tidiness.

Clearing `public_domain` while a service is still exposed is refused by name,
because it would leave a service configured to be public with nowhere to be
public at.
