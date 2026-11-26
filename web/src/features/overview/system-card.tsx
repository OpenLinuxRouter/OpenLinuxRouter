import { OverviewCard, OverviewCardHeader, OverviewMeter } from './card'
import { Skeleton } from '@/components/ui/skeleton'
import { useHostMetrics } from '@/features/system/queries'
import { t, useLocale } from '@/lib/i18n'
import { formatBytes } from '@/lib/utils'

function percent(used: number | null | undefined, total: number | null | undefined) {
  if (used == null || total == null || total <= 0) return undefined
  return Math.max(0, Math.min(100, (used / total) * 100))
}

function Row({ label, value, note }: { label: string; value?: number; note: string }) {
  return <div className="space-y-1.5">
    <div className="flex items-baseline gap-2">
      <span className="text-sm leading-5 font-medium">{label}</span>
      <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground tabular-nums">{note}</span>
      <span className="text-sm leading-5 font-medium tabular-nums">{value == null ? '—' : `${Math.round(value)}%`}</span>
    </div>
    <OverviewMeter percent={value} />
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

  return <OverviewCard bodyClassName="gap-4">
    <OverviewCardHeader title={t('System')} meta={data && t('{count} cores', { count: data.cpu_cores })} />

    {host.isPending
      ? <Skeleton className="h-32 w-full" />
      : host.isError
        ? <p className="text-xs text-muted-foreground">{t('Unavailable')}</p>
        : <div className="flex flex-1 flex-col justify-between gap-4">
            <Row label="CPU" value={cpu} note={data && data.cpu_used_cores != null ? `${data.cpu_used_cores.toFixed(1)} / ${data.cpu_cores}` : '—'} />
            <Row label={t('Memory')} value={memory} note={data ? `${formatBytes(data.memory_used_bytes)} / ${formatBytes(data.memory_total_bytes)}` : '—'} />
            <Row label={t('Disk')} value={disk} note={data?.disk_total_bytes ? `${formatBytes(data.disk_used_bytes ?? 0)} / ${formatBytes(data.disk_total_bytes)}` : '—'} />
          </div>}
  </OverviewCard>
}
