import { OverviewCard } from './card'
import { Skeleton } from '@/components/ui/skeleton'
import { Heading } from '@/components/ui/heading'
import { useHostMetrics } from '@/features/system/queries'
import { t, useLocale } from '@/lib/i18n'
import { formatBytes } from '@/lib/utils'

function percent(used: number | null | undefined, total: number | null | undefined) {
  if (used == null || total == null || total <= 0) return undefined
  return Math.max(0, Math.min(100, (used / total) * 100))
}

function Meter({ value }: { value?: number }) {
  return <div className="h-1 w-full overflow-hidden rounded-full bg-muted">
    {value != null && <div className="h-full rounded-full bg-foreground" style={{ width: `${value}%` }} />}
  </div>
}

function Row({ label, value, note }: { label: string; value?: number; note: string }) {
  return <div className="space-y-1.5">
    <div className="flex items-baseline justify-between gap-2">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="text-xs tabular-nums">{value == null ? '—' : `${Math.round(value)}%`}</span>
    </div>
    <Meter value={value} />
    <p className="text-xs leading-4 text-muted-foreground">{note}</p>
  </div>
}

/** The host's three capacities, as bars. */
export function SystemCard() {
  useLocale()
  const host = useHostMetrics()
  const data = host.data
  const cpu = data?.cpu_used_cores == null ? undefined : percent(data.cpu_used_cores, data.cpu_cores)
  const memory = data ? percent(data.memory_used_bytes, data.memory_total_bytes) : undefined
  const disk = data ? percent(data.disk_used_bytes, data.disk_total_bytes) : undefined

  return <OverviewCard>
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <Heading as="h2" size="sm">{t('System')}</Heading>
      {data && <span className="ml-auto text-xs text-muted-foreground tabular-nums">{t('{count} cores', { count: data.cpu_cores })}</span>}
    </div>

    {host.isPending
      ? <Skeleton className="h-40 w-full" />
      : host.isError
        ? <p className="text-xs text-muted-foreground">{t('Unavailable')}</p>
        : <>
            <Row label="CPU" value={cpu} note={data && data.cpu_used_cores != null ? `${data.cpu_used_cores.toFixed(1)} / ${data.cpu_cores}` : '—'} />
            <Row label={t('Memory')} value={memory} note={data ? `${formatBytes(data.memory_used_bytes)} / ${formatBytes(data.memory_total_bytes)}` : '—'} />
            <Row label={t('Disk')} value={disk} note={data?.disk_total_bytes ? `${formatBytes(data.disk_used_bytes ?? 0)} / ${formatBytes(data.disk_total_bytes)}` : '—'} />
          </>}
  </OverviewCard>
}
