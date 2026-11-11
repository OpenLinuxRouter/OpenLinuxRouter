import { t, useLocale } from '@/lib/i18n'
import type { ReactNode } from 'react'

import { Heading } from '@/components/ui/heading'

/**
 * The top of a device page: one big picture, one editable name, and the traffic
 * matrix behind both.
 *
 * Shared by `/clients/:mac` and `/clients/wireguard/:name` so that a device at
 * home and one that dialed in open the same way. The two pages used to draw this
 * block twice, and the copies drifted: the peer's kept the pointer on its title
 * block and the device's did not, which cost the peer two rows of activity dots
 * their hover ([311204f]).
 *
 * What is *not* shared is as deliberate. A name is the only editable thing here,
 * and both pages store it in a different place — `devices` in olr.json, `remote`
 * on the peer entry — so each page keeps its own editor and passes it in. What
 * follows the name is `status`, and only one page has evidence for one: a peer's
 * connection state is a fact about the tunnel (`PersistentKeepalive = 25` in
 * every configuration this router renders means a connected peer handshakes at
 * least every two minutes, so "no handshake for three minutes" really is "not
 * connected"), while a device at home has no equivalent — its lease outlives its
 * presence by hours and the neighbour table keeps a STALE entry looking complete
 * — so it gets no status at all. See docs/ui-content-guide.md.
 *
 * The key each page is addressed by — the MAC, or the peer's configured name —
 * is not drawn here. Both pages put it in their facts table, where it is a row
 * like any other identity, rather than a chip that only appears when the title
 * happens to hide it.
 */
export function ClientHeader({
  activity,
  picture,
  onPicture,
  pictureBusy,
  title,
  status,
  note,
}: {
  /** The traffic matrix, drawn behind everything else. */
  activity?: ReactNode
  /** The picture, already sized and decorated by the caller. */
  picture: ReactNode
  onPicture: () => void
  pictureBusy?: boolean
  /** The name, either a button or the field that replaces it. */
  title: ReactNode
  /** A connection state, for a page that has evidence for one. */
  status?: ReactNode
  /**
   * A quiet line under the name, for a page whose name needs one sentence to be
   * honest about itself — a name remembered rather than heard. Not a status:
   * it says where the name came from, not whether the device is here.
   */
  note?: string
}) {
  useLocale()
  return (
    <div className="relative flex min-h-20 items-center gap-4 border-b pb-4">
      {activity}
      <button
        type="button"
        onClick={onPicture}
        disabled={pictureBusy}
        aria-label={t('Change client icon')}
        className="relative shrink-0 rounded-lg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
      >
        {picture}
      </button>
      {/* The activity dots are painted behind this block and a transparent box
          still takes the pointer, so it gives the pointer up — and the name
          takes it back, because the name is a button. Without that pairing the
          dots at the title's height answer no hover at all, which reads as
          "this one has traffic but no numbers". */}
      <div className="pointer-events-none relative min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <Heading as="h1" size="lg" className="pointer-events-auto min-w-0 max-w-full [overflow-wrap:anywhere]">
            {title}
          </Heading>
          {status}
        </div>
        {note && <p className="mt-1 text-xs text-muted-foreground">{note}</p>}
      </div>
    </div>
  )
}
