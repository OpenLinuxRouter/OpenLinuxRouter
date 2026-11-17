import type { Device, Snapshot } from '@/features/devices/types'
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

export function equipmentFacts(snapshot: Snapshot | undefined): EquipmentFacts | undefined {
  if (snapshot === undefined) return undefined
  const facts: EquipmentFacts = {}
  const model = [snapshot.identity?.manufacturer, snapshot.identity?.model].filter(Boolean).join(' ')
  if (model !== '') facts.model = model
  const ports = snapshot.ports
  if (ports !== undefined && ports.length > 0) {
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
