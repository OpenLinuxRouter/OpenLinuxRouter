import type { HourlySample } from '@/features/gateway/hourly-latency'

export interface LatencyPoint {
  hour: number
  label: string
  ms: number | null
}

export interface LatencyToday {
  points: LatencyPoint[]
  p95: number | null
  max: number | null
  samples: number
  sites: number
}

/**
 * Today's latency, averaged across every probe that has a sample.
 *
 * The hourly history already exists per site for the latency page. Overview
 * wants the network's answer, not one site's, so each hour is the mean of the
 * sites that reported in it; a missing hour stays missing rather than being
 * joined across, because a failed probe is not a fast one.
 */
export function summarizeLatency(siteSlots: (HourlySample | null)[][]): LatencyToday {
  const reporting = siteSlots.filter(slots => slots.some(sample => sample?.milliseconds != null)).length
  const points: LatencyPoint[] = Array.from({ length: 24 }, (_, hour) => {
    let total = 0
    let count = 0
    for (const slots of siteSlots) {
      const sample = slots[hour]
      if (sample?.milliseconds != null) {
        total += sample.milliseconds
        count += 1
      }
    }
    return {
      hour,
      label: `${String(hour).padStart(2, '0')}:00`,
      ms: count > 0 ? total / count : null,
    }
  })
  const values = points.flatMap(point => point.ms == null ? [] : [point.ms]).sort((a, b) => a - b)
  const p95 = values.length === 0 ? null : values[Math.min(values.length - 1, Math.floor(values.length * 0.95))]
  return {
    points,
    p95,
    max: values.length === 0 ? null : values[values.length - 1],
    samples: values.length,
    sites: reporting,
  }
}
