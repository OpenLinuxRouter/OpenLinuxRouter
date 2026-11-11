import { t } from '@/lib/i18n'
import type { ClientRow } from '@/lib/api-types'
import { formatAgo } from '@/lib/utils'

/**
 * When a device was last heard from, which is the only half of presence a
 * device at home can be honest about.
 *
 * `online` looks like an answer and is not one. It is true while a DHCP lease
 * is unexpired — twelve hours by default — or while the neighbour table holds a
 * complete entry, and the kernel keeps a STALE entry for a device that left an
 * hour ago. So "online" mostly means "somewhere within its lease period", and
 * an interface that lost its heatmap to it would be claiming knowledge nobody
 * has.
 *
 * A time is a fact: the router either heard from it or did not. So every surface
 * that used to say Online/Offline about a device on the LAN says when instead,
 * through these two functions, and a dialed-in device is the only one left
 * with a state — because `PersistentKeepalive = 25` makes three minutes without
 * a handshake mean the tunnel really is down (internal/remote/view.go).
 *
 * The words are one set for every surface, which is the point: a group row, the
 * device dialog and a network-map node used to say `Offline`, `Offline` and
 * `away` about the same device on the same screen.
 */

/** The compact form, for a row that has one column: "2h ago", "—", "never seen". */
export function lastHeardLabel(device: ClientRow): string {
  if (device.last_seen) return formatAgo(device.last_seen)
  // Seen but never confirmed answering: a lease lists it and the neighbour table
  // has not. Not the same as never seen, and not a time — so a dash, which
  // claims nothing.
  return device.seen ? '—' : t('never seen')
}

/** The full sentence, for a tooltip or a dialog: what "—" and "never seen" mean. */
export function lastHeardTitle(device: ClientRow): string {
  if (device.last_seen) return `Last heard ${new Date(device.last_seen).toLocaleString()}`
  return device.seen
    ? 'Nothing has confirmed it answering since this router started keeping the record'
    : 'No source has ever seen this device'
}
