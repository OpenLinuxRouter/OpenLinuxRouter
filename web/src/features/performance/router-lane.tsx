import { CartesianGrid, Line, LineChart, ReferenceLine, XAxis, YAxis } from 'recharts'

import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/components/ui/chart'
import { Heading } from '@/components/ui/heading'
import { OverviewCard } from '@/features/overview/card'
import { axisScale } from '@/lib/chart-ticks'
import { t, useLocale } from '@/lib/i18n'

import { clock } from './live'
import { LaneEmpty, LegendItem, Readout, TimeTick } from './parts'
import { formatDropRate, formatPercent } from './samples'
import type { PerformanceEvidence } from './use-performance'

/**
 * The router beside the line, on the same clock.
 *
 * CPU is pinned to 0–100 rather than auto-scaled: a 3% idle core drawn across
 * the whole plot would look like a busy router. The drops axis follows the
 * data, but a clean counter window draws a flat line at zero, which is the
 * point of putting the two measurements under a busy link.
 */
export function RouterLane({ evidence }: { evidence: PerformanceEvidence }) {
  const locale = useLocale()
  const cpuPoints = evidence.cpuSamples.filter(sample => sample.at >= evidence.start)
  const dropPoints = evidence.history
    .filter(sample => sample.at >= evidence.start && sample.dropped != null)
    .map(sample => ({ at: sample.at, drops: sample.dropped! }))
  const hasCpuLine = cpuPoints.length >= 2
  const hasDropsLine = dropPoints.length >= 2
  const showChart = hasCpuLine || hasDropsLine

  const points = (() => {
    const merged = new Map<number, { at: number; cpu?: number; drops?: number }>()
    for (const sample of cpuPoints) merged.set(sample.at, { at: sample.at, cpu: sample.pct })
    for (const sample of dropPoints) {
      merged.set(sample.at, { ...(merged.get(sample.at) ?? { at: sample.at }), drops: sample.drops })
    }
    return [...merged.values()].sort((a, b) => a.at - b.at)
  })()

  const dropScale = axisScale(Math.max(0, ...dropPoints.map(point => point.drops)), 3)

  const config = {
    cpu: { label: t('CPU'), color: 'var(--primary)' },
    drops: { label: t('Dropped packets'), color: 'var(--foreground)' },
  } satisfies ChartConfig

  const cpuValue = formatPercent(evidence.cpuPercent == null ? undefined : evidence.cpuPercent / 100)
  const dropValue = formatDropRate(evidence.dropsPerSecond)

  const cpuNote = evidence.cpuError ? t('Host metrics could not be read.')
    : evidence.cpuPending ? t('Reading CPU…')
      : evidence.cpuCores ? t('{count} cores', { count: evidence.cpuCores })
        : undefined

  const dropNote = evidence.interfacesError ? t('The interface counters could not be read.')
    : !evidence.uplinkName ? t('The uplink interface is not known yet, so its counters cannot be read.')
      : evidence.interfacesPending ? t('Reading the interface list…')
        : evidence.linkState === 'missing' ? t('The uplink interface is not present on this machine.')
          : evidence.linkState === 'down' ? t('The uplink interface is down, so throughput cannot be read.')
            : !evidence.interfaceKnown ? t('The uplink interface is not in the interface list.')
              : evidence.dropCounters === 'absent' ? t('This interface does not report drop counters; drops are unavailable, not zero.')
                : evidence.dropsPerSecond == null ? t('Waiting for a second counter reading.')
                  : t('Received and transmitted')

  const empty = evidence.cpuError && evidence.dropCounters !== 'present'
    ? t('CPU metrics and drop counters are unavailable for this window.')
    : t('Waiting for a second reading…')

  return <OverviewCard bodyClassName="gap-3">
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <Heading as="h2" size="sm">{t('Router')}</Heading>
      {evidence.uplinkName && <span className="font-mono text-xs text-muted-foreground">{evidence.uplinkName}</span>}
      {showChart && <div className="ml-auto flex flex-wrap items-center gap-x-3 gap-y-1">
        {hasCpuLine && <LegendItem label={t('CPU')} color="text-primary" />}
        {hasDropsLine && <LegendItem label={t('Dropped packets')} color="text-foreground" />}
      </div>}
    </div>

    <div className="grid grid-cols-2 gap-3">
      <Readout label={t('CPU')} value={cpuValue} provenance={cpuValue == null ? undefined : t('Measured')} note={cpuNote} />
      <Readout label={t('Dropped packets')} value={dropValue} unit={t('packets/s')} provenance={dropValue == null ? undefined : t('Derived')} note={dropNote} />
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
                sample, so a drops-only window still aligns with the link lane. */}
            <YAxis yAxisId="cpu" width={58} tickLine={false} axisLine={false}
              domain={[0, 100]} ticks={[0, 25, 50, 75, 100]}
              tick={hasCpuLine ? undefined : false}
              tickFormatter={(value: number) => `${value}%`} />
            <YAxis yAxisId="drops" orientation="right" width={40} tickLine={false} axisLine={false}
              domain={[0, dropScale.top]} ticks={hasDropsLine ? dropScale.ticks : []}
              tick={hasDropsLine ? undefined : false}
              tickFormatter={(value: number) => formatDropRate(value) ?? ''} />
            <ChartTooltip content={<ChartTooltipContent
              labelFormatter={(value) => clock(Number(value), locale)}
              formatter={(value, name) => {
                const key = name as keyof typeof config
                return <div className="flex w-full items-center justify-between gap-3">
                  <span className="text-muted-foreground">{config[key]?.label}</span>
                  <span className="font-mono font-medium tabular-nums">
                    {key === 'cpu' ? `${Number(value).toFixed(1)}%` : `${formatDropRate(Number(value))} /s`}
                  </span>
                </div>
              }}
            />} />
            <ReferenceLine yAxisId={hasCpuLine ? 'cpu' : 'drops'} y={0} />
            {hasCpuLine && <Line yAxisId="cpu" type="linear" dataKey="cpu" stroke="var(--color-cpu)"
              strokeWidth={1.5} dot={false} connectNulls isAnimationActive={false} />}
            {hasDropsLine && <Line yAxisId="drops" type="linear" dataKey="drops" stroke="var(--color-drops)"
              strokeWidth={1.5} strokeOpacity={0.6} dot={false} connectNulls isAnimationActive={false} />}
          </LineChart>
        </ChartContainer>
      : <LaneEmpty>{empty}</LaneEmpty>}

    <div className="border-t border-border pt-2 text-xs leading-4 text-muted-foreground">
      <p>{t('CPU is measured; dropped packets are derived from the uplink counter difference.')}</p>
    </div>
  </OverviewCard>
}
