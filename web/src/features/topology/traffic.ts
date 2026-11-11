import { useMemo, useState } from 'react'

import { readTrafficSample, type TrafficSample } from '@/features/gateway/traffic-sample'
import type { ClientRow, GatewayTraffic } from '@/lib/api-types'

/**
 * How much a device (or a group, or the whole box) has moved, and how fast.
 *
 * Bytes are cumulative since counting started. The rates are bytes per second
 * and are absent until there are two samples to difference — "no rate yet" is
 * not the same answer as "idle", and drawing a zero would claim the second.
 */
export interface Flow {
  down: number
  up: number
  downRate?: number
  upRate?: number
}

/** Everything the map needs from gateway's counters, already joined. */
export interface TrafficView {
  /**
   * Whether there are numbers at all. False when the counters are switched off,
   * when the kernel has no table, and when the endpoint could not be read —
   * three causes, one consequence for the picture: nothing to draw.
   */
  counting: boolean

  /** Whether rates exist yet, which takes a second sample. */
  rated: boolean

  /** Every counted byte, whether or not it belongs to a device we know. */
  total: Flow

  /**
   * One address's share: its rows, summed across every exit.
   *
   * The level a device is built on, exposed for clients that are an address and
   * nothing else. A device that dialed in over WireGuard is one of those: it has
   * no MAC, so it is not a `ClientRow` (docs/remote-access.md §3.3), and the
   * counters key on exactly the tunnel address it was given.
   */
  flowOfAddress: (address: string) => Flow | undefined

  /** One device's share: its addresses' rows, summed across every exit. */
  flowOf: (device: ClientRow) => Flow | undefined

  /** One way out's share: every address's rows through it. */
  flowOfExit: (exit: string) => Flow | undefined
}

/**
 * Joins gateway's counters to devices and turns totals into rates.
 *
 * gateway counts per *address × exit*, not per device, and only as running
 * totals (internal/gateway trafficView). Both halves of what the map wants are
 * therefore worked out here, in the browser:
 *
 *   A device is its addresses. Its rows are the ones whose address is one of
 *   `device.ips`, summed over every exit — a laptop with an IPv4 and two IPv6
 *   addresses going out two ways is six rows and one device. An address nobody
 *   on the device list holds still counts toward the box total; it just has no
 *   row to be drawn on.
 *
 *   A rate is the difference between two polls divided by the difference in
 *   their `as_of` stamps, never by the poll interval: a backgrounded tab polls
 *   late, and dividing by the interval would report the late sample as a burst.
 *
 * Differences are taken per row, not per device, so a row that appears between
 * two polls — a device's first packet through a new exit — counts from zero
 * rather than dragging its sum. A row that went *down* was reset (counting
 * toggled, the table rebuilt), and reads as 0 rather than as a negative rate
 * or the whole of its new total arriving in ten seconds.
 *
 * What it cannot see is set by what is counted: only traffic crossing the
 * router. Two devices talking on the same network never reach the forwarding
 * path, so a busy NAS serving a laptop down the hall reads as idle here.
 */
export function useTrafficView(data: GatewayTraffic | undefined, failed: boolean): TrafficView {
  // The previous sample, held as state and swapped during render — React's
  // documented pattern for deriving from a changing prop, and what keeps the
  // pair consistent without an effect that would render once with a stale
  // previous sample. Keyed on `as_of` rather than identity, so a refetch that
  // returned the very same moment does not become a zero-second interval.
  //
  // It starts from the reading the last visit kept (traffic-sample.ts): the
  // counters are the router's, so a refresh does not wipe out the first half of
  // the difference and the first poll can already carry a rate.
  const [samples, setSamples] = useState<{ prev?: TrafficSample; cur?: GatewayTraffic }>(() => {
    const saved = readTrafficSample()
    return saved ? { prev: saved } : {}
  })
  if (data && data.as_of !== samples.cur?.as_of) {
    // Normally the current sample becomes the previous one. A seeded sample is
    // only ever `prev`, because there was no current one yet.
    setSamples({ prev: samples.cur ?? samples.prev, cur: data })
  }

  return useMemo(() => {
    const cur = samples.cur
    const counting = Boolean(cur?.counting) && !failed
    if (!cur || !counting) {
      return {
        counting: false,
        rated: false,
        total: { down: 0, up: 0 },
        flowOfAddress: () => undefined,
        flowOf: () => undefined,
        flowOfExit: () => undefined,
      }
    }

    const prev = samples.prev?.counting ? samples.prev : undefined
    const seconds = prev ? (Date.parse(cur.as_of) - Date.parse(prev.as_of)) / 1000 : 0
    const rated = seconds > 0

    const before = new Map<string, { up: number; down: number }>()
    for (const u of prev?.usage ?? []) {
      before.set(rowKey(u.address, u.exit), { up: u.up_bytes, down: u.down_bytes })
    }

    const byAddress = new Map<string, Flow>()
    const byExit = new Map<string, Flow>()
    const total: Flow = { down: 0, up: 0, downRate: rated ? 0 : undefined, upRate: rated ? 0 : undefined }

    for (const u of cur.usage) {
      const was = before.get(rowKey(u.address, u.exit))
      // A new row, or one whose counter went backwards, contributes nothing to
      // this interval: see the reset rule above.
      const dDown = was && u.down_bytes >= was.down ? u.down_bytes - was.down : 0
      const dUp = was && u.up_bytes >= was.up ? u.up_bytes - was.up : 0

      const key = u.address.toLowerCase()
      const flow = byAddress.get(key) ?? empty(rated)
      const exit = byExit.get(u.exit) ?? empty(rated)
      for (const f of [flow, exit, total]) {
        f.down += u.down_bytes
        f.up += u.up_bytes
        if (rated) {
          f.downRate! += dDown / seconds
          f.upRate! += dUp / seconds
        }
      }
      byAddress.set(key, flow)
      byExit.set(u.exit, exit)
    }

    const flowOfAddress = (address: string): Flow | undefined => byAddress.get(address.toLowerCase())

    const flowOf = (device: ClientRow): Flow | undefined =>
      sumFlows((device.ips ?? []).map(flowOfAddress))

    return { counting, rated, total, flowOfAddress, flowOf, flowOfExit: (exit: string) => byExit.get(exit) }
  }, [samples, failed])
}

function empty(rated: boolean): Flow {
  return { down: 0, up: 0, downRate: rated ? 0 : undefined, upRate: rated ? 0 : undefined }
}

function rowKey(address: string, exit: string) {
  return `${address.toLowerCase()}|${exit}`
}

/** Adds flows, keeping a rate only if every flow that has bytes has one. */
export function sumFlows(flows: (Flow | undefined)[]): Flow | undefined {
  let out: Flow | undefined
  for (const f of flows) {
    if (!f) continue
    if (!out) {
      out = { ...f }
      continue
    }
    out.down += f.down
    out.up += f.up
    out.downRate = out.downRate === undefined || f.downRate === undefined ? undefined : out.downRate + f.downRate
    out.upRate = out.upRate === undefined || f.upRate === undefined ? undefined : out.upRate + f.upRate
  }
  return out
}

/** The one number a bar is scaled by: the rate when there is one, else the total. */
export function magnitude(flow: Flow | undefined, rated: boolean): number {
  if (!flow) return 0
  return rated ? (flow.downRate ?? 0) + (flow.upRate ?? 0) : flow.down + flow.up
}
