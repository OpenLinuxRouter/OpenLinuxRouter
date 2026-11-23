import { useQuery } from '@tanstack/react-query'

import { api, ApiError } from '@/lib/api'

/**
 * One direction's measured capacity baseline.
 *
 * `p99_bps` is bits per second, which is how carriers quote capacity and how
 * the chart's byte rates have to be converted before they are compared. `peak`
 * is published for evidence, not for utilisation: a p99 is the repeatable
 * ceiling, and dividing by a single peak would understate every later reading.
 */
export interface CapacitySide {
  p99_bps: number
  peak_bps: number
  points: number
  /** Omitted when points is 0: there is no window to describe yet. */
  since?: string
}

/**
 * `GET /api/gateway/capacity`, which is being added alongside this page.
 *
 * Absent on a daemon that predates it. The page treats a failed read as "no
 * baseline" rather than as an error: there is nothing an operator can fix here,
 * and a guessed capacity would be worse than a dash.
 */
export interface GatewayCapacity {
  uplink: string
  rx: CapacitySide
  tx: CapacitySide
  state: 'measured' | 'warming'
  as_of: string
}

export const capacityKeys = {
  current: ['gateway', 'capacity'] as const,
}

export function useGatewayCapacity() {
  return useQuery({
    queryKey: capacityKeys.current,
    queryFn: () => api.get<GatewayCapacity>('/api/gateway/capacity'),
    // A 404 is the expected answer on a daemon without the endpoint, so it is
    // not retried. The interval still asks again: the endpoint can arrive with
    // a daemon upgrade while this page is open.
    retry: false,
    staleTime: 30_000,
    refetchInterval: 60_000,
  })
}

/**
 * One client's measured connections, in the gateway's own terms.
 *
 * Rates are bits per second, like the capacity endpoint; the page converts
 * them for display. Every field is optional here because a client row is still
 * useful when one of them is missing — the cell then shows a dash rather than
 * zero.
 */
export interface ConnectionDistribution {
  p50_bps?: number
  p90_bps?: number
  p99_bps?: number
}

export interface ClientConnection {
  ip?: string
  mac?: string
  name?: string
  connections?: number
  tcp?: number
  udp?: number
  half_open?: number
  rx_bps?: number
  tx_bps?: number
  per_connection?: ConnectionDistribution
}

/**
 * Whether the census is a measurement.
 *
 * The sampler publishes `warming` before a second walk gives it a window,
 * `ready` once rates are real, `disabled` when the router's statistics switch
 * is off, and `unavailable` when the connection table could not be read. An
 * older daemon that predates these fields omits them; that is treated as a
 * reading, because the endpoint only answers once it has one.
 */
export type CensusState = 'warming' | 'ready' | 'disabled' | 'unavailable'

/** `GET /api/gateway/connections`, added alongside this page and absent on older daemons. */
export interface GatewayConnections {
  as_of?: string
  window_seconds?: number
  state?: CensusState | string
  /** Why the census is not a measurement, when it isn't. Backend words, kept verbatim. */
  error?: string
  clients?: ClientConnection[]
}

/** The gateway's top-flow sample. `client` is the client name the gateway attributed it to. */
export interface GatewayFlow {
  proto?: string
  src?: string
  dst?: string
  sport?: number
  dport?: number
  state?: string
  rx_bps?: number
  tx_bps?: number
  client?: string
}

/** `GET /api/gateway/flows?limit=20`, added alongside this page and absent on older daemons. */
export interface GatewayFlows {
  as_of?: string
  window_seconds?: number
  state?: CensusState | string
  error?: string
  flows?: GatewayFlow[]
}

/** The flow endpoint's own cap, which the drawer asks for and then reports. */
export const FLOW_API_LIMIT = 20

export const connectionsKeys = {
  current: ['gateway', 'connections'] as const,
  flows: (limit: number) => ['gateway', 'flows', limit] as const,
}

/**
 * A missing endpoint is a different answer from a failed read.
 *
 * Neither one may stop the lanes, but the table and the drawer say which
 * happened: 404 means this daemon does not serve the endpoint, while any other
 * failure means the read itself failed.
 */
export type QueryFailure = 'missing' | 'failed'

export function queryFailure(error: unknown): QueryFailure | undefined {
  if (error == null) return undefined
  return error instanceof ApiError && error.status === 404 ? 'missing' : 'failed'
}

/** The daemon's own words for a failure, kept verbatim so an operator can act on them. */
export function failureReason(error: unknown): string | undefined {
  if (error instanceof ApiError && error.message) return error.message
  if (error instanceof Error && error.message) return error.message
  return undefined
}

export function useGatewayConnections() {
  return useQuery({
    queryKey: connectionsKeys.current,
    queryFn: () => api.get<GatewayConnections>('/api/gateway/connections'),
    // A 404 is the expected answer on a daemon that predates the endpoint, so
    // it is not retried; the interval still asks again so an upgrade appears
    // without a reload.
    retry: false,
    staleTime: 5_000,
    refetchInterval: 10_000,
  })
}

export function useGatewayFlows(limit = FLOW_API_LIMIT, enabled = true) {
  return useQuery({
    queryKey: connectionsKeys.flows(limit),
    queryFn: () => api.get<GatewayFlows>(`/api/gateway/flows?limit=${limit}`),
    enabled,
    retry: false,
    staleTime: 5_000,
    refetchInterval: enabled ? 10_000 : false,
  })
}
