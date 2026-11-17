# Network quality: measurement and scoring design

Status: stage 1 reference assessment implemented; broader coverage is a design
proposal. The five top-level categories, simple aggregation rules
and overall point budgets are agreed. Individual tests, endpoint choices,
per-metric scoring curves, internal category weights and release boundaries
still need review. This document is a capability roadmap, not a claim that these
tests already exist. The Overview radar now displays measured results;
the implementation scope and initial curves are listed in §13.

## 1. Product goal and scope

Provide a broad, explainable network assessment across real services and
networks, rather than a single speed test against one arbitrarily chosen server.
An operator should be able to expand a category into service results, then
inspect the measurements and path that explain a result.

The five categories are:

| Category | Question answered |
| --- | --- |
| Speed | How quickly can this connection transfer data to and from different services? |
| Latency | How long do network and service interactions take? |
| Stability | How consistently does the connection perform, including under load? |
| Functionality | Which services, protocols and connection capabilities work? |
| Wi-Fi | How well do observed wireless links perform? |

OLR serves users in every region. Do not divide targets, results, profiles or
scores into domestic/international groups or assume a user's home country.
Organize targets by service, provider/network and capability. Geography, where
known, can describe an actual endpoint or measurement location; it must not
define which services a user is expected to use. Test profiles can select
services and use cases, independently of region.

Coverage is intentionally incremental. An unimplemented test is not a failed
test. A comprehensive design does not require every test to run in every session.
Default measurements originate at the router over its default route; selected
exits and client-originated measurements are separate scopes. Router results
must not be presented as measurements of every client's experience.

## 2. Targets and measurement methods

Separate the target catalog from measurement methods and score calculations.
One target can supply evidence for several categories. A single connection can,
for example, produce connection timing and an access result; a download can
produce throughput and transfer-variation data. Reuse evidence without repeating
traffic or counting the same limitation several times in a score.

Each catalog entry needs a stable ID and version, service/provider labels,
supported test methods, endpoint/resource definition, expected response,
supported address families and protocols, and time/byte/concurrency limits.
Record actual connected IPs and effective endpoints for each run: DNS and CDN
selection can change the node serving a resource. Provider attribution needs
evidence; a service may use multiple CDNs.

The names below are candidate coverage, not verified endpoint integrations.
Before shipping an adapter, establish an appropriate, permitted test resource,
its expected behavior, operational limits and maintenance strategy. Some
services need authentication, signed media URLs, client execution or a
cooperating server. Unsupported integrations remain explicitly unavailable.
Do not replace a media test with a homepage fetch and keep the media claim.

Endpoint adapters should distinguish a changed resource, rate limit or service
challenge from evidence of a connection failure. Tests must have bounded
redirects and resource use. Operator-supplied targets need an explicit target
policy rather than inheriting arbitrary redirect or private-address access.

## 3. Speed

Preserve target-specific results and upload/download directions. Differentiate
short transfers, sustained throughput and single/multiple connections.

| Subtest | Candidate coverage | Measurements |
| --- | --- | --- |
| Public CDN download | Cloudflare, Akamai, Fastly, CloudFront | Sustained Mbps, transferred bytes, duration, ramp-up time |
| Additional CDN/network coverage | Alibaba Cloud, Tencent Cloud, Wangsu, Volcengine and other independently operated networks | Same measurements, organized by provider rather than region |
| Video media delivery | YouTube, Douyin, Bilibili, Netflix | Media-segment fetch time, delivery Mbps and sustained delivery capacity |
| Software/file distribution | GitHub Releases, OS/package mirrors and update sources | Download Mbps and long-transfer behavior |
| Game distribution | Steam and other supported distribution networks | Large-content download Mbps |
| Cloud storage | Supported object stores and file services | Download and, with a suitable endpoint, upload Mbps |
| Upload diversity | Multiple independent cooperating networks | Sustained upload Mbps per endpoint |
| Short/burst transfers | Fixed, documented resource sizes | Completion time and effective transfer rate |
| Single/multiple connections | Same endpoint and comparable conditions | Single-flow Mbps and multi-flow aggregate Mbps |
| Local link throughput | Client to router or a wired LAN test endpoint | Directional local throughput, separated from internet measurements |

Use repeatable payloads and record cache/compression behavior, stream count and
transfer size. Establish that a resource is large enough for the intended test.
A byte-capped or short transfer can yield a valid observed rate without proving
the connection's maximum capacity. Non-overlapping throughput tests avoid
unintentional contention; intentional concurrent tests are labeled load tests.

Media transfer measurements do not establish actual playback resolution,
startup, rebuffering or rendering performance without a player participating.
Likewise, one CDN resource does not benchmark every node in that provider.

## 4. Latency

Keep timing layers separate. The existing Overview HTTPS measurement times an
application round trip from an established connection to the first response
byte of a HEAD request. It excludes DNS, TCP/TLS setup and document download,
but includes server processing; it is not network RTT. See
[Overview latency](latency.md).

| Subtest | Measurements and interpretation |
| --- | --- |
| Client-to-router/local endpoint | RTT distribution; requires a client-side probe |
| Upstream path | RTT to responding upstream nodes; nonresponse alone is not a fault |
| Network RTT | ICMP round trips to multiple independent supported targets |
| TCP connection | TCP handshake duration per service/CDN target |
| TLS connection | TLS handshake duration, recording full/resumed sessions |
| DNS | Query duration and outcome through the configured resolver path; distinguish controlled cached/uncached cases where feasible |
| First byte | Request-to-first-response-byte time with explicitly defined timing boundaries |
| Small request completion | Complete response time for a bounded, known small resource |
| QUIC connection | QUIC/HTTP/3 establishment timing where supported |
| Service-specific response | Comparable measurements for selected video, search, developer, game and cloud service targets |

Preserve sample count, median and P95 where enough observations exist. Compute
statistics within each target/path/protocol before combining results. A target
at 20 ms and another at 100 ms do not imply 80 ms of jitter. TCP, TLS, RTT and
first-byte values are different metrics, not interchangeable samples. Server
processing contributes to application response time. Record fresh/reused
connections and precise timing definitions so comparisons remain meaningful.

## 5. Stability

Stability includes behavior under load; load responsiveness is a subtest, not
a sixth category. Long-term availability is useful historical context rather
than a separate radar axis.

| Subtest | Measurements |
| --- | --- |
| RTT variation | Within-target RTT variation and tail latency, with a versioned statistical definition |
| Probe loss | Sent/replied/timed-out counts for a specified protocol and target |
| Interruptions and recovery | Consecutive-failure events, duration, recovery time and observation coverage |
| Throughput variation | Time-bucketed rates during the same sustained transfer |
| Transfer stalls | Count and duration of intervals with no useful transfer progress |
| Connection reliability | Connection failures, resets and unexpected disconnects across repeated attempts |
| Long-lived sessions | TCP, WebSocket or controlled UDP session continuity |
| Download load | RTT increase, variation and probe loss during download relative to an idle baseline |
| Upload load | Same measurements during upload, kept separate from download |
| Bidirectional load | Same measurements with simultaneous upload and download |
| Time-of-day consistency | Historical per-target speed/latency distributions over comparable test conditions |
| Concurrent service experience | Small requests or controlled real-time traffic while bulk transfers run |

Store the idle baseline and loaded intervals together. Call a baseline idle
only when observations support that description; otherwise report ambient
traffic or an uncontrolled baseline. Tests that do not actually fill the
bottleneck must not claim to measure maximum-load behavior.

ICMP nonresponse can reflect filtering or rate limiting. TCP retransmissions
are transport evidence, not a direct measurement of physical-link packet loss.
Keep local collection errors separate from network timeouts. A failed single
service does not prove a whole-connection outage. Historical availability must
report observation gaps; daemon downtime is not measured internet downtime.

## 6. Functionality

Functionality describes supported uses. Reachability is evidence within this
category, not a separate top-level score. Distinguish observed network
properties from capabilities verified by an end-to-end test.

| Group | Subtest | Evidence |
| --- | --- | --- |
| Service access | CDN, video, social, search, developer, game and cloud services | DNS, connection, TLS and expected resource/application response per target |
| IP support | IPv4 internet access | Actual IPv4 transaction |
| IP support | IPv6 internet access | Address/route observations plus an actual IPv6 transaction |
| IP support | Dual-stack behavior | Per-family results; actual fallback behavior requires a participating client/implementation |
| Address properties | Public IPv4 assignment | Uplink address classification compared with externally observed address |
| Address properties | Upstream NAT or possible CGNAT | Address evidence and qualified inference, not an unsupported definitive label |
| DNS | Selected service names | Resolution outcome and checks against explicitly known expectations |
| DNS | Configured resolver transports | Actual use of configured DNS, DoH or DoT paths |
| DNS | DNSSEC validation | Acceptance of controlled valid records and rejection of controlled bogus records |
| Transport | TCP, UDP and QUIC/HTTP/3 | Protocol-specific transaction with a supporting endpoint |
| Inbound | IPv4 TCP and UDP | External peer reaches a specific temporary test listener/mapping |
| Inbound | IPv6 TCP and UDP | External peer reaches a specific IPv6 test listener |
| NAT/P2P | UDP mapping and filtering behavior | Multi-endpoint STUN observations with method and limitations |
| NAT/P2P | Actual UDP hole punching | Successful coordinated two-peer session establishment |
| NAT/P2P | Mapping lifetime | Observed mapping usability after documented idle intervals |
| NAT/P2P | NAT loopback | LAN client accesses an internal test service via the public entry point |
| Port mapping | Static mapping, PCP, UPnP where supported/configured | Protocol support and actual mapping effectiveness |
| Path compatibility | MTU and PMTUD | Controlled packet-size tests and evidence of discovery failures/black holes |
| Access conditions | Captive portal and unexpected redirection | Comparison with a controlled expected response |

An externally observed public IPv4 address does not prove that the router owns
that address: upstream NAT also exposes a public address. A global IPv6 address
does not prove inbound reachability. STUN success and NAT classification do not
prove hole-punch success. A successful test resource does not prove all features
of an authenticated service work. Test endpoint failure is not automatically
proof that the network lacks the capability.

Group access results by service, not raw URL count. Public address and NAT
properties explain outcomes; do not independently reward a public IPv4 when
the relevant inbound/P2P capability has already been scored. Disabled or
untested inbound access is not automatically a fault: capability scoring
depends on an explicit use-case profile. Report observed availability history
without penalizing the same interruption again under another category.

## 7. Wi-Fi

Wi-Fi results require a stated AP/client/link scope. The router may have no
radio or may use an external AP without telemetry. Unknown Wi-Fi quality must
remain unavailable rather than being inferred from internet speed.

| Subtest | Measurements | Dependency |
| --- | --- | --- |
| Signal and coverage | RSSI, SNR where available, variation by location | AP/client telemetry; coverage needs multiple locations |
| Negotiated link | Band, channel, width, standard, directional PHY rate | AP/client telemetry |
| Actual local throughput | Upload/download to a local wired endpoint | Client test participant and local endpoint |
| Local latency | Client-to-local-endpoint RTT and tail latency | Client test participant |
| Local stability | Local loss, variation and disconnects | Client probes and/or AP events |
| Radio delivery | Frame retries and failed transmissions | Supported driver/AP counters |
| Channel conditions | Channel utilization, noise and interference observations | Supported radio telemetry |
| Contention/load | Local response during single-client load and multi-client competition | One or more test clients |
| Roaming | Handoff interruption, loss and weak-AP persistence | Moving client and multiple APs |
| Mesh/backhaul | Backhaul signal/rate and actual throughput consistency | AP integration or suitable test endpoints |
| Device experience distribution | Per-device results and proportion of poorly performing observed links | Representative multi-device observations |
| Configuration capabilities | Bands, security modes and compatibility features | AP configuration/telemetry; primarily explanatory |

High negotiated PHY rate does not prove high useful throughput. Nearby AP count
does not by itself measure interference. Keep router-terminating local tests
distinct from forwarding tests and internet tests. Do not summarize an entire
site from one observed client without stating coverage. The reference
implementation (§13) chooses a first formula — one score per observed link,
averaged over the links in use — and states the scope it covers; a whole-site
coverage formula that would need clients at known locations is still not
chosen.

## 8. Data model

Use three layers: immutable measurement evidence, windowed metric summaries,
then versioned scores. These are conceptual contracts; exact Go/JSON types,
storage schema and API routes are implementation decisions for a later review.

### 8.1 Measurement evidence

Every observation references a finite run or background collection window and
records:

| Field group | Required information |
| --- | --- |
| Identity | Observation/run IDs, target/catalog version, method/version |
| Scope | Origin (router/client), client/AP/link where applicable, requested exit/interface and effective path evidence |
| Connection | Requested family/protocol, actual family/protocol, resolved/connected addresses and effective endpoint; DNS path separately |
| Conditions | Idle/uncontrolled/download/upload/bidirectional load, concurrency, connection reuse and ambient traffic evidence |
| Time | Start/end timestamps; elapsed durations measured with a monotonic clock |
| Outcome | Success, failure, inconclusive, skipped, unsupported, cancelled or collector error; failure stage and stable reason code |
| Measurements | Typed metric-specific values with explicit units, sample counts and bounded time series where needed |
| Resources | Bytes transferred, duration and applied limits |
| Relationships | Baseline/load links and shared evidence IDs across categories |

Metric payloads should be a typed union keyed by measurement kind, rather than
an unstructured string map. Failed or interrupted runs may preserve valid
partial observations, clearly marked as partial. Exclude credentials and signed
URL secrets from persisted/displayed endpoint metadata.

### 8.2 Metric summaries

Summarize comparable observations by target, scope, address family, transport,
method and load condition. Keep the window, attempted/valid/failed/error sample
counts, freshness, distribution statistics and referenced evidence IDs.

Examples include directional Mbps; per-target RTT median/P95; per-target probe
counts and variation; idle/loaded RTT deltas; individual functionality checks;
and per-client Wi-Fi metrics. Throughput and load metrics from the same speed
test share a run ID. Do not silently mix exits, clients, IPv4/IPv6, idle/load
conditions or observations separated by a path/configuration change.

### 8.3 Score snapshots

A score snapshot records its generation time, catalog version, use-case profile
ID/version, scoring version and the scope of each result. Each of the five
categories contains:

- State: measuring, ready, partial, stale or unavailable, plus a reason code.
- Nullable 0–100 score, oriented higher-is-better.
- Metric/subtest results and evidence references explaining the score.
- Actual measurement window and expiration, including per-component times
  where source measurements differ in age.
- Coverage: expected, implemented, attempted and usable scored components,
  their effective weights, and omitted components with reasons.

The snapshot also contains a nullable overall 0–100 score, its state and
coverage, and each category's point budget and earned contribution. Preserve
unrounded scores for calculation; round only for display. Expose the formula
and contributions so the overall result can be explained as a sum of points.

Collection outcome, freshness and score are distinct. No test means no score;
confirmed failure is usable evidence and may yield a low score. All RTT probes
timing out can establish probe failure but cannot supply a measured RTT of zero
or infinity. Unsupported, not selected, not implemented, collector error and
tested failure must remain distinguishable.

Snapshot generation time is not measurement time. A fresh background sample
must not make yesterday's speed test appear newly measured. Retention and
restart behavior must be explicit: a memory-only restart creates observation
gaps, and expired history remains historical rather than current evidence.

## 9. Scoring and presentation rules

Scores are approximate references, not precise predictions of every user's
experience. Keep aggregation understandable: normalize measurements to 0–100,
then use the following three rules. The reference assessment's initial curves,
weights and evidence requirements are defined in §13 and can evolve by version.

### 9.1 Three aggregation rules

| Relationship | Rule | Example |
| --- | --- | --- |
| Alternative ways to satisfy the same use | Maximum score | Either IPv4 or IPv6 can complete the same service transaction |
| Different aspects contributing to experience | Weighted addition using fixed point budgets | Speed, latency and stability contribute separate points |
| Repeated or representative comparable observations | Arithmetic mean, with explicit weights where needed | Repeated runs or independent services in the same group |

Define the question before choosing the rule. For basic service access,
`max(IPv4 access score, IPv6 access score)` represents either family working.
For dual-stack completeness, both families contribute separately. One family
working perfectly and the other failing must not reduce the basic access result
to 50 merely because two alternatives were tested.

Different CDN/service results contribute to breadth of experience: average them
within reviewed groups instead of taking only the fastest service. Balance
service/provider weights so extra URLs or repeated samples do not increase a
provider's importance. Publish which observations and weights form each mean.

Do not use addition to turn prerequisite stages into partial service success.
DNS, connection and TLS may all succeed while the required application response
fails; that transaction still failed. Keep stage results as diagnostic evidence.
Similarly, alternative paths cannot bypass the prerequisites of the use being
tested: record which path actually completed that use.

Each scored component needs a stable ID, a normalization rule, an aggregation
relationship, fixed weights/budget where applicable, required evidence and its
coverage policy. Not every diagnostic measurement needs to earn points.

### 9.2 Overall score: a 100-point budget

Keep each radar category on a 0–100 scale. These base weights express the
relative importance of categories with usable current results. Normalize their
weights to fill 100 points, including when some tests are not implemented yet.

| Category | Base weight |
| --- | ---: |
| Speed | 25 points |
| Latency | 25 points |
| Stability | 25 points |
| Wi-Fi | 15 points |
| Functionality | 10 points |
| Total | 100 points |

```text
overall = speed * 0.25
        + latency * 0.25
        + stability * 0.25
        + wifi * 0.15
        + functionality * 0.10
```

The formula above applies when all five categories are scored. In general:

```text
category = sum(scored component score * component weight)
         / sum(weights of scored components)
overall = sum(scored category score * category base weight)
        / sum(base weights of scored categories)
category effective points = 100 * category base weight
                          / sum(base weights of scored categories)
category earned points = category score * category effective points / 100
```

Only usable, fresh results enter either denominator. A confirmed zero is a
scored result and stays in the denominator. If no category can be scored, the
total is null. Every supported assessment can reach 100, regardless of how many
future tests remain unimplemented.

For example, scores of 80, 90, 70, 80 and 60 respectively contribute
20 + 22.5 + 17.5 + 12 + 6 = 78 overall points. Show this breakdown in details.
Use the same budget-and-addition approach within categories once their internal
weights are reviewed. With Wi-Fi unmeasured, scores of 80, 90, 70 and 60 use
base weights 25, 25, 25 and 10, giving 77.6 overall out of 100. No geometric
means, squared penalties or hidden shortfall multipliers are needed for the
overall score.

### 9.3 Coverage and presentation

Coverage is separate from quality. An unimplemented test is not evidence of a
poor network. Exclude unimplemented, unselected, insufficient, invalid and stale
results, then explicitly normalize the remaining weights within categories and
across categories. The result view leads with category and test scores and keeps
the arithmetic out of the page: it never asks a reader to add up weight
contributions, and the rule that produces the total is documented here rather
than printed on every visit. Each piece of evidence sits under the row it
scores, opened by that row, so the page shows one level of claim at a time.
An unmeasured Wi-Fi category remains a dash while the measured categories share
100 points. A ready total means the current assessment is scored, not that the
entire planned test catalog has been implemented or measured. The UI groups the
same reference score into presentation labels — Excellent 90–100, Good 75–89,
Fair 60–74 and Poor below 60 — without changing normalization. Missing,
cancelled, timed-out and expired results show a dash and no label rather than a
number.

For an alternative-path maximum, choose the higher normalized measured score
and retain its path evidence. Never treat an unknown alternative as a confirmed
failure. Cancelled or timed-out assessments retain partial evidence but do not
publish a finished total; running and expired assessments also withhold it.

- Preserve raw results even when no defensible score formula exists yet.
- Review absolute versus expected-bandwidth speed scoring. Do not silently use
  Overview display limits or QoS shaping configuration as the user's target.
- Group and weight by service/network/capability before aggregation, so adding
  many URLs for one provider cannot dominate a category.
- Profiles express chosen services/use cases, with versioned weights. They do
  not define regional expectations. Scores from different profiles, coverage
  or formula versions are not automatically comparable.
- Missing tests never become zero. Show which categories/components supplied
  the result and which were excluded. Enforce each metric's minimum evidence
  before scoring, without reserving points for future implementations. Adding
  scored tests or changing their weights updates the scoring version.
- Avoid scoring the same outage, NAT restriction or access failure repeatedly
  just because several derived checks describe it.

The radar retains five axes but cannot turn absent axes into zero or draw an
apparently complete measured polygon from missing values. Provide an accessible
text/detail view with scores, raw values, freshness, scope and coverage. Do not
mix simulated and live scores into a single apparently measured result. This
implementation replaces the simulated preview with measured results and a
normalized total for the available scope.

### 9.4 Report layout

The assessment report is one column: an overall-score band, then the five
categories. A category opens to its scored tests, and a test opens to the
measurements behind it. Nothing is summarised twice and nothing is explained in
a block of its own.

- The overall band carries the score, its grade label, how many categories were
  scored and the measured coverage. Coverage alone needs a sentence — it does
  not reduce the score — so it is the one figure with a hover hint.
- Speed opens to the reference transfer: download and upload in Mbps, the
  server it used, and the one-server caveat. These numbers are the evidence
  behind the category, not a second score beside it.
- Latency, Stability and Functionality list one row per service. Opening a
  service shows its own attempts, weighted to the question that category asks:
  DNS/TCP/HTTPS timings for Latency, the ordered success sequence for
  Stability, and the resource outcome and error for Functionality. Because one
  set of six attempts feeds all three, the same attempts appear in each, each
  time showing only the columns that category is scored from.
- NAT observations are Functionality evidence, not a category and not a score.
  They appear as a row with no number, say "Diagnostic only, not scored", and
  explain in their own detail why an observed mapping proves no inbound access.

UI changes update the
[content guide](ui-content-guide.md) and [style guide](ui-style-guide.md).

## 10. Execution and existing building blocks

Separate lightweight background sampling, on-demand assessment, and specialist
tests requiring a client or external peer. A snapshot read must not launch tests.
Bandwidth/load tests run on demand initially, with progress, cancellation,
duration/byte budgets and controlled concurrency. Only deliberate load tests
should compete with another measurement. Report limits and partial completion.

Inbound, P2P and mapping checks need an explicit test lifecycle. Ordinary
assessment must not silently change firewall, forwarding, UPnP/PCP or other
router configuration. Any temporary listener/mapping needs a bounded lifetime
and owned-resource cleanup; its implementation is a separate reviewed step.

Existing code provides useful starting points, not complete catalog coverage:

| Building block | Current capability and gap |
| --- | --- |
| Overview radar | Measured partial reference assessment with raw evidence; complete coverage remains future work |
| Gateway latency monitor | Single HTTPS HEAD application-round-trip sample against a saved target, plus separate DNS timing; not network RTT or a quality history store |
| Tools speed test | Router-originated download/upload/latency/jitter against Ookla (selectable server), Cloudflare or fast.com; all three accept a way out, and the two command-line services take it through an nftables socket-group mark; not per-service or per-CDN coverage beyond those three |
| Tools ping | Finite ICMP RTT/loss test; not a continuous representative stability window |
| Tools NatTypeTester | Default-route IPv4 NatTypeTester 10.0.10: its RFC 3489 probe names the NAT type and its RFC 5780 probe reports mapping/filtering, run in parallel against one STUN server with a second server as fallback; a partial answer is reported when only one probe completes; still subject to server support, and not actual P2P or inbound verification |
| LibreSpeed and iperf3 | Browser-based LibreSpeed test between one device and this router (download/upload/latency/jitter), plus a temporary iperf3 server for sustained wired throughput; neither supplies whole-site Wi-Fi telemetry |

See [device link testing](device-link-test.md) for local-test scope. Reuse test
implementations behind shared measurement interfaces where appropriate rather
than invoking existing HTTP handlers from a new scoring system. Ownership of
the coordinator, history retention, API endpoints and client/AP integration
remain implementation decisions.

## 11. Suggested delivery stages

Stages describe dependencies and useful increments, not a commitment to ship
every catalog entry or all five scores at once.

| Stage | Deliverable | Intentionally deferred |
| --- | --- | --- |
| 1. Measurement foundation | Small verified target catalog; bounded on-demand run model; evidence/freshness/coverage; existing speed, ping, NatTypeTester and observed address data exposed with honest scope; basic IPv4/IPv6 service transactions | Broad provider claims, complete category scores, whole-site Wi-Fi |
| 2. First useful quality assessment | Several independently operated verified transfer/response endpoints; target-level latency and repeated-probe stability; basic service-access results; idle versus download/upload load measurements; reviewed scoring for sufficiently covered categories | Broad media integrations, coordinated inbound/P2P, AP integrations |
| 3. Rich service coverage and history | Additional CDN, media, distribution and storage adapters; protocol/DNS diagnostics; time-of-day trends and observed availability; optional additional exit scopes | Tests without suitable resources or adequate observation history |
| 4. Cooperative diagnostics and Wi-Fi | External-peer inbound/P2P, mapping lifetime, client/local-link probes, supported AP telemetry and later roaming/backhaul tests | Universal AP support or whole-site claims without representative data |

The smallest first implementation can be a subset of stage 1. Review exact
targets and acceptance criteria before coding; do not enable a five-axis live
score merely because five category containers exist.

## 12. Validation and decisions still open

Implementation validation should cover timing/statistical definitions, partial
runs, target changes, errors versus failures, path/family isolation, stale data,
coverage and scoring boundaries. Use controlled test servers for collectors and
deterministic evidence fixtures for aggregation. Verify cancellation, traffic
budgets and resource cleanup. Live service checks are bounded integration
checks, not unbounded public-network dependencies in ordinary unit tests.

Next reviews should settle:

1. The smallest verified target/resource set and per-method traffic budgets.
2. Absolute versus expected-bandwidth speed scoring and use-case profiles.
3. Sampling cadence/windows, minimum evidence, expiry and history retention.
4. Per-metric curves, internal category weights, minimum coverage and
   partial-score UI; overall category budgets are fixed in §9.2.
5. Coordinator/API ownership and reuse of current Tools implementations.
6. External-peer operation and the first supported Wi-Fi/client data source.

## 13. Reference implementation: reference-v1 / quality-v4

This is an initial reference assessment, not the complete service catalog.
Snapshots identify the fixed `reference` profile at version `1` separately
from the scoring and catalog versions.
Every measurement originates at this router or at an access point it manages:
the service probes and the reference transfer leave over the router's default
route without environment proxies, and the Wi-Fi evidence is what each managed
access point reports about its own clients. The snapshot scope is
`router-default-route`, or `router-and-access-points` once wireless evidence is
present. The UI is reached through the Quality readout on Overview,
whose assessment page at /quality uses the shared header, content area and
footer, with Overview selected in navigation.
It replaces the fixed sample scores and renames Reachability to Functionality.

### API and lifecycle

- `GET /api/tools/quality` passively returns the latest snapshot and calculated
  category/overall results. It is also published as the `tools_show_quality`
  read tool. The initial state is `idle`, with null scores.
- `POST /api/tools/quality?scope=all&speed=true` starts a finite asynchronous
  full assessment and returns 202. The assessment page always sends
  `speed=true`; API callers may omit it for a lighter run. `scope=responses`
  reruns every service probe, `scope=target&target=cloudflare` reruns one
  service, `scope=speed` reruns the reference transfer, `scope=nat` reruns the
  NAT observations and `scope=wifi` rereads the access points. A scoped run
  requires a completed snapshot and merges its fresh evidence into the other
  results; `scope=wifi` is refused with 400 when no access point source exists. Another assessment, reference speed
  test or NatTypeTester test holds the shared runner reservations and produces 409.
  Regular user traffic is not controlled.
- `DELETE /api/tools/quality` requests cancellation. The runner releases its
  reservations after stopping and retains completed evidence. Daemon shutdown
  also cancels it. Closing the assessment view does not cancel a test.
- A full assessment replaces the previous in-memory result. A scoped run
  replaces only the targets, speed, NAT or wireless evidence it names and
  recalculates every affected score. While a scoped run is active, `running_scope` and
  `running_target` identify it and `state` stays `completed`, so readers keep
  the last scores; `completed_at` advances to the last successful update. A
  cancelled scoped run changes neither the published evidence nor the
  checkpoint. Tests run only on demand. Scores do not expire and stay available
  until a full assessment or a scoped rerun replaces their evidence. Finished
  assessments (including cancelled or timed-out full runs with partial evidence)
  are atomically saved to `/var/lib/open-linux-router/quality.json` and
  restored after daemon or router restart. An interrupted run leaves the
  previous checkpoint intact until it finishes; an abrupt shutdown therefore
  restores the last finished run. Category components carry `target` and `kind`
  so a reader can group the timing, stability and access measurements of one
  service into that service's single test item.

The run first samples the following fixed resources with IPv4 and IPv6
independently: Cloudflare `https://speed.cloudflare.com/__down?bytes=0` (200),
Google `https://www.google.com/generate_204` (204), and GitHub
`https://api.github.com/zen` (200). Each target/family makes six sequential fresh
HTTPS HEAD requests; the six target/family workers run concurrently. Each
request has a five-second deadline, a 32 KiB response-header limit and no
redirect following or document-body transfer. There is no uncached-DNS claim.
Only the first resolved address for each family is attempted in each sample.

Evidence includes start/end time, actual address, HTTP status, failure stage,
DNS lookup time, TCP connect time excluding DNS, and time from established TLS
connection acquisition to the first HTTP response byte. TLS certificates are
verified. Unexpected HTTP statuses, including challenges and rate limits, are
inconclusive access evidence rather than a scored network failure. An expected
status verifies only that HEAD resource transaction; it does not verify response
body content or all functionality of the named service.

The Cloudflare reference transfer runs after response sampling in a full
assessment, or alone when its test is rerun, with a 90-second deadline. OLR
invokes the upstream Rust `cloudflare-speed-cli` with JSON output and history
saving disabled; OLR does not reimplement its
measurement algorithm. The packaged version is v1.0.10, with six concurrent
workers and ten-second download and upload phases. It has no fixed byte cap
and can consume substantial data. The assessment page always runs it; the API
leaves `speed` optional and off by default for callers that need a lighter run.
This measures the router's default route to Cloudflare, not Wi-Fi or all
internet services. Speed scoring uses the existing curves, weighting download
70% and upload 30%; missing, invalid or timed-out transfers are excluded.
Scoring version `quality-v3` identified the reference provider change; `quality-v4`
adds the wireless evidence and its curve.
The standalone speed tests are one tool each — Speedtest by Ookla, Cloudflare
Speed Test and Fast.com — because the three measure different paths and an
operator arrives already knowing which one they want; a page whose command is
missing says so instead of offering the test. Ookla keeps its nearby server
picker and probes latency only when the test runs. Cloudflare uses the same
packaged CLI as the reference transfer, and Fast.com uses the packaged Zig
`fast-cli`. All three accept a way out: Ookla marks its own sockets with
`SO_MARK`, while the two command-line tools are started under a reserved group
id and an nftables rule marks every socket that group opens with the exit's
fwmark (`meta skgid … meta mark set …` in a `type route` output chain). That is
the mechanism the inspection proxy already uses for `mitmweb`; the group is
never created in `/etc/group`, the table is `olr_speed` and not `olr_route`, and
a zero mark means the router's default route and installs no rule. A chosen way
out also refuses that group's IPv6 sockets (`meta nfproto ipv6 reject with
icmpv6 admin-prohibited`). Exit tables send IPv6 to `unreachable`, but a local
socket settles its source address on the default route before this chain runs,
so an AAAA connection would sit in SYN-SENT until TCP gave up instead of
failing; refusing the family at once lets a tool that resolves both fall back to
IPv4 — `fast-cli` opens every address it resolves — rather than outliving the
run's 90-second deadline. What is left of that class of failure is the exit's
own path: a connect that never answers. Each command-line attempt is therefore
capped (40 seconds for Fast.com, 60 for Cloudflare) and a run that ends without
a measurement is given a second process inside the same 90-second budget. The
tools resolve their hosts and open their connections themselves, so the next
attempt usually gets through; Ookla, which walks its own server list, is not
retried. The two command-line services cannot choose a server, because the
service does. Fast.com
reports latency and download/upload but no jitter, and its numbers describe the
Netflix Open Connect path rather than general internet quality.

A running test publishes its stages, not only its end. While `POST
/api/tools/speedtest` is open, `GET /api/tools/speedtest/progress` reports the
stage in flight, the server being tested, the values already settled and the
rate the current transfer is reaching; the elapsed time comes from the run's own
start on the router's clock. Ookla can report latency, download and upload
separately because the library runs them in that order, and its transfer
callbacks supply the live rate. The tool page draws that run as two dials on a
logarithmic 0–10 Gbps scale, the way LibreSpeed draws its own: the direction
being measured carries its live rate, the other holds its last reading, and both
settle on the result with latency and jitter above them. The two command-line
services measure both directions inside one process whose only output is the
final JSON, so they report one `transfer` stage and no rate: their dials stay
empty until the result arrives rather than inventing intermediate numbers. The
page polls, like the rest of the UI, because EventSource cannot send the API's
`Authorization` header. A stage that has not measured anything stays absent
rather than becoming a zero, and the live rate is dropped as soon as its
transfer ends.

The `.deb` includes the unmodified CLI, its GPLv3 license and corresponding
source under `/usr/share/doc/olr/cloudflare-speed-cli`. `make cloudflare-speed`
downloads pinned amd64/arm64 musl builds and verifies committed SHA-256 hashes.
Build hosts need curl, tar, xz and sha256sum. Single-binary tarball users must
install `cloudflare-speed-cli` separately on the router's PATH, using the
[upstream release](https://github.com/kavehtehrani/cloudflare-speed-cli/releases/tag/v1.0.10)
or `cargo install cloudflare-speed-cli --version 1.0.10 --locked`.
An absent CLI is reported as an unmeasured speed component, without silently
falling back to another provider. Proxy environment variables are removed from
the child process so they cannot change the measured path.

The `.deb` also includes the unmodified MIT `fast-cli` v0.3.10 and its license
under `/usr/share/doc/olr/fast-cli`. `make fast-cli` downloads pinned
amd64/arm64 static builds and verifies committed SHA-256 hashes; build hosts
need curl, tar and sha256sum. Single-binary tarball users must install
`fast-cli` separately on the router's PATH, using the
[upstream release](https://github.com/mikkelam/fast-cli/releases/tag/v0.3.10).
The tool has the fast.com app token compiled in, so a token rotation needs a
newer release; the test reports the CLI's error and does not fall back to
another service. The token is not the only way the API refuses a run: from a
source address Netflix does not serve — mainland China among them — it answers
`403 Not Available` before any transfer starts. `fast-cli` puts that reason on
stderr while its JSON only says `Failed to get URLs`, so the tool passes the
last stderr line through to the page's technical details; reading it separates a
blocked path from an expired token. A blocked path needs a way out in a country
the service serves, not a newer binary.

The `.deb` also includes the unmodified MIT NatTypeTester 10.0.10 console binary
as `/usr/bin/natty`. `make natty` downloads the pinned linux-x64/linux-arm64
archives and verifies committed SHA-256 hashes; build hosts need curl, 7z and
sha256sum. The binary is native AOT and needs no .NET runtime, but it needs ICU
for globalization; the tool runs it with
`DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1` so a box without libicu still gets
the English labels its output is parsed by. The upstream linux builds need
glibc 2.38 or newer, which Debian 13 and Ubuntu 24.04 provide. Single-binary
tarball users must install the [upstream console
release](https://github.com/HMBSbige/NatTypeTester/releases/tag/10.0.10) as
`natty` on the router's PATH. Its two probes run in parallel and share a
15-second budget; a partial answer names the probe that failed. NAT results are
diagnostic only and do not prove public IPv4 assignment, inbound or P2P access.
A full assessment has a 150-second deadline; scoped reruns use shorter deadlines
(65 seconds for all service probes, 40 seconds for one service, 100 seconds for
the reference transfer, 20 seconds for NAT and 45 seconds for the access-point
read). No configuration changes, temporary inbound listeners or port mappings
are made.

### Wireless evidence

The Wi-Fi category reads the access points in the network equipment inventory;
it does not measure Wi-Fi from this router, which has no radio. Every device
with the `ap` role is read through the same equipment service the Wireless page
uses, bounded at 15 seconds each, and the read collects each association's
client MAC, radio and band, signal, noise, signal-to-noise ratio (only when the
access point reported both) and directional rates. An access point that does
not answer is listed as an error beside its peers instead of shrinking the
client count; if none answers the read fails and the previous links stay with
`wireless_error` naming the failure. `wireless_at` dates the evidence.

Each link is scored from its signal strength (0 at −95 dBm, 10 at −85, 25 at
−80, 45 at −75, 65 at −70, 75 at −67, 90 at −60, 100 at −50), capped by SNR when
the access point reported a noise floor (20 at 10 dB, 45 at 15, 70 at 20, 90 at
25); SNR can only lower a link's score. The category score is the mean over the
observed links, not a median: a client in a dead corner is the case a median
would hide. A link without a reported signal level is skipped rather than
scored zero, and a read that observed no link at all leaves the category
unscored rather than zero.

This is a stated-scope first formula, not a coverage claim. It sees only the
clients associated at the moment of the read: a device that dropped off, an
empty room and an idle link are outside it. The negotiated rate is kept as
evidence but is not scored — a radio agreement is not throughput — and retries,
failed transmissions, airtime and channel utilization stay unknown because the
tested Beam WRT build grants neither `iwinfo.survey` nor hostapd's retry
counters. Measuring one link's actual speed still means running LibreSpeed or
iperf3 on that client (`docs/device-link-test.md`); it needs a participant and
is not part of the scheduled assessment.

### Component base weights

| Category | Implemented components | Future/unmeasured components |
| --- | --- | --- |
| Speed (25 points) | Reference transfer: 40% of category; within this, download 70%, upload 30% | Service-specific transfers: 60% |
| Latency (25 points) | TCP connect 60%, DNS 15%, HTTPS response 25%, each averaged equally across the three targets | Unsupported/missing timings remain unscored |
| Stability (25 points) | Connection establishment success 40%; TCP timing variation 30%, averaged equally across the three targets | Behavior under load: 30% |
| Wi-Fi (15 points) | Measured wireless links: 100% — each observed link scored on signal and SNR, then averaged | Client throughput, retries/airtime, roaming and mesh measurements |
| Functionality (10 points) | Test-resource access: 50%, averaged equally across the three services | Protocol capabilities 25%; inbound/P2P 25% |

For access, either family returning the expected status earns that service's
100; both families failing all attempts yields 0. Inconclusive or incomplete
evidence with no success remains unscored. For timing and stability, select the
family with the higher normalized measured score per target/category;
keep that family's fields together rather than mixing paths. This is a best
measured family reference, not a prediction of a particular client's family
selection or fallback. Missing alternatives are not known failures.

Timing scores require at least four valid measurements and use their median.
Connection continuity requires all six attempts with non-collector outcomes and
is `100 * established TCP connections / attempts`; it is not packet loss or
long-term availability. Timing variation uses the mean absolute difference of
consecutive successful TCP connect durations, requires at least three adjacent
pairs in a six-attempt run, and does not bridge failed attempts. These are short
reference samples, not representative historical stability or loaded behavior.

### Initial normalization curves

Use linear interpolation between these anchors and clamp beyond the endpoints.
Negative and non-finite measurements are invalid, except signal strength, which
is negative by nature and clamps at its lowest anchor. These initial experience hints
are versioned and can be calibrated later without changing the aggregation
rules. They are not statistical confidence estimates.

| Metric | Measurement → score anchors |
| --- | --- |
| Download Mbps | 0→0, 3→20, 10→40, 25→60, 75→80, 150→90, 500→100 |
| Upload Mbps | 0→0, 1→20, 3→40, 10→60, 30→80, 75→90, 200→100 |
| TCP connect ms | 0→100, 10→100, 30→90, 60→75, 100→60, 200→35, 500→0 |
| DNS lookup ms | 0→100, 10→100, 30→90, 60→75, 100→60, 200→35, 500→0 |
| HTTPS response ms | 0→100, 30→100, 100→90, 200→75, 500→50, 1000→20, 2000→0 |
| TCP variation ms | 0→100, 2→95, 5→85, 10→70, 20→50, 50→20, 100→0 |
| Signal dBm | −95→0, −85→10, −80→25, −75→45, −70→65, −67→75, −60→90, −50→100 |
| SNR dB | 0→0, 10→20, 15→45, 20→70, 25→90, 30→100 |

Per category, normalize scored component weights to sum to 1. Reference
transfer therefore supplies 100% of the current speed score; stability uses
connection success and timing variation in the ratio 4:3 when both are usable;
functionality uses the equal mean of scored service-access results; Wi-Fi uses
the mean of its scored links.

Normalize the base weights of scored categories to a 100-point total. With
bandwidth enabled and every category measured, Speed, Latency, Stability, Wi-Fi
and Functionality hold effective point budgets of 25, 25, 25, 15 and 10.
Without bandwidth testing they become 33.33, 33.33, 20 and 13.33 for Latency,
Stability, Wi-Fi and Functionality. Without a readable access point Wi-Fi stays
unmeasured and excluded, and the remaining budgets become 29.41, 29.41, 29.41
and 11.76 with bandwidth, or 41.67, 41.67 and 16.67 without. Any of these
assessments can score 100.

The API exposes component `weight` (base) and `effective_weight` (normalized
within the category). Category `budget` retains its base weight;
`available_points` is its effective share of the normalized total and
`earned_points` is its score times that share divided by 100. Overall
`available_points` is 100 when any fresh category is scored; `earned_points`
is the sum of category contributions. `coverage_percent` retains the weighted
fraction of the planned catalog with usable evidence as separate metadata;
it neither reduces the quality score nor gates it. Category `partial` denotes
limited measured scope, while overall `ready` denotes a scored assessment.
Running, cancelled, timed-out and stale snapshots withhold the overall score.

Scoring version `quality-v2` introduces this normalization; `quality-v1`
withheld totals pending the full planned catalog. The reference profile and
catalog remain version 1 because their targets and base weights are unchanged.
Future tests and weights can evolve by version without reserving points now.
