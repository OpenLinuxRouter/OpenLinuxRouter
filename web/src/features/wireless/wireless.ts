import type { Association, Neighbour, Radio, Snapshot, WirelessNetwork } from './types'

/** A finding is a sentence plus the values that fill it, so it can be translated. */
export interface Finding {
  id: string
  tone: 'warning' | 'notice'
  title: string
  detail: string
  values: Record<string, string | number>
}

const protocolRank: Record<string, number> = { be: 7, ax: 6, ac: 5, n: 4, g: 3, b: 2, a: 2 }

export function bandLabel(band?: string): string {
  if (band === '2g') return '2.4 GHz'
  if (band === '5g') return '5 GHz'
  if (band === '6g') return '6 GHz'
  return band || 'Unknown band'
}

/** The newest standard the driver advertises, named the way people buy it. */
export function standardLabel(protocols?: string[]): string {
  let best = 0
  for (const protocol of protocols ?? []) best = Math.max(best, protocolRank[protocol] ?? 0)
  if (best >= 7) return 'Wi-Fi 7'
  if (best >= 6) return 'Wi-Fi 6'
  if (best >= 5) return 'Wi-Fi 5'
  if (best >= 4) return 'Wi-Fi 4'
  return ''
}

/** The PHY prefix that belongs beside an MCS index. */
export function mcsPrefix(protocols?: string[]): string {
  if (protocols?.includes('be')) return 'EHT'
  if (protocols?.includes('ax')) return 'HE'
  if (protocols?.includes('ac')) return 'VHT'
  return ''
}

export function linkTech(client: Association, radio?: Radio): string {
  const parts: string[] = []
  if (client.tx_mcs != null) {
    const prefix = mcsPrefix(radio?.protocols)
    parts.push(prefix ? `${prefix}-MCS ${client.tx_mcs}` : `MCS ${client.tx_mcs}`)
  }
  if (client.tx_width_mhz) parts.push(`${client.tx_width_mhz} MHz`)
  return parts.join(' · ')
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

/** A stable, legal device ID derived from a management address. */
export function idFromAddress(address: string): string {
  try {
    const trimmed = address.trim()
    const host = new URL(/^[a-z]+:\/\//i.test(trimmed) ? trimmed : `http://${trimmed}`).hostname
    return host.replace(/[^a-zA-Z0-9_-]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 64)
  } catch {
    return ''
  }
}
