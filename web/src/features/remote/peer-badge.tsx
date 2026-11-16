import { useLocale } from '@/lib/i18n'

import { StatusInline } from '@/components/ui/status'
import { elapsed, peerHealth } from '@/lib/state'
import type { RemotePeer } from '@/lib/api-types'

/**
 * Four answers, not two, and the one that matters is "never connected".
 *
 * A device that has *never* connected almost always means the configuration was
 * never imported, or the port is not reachable from outside — a setup problem
 * with somewhere to look. A device last seen yesterday is working and asleep.
 * Collapsing them into "offline" would hide the only failure this module
 * reliably produces on a first install.
 *
 * Shared by the device list on the tunnel page and the device's own page, so
 * that one peer cannot read as "Connected" on one screen and "offline" on the
 * next. A last-seen time is a fact rather than a state, so it is plain text with
 * no dot — the row is not claiming health, only recency.
 */
export function PeerStatus({ peer }: { peer: RemotePeer }) {
  useLocale()
  if (!peer.unknown && !peer.online && peer.last_handshake) {
    return <span className="text-meta text-muted-foreground tabular-nums">{elapsed(peer.last_handshake)}</span>
  }
  const health = peerHealth({ unknown: peer.unknown, online: peer.online, lastHandshake: peer.last_handshake })
  return <StatusInline tone={health.tone}>{health.label}</StatusInline>
}

/** Kept for the call sites that read as a badge; it is an inline state now. */
export const PeerBadge = PeerStatus
