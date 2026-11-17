import type { QualityCategory, QualitySample, QualitySnapshot } from './types'

/**
 * The reference score is built from medians, so the report reads the same
 * statistic instead of inventing a second summary of the same samples.
 * `mean` would let one stalled connection move the number a reader is asked to
 * trust, and the backend already rejects medians with fewer than four timings.
 */
export function median(values: number[]): number | null {
  const finite = values.filter(value => Number.isFinite(value))
  if (finite.length === 0) return null
  finite.sort((a, b) => a - b)
  const middle = finite.length >> 1
  return finite.length % 2 === 1 ? finite[middle] : (finite[middle - 1] + finite[middle]) / 2
}

function timings(samples: QualitySample[], key: 'tcp_ms' | 'dns_ms' | 'response_ms') {
  return samples.map(sample => sample[key]).filter((value): value is number => value != null && Number.isFinite(value))
}

/** Mean absolute difference between consecutive TCP timings: the same definition
 * the stability score uses. A gap restarts the run, because a discontinuity in
 * time is not a variation in latency. */
function variation(samples: QualitySample[]): number | null {
  let total = 0
  let pairs = 0
  let previous: number | null = null
  for (const sample of samples) {
    if (sample.tcp_ms == null || !Number.isFinite(sample.tcp_ms)) {
      previous = null
      continue
    }
    if (previous != null) {
      total += Math.abs(sample.tcp_ms - previous)
      pairs++
    }
    previous = sample.tcp_ms
  }
  return pairs > 0 ? total / pairs : null
}

export interface ServiceEvidence {
  id: string
  name: string
  family: 'ipv4' | 'ipv6'
  samples: number
  responded: number
  failed: number
  tcp: number | null
  dns: number | null
  response: number | null
  variation: number | null
}

function evidence(target: QualitySnapshot['targets'][number]): ServiceEvidence {
  return {
    id: target.id,
    name: target.name,
    family: target.family,
    samples: target.samples.length,
    responded: target.samples.filter(sample => sample.state === 'ok').length,
    failed: target.samples.filter(sample => sample.state === 'failed').length,
    tcp: median(timings(target.samples, 'tcp_ms')),
    dns: median(timings(target.samples, 'dns_ms')),
    response: median(timings(target.samples, 'response_ms')),
    variation: variation(target.samples),
  }
}

/**
 * One row per service, not per address family. The score picks one whole family
 * for each service, so the report picks one too: the family that completed more
 * responses, then the faster one. Showing both families would double every row
 * and invite a reader to average two paths the score never averages.
 */
export function serviceEvidence(data: QualitySnapshot | undefined): ServiceEvidence[] {
  const grouped = new Map<string, ServiceEvidence[]>()
  for (const target of data?.targets ?? []) {
    const list = grouped.get(target.id) ?? []
    list.push(evidence(target))
    grouped.set(target.id, list)
  }
  const out: ServiceEvidence[] = []
  for (const list of grouped.values()) {
    const best = [...list].sort((a, b) => {
      if (a.responded !== b.responded) return b.responded - a.responded
      if (a.response != null && b.response != null && a.response !== b.response) return a.response - b.response
      return a.family === 'ipv4' ? -1 : 1
    })[0]
    out.push(best)
  }
  return out
}

export interface SpeedEvidence {
  download: number
  upload: number
  server: string
  location: string
}

export interface LatencyEvidence {
  response: number | null
  variation: number | null
}

export interface StabilityEvidence {
  responded: number
  attempted: number
  variation: number | null
}

export interface FunctionalityEvidence {
  reachable: number
  measured: number
}

export interface WirelessEvidence {
  links: number
  accessPoints: number
  answered: number
  weakest: number | null
}

export type CategoryEvidence =
  | { kind: 'speed'; speed: SpeedEvidence | null }
  | { kind: 'latency'; latency: LatencyEvidence }
  | { kind: 'stability'; stability: StabilityEvidence }
  | { kind: 'functionality'; functionality: FunctionalityEvidence }
  | { kind: 'wireless'; wireless: WirelessEvidence | null }
  | { kind: 'unmeasured' }

/**
 * What the category row says beside its score. Each category is summarised in
 * its own units: a percentage of responses would be a second score, and the row
 * already has one.
 */
export function categoryEvidence(category: QualityCategory, services: ServiceEvidence[], data?: QualitySnapshot): CategoryEvidence {
  const used = (evidence: ServiceEvidence[]) => evidence.filter(item => item.samples > 0)
  switch (category.id) {
    case 'speed': {
      const speed = data?.speed
      return {
        kind: 'speed',
        speed: speed
          ? { download: speed.download_mbps, upload: speed.upload_mbps, server: speed.server, location: speed.location }
          : null,
      }
    }
    case 'latency':
      return {
        kind: 'latency',
        latency: {
          response: median(used(services).map(item => item.response).filter((value): value is number => value != null)),
          variation: median(used(services).map(item => item.variation).filter((value): value is number => value != null)),
        },
      }
    case 'stability': {
      const measured = used(services)
      return {
        kind: 'stability',
        stability: {
          responded: measured.reduce((total, item) => total + item.responded, 0),
          attempted: measured.reduce((total, item) => total + item.samples, 0),
          variation: median(measured.map(item => item.variation).filter((value): value is number => value != null)),
        },
      }
    }
    case 'functionality': {
      const measured = used(services)
      return {
        kind: 'functionality',
        functionality: {
          reachable: measured.filter(item => item.responded > 0).length,
          measured: measured.length,
        },
      }
    }
    case 'wifi': {
      const wireless = data?.wireless
      if (!wireless) return { kind: 'wireless', wireless: null }
      const signals = wireless.links
        .map(link => link.signal_dbm)
        .filter((value): value is number => value != null && Number.isFinite(value))
      return {
        kind: 'wireless',
        wireless: {
          links: wireless.links.length,
          accessPoints: wireless.aps.length,
          answered: wireless.aps.filter(ap => ap.state === 'ok').length,
          weakest: signals.length > 0 ? Math.min(...signals) : null,
        },
      }
    }
    default:
      return { kind: 'unmeasured' }
  }
}
