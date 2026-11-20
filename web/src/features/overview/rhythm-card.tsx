import { Card } from '@/components/ui/card'
import { Heading } from '@/components/ui/heading'
import { Skeleton } from '@/components/ui/skeleton'
import { useActivitySeries } from '@/features/gateway/queries'
import { t, useLocale } from '@/lib/i18n'
import { formatBytes } from '@/lib/utils'

import { trafficRhythm } from './series'

const GIB = 1024 ** 3

function level(value: number, max: number) {
  if (max <= 0) return 0
  const share = value / max
  return share > 0.8 ? 0.9 : share > 0.6 ? 0.7 : share > 0.4 ? 0.5 : share > 0.2 ? 0.3 : 0.14
}

/**
 * When the week is busy. Seven local days down, twenty-four local hours across.
 *
 * This is not a second copy of the 24 h chart: that answers "how much today",
 * this answers "which hours of which days", which is the question that changes
 * a schedule or a backup window. Future hours are gaps, not zeros.
 */
export function RhythmCard() {
  useLocale()
  const series = useActivitySeries(24 * 8)
  const rhythm = trafficRhythm(series.data?.buckets ?? [])
  const peak = rhythm.days[rhythm.peakDayIndex]
  const weekday = (date: Date) => date.toLocaleDateString(undefined, { weekday: 'short' })
  const label = t('Traffic rhythm for the last {days} days, busiest at {hour}:00', { days: 7, hour: rhythm.peakHour })

  return <Card className="gap-0 py-0">
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-4 pt-3.5">
      <Heading as="h2" size="sm">{t('Traffic rhythm')}</Heading>
      <span className="text-xs text-muted-foreground">{t('last {days} days', { days: 7 })}</span>
      <span className="ml-auto text-xs text-muted-foreground tabular-nums">{t('peak {time}', { time: `${String(rhythm.peakHour).padStart(2, '0')}:00` })}</span>
    </div>

    <div className="px-4 pt-3 pb-3.5">
      {series.isPending
        ? <Skeleton className="h-[180px] w-full" />
        : !series.data || rhythm.totalGiB === 0
          ? <div className="grid h-[180px] place-items-center rounded-lg border border-dashed border-border text-xs text-muted-foreground">
              {series.isError ? t('The traffic history could not be read.') : t('Nothing counted yet')}
            </div>
          : <div className="overflow-x-auto pb-1">
              <div className="min-w-0 sm:min-w-[560px]" role="img" aria-label={label}>
                <div className="grid gap-[3px]" style={{ gridTemplateColumns: '2rem repeat(24, minmax(0, 1fr))' }}>
                  <span />
                  {Array.from({ length: 24 }, (_, hour) => <span key={hour} className="text-xs leading-4 text-muted-foreground tabular-nums">
                    {[0, 6, 12, 18].includes(hour) ? String(hour).padStart(2, '0') : ''}
                  </span>)}
                  {rhythm.days.map((day, dayIndex) => [
                    <span key={`${day.key}-label`} className="text-xs leading-[18px] text-muted-foreground">{weekday(day.date)}</span>,
                    ...day.hours.map((value, hour) => {
                      const future = value == null
                      const now = dayIndex === rhythm.nowDay && hour === rhythm.nowHour
                      const backgroundColor = future
                        ? undefined
                        : value === 0
                          ? 'var(--muted)'
                          : `color-mix(in srgb, var(--receive) ${Math.round(level(value, rhythm.maxGiB) * 100)}%, var(--muted))`
                      return <span
                        key={`${day.key}-${hour}`}
                        title={`${weekday(day.date)} ${String(hour).padStart(2, '0')}:00 · ${future ? t('Not yet') : formatBytes(value * GIB)}`}
                        aria-hidden
                        className={`h-[18px] rounded-[3px] ${future ? 'border border-dashed border-border/70' : ''} ${now ? 'ring-1 ring-foreground ring-offset-1 ring-offset-card' : ''}`}
                        style={backgroundColor ? { backgroundColor } : undefined}
                      />
                    }),
                  ])}
                </div>
              </div>
            </div>}

      <div className="mt-3 flex items-center gap-2 text-xs text-muted-foreground">
        <span>{t('Less')}</span>
        {[0.14, 0.3, 0.5, 0.7, 0.9].map(level => <i key={level} className="h-1.5 w-3 rounded-[2px]" style={{ backgroundColor: `color-mix(in srgb, var(--receive) ${level * 100}%, var(--muted))` }} />)}
        <span>{t('More')}</span>
        <span className="ml-auto">{t('GiB per hour')}</span>
      </div>

      <div className="mt-3 flex flex-wrap items-baseline gap-x-3 gap-y-1 border-t border-border pt-2 text-xs text-muted-foreground">
        <span>{t('Week total')}</span>
        <span className="tabular-nums text-foreground">{formatBytes(rhythm.totalGiB * GIB)}</span>
        {peak && <>
          <span className="ml-auto">{t('Peak day')}</span>
          <span className="tabular-nums text-foreground">{weekday(peak.date)} · {formatBytes(peak.totalGiB * GIB)}</span>
        </>}
      </div>
    </div>
  </Card>
}
