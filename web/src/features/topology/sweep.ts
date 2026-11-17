import { useSyncExternalStore } from 'react'

import type { FarEnd } from './equipment.ts'

/**
 * What one sweep of a switch's forwarding table found, kept for this session.
 *
 * A forwarding table cannot be read in one request on this hardware: the
 * firmware searches one MAC at a time, so "which port is each address on" is one
 * conversation per address, and a sweep of a LAN's inventory takes half a
 * minute. That is too slow to run on arrival and too expensive to run twice, so
 * the answer stays here, keyed by device, until the page is reloaded — with the
 * time it arrived, because a forwarding table ages.
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
