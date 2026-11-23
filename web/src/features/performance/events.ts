import type { RateSample } from '@/features/link/interface-traffic'
import type { CpuSample } from './live'
import { BUSY_CPU, DROP_BUSY } from './verdict.ts'

/**
 * Events detected in the live window, from the samples already on screen.
 *
 * The band exists to show when a measured series moved, not to explain why it
 * moved. Every event carries the two measured values and the interval between
 * them; this module deliberately has no vocabulary for a cause.
 */

/** A fall has to be below this share of the window's own median to count. */
export const FALL_SHARE = 0.5

export type EventSource = 'throughput' | 'cpu' | 'drops'

export interface ThroughputEvent {
  kind: 'throughput'
  at: number
  direction: 'down' | 'up'
  /** Bytes per second, the unit the interface counters produce. */
  fromBps: number
  toBps: number
  overMs: number
  /** The window median the fall was measured against. */
  medianBps: number
}

export interface CpuEvent {
  kind: 'cpu'
  at: number
  fromPercent: number
  toPercent: number
}

export interface DropsEvent {
  kind: 'drops'
  at: number
  fromPerSecond: number
  toPerSecond: number
}

export type PerformanceEvent = ThroughputEvent | CpuEvent | DropsEvent

export interface EventInput {
  start: number
  end: number
  history: readonly RateSample[]
  cpuSamples: readonly CpuSample[]
}

export interface EventScan {
  events: PerformanceEvent[]
  /** The sources that had enough samples to look at, whether or not they fired. */
  watched: EventSource[]
}

export function median(values: readonly number[]): number | undefined {
  const usable = values.filter(value => Number.isFinite(value)).sort((a, b) => a - b)
  if (usable.length === 0) return undefined
  const middle = Math.floor(usable.length / 2)
  return usable.length % 2 === 1
    ? usable[middle]
    : (usable[middle - 1] + usable[middle]) / 2
}

/**
 * A fall below half the window's median, after a reading at or above it.
 *
 * "Previously high" is the window's own median: the fall has to start from a
 * reading the window itself calls high. The detector fires once per fall, then
 * waits for the series to reach the median again before it can fire again, so a
 * sustained low is one event rather than one per sample.
 */
function throughputEvents(samples: readonly RateSample[]): ThroughputEvent[] {
  if (samples.length === 0) return []
  const events: ThroughputEvent[] = []

  for (const direction of ['down', 'up'] as const) {
    const values = samples.map(sample => direction === 'down' ? sample.rx : sample.tx)
    const base = median(values)
    if (base == null || base <= 0) continue

    let high: { at: number; value: number } | undefined
    for (const sample of samples) {
      const value = direction === 'down' ? sample.rx : sample.tx
      if (!Number.isFinite(value)) continue
      if (value >= base) {
        high = { at: sample.at, value }
        continue
      }
      if (high == null || value >= base * FALL_SHARE) continue
      events.push({
        kind: 'throughput',
        at: sample.at,
        direction,
        fromBps: high.value,
        toBps: value,
        overMs: sample.at - high.at,
        medianBps: base,
      })
      // Wait for the series to recover to the median before marking another
      // fall. Otherwise every following low sample would be a new event.
      high = undefined
    }
  }

  return events
}

function cpuEvents(samples: readonly CpuSample[]): CpuEvent[] {
  const events: CpuEvent[] = []
  for (let index = 1; index < samples.length; index += 1) {
    const previous = samples[index - 1]
    const current = samples[index]
    if (!Number.isFinite(previous.pct) || !Number.isFinite(current.pct)) continue
    if (previous.pct < BUSY_CPU && current.pct >= BUSY_CPU) {
      events.push({ kind: 'cpu', at: current.at, fromPercent: previous.pct, toPercent: current.pct })
    }
  }
  return events
}

function dropsEvents(samples: readonly RateSample[]): DropsEvent[] {
  const measured = samples.filter((sample): sample is RateSample & { dropped: number } =>
    sample.dropped != null && Number.isFinite(sample.dropped))
  const events: DropsEvent[] = []
  for (let index = 1; index < measured.length; index += 1) {
    const previous = measured[index - 1]
    const current = measured[index]
    if (previous.dropped < DROP_BUSY && current.dropped >= DROP_BUSY) {
      events.push({ kind: 'drops', at: current.at, fromPerSecond: previous.dropped, toPerSecond: current.dropped })
    }
  }
  return events
}

function inWindow<T extends { at: number }>(samples: readonly T[], start: number, end: number): T[] {
  return samples.filter(sample => Number.isFinite(sample.at) && sample.at >= start && sample.at <= end)
}

/**
 * Scan the window once.
 *
 * A source with fewer than two samples cannot show a crossing, so it is not
 * watched. The band says so rather than calling the window quiet: "quiet" is a
 * measurement, and an unread series is not one.
 */
export function scanEvents(input: EventInput): EventScan {
  const history = inWindow(input.history, input.start, input.end)
  const cpu = inWindow(input.cpuSamples, input.start, input.end)

  const watched: EventSource[] = []
  const events: PerformanceEvent[] = []

  if (history.length >= 2) {
    watched.push('throughput')
    events.push(...throughputEvents(history))
  }
  if (cpu.length >= 2) {
    watched.push('cpu')
    events.push(...cpuEvents(cpu))
  }
  if (history.filter(sample => sample.dropped != null).length >= 2) {
    watched.push('drops')
    events.push(...dropsEvents(history))
  }

  const order: Record<EventSource, number> = { throughput: 0, cpu: 1, drops: 2 }
  events.sort((a, b) => a.at - b.at || order[a.kind] - order[b.kind])
  return { events, watched }
}
