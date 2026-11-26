import { Bar, BarChart, CartesianGrid, ReferenceLine, XAxis, YAxis } from 'recharts'

import { OverviewCard, OverviewCardHeader } from './card'
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/components/ui/chart'
import { MetricValue } from '@/components/ui/metric-value'
import { Skeleton } from '@/components/ui/skeleton'
import { useActivitySeries } from '@/features/gateway/queries'
import { axisScale, formatTick } from '@/lib/chart-ticks'
import { t, useLocale } from '@/lib/i18n'
import { formatBytes } from '@/lib/utils'

import { hourlyTraffic, type HourlyTraffic } from './series'

const GIB = 1024 ** 3

function ClockTick({ x = 0, y = 0, index = 0, payload, count }: {
  x?: number
  y?: number
  index?: number
  payload?: { value?: string }
  count: number
}) {
  const anchor = index === 0 ? 'start' : index === count - 1 ? 'end' : 'middle'
  return <text x={x} y={y} dy={10} textAnchor={anchor} className="fill-muted-foreground text-xs tabular-nums">{payload?.value}</text>
}

/** The 24 h shape: one bar per clock hour, download under upload. */
export function TrafficCard() {
  useLocale()
  const series = useActivitySeries(24)
  const rows = hourlyTraffic(series.data?.buckets ?? []).map(row => ({
    ...row,
    down: row.downGiB,
    up: row.upGiB,
  }))
  const totalBytes = (series.data?.buckets ?? []).reduce((sum, bucket) => sum + bucket.bytes.down + bucket.bytes.up, 0)
  const [total, totalUnit] = formatBytes(totalBytes).split(' ')

  const peak = rows.reduce<(HourlyTraffic & { down: number; up: number }) | undefined>((best, row) => {
    if (!best) return row
    return row.downGiB + row.upGiB > best.downGiB + best.upGiB ? row : best
  }, undefined)
  const peakBytes = peak ? (peak.downGiB + peak.upGiB) * GIB : 0
  // The axis is drawn from the tallest hour, not from the axis' own idea of a
  // round number: a scale computed from rounded labels prints the same number
  // twice (lib/chart-ticks.ts).
  const scale = axisScale(peakBytes / GIB)
  const ticks = rows.length === 0 ? [] : [0, 6, 12, 18, rows.length - 1]
    .map(index => rows[Math.min(index, rows.length - 1)]?.label)
    .filter((label, index, all): label is string => Boolean(label) && all.indexOf(label) === index)

  const config = {
    down: { label: t('Download'), color: 'var(--receive)' },
    up: { label: t('Upload'), color: 'var(--transmit)' },
  } satisfies ChartConfig

  return <OverviewCard>
    <OverviewCardHeader title={t('Traffic')} scope={t('last {hours} hours', { hours: 24 })}
      meta={peak && t('peak {value} at {time}', { value: formatBytes(peakBytes), time: peak.label })} />

    <div className="flex min-w-0 flex-col gap-2">
      <div className="flex flex-wrap items-baseline gap-2">
        <MetricValue value={series.isPending ? undefined : total} unit={series.isPending ? undefined : totalUnit} size="lg" />
        <span className="text-xs text-muted-foreground">{t('down + up')}</span>
        <span className="ml-auto flex items-center gap-3 text-xs text-muted-foreground">
          <span className="inline-flex items-center gap-1.5"><span aria-hidden className="size-2 rounded-xs bg-receive" />{t('Download')}</span>
          <span className="inline-flex items-center gap-1.5"><span aria-hidden className="size-2 rounded-xs bg-transmit" />{t('Upload')}</span>
          <span className="text-muted-foreground/70">{t('GiB per hour')}</span>
        </span>
      </div>

      {series.isPending
        ? <Skeleton className="h-[240px] w-full" />
        : rows.length === 0
          ? <div className="grid h-[240px] place-items-center rounded-lg border border-dashed border-border text-xs text-muted-foreground">
              {series.isError ? t('The traffic history could not be read.') : t('Nothing counted yet')}
            </div>
          : <ChartContainer config={config} className="h-[240px] w-full">
              <BarChart data={rows} margin={{ top: 8, right: 4, bottom: 0, left: -10 }} barCategoryGap="22%">
                <CartesianGrid vertical={false} />
                <XAxis dataKey="label" ticks={ticks} interval={0} tick={<ClockTick count={ticks.length} />} tickLine={false} axisLine={false} tickMargin={8} />
                <YAxis tickLine={false} axisLine={false} width={38} domain={[0, scale.top]} ticks={scale.ticks} tickFormatter={(value: number) => formatTick(value, scale.decimals)} />
                <ReferenceLine y={0} />
                <ChartTooltip
                  cursor={{ fill: 'var(--color-muted)', opacity: 0.35 }}
                  content={<ChartTooltipContent
                    formatter={(value, name) => <div className="flex w-full items-center justify-between gap-3">
                      <span className="text-muted-foreground">{config[name as 'down' | 'up'].label}</span>
                      <span className="font-mono font-medium tabular-nums">{formatBytes(Number(value) * GIB)}</span>
                    </div>}
                  />}
                />
                <Bar dataKey="down" stackId="traffic" fill="var(--color-down)" radius={[0, 0, 2, 2]} maxBarSize={30} isAnimationActive={false} />
                <Bar dataKey="up" stackId="traffic" fill="var(--color-up)" radius={[2, 2, 0, 0]} maxBarSize={30} isAnimationActive={false} />
              </BarChart>
            </ChartContainer>}

      <p className="mt-2 text-xs leading-4 text-muted-foreground">
        {t('Counted for clients this router can name. Traffic it cannot attribute is not in this chart, so the link carried at least this much.')}
      </p>
    </div>
  </OverviewCard>
}
