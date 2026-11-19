import { useSearchParams } from 'react-router'

import { EquipmentMap } from '@/features/topology/equipment-map'

/**
 * The hardware, as this router can read it.
 *
 * The group map that used to sit here lives on Overview, where it answers
 * "what is on my network". It is not topology: a line there is membership, and
 * drawing it twice made the one page named after the network's shape into a
 * second copy of a page about groups.
 *
 * What is left is the part of the network this router can actually observe —
 * the boxes it reads, where each one is managed from, and the clients each one
 * reports — and the drawing is the page. No title block, no legend under it:
 * the section rail already names this place, the marks on the map say what
 * they are, and the shell keeps an `sr-only` h1 for assistive technology
 * (docs/ui-style-guide.md, section landing pages).
 */
export function TopologyPage() {
  const [params] = useSearchParams()
  return <EquipmentMap highlight={params.get('device') ?? undefined} />
}
