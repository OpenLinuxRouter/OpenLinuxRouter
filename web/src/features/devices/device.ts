import type { StateWord } from '@/lib/state'

import type { Capability, Collection, Device, Port } from './types'

/**
 * The small decisions this feature makes in more than one place.
 *
 * Kept free of React and of runtime imports so the rules can be tested on their
 * own — the same reason `wireless.ts` exists. Words come back as the keys of
 * `lib/state.ts`'s table rather than as tones, so a page can name a status with
 * `state(word)` and can never invent a colour of its own.
 */

export function adapterLabel(adapter: Device['adapter']): string {
  return adapter === 'mercury' ? 'Mercury' : 'OpenWrt'
}

export function roleLabel(role: NonNullable<Device['roles']>[number]): string {
  if (role === 'switch') return 'Switch'
  if (role === 'ap') return 'Access point'
  return 'Router'
}

/** The roles written on the inventory entry, in the order people name them. */
export function roleLabels(device: Device): string[] {
  const order = ['router', 'switch', 'ap'] as const
  return order.filter((role) => device.roles?.includes(role)).map(roleLabel)
}

/**
 * Ports in the order the hardware labels them. Mercury reports decimal IDs and
 * netifd reports interface names; neither is alphabetical, and a switch's port
 * 10 must not sort between 1 and 2.
 */
export function sortPorts(ports: Port[]): Port[] {
  return [...ports].sort((a, b) => {
    const numericA = /^\d+$/.test(a.id)
    const numericB = /^\d+$/.test(b.id)
    if (numericA && numericB) return Number(a.id) - Number(b.id)
    if (numericA !== numericB) return numericA ? -1 : 1
    return (a.name ?? a.id).localeCompare(b.name ?? b.id, undefined, { numeric: true })
  })
}

export function portLabel(port: Port): string {
  return port.name || port.id
}

/** The name a VLAN's port list refers to, falling back to the ID it printed. */
export function portName(ports: Port[] | undefined, id: string): string {
  const port = ports?.find((candidate) => candidate.id === id)
  return port ? portLabel(port) : id
}

/**
 * A port's link state. Admin-disabled is not the same as nothing plugged in, and
 * an adapter that reports neither is unknown rather than down.
 */
export function portState(port: Port): StateWord {
  if (port.enabled === false) return 'Disabled'
  if (port.link_up === true) return 'Connected'
  if (port.link_up === false) return 'No cable'
  return 'Unknown'
}

/** Canonical port kinds from the contract; anything else is shown as it came. */
export function portKindLabel(kind: string): string {
  switch (kind) {
    case 'ethernet': return 'Ethernet'
    case 'fiber': return 'Fiber'
    case 'bridge': return 'Bridge'
    case 'vlan': return 'VLAN interface'
    case 'lag': return 'Link aggregation'
    case 'wireless': return 'Wireless'
    case 'other': return 'Other'
    default: return kind
  }
}

/**
 * A collection's state as a word. `unsupported` is the adapter saying it has no
 * API for this, which is not a fault and not an empty answer, so it must not
 * read as either.
 */
export function collectionWord(state: Collection['state']): StateWord {
  if (state === 'ok') return 'OK'
  if (state === 'error') return 'Not answering'
  return 'Unavailable'
}

export function capabilityWord(state: Capability['state']): StateWord {
  if (state === 'supported') return 'OK'
  if (state === 'unsupported') return 'Not in use'
  return 'Unknown'
}

export function formatSpeed(mbps: number): string {
  if (mbps >= 1000) {
    const gbps = mbps / 1000
    return `${Number.isInteger(gbps) ? gbps : gbps.toFixed(1)} Gbps`
  }
  return `${mbps} Mbps`
}

export function duplexLabel(duplex?: string): string {
  if (duplex === 'full') return 'Full duplex'
  if (duplex === 'half') return 'Half duplex'
  return ''
}

/** Counters, compactly. These are packets or errors, never rates. */
export function formatCount(n: number): string {
  if (n < 1000) return String(n)
  const units = ['k', 'M', 'G', 'T']
  let value = n / 1000
  let unit = 0
  while (value >= 1000 && unit < units.length - 1) {
    value /= 1000
    unit += 1
  }
  return `${value.toFixed(1)}${units[unit]}`
}

export function formatUptime(seconds: number): string {
  const days = Math.floor(seconds / 86_400)
  const hours = Math.floor((seconds % 86_400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  return `${minutes}m`
}

/** Unicast MAC in the six-octet form every switch's search field expects. */
export function macValid(mac: string): boolean {
  return /^[0-9a-f]{2}(:[0-9a-f]{2}){5}$/i.test(mac.trim())
}

/** VLAN IDs are 1–4094; 0 and 4095 are reserved. */
export function vlanValid(vlan: number): boolean {
  return Number.isInteger(vlan) && vlan >= 1 && vlan <= 4094
}

/**
 * A stable identifier from the management address, so the first thing a person
 * types is enough to save with. Never invents one: an address that parses to
 * nothing leaves the field for the operator.
 */
export function idFromAddress(address: string): string {
  try {
    const trimmed = address.trim()
    const host = new URL(/^[a-z]+:\/\//i.test(trimmed) ? trimmed : `http://${trimmed}`).hostname
    return host.replace(/[^a-zA-Z0-9_-]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 64)
  } catch {
    return ''
  }
}
