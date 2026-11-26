import { useEffect, useRef, useState } from 'react'
import { Area, AreaChart, CartesianGrid, YAxis } from 'recharts'

import { OverviewCard, OverviewCardHeader } from './card'
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/components/ui/chart'
import { MetricValue } from '@/components/ui/metric-value'
import { StatusDot } from '@/components/ui/status'
import type { TrafficView } from '@/features/topology/traffic'
import type { GatewayTraffic } from '@/lib/api-types'
import { t, useLocale } from '@/lib/i18n'
import { formatRate } from '@/lib/utils'

/** How many one-second samples the live plot keeps: one minute. */
const WINDOW = 60

interface Sample { at: number; down: number; up: number }

/** A rate's figure and unit, so the figure can stay the large part. */
function splitUnit(value: string): [string, string?] {
  const at = value.lastIndexOf(' ')
  return at < 0 ? [value] : [value.slice(0, at), value.slice(at + 1)]
}

/**
 * The last minute, kept in the page rather than asked of the router.
 *
 * The overview already polls the counters every second to print the rate; this
 * only remembers what it printed. Nothing is stored and nothing is fetched, so
 * a fresh visit starts with an empty plot that fills from the right, which is
 * honest: there is no minute before the page was opened to show.
 */
function useLiveSamples(flows: TrafficView, sampledAt: number) {
  const [samples, setSamples] = useState<Sample[]>([])
  const last = useRef(0)
  useEffect(() => {
    if (!flows.rated || !sampledAt || sampledAt === last.current) return
    last.current = sampledAt
    const sample = { at: sampledAt, down: flows.total.downRate ?? 0, up: flows.total.upRate ?? 0 }
    setSamples(previous => [...previous, sample].slice(-WINDOW))
  }, [flows, sampledAt])
  return samples
}

/**
 * Throughput now, with the minute that led to it.
 *
 * Both directions are one reading, so they share one card and one plot. The
 * figures carry the meaning and the plot carries the shape; the plot has no
 * axis labels because its scale is a minute of one connection, and a tooltip
 * gives any second's exact rate.
 */
export function LiveCard({ traffic, trafficFailed, flows, sampledAt }: {
  traffic: GatewayTraffic | undefined
  trafficFailed: boolean
  flows: TrafficView
  sampledAt: number
}) {
  useLocale()
  const samples = useLiveSamples(flows, sampledAt)
  const rate = (direction: 'downRate' | 'upRate') =>
    trafficFailed || !flows.rated ? undefined : splitUnit(formatRate(flows.total[direction] ?? 0))
  const down = rate('downRate')
  const up = rate('upRate')

  const note = trafficFailed ? t('Traffic could not be read')
    : !traffic ? t('Checking…')
      : !traffic.counting ? t('Traffic accounting is off')
        : !flows.rated ? t('Waiting for a sample')
          : undefined

  // Left-pad the window so the line grows in from the right edge instead of
  // stretching one sample across the whole card.
  const rows: (Partial<Sample> & { slot: number })[] = Array.from({ length: WINDOW }, (_, slot) => {
    const sample = samples[slot - (WINDOW - samples.length)]
    return sample ? { slot, ...sample } : { slot }
  })
  const peak = samples.reduce((max, sample) => Math.max(max, sample.down, sample.up), 0)

  const config = {
    down: { label: t('Download'), color: 'var(--receive)' },
    up: { label: t('Upload'), color: 'var(--transmit)' },
  } satisfies ChartConfig

  return <OverviewCard bodyClassName="gap-4">
    <OverviewCardHeader
      level="h2"
      title={t('Throughput now')}
      scope={t('last minute')}
      meta={note ?? <span className="inline-flex items-center gap-1.5"><StatusDot tone="ok" />{t('Live')}</span>} />

    <div className="grid grid-cols-2 gap-4 sm:flex sm:gap-10">
      <div className="min-w-0">
        <p className="flex items-center gap-1.5 text-xs leading-4 text-muted-foreground">
          <span aria-hidden className="size-2 rounded-xs bg-receive" />↓ {t('Download')}
        </p>
        <MetricValue value={down?.[0]} unit={down?.[1]} size="lg" className="mt-1 text-4xl leading-10" />
      </div>
      <div className="min-w-0">
        <p className="flex items-center gap-1.5 text-xs leading-4 text-muted-foreground">
          <span aria-hidden className="size-2 rounded-xs bg-transmit" />↑ {t('Upload')}
        </p>
        <MetricValue value={up?.[0]} unit={up?.[1]} size="lg" className="mt-1 text-4xl leading-10" />
      </div>
    </div>

    <div aria-hidden className="relative mt-auto h-[120px] min-w-0">
      {samples.length < 2
        ? <div className="grid h-full place-items-center rounded-lg bg-muted/50 text-xs text-muted-foreground">
            {note ?? t('Collecting the first seconds…')}
          </div>
        : <ChartContainer config={config} className="h-full w-full">
            <AreaChart data={rows} margin={{ top: 4, right: 0, bottom: 0, left: 0 }}>
              <CartesianGrid vertical={false} />
              <YAxis hide domain={[0, Math.max(peak * 1.15, 1)]} />
              <ChartTooltip
                cursor={{ stroke: 'var(--color-border)' }}
                content={<ChartTooltipContent
                  hideLabel
                  formatter={(value, name) => <div className="flex w-full items-center justify-between gap-3">
                    <span className="text-muted-foreground">{config[name as 'down' | 'up'].label}</span>
                    <span className="font-mono font-medium tabular-nums">{formatRate(Number(value))}</span>
                  </div>}
                />}
              />
              <Area type="monotone" dataKey="down" stroke="var(--color-down)" strokeWidth={1.5} fill="var(--color-down)" fillOpacity={0.1} dot={false} connectNulls={false} isAnimationActive={false} />
              <Area type="monotone" dataKey="up" stroke="var(--color-up)" strokeWidth={1.5} fill="var(--color-up)" fillOpacity={0.1} dot={false} connectNulls={false} isAnimationActive={false} />
            </AreaChart>
          </ChartContainer>}
    </div>
  </OverviewCard>
}
