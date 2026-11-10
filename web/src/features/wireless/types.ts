/**
 * The subset of internal/network's snapshot the wireless page reads.
 *
 * The daemon's snapshot is a shared contract, so every optional field here is
 * genuinely optional: an adapter or firmware that cannot observe a metric must
 * leave it absent rather than report a zero. The page renders absence as "—".
 */
export interface NetworkDevice {
  id: string
  name?: string
  adapter: 'mercury' | 'openwrt'
  address: string
  username?: string
  /** Redacted on read; the mask sent back means "unchanged". */
  password?: string
  password_env?: string
  roles?: ('router' | 'switch' | 'ap')[]
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
  firmware?: string
  hostname?: string
  uptime_seconds?: number
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
