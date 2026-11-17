import type { Device, Snapshot } from '@/features/devices/types'
import { hostOf } from './placement.ts'
import type { ClientRow } from '@/lib/api-types'

/**
 * What one reading of a device says, reduced to the lines a map card shows.
 *
 * The device pages answer "what is this box doing" in detail; a card under the
 * map has room for three facts and has to pick the ones that place it in a
 * picture: how many of its ports have a link, and which clients it says are
 * attached to it.
 *
 * Every field is present only when the reading reported it. An absent port
 * table is not a box with no ports — a Mercury switch, an OpenWrt access point
 * and a fiber ONT expose different sections — and an access point that has no
 * association list has not said "nobody is connected", it has not said
 * anything.
 */
export interface EquipmentFacts {
  /** Manufacturer and model, as the box names itself. */
  model?: string
  /** Ports with a carrier, out of the ports the box listed. */
  ports?: { up: number; total: number }
  /** Clients the box reported as attached. Absent when it reports no such list. */
  wireless?: number
}

/**
 * A port a cable goes into. A bridge, a VLAN, a bond and a radio are interfaces
 * the firmware lists, and counting them as ports would put a number on a card
 * that the panel next to it does not draw.
 */
export function isPhysical(port: { kind: string }): boolean {
  return port.kind === 'ethernet' || port.kind === 'fiber'
}

/** A port that takes a module instead of a plug. */
export function isFiber(port: { kind: string }): boolean {
  return port.kind === 'fiber'
}

export function equipmentFacts(snapshot: Snapshot | undefined): EquipmentFacts | undefined {
  if (snapshot === undefined) return undefined
  const facts: EquipmentFacts = {}
  const model = [snapshot.identity?.manufacturer, snapshot.identity?.model].filter(Boolean).join(' ')
  if (model !== '') facts.model = model
  const ports = (snapshot.ports ?? []).filter(isPhysical)
  if (ports.length > 0) {
    facts.ports = { up: ports.filter(port => port.link_up === true).length, total: ports.length }
  }
  // A read that reported the collection as unsupported leaves the array absent,
  // which is not the same as an empty list and must not print "0 clients".
  if (snapshot.associations !== undefined) facts.wireless = snapshot.associations.length
  return facts
}

/** One client an access point named, with the name the client inventory holds for it. */
export interface AttachedClient {
  mac: string
  name: string
  signal?: number
}

/**
 * The clients one reading placed on this device, named from the client list.
 *
 * The MAC is the box's own word, so a client the inventory has never seen keeps
 * its address rather than borrowing a name from a near match. Signal is dBm and
 * absent when the firmware did not report it; the list is left in the order the
 * access point gave, which is by radio and then by association.
 */
export function attachedClients(
  snapshot: Snapshot | undefined,
  clients: ReadonlyMap<string, ClientRow>,
): AttachedClient[] {
  return (snapshot?.associations ?? []).map(association => ({
    mac: association.mac,
    name: clients.get(association.mac.toLowerCase())?.name || association.mac,
    signal: association.signal_dbm,
  }))
}

/**
 * The boxes still to ask, in inventory order.
 *
 * An answer this session already holds is evidence, and evidence is not
 * reread: opening the page again shows the same reading with the same time on
 * it rather than starting four HTTP conversations to learn nothing. A box that
 * has not been read, or one that failed the last time, has no entry and is
 * asked again.
 */
export function planReads(devices: readonly Device[], answered: (id: string) => boolean): Device[] {
  return devices.filter(device => !answered(device.id))
}

/** One address a switch reported on one port. */
export interface LearnedAddress {
  mac: string
  port_id: string
}

/**
 * Which addresses each port learned, keyed by port id.
 *
 * A forwarding table names one port per address, so this is a grouping — and an
 * address reported twice is still one address: the last report wins, because a
 * table that moved an address moved it. This is what the switch said, not a
 * count of machines: a device behind a second switch is learned on the uplink,
 * together with everything behind it.
 */
export function learnedByPort(entries: readonly LearnedAddress[]): Map<string, string[]> {
  const latest = new Map<string, string>()
  for (const entry of entries) latest.set(entry.mac.toLowerCase(), entry.port_id)
  const byPort = new Map<string, string[]>()
  for (const [mac, port] of latest) {
    const port_macs = byPort.get(port)
    if (port_macs === undefined) byPort.set(port, [mac])
    else port_macs.push(mac)
  }
  return byPort
}

/** What is at the other end of a port, as far as the two ends we know can say. */
export interface FarEnd {
  mac: string
  kind: 'router' | 'device' | 'client'
  /** The name to show: an interface name, a box, a client, or the address itself. */
  name: string
  /** Where it is: this router's interface, a box's role, a client's address. */
  detail?: string
  /** A box this console manages, when the address belongs to one. */
  deviceId?: string
  /** A client in the inventory, when the address belongs to one. */
  clientMac?: string
}

/** The three inventories an address can be recognised in. */
export interface FarEndSources {
  /** This router's own interfaces: MAC to interface name. */
  self: ReadonlyMap<string, string>
  /** The client inventory, keyed by MAC. */
  clients: ReadonlyMap<string, { name: string; ip?: string }>
  /** The managed boxes, so an address that is one of them reads as that box. */
  devices: readonly Device[]
}

/**
 * Who is at the other end of a learned address.
 *
 * Three answers, in order of how certain they are: this router's own interface
 * (we hold that MAC), a box this console manages (its management address is the
 * client's address — a join, not a reading), or a client from the inventory. An
 * address in none of them keeps its own MAC rather than borrowing a nearby name.
 */
export function farEnd(mac: string, sources: FarEndSources): FarEnd {
  const key = mac.toLowerCase()
  const ownInterface = sources.self.get(key)
  if (ownInterface !== undefined) return { mac: key, kind: 'router', name: ownInterface }
  const client = sources.clients.get(key)
  const host = client?.ip
  const device = host === undefined ? undefined : sources.devices.find(candidate => hostOf(candidate.address) === host)
  if (device !== undefined) {
    return { mac: key, kind: 'device', name: device.name || device.id, detail: host, deviceId: device.id }
  }
  return { mac: key, kind: 'client', name: client?.name || key, detail: host, clientMac: key }
}
