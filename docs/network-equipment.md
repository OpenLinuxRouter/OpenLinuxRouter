# Network equipment contract, version 1

`network` owns managed network equipment, its connection settings and brand adapters. It is distinct from `devices`, the existing MAC-keyed client inventory, and from `link`, which configures interfaces on the local router. This increment supplies the backend contract and two adapters; it does not rename the Gateway UI, draw a physical topology, provision a fleet or change local routing.

## Identity and types

A configured device has an operator-assigned stable `id` (1–64 ASCII letters, digits, underscores or hyphens, starting with a letter/digit), optional name, adapter (`mercury` or `openwrt`), HTTP(S) management origin, username, password environment reference and optional roles. Roles are a set of `router`, `switch`, `ap`: a single device may have several. An IP address, interface MAC or SSID is not a hardware identity. Manufacturer/model/firmware/MACs/uptime are observations, separate from the inventory.

Port, radio and wireless network IDs are scoped to the managed device. References must carry the device ID across devices. Mercury port IDs are decimal strings (`1` through `9`); OpenWrt uses netifd interface names. Names may change if the underlying platform renames an interface. Do not infer stable hardware identity from OpenWrt ifindex or interface name alone.

## Data contract

`GET /api/network/contract` publishes `version: 1` and JSON Schemas derived from the Go structs for devices, snapshots, FDB queries/results and operations. `/api/schema` publishes inventory config; `/api/routes` publishes route metadata. Additive optional fields may extend v1; changes in field meanings need a version change.

A snapshot contains:

| Group | Fields and semantics |
| --- | --- |
| Identity | Manufacturer, model, serial when known, firmware, hostname, interface MACs, roles, uptime seconds. |
| Collections | A status for each independently read section: `ok`, `unsupported`, or `error`; timestamp and sanitized diagnostic. A failed section does not suppress successful sections. |
| Capabilities | Named abilities with `supported`, `unsupported`, or `unknown`, plus an optional reason. Ability to read never implies ability to write. |
| Ports | ID/name, canonical kind (`ethernet`, `fiber`, `bridge`, `vlan`, `lag`, `wireless`, `other`), MAC, parent, members, addresses, administrative enabled and actual link state, speed Mbps, duplex, RX/TX counters and optional Mbps readings, PVID, optional PoE. |
| VLANs | Device-local VID, name, tagged and untagged port IDs; `vlan_enabled` separately describes whether the switch's VLAN mode is active. Equal VIDs on separate devices do not prove connectivity. |
| FDB | MAC, optional VLAN, reachable port ID, optional learning type. A lookup is distinct from exporting the whole table. |
| Neighbors | Local port, remote chassis/port/name, discovery source, optional expiry. Reserved in the common model; neither initial adapter supplies LLDP neighbors. |
| Radios | ID, administrative enabled, band, runtime channel and channel width when known, the standards the driver advertises, noise floor, transmit power, country and recent channel utilization. `configured_width_mhz` separately reports the netifd configured width. |
| Wireless networks | Unique device-local ID, radio/interface IDs, SSID, BSSID, mode, encryption mode (never key), and netifd logical network names. An SSID can occur on many radios/APs. |
| Associations | Client MAC, wireless network and radio IDs, signal and average signal dBm, noise, connected and inactive time, directional link rates, MCS and width, expected throughput, retries, failed transmissions, bytes and packets, transmit airtime, MFP and WMM state. Link rate is not throughput. Missing counters stay unknown. |
| Scan | On request and outside the snapshot: one radio's neighbours — BSSID, optional SSID, primary channel, width, signal, transport family and security family. Duplicates by BSSID are collapsed and rows arrive in channel order. A scan is a dated sample taken while the radio was off its channel, never current neighbourhood state. |

Missing optional metrics mean unknown/unavailable, not zero or false. `ok` with no rows means a successfully observed empty inventory. An absent row array with `unsupported` or `error` is not an empty network. Counts are unsigned 64-bit integers; consumers must preserve integer precision. RX/TX always use the reporting port/AP's own receive/transmit perspective. Counters may reset/wrap; no width or uninterrupted continuity is promised. Mercury RX/TX packet totals count good packets with errors separate, not cumulative bytes.

HTTP calls are sequential observations, not an atomic hardware snapshot. Timestamps bound an observation attempt; separate sections can represent different moments. Reads are on demand and bounded (service request budget 30 seconds), serialized by equipment lane. This increment does not persist snapshots, poll in the background or present old readings as current. A failed poll must not become an offline hardware assertion. In particular, an authentication failure does not prove the network link is down.

## Capability vocabulary

Initial canonical keys are `identity.read`, `ports.read`, `statistics.read`, `vlans.read`, `pvids.read`, `fdb.lookup`, `fdb.list`, `neighbors.read`, `radios.read`, `wireless.read`, `associations.read`, `ports.configure`, `vlans.configure`, `wireless.configure`, `poe.configure`, `device.reboot`, `client.disconnect`.

`unsupported` means unavailable through this implemented adapter/API or current permission set, not a claim about all capabilities of the hardware. `unknown` means observation could not establish the answer. Missing capability keys must be treated as unsupported by a client. Device-wide operation support means at least one suitable target; preview revalidates the exact target and permissions.

## Adapter boundary

The mandatory Go contract is `Adapter.Read(ctx, Device) (Snapshot, error)`. Optional `FDBLookup.LookupFDB` handles a known MAC, `Scanner.Scan(ctx, Device, radioID)` takes one dated neighbour sample, keyed by the radio ID the snapshot reports, and `Operator.Plan` / `Operator.Execute` handle typed operations. A scan is separate from `Read` because it interrupts the radio; no snapshot ever contains one. Brand protocol and credentials stay in the adapter; normalization and cross-device topology inference belong above it. The service receives adapters explicitly, without a dynamic plugin loader.

An adapter supplies evidence, not a parent tree. Future topology inference should preserve local/remote endpoints, physical adjacency vs reachability vs wireless association, evidence source, age and uncertainty. Manual links must remain distinct from automatic evidence. Multipath, LAGs and wireless backhaul require a graph, not a single parent ID. This increment exposes the source facts; it does not fabricate inferred direct cables.

## Mercury SE109 Pro

Adapter `mercury` targets SE109 Pro, captured on firmware `1.0.0 Build 20240914 Rel.40108`:

- Reads `MainRpm.htm`, `PortStatisticsRpm.htm`, `Vlan8021QRpm.htm`, `Vlan8021QPvidRpm.htm` independently.
- Supports identity, physical ports, Mbps readings, good packet/error counters, VLAN membership and PVIDs. Array placeholders beyond `max_port_num` are excluded.
- Parses a bounded subset of JavaScript literals (unquoted keys, single/double quoted strings, hex integers, arrays/objects, trailing commas). It never evaluates JavaScript. Expressions, calls, duplicate keys, excessive nesting and oversized payloads are rejected.
- Reads firmware password-encoding constants only after matching the captured algorithm; an unknown algorithm is refused before submitting a password. Uses an isolated CookieJar, at most one password login per operation, a dynamically obtained `g_tid`, default TLS validation and no redirects.
- Known-MAC lookup calls `mac_address_search.cgi` with the captured form fields. Full FDB export, LAG result numbering, LLDP, byte totals, PoE and remote writes are not implemented. MAC lookup requires successful runtime parsing; nonempty lookup results are covered by synthetic protocol fixtures, not a new live-hardware verification.

The sanitized fixtures derive from the earlier read-only research captures. No password, SessionID or live token is committed. Firmware changes require compatibility verification.

## OpenWrt

Adapter `openwrt` speaks uhttpd ubus JSON-RPC at `<origin>/ubus`. The device needs uhttpd ubus/rpcd available and an account with appropriate ubus ACLs. There is no SSH/remote shell fallback. Username defaults to `root` when omitted; a dedicated account with only the required methods is also supported.

Read methods: `system.board`, `system.info`, `network.device.status`, `network.interface.dump`, `network.wireless.status`, per-AP `hostapd.<ifname>.get_status` and `get_clients`. Optional `iwinfo.info` and `iwinfo.assoclist` enrich the radio with observed width, noise, transmit power and country, and clients with signal, counters, rates, MCS and expected throughput. `iwinfo.scan` answers only an explicit per-radio neighbour request, under a 25-second per-call bound instead of the usual 8. When a build does not grant netifd's wireless object or hostapd's status and client methods to the management session, the adapter falls back to LuCI's read model (`luci-rpc.getWirelessDevices`) and `iwinfo.assoclist`; utilization, MFP/WMM and airtime then remain unknown rather than failing the section. `iwinfo.survey` is not used: the tested firmware denies it, so there is no measured per-channel airtime to show. Scan results publish no hidden SSID guess and no key material — only the beacon's own SSID, which is empty for a hidden network. Wireless keys returned inside netifd configuration are discarded and never included in snapshots/errors. Runtime method/permission failures are reported by section. Netifd's generic `Network device` does not establish Ethernet media, so its port kind remains `other`.

Stock portable ubus does not expose the bridge FDB, switch VLAN/PVID table or LLDP neighbors used by this adapter; these remain unsupported. Different hostapd/wpad/rpcd builds can expose different methods. AP role can be observed from AP interfaces; full router/switch role detection is not inferred merely from OpenWrt being installed.

Only `client.disconnect` is implemented as a remote mutation. It validates a unicast MAC and an observed AP interface, reads hostapd status, checks `del_client` method signature and checks session ACL permission. It sends `addr`, `deauth: true`, `reason: 5`, `ban_time: 0`. This disconnects rather than permanently blocks a client; it may reconnect immediately. SSID/channel/VLAN/port configuration and reboot are not implemented.

OpenWrt protocol fixtures are synthetic, matching the supported stock API shapes; this change has no live-device compatibility claim. An actual firmware/account combination should be checked with a snapshot before relying on its reported capabilities.

## Web page

`/equipment` is the inventory in the WebUI: one row per managed box, with its
adapter, management origin and roles, and one page per device at
`/equipment/{id}`. Adding, editing and forgetting happen there, through the same
dialog the Wireless page uses for its access point; the empty state offers a
switch or an access point and stores the management password in the
configuration, so onboarding never requires setting a variable in `olrd`'s
environment.

The device page is read-only and grows its tabs from the snapshot rather than
from a device type:

| Tab | Shows |
| --- | --- |
| Overview | Identity, the state of every collection this read attempted, and the device's own capability list with each reason. |
| Ports | Link state, speed and duplex, packet or byte counters, and error counters, in the device's own port order. |
| VLAN | Device-local VLANs with tagged and untagged ports, and whether VLAN mode is active. |
| MAC lookup | A bounded `fdb.lookup` for one address, with an optional VLAN. |

Wi-Fi stays on `/wireless`: an access point's radios, clients, channels and
scans are a view of that device, not a second copy of it, and the device page
links there. Mercury switches have no radios, so the link is absent.

The list is configuration only. Opening a device is what contacts it — the
daemon reads on demand and nothing polls the hardware from the page, so a row
does not claim liveness it has not observed. A section the adapter cannot read
renders as unavailable with the adapter's reason, never as an empty table.

The URL is `/equipment`, not `/network`: in this product a network is the subnet
object Gateway serves (and `/gateway/networks` already redirects to DHCP), while
`/api/network` is the module's own route prefix. The page and the API are
deliberately not named the same thing, because they are not the same noun.

## Inventory and secrets

Inventory lives in the `network` subtree of the existing configuration store, with strict decoding, shared apply lock, ETags and preservation of other modules. Configuration backup includes the inventory and validates it on restore, without connecting to equipment.

A device's management credential is either stored in the inventory as `password`, or named by `password_env`, which points at a variable in **the daemon process environment** rather than the shell running an API client. A stored password wins when both are present. The page's add/edit dialog writes the stored form, which is what makes an access point usable without changing the daemon's environment; `password_env` remains for deployments that keep credentials outside the configuration file.

Every read surface replaces a stored password with `********` (`network.RedactedSecret`), and sending that mask back means "unchanged", so a form nobody retyped cannot overwrite the real credential with asterisks. The value never appears in responses, events or error bodies. Like the DNS credentials already in the same document, a stored password travels with a configuration backup: treat a backup file as credential-bearing, and re-enter the password after restoring onto a different host if that is the policy.

HTTP origins can be LAN addresses. HTTPS validates the host's trust chain; the API has no insecure-TLS bypass. Redirects are refused to avoid sending login material elsewhere. HTTP is supported for equipment whose management UI only offers HTTP; choose a trusted management network.

Example inventory:

```json
{
  "network": {
    "devices": [
      {"id":"core-switch","name":"Core switch","adapter":"mercury","address":"https://mercury.example.net","username":"admin","password_env":"OLR_MERCURY_PASSWORD","roles":["switch"]},
      {"id":"living-room","name":"Living room AP","adapter":"openwrt","address":"https://ap.example.net","username":"root","password_env":"OLR_AP_PASSWORD","roles":["ap"]}
    ]
  }
}
```

## HTTP and CLI

Routes below are relative to `/api/network`:

| Method/path | Meaning |
| --- | --- |
| `GET /config` | Stored inventory, no remote reads. |
| `PUT/PATCH /config` | Replace/merge inventory only. Arrays follow normal merge-patch replacement. |
| `PUT/DELETE /devices/{id}` | Add/replace/forget one inventory entry. Does not change hardware. |
| `GET /contract` | Versioned model schemas. |
| `GET /devices/{id}/snapshot` | Fresh observations with collections/capabilities. |
| `GET /devices/{id}/capabilities` | Fresh device capability assessment (performs reads). |
| `GET /devices/{id}/fdb?mac=…&vlan=…` | Known-MAC lookup; VLAN optional. |
| `POST /devices/{id}/operations/plan` | Read-only preview; JSON `Operation`, or query `kind`, `target`, `mac` for generated MCP clients. |
| `POST /devices/{id}/operations/execute` | JSON `{operation, confirmation}` from preview. |

Invalid input returns 400, unknown device 404, unsupported ability 422, stale/used confirmation 409, upstream failure 502, timeout 504. A successful snapshot may contain failed sections; inspect `collections`, not just HTTP status. Error bodies follow the existing core error envelope and omit remote HTML, tokens, passwords and credential-bearing URLs.

```bash
olr network add device core-switch --adapter mercury --address https://mercury.example.net --username admin --password-env OLR_MERCURY_PASSWORD
olr network add device living-room --adapter openwrt --address https://ap.example.net --password-env OLR_AP_PASSWORD
olr network show config
olr network show contract
olr network show snapshot living-room
olr network show capabilities living-room
olr network show fdb core-switch --mac 02:11:22:33:44:55 --vlan 1
olr network show operation living-room --target phy0-ap0 --mac 02:aa:bb:cc:dd:ee
# After reviewing impact, use the same operation and returned confirmation:
olr network set client living-room phy0-ap0 02:aa:bb:cc:dd:ee --confirmation '<returned confirmation>'
```

New CLI responses are structured JSON, including when using the default output mode. `--dry-run` on add/remove prints inventory intent without saving; on `set client` it returns a preview without disconnecting.

Execution confirmation is random, process-local, valid for five minutes and bound to the full configured device plus exact operation. It is consumed before calling the adapter, including failed or ambiguous calls. A changed inventory entry invalidates an old plan. Do not automatically re-plan/replay after a timeout: re-read device state and obtain a new user decision. Accepted responses are not reported as verified disconnections. Confirmation does not promise transactionality or rollback on the remote hardware.

MCP projects read routes and operation preview from the same route table. Inventory writes and remote execution are intentionally not published as MCP tools, matching the repository-wide current write-publication policy.

## Verification

Run `go test ./internal/network ./internal/mcp ./internal/daemon ./cmd/olr` and `make check`. Tests use httptest for authentication, protocol shape, permission checks, partial failures, numeric precision, malformed input, response limits, redirects, context cancellation and non-replay. No test modifies real network equipment.
