# Wireless page

Status: implemented. Read-only for one managed access point, plus an explicit
per-band neighbour scan.

## What the page is

The top-level Wireless section shows one access point from the network equipment
inventory: its radios, the findings that follow from the current reading, and
the clients associated right now. It is a monitoring surface, not a
configuration editor. A channel change interrupts every client, so it belongs
behind the same preview-and-recovery contract as the rest of the router rather
than beside the charts.

## Adding the access point

The page owns its inventory entry. Its empty state and header offer an add/edit
dialog for the management origin, username and password; removing an entry only
forgets it here and never touches the access point. A stored password is
returned masked and is never rendered again, which is why editing an address
does not require retyping it. `docs/network-equipment.md` describes the
credential rules and what a backup carries.

## Where the data comes from

The browser reads the equipment inventory and then one device snapshot:

```
GET    /api/network/config
PUT    /api/network/devices/{id}        inventory entry only, never the device
DELETE /api/network/devices/{id}
GET    /api/network/devices/{id}/snapshot
POST   /api/network/devices/{id}/scan   {"radio_id": "radio1"}
```

The daemon talks to the access point. The browser never reaches it directly and
holds no access-point credential. `internal/network` owns the adapter, the
snapshot contract and the per-section collection status; the page treats every
reading as dated evidence, never as current truth.

On the tested Beam WRT build, netifd's `network.wireless` object and hostapd's
`get_status`/`get_clients` are not granted to the management session. The
adapter therefore falls back to LuCI's `luci-rpc.getWirelessDevices` and
`iwinfo.assoclist`, which the same session may read. Utilization, MFP/WMM and
airtime stay absent on that firmware and the page renders them as “—”, not as
zero.

`radio_id` is the ID the snapshot reports — on this firmware `radio0` and
`radio1`. The adapter resolves it to that radio's AP interface (`phy0-ap0`,
`phy1-ap0`) before asking iwinfo, and also accepts the interface name itself.

The scan is a separate request on purpose. `iwinfo.scan` is granted on that
firmware and takes roughly four seconds per radio, while the radio is off its
own channel and every client on that band waits. `iwinfo.survey`, which would
report per-channel busy time directly, is denied. So the snapshot never scans,
the page never polls a scan, and a person asks for one band at a time. The
request is not published as an MCP tool for the same reason: an agent must not
stall the Wi-Fi to fill in a diagram.

## What it shows

| Area | Content |
| --- | --- |
| Radios | SSID, BSSID, band, advertised standard, runtime channel, actual and configured width, transmit power, country, noise, clients, utilization |
| Findings | configured-versus-actual width, clients at or below -75 dBm, utilization at or above 60% |
| Clients | signal and SNR, directional rates, MCS and width, expected throughput, retries and failures, idle and connected time, bytes and packets, airtime, MFP and WMM |
| Channels | on request, one band's scan: every BSS heard with its primary channel, width, signal and security, a strip counting BSS per channel, and how many overlap the radio's own channel |

Client names come from the devices module when that MAC is known. Otherwise the
page says “Private address” for a locally administered address, or shows the
MAC. A dash means the access point did not report the value.

## Deliberate limits

- **A scan is an action, not a reading.** It interrupts the band for a few
  seconds, so the page asks first, offers it per band, and keeps the answer as a
  dated sample. Absence of a scan is not absence of neighbours.
- **Occupancy is counted, not measured.** Without `iwinfo survey` the strip
  counts BSS per primary channel; overlap is inferred from channel, width and
  signal, not from measured airtime. A strong neighbour on an overlapping
  channel is evidence of competition, not proof of the cause of a slow client.
- **No hostapd-only counters** when the firmware does not expose them.
  Utilization, MFP/WMM and airtime disappear rather than being substituted.
- **No writes to the access point.** Editing the inventory entry is a
  configuration change on this router, not a change on the device. Disconnect,
  steering and channel changes remain operations with their own capability
  checks and confirmations. The snapshot can report that
  `client.disconnect` is supported; the page does not yet offer it.
- **No history.** A reading is a point in time. Trend lines need a sampler and a
  retention decision; none is implied by this page.

## Verification

The snapshot shape was exercised against a Xiaomi BE7000 running Beam WRT 1.5.6
(OpenWrt 25.12.6): two radios, three associations, the 2.4 GHz width fallback,
and the LuCI/iwinfo fallback above. A live scan of the 5 GHz radio heard 25
BSS in about four seconds and a 2.4 GHz scan heard 100 in about four more;
`survey` answered “Access denied”. Adapter fixtures remain synthetic and cover
the same protocol shapes.
