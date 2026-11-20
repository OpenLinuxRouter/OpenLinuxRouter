import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from 'recharts'
import { Link } from 'react-router'

import { OverviewCard } from './card'
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/components/ui/chart'
import { Heading } from '@/components/ui/heading'
import { MetricValue } from '@/components/ui/metric-value'
import { Skeleton } from '@/components/ui/skeleton'
import { useGatewayLatency } from '@/features/gateway/queries'
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
  const current = latency.data?.milliseconds
  const measuring = latency.data?.state === 'measuring'

  const config = {
    ms: { label: t('Latency'), color: 'var(--foreground)' },
  } satisfies ChartConfig

  return <OverviewCard>
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <Heading as="h2" size="sm"><Link to="/latency" className="underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-ring">{t('Internet health')}</Link></Heading>
      <span className="text-xs text-muted-foreground">{t('today')}{target ? ` · ${target.name}` : ''}</span>
      <span className="ml-auto text-xs text-muted-foreground tabular-nums">{latency.data?.state === 'unreachable' ? t('No response') : measuring ? t('Measuring…') : current == null ? t('Unavailable') : `${Math.round(current)} ms`}</span>
    </div>

    <div className="flex min-w-0 flex-col gap-2">
      <div className="flex flex-wrap items-baseline gap-2">
        <MetricValue value={current == null ? undefined : Math.round(current)} unit={current == null ? undefined : 'ms'} size="lg" />
        <span className="text-xs text-muted-foreground">{t('DNS lookup')} {latency.data?.dns_milliseconds == null ? '—' : `${Math.round(latency.data.dns_milliseconds)} ms`}</span>
      </div>

      {latency.isPending
        ? <Skeleton className="h-[132px] w-full" />
        : !today || today.samples === 0
          ? <div className="grid h-[132px] place-items-center rounded-lg border border-dashed border-border text-xs text-muted-foreground">
              {latency.isError ? t('Could not read measurements') : t('No measurements today')}
            </div>
          : <ChartContainer config={config} className="h-[132px] w-full">
              <AreaChart data={today.points} margin={{ top: 6, right: 8, bottom: 0, left: 0 }}>
                <CartesianGrid vertical={false} strokeDasharray="2 4" />
                <XAxis dataKey="label" ticks={['00:00', '06:00', '12:00', '18:00', '23:00']} interval={0} tick={<ClockTick count={5} />} tickLine={false} axisLine={false} tickMargin={8} />
                <YAxis width={34} tickLine={false} axisLine={false} domain={[0, 'auto']} allowDecimals={false} />
                <ChartTooltip content={<ChartTooltipContent
                  formatter={(value) => `${Math.round(Number(value))} ms`}
                  labelFormatter={(_, payload) => (payload?.[0]?.payload as { label?: string } | undefined)?.label}
                />} />
                <Area type="linear" dataKey="ms" stroke="var(--color-ms)" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" fill="var(--color-ms)" fillOpacity={0.08} dot={false} connectNulls={false} isAnimationActive={false} />
              </AreaChart>
            </ChartContainer>}

      <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1 border-t border-border pt-2 text-xs text-muted-foreground">
        <span>{t('p95')} <span className="tabular-nums text-foreground">{today?.p95 == null ? '—' : `${Math.round(today.p95)} ms`}</span></span>
        <span>{t('Max')} <span className="tabular-nums text-foreground">{today?.max == null ? '—' : `${Math.round(today.max)} ms`}</span></span>
        <span className="ml-auto">{t('{count} hours measured', { count: today?.samples ?? 0 })}</span>
      </div>
      {pinned.length > 0 && <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-border pt-2 text-xs text-muted-foreground">
        {pinned.map(site => <Link key={site.name} to="/latency" className="inline-flex items-center gap-1.5 underline-offset-4 hover:text-foreground hover:underline focus-visible:outline-2 focus-visible:outline-ring">
          {site.name}
          <span className="tabular-nums text-foreground">{site.milliseconds == null ? '—' : `${Math.round(site.milliseconds)} ms`}</span>
        </Link>)}
      </div>}
    </div>
  </OverviewCard>
}
