import { useEffect, useState } from 'react'

import { useHostMetrics } from '@/features/system/queries'

/** The live window: three minutes, sampled once a second. */
export const SAMPLE_MS = 1000
export const WINDOW_MS = 180_000

/** A clock that advances at the sample cadence, shared by both lanes. */
export function useNow(intervalMs = SAMPLE_MS) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), intervalMs)
    return () => window.clearInterval(timer)
  }, [intervalMs])
  return now
}

/** The wall-clock edges and tick positions both lanes draw against. */
export function timeWindow(now: number) {
  const start = now - WINDOW_MS
  return {
    start,
    end: now,
    ticks: [0, 60_000, 120_000, WINDOW_MS].map(offset => start + offset),
  }
}

export function clock(at: number, locale: string) {
  return new Date(at).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

/**
 * The short form for an axis tick. Four labels have to fit inside a phone's
 * plot, and a three-minute window never needs the hour to identify a tick.
 */
export function clockShort(at: number, locale: string) {
  return new Date(at).toLocaleTimeString(locale, { minute: '2-digit', second: '2-digit' })
}

export function when(iso: string, locale: string) {
  const at = Date.parse(iso)
  if (!Number.isFinite(at)) return undefined
  return new Date(at).toLocaleString(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

export interface CpuSample {
  at: number
  pct: number
}

/**
 * CPU history from the existing host metrics endpoint.
 *
 * The endpoint publishes one current reading, not a series, so the page keeps
 * its own three-minute buffer. `dataUpdatedAt` is the observation time: it is
 * when this browser received that reading, and it changes even when the
 * percentage is identical, which a value-keyed effect would not.
 */
export function useCpuSamples() {
  const host = useHostMetrics(SAMPLE_MS)
  const [samples, setSamples] = useState<CpuSample[]>([])
  const used = host.data?.cpu_used_cores
  const cores = host.data?.cpu_cores
  const at = host.dataUpdatedAt

  useEffect(() => {
    if (used == null || !cores || !at) return
    const pct = Math.max(0, Math.min(100, (used / cores) * 100))
    setSamples(previous => previous.at(-1)?.at === at
      ? previous
      : [...previous, { at, pct }].slice(-600))
  }, [at, used, cores])

  // A failed read is not the last value. The chart keeps the samples that were
  // actually measured; the current figure becomes a dash.
  const current = !host.isError && used != null && cores ? Math.max(0, Math.min(100, (used / cores) * 100)) : undefined
  return {
    samples,
    current,
    cores,
    isError: host.isError,
    isPending: host.isPending,
  }
}
