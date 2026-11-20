import { Link } from 'react-router'

import { Card } from '@/components/ui/card'
import { Heading } from '@/components/ui/heading'
import { MetricValue } from '@/components/ui/metric-value'
import { useDnsStatus } from '@/features/dns/queries'
import { t, useLocale } from '@/lib/i18n'
import { formatAgo } from '@/lib/utils'

/** The relay's account of itself: how many, how many blocked, how many failed. */
export function DnsCard() {
  useLocale()
  const dns = useDnsStatus()
  const stats = dns.data?.stats
  const enabled = Boolean(dns.data?.enabled)
  const blockedShare = stats && stats.queries > 0 ? (stats.blocked / stats.queries) * 100 : undefined
  const answered = stats ? Math.max(0, stats.queries - stats.blocked) : 0

  return <Card className="gap-3">
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <Heading as="h2" size="sm"><Link to="/gateway/dns" className="underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-ring">{t('DNS')}</Link></Heading>
      {stats && <span className="ml-auto text-xs text-muted-foreground tabular-nums">{t('started {ago}', { ago: formatAgo(stats.since) })}</span>}
    </div>

    {!enabled && dns.data
      ? <p className="text-xs text-muted-foreground">{t('DNS is off')}</p>
      : !stats
        ? <p className="text-xs text-muted-foreground">{dns.isPending ? t('Checking…') : dns.isError ? t('Unavailable') : t('No relay stats')}</p>
        : <>
            <div>
              <MetricValue value={stats.queries.toLocaleString()} size="lg" />
              <p className="mt-0.5 text-xs text-muted-foreground">
                {t('queries')}{blockedShare == null ? '' : ` · ${t('{count} blocked', { count: stats.blocked.toLocaleString() })} · ${blockedShare.toFixed(1)}%`}
              </p>
            </div>
            <div className="space-y-1.5">
              <div className="flex h-1 w-full overflow-hidden rounded-full bg-muted">
                <div className="h-full bg-foreground/35" style={{ width: `${stats.queries > 0 ? answered / stats.queries * 100 : 0}%` }} />
                <div className="h-full bg-foreground/70" style={{ width: `${blockedShare ?? 0}%` }} />
              </div>
              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span>{t('Answered')}</span>
                <span>{t('Blocked')}</span>
              </div>
            </div>
            <p className="mt-auto text-xs leading-4 text-muted-foreground">
              {stats.failed > 0 ? t('{count} failed', { count: stats.failed.toLocaleString() }) : t('No failed lookups')}
              {stats.dropped > 0 ? ` · ${t('{count} dropped', { count: stats.dropped.toLocaleString() })}` : ''}
            </p>
          </>}
  </Card>
}
