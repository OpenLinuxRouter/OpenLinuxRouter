import { t, useLocale } from '@/lib/i18n'
import { Area, AreaChart, XAxis, YAxis } from 'recharts'

import { Card } from '@/components/ui/card'
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart'
import type { ChartConfig } from '@/components/ui/chart'
import { Heading } from '@/components/ui/heading'
import { Skeleton } from '@/components/ui/skeleton'
import { formatBytes, formatRate } from '@/lib/utils'

import { useActivitySeries } from './queries'

type Bucket = { at: string; bytes: { up: number; down: number } }

/**
 * The day so far, in half-hour steps.
 *
 * Three decisions, all of them about not lying:
 *
 * - It says **counted**, not "throughput". The sampler records nothing it cannot
 *   attribute to a client or a named address, so this is the part of the day that
 *   could be given an owner. The link carried at least this much; it is not a
 *   link counter and the caption says so.
 * - A gap is a gap. Buckets with no traffic are drawn as zero rather than
 *   interpolated across, and a missing range is left missing rather than filled
 *   from the neighbouring bucket.
 * - The peak is a half-hour average, and the caption says "over 30 min" rather
 *   than presenting it as an instantaneous rate the way a live meter would.
 */
export function TrafficSeriesCard({ hours = 24 }: { hours?: number }) {
  useLocale()
  const series = useActivitySeries(hours)
  const buckets = series.data?.buckets ?? []

  const total = buckets.reduce((sum, bucket) => sum + bucket.bytes.up + bucket.bytes.down, 0)
  const peakDown = buckets.reduce((max, bucket) => Math.max(max, bucket.bytes.down), 0)
  const peakUp = buckets.reduce((max, bucket) => Math.max(max, bucket.bytes.up), 0)
  const peak = Math.max(peakDown, peakUp)

  return (
    <Card className="gap-0 py-0">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-4 pt-3.5">
        <Heading as="h2" size="sm">{t('Traffic counted')}</Heading>
        <span className="text-xs text-muted-foreground">{t('last {hours} hours', { hours })}</span>
        <span className="ml-auto text-xs text-muted-foreground tabular-nums">
          {series.isPending ? t('Reading…')
            : series.isError ? t('Unavailable')
              : buckets.length === 0 ? t('Nothing counted yet')
                : t('peak {rate} over 30 min', { rate: formatRate((peak * 8) / 1800) })}
        </span>
      </div>

      <div className="px-4 pt-2 pb-3.5">
        <div className="mb-1 flex items-baseline gap-2">
          <span className="text-xl font-semibold tabular-nums">
            {series.isPending ? <Skeleton className="inline-block h-6 w-24 align-middle" />
              : buckets.length === 0 ? '—' : formatBytes(total)}
          </span>
          <span className="text-xs text-muted-foreground">{t('down + up')}</span>
          {/* The two lines are told apart by name, not by hue alone. */}
          {buckets.length > 0 && (
            <span className="ml-auto flex items-center gap-3 text-xs text-muted-foreground">
              <span className="inline-flex items-center gap-1.5"><span aria-hidden className="h-0.5 w-3 rounded-full bg-receive" />{t('Download')}</span>
              <span className="inline-flex items-center gap-1.5"><span aria-hidden className="h-0.5 w-3 rounded-full bg-transmit" />{t('Upload')}</span>
            </span>
          )}
        </div>

        {buckets.length > 0
          ? <Plot buckets={buckets} />
          : <div className="grid h-[136px] place-items-center rounded-lg border border-dashed border-border text-xs text-muted-foreground">
              {series.isError ? t('The traffic history could not be read.') : t('No traffic was counted in this window.')}
            </div>}

        <p className="mt-2 text-xs leading-4 text-muted-foreground">
          {t('Counted for clients this router can name. Traffic it cannot attribute is not in this chart, so the link carried at least this much.')}
        </p>
      </div>
    </Card>
  )
}

type Row = { clock: string; down: number; up: number }

const clock = (iso: string) => new Date(iso).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })

/** Five clocks: both ends and the quarter points, so the axis reads left to right. */
function marks(rows: Row[]) {
  const at = [0, Math.floor(rows.length / 4), Math.floor(rows.length / 2), Math.floor((rows.length * 3) / 4), rows.length - 1]
  return [...new Set(at.map(index => rows[index].clock))]
}

/** Bytes per half hour, two directions, on one scale so their heights compare. */
function Plot({ buckets }: { buckets: Bucket[] }) {
  const rows: Row[] = buckets.map(bucket => ({ clock: clock(bucket.at), down: bucket.bytes.down, up: bucket.bytes.up }))
  const ticks = marks(rows)

  const config = {
    down: { label: t('Download'), color: 'var(--color-receive)' },
    up: { label: t('Upload'), color: 'var(--color-transmit)' },
  } satisfies ChartConfig

  return (
    // The axis sits inside the same box as the plot, so the frame is the old
    // 136px of plot plus one row of clocks. The plot stays decoration for the
    // accessibility layer the way the old SVG was — the total above and the
    // caption below are the description — so nothing focusable hides in it.
    <div className="h-[158px] w-full">
      <ChartContainer config={config} className="size-full" aria-hidden>
        <AreaChart data={rows} margin={{ top: 8, right: 0, bottom: 0, left: 0 }} accessibilityLayer={false}>
          <XAxis dataKey="clock" ticks={ticks} interval={0} tick={<ClockTick count={ticks.length} />}
            axisLine={false} tickLine={false} height={20} tickMargin={4} />
          {/* One scale for both directions, and no numerals: the shape is the point. */}
          <YAxis hide domain={[0, 'auto']} />
          <ChartTooltip
            cursor={{ stroke: 'var(--color-border)' }}
            content={<ChartTooltipContent indicator="line" labelFormatter={(_, payload) => (payload?.[0]?.payload as Row | undefined)?.clock} formatter={(value, _, item) => (
              <div className="flex w-full items-stretch gap-2">
                <div className="my-0.5 w-1 shrink-0 rounded-[2px]" style={{ backgroundColor: item.color }} />
                <div className="flex flex-1 justify-between gap-3 leading-none">
                  <span className="text-muted-foreground">{config[(item.dataKey as 'down' | 'up') ?? 'down'].label}</span>
                  <span className="font-mono font-medium tabular-nums">{formatBytes(Number(value))}</span>
                </div>
              </div>
            )} />}
          />
          <Area type="linear" dataKey="down" stroke="var(--color-down)" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round"
            fill="var(--color-down)" fillOpacity={0.1} dot={false} activeDot={{ r: 3, strokeWidth: 0 }} isAnimationActive={false} />
          <Area type="linear" dataKey="up" stroke="var(--color-up)" strokeWidth={1.4} strokeLinecap="round" strokeLinejoin="round"
            fill="none" dot={false} activeDot={{ r: 3, strokeWidth: 0 }} isAnimationActive={false} />
        </AreaChart>
      </ChartContainer>
    </div>
  )
}

/**
 * The clocks, flush to both edges of the plot the way the old hand-drawn axis
 * was: a centred first label hangs half its width outside the card.
 */
function ClockTick({ x = 0, y = 0, index = 0, payload, count }: {
  x?: number; y?: number; index?: number; payload?: { value?: string }; count: number
}) {
  const anchor = index === 0 ? 'start' : index === count - 1 ? 'end' : 'middle'
  return <text x={x} y={y} dy={10} textAnchor={anchor} className="fill-muted-foreground text-xs tabular-nums">{payload?.value}</text>
}
