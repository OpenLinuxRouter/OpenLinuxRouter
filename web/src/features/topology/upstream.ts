import { hostOf, parseIPv4 } from './placement.ts'

/**
 * Which managed box is upstream of this router, and so belongs above it.
 *
 * The map runs top to bottom in the direction traffic flows from the internet:
 * this router at the top, unless something we manage sits between it and the
 * internet. Two situations put a box there:
 *
 * - **This router's way out is a private address.** Its default route leaves
 *   with an RFC 1918 address, so another box is doing NAT in front of it — a
 *   modem in router mode. The managed box at the route's next hop is that box;
 *   failing that, a managed box inside the same subnet as the way out.
 * - **This router dials, and the modem bridges.** Its way out is public (PPPoE
 *   on a bridged ONT), so no next hop names the modem; but a box the operator
 *   marked as an ONT that is not on any of this router's own networks can only
 *   be on the far side of the uplink.
 *
 * Carrier-grade NAT (100.64.0.0/10) is not a box in the house: the NAT is the
 * ISP's, and nothing we manage stands there.
 */
export interface UpstreamInput {
  devices: readonly { id: string; address: string; adapter?: string; roles?: readonly string[] }[]
  /** The interface the default route leaves by, and its addresses as prefixes. */
  route?: { dev: string; via?: string; addresses?: readonly string[] }
  /** The interface the uplink is configured on, when olr manages it. */
  uplinkInterface?: string
  /** This router's own networks, as CIDRs, to tell a LAN box from a WAN one. */
  lan: readonly string[]
  /** This router's physical ports, so the cable lands on one when it can. */
  ports: readonly string[]
}

export function findUpstream({ devices, route, uplinkInterface, lan, ports }: UpstreamInput): { deviceId: string; port?: string } | undefined {
  if (route === undefined) return undefined
  const port = [route.dev, uplinkInterface].find(name => name !== undefined && ports.includes(name))
  const hostIP = (address: string) => {
    const host = hostOf(address.trim())
    return host === undefined ? undefined : parseIPv4(host)
  }
  const wan = (route.addresses ?? []).map(parseCIDR).filter(range => range !== undefined)

  if (wan.some(range => isPrivate(range.base))) {
    const via = route.via === undefined ? undefined : parseIPv4(route.via)
    const atHop = via === undefined ? undefined : devices.find(device => hostIP(device.address) === via)
    const inSubnet = devices.find(device => {
      const ip = hostIP(device.address)
      return ip !== undefined && wan.some(range => contains(range, ip))
    })
    const found = atHop ?? inSubnet
    return found === undefined ? undefined : { deviceId: found.id, port }
  }

  const lanRanges = lan.map(parseCIDR).filter(range => range !== undefined)
  const bridged = devices.find(device => {
    const ont = device.roles?.includes('ont') === true || (device.roles === undefined || device.roles.length === 0) && device.adapter === 'huawei'
    if (!ont) return false
    const ip = hostIP(device.address)
    return ip === undefined || !lanRanges.some(range => contains(range, ip))
  })
  return bridged === undefined ? undefined : { deviceId: bridged.id, port }
}

interface Range { base: number; mask: number }

function parseCIDR(cidr: string): Range | undefined {
  const [address, suffix] = cidr.trim().split('/')
  const base = address === undefined ? undefined : parseIPv4(address)
  const bits = suffix === undefined ? 32 : Number(suffix)
  if (base === undefined || !Number.isInteger(bits) || bits < 0 || bits > 32) return undefined
  const mask = bits === 0 ? 0 : (0xffffffff << (32 - bits)) >>> 0
  return { base, mask }
}

function contains(range: Range, ip: number): boolean {
  return ((ip & range.mask) >>> 0) === ((range.base & range.mask) >>> 0)
}

/** RFC 1918. */
export function isPrivate(ip: number): boolean {
  return (ip >>> 24) === 10 ||
    (ip >>> 20) === ((172 << 4) | 1) ||
    (ip >>> 16) === ((192 << 8) | 168)
}
