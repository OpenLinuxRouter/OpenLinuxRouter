import type { ReactNode } from 'react'

import { Card } from '@/components/ui/card'
import { cn } from '@/lib/utils'

/**
 * One overview card, with the same 16px on every side at every breakpoint.
 *
 * `Card` carries vertical padding only; horizontal padding belongs to the
 * content layer. Writing that per card is how the overview grew three different
 * gutters. The cards here own no padding of their own: they hand the whole box
 * to this one body, so a chart, a readout and a status list all start and end on
 * the same line.
 */
export function OverviewCard({ children, className, bodyClassName }: {
  children: ReactNode
  className?: string
  bodyClassName?: string
}) {
  return <Card className={cn('h-full gap-0 py-0', className)}>
    <div className={cn('flex min-w-0 flex-1 flex-col gap-3 p-4', bodyClassName)}>{children}</div>
  </Card>
}
