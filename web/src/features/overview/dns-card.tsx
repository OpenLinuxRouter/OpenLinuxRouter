import { OverviewCard, OverviewCardHeader, OverviewStat } from './card'
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
  const share = (count: number) => stats && stats.queries > 0 ? `${(count / stats.queries * 100).toFixed(1)}%` : undefined
  const answered = stats ? Math.max(0, stats.queries - stats.blocked - stats.failed) : 0

  return <OverviewCard quiet bodyClassName="gap-4">
    <OverviewCardHeader title={t('DNS')} to="/gateway/dns" meta={stats && t('started {ago}', { ago: formatAgo(stats.since) })} />

    {!enabled && dns.data
      ? <p className="text-xs text-muted-foreground">{t('DNS is off')}</p>
      : !stats
        ? <p className="text-xs text-muted-foreground">{dns.isPending ? t('Checking…') : dns.isError ? t('Unavailable') : t('No relay stats')}</p>
        : <div className="flex flex-1 flex-col justify-between gap-4">
            <div>
              <MetricValue value={stats.queries.toLocaleString()} unit={t('queries')} size="md" />
              {/* One bar, three parts: the share each outcome took. Blocked
                  and failed sit at the end so a small share is still a mark. */}
              <div className="mt-3 flex h-1.5 w-full gap-px overflow-hidden rounded-full bg-muted">
                <div className="h-full bg-foreground/45" style={{ width: `${stats.queries > 0 ? answered / stats.queries * 100 : 0}%` }} />
                {stats.blocked > 0 && <div className="h-full min-w-0.5 bg-foreground/75" style={{ width: share(stats.blocked) }} />}
                {stats.failed > 0 && <div className="h-full min-w-0.5 bg-foreground" style={{ width: share(stats.failed) }} />}
              </div>
            </div>
            <div className="grid grid-cols-3 gap-3 rounded-lg bg-muted/50 px-3 py-2">
              <OverviewStat label={t('Answered')} value={answered.toLocaleString()} />
              <OverviewStat label={t('Blocked')} value={stats.blocked.toLocaleString()} />
              <OverviewStat label={t('Failed')} value={stats.failed.toLocaleString()} />
            </div>
            {stats.dropped > 0 && <p className="-mt-2 text-xs leading-4 text-muted-foreground">{t('{count} dropped', { count: stats.dropped.toLocaleString() })}</p>}
          </div>}
  </OverviewCard>
}
