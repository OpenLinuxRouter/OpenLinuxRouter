# Client request inspection

Inspection is a client setting that stays on until you turn it off. Page reloads,
OLR daemon restarts, package upgrades and router reboots preserve that choice.
Completed and failed captured HTTP(S) requests are saved with their full native
mitmproxy content, so stopping inspection does not erase what was already saved.
Only one client can be inspected at a time; saved history for every client remains
available while its switch is off or another client is being inspected.

## Setup

Install `mitmproxy` (`mitmweb`) on the Linux router before starting. On Debian,
use `sudo apt install mitmproxy`. It is optional rather than a package dependency.

1. Open the client page, check its IPv4 addresses, and turn Inspection on. OLR
   creates its CA on the first start. The switch starts interception directly.
2. Open the gear beside the switch. Scan the QR code on the client or follow
   **Open setup guide** to the public five-step guide at `/inspection-setup.html`.
   The certificate download is at step two. The guide and public certificate need
   no administrator token; they never expose the private key.
3. Install and trust the CA on the client for HTTPS. On iPhone, enable full trust
   in Settings > General > About > Certificate Trust Settings after installing
   the profile. Compare its SHA-256 fingerprint with the guide's current CA.
4. Browse from that client and open saved requests or the Live request viewer.
   HTTP needs no CA. HTTPS content is available only when the app accepts the CA.
5. Turn Inspection off when you want interception to stop. Saved requests and
   activity remain available. Remove the CA from the client when it is no longer
   needed; stopping inspection does not revoke the client's trust.

OLR keeps the CA, client switch and inspection data beneath
`/var/lib/open-linux-router/inspection` with restricted permissions. Removing or
backing up that directory also affects the CA trusted by clients. The API serves
only the public PEM, never its private key. If the configured proxy cannot start,
the client page keeps the switch on, shows the reason and offers **Retry**.

## Saved requests and the Live viewer

The Inspection card shows **Activity**, current **DNS queries** while active, and
**Requests**. Requests opens saved history by default. During active inspection,
**Saved** and **Live** select between that history and the embedded mitmweb viewer.
Saved history is independent of the live viewer's rolling memory list.

Search saved requests and use **Load older requests** to page through them. Each
row shows the timestamp, method, URL, HTTP status (or failure) and archived size.
Select a row to read request/response headers and text bodies inline. Each body
preview is limited to 64 KiB and labels truncation; binary bodies are identified
instead of rendered as text. Failed requests can have no response. **Download
.mitm** downloads the full native flow, including content beyond the preview.
Open it in mitmproxy or mitmweb, for example `mitmweb -r inspection-ID.mitm`.
Downloads use the administrator bearer credential in an HTTP header, never a URL.

The archive saves a flow after the captured HTTP request completes or fails. A
request still in progress is not yet a durable archive: stopping or crashing the
proxy can lose that unfinished content. A router reboot also ends those requests.
An OLR daemon restart does not stop an already running inspection proxy on the
packaged Linux service; its completed flows continue to be archived while the
control daemon is restarting. Archiving failures appear explicitly in the card.
A live response alone is not proof that its archive was successfully written.

**Clear saved requests** opens a confirmation dialog and deletes only the current
client's saved request/response archive. It leaves other clients' archives and
site activity intact, and an enabled inspection continues capturing new requests.
Turning Inspection off leaves all saved history intact.

The gear dialog contains global retention settings. Defaults are **30 days** and
**5 GiB of saved request storage total across all clients on this router**. The
request list shows client usage and router-wide usage against that shared cap.
The oldest saved requests are removed when the age or storage limit is reached.
Retention is adjustable from 1 to 365 days and from 1 MiB to 1 TiB. Reducing a
limit can remove older saved requests immediately. This is bounded local history;
export flows you need to keep beyond those limits. Filesystem metadata and
transient files can require additional disk space beyond saved archive usage.

The live viewer keeps the latest 1000 flows in memory so leaving inspection on
cannot grow its working set without bound. This limit does not limit the archive
to 1000 flows. Mitmweb binds only to router loopback (`127.0.0.1:18082`); OLR
embeds it at `/inspection-web/` for the active client. The browser obtains a
short-lived HttpOnly viewer cookie through the authenticated API, and OLR adds
the proxy's bearer token server-side for HTTP and WebSocket traffic. No second
login or LAN listener is required. Turning Inspection off revokes live access.

## Retained site activity and DNS

The Activity chart offers **4 hours**, **24 hours**, **7 days** and **30 days**.
It reads retained samples by client MAC, including when the client has no current
address, inspection is off, or another client is active. Its total is for the
selected range. Samples are collected only while this client's inspection is on;
saved samples survive stop/start and OLR restarts. There are no samples during
control-daemon downtime, and retaining earlier samples does not fill those gaps.
Site samples have a separate fixed bound of 30 days and 64 MiB across clients;
they do not consume the configurable 5 GiB saved-request allowance.

Sites use registrable domains under the public suffix list, so a suffix such as
`com.cn` is not itself presented as a site. The chart uses the DNS relay's address
names, then names observed by the proxy for the same client/destination pair.
Connections outside interception's scope can remain unnamed. Activity is sampled
connection traffic, not the request archive's file size or a complete browsing
history. Clearing saved requests does not clear these activity samples.

DNS queries are a separate recent network-wide sample matched to the client's
current IPs; the relay's query log must be enabled. A DNS lookup is not proof of
a visit. The DNS sample is not the saved request archive.

## Scope and service behavior

OLR intercepts only selected source IPv4 traffic on TCP ports 80/443 to public
destinations. IPv6, UDP/443 (QUIC), LAN destinations, other ports and non-HTTP
protocols are outside scope. OLR does not block QUIC to force fallback. A client
can have up to eight explicitly selected IPv4 addresses. Check their ownership:
OLR does not prove the MAC owns them or automatically follow reassignment.
Certificate pinning and separate app trust stores can prevent decryption or make
an app fail to connect. The page does not claim a complete list of connections.

When the client has an explicit gateway exit, OLR marks only mitmweb's upstream
packets to follow that exit so fake-IP DNS answers can remain usable. Without an
explicit exit, `198.18.0.0/15` destinations bypass interception. Stop and restart
inspection after changing the client's exit to pick up that change.

On packaged Linux systems the inspection proxy runs as its own systemd service,
independent of the OLR daemon, so a control-daemon deployment does not interrupt
capture. Reboot starts the configured inspection again. An explicit stop ends the
proxy and restores normal forwarding. OLR waits for proxy readiness before
installing interception and reconciles its own nftables rules after failure.
The listener guard accepts only selected sources whose conntrack original TCP
destination was port 80/443; direct LAN connections to the proxy port are dropped.
Do not use `ct status dnat` alone to identify these redirects.

Redirect-rule packet counts and accepted proxy socket counts are diagnostics,
not evidence of a completed or saved response. A zero redirect count after a
known IPv4 HTTP request indicates a rule mismatch; growing redirects with zero
proxy accepts points to delivery between netfilter and the proxy.

Startup errors include recent proxy stderr with its viewer token redacted. Retry
shows progress and preserves the failure reason. Process cleanup requests normal
termination before forced termination so standalone PyInstaller extraction files
can be removed by their owner. OLR does not sweep unrelated temporary files.

Validate on a real Linux router before relying on the interception path: HTTP,
trusted HTTPS, pinned apps, IPv4 changes, QUIC, proxy failure, OLR restart,
reboot, archive persistence/retention and nftables cleanup. macOS Go/UI checks
cannot exercise that path.

The opt-in `TestManagedSystemdContinuity` smoke test validates the generated unit
on a real root/systemd host with mitmweb installed. Build the inspection test
binary for that host, then run
`sudo env OLR_INSPECTION_SYSTEMD_SMOKE=1 ./inspection.test -test.v -test.run '^TestManagedSystemdContinuity$'`.
It uses its own temporary service, CA, and loopback ports 28081/28082, with a
no-op stop hook instead of nftables. It verifies an unchanged proxy PID and
continued capture after control-plane detachment, then checks retained records
after proxy restart. The test removes its own service and temporary data.

## History API

All endpoints below require the same administrator authentication as other API
operations. MAC identifies the archive owner independently of current addresses.

| Method and path | Purpose |
| --- | --- |
| `GET /api/inspection/history?mac=&before=&limit=&search=` | Newest-first saved entries, opaque pagination cursor, client/router byte usage and current retention limits |
| `GET /api/inspection/history/detail?mac=&id=` | Saved entry metadata with request/response headers and bounded text previews |
| `GET /api/inspection/history/flow?mac=&id=` | Full native `.mitm` download |
| `DELETE /api/inspection/history?mac=` | Clear this client's saved request archive only, after explicit UI confirmation |
| `GET /api/inspection/retention` | Global `{days,max_bytes}` retention settings |
| `PUT /api/inspection/retention` | Update global `{days,max_bytes}` limits |
| `GET /api/inspection/activity?mac=&range=4h\|24h\|7d\|30d` | Retained activity buckets and current `recording` state |

History entries carry `id`, Unix-seconds `at`, `method`, `url`, `status` and
`bytes`. `status: 0` identifies failed captured requests. List responses include
`entries`, `next_before`, per-client `retained_bytes`, router-wide `total_bytes`,
`retention_days` and global `max_bytes`. Detail messages are nullable and include
`headers: [{name,value}]`, `body`, `body_bytes`, `body_truncated` and
`body_binary`; a failed flow may also include `error`. Inspection status includes
`history_error` when archiving fails.
