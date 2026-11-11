import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { GatewayApplyResult, GatewayLatency, GatewayPlan, GatewayRoutes, GatewayStatus, GatewayTraffic } from '@/lib/api-types'
import type { GatewayConfig } from '@/lib/config-types'

import { TRAFFIC_SAMPLE_SAVE_MS, writeTrafficSample } from './traffic-sample'

// The same polling story as the other modules: EventSource cannot send an
// Authorization header, so streaming from the browser needs a cookie session or
// a fetch-based reader, and neither is worth inventing before there is live
// data to carry.
//
// This screen has one reason to poll the others do not: an exit's health is
// decided by a background prober inside olrd, so the answer can change with
// nobody having clicked anything. "Living Room TV: no internet — Proxy is down"
// is only useful if it appears on its own.
const OBSERVED_REFETCH_MS = 5000

// Byte counts refetch at half that rate, because they are not the same question.
//
// An exit going down is worth knowing within seconds: nothing else on screen
// would say so. A running total is worth knowing whenever the eye lands on it,
// and producing it costs a walk of every device on the network. Polling both at
// one interval meant paying for the expensive one at the cadence the cheap one
// needed.
const TRAFFIC_REFETCH_MS = 10000

export const gatewayKeys = {
  config: ['gateway', 'config'] as const,
  status: ['gateway', 'status'] as const,
  traffic: ['gateway', 'traffic'] as const,
  routes: ['gateway', 'routes'] as const,
}

/**
 * The routing table, read from the kernel.
 *
 * Its own query because it answers a different question from /status: that one
 * is about what OLR wrote and whether it stuck, this one is about every table
 * on the box, including the ones nobody here wrote.
 */
export function useGatewayRoutes() {
  return useQuery({
    queryKey: gatewayKeys.routes,
    queryFn: () => api.get<GatewayRoutes>('/api/gateway/routes'),
  })
}

export function useGatewayConfig() {
  return useQuery({
    queryKey: gatewayKeys.config,
    queryFn: () => api.get<GatewayConfig>('/api/gateway/config'),
    structuralSharing: false,
  })
}

export function useGatewayStatus() {
  return useQuery({
    queryKey: gatewayKeys.status,
    queryFn: () => api.get<GatewayStatus>('/api/gateway/status'),
    // Observed state, never cached by the daemon (design.md §4.5), so the only
    // way to stay current is to ask again.
    refetchInterval: OBSERVED_REFETCH_MS,
  })
}

/** When the last reading was kept for the next visit; see traffic-sample.ts. */
let trafficSampleSavedAt = 0

/**
 * Per-device byte counts, read from the kernel on every request.
 *
 * Its own query rather than part of the status one, because it costs a walk of
 * every device on the network and is wanted at a different rate.
 *
 * Writing the reading on every tick would be work for a value only a reload
 * reads, so it is saved at most every few seconds. A slightly older reading
 * still differences against the counters just as well.
 */
export function useGatewayTraffic(refetchInterval = TRAFFIC_REFETCH_MS) {
  return useQuery({
    queryKey: gatewayKeys.traffic,
    queryFn: async () => {
      const next = await api.get<GatewayTraffic>('/api/gateway/traffic')
      if (Date.now() - trafficSampleSavedAt >= TRAFFIC_SAMPLE_SAVE_MS) {
        writeTrafficSample(next)
        trafficSampleSavedAt = Date.now()
      }
      return next
    },
    refetchInterval,
  })
}

/**
 * One change, described the way the API names it.
 *
 * A value rather than a call, because a change has to survive being held: when
 * the daemon answers 409 because the change would move traffic that is flowing,
 * the page shows the plan and then sends *the same change* again with
 * `confirm=true`. Replaying is only simple if the change is data.
 */
export interface GatewayChange {
  method: 'PUT' | 'DELETE' | 'PATCH'
  path: string
  body?: unknown
}

const base = '/api/gateway'
const seg = (s: string) => encodeURIComponent(s)

/**
 * The changes this screen can make, each naming the thing it changes.
 *
 * The split is not arbitrary. RFC 7386 merges an object key by key but replaces
 * an array wholesale, so the scalars go through PATCH and the lists get a route
 * per item. It is also what keeps the rules on the daemon's side: `saveExit`
 * posts the new exit to the *old* name's path, and renaming every reference
 * along with it is Config.Rename's job, not this file's.
 */
export const gatewayChange = {
  client: (mac: string, exit: string): GatewayChange => ({
    method: 'PUT',
    path: `${base}/clients/${seg(mac)}`,
    body: { exit },
  }),
  removeClient: (mac: string): GatewayChange => ({
    method: 'DELETE',
    path: `${base}/clients/${seg(mac)}`,
  }),
  saveExit: (name: string, exit: unknown): GatewayChange => ({
    method: 'PUT',
    path: `${base}/exits/${seg(name)}`,
    body: exit,
  }),
  removeExit: (name: string): GatewayChange => ({
    method: 'DELETE',
    path: `${base}/exits/${seg(name)}`,
  }),
  assign: (iface: string, exit: string): GatewayChange => ({
    method: 'PUT',
    path: `${base}/assignments/${seg(iface)}`,
    body: { exit },
  }),
  removeAssignment: (iface: string): GatewayChange => ({
    method: 'DELETE',
    path: `${base}/assignments/${seg(iface)}`,
  }),
  settings: (fields: Partial<GatewayConfig>): GatewayChange => ({
    method: 'PATCH',
    path: `${base}/config`,
    body: fields,
  }),
}

/**
 * Sends one change, optionally already confirmed.
 *
 * There is no separate preview call. The daemon plans before it writes and
 * refuses on its own when the plan is disruptive, so the round trip that used to
 * be spent asking is only spent when the answer is "ask the operator" — which on
 * this screen is the one answer worth waiting for. Every other edit lands first
 * time.
 */
export function useApplyGatewayChange() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({ change, confirm }: { change: GatewayChange; confirm?: boolean }) =>
      api.send<GatewayApplyResult>(
        change.method,
        confirm ? `${change.path}?confirm=true` : change.path,
        change.body,
      ),
    onSettled: () => {
      // Invalidated on failure too. A partial apply changed the kernel, so
      // every observed answer is stale whether or not the call succeeded.
      queryClient.invalidateQueries({ queryKey: ['gateway'] })
    },
  })
}

/**
 * Re-applies stored intent unchanged, which is the repair path design.md
 * §5.3.2 offers in place of a rollback: if an apply failed halfway, or somebody
 * ran `ip rule del` by hand, this finishes the job.
 */
export function usePlanGatewayRepair() {
  return useMutation({
    mutationFn: () => api.post<GatewayPlan>('/api/gateway/plan'),
  })
}

export function useReapplyGateway() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => api.post<GatewayApplyResult>('/api/gateway/apply', undefined),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['gateway'] }),
  })
}

const LATENCY_SNAPSHOT_KEY = 'olr.gateway.latency-hourly-v1'

function savedLatency(): GatewayLatency | undefined {
  try {
    const stored = sessionStorage.getItem(LATENCY_SNAPSHOT_KEY)
    if (!stored) return undefined
    const snapshot = JSON.parse(stored) as GatewayLatency
    // A saved spinner belongs to the page that is gone. Only the daemon can say
    // whether a probe is running now.
    return Array.isArray(snapshot.custom)
      ? { ...snapshot, custom: snapshot.custom.map((site) => ({ ...site, measuring: false })) }
      : undefined
  } catch { return undefined }
}

export function useGatewayLatency() {
  return useQuery({
    queryKey: ['gateway', 'latency'],
    initialData: savedLatency,
    queryFn: async () => {
      const next = await api.get<GatewayLatency>('/api/gateway/latency')
      try { sessionStorage.setItem(LATENCY_SNAPSHOT_KEY, JSON.stringify(next)) } catch { /* storage may be disabled */ }
      return next
    },
    // Poll faster during a manual test; automatic hourly samples arrive through
    // the normal passive reads, which also advance the router's day and hour.
    refetchInterval: (query) => query.state.data?.custom.some((site) => site.measuring) ? 1000 : OBSERVED_REFETCH_MS,
  })
}

/**
 * Probe one monitored site now, rather than at the next background round.
 *
 * The response carries the snapshot with that site's `measuring` set; the
 * follow-up invalidation starts the faster poll at a reading newer than the
 * click, so the spinner ends when this probe lands instead of at the previous
 * five-second tick.
 */
export function useMeasureLatencySite() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => api.post<GatewayLatency>(`/api/gateway/latency/sites/${encodeURIComponent(name)}/measure`),
    onSuccess: (snapshot) => {
      queryClient.setQueryData(['gateway', 'latency'], snapshot)
      queryClient.invalidateQueries({ queryKey: ['gateway', 'latency'] })
    },
  })
}

export function usePinLatencySite() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ name, pinned }: { name: string; pinned: boolean }) => api.put<GatewayLatency>(`/api/gateway/latency/sites/${encodeURIComponent(name)}/pin`, { pinned }),
    onMutate: () => queryClient.cancelQueries({ queryKey: ['gateway', 'latency'] }),
    onSuccess: (snapshot) => {
      queryClient.setQueryData(['gateway', 'latency'], snapshot)
      try { sessionStorage.setItem(LATENCY_SNAPSHOT_KEY, JSON.stringify(snapshot)) } catch { /* storage may be disabled */ }
      queryClient.invalidateQueries({ queryKey: ['gateway', 'config'] })
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['gateway', 'latency'] }),
  })
}

export function useSaveLatencySites() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ sites, target }: { sites: { name: string; url: string; exit?: string; icon?: string }[]; target?: string }) => {
      await api.put('/api/gateway/latency/sites', sites)
      if (target !== undefined) await api.put('/api/gateway/latency/target', { target })
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['gateway', 'latency'] }),
  })
}
