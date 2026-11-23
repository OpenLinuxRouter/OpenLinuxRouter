import { clientThroughput, readCensusState } from './connections.ts'
import type { CensusState, ClientConnection, GatewayFlow, GatewayFlows } from './queries'

/**
 * The per-flow composition of one client's measured total.
 *
 * A flow rate is the sum of the directions the gateway measured for it, and a
 * share is that rate over the client's measured total. Both are arithmetic on
 * measured values; neither is a guess about what a flow is doing.
 */
export const FLOW_ROW_LIMIT = 8

function rate(value: number | undefined): number | undefined {
  return value != null && Number.isFinite(value) && value >= 0 ? value : undefined
}

function port(value: number | undefined): number | undefined {
  return value != null && Number.isFinite(value) && value >= 0 ? value : undefined
}

export function flowDirections(flow: GatewayFlow): { down?: number; up?: number } {
  return { down: rate(flow.rx_bps), up: rate(flow.tx_bps) }
}

export function flowRate(flow: GatewayFlow): number | undefined {
  const { down, up } = flowDirections(flow)
  if (down == null && up == null) return undefined
  return (down ?? 0) + (up ?? 0)
}

/**
 * A flow's share of a client's total.
 *
 * The denominator is the client's measured throughput. A zero or absent total
 * has no share to state, so the answer is a dash rather than 0%. A share above
 * 100% is left as the arithmetic says rather than clamped: it would mean the
 * two reads were sampled at different moments, and the screen should not hide
 * that.
 */
export function shareOfTotal(flowBps: number | undefined, totalBps: number | undefined): number | undefined {
  if (flowBps == null || !Number.isFinite(flowBps) || flowBps < 0) return undefined
  if (totalBps == null || !Number.isFinite(totalBps) || totalBps <= 0) return undefined
  return flowBps / totalBps
}

export function flowShare(flow: GatewayFlow, client: ClientConnection): number | undefined {
  return shareOfTotal(flowRate(flow), clientThroughput(client))
}

/**
 * Whether the gateway attributed this flow to this client.
 *
 * The name is the gateway's own attribution; an address match is the fallback
 * for a flow that names no client or names a stale one. Matching on address
 * only is safe because the client row and the flow sample come from the same
 * gateway window.
 */
export function isClientFlow(flow: GatewayFlow, client: ClientConnection): boolean {
  const name = flow.client?.trim()
  const clientName = client.name?.trim()
  if (name && clientName && name === clientName) return true
  const ip = client.ip?.trim()
  if (!ip) return false
  return flow.src?.trim() === ip || flow.dst?.trim() === ip
}

export interface PeerEndpoint {
  address?: string
  port?: number
}

/**
 * The flow's address that is not this client.
 *
 * A download arrives from the peer and an upload leaves toward it, so the side
 * that is not the client is the peer. When the client's address is unknown the
 * flow still has two measured endpoints; the drawer shows the pair rather than
 * guessing which one is the peer.
 */
export function flowPeer(flow: GatewayFlow, client: ClientConnection): PeerEndpoint {
  const ip = client.ip?.trim()
  if (!ip) return {}
  if (flow.src?.trim() === ip) return { address: flow.dst, port: port(flow.dport) }
  if (flow.dst?.trim() === ip) return { address: flow.src, port: port(flow.sport) }
  return {}
}

/** The two measured endpoints, for a flow whose peer side cannot be decided. */
export function flowEndpoints(flow: GatewayFlow): string | undefined {
  const src = flow.src?.trim()
  const dst = flow.dst?.trim()
  if (!src && !dst) return undefined
  return `${src || '—'} → ${dst || '—'}`
}

/**
 * The peer's address and port in one technical value. An IPv6 address keeps
 * its brackets so the port is not the last of its colons.
 */
export function peerLabel(peer: PeerEndpoint): string | undefined {
  const address = peer.address?.trim()
  if (!address) return undefined
  const port = peer.port
  if (port == null) return address
  return address.includes(':') ? `[${address}]:${port}` : `${address}:${port}`
}

/** Busiest flow first; a flow with no measured rate goes last. */
export function sortFlows(flows: readonly GatewayFlow[]): GatewayFlow[] {
  return [...flows].sort((a, b) => {
    const av = flowRate(a)
    const bv = flowRate(b)
    if (av == null && bv == null) return 0
    if (av == null) return 1
    if (bv == null) return -1
    if (av !== bv) return bv - av
    const ae = flowEndpoints(a) ?? ''
    const be = flowEndpoints(b) ?? ''
    return ae < be ? -1 : ae > be ? 1 : 0
  })
}

/** The flows towards this client, from the gateway's top-flow sample. */
export function clientFlows(flows: readonly GatewayFlow[], client: ClientConnection): GatewayFlow[] {
  return flows.filter(flow => isClientFlow(flow, client))
}

export interface VisibleFlows {
  visible: GatewayFlow[]
  hidden: number
}

/** The drawer caps its list like the table does, and says how many it held back. */
export function visibleFlows(sorted: readonly GatewayFlow[], expanded: boolean, limit = FLOW_ROW_LIMIT): VisibleFlows {
  if (expanded || sorted.length <= limit) return { visible: [...sorted], hidden: 0 }
  return { visible: sorted.slice(0, limit), hidden: sorted.length - limit }
}

/**
 * True when the response came back at the requested limit.
 *
 * The endpoint reports the flows, not the number it omitted, so this is all the
 * screen can honestly say: the limit was reached, and flows below the cut are
 * not in the response.
 */
export function apiCapReached(returned: number, limit: number): boolean {
  return Number.isFinite(returned) && limit > 0 && returned >= limit
}

export interface FlowsReading {
  flows: GatewayFlow[]
  windowSeconds?: number
  asOf?: string
  state?: CensusState
  /** The daemon's own words for a list that is not a measurement. */
  error?: string
}

function positive(value: number | undefined): number | undefined {
  return value != null && Number.isFinite(value) && value > 0 ? value : undefined
}

/** A response without a flows array is not an empty flow list. */
export function usableFlows(data: GatewayFlows | undefined): FlowsReading | undefined {
  if (!data || !Array.isArray(data.flows)) return undefined
  return {
    flows: data.flows.filter((flow): flow is GatewayFlow => flow != null && typeof flow === 'object'),
    windowSeconds: positive(data.window_seconds),
    asOf: data.as_of && Number.isFinite(Date.parse(data.as_of)) ? data.as_of : undefined,
    state: readCensusState(data.state),
    error: typeof data.error === 'string' && data.error ? data.error : undefined,
  }
}
