import { t } from '@/lib/i18n'

/**
 * The five things a status line is allowed to say, and the one table that
 * decides which is which.
 *
 * Before this file every surface chose its own word and its own colour, so
 * "Running" was green in one card and neutral in the next, and a failed read was
 * red on Overview and amber on Gateway. A reader cannot learn a language that
 * changes per page, so the tone of a word lives here and nowhere else: a call
 * site names a word from `STATE` and gets the tone that word always has.
 *
 * The words are still the product's words — "Not answering" means what it means
 * on Gateway and "Behind shared NAT" means what it means on DDNS. Sharing the
 * vocabulary is not sharing the sentence; `docs/ui-control-language.md` says
 * which words are legal and what each tone claims.
 *
 * `off` is the neutral tone and covers anything that is deliberately not doing
 * something: switched off, blocked, not applicable, nothing to report. It never
 * means "broken".
 * `unknown` is for an answer we do not have: still reading, unreadable, never
 * checked. It is a ring rather than a grey dot so it cannot be mistaken for `off`
 * at six pixels.
 */
export type Tone = 'ok' | 'warn' | 'fault' | 'off' | 'unknown'

/**
 * The closed set of state words and their tones.
 *
 * Adding a word is adding one line here, which is the point: the compiler will
 * not let a surface invent one, and the guard test refuses colour classes
 * outside this file.
 */
export const STATE = {
  // Working as asked.
  Running: 'ok',
  Connected: 'ok',
  OK: 'ok',
  Working: 'ok',
  On: 'ok',
  'Up to date': 'ok',
  Delegated: 'ok',
  Advertising: 'ok',
  Configured: 'ok',
  'IPv6 enabled': 'ok',
  Tunnel: 'ok',
  'Via exit': 'ok',
  'Default route': 'ok',
  'Valid for {days} days': 'ok',

  // Working, but somebody should look.
  'Needs attention': 'warn',
  'Not yet applied': 'warn',
  'Not up to date': 'warn',
  'Behind shared NAT': 'warn',
  'Renewal overdue': 'warn',
  'Not answering': 'warn',
  'Never connected': 'warn',
  'No cable': 'warn',
  'No address': 'warn',
  'Not configured': 'warn',
  'No default route': 'warn',
  'No network': 'warn',
  'No route': 'warn',
  'Unknown exit': 'warn',
  'Bypasses the exit': 'warn',
  Partial: 'warn',

  // Not working, and something on this page can fix it.
  'Not running': 'fault',
  Down: 'fault',
  Expired: 'fault',
  'Not installed': 'fault',
  'Not set': 'fault',
  Unreachable: 'fault',
  'Not found': 'fault',
   'LAN offline': 'warn',

  // Deliberately not doing anything, or nothing to report.
  Off: 'off',
  Disabled: 'off',
  'Not needed yet': 'off',
  Blocking: 'off',
  'No IPv6': 'off',
  'No routes': 'off',
  'Not managed': 'off',
  'Nothing yet': 'off',
  'IPv6 disabled': 'off',
  'Not in use': 'off',
  Direct: 'off',
  Loopback: 'off',
  Blocked: 'off',

  // No answer yet, or no answer at all.
  'Checking…': 'unknown',
  'Reading…': 'unknown',
  'Applying…': 'unknown',
  Waiting: 'unknown',
  Unknown: 'unknown',
  'Not checked': 'unknown',
  Unavailable: 'unknown',
  'Not counted': 'unknown',
  'Status unknown': 'unknown',
  'Asking…': 'unknown',
  Updating: 'unknown',
} as const satisfies Record<string, Tone>

export type StateWord = keyof typeof STATE

/** A word plus the tone it always carries, already translated. */
export interface Health {
  readonly tone: Tone
  readonly label: string
}

/**
 * The only legal way to build a status. The word has to be in `STATE`, so its
 * tone is decided once, here, rather than at the call site.
 */
export function state(word: StateWord, values?: Record<string, string | number>): Health {
  return { tone: STATE[word], label: t(word, values) }
}

/**
 * A verdict about something with a number in it — an elapsed time, a certificate
 * lifetime — whose label cannot be a constant. The tone is still explicit
 * because a dynamic label has no entry in `STATE`.
 */
export function healthOf(tone: Tone, label: string): Health {
  return { tone, label }
}

/**
 * The state of a network interface, in the words the interface cards and the
 * interface list both use.
 *
 * It used to be two enums: `Connected | Disconnected | Down | Not found |
 * Inactive` on Gateway's summary and `Not found | No cable | No address |
 * Connected` on the interface cards. One port cannot be "Down" in one half of a
 * page and "No cable" in the other, so there is one enum now, and the tone of
 * each word comes from `STATE`.
 */
export function linkHealth(row: {
  present: boolean
  up?: boolean
  running?: boolean
  prefixes?: string[]
  network?: string
  address?: string
}): Health {
  if (!row.present) return state('Not found')
  if (row.up === false) return state('Down')
  if (!row.running) return state('No cable')
  if (!row.prefixes?.length && !row.address) return state('No address')
  return state('Connected')
}

/**
 * The half of a read's result this decision needs, so the rule is stated once
 * and the page can pass React Query's cache state straight in.
 */
export interface DeviceRead {
  status: 'pending' | 'error' | 'success'
  fetchStatus: 'fetching' | 'paused' | 'idle'
}

/**
 * A device in the inventory, whose state is what this session has read.
 *
 * The inventory is configuration: on a fresh load we know every box's name,
 * roles, address and adapter and nothing about whether any of them is
 * answering. So no answer yet is `Not checked`, not `Connected` — and a failed
 * read is `Not answering`, not `Unreachable`, because a refused credential or a
 * timeout is a fact about our conversation with the box, not proof that the
 * link is down. The words and tones already exist in `STATE`; this function
 * only decides which one the session has earned.
 */
export function deviceHealth(read?: DeviceRead): Health {
  if (!read) return state('Not checked')
  if (read.status === 'error') return state('Not answering')
  if (read.status === 'success') return state('Connected')
  // A first read is in flight. Saying "Not checked" while we are checking would
  // be the helpfulness of a progress bar that never moves.
  if (read.fetchStatus === 'fetching') return state('Reading…')
  return state('Not checked')
}

/** A dialed-in peer: four answers, and "never connected" is the one to act on. */
export function peerHealth(peer: { unknown?: boolean; online?: boolean; lastHandshake?: string }): Health {
  if (peer.unknown) return state('Unknown')
  if (peer.online) return state('Connected')
  if (!peer.lastHandshake) return state('Never connected')
  return healthOf('off', elapsed(peer.lastHandshake))
}

/** "3 min ago" — a fact about a timestamp, not a state. */
export function elapsed(iso: string, now = Date.now()): string {
  const minutes = Math.round((now - new Date(iso).getTime()) / 60_000)
  if (minutes < 60) return `${minutes} min ago`
  const hours = Math.round(minutes / 60)
  if (hours < 48) return `${hours}h ago`
  return `${Math.round(hours / 24)}d ago`
}
