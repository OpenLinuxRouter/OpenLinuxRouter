import { useState } from 'react'

import type { InterfaceList, InterfaceRow } from '@/lib/api-types'

export interface RateSample {
  /** Observation time, from the interface list's `as_of`. */
  at: number
  /** Bytes per second, from the NIC byte counters. */
  rx: number
  tx: number
  /**
   * Dropped packets per second, received plus transmitted. Absent when the
   * driver does not publish both drop counters: a driver that counts only one
   * direction leaves the total unknown, not zero.
   */
  dropped?: number
}
interface Reading { row: InterfaceRow; at: number; history: RateSample[] }

/** Rates are bytes/second from NIC counters, not forwarded gateway traffic. */
export function nextInterfaceReadings(data: InterfaceList, before: Map<string, Reading>, historyMs = 60_000) {
  const at = Date.parse(data.as_of)
  const next = new Map<string, Reading>()
  if (!Number.isFinite(at)) return next
  for (const row of data.interfaces) {
    const prev = before.get(row.name)
    const age = prev ? at - prev.at : 0
    const counters = [row.rx_bytes, row.tx_bytes, prev?.row.rx_bytes, prev?.row.tx_bytes]
    const valid = counters.every(n => n !== undefined && Number.isSafeInteger(n) && n >= 0)
    const dropCounters = [row.rx_dropped, row.tx_dropped, prev?.row.rx_dropped, prev?.row.tx_dropped]
    // A counter that goes backwards is a reset, not a negative drop rate.
    const dropsValid = dropCounters.every(n => n !== undefined && Number.isSafeInteger(n) && n >= 0)
      && prev != null && row.rx_dropped! >= prev.row.rx_dropped! && row.tx_dropped! >= prev.row.tx_dropped!
    const same = prev && row.index === prev.row.index && row.mac === prev.row.mac
    // Two responses can carry the same server timestamp at a one-second poll.
    // That is not a zero-length interval to divide by, and it is not a reason to
    // throw the window away: keep the history and let the next advancing read
    // produce the sample. A clock that moves backwards still resets below.
    if (same && age === 0) {
      next.set(row.name, { ...prev, row })
      continue
    }
    const connected = row.present && row.up && row.running
    const continuous = same && connected && prev.row.present && prev.row.up && prev.row.running
      && age > 0 && age <= 15_000 && valid
      && row.rx_bytes! >= prev.row.rx_bytes! && row.tx_bytes! >= prev.row.tx_bytes!
    const seconds = age / 1000
    const dropped = continuous && dropsValid
      ? ((row.rx_dropped! - prev.row.rx_dropped!) + (row.tx_dropped! - prev.row.tx_dropped!)) / seconds
      : undefined
    const history = continuous ? [
      ...prev.history,
      {
        at,
        rx: (row.rx_bytes! - prev.row.rx_bytes!) / seconds,
        tx: (row.tx_bytes! - prev.row.tx_bytes!) / seconds,
        // A missing drop counter stays absent rather than becoming a zero.
        ...(dropped == null ? {} : { dropped }),
      },
    ].filter(item => item.at > at - historyMs) : []
    next.set(row.name, { row, at, history })
  }
  return next
}

/** Reset after counter rollover, unplug/recreation, or a long polling gap. */
export function useInterfaceTraffic(data: InterfaceList | undefined, failed: boolean, historyMs = 60_000) {
  const [state, setState] = useState<{ data?: InterfaceList; failed: boolean; readings: Map<string, Reading> }>({
    failed: false, readings: new Map(),
  })
  if (data !== state.data || failed !== state.failed) {
    setState({ data, failed, readings: data && !failed ? nextInterfaceReadings(data, state.failed ? new Map() : state.readings, historyMs) : new Map() })
  }
  return failed ? new Map<string, Reading>() : state.readings
}
