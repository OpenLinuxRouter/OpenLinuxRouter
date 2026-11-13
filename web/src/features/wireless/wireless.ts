import type { Association, Device, Neighbour, Radio, Snapshot, WirelessNetwork } from '@/features/devices/types'

/**
 * The two choices this page keeps in the URL: which access point, and which
 * view. Both the reads and the writes name them here, because a page that
 * writes one name and reads another silently ignores every selection.
 */
export const wirelessQuery = { device: 'device', tab: 'tab' } as const

/**
 * The access point to show: the one a link names, else the likeliest one.
 *
 * A name that no longer exists never leaves the page empty — it falls back to
 * the first device that calls itself an access point, so an old bookmark still
 * shows something true. Switches and modems are skipped for the same reason:
 * they have no radios to report.
 */
export function deviceForRoute(devices: Device[], requested: string | null | undefined): Device | undefined {
  return devices.find(device => device.id === requested)
    ?? devices.find(device => device.roles?.includes('ap'))
    ?? devices.find(device => device.adapter === 'openwrt')
    ?? devices[0]
}

/** A finding is a sentence plus the values that fill it, so it can be translated. */
export interface Finding {
  id: string
  tone: 'warning' | 'notice'
  title: string
  detail: string
  values: Record<string, string | number>
}

const protocolRank: Record<string, number> = { be: 7, ax: 6, ac: 5, n: 4, g: 3, b: 2, a: 2 }
const generationNames: Record<string, string> = { be: 'Wi-Fi 7', ax: 'Wi-Fi 6', ac: 'Wi-Fi 5', n: 'Wi-Fi 4' }

export function bandLabel(band?: string): string {
  if (band === '2g') return '2.4 GHz'
  if (band === '5g') return '5 GHz'
  if (band === '6g') return '6 GHz'
  return band || 'Unknown band'
}

/** One PHY standard, named the way people buy it. */
export function generationLabel(protocol?: string): string {
  return protocol ? generationNames[protocol] ?? '' : ''
}

function newestProtocol(protocols?: string[]): string {
  let best = ''
  for (const protocol of protocols ?? []) {
    if ((protocolRank[protocol] ?? 0) > (protocolRank[best] ?? 0)) best = protocol
  }
  return best
}

/** The newest standard the driver advertises, named the way people buy it. */
export function standardLabel(protocols?: string[]): string {
  return generationLabel(newestProtocol(protocols))
}

/**
 * The standard a radio is running, and the newer one it could run.
 *
 * The badge names what the radio is doing now, because "am I on Wi-Fi 7" is
 * the question the card answers. `newer` only appears when the driver
 * advertises something the configured mode does not use; a radio left at
 * HE80 on Wi-Fi 7 hardware is the case that hides otherwise.
 */
export function radioStandard(radio: Radio): { label: string; newer?: string } {
  const label = generationLabel(radio.protocol) || standardLabel(radio.protocols)
  const supported = newestProtocol(radio.protocols)
  const newer = radio.protocol && (protocolRank[supported] ?? 0) > (protocolRank[radio.protocol] ?? 0) ? generationLabel(supported) : ''
  return newer ? { label, newer } : { label }
}

/**
 * How a person names a network's security, from the option uci stores.
 *
 * The value is a wifi-encryption token such as `psk2` or `sae-mixed`, sometimes
 * with a cipher suffix (`psk2+ccmp`); the cipher does not change the name. An
 * unknown token is shown as it is, because a guess would be worse than jargon.
 */
export function securityLabel(encryption?: string): string {
  const [base] = (encryption ?? '').toLowerCase().split('+')
  switch (base) {
    case '':
    case 'none':
    case 'open': return 'Open'
    case 'owe': return 'Enhanced Open'
    case 'wep':
    case 'wep-open':
    case 'wep-shared': return 'WEP'
    case 'psk': return 'WPA-Personal'
    case 'psk2': return 'WPA2-Personal'
    case 'psk-mixed': return 'WPA/WPA2-Personal'
    case 'sae': return 'WPA3-Personal'
    case 'sae-mixed': return 'WPA2/WPA3-Personal'
    case 'wpa': return 'WPA-Enterprise'
    case 'wpa2':
    case '8021x': return 'WPA2-Enterprise'
    case 'wpa-mixed': return 'WPA/WPA2-Enterprise'
    case 'wpa3': return 'WPA3-Enterprise'
    default: return encryption ?? ''
  }
}

/** The direction with the newer standard; a tie goes to the downlink. */
export function linkProtocol(client: Association): string {
  const rx = protocolRank[client.rx_protocol ?? ''] ?? 0
  const tx = protocolRank[client.tx_protocol ?? ''] ?? 0
  return rx > tx ? client.rx_protocol ?? '' : client.tx_protocol ?? ''
}

/** Generation and width, without an MCS index: the row's own summary. */
export function linkLabel(client: Association): string {
  const rx = protocolRank[client.rx_protocol ?? ''] ?? 0
  const tx = protocolRank[client.tx_protocol ?? ''] ?? 0
  const newer = rx > tx
  const width = newer ? client.rx_width_mhz : client.tx_width_mhz
  return [generationLabel(newer ? client.rx_protocol : client.tx_protocol), width ? `${width} MHz` : ''].filter(Boolean).join(' · ')
}

/** One direction of a link: generation, MCS index and width. */
export function linkMode(protocol?: string, mcs?: number, width?: number): string {
  return [generationLabel(protocol), mcs != null ? `MCS ${mcs}` : '', width ? `${width} MHz` : ''].filter(Boolean).join(' · ')
}

export type SignalLevel = 'good' | 'fair' | 'weak' | 'unknown'

export function signalLevel(signal?: number): SignalLevel {
  if (signal == null) return 'unknown'
  if (signal >= -65) return 'good'
  if (signal >= -75) return 'fair'
  return 'weak'
}

/** Four filled bars at -55 dBm and above, one below -75. */
export function signalBars(signal?: number): number {
  if (signal == null) return 0
  if (signal >= -55) return 4
  if (signal >= -65) return 3
  if (signal >= -75) return 2
  return 1
}

export function snr(client: Association): number | undefined {
  if (client.signal_dbm == null || client.noise_dbm == null) return undefined
  return client.signal_dbm - client.noise_dbm
}

/** A compact, language-neutral age: 2h 32m, 33m, 45s. */
export function formatDuration(seconds?: number): string {
  if (seconds == null) return '—'
  if (seconds < 60) return `${Math.round(seconds)}s`
  const totalMinutes = Math.round(seconds / 60)
  if (totalMinutes < 60) return `${totalMinutes}m`
  const hours = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60
  return minutes ? `${hours}h ${minutes}m` : `${hours}h`
}

export function formatIdle(milliseconds?: number): string {
  if (milliseconds == null) return '—'
  if (milliseconds < 1000) return '<1s'
  if (milliseconds < 60_000) return `${(milliseconds / 1000).toFixed(milliseconds < 10_000 ? 1 : 0)}s`
  return `${Math.round(milliseconds / 60_000)}m`
}

/** A locally administered MAC has its second-least-significant bit set. */
export function isPrivateAddress(mac: string): boolean {
  const first = Number.parseInt(mac.slice(0, 2), 16)
  return Number.isFinite(first) && (first & 0x02) !== 0
}

export function radioFor(client: Association, snapshot: Snapshot): Radio | undefined {
  if (client.radio_id) return snapshot.radios?.find(radio => radio.id === client.radio_id)
  const network = snapshot.wireless?.find(item => item.id === client.network_id)
  return snapshot.radios?.find(radio => radio.id === network?.radio_id)
}

export function networkFor(client: Association, snapshot: Snapshot): WirelessNetwork | undefined {
  return snapshot.wireless?.find(network => network.id === client.network_id)
}

/** 5 GHz first, then 6 GHz, then 2.4 GHz; within a band, the strongest first. */
export function sortClients(clients: Association[], snapshot: Snapshot): Association[] {
  const rank = (band?: string) => band === '5g' ? 0 : band === '6g' ? 1 : 2
  return [...clients].sort((a, b) => {
    const ra = rank(radioFor(a, snapshot)?.band)
    const rb = rank(radioFor(b, snapshot)?.band)
    return ra - rb || (b.signal_dbm ?? -999) - (a.signal_dbm ?? -999)
  })
}

/**
 * What a reader should know before reading any table.
 *
 * Only rules that the snapshot can actually prove belong here: a configured
 * width the driver did not honour, a client at the edge, or a radio reporting
 * heavy utilization. Neighbour counts and interference need a scan, which this
 * read does not perform, so they are absent rather than guessed.
 */
export function findingsFor(snapshot: Snapshot): Finding[] {
  const findings: Finding[] = []
  for (const radio of snapshot.radios ?? []) {
    if (radio.configured_width_mhz && radio.width_mhz && radio.width_mhz < radio.configured_width_mhz) {
      findings.push({
        id: `width-${radio.id}`,
        tone: 'warning',
        title: '{band} is narrower than configured',
        detail: 'Configured {configured} MHz, running {actual} MHz. The driver may have fallen back.',
        values: { band: bandLabel(radio.band), configured: radio.configured_width_mhz, actual: radio.width_mhz },
      })
    }
    if (radio.utilization_percent != null && radio.utilization_percent >= 60) {
      findings.push({
        id: `utilization-${radio.id}`,
        tone: 'notice',
        title: '{band} channel is busy',
        detail: 'Channel utilization was {percent}% in the last hostapd window.',
        values: { band: bandLabel(radio.band), percent: Math.round(radio.utilization_percent) },
      })
    }
  }
  const weak = (snapshot.associations ?? []).filter(client => client.signal_dbm != null && client.signal_dbm <= -75)
  if (weak.length) {
    findings.push({
      id: 'weak-clients',
      tone: 'warning',
      title: 'Clients have a weak signal',
      detail: 'At or below -75 dBm: {clients}. Weak clients retry more and drop more often.',
      values: { count: weak.length, clients: weak.map(client => client.mac).join(', ') },
    })
  }
  return findings
}

/** The 20 MHz channels a band can carry, as people name them. */
export function channelsFor(band?: string): number[] {
  if (band === '2g') return [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13]
  if (band === '5g') return [36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108, 112, 116, 120, 124, 128, 132, 136, 140, 144, 149, 153, 157, 161, 165]
  return []
}

/** Centre frequency in MHz, so bandwidths can be compared as intervals. */
export function channelFrequency(channel: number, band?: string): number {
  if (band === '2g') return channel === 14 ? 2484 : 2407 + channel * 5
  if (band === '6g') return 5950 + channel * 5
  return 5000 + channel * 5
}

/** Whether two BSS occupy any of the same air, once width is taken into account. */
export function channelOverlap(
  a: { channel?: number; width_mhz?: number },
  b: { channel?: number; width_mhz?: number },
  band?: string,
): boolean {
  if (a.channel == null || b.channel == null) return false
  const distance = Math.abs(channelFrequency(a.channel, band) - channelFrequency(b.channel, band))
  return distance < ((a.width_mhz ?? 20) + (b.width_mhz ?? 20)) / 2
}

/** What one scan says about the channel this radio is on. */
export interface ScanSummary {
  heard: number
  coChannel: number
  overlapping: number
  strongest?: Neighbour
}

export function scanSummary(neighbours: Neighbour[], radio: Radio): ScanSummary {
  let coChannel = 0
  let overlapping = 0
  let strongest: Neighbour | undefined
  for (const neighbour of neighbours) {
    if (neighbour.channel != null && neighbour.channel === radio.channel) coChannel++
    if (channelOverlap(neighbour, radio, radio.band)) overlapping++
    if (neighbour.signal_dbm != null && (strongest?.signal_dbm == null || neighbour.signal_dbm > strongest.signal_dbm)) strongest = neighbour
  }
  return { heard: neighbours.length, coChannel, overlapping, strongest }
}

/** Strongest first; a scan returns rows in channel order, which hides the loudest. */
export function sortNeighbours(neighbours: Neighbour[]): Neighbour[] {
  return [...neighbours].sort((a, b) => (b.signal_dbm ?? -999) - (a.signal_dbm ?? -999) || (a.channel ?? 0) - (b.channel ?? 0))
}

/** How many BSS use each primary channel, for the occupancy strip. */
export function occupancyByChannel(neighbours: Neighbour[], band?: string): { channel: number; count: number }[] {
  const counts = new Map<number, number>()
  for (const neighbour of neighbours) {
    if (neighbour.channel == null) continue
    counts.set(neighbour.channel, (counts.get(neighbour.channel) ?? 0) + 1)
  }
  return channelsFor(band).map(channel => ({ channel, count: counts.get(channel) ?? 0 }))
}

