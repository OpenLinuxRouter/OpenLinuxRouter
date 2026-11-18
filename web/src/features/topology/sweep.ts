import { useSyncExternalStore } from 'react'

import { learnedByPort, type FarEnd, type LearnedAddress } from './equipment.ts'

/**
 * What one sweep of a switch's forwarding table found, kept for this session.
 *
 * A forwarding table cannot be read in one request on this hardware: the
 * firmware searches one MAC at a time, so "which port is each address on" is one
 * conversation per address, and a sweep of a LAN's inventory takes half a
 * minute. The page runs it on arrival, after the boxes have been read, and the
 * map fills in as answers land; the answer stays here, keyed by device, until
 * the page is reloaded — with the time it arrived, because a forwarding table
 * ages.
 */
export interface PortSweep {
  /** When the last answer in this sweep arrived. */
  at: number
  /** What each port has learned, by port id. */
  byPort: ReadonlyMap<string, readonly FarEnd[]>
  /** Addresses the switch placed on a port, out of the ones asked about. */
  placed: number
  /** Addresses asked about: the client inventory this router knows. */
  asked: number
  /** Lookups the switch did not answer. */
  failed: number
  /** Still asking: the table so far, growing as answers arrive. */
  partial?: boolean
  /** Addresses this sweep will ask about in all. */
  total?: number
}

let sweeps: ReadonlyMap<string, PortSweep> = new Map()
const listeners = new Set<() => void>()

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

export function rememberSweep(deviceId: string, sweep: PortSweep): void {
  // A new map, not a set: the map page reads this store through
  // useSyncExternalStore, which compares by identity.
  const next = new Map(sweeps)
  next.set(deviceId, sweep)
  sweeps = next
  listeners.forEach(listener => listener())
}

/** One device's sweep, re-rendering the caller when a sweep lands. */
export function usePortSweep(deviceId: string): PortSweep | undefined {
  return useSyncExternalStore(subscribe, () => sweeps.get(deviceId))
}

/** Every sweep this session holds, as a map the map can lay out from. */
export function allSweeps(): ReadonlyMap<string, PortSweep> {
  return sweeps
}

export function useAllSweeps(): ReadonlyMap<string, PortSweep> {
  return useSyncExternalStore(subscribe, allSweeps, allSweeps)
}

const running = new Set<string>()

/** Whether a sweep of this device is in flight. */
export function isSweeping(deviceId: string): boolean {
  return running.has(deviceId)
}

/**
 * Ask one switch where each address is, one lookup at a time, publishing the
 * table after every answer so the map fills in while it runs. A device already
 * being swept is left alone; one already swept this session is only asked
 * again when `force` says so.
 */
export async function runSweep({
  deviceId,
  macs,
  lookup,
  resolve,
  force = false,
}: {
  deviceId: string
  macs: readonly string[]
  lookup: (mac: string) => Promise<readonly LearnedAddress[]>
  resolve: (mac: string) => FarEnd
  force?: boolean
}): Promise<void> {
  if (running.has(deviceId) || (!force && sweeps.has(deviceId) && sweeps.get(deviceId)?.partial !== true)) return
  running.add(deviceId)
  const learned: LearnedAddress[] = []
  let failed = 0
  const publish = (asked: number, partial: boolean) => {
    const byPort = new Map([...learnedByPort(learned)].map(([port, addresses]) => [port, addresses.map(resolve)]))
    rememberSweep(deviceId, {
      at: Date.now(),
      byPort,
      placed: [...byPort.values()].reduce((sum, ends) => sum + ends.length, 0),
      asked,
      failed,
      partial,
      total: macs.length,
    })
  }
  try {
    publish(0, true)
    for (const [index, mac] of macs.entries()) {
      try {
        learned.push(...await lookup(mac))
      } catch {
        failed += 1
      }
      publish(index + 1, index + 1 < macs.length)
    }
    if (macs.length === 0) publish(0, false)
  } finally {
    running.delete(deviceId)
  }
}
