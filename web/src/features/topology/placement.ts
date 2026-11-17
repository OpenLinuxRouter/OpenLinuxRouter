import type { Device } from '@/features/devices/types'

/** A network as Link reports it: a name, and the range it serves when it has one. */
export interface NetworkRange {
  name: string
  subnet?: string
}

/**
 * Which of this router's networks holds a device's management address.
 *
 * It is a join, not a reading: the inventory stores where the box is reachable
 * and Link stores the ranges, so placing a box costs no conversation with it.
 * That is why this can run on the first paint of a page that must not read
 * devices to draw itself.
 *
 * The claim is deliberately narrow — **the management address falls inside that
 * range** — because that is all the two facts together support. A switch
 * managed from another segment is placed where its address is, not where its
 * ports are, so the page says "by its management address" rather than
 * "attached to". A box behind NAT, or one reached by name, has no placement
 * here and the page has to say so instead of guessing.
 *
 * The longest matching range wins, so a network of /24 inside a /16 is not
 * hidden by the wider one. IPv4 only: `link.networks` carries `subnet` for
 * IPv4 and a delegated `subnet6`, and no device in this inventory is reached
 * over IPv6 today.
 */
export function placeDevice(device: Device, networks: readonly NetworkRange[]): string | undefined {
  const host = hostOf(device.address)
  const ip = host === undefined ? undefined : parseIPv4(host)
  if (ip === undefined) return undefined
  let placed: { name: string; bits: number } | undefined
  for (const network of networks) {
    const range = network.subnet === undefined ? undefined : parseRange(network.subnet)
    if (range === undefined) continue
    if ((ip & range.mask) >>> 0 !== (range.base & range.mask) >>> 0) continue
    if (placed === undefined || range.bits > placed.bits) placed = { name: network.name, bits: range.bits }
  }
  return placed?.name
}

/** The host a management address points at, or undefined when it is not a URL. */
export function hostOf(address: string): string | undefined {
  try {
    const host = new URL(address).hostname
    // A URL keeps IPv6 literals in brackets; nothing below understands them,
    // and returning them unbracketed would just fail later.
    return host.startsWith('[') && host.endsWith(']') ? host.slice(1, -1) : host
  } catch {
    return undefined
  }
}

/** Dotted quad to its 32-bit value; undefined for anything that is not one. */
export function parseIPv4(value: string): number | undefined {
  const parts = value.split('.')
  if (parts.length !== 4) return undefined
  let out = 0
  for (const part of parts) {
    // Leading zeros are ambiguous (octal to some parsers), so they are refused
    // rather than read one way and shown another.
    if (!/^(0|[1-9]\d{0,2})$/.test(part)) return undefined
    const octet = Number(part)
    if (octet > 255) return undefined
    out = (out << 8) | octet
  }
  return out >>> 0
}

interface Range {
  base: number
  mask: number
  bits: number
}

function parseRange(cidr: string): Range | undefined {
  const [address, suffix, ...rest] = cidr.trim().split('/')
  if (address === undefined || suffix === undefined || rest.length > 0) return undefined
  const base = parseIPv4(address)
  if (base === undefined || !/^(0|[1-9]\d?)$/.test(suffix)) return undefined
  const bits = Number(suffix)
  if (bits > 32) return undefined
  return { base, bits, mask: maskOf(bits) }
}

function maskOf(bits: number): number {
  if (bits <= 0) return 0
  if (bits >= 32) return 0xffffffff
  return (0xffffffff << (32 - bits)) >>> 0
}
