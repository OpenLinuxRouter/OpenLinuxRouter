import { t, useLocale } from '@/lib/i18n'
import { useSearchParams } from 'react-router'

import { PageHeading } from '@/components/layout/page-heading'

import { EquipmentBand } from '@/features/topology/equipment-band'

/**
 * The hardware, as this router can read it.
 *
 * The group map that used to sit below this heading lives on Overview, where it
 * answers "what is on my network". It is not topology: a line there is
 * membership, and drawing it twice made the one page named after the network's
 * shape into a second copy of a page about groups.
 *
 * What is left is the part of the network this router can actually observe —
 * the boxes it reads, where each one is managed from, and the clients each one
 * reports — with its limits stated on the page rather than in a legend about
 * lines nobody drew.
 */
export function TopologyPage() {
  useLocale()
  const [params] = useSearchParams()
  return (
    <div className="space-y-6">
      <PageHeading
        title={t('Topology')}
        description={t('The boxes this router reads, and the clients each one reports.')}
      />
      <EquipmentBand highlight={params.get('device') ?? undefined} />
    </div>
  )
}
