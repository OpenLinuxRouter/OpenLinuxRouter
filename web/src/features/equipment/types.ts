/**
 * The network equipment inventory, and the subset of internal/network's
 * snapshot this app reads.
 *
 * An inventory entry is not a "device" in the MAC-keyed client list: it is a box
 * olr reaches over its management interface. `roles` is a set rather than a
 * type, because one box can be a switch and an access point at once, and pages
 * grow their tabs from what a snapshot reports instead of from a fixed model.
 *
 * Every optional field is genuinely optional. An adapter or firmware that cannot
 * observe a metric leaves it absent rather than reporting a zero, and the page
 * renders absence as "—". The snapshot is one dated answer per collection, not
 * an atomic reading of the hardware.
 */

export type Adapter = 'mercury' | 'openwrt'
export type DeviceRole = 'router' | 'switch' | 'ap'

export interface NetworkDevice {
  id: string
  name?: string
  adapter: Adapter
  address: string
  username?: string
  /** Redacted on read; the mask sent back means "unchanged". */
  password?: string
  password_env?: string
  roles?: DeviceRole[]
}

export interface NetworkConfig {
  devices?: NetworkDevice[]
}

export interface Collection {
  state: 'ok' | 'unsupported' | 'error'
  observed_at: string
  error?: string
}

export interface Capability {
  state: 'supported' | 'unsupported' | 'unknown'
  reason?: string
}

export interface Identity {
  manufacturer?: string
  model?: string
  serial?: string
  firmware?: string
  hostname?: string
  macs?: string[]
  uptime_seconds?: number
}

export interface PoE {
  enabled?: boolean
  delivering?: boolean
  power_watts?: number
}

/**
 * One port. Counters are whatever the device reports: Mercury firmware has
 * packet totals with errors separate and no byte totals, so a page must render
 * what is present and never fill a missing counter with zero.
 */
export interface Port {
  id: string
  name?: string
  kind: string
  mac?: string
  parent_id?: string
  members?: string[]
  addresses?: string[]
  enabled?: boolean
  link_up?: boolean
  speed_mbps?: number
  duplex?: string
  rx_bytes?: number
  tx_bytes?: number
  rx_packets?: number
  tx_packets?: number
  rx_errors?: number
  tx_errors?: number
  rx_mbps?: number
  tx_mbps?: number
  pvid?: number
  poe?: PoE
}

/** Device-local VLAN. Equal VIDs on two devices do not prove connectivity. */
export interface VLAN {
  id: number
  name?: string
  tagged_ports: string[]
  untagged_ports: string[]
}

export interface FDBEntry {
  mac: string
  vlan?: number
  port_id: string
  type?: string
}

/** A lookup is evidence of reachability, not proof of a direct cable. */
export interface FDBQuery {
  mac: string
  vlan?: number
}

export interface FDBResult {
  device_id: string
  observed_at: string
  entries: FDBEntry[]
}

export interface Radio {
  id: string
  enabled?: boolean
  band?: string
  channel?: number
  width_mhz?: number
  configured_width_mhz?: number
  protocols?: string[]
  noise_dbm?: number
  txpower_dbm?: number
  country?: string
  utilization_percent?: number
}

export interface WirelessNetwork {
  id: string
  radio_id: string
  interface_id: string
  ssid?: string
  bssid?: string
  mode?: string
  encryption?: string
  network_names?: string[]
}

export interface Association {
  mac: string
  network_id: string
  radio_id?: string
  signal_dbm?: number
  signal_avg_dbm?: number
  noise_dbm?: number
  connected_seconds?: number
  inactive_ms?: number
  rx_rate_mbps?: number
  tx_rate_mbps?: number
  rx_mcs?: number
  tx_mcs?: number
  rx_width_mhz?: number
  tx_width_mhz?: number
  expected_mbps?: number
  retries?: number
  failed?: number
  rx_bytes?: number
  tx_bytes?: number
  rx_packets?: number
  tx_packets?: number
  tx_airtime_us?: number
  mfp?: boolean
  wmm?: boolean
}

export interface Snapshot {
  device_id: string
  observed_at: string
  identity: Identity
  capabilities: Record<string, Capability>
  collections: Record<string, Collection>
  ports?: Port[]
  vlan_enabled?: boolean
  vlans?: VLAN[]
  fdb?: FDBEntry[]
  radios?: Radio[]
  wireless?: WirelessNetwork[]
  associations?: Association[]
}

/**
 * One BSS another radio heard during an explicit scan. `open` is absent when
 * the beacon carried no usable privacy information, which is not the same as
 * an open network; `encryption` names the protocol family, never a key.
 */
export interface Neighbour {
  bssid?: string
  ssid?: string
  channel?: number
  width_mhz?: number
  signal_dbm?: number
  encryption?: string
  open?: boolean
  transport?: string
}

/** A scan is a dated sample: the radio left its channel while it ran. */
export interface ScanResult {
  device_id: string
  radio_id: string
  observed_at: string
  neighbours: Neighbour[]
}
