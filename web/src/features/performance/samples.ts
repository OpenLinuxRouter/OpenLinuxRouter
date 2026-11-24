import type { RateSample } from '@/features/link/interface-traffic'
import type { GatewayLatency } from '@/lib/api-types'

import type { CapacitySide, GatewayCapacity } from './queries'

export interface LinkPoint {
  at: number
  down?: number
  up?: number
  latency?: number
}

export interface LatencyPoint {
  at: number
  ms: number
}

/** A baseline is only usable when it is a positive, finite rate. */
export function usableCapacity(side: CapacitySide | undefined): number | undefined {
  if (!side || !Number.isFinite(side.p99_bps) || side.p99_bps <= 0) return undefined
  return side.p99_bps
}

/**
 * The baseline, if this daemon published one. Anything malformed is "no
 * baseline": a partial capacity is not a number to draw a line at.
 */
export function usableCapacityData(data: GatewayCapacity | undefined): GatewayCapacity | undefined {
  if (!data || !data.uplink) return undefined
  if (data.state !== 'measured' && data.state !== 'warming') return undefined
  if (!Number.isFinite(Date.parse(data.as_of))) return undefined
  if (usableCapacity(data.rx) == null && usableCapacity(data.tx) == null) return undefined
  return data
}

/** Current bytes per second over capacity in bits per second, as a ratio. */
export function utilisation(bytesPerSecond: number | undefined, capacityBps: number | undefined): number | undefined {
  if (bytesPerSecond == null || !Number.isFinite(bytesPerSecond) || bytesPerSecond < 0) return undefined
  if (capacityBps == null || !Number.isFinite(capacityBps) || capacityBps <= 0) return undefined
  return (bytesPerSecond * 8) / capacityBps
}

export function formatPercent(ratio: number | undefined): string | undefined {
  if (ratio == null || !Number.isFinite(ratio)) return undefined
  const percent = ratio * 100
  return percent < 10 ? `${percent.toFixed(1)}%` : `${Math.round(percent)}%`
}

/** Frames per second, an integer unless it is a small fraction. */
export function formatFrameRate(value: number | undefined): string | undefined {
  if (value == null || !Number.isFinite(value)) return undefined
  if (value === 0) return '0'
  return value < 10 ? value.toFixed(1) : String(Math.round(value))
}

/**
 * A share of received frames, as a percentage.
 *
 * Below a tenth of a percent it reads "<0.1%" rather than rounding to zero: a
 * counter that keeps ticking after being rounded away is exactly the reading
 * this figure exists to keep in view.
 */
export function formatShare(ratio: number | undefined): string | undefined {
  if (ratio == null || !Number.isFinite(ratio) || ratio < 0) return undefined
  const percent = ratio * 100
  if (percent === 0) return '0%'
  if (percent < 0.1) return '<0.1%'
  return percent < 10 ? `${percent.toFixed(1)}%` : `${Math.round(percent)}%`
}

/** The coverage sentence for a provisional baseline. */
export function coverageCopy(coveredMs: number | undefined): { key: string; count: number } | undefined {
  if (coveredMs == null || !Number.isFinite(coveredMs) || coveredMs <= 0) return undefined
  const minutes = Math.max(1, Math.round(coveredMs / 60_000))
  // Whole hours read best; anything else stays in the unit it was measured in
  // rather than rounding 90 minutes up to a claim of two hours.
  if (minutes >= 60 && minutes % 60 === 0) {
    return { key: 'Provisional baseline. Based on {count} hours of samples.', count: minutes / 60 }
  }
  return { key: 'Provisional baseline. Based on {count} minutes of samples.', count: minutes }
}

/** The shorter of the two direction windows, so the coverage claim is shared. */
export function coveredWindow(data: GatewayCapacity | undefined): number | undefined {
  if (!data) return undefined
  const asOf = Date.parse(data.as_of)
  const starts = [data.rx?.since, data.tx?.since]
    .filter((since): since is string => typeof since === 'string')
    .map(since => Date.parse(since))
    .filter(at => Number.isFinite(at))
  if (!Number.isFinite(asOf) || starts.length === 0) return undefined
  const covered = asOf - Math.max(...starts)
  return covered > 0 ? covered : undefined
}

/** The smaller sample count of the two directions, so the claim is conservative. */
export function sampleCount(data: GatewayCapacity | undefined): number | undefined {
  if (!data) return undefined
  const points = [data.rx?.points, data.tx?.points]
    .filter(value => Number.isFinite(value) && value >= 0)
  return points.length ? Math.min(...points) : undefined
}

/** The target HealthCard and the latency page read: the selected probe. */
export function latencyTarget(latency: GatewayLatency | undefined) {
  return latency?.custom.find(site => site.url === latency.target_url)
    ?? latency?.custom.find(site => site.name === latency.target)
    ?? latency?.custom[0]
}

/**
 * Probe samples that fall inside the live window.
 *
 * These are existing measurements with their own `checked_at`; nothing is
 * interpolated onto a one-second grid, and a failed sample is omitted rather
 * than drawn as a fast one.
 */
export function latencyPoints(latency: GatewayLatency | undefined, start: number, end: number): LatencyPoint[] {
  const target = latencyTarget(latency)
  const points = new Map<number, number>()
  for (const sample of target?.samples ?? []) {
    const at = Date.parse(sample.checked_at)
    if (sample.milliseconds == null || !Number.isFinite(at) || at < start || at > end) continue
    points.set(at, sample.milliseconds)
  }
  const checked = target?.checked_at ? Date.parse(target.checked_at) : Number.NaN
  if (target?.milliseconds != null && Number.isFinite(checked) && checked >= start && checked <= end) {
    points.set(checked, target.milliseconds)
  }
  return [...points.entries()]
    .map(([at, ms]) => ({ at, ms }))
    .sort((a, b) => a.at - b.at)
}

/**
 * Throughput points with latency attached where the two series can be aligned.
 *
 * The latency probe and the interface counters are polled separately, so their
 * timestamps almost never coincide. A latency sample is attached to the nearest
 * throughput sample within two seconds; a sample with no throughput point
 * nearby is dropped rather than drawn on a timestamp where nothing was read.
 * That keeps the throughput line free of artificial holes, and a latency line
 * with fewer points than the probe produced says only that the two samplers
 * were not in step.
 */
export function linkPoints(history: RateSample[], latency: LatencyPoint[], start: number, toleranceMs = 2000): LinkPoint[] {
  const points: LinkPoint[] = history
    .filter(sample => sample.at >= start)
    .map(sample => ({ at: sample.at, down: sample.rx, up: sample.tx }))

  for (const point of latency) {
    if (point.at < start || points.length === 0) continue
    let nearest = points[0]
    for (const candidate of points) {
      if (Math.abs(candidate.at - point.at) < Math.abs(nearest.at - point.at)) nearest = candidate
    }
    if (Math.abs(nearest.at - point.at) > toleranceMs) continue
    nearest.latency = point.ms
  }

  return points.sort((a, b) => a.at - b.at)
}
