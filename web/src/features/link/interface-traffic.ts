import { useState } from 'react'

import type { InterfaceList, InterfaceRow } from '@/lib/api-types'

export interface RateSample {
  /** Observation time, from the interface list's `as_of`. */
  at: number
  /** Bytes per second, from the NIC byte counters. */
  rx: number
  tx: number
  /**
   * Received packets per second. Only the denominator a discard share needs:
   * "1.0 per second" says nothing until it is a fraction of something.
   */
  packets?: number
  /**
   * Input discards per second — RFC 2863 `ifInDiscards`: received, and
   * discarded although no error prevented delivery. Receive-only on purpose:
   * `ifOutDiscards` is a different counter with a different cause, and adding
   * the two directions together produces a number no standard defines.
   *
   * Absent when the driver publishes no counter, which is not a zero.
   */
  discards?: number
  /** Input errors per second — `ifInErrors`: arrived, but not deliverable. */
  errors?: number
  /**
   * Input missed per second — packets the device could not hand up because it
   * ran out of buffer space. The kernel keeps this apart from `rx_dropped`
   * because a full device and an unwanted frame are different faults.
   */
  missed?: number
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
    // One rule for every counter: present, an integer, and never going
    // backwards. A counter that moves backwards is a reset, not a negative
    // rate, and a driver that publishes nothing leaves the rate absent rather
    // than zero — the two read the same on a chart and mean opposite things.
    const rateOf = (current?: number, previous?: number) => {
      if (current == null || previous == null) return undefined
      if (!Number.isSafeInteger(current) || !Number.isSafeInteger(previous)) return undefined
      if (previous < 0 || current < previous) return undefined
      return (current - previous) / seconds
    }
    let history: RateSample[] = []
    if (continuous) {
      const sample: RateSample = {
        at,
        rx: (row.rx_bytes! - prev.row.rx_bytes!) / seconds,
        tx: (row.tx_bytes! - prev.row.tx_bytes!) / seconds,
      }
      // A counter the kernel did not answer for stays absent, because an
      // absent rate and a measured zero mean opposite things.
      const packets = rateOf(row.rx_packets, prev.row.rx_packets)
      const discards = rateOf(row.rx_dropped, prev.row.rx_dropped)
      const errors = rateOf(row.rx_errors, prev.row.rx_errors)
      const missed = rateOf(row.rx_missed_errors, prev.row.rx_missed_errors)
      if (packets != null) sample.packets = packets
      if (discards != null) sample.discards = discards
      if (errors != null) sample.errors = errors
      if (missed != null) sample.missed = missed
      history = [...prev.history, sample].filter(item => item.at > at - historyMs)
    }
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
