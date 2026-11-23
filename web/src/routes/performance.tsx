import { PageHeading } from '@/components/layout/page-heading'
import { ClientTable } from '@/features/performance/client-table'
import { EventBand } from '@/features/performance/event-band'
import { LinkLane } from '@/features/performance/link-lane'
import { RouterLane } from '@/features/performance/router-lane'
import { SummaryStrip } from '@/features/performance/summary'
import { usePerformance } from '@/features/performance/use-performance'
import { t, useLocale } from '@/lib/i18n'

/**
 * The line and the router on one clock.
 *
 * The overview answers what the network is doing; this page answers which side
 * of it is responsible when something slows down. Both lanes share one window
 * and one set of time ticks, so a throughput dip can be read straight across
 * against CPU and drops without switching charts.
 */
export function PerformancePage() {
  useLocale()
  const evidence = usePerformance()

  return <div className="space-y-6">
    <PageHeading
      title={t('Performance details')}
      description={t('Throughput, CPU and drops on one time axis, so a dip can be lined up against the line and the router.')}
    />
    <SummaryStrip evidence={evidence} />
    <EventBand evidence={evidence} />
    <LinkLane evidence={evidence} />
    <RouterLane evidence={evidence} />
    <ClientTable />
  </div>
}
