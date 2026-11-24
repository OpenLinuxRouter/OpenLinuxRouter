import { CartesianGrid, Line, LineChart, ReferenceLine, XAxis, YAxis } from 'recharts'

import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/components/ui/chart'
import { Heading } from '@/components/ui/heading'
import { OverviewCard } from '@/features/overview/card'
import { axisScale } from '@/lib/chart-ticks'
import { t, useLocale } from '@/lib/i18n'

import { clock } from './live'
import { LaneEmpty, LegendItem, Readout, TimeTick } from './parts'
import { formatFrameRate, formatPercent, formatShare } from './samples'
import type { CounterState, PerformanceEvidence } from './use-performance'

/**
 * The router beside the line, on the same clock.
 *
 * The card reports the three input counters RFC 2863 and the kernel keep
 * apart, because they are the three that are fixed differently: an error
 * never arrived intact, a discard arrived and was not processed, and a miss
 * never left the device's buffers. A fused "dropped packets" figure answers
 * none of those questions, which is why this card does not print one.
 *
 * CPU is pinned to 0–100 rather than auto-scaled: a 3% idle core drawn across
 * the whole plot would look like a busy router.
 */
export function RouterLane({ evidence }: { evidence: PerformanceEvidence }) {
  const locale = useLocale()
  const cpuPoints = evidence.cpuSamples.filter(sample => sample.at >= evidence.start)
  const discardPoints = evidence.history
    .filter(sample => sample.at >= evidence.start && sample.discards != null)
    .map(sample => ({ at: sample.at, discards: sample.discards! }))
  const hasCpuLine = cpuPoints.length >= 2
  const hasDiscardLine = discardPoints.length >= 2
  const showChart = hasCpuLine || hasDiscardLine

  const points = (() => {
    const merged = new Map<number, { at: number; cpu?: number; discards?: number }>()
    for (const sample of cpuPoints) merged.set(sample.at, { at: sample.at, cpu: sample.pct })
    for (const sample of discardPoints) {
      merged.set(sample.at, { ...(merged.get(sample.at) ?? { at: sample.at }), discards: sample.discards })
    }
    return [...merged.values()].sort((a, b) => a.at - b.at)
  })()

  const discardScale = axisScale(Math.max(0, ...discardPoints.map(point => point.discards)), 3)

  const config = {
    cpu: { label: t('CPU'), color: 'var(--primary)' },
    discards: { label: t('Input discards'), color: 'var(--foreground)' },
  } satisfies ChartConfig

  const cpuValue = formatPercent(evidence.cpuPercent == null ? undefined : evidence.cpuPercent / 100)
  const discardValue = formatFrameRate(evidence.discardsPerSecond)
  const errorValue = formatFrameRate(evidence.errorsPerSecond)
  const missedValue = formatFrameRate(evidence.missedPerSecond)
  const share = formatShare(evidence.discardShare)

  const cpuNote = evidence.cpuError ? t('Host metrics could not be read.')
    : evidence.cpuPending ? t('Reading CPU…')
      : evidence.cpuCores ? t('{count} cores', { count: evidence.cpuCores })
        : undefined

  /**
   * Why a figure is missing when it is, and what it counts when it is not.
   * An interface that publishes no counter is not an interface dropping
   * nothing, so the two cases never share a sentence.
   */
  const counterNote = (state: CounterState, value: number | undefined, counted: string): string => {
    if (evidence.interfacesError) return t('The interface counters could not be read.')
    if (!evidence.uplinkName) return t('The uplink interface is not known yet, so its counters cannot be read.')
    if (evidence.interfacesPending) return t('Reading the interface list…')
    if (evidence.linkState === 'missing') return t('The uplink interface is not present on this machine.')
    if (evidence.linkState === 'down') return t('The uplink interface is down, so its counters cannot be read.')
    if (state === 'absent') return t('This interface does not report it; it is unavailable, not zero.')
    if (value == null) return t('Waiting for a second counter reading.')
    return counted
  }

  const discardNote = counterNote(evidence.discardCounter, evidence.discardsPerSecond,
    share ? t('{share} of received frames', { share }) : t('Received, and discarded without an error.'))
  const errorNote = counterNote(evidence.errorCounter, evidence.errorsPerSecond,
    t('Arrived with errors, so not deliverable.'))
  const missedNote = counterNote(evidence.missedCounter, evidence.missedPerSecond,
    t('Dropped by the device for lack of buffer space.'))

  const empty = evidence.cpuError && evidence.discardCounter !== 'present'
    ? t('CPU metrics and discard counters are unavailable for this window.')
    : t('Waiting for a second reading…')

  return <OverviewCard bodyClassName="gap-3">
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <Heading as="h2" size="sm">{t('Router')}</Heading>
      {evidence.uplinkName && <span className="font-mono text-xs text-muted-foreground">{evidence.uplinkName}</span>}
      {showChart && <div className="ml-auto flex flex-wrap items-center gap-x-3 gap-y-1">
        {hasCpuLine && <LegendItem label={t('CPU')} color="text-primary" />}
        {hasDiscardLine && <LegendItem label={t('Input discards')} color="text-foreground" />}
      </div>}
    </div>

    <div className="grid grid-cols-2 gap-3">
      <Readout label={t('CPU')} value={cpuValue} provenance={cpuValue == null ? undefined : t('Measured')} note={cpuNote} />
      <Readout label={t('Input discards')} value={discardValue} unit={t('packets/s')}
        provenance={discardValue == null ? undefined : t('Derived')} note={discardNote} />
      <Readout label={t('Input errors')} value={errorValue} unit={t('packets/s')}
        provenance={errorValue == null ? undefined : t('Derived')} note={errorNote} />
      <Readout label={t('Input missed')} value={missedValue} unit={t('packets/s')}
        provenance={missedValue == null ? undefined : t('Derived')} note={missedNote} />
    </div>

    {showChart
      ? <ChartContainer config={config} className="h-[240px] w-full" aria-hidden>
          {/* Same axis slots as the link lane: the shared window only means
              something if both plots put the same time at the same pixel. */}
          <LineChart data={points} margin={{ top: 8, right: 4, bottom: 0, left: -6 }}>
            <CartesianGrid vertical={false} />
            <XAxis dataKey="at" type="number" domain={[evidence.start, evidence.end]} ticks={evidence.ticks}
              tick={<TimeTick start={evidence.start} end={evidence.end} />} tickLine={false} axisLine={false} tickMargin={8} />
            {/* Both slots stay mounted whether or not their series has a
                sample, so a discards-only window still aligns with the link lane. */}
            <YAxis yAxisId="cpu" width={58} tickLine={false} axisLine={false}
              domain={[0, 100]} ticks={[0, 25, 50, 75, 100]}
              tick={hasCpuLine ? undefined : false}
              tickFormatter={(value: number) => `${value}%`} />
            <YAxis yAxisId="discards" orientation="right" width={40} tickLine={false} axisLine={false}
              domain={[0, discardScale.top]} ticks={hasDiscardLine ? discardScale.ticks : []}
              tick={hasDiscardLine ? undefined : false}
              tickFormatter={(value: number) => formatFrameRate(value) ?? ''} />
            <ChartTooltip content={<ChartTooltipContent
              labelFormatter={(value) => clock(Number(value), locale)}
              formatter={(value, name) => {
                const key = name as keyof typeof config
                return <div className="flex w-full items-center justify-between gap-3">
                  <span className="text-muted-foreground">{config[key]?.label}</span>
                  <span className="font-mono font-medium tabular-nums">
                    {key === 'cpu' ? `${Number(value).toFixed(1)}%` : `${formatFrameRate(Number(value))} /s`}
                  </span>
                </div>
              }}
            />} />
            <ReferenceLine yAxisId={hasCpuLine ? 'cpu' : 'discards'} y={0} />
            {hasCpuLine && <Line yAxisId="cpu" type="linear" dataKey="cpu" stroke="var(--color-cpu)"
              strokeWidth={1.5} dot={false} connectNulls isAnimationActive={false} />}
            {hasDiscardLine && <Line yAxisId="discards" type="linear" dataKey="discards" stroke="var(--color-discards)"
              strokeWidth={1.5} strokeOpacity={0.6} dot={false} connectNulls isAnimationActive={false} />}
          </LineChart>
        </ChartContainer>
      : <LaneEmpty>{empty}</LaneEmpty>}

    <div className="border-t border-border pt-2 text-xs leading-4 text-muted-foreground">
      <p>{t('Counters follow RFC 2863: errors could not be delivered, discards were received and not processed. Linux has no per-interface unknown-protocol counter (ifInUnknownProtos), so frames for a protocol nothing implements are counted as discards.')}</p>
    </div>
  </OverviewCard>
}
