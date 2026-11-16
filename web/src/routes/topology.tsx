import { t, useLocale } from '@/lib/i18n'
import { Waypoints } from 'lucide-react'

import { PageHeading } from '@/components/layout/page-heading'

import { NetworkMapPanel } from '@/features/topology/map-panel'

/**
 * The network as this router can see it.
 *
 * It is the Overview's picture with the room to be the whole page rather than a
 * section under the counters. The legend is here and not on Overview because
 * this is the page someone opens to ask what the drawing means.
 */
export function TopologyPage() {
  useLocale()
  return (
    <div className="space-y-6">
      <PageHeading
        title={t('Topology')}
        description={t('The router at the top, the groups you made below it, and the devices in them. A line joins a group to the router — it is membership, not a cable.')}
      />
      <Legend />
      <NetworkMapPanel density="auto" />
    </div>
  )
}

/**
 * What the picture encodes, and the one thing it deliberately cannot.
 *
 * Three rows, no swatches invented for things the drawing does not have: the
 * honest limit is part of the legend, because a topology page that implied
 * cabling would be the most confident wrong screen in the product.
 */
function Legend() {
  useLocale()
  return (
    <dl className="grid gap-2 rounded-xl border border-border bg-card px-4 py-3 text-xs text-muted-foreground sm:grid-cols-3">
      <div className="flex gap-2">
        <dt aria-hidden className="mt-0.5 size-2 shrink-0 rounded-full border border-muted-foreground/60" />
        <dd>{t('A card is a device this router knows, with the address it was seen on.')}</dd>
      </div>
      <div className="flex gap-2">
        <Waypoints aria-hidden className="mt-0.5 size-3.5 shrink-0" />
        <dd>{t('A line joins the router to one of your groups. Its thickness is that group’s traffic; it is not a cable.')}</dd>
      </div>
      <div className="flex gap-2">
        <dt aria-hidden className="mt-0.5 size-2 shrink-0 rounded-sm border border-dashed border-muted-foreground/60" />
        <dd>{t('Cabling is not drawn: no adapter reports its neighbours yet, so a cable between two boxes is not something this router can see.')}</dd>
      </div>
    </dl>
  )
}
