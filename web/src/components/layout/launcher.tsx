import { t, useLocale } from '@/lib/i18n'
import { Link } from 'react-router'

import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

/**
 * The app launcher: a grid of one-tile-per-destination directories (Access,
 * Advanced), not a list of settings.
 *
 * A directory answers "where do I go" before it answers "what does it say", so
 * the tile is a mark and a short name on one line. The state still travels with
 * the tile — hover/focus explains the function and the current answer, and the
 * accessible name repeats that answer for touch and screen readers — but it does
 * not get a line of its own, because a directory that prints nine statuses has
 * stopped being a directory.
 *
 * Every tile is one link with one destination. A destination OLR does not
 * configure may open the project's own site instead; there is no tile whose
 * click opens a dialog.
 */
export function Launcher({ children }: { children: React.ReactNode }) {
  useLocale()
  return <div className="space-y-8">{children}</div>
}

/** One headed row of tiles. The id names the heading the tiles are labelled by. */
export function LauncherSection({ id, title, children }: { id: string; title: string; children: React.ReactNode }) {
  useLocale()
  return (
    <section className="space-y-4" aria-labelledby={`launcher-${id}`}>
      <h2 id={`launcher-${id}`} className="text-2xl leading-8 font-semibold">{title}</h2>
      <div className="grid grid-cols-3 gap-x-3 gap-y-6 sm:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8">{children}</div>
    </section>
  )
}

/** A feature's current answer, in the same shape the section summaries use. */
export type LauncherState = { text: string }

/**
 * What sits on the 64px backing.
 *
 * Bundled project artwork stays on white so brand colours survive both themes.
 * Artwork that already carries its own rounded background fills the tile
 * instead of sitting inside a second one. Protocols and OLR features may use
 * approved original app artwork too. Entries without artwork wear their initial
 * on a tinted tile — the hue keeps shared initials apart. The icon variant is
 * the fallback for a directory entry with neither.
 */
export type LauncherMark =
  | { kind: 'logo'; src: string }
  | { kind: 'app-icon'; src: string }
  | { kind: 'letter'; letter: string; hue: number }
  | { kind: 'icon'; children: React.ReactNode }

// Each variant owns its whole surface: a logo needs white behind it, and an
// app icon brings its own background, so it is neither white-backed nor padded
// away from the tile edge.
const MARK_BACKING = {
  logo: 'bg-white p-3 group-hover:bg-accent group-active:bg-accent',
  'app-icon': 'overflow-hidden group-hover:brightness-110 group-active:brightness-95',
  letter: 'p-3 text-white group-hover:brightness-110 group-active:brightness-95',
  icon: 'p-3 bg-accent/60 text-primary group-hover:bg-accent group-active:bg-accent',
}

export function LauncherTile({
  name,
  description,
  mark,
  notes,
  to,
  href,
  planned,
}: {
  name: string
  /** One sentence for the tooltip; never the only place a function is stated. */
  description: string
  mark: LauncherMark
  /** The tile's current answer: hover/focus and the accessible name, nowhere else. */
  notes?: LauncherState[]
  to?: string
  href?: string
  /**
   * A choice the directory will carry but cannot configure yet. It keeps its
   * place and its name so a group states what it is going to contain, and says
   * plainly that nothing opens — an inert tile is not an external one.
   */
  planned?: boolean
}) {
  useLocale()
  const summary = notes?.[0]
  // A tile with an OLR page is integrated even when it has nothing live to
  // report; only a tile that leaves the app is an external setup.
  const external = !to && !planned
  const label = summary
    ? `${name} · ${summary.text}`
    : planned ? `${name} · ${t('Not available yet')}` : external ? `${name} · Not integrated` : name
  return (
    <Tooltip>
      <TooltipTrigger
        render={to ? <Link to={to} /> : href ? <a href={href} target="_blank" rel="noopener noreferrer" /> : <span />}
        aria-label={label}
        className={cn(
          'group flex min-w-0 flex-col items-center gap-2 rounded-xl px-0 py-2 text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-4 focus-visible:ring-offset-background',
          // Nothing opens, so the tile loses the pointer and reads as a
          // statement of intent rather than a link that happens to be broken.
          planned && 'cursor-default opacity-55',
        )}
      >
        <span
          className={cn('flex size-16 shrink-0 items-center justify-center rounded-2xl transition duration-150', MARK_BACKING[mark.kind])}
          style={mark.kind === 'letter' ? { background: `oklch(0.52 0.12 ${mark.hue})` } : undefined}
        >
          {mark.kind === 'app-icon'
            ? <img src={mark.src} alt="" className="size-full object-cover" />
            : mark.kind === 'logo'
              ? <img src={mark.src} alt="" className="max-h-full max-w-full object-contain" />
              : mark.kind === 'letter'
                ? <span aria-hidden className="text-[28px] leading-none font-semibold tracking-tight">{mark.letter}</span>
                : mark.children}
        </span>
        {/* A name the tile has to show in full — "Cloudflare Speed Test" names
            one service among three — wraps to a second line rather than being
            cut off at the ellipsis, which would leave two tiles reading
            "Cloudflare…". Short names still sit on one line. */}
        <span className="w-full text-center text-[13px] leading-tight font-medium text-balance sm:text-sm">{name}</span>
      </TooltipTrigger>
      <TooltipContent className="block space-y-2 text-sm leading-relaxed">
        <p>{description}</p>
        {notes?.length
          ? notes.map((note, index) => <p key={index}>{note.text}</p>)
          : planned ? <p>{t('Not available yet. This router does not configure it, and nothing opens here.')}</p>
            : external ? <p>{t('Not integrated · External setup')}</p> : null}
      </TooltipContent>
    </Tooltip>
  )
}
