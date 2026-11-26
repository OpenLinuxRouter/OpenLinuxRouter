import type { ReactNode } from 'react'
import { ChevronRight } from 'lucide-react'
import { Link } from 'react-router'

import { Card } from '@/components/ui/card'
import { Heading } from '@/components/ui/heading'
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
export function OverviewCard({ children, className, bodyClassName, quiet = false }: {
  children: ReactNode
  className?: string
  bodyClassName?: string
  /**
   * The background tier: no shadow, so the facts about the box itself sit
   * behind the network's own readings instead of beside them.
   */
  quiet?: boolean
}) {
  return <Card className={cn('h-full gap-0 py-0', quiet && 'shadow-none', className)}>
    <div className={cn('flex min-w-0 flex-1 flex-col gap-3 p-4', bodyClassName)}>{children}</div>
  </Card>
}

/**
 * Every overview card's first line: title, its scope in muted text, and one
 * trailing fact. A title that leads to a page carries the row's chevron, so
 * the cards that open something look different from the ones that do not
 * without anyone reading an underline on hover.
 */
export function OverviewCardHeader({ title, scope, meta, to, level = 'h3' }: {
  title: string
  scope?: ReactNode
  meta?: ReactNode
  to?: string
  /** h3 under a section heading; h2 for the cards above the first section. */
  level?: 'h2' | 'h3'
}) {
  return <div className="flex min-h-6 flex-wrap items-center gap-x-2 gap-y-1">
    <Heading as={level} size="sm" className="min-w-0">
      {to
        ? <Link to={to} className="group/title inline-flex items-center gap-0.5 rounded-sm hover:text-primary focus-visible:outline-2 focus-visible:outline-ring">
            {title}
            <ChevronRight aria-hidden className="size-3.5 text-muted-foreground transition-colors group-hover/title:text-primary" />
          </Link>
        : title}
    </Heading>
    {scope && <span className="text-xs text-muted-foreground">{scope}</span>}
    {meta && <span className="ml-auto text-xs text-muted-foreground tabular-nums">{meta}</span>}
  </div>
}

/** A label over its value, for the small stat groups at the foot of a card. */
export function OverviewStat({ label, value, className }: { label: ReactNode; value: ReactNode; className?: string }) {
  return <div className={cn('min-w-0', className)}>
    <div className="truncate text-xs leading-4 text-muted-foreground">{label}</div>
    <div className="mt-0.5 truncate text-sm leading-5 font-medium tabular-nums">{value}</div>
  </div>
}

/** A thin share bar; neutral, because a share is not a state. */
export function OverviewMeter({ percent, className = 'bg-foreground/70' }: { percent?: number; className?: string }) {
  return <div className="h-1 w-full overflow-hidden rounded-full bg-muted">
    {percent != null && <div className={cn('h-full rounded-full', className)} style={{ width: `${Math.max(0, Math.min(100, percent))}%` }} />}
  </div>
}

/**
 * A tier of the overview: a 16px heading over its cards, with the tier's 32px
 * gap above it set by the page. The heading names what kind of reading follows
 * — what happened, what the box is doing, who is on it — so the page reads as
 * three groups instead of one stack of equal cards.
 */
export function OverviewSection({ title, aside, children }: { title: string; aside?: ReactNode; children: ReactNode }) {
  return <section className="space-y-3">
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <Heading as="h2" size="md" className="text-muted-foreground">{title}</Heading>
      {aside && <div className="ml-auto text-xs text-muted-foreground">{aside}</div>}
    </div>
    <div className="space-y-4">{children}</div>
  </section>
}
