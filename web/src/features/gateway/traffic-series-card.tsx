import { t, useLocale } from '@/lib/i18n'

import { Card } from '@/components/ui/card'
import { Heading } from '@/components/ui/heading'
import { Skeleton } from '@/components/ui/skeleton'
import { formatBytes, formatRate } from '@/lib/utils'

import { useActivitySeries } from './queries'

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
          <span className="text-[1.375rem] leading-8 font-semibold tracking-[-0.02em] tabular-nums">
            {series.isPending ? <Skeleton className="inline-block h-6 w-24 align-middle" />
              : buckets.length === 0 ? '—' : formatBytes(total)}
          </span>
          <span className="text-xs text-muted-foreground">{t('down + up')}</span>
        </div>

        {buckets.length > 0
          ? <Plot buckets={buckets} />
          : <div className="grid h-[136px] place-items-center rounded-lg border border-dashed border-border text-xs text-muted-foreground">
              {series.isError ? t('The traffic history could not be read.') : t('No traffic was counted in this window.')}
            </div>}

        <p className="mt-2 text-[0.6875rem] leading-4 text-muted-foreground">
          {t('Counted for clients this router can name. Traffic it cannot attribute is not in this chart, so the link carried at least this much.')}
        </p>
      </div>
    </Card>
  )
}

/** Bytes per half hour, two directions, on one scale so their heights compare. */
function Plot({ buckets }: { buckets: { at: string; bytes: { up: number; down: number } }[] }) {
  const width = 640
  const height = 136
  const top = 6
  const bottom = height - 14
  const step = buckets.length > 1 ? width / (buckets.length - 1) : width
  const max = Math.max(1, ...buckets.map(bucket => Math.max(bucket.bytes.up, bucket.bytes.down)))

  const points = (pick: (bytes: { up: number; down: number }) => number) =>
    buckets.map((bucket, index) => {
      const x = index * step
      const y = bottom - (pick(bucket.bytes) / max) * (bottom - top)
      return `${x.toFixed(1)} ${y.toFixed(1)}`
    })

  const down = points(bytes => bytes.down)
  const up = points(bytes => bytes.up)
  const area = `M0 ${bottom} L${down.join(' L')} L${width} ${bottom} Z`

  const clock = (iso: string) => new Date(iso).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
  const marks = [0, Math.floor(buckets.length / 4), Math.floor(buckets.length / 2), Math.floor((buckets.length * 3) / 4), buckets.length - 1]

  return (
    <>
      <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" className="block h-[136px] w-full" aria-hidden>
        <path d={area} className="fill-receive/10" />
        <polyline points={down.join(' ')} fill="none" strokeWidth={1.6} className="stroke-receive" strokeLinejoin="round" />
        <polyline points={up.join(' ')} fill="none" strokeWidth={1.4} className="stroke-transmit" strokeLinejoin="round" opacity={0.85} />
      </svg>
      <div className="flex justify-between text-[0.6875rem] tabular-nums text-muted-foreground">
        {marks.map(index => <span key={index}>{clock(buckets[index].at)}</span>)}
      </div>
    </>
  )
}
