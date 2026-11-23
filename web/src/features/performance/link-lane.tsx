import { CartesianGrid, Line, LineChart, ReferenceLine, XAxis, YAxis } from 'recharts'

import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/components/ui/chart'
import { Heading } from '@/components/ui/heading'
import { OverviewCard } from '@/features/overview/card'
import { axisScale } from '@/lib/chart-ticks'
import { t, useLocale } from '@/lib/i18n'
import { formatRate } from '@/lib/utils'

import { clock, when, WINDOW_MS } from './live'
import { LaneEmpty, LegendItem, Readout, TimeTick } from './parts'
import { formatPercent, linkPoints } from './samples'
import type { PerformanceEvidence } from './use-performance'

const WINDOW_MINUTES = Math.round(WINDOW_MS / 60_000)

function rateParts(bytesPerSecond: number | undefined) {
  if (bytesPerSecond == null) return {}
  const text = formatRate(bytesPerSecond)
  const at = text.lastIndexOf(' ')
  return at < 0 ? { value: text } : { value: text.slice(0, at), unit: text.slice(at + 1) }
}

/**
 * The line, on the same clock as the router below it.
 *
 * Throughput comes from the uplink's NIC byte counters, capacity from the
 * measured baseline, and latency from the existing probe. The dashed line is
 * drawn only from that baseline; the virtio NIC reports no link speed, so there
 * is no second, inferred capacity to fall back to.
 */
export function LinkLane({ evidence }: { evidence: PerformanceEvidence }) {
  const locale = useLocale()
  const points = linkPoints(evidence.history, evidence.latencyPoints, evidence.start)
  const showChart = evidence.history.length >= 2
  const latencyLine = evidence.latencyPoints.length > 0

  const rateMax = Math.max(
    0,
    ...evidence.history.flatMap(sample => [sample.rx, sample.tx]),
    ...(evidence.downloadCapacityBps == null ? [] : [evidence.downloadCapacityBps / 8]),
    ...(evidence.uploadCapacityBps == null ? [] : [evidence.uploadCapacityBps / 8]),
  )
  const rateScale = axisScale(rateMax, 4)
  const latencyMax = Math.max(0, ...evidence.latencyPoints.map(point => point.ms))
  const latencyScale = axisScale(latencyMax, 3)

  // On the uplink, receive is download and transmit is upload.
  const download = rateParts(evidence.downloadRate)
  const upload = rateParts(evidence.uploadRate)

  const config = {
    down: { label: t('Download'), color: 'var(--receive)' },
    up: { label: t('Upload'), color: 'var(--transmit)' },
    latency: { label: t('Latency'), color: 'var(--foreground)' },
    capacity: { label: t('Capacity'), color: 'var(--muted-foreground)' },
  } satisfies ChartConfig

  const measuredBaseline = evidence.downloadCapacityBps != null || evidence.uploadCapacityBps != null
  const capacityNote = (() => {
    if (evidence.capacityPending) return t('Reading the capacity baseline…')
    if (!evidence.capacity) return t('No capacity baseline. The capacity line and utilisation are hidden.')
    if (evidence.capacity.state === 'warming') {
      return evidence.coverage
        ? t(evidence.coverage.key, { count: evidence.coverage.count })
        : t('The capacity baseline is provisional.')
    }
    const asOf = evidence.baselineAsOf ? when(evidence.baselineAsOf, locale) : undefined
    return evidence.baselineSamples != null && asOf
      ? t('Capacity measured from {count} samples; baseline updated {time}.', { count: evidence.baselineSamples, time: asOf })
      : t('Capacity measured from the published baseline.')
  })()

  const latencyNote = (() => {
    if (evidence.latencyError) return t('The latency probe could not be read.')
    if (evidence.latencyPending) return t('Reading latency samples…')
    if (latencyLine) return t('Latency points are the selected probe’s measured samples in this window.')
    if (evidence.latencyAvailable) {
      return t('No latency sample in the last {count} minutes; the probe runs on its own schedule.', { count: WINDOW_MINUTES })
    }
    return undefined
  })()

  const empty = (() => {
    if (evidence.interfacesError) return t('The interface counters could not be read.')
    if (!evidence.uplinkName) return t('The uplink interface is not known yet, so its counters cannot be read.')
    if (evidence.interfacesPending) return t('Reading the interface list…')
    if (evidence.linkState === 'missing') return t('The uplink interface is not present on this machine.')
    if (evidence.linkState === 'down') return t('The uplink interface is down, so throughput cannot be read.')
    if (!evidence.interfaceKnown) return t('The uplink interface is not in the interface list.')
    if (evidence.byteCounters === 'absent') return t('This interface does not report byte counters, so throughput cannot be charted.')
    return t('Waiting for a second counter reading…')
  })()

  return <OverviewCard bodyClassName="gap-3">
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <Heading as="h2" size="sm">{t('Link')}</Heading>
      {evidence.uplinkName
        ? <span className="font-mono text-xs text-muted-foreground">{evidence.uplinkName}</span>
        : <span className="text-xs text-muted-foreground">{t('Uplink interface unknown')}</span>}
      {showChart && <div className="ml-auto flex flex-wrap items-center gap-x-3 gap-y-1">
        <LegendItem label={t('Download')} color="text-receive" />
        <LegendItem label={t('Upload')} color="text-transmit" />
        {latencyLine && <LegendItem label={t('Latency')} color="text-foreground" />}
        {measuredBaseline && <LegendItem label={t(evidence.capacity?.state === 'warming' ? 'Capacity (provisional)' : 'Capacity')} color="text-muted-foreground" dashed />}
      </div>}
    </div>

    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
      <Readout label={t('Download')} value={download.value} unit={download.unit} provenance={download.value == null ? undefined : t('Derived')} />
      <Readout label={t('Upload')} value={upload.value} unit={upload.unit} provenance={upload.value == null ? undefined : t('Derived')} />
      <div className="col-span-2 min-w-0 sm:col-span-1">
        <Readout
          label={t('Utilisation')}
          provenance={measuredBaseline && (evidence.downloadRate != null || evidence.uploadRate != null) ? t('Inferred') : undefined}
          value={<span className="inline-flex flex-wrap items-baseline gap-x-3">
            <span className="text-receive">↓ {formatPercent(evidence.downloadUtil) ?? '—'}</span>
            <span className="text-transmit">↑ {formatPercent(evidence.uploadUtil) ?? '—'}</span>
          </span>}
          note={evidence.capacityPending
            ? t('Reading the capacity baseline…')
            : evidence.downloadRate == null && evidence.uploadRate == null
              ? t('Waiting for a throughput reading.')
              : measuredBaseline
                ? t('Measured capacity {download} down, {upload} up', {
                    download: evidence.downloadCapacityBps == null ? '—' : formatRate(evidence.downloadCapacityBps / 8),
                    upload: evidence.uploadCapacityBps == null ? '—' : formatRate(evidence.uploadCapacityBps / 8),
                  })
                : t('No capacity baseline')}
        />
      </div>
    </div>

    {showChart
      ? <ChartContainer config={config} className="h-[240px] w-full" aria-hidden>
          {/* Both lanes reserve the same left and right axis slots, so the plot
              area — and therefore every timestamp — starts at the same pixel. */}
          <LineChart data={points} margin={{ top: 8, right: 4, bottom: 0, left: -6 }}>
            <CartesianGrid vertical={false} />
            <XAxis dataKey="at" type="number" domain={[evidence.start, evidence.end]} ticks={evidence.ticks}
              tick={<TimeTick start={evidence.start} end={evidence.end} />} tickLine={false} axisLine={false} tickMargin={8} />
            <YAxis yAxisId="rate" width={58} tickLine={false} axisLine={false}
              domain={[0, rateScale.top]} ticks={rateScale.ticks}
              tickFormatter={(value: number) => formatRate(value)} />
            {/* Kept mounted even without a sample: the lane below reserves the
                same slot, which is what makes the two plots line up. */}
            <YAxis yAxisId="latency" orientation="right" width={40} tickLine={false} axisLine={false}
              domain={[0, latencyScale.top]} ticks={latencyLine ? latencyScale.ticks : []}
              tick={latencyLine ? undefined : false}
              tickFormatter={(value: number) => `${Math.round(value)} ms`} />
            <ChartTooltip content={<ChartTooltipContent
              labelFormatter={(value) => clock(Number(value), locale)}
              formatter={(value, name) => {
                const key = name as keyof typeof config
                return <div className="flex w-full items-center justify-between gap-3">
                  <span className="text-muted-foreground">{config[key]?.label}</span>
                  <span className="font-mono font-medium tabular-nums">
                    {key === 'latency' ? `${Math.round(Number(value))} ms` : formatRate(Number(value))}
                  </span>
                </div>
              }}
            />} />
            {evidence.downloadCapacityBps != null && <ReferenceLine yAxisId="rate" y={evidence.downloadCapacityBps / 8}
              stroke="var(--color-capacity)" strokeDasharray="4 4" />}
            {evidence.uploadCapacityBps != null && evidence.uploadCapacityBps !== evidence.downloadCapacityBps && <ReferenceLine yAxisId="rate" y={evidence.uploadCapacityBps / 8}
              stroke="var(--color-capacity)" strokeDasharray="4 4" />}
            <Line yAxisId="rate" type="linear" dataKey="down" stroke="var(--color-down)" strokeWidth={1.5}
              dot={false} connectNulls={false} isAnimationActive={false} />
            <Line yAxisId="rate" type="linear" dataKey="up" stroke="var(--color-up)" strokeWidth={1.5}
              dot={false} connectNulls={false} isAnimationActive={false} />
            {latencyLine && <Line yAxisId="latency" type="linear" dataKey="latency" stroke="var(--color-latency)"
              strokeWidth={1} strokeOpacity={0.55} dot={false} connectNulls isAnimationActive={false} />}
          </LineChart>
        </ChartContainer>
      : <LaneEmpty>{empty}</LaneEmpty>}

    <div className="space-y-1 border-t border-border pt-2 text-xs leading-4 text-muted-foreground">
      <p>{t('Throughput is derived from the uplink interface counters, not from forwarded client traffic.')}</p>
      <p>{capacityNote}</p>
      {latencyNote && <p>{latencyNote}</p>}
    </div>
  </OverviewCard>
}
