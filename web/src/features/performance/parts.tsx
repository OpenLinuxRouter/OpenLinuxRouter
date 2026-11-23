import type { ReactNode } from 'react'

import { MetricValue } from '@/components/ui/metric-value'
import { useLocale } from '@/lib/i18n'
import { cn } from '@/lib/utils'

import { clockShort } from './live'

/** One label, one figure, and the word that says where the figure came from. */
export function Readout({ label, value, unit, provenance, note }: {
  label: string
  value?: ReactNode
  unit?: string
  provenance?: string
  note?: ReactNode
}) {
  return <div className="min-w-0 rounded-lg bg-muted/60 p-3">
    <div className="flex flex-wrap items-baseline gap-x-2">
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      {provenance && <span className="text-xs text-muted-foreground/80">{provenance}</span>}
    </div>
    <div className="mt-1"><MetricValue value={value} unit={unit} size="md" /></div>
    {note && <p className="mt-0.5 text-xs leading-4 text-muted-foreground">{note}</p>}
  </div>
}

/**
 * A chart's legend swatch takes the shape of its mark: a short rule for a line,
 * a dashed rule for the capacity reference line.
 */
export function LegendItem({ label, color, dashed = false }: { label: string; color: string; dashed?: boolean }) {
  return <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
    <span aria-hidden className={cn('shrink-0', dashed ? 'w-4 border-t border-dashed border-current' : 'h-0.5 w-3 rounded-full bg-current', color)} />
    {label}
  </span>
}

/** A lane with no series says what is missing rather than drawing an empty box. */
export function LaneEmpty({ children }: { children: ReactNode }) {
  return <div className="grid min-h-[180px] place-items-center rounded-lg border border-dashed border-border px-4 py-6 text-center">
    <div className="max-w-md text-xs leading-5 text-muted-foreground">{children}</div>
  </div>
}

/** A clock tick anchored so the first and last labels stay inside the plot. */
export function TimeTick({ x = 0, y = 0, payload, start, end }: {
  x?: number
  y?: number
  payload?: { value?: number | string }
  start: number
  end: number
}) {
  const locale = useLocale()
  const at = Number(payload?.value)
  const anchor = at <= start ? 'start' : at >= end ? 'end' : 'middle'
  return <text x={x} y={y} dy={10} textAnchor={anchor} className="fill-muted-foreground text-xs tabular-nums">
    {Number.isFinite(at) ? clockShort(at, locale) : ''}
  </text>
}
