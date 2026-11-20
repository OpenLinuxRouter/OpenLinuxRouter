import { Link } from 'react-router'

import { OverviewCard } from './card'
import { MetricValue } from '@/components/ui/metric-value'
import { StatusDot } from '@/components/ui/status'
import { useClientList } from '@/features/clients/queries'
import { useNetworkQuality } from '@/features/network-quality/queries'
import { scoreText } from '@/features/network-quality/score'
import { useHostMetrics } from '@/features/system/queries'
import type { TrafficView } from '@/features/topology/traffic'
import { t, useLocale } from '@/lib/i18n'
import { formatRate } from '@/lib/utils'
import type { GatewayTraffic } from '@/lib/api-types'
import type { ReactNode } from 'react'

/** A rate's figure and unit, so the figure can stay the large part. */
function splitUnit(value: string): [string, string?] {
  const at = value.lastIndexOf(' ')
  return at < 0 ? [value] : [value.slice(0, at), value.slice(at + 1)]
}

function Bar({ percent, className = 'bg-foreground' }: { percent: number; className?: string }) {
  return <div className="h-1 w-full overflow-hidden rounded-full bg-muted">
    <div className={`h-full rounded-full ${className}`} style={{ width: `${Math.max(0, Math.min(100, percent))}%` }} />
  </div>
}

function KpiCard({ label, value, unit, note, to, size = 'lg', children }: {
  label: string
  value?: ReactNode
  unit?: string
  note?: ReactNode
  to?: string
  size?: 'md' | 'lg'
  children?: ReactNode
}) {
  const card = <OverviewCard className="transition-colors group-hover:bg-accent/40 group-focus-visible:bg-accent/40" bodyClassName="gap-2">
    <p className="truncate text-xs leading-4 font-medium text-muted-foreground">{label}</p>
    <MetricValue value={value} unit={unit} size={size} />
    {note && <p className="text-xs leading-4 text-muted-foreground">{note}</p>}
    {children && <div className="mt-auto min-w-0 pt-1">{children}</div>}
  </OverviewCard>
  return <div className="h-full min-w-0">
    {to
      ? <Link to={to} className="group block h-full rounded-xl focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring">{card}</Link>
      : card}
  </div>
}

/**
 * The four numbers a visit starts with.
 *
 * Throughput is one card with both directions, not two — they are one reading.
 * Clients and Quality own their bars; uptime is the quiet answer that makes the
 * others meaningful. Nothing here opens a dialog.
 */
export function KpiRow({ traffic, trafficFailed, flows }: {
  traffic: GatewayTraffic | undefined
  trafficFailed: boolean
  flows: TrafficView
}) {
  useLocale()
  const clients = useClientList()
  const quality = useNetworkQuality()
  const host = useHostMetrics()

  const rate = (direction: 'down' | 'up') => {
    if (trafficFailed || !flows.rated) return undefined
    const value = formatRate(flows.total[direction === 'down' ? 'downRate' : 'upRate'] ?? 0)
    return splitUnit(value)
  }
  const down = rate('down')
  const up = rate('up')

  const known = clients.data?.clients.length
  const online = clients.data ? clients.data.clients.filter(client => client.online).length : undefined
  const score = quality.isError ? undefined : quality.data?.overall.score ?? undefined
  const measuring = quality.data?.overall.state === 'measuring'
  const uptime = host.data?.uptime_seconds
  const days = uptime == null ? undefined : Math.floor(uptime / 86400)
  const hours = uptime == null ? undefined : Math.floor((uptime % 86400) / 3600)
  const minutes = uptime == null ? undefined : Math.floor((uptime % 3600) / 60)
  const uptimeValue = days == null ? undefined : days > 0 ? days : hours! > 0 ? hours : minutes
  const uptimeUnit = days == null ? undefined : days > 0 ? t('days') : hours! > 0 ? 'h' : 'm'
  const uptimeNote = days == null ? undefined : days > 0 ? `${hours}h ${minutes}m` : hours! > 0 ? `${minutes}m` : undefined

  const throughputNote = trafficFailed ? t('Traffic could not be read')
    : !traffic ? t('Checking…')
      : !traffic.counting ? t('Traffic accounting is off')
        : !flows.rated ? t('Waiting for a sample')
          : undefined

  return <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
    <div className="col-span-2 min-w-0 xl:col-span-1">
      <OverviewCard bodyClassName="gap-2">
        <p className="truncate text-xs leading-4 font-medium text-muted-foreground">{t('Throughput now')}</p>
        <div className="flex min-w-0 items-start gap-4">
          <div className="min-w-0">
            <MetricValue value={down?.[0]} unit={down?.[1]} size="lg" />
            <p className="mt-0.5 text-xs leading-4 text-receive">↓ {t('Download')}</p>
          </div>
          <div className="min-w-0">
            <MetricValue value={up?.[0]} unit={up?.[1]} size="lg" />
            <p className="mt-0.5 text-xs leading-4 text-transmit">↑ {t('Upload')}</p>
          </div>
        </div>
        {throughputNote
          ? <p className="mt-auto text-xs leading-4 text-muted-foreground">{throughputNote}</p>
          : <p className="mt-auto inline-flex items-center gap-1.5 text-xs leading-4 text-muted-foreground"><StatusDot tone="ok" />{t('Live')}</p>}
      </OverviewCard>
    </div>

    <KpiCard label={t('Clients')} value={online} unit={t('online')} to="/topology"
      note={clients.isPending ? t('Checking…') : online == null || known == null ? t('Unavailable') : t('{online} of {known} known', { online, known })}>
      <Bar percent={known ? (online ?? 0) / known * 100 : 0} className="bg-foreground/50" />
    </KpiCard>

    <KpiCard label={t('Quality')} value={score == null || measuring ? undefined : scoreText(score)}
      unit={score == null ? undefined : t('of 100')} to="/quality"
      note={quality.isPending ? t('Checking…') : measuring ? t('Measuring…') : score == null ? t('Not tested') : t('Measured')}>
      <Bar percent={score ?? 0} />
    </KpiCard>

    <div className="col-span-2 min-w-0 xl:col-span-1">
      <KpiCard label={t('Uptime')} value={uptimeValue} unit={uptimeUnit}
        note={host.isPending ? t('Checking…') : uptimeNote ?? t('Unavailable')} />
    </div>
  </div>
}
