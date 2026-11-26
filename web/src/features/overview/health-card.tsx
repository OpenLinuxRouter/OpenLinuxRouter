import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from 'recharts'
import { Link } from 'react-router'

import { OverviewCard, OverviewCardHeader, OverviewStat } from './card'
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/components/ui/chart'
import { Skeleton } from '@/components/ui/skeleton'
import { useGatewayLatency } from '@/features/gateway/queries'
import { axisScale, formatTick } from '@/lib/chart-ticks'
import { t, useLocale } from '@/lib/i18n'

import { hourlyLatency, latencyDay } from '@/features/gateway/hourly-latency'

import { summarizeLatency } from './latency'

/** Today's latency, across every probe that reported an hour. */
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

export function HealthCard() {
  useLocale()
  const latency = useGatewayLatency()
  const day = latency.data ? latencyDay(latency.data.local_time) : undefined
  // The figure beside the chart is the selected target's latest sample, so the
  // chart has to be that target's day too. Averaging every probe would put the
  // fast and slow sites in one line and disagree with the number above it.
  const target = latency.data?.custom.find(site => site.url === latency.data?.target_url)
    ?? latency.data?.custom.find(site => site.name === latency.data?.target)
    ?? latency.data?.custom[0]
  const pinned = latency.data?.custom.filter(site => site.pinned) ?? []
  const today = latency.data && day && target
    ? summarizeLatency([hourlyLatency(target, day)])
    : undefined
  // One scale for the day's line, chosen so the labels are evenly spaced round
  // milliseconds rather than whatever the tallest spike happened to be
  // (lib/chart-ticks.ts).
  const scale = axisScale(today?.max ?? 0, 4)
  const current = latency.data?.milliseconds
  const measuring = latency.data?.state === 'measuring'

  const config = {
    ms: { label: t('Latency'), color: 'var(--primary)' },
  } satisfies ChartConfig

  const fmt = (value?: number | null) => value == null ? '—' : `${Math.round(value)} ms`

  return <OverviewCard>
    <OverviewCardHeader title={t('Internet health')} to="/latency"
      scope={`${t('today')}${target ? ` · ${target.name}` : ''}`}
      meta={t('{count} hours measured', { count: today?.samples ?? 0 })} />

    <div className="grid grid-cols-3 gap-3 rounded-lg bg-muted/50 px-3 py-2">
      <OverviewStat label={t('Now')} value={latency.data?.state === 'unreachable' ? t('No response') : measuring ? '…' : current == null ? '—' : current < 1 ? '<1 ms' : fmt(current)} />
      <OverviewStat label={t('p95')} value={fmt(today?.p95)} />
      <OverviewStat label={t('Max')} value={fmt(today?.max)} />
    </div>

    <div className="flex min-w-0 flex-1 flex-col gap-2">
      {latency.isPending
        ? <Skeleton className="h-[140px] w-full" />
        : !today || today.samples === 0
          ? <div className="grid h-[140px] place-items-center rounded-lg border border-dashed border-border text-xs text-muted-foreground">
              {latency.isError ? t('Could not read measurements') : t('No measurements today')}
            </div>
          : <ChartContainer config={config} className="h-[140px] w-full">
              <AreaChart data={today.points} margin={{ top: 6, right: 8, bottom: 0, left: 0 }}>
                <CartesianGrid vertical={false} />
                <XAxis dataKey="label" ticks={['00:00', '06:00', '12:00', '18:00', '23:00']} interval={0} tick={<ClockTick count={5} />} tickLine={false} axisLine={false} tickMargin={8} />
                <YAxis width={34} tickLine={false} axisLine={false} domain={[0, scale.top]} ticks={scale.ticks} tickFormatter={(value: number) => formatTick(value, scale.decimals)} />
                <ChartTooltip content={<ChartTooltipContent
                  formatter={(value) => `${Math.round(Number(value))} ms`}
                  labelFormatter={(_, payload) => (payload?.[0]?.payload as { label?: string } | undefined)?.label}
                />} />
                <Area type="linear" dataKey="ms" stroke="var(--color-ms)" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" fill="var(--color-ms)" fillOpacity={0.1} dot={false} connectNulls={false} isAnimationActive={false} />
              </AreaChart>
            </ChartContainer>}

      {pinned.length > 0 && <div className="mt-auto flex flex-wrap gap-1.5 border-t border-border pt-3">
        {pinned.map(site => <Link key={site.name} to="/latency" className="inline-flex h-7 items-center gap-1.5 rounded-md bg-muted/60 px-2 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring">
          {site.name}
          <span className="font-medium tabular-nums text-foreground">{fmt(site.milliseconds)}</span>
        </Link>)}
      </div>}
    </div>
  </OverviewCard>
}
