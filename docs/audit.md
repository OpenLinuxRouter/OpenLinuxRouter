# Audit — the record of what changed this box

Status: **built.** `internal/core/audit.go` writes it, `GET /api/audit` reads it,
and `olr audit` is the operator's way in.

> Every mutating request, one line: who asked, from where, what the box
> answered. No request bodies.

## 1. The question it answers

State answers *what is true now*. It cannot answer *who made it true*, and those
are different questions at exactly the moments that matter: a published service
somebody cannot explain, a firewall that is off, a DNS record that points
somewhere unexpected, a certificate that stopped renewing after a token was
rotated.

The record is written where the change is: one line per mutating request,
whoever sent it — CLI, WebUI, or a future MCP write. It is deliberately not a
per-module feature. "Who changed the ingress config" and "who turned the
firewall off" are the same question, and a module that forgot to record its own
writes would be a hole nobody could see.

Reads are not recorded. A UI polls; recording reads would bury the changes in
noise and turn a bounded record into a traffic log.

## 2. What a line says

| Field | Example | |
|---|---|---|
| `at` | `2026-11-13T18:50:26+08:00` | when the request started, not when the apply finished |
| `module` | `ingress` | the mount point, never parsed out of the path |
| `method`, `path` | `PUT`, `/api/ingress/services/rustfs` | the concrete path, so the object is named |
| `source` | `172.16.1.50` | see §4 |
| `forwarded_for` | `172.16.1.50` | the header as received, kept beside the attribution |
| `user_agent` | `olr/0.4.1` | |
| `status`, `outcome` | `200`, `succeeded` | `succeeded` / `refused` (4xx) / `failed` (5xx) |
| `dry_run` | `true` | a mutation that was planned, not applied |
| `duration_ms` | `41` | how long the box took to answer |

**What is deliberately absent: the request body.** The body of
`PUT /api/ingress/config` carries the DNS provider's API token, and the body of
`PUT /api/system/...` carries a password. A record that stored bodies would be a
second copy of every secret, on disk, retained for months. The path already says
what was changed; the token says nothing a reader needs.

The query string is also dropped, except for `dry_run`, which is carried as its
own field because otherwise the record would claim a change that only happened
in a preview.

## 3. Where it lives, and why a file

```
/var/lib/open-linux-router/audit/changes.jsonl      mode 0600, directory 0700
/var/lib/open-linux-router/audit/changes.jsonl.1    older segments, .2, …
```

This is the one place olr writes a file instead of logging, and it needs saying
because [docs/install.md](install.md#L265) records the opposite as a principle:
*olr keeps no log files of its own*. That principle is about **logs** — journald
already rotates, ships and correlates those, and a second logging system is a
second thing to get wrong.

The audit is not a log in that sense. It is a bounded, structured record that a
client reads back over the API, and it has to survive the two things journald
cannot promise: persistence (it is volatile on many installs) and a stable
shape (free text is for reading, not for answering "what changed at 14:02"). The
precedent is the connectivity journal in
[configuration-safety.md](configuration-safety.md#L53), which is also a 0600 file
under `/var/lib` read back by the box rather than by hand.

**Rotation is by size, not by age:** 4 MiB per segment, five segments, newest
first. At roughly 200 bytes an entry that is about a hundred thousand changes —
and when it runs out, the oldest lines are gone. The record is bounded on
purpose; see §5 for what that means.

There is no `fsync`. A power loss can lose the last line; it cannot corrupt the
ones before it, because each line is written in one call to an `O_APPEND` file.

## 4. Attribution

`source` is the address the connection came from, with one exception. The WebUI
is normally reached through `ingress` — `https://olr.<domain>` — so the TCP peer
is `127.0.0.1` and the client only exists in `X-Forwarded-For`. When the peer is
a loopback address and that header is present, the **last** hop is used, because
that is the one the local proxy observed and appended; earlier entries are
whatever the client claimed.

A request that did not come through a local proxy keeps its own address and
cannot borrow one from the header, which is why the peer is checked first. Over
the unix socket there is no peer address at all and the field is empty.

This is attribution, not authentication. There is one admin credential today
(§7), so "who" means "the admin, from this address, with this user agent".

## 5. Failure model

**A failed write never fails the request.** The error goes to the journal and
the change proceeds. The alternative — refusing a change because its record
could not be written — turns a full disk or a permissions mistake into a router
nobody can configure, which is worse than a gap.

Two honest limits follow:

- **It is not tamper-evident.** Root can edit the file. What the record buys is
  that a change is *seen*, not that it is *proven*.
- **It is not complete.** Rotation drops history; a change older than five
  segments is gone. It answers "what changed recently", not "what has ever
  changed".

## 6. Reading it

```sh
olr audit                 # newest 50, text
olr audit --limit 200     # more
olr audit -o json         # the whole response
```

```
WHEN                 MODULE   METHOD  PATH                            RESULT       FROM
2026-11-13 18:50:26  ingress  PUT     /api/ingress/services/rustfs    succeeded 200  172.16.1.50 (via proxy)
```

Over HTTP: `GET /api/audit?limit=N`, newest first, with `as_of` saying when the
answer was read. A malformed `limit` is refused rather than defaulted, so a typo
cannot look like an empty record.

## 7. What it does not cover yet

- **No per-user identity.** There is one admin credential; the record can say
  which address and which client, not which person.
- **No access log for published services.** Who *visited* `https://rustfs.<domain>`
  is a different record, on the proxy, with different retention and privacy
  rules. It belongs to the public-exposure work, not to this one; a change audit
  is not traffic analysis.
- **No export.** The file is plain JSONL and can be copied, but olr does not
  ship, ship-off, or sign it.
