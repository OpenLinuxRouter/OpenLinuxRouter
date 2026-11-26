import type { ReactNode } from 'react'
import { ChevronRight } from 'lucide-react'
import { Link } from 'react-router'

import { OverviewMeter } from './card'
import { Card } from '@/components/ui/card'
import { MetricValue } from '@/components/ui/metric-value'
import { useClientList } from '@/features/clients/queries'
import { useGatewayLatency } from '@/features/gateway/queries'
import { useNetworkQuality } from '@/features/network-quality/queries'
import { scoreText } from '@/features/network-quality/score'
import { t, useLocale } from '@/lib/i18n'

function Figure({ label, value, unit, note, to, meter }: {
  label: string
  value?: ReactNode
  unit?: string
  note: ReactNode
  to: string
  meter?: number
}) {
  return <Link to={to} className="group flex min-w-0 flex-1 items-start gap-3 lg:items-center px-3 py-3 transition-colors sm:px-4 hover:bg-accent/40 focus-visible:bg-accent/40 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring">
    <div className="min-w-0 flex-1">
      <p className="truncate text-xs leading-4 font-medium text-muted-foreground">{label}</p>
      <MetricValue value={value} unit={unit} size="lg" className="mt-1 text-xl leading-7 sm:text-3xl sm:leading-9" />
      <div className="mt-1 flex flex-col gap-1.5 lg:flex-row lg:items-center lg:gap-3">
        <p className="min-w-0 flex-1 truncate text-xs leading-4 text-muted-foreground">{note}</p>
        {meter !== undefined && <div className="w-full shrink-0 lg:w-20"><OverviewMeter percent={meter} /></div>}
      </div>
    </div>
    <ChevronRight aria-hidden className="hidden size-4 shrink-0 lg:block text-muted-foreground transition-colors group-hover:text-foreground" />
  </Link>
}

/**
 * The three numbers beside the live plot, each one a door to its own page.
 *
 * One card with hairlines rather than three cards: they are read together, as
 * "who is here, how good is it, how far is the internet", and three shadows for
 * three numbers is what made the old row look like a spreadsheet of tiles.
 * Uptime left for the status band above, where it answers "has it been fine".
 * Below 1024px the three sit side by side as one strip under the plot.
 */
export function KeyFigures() {
  useLocale()
  const clients = useClientList()
  const quality = useNetworkQuality()
  const latency = useGatewayLatency()

  const known = clients.data?.clients.length
  const online = clients.data ? clients.data.clients.filter(client => client.online).length : undefined
  const score = quality.isError ? undefined : quality.data?.overall.score ?? undefined
  const measuring = quality.data?.overall.state === 'measuring'
  const ms = latency.data?.milliseconds
  const dns = latency.data?.dns_milliseconds
  const target = latency.data?.target

  return <Card className="grid h-full grid-cols-3 gap-0 divide-x divide-border py-0 lg:grid-cols-1 lg:divide-x-0 lg:divide-y">
    <Figure label={t('Clients')} value={online} unit={t('online')} to="/topology"
      meter={known ? (online ?? 0) / known * 100 : undefined}
      note={clients.isPending ? t('Checking…') : online == null || known == null ? t('Unavailable') : t('{online} of {known} known', { online, known })} />
    <Figure label={t('Quality')} value={score == null || measuring ? undefined : scoreText(score)}
      unit={score == null || measuring ? undefined : t('of 100')} to="/quality"
      meter={score == null || measuring ? undefined : score}
      note={quality.isPending ? t('Checking…') : measuring ? t('Measuring…') : score == null ? t('Not tested') : t('Measured')} />
    <Figure label={t('Latency')} to="/latency"
      value={ms == null ? undefined : ms < 1 ? '<1' : Math.round(ms)} unit={ms == null ? undefined : 'ms'}
      note={latency.isPending ? t('Checking…')
        : latency.data?.state === 'unreachable' ? t('No response')
          : latency.data?.state === 'measuring' ? t('Measuring…')
            : [target, dns == null ? undefined : `${t('DNS lookup')} ${Math.round(dns)} ms`].filter(Boolean).join(' · ') || t('Unavailable')} />
  </Card>
}
