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
  if (adapter === 'mercury') return 'Mercury'
  if (adapter === 'huawei') return 'Huawei ONT'
  return 'OpenWrt'
}

export function roleLabel(role: NonNullable<Device['roles']>[number]): string {
  if (role === 'ont') return 'ONT'
  if (role === 'switch') return 'Switch'
  if (role === 'ap') return 'Access point'
  return 'Router'
}

/** The roles written on the inventory entry, in the order people name them. */
export function roleLabels(device: Device): string[] {
  const order = ['ont', 'router', 'switch', 'ap'] as const
  return order.filter((role) => device.roles?.includes(role)).map(roleLabel)
}

/**
 * The management address as a list reads it: the scheme and a trailing slash
 * are how it is stored, not part of the box's address.
 */
export function addressLabel(address: string): string {
  const trimmed = address.trim()
  const bare = trimmed.replace(/^[a-z][a-z0-9+.-]*:\/\//i, '').replace(/\/+$/, '')
  return bare || trimmed
}

/**
 * An address reduced to the host two addresses can be compared on: no scheme,
 * path, port, prefix mask or IPv6 brackets. Used only to answer "is this the
 * local box", never for display.
 */
export function hostKey(address: string): string {
  const trimmed = address.trim()
  if (!trimmed) return ''
  const withoutScheme = trimmed.replace(/^[a-z][a-z0-9+.-]*:\/\//i, '')
  const authority = withoutScheme.split('/')[0].split('@').pop() ?? ''
  const bracketed = authority.match(/^\[([^\]]+)\]/)
  if (bracketed) return bracketed[1].toLowerCase()
  // A bare IPv6 address has more than one colon; splitting it on ":" would
  // reduce it to its first group. One colon is a port.
  if ((authority.match(/:/g)?.length ?? 0) > 1) return authority.toLowerCase()
  return (authority.split(':')[0] ?? '').toLowerCase()
}

/**
 * Whether this entry is the box serving the console: it routes, and its
 * management address is one of this machine's own. Both halves matter — a
 * router role is something an operator can put on any upstream box.
 */
export function isThisRouter(device: Device, localHosts: Iterable<string>): boolean {
  if (!device.roles?.includes('router')) return false
  const host = hostKey(device.address)
  if (!host) return false
  for (const local of localHosts) {
    if (hostKey(local) === host) return true
  }
  return false
}

/** The pictures an inventory entry can wear; all four exist in the catalogue. */
export type PortraitCategory = 'switch' | 'accesspoint' | 'modem' | 'router'

/**
 * The picture an inventory entry wears.
 *
 * Roles decide it, because a box can be several things at once and a picture
 * can only show one: a switch that also routes is still a switch, an access
 * point that routes is still an access point, and an ONT that routes is still
 * the box the fiber terminates in. The adapter answers for an entry whose
 * roles nobody filled in — it is the one thing the inventory always knows.
 */
export function portraitCategory(device: Device): PortraitCategory {
  const roles = device.roles ?? []
  if (roles.includes('switch')) return 'switch'
  if (roles.includes('ap')) return 'accesspoint'
  if (roles.includes('ont')) return 'modem'
  if (roles.includes('router')) return 'router'
  if (device.adapter === 'mercury') return 'switch'
  if (device.adapter === 'huawei') return 'modem'
  return 'router'
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
  // One link bit is all the snapshot carries, so a switch port cannot tell an
  // empty socket from a cable whose far end is dead — and must not claim it can.
  // `No cable` belongs to a local interface, where the kernel knows the carrier.
  if (port.link_up === false) return 'Down'
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
