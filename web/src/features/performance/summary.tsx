import { StatusDot } from '@/components/ui/status'
import { OverviewCard } from '@/features/overview/card'
import { t, useLocale } from '@/lib/i18n'
import type { Tone } from '@/lib/state'

import { WINDOW_MS } from './live'
import { formatFrameRate, formatPercent } from './samples'
import type { PerformanceEvidence } from './use-performance'

const WINDOW_MINUTES = Math.round(WINDOW_MS / 60_000)

function toneFor(code: PerformanceEvidence['verdict']['code']): Tone {
  if (code === 'link-free' || code === 'link-free-router-idle') return 'ok'
  if (code === 'checking' || code === 'no-capacity' || code === 'no-throughput' || code === 'no-utilisation') return 'unknown'
  return 'warn'
}

/**
 * The verdict, the evidence sentence, and the window it came from.
 *
 * The sentence is built from the measured figures rather than from a template
 * that sounds confident: "no capacity baseline" is a complete answer, and the
 * summary says which part of the question the data cannot reach.
 */
export function SummaryStrip({ evidence }: { evidence: PerformanceEvidence }) {
  useLocale()
  const { verdict } = evidence
  const best = Math.max(evidence.downloadUtil ?? -1, evidence.uploadUtil ?? -1)
  const percent = formatPercent(best >= 0 ? best : undefined)
  const cpu = formatPercent(evidence.cpuPercent == null ? undefined : evidence.cpuPercent / 100)
  const discards = formatFrameRate(evidence.discardsPerSecond)

  let headline: string
  let detail: string

  switch (verdict.code) {
    case 'checking':
      headline = t('Checking the capacity baseline…')
      detail = t('Reading the capacity baseline before scoring the line.')
      break
    case 'no-capacity':
      headline = t('No capacity baseline yet')
      detail = t('Without a measured baseline the line cannot be scored. Utilisation and the capacity line are unavailable.')
      break
    case 'no-throughput':
      headline = t('No throughput readings yet')
      detail = evidence.linkState === 'down'
        ? t('The uplink interface is down, so there is no rate to score.')
        : evidence.linkState === 'missing'
          ? t('The uplink interface is not present, so there is no rate to score.')
          : t('The uplink counters are not reporting a rate, so the line cannot be scored.')
      break
    case 'no-utilisation':
      headline = t('Utilisation unavailable')
      detail = t('No usable capacity covers the direction that reported a rate.')
      break
    case 'upload-near-capacity':
      headline = t('Upload is near the known capacity')
      detail = t('Inferred upload utilisation is {percent} of the measured upload capacity.', { percent: percent ?? '—' })
      break
    case 'download-near-capacity':
      headline = t('Download is near the known capacity')
      detail = t('Inferred download utilisation is {percent} of the measured download capacity.', { percent: percent ?? '—' })
      break
    case 'router-dropping':
      headline = t('The router is discarding input')
      detail = t('The line has headroom, but the router discards {rate} frames per second: received, and not processed (RFC 2863 ifInDiscards).', { rate: discards ?? '—' })
      break
    case 'link-free-router-busy':
      headline = t('Line not saturated, router busy')
      detail = t('Inferred utilisation is {percent} of measured capacity, and measured CPU is {cpu}.', { percent: percent ?? '—', cpu: cpu ?? '—' })
      break
    case 'link-free-router-idle':
      headline = t('Line not saturated, router idle')
      detail = t('Inferred utilisation is {percent} of measured capacity, and measured CPU is {cpu}.', { percent: percent ?? '—', cpu: cpu ?? '—' })
      break
    default:
      headline = t('Line not saturated')
      detail = t('Inferred utilisation is {percent} of measured capacity.', { percent: percent ?? '—' })
      break
  }

  return <OverviewCard bodyClassName="gap-3">
    <div className="flex flex-wrap items-start justify-between gap-4">
      <div className="min-w-0 space-y-1.5">
        <div className="flex items-center gap-2">
          <StatusDot tone={toneFor(verdict.code)} size="md" />
          <p className="text-base leading-6 font-semibold">{headline}</p>
        </div>
        <p className="max-w-2xl text-sm leading-5 text-muted-foreground">{detail}</p>
        {verdict.provisional && <p className="text-xs leading-4 text-muted-foreground">
          {evidence.coverage
            ? t(evidence.coverage.key, { count: evidence.coverage.count })
            : t('Provisional baseline; the covered window is unavailable.')}
        </p>}
      </div>
      <div className="shrink-0 space-y-0.5 sm:text-right">
        <p className="text-xs font-medium text-muted-foreground">{t('Time window')}</p>
        <p className="text-sm tabular-nums">{t('Last {count} minutes', { count: WINDOW_MINUTES })}</p>
        <p className="text-xs text-muted-foreground">{t('1 s resolution, live')}</p>
      </div>
    </div>
  </OverviewCard>
}
