import { cn } from '@/lib/utils'
import type { Tone } from '@/lib/state'

/**
 * The three shapes a status is allowed to take, and the colour of each tone.
 *
 * A shape says what kind of thing is being reported, so a reader can tell a
 * subject's state from an item's state from a page verdict before reading a
 * word:
 *
 *   StatusPill   — the state of the thing this card is about. One per surface.
 *   StatusInline — the state of an item in a list, stack or table. Many.
 *   StatusStrip  — the verdict for a whole module or page.
 *
 * Colour is confined to this file: no page writes `bg-success`, so "Running"
 * cannot be green here and grey there. The dot carries the tone and the word
 * carries the meaning, which is what keeps the state readable without colour.
 */
export const dotTone: Record<Tone, string> = {
  ok: 'bg-success',
  warn: 'bg-warning',
  fault: 'bg-destructive',
  // Filled neutral: we know, and it is deliberately doing nothing.
  off: 'bg-muted-foreground/40',
  // A ring, not a paler fill: "no answer" must not look like "off" at 6px.
  unknown: 'border-[1.5px] border-muted-foreground/60 bg-transparent',
}

const textTone: Record<Tone, string> = {
  ok: 'text-success-foreground',
  warn: 'text-warning-foreground',
  fault: 'text-destructive',
  off: 'text-muted-foreground',
  unknown: 'text-muted-foreground',
}

const pillTone: Record<Tone, string> = {
  ok: 'bg-success/10 text-success-foreground dark:bg-success/15',
  warn: 'bg-warning/10 text-warning-foreground dark:bg-warning/15',
  fault: 'bg-destructive/10 text-destructive dark:bg-destructive/20',
  off: 'bg-muted text-muted-foreground',
  unknown: 'border border-border bg-transparent text-muted-foreground',
}

/** The tone's dot fill, for artwork that draws its own badge (interface pictures). */
export function dotClass(tone: Tone): string {
  return dotTone[tone]
}

/** The tone's text colour, for a headline answer that is words rather than a dot. */
export function textClass(tone: Tone): string {
  return textTone[tone]
}

/** Tone belongs to the dot; a word never doubles the colour. */
export function StatusDot({
  tone,
  size = 'sm',
  className,
}: {
  tone: Tone
  /** `sm` inside rows and words, `md` for a page or strip verdict. */
  size?: 'sm' | 'md'
  className?: string
}) {
  return (
    <span
      aria-hidden
      className={cn('inline-block shrink-0 rounded-full', size === 'md' ? 'size-2' : 'size-1.5', dotTone[tone], className)}
    />
  )
}

/**
 * A dot and a state word, for one item among many.
 *
 * `answer` is the row's payload, so its word is foreground text; `dense` is for
 * a compact list that is about something else (Overview's module list), where
 * tinting every word would make the tile louder than its own figure.
 */
export function StatusInline({
  tone,
  emphasis = 'answer',
  className,
  children,
}: {
  tone: Tone
  emphasis?: 'answer' | 'dense'
  className?: string
  children: React.ReactNode
}) {
  return (
    <span className={cn('inline-flex min-w-0 items-center gap-1.5 text-meta leading-5', className)}>
      <StatusDot tone={tone} />
      <span className={cn('min-w-0 truncate', emphasis === 'answer' ? 'font-medium text-foreground' : 'text-muted-foreground')}>
        {children}
      </span>
    </span>
  )
}

/**
 * The state of the surface's subject: one per card or page header, never a tag
 * and never a measurement.
 */
export function StatusPill({
  tone,
  className,
  children,
}: {
  tone: Tone
  className?: string
  children: React.ReactNode
}) {
  return (
    <span
      className={cn(
        'inline-flex h-6 shrink-0 items-center rounded-md border border-transparent px-2 text-xs font-medium whitespace-nowrap',
        pillTone[tone],
        className,
      )}
    >
      {children}
    </span>
  )
}
