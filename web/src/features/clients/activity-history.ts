import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'

export interface Bytes { up: number; down: number }
export interface ActivityBucket { at: string; bytes: Bytes }

/**
 * Retained half-hour buckets, and the window they were cut from.
 *
 * `days` is the daemon's own retention window rather than a constant here, so
 * the payload describes itself and no screen has to keep a second copy that
 * would go stale the week after the daemon changed it. Nothing renders it: the
 * matrix derives its columns from the width available and the total states no
 * window.
 */
export interface ActivityHistory {
  days: number
  buckets: ActivityBucket[]
}

/**
 * What a screen asks history about: a client, or — for a peer, which has no MAC
 * — an address.
 *
 * The daemon stores a client's traffic under that client however many addresses
 * it used, so the query is by MAC. The address form survives because a WireGuard
 * peer is keyed by address everywhere else in the model, and because history
 * written before presence could place an address is still filed under it.
 */
export type ActivityTarget = { mac: string } | { addresses: string[] }

/**
 * One device's retained activity.
 *
 * Polled a minute at a time rather than every ten seconds, because ninety days
 * of half-hour buckets is a few hundred kilobytes and the matrix's *colour* is
 * the only thing that moves: the breathing that says "right now" reads live
 * rates from the traffic endpoint, not from here. A minute is ample against a
 * history whose finest grain is thirty.
 *
 * Both halves of the device card — the matrix and the total under it — call
 * this, and share one request through the query cache.
 */
export function useActivity(target: ActivityTarget) {
  const key = 'mac' in target ? `mac:${target.mac}` : `address:${target.addresses.join(' ')}`
  return useQuery({
    queryKey: ['gateway', 'activity', key],
    queryFn: () => {
      const query = new URLSearchParams()
      if ('mac' in target) query.set('mac', target.mac)
      else target.addresses.slice(0, 32).forEach((address) => query.append('address', address))
      return api.get<ActivityHistory>(`/api/gateway/activity?${query}`)
    },
    refetchInterval: 60000,
  })
}

/**
 * The matrix, added up.
 *
 * Derived from the drawn buckets rather than read from the kernel's cumulative
 * counters (topology/traffic.ts), which is the whole point: those counters have
 * no window, so they reset on a restart, expire after a day of silence, and can
 * read smaller than a single four-hour cell that history remembers. A total
 * that cannot be less than one of the cells above it is a total a reader can
 * check; the cost is that it is a *recorded* volume rather than an exact one,
 * since the sampler drops the interval whenever its own sampling gap covers it.
 */
export function activitySummary(history?: ActivityHistory) {
  if (!history) return undefined
  const summary = { up: 0, down: 0 }
  for (const bucket of history.buckets) {
    summary.up += bucket.bytes.up
    summary.down += bucket.bytes.down
  }
  return summary
}
