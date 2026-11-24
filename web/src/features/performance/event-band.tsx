import { useMemo } from 'react'

import { Heading } from '@/components/ui/heading'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { OverviewCard } from '@/features/overview/card'
import { t, useLocale } from '@/lib/i18n'
import { cn, formatRate } from '@/lib/utils'

import { scanEvents, type EventSource, type PerformanceEvent } from './events'
import { clock } from './live'
import { formatFrameRate, formatPercent } from './samples'
import type { PerformanceEvidence } from './use-performance'
import { BUSY_CPU, DISCARD_BUSY } from './verdict'

/**
 * Events on the same time axis as the two lanes.
 *
 * This is not a second plot: it has no series and no scale of its own. It is a
 * strip whose horizontal geometry repeats the lanes' fixed axis slots — the
 * left Y axis is 58px wide with the chart's −6px margin, and the right axis is
 * 40px with a 4px margin — so a mark sits under the sample that fired it. That
 * is the one thing the strip has to get right; the detection itself lives in
 * events.ts and is pure.
 */

/** The lanes' plot insets: (−6 + 58)px on the left and (40 + 4)px on the right. */
const PLOT_INSET = 'pl-[52px] pr-[44px]'

/** The three rows, top to bottom, keep simultaneous marks from covering each other. */
const ROW_TOP: Record<EventSource, string> = {
  throughput: 'top-0',
  cpu: 'top-3',
  discards: 'top-6',
}

function MarkShape({ kind, className }: { kind: EventSource; className?: string }) {
  if (kind === 'throughput') return <span aria-hidden className={cn('size-2 rounded-full bg-foreground', className)} />
  if (kind === 'cpu') return <span aria-hidden className={cn('size-2 rounded-[1px] bg-foreground', className)} />
  return <span aria-hidden className={cn('size-2 bg-foreground [clip-path:polygon(50%_0,100%_100%,0_100%)]', className)} />
}

function LegendItem({ kind, label }: { kind: EventSource; label: string }) {
  return <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
    <MarkShape kind={kind} />
    {label}
  </span>
}

/** Only the measured pair and the interval between them; never a cause. */
function eventSentence(event: PerformanceEvent): string {
  switch (event.kind) {
    case 'throughput': {
      const values = {
        from: formatRate(event.fromBps),
        to: formatRate(event.toBps),
        count: Math.max(1, Math.round(event.overMs / 1000)),
      }
      return event.direction === 'down'
        ? t('Download {from} → {to} over {count}s', values)
        : t('Upload {from} → {to} over {count}s', values)
    }
    case 'cpu':
      return t('CPU {from} → {to}', {
        from: formatPercent(event.fromPercent / 100) ?? '—',
        to: formatPercent(event.toPercent / 100) ?? '—',
      })
    case 'discards':
      return t('Input discards {from} → {to} /s', {
        from: formatFrameRate(event.fromPerSecond) ?? '—',
        to: formatFrameRate(event.toPerSecond) ?? '—',
      })
  }
}

function fraction(at: number, start: number, end: number): number {
  if (end <= start) return 0
  return Math.min(1, Math.max(0, (at - start) / (end - start)))
}

export function EventBand({ evidence }: { evidence: PerformanceEvidence }) {
  const locale = useLocale()
  const scan = useMemo(
    () => scanEvents({ start: evidence.start, end: evidence.end, history: evidence.history, cpuSamples: evidence.cpuSamples }),
    [evidence.start, evidence.end, evidence.history, evidence.cpuSamples],
  )

  const missing: string[] = []
  if (!scan.watched.includes('throughput')) {
    missing.push(evidence.interfacesError
      ? t('Throughput was not watched: the interface counters could not be read.')
      : evidence.byteCounters === 'absent'
        ? t('Throughput was not watched: the uplink interface does not report byte counters.')
        : evidence.uplinkName && evidence.interfaceKnown
          ? t('Throughput was not watched: the window has too few samples.')
          : t('Throughput was not watched: the uplink interface is not known.'))
  }
  if (!scan.watched.includes('cpu')) {
    missing.push(evidence.cpuError
      ? t('CPU was not watched: host metrics could not be read.')
      : t('CPU was not watched: the window has too few samples.'))
  }
  if (!scan.watched.includes('discards')) {
    missing.push(evidence.interfacesError
      ? t('Discards were not watched: the interface counters could not be read.')
      : evidence.discardCounter === 'absent'
        ? t('Discards were not watched: the uplink interface does not report a discard counter.')
        : evidence.uplinkName && evidence.interfaceKnown
          ? t('Discards were not watched: the window has too few samples.')
          : t('Discards were not watched: the uplink interface is not known.'))
  }

  const quiet = scan.events.length === 0

  return <OverviewCard bodyClassName="gap-2">
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <Heading as="h2" size="sm">{t('Events in this window')}</Heading>
      <span className="text-xs text-muted-foreground/80">{t('Derived')}</span>
      <div className="ml-auto flex flex-wrap items-center gap-x-3 gap-y-1">
        <LegendItem kind="throughput" label={t('Throughput fall')} />
        <LegendItem kind="cpu" label={t('CPU busy')} />
        <LegendItem kind="discards" label={t('Input discards')} />
      </div>
    </div>

    {quiet
      ? <div className={cn('flex min-h-9 items-center rounded-lg bg-muted/40 py-2 text-xs text-muted-foreground', PLOT_INSET)}>
          <p className="w-full text-center">
            {scan.watched.length === 0
              ? t('Not enough samples in this window to detect events.')
              : t('The window was quiet: no threshold was crossed.')}
          </p>
        </div>
      : <div className={cn('rounded-lg bg-muted/40', PLOT_INSET)}>
          <div className="relative h-9">
            {scan.events.map((event, index) => {
              const sentence = eventSentence(event)
              const at = clock(event.at, locale)
              return <Tooltip key={`${event.kind}-${event.at}-${index}`}>
                <TooltipTrigger
                  delay={0}
                  aria-label={`${sentence}, ${at}`}
                  style={{ left: `${fraction(event.at, evidence.start, evidence.end) * 100}%` }}
                  className={cn(
                    'absolute flex h-3 w-5 -translate-x-1/2 items-center justify-center rounded-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
                    ROW_TOP[event.kind],
                  )}
                >
                  <MarkShape kind={event.kind} />
                </TooltipTrigger>
                <TooltipContent className="block max-w-64 leading-relaxed">
                  <span className="block">{sentence}</span>
                  <span className="mt-0.5 block text-background/70 tabular-nums">{at}</span>
                </TooltipContent>
              </Tooltip>
            })}
          </div>
        </div>}

    <div className="space-y-1 pt-1 text-xs leading-4 text-muted-foreground">
      <p>{t('A mark fires when download or upload falls below half the window median after a reading at or above it, CPU reaches {cpu}%, or input discards reach {discards}/s.', { cpu: BUSY_CPU, discards: DISCARD_BUSY })}</p>
      {!quiet && <p>{t('A mark reports the measured change only; it names no cause.')}</p>}
      {missing.map(note => <p key={note}>{note}</p>)}
    </div>
  </OverviewCard>
}
