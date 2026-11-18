import type { FarEnd } from './equipment.ts'

/**
 * Which cable goes where, from the three things this router can learn.
 *
 * A switch's forwarding table says which port an address arrived on, an access
 * point's association list says who is on its radio, and the default route says
 * which box is upstream. None of them says "this is plugged into that", so the
 * cables are an inference, and the rules are the ones a person would apply
 * reading the same tables:
 *
 * - **A port that sees this router is the cable to this router.** Everything
 *   else learned there is upstream of the switch or behind the router, never on
 *   that port, so it is dropped rather than drawn as a client of the switch.
 * - **A port that sees a box we manage is the cable to that box.** Clients
 *   learned on the same port are behind the box, so they hang off the box — the
 *   switch only knows they are somewhere down that wire.
 * - **Nearest wins.** An address an access point names as associated is on its
 *   radio. Otherwise an address seen by two switches belongs to the port that
 *   learned the fewest addresses: the port behind a cascade sees everything
 *   downstream, the port the client is plugged into sees only the client.
 * - **The modem is above.** The box the default route leads to is drawn above
 *   this router, joined to the interface the route leaves by.
 * - **A box nothing places is still shown as unplaced.** It gets a cable to this
 *   router marked as unknown, so it is never drawn as if it hung in the air.
 */
export type WireKind = 'wired' | 'wireless' | 'unknown'

export interface Wire {
  from: string
  to: string
  /** The port on `from`, when that end named one. */
  fromPort?: string
  /** The port on `to`, when that end named one. */
  toPort?: string
  kind: WireKind
}

export interface WiringInput {
  /** Boxes this console manages, by id. `self` is this router and is implied. */
  devices: readonly string[]
  /** Each switch's forwarding table, as far as it was swept: port id to far ends. */
  sweeps: ReadonlyMap<string, ReadonlyMap<string, readonly FarEnd[]>>
  /** Each access point's associated clients, as lowercase MACs. */
  associations: ReadonlyMap<string, readonly string[]>
  /** The box the default route leads to, and this router's interface towards it. */
  uplink?: { deviceId: string; port?: string }
}

export interface Wiring {
  wires: Wire[]
  /** Client nodes, keyed by node id (`mac:<mac>`), with what named them. */
  clients: Map<string, FarEnd>
  /** Nodes drawn above this router. */
  above: string[]
}

export const SELF = 'self'
export const clientNode = (mac: string) => `mac:${mac.toLowerCase()}`

interface Owner {
  parent: string
  port?: string
  kind: 'wired' | 'wireless'
  /** Lower is nearer: -1 for a radio, else the size of the port that learned it. */
  score: number
  end: FarEnd
}

export function wire({ devices, sweeps, associations, uplink }: WiringInput): Wiring {
  const known = new Set(devices)
  const boxWires = new Map<string, Wire>()
  const addBox = (w: Wire) => {
    // Two cables between the same pair of boxes are two cables — this router
    // has two NICs on the switch — so ports are part of what makes one unique.
    const ends = [`${w.from}:${w.fromPort ?? ''}`, `${w.to}:${w.toPort ?? ''}`].sort()
    const pair = [w.from, w.to].sort().join('|')
    const key = w.fromPort === undefined && w.toPort === undefined ? pair : ends.join('|')
    if (w.fromPort === undefined && w.toPort === undefined && [...boxWires.values()].some(other => [other.from, other.to].sort().join('|') === pair)) return
    if (!boxWires.has(key)) boxWires.set(key, w)
  }
  const owners = new Map<string, Owner>()
  const claim = (mac: string, owner: Owner) => {
    const key = mac.toLowerCase()
    const current = owners.get(key)
    if (current === undefined || owner.score < current.score) owners.set(key, owner)
  }

  for (const [ap, macs] of associations) {
    if (!known.has(ap)) continue
    for (const mac of macs) {
      claim(mac, { parent: ap, kind: 'wireless', score: -1, end: { mac: mac.toLowerCase(), kind: 'client', name: mac } })
    }
  }

  for (const [sw, byPort] of sweeps) {
    if (!known.has(sw)) continue
    for (const [port, ends] of byPort) {
      const routers = ends.filter(end => end.kind === 'router')
      if (routers.length > 0) {
        for (const end of routers) addBox({ from: SELF, fromPort: end.name, to: sw, toPort: port, kind: 'wired' })
        continue
      }
      const boxes = [...new Set(ends
        .filter(end => end.kind === 'device' && end.deviceId !== undefined && end.deviceId !== sw && known.has(end.deviceId))
        .map(end => end.deviceId as string))]
      for (const box of boxes) addBox({ from: sw, fromPort: port, to: box, kind: 'wired' })
      const clients = ends.filter(end => end.kind === 'client')
      for (const end of clients) {
        claim(end.mac, boxes.length === 1
          ? { parent: boxes[0], kind: 'wired', score: ends.length, end }
          : { parent: sw, port, kind: 'wired', score: ends.length, end })
      }
    }
  }

  const above: string[] = []
  if (uplink !== undefined && known.has(uplink.deviceId)) {
    const placed = [...boxWires.values()].some(w => w.from === uplink.deviceId || w.to === uplink.deviceId)
    if (!placed) {
      addBox({ from: uplink.deviceId, to: SELF, toPort: uplink.port, kind: 'wired' })
      above.push(uplink.deviceId)
    }
  }

  // Anything not reachable from this router through the cables above is shown
  // as unplaced, joined to the router by a cable that says so.
  const reach = new Set([SELF])
  const queue = [SELF]
  while (queue.length > 0) {
    const at = queue.shift() as string
    for (const w of boxWires.values()) {
      const next = w.from === at ? w.to : w.to === at ? w.from : undefined
      if (next !== undefined && !reach.has(next)) {
        reach.add(next)
        queue.push(next)
      }
    }
  }
  for (const id of devices) {
    if (!reach.has(id)) addBox({ from: SELF, to: id, kind: 'unknown' })
  }

  const wires = [...boxWires.values()]
  const clients = new Map<string, FarEnd>()
  for (const [mac, owner] of owners) {
    const id = clientNode(mac)
    clients.set(id, owner.end)
    wires.push({ from: owner.parent, fromPort: owner.port, to: id, kind: owner.kind })
  }
  return { wires, clients, above }
}
