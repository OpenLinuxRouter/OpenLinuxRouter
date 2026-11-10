# Wireless page

Status: first increment implemented. Read-only for one managed access point.

## What the page is

The top-level Wireless section shows one access point from the network equipment
inventory: its radios, the findings that follow from the current reading, and
the clients associated right now. It is a monitoring surface, not a
configuration editor. A channel change interrupts every client, so it belongs
behind the same preview-and-recovery contract as the rest of the router rather
than beside the charts.

## Where the data comes from

The browser reads the equipment inventory and then one device snapshot:

```
GET /api/network/config
GET /api/network/devices/{id}/snapshot
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

## What it shows

| Area | Content |
| --- | --- |
| Radios | SSID, BSSID, band, advertised standard, runtime channel, actual and configured width, transmit power, country, noise, clients, utilization |
| Findings | configured-versus-actual width, clients at or below -75 dBm, utilization at or above 60% |
| Clients | signal and SNR, directional rates, MCS and width, expected throughput, retries and failures, idle and connected time, bytes and packets, airtime, MFP and WMM |

Client names come from the devices module when that MAC is known. Otherwise the
page says “Private address” for a locally administered address, or shows the
MAC. A dash means the access point did not report the value.

## Deliberate limits

- **No channel scan.** Neighbours and interference need `iwinfo scan`/`survey`
  or a scan with an explicit freshness cache. The tested firmware does not
  grant those methods, and scanning pulls the radio off-channel. This is the
  next increment, not a hidden setting.
- **No hostapd-only counters** when the firmware does not expose them.
  Utilization, MFP/WMM and airtime disappear rather than being substituted.
- **No writes.** Disconnect, steering and channel changes remain operations with
  their own capability checks and confirmations. The snapshot can report that
  `client.disconnect` is supported; the page does not yet offer it.
- **No history.** A reading is a point in time. Trend lines need a sampler and a
  retention decision; none is implied by this page.

## Verification

The snapshot shape was exercised against a Xiaomi BE7000 running Beam WRT 1.5.6
(OpenWrt 25.12.6): two radios, three associations, the 2.4 GHz width fallback,
and the LuCI/iwinfo fallback above. Adapter fixtures remain synthetic and cover
the same protocol shapes.
