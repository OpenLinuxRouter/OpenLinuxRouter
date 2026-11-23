import type { CensusState, ClientConnection, GatewayConnections } from './queries'

/**
 * The client table is a table, not a leaderboard.
 *
 * A network of 1,600 clients must not push the two lanes off screen, so the
 * table shows a fixed number of rows and reveals the rest on request. These
 * values are the pure part: sorting, which rows are visible, and which fields
 * the endpoint actually reported. Nothing here defaults a missing field to
 * zero.
 */
export const CLIENT_ROW_LIMIT = 8

export type ClientSortKey = 'name' | 'connections' | 'throughput' | 'p50'
export type SortDirection = 'asc' | 'desc'

export interface ClientSort {
  key: ClientSortKey
  direction: SortDirection
}

/** The table opens on measured throughput, busiest first, as the page leads. */
export const DEFAULT_CLIENT_SORT: ClientSort = { key: 'throughput', direction: 'desc' }

export interface VisibleClients {
  visible: ClientConnection[]
  hidden: number
}

function rate(value: number | undefined): number | undefined {
  return value != null && Number.isFinite(value) && value >= 0 ? value : undefined
}

/** Download and upload as the endpoint reported them, in bits per second. */
export function clientDirections(client: ClientConnection): { down?: number; up?: number } {
  return { down: rate(client.rx_bps), up: rate(client.tx_bps) }
}

/**
 * The figure the throughput column is sorted by.
 *
 * Both directions are measured in the same window, so their sum is the client's
 * measured throughput. A direction the endpoint omitted contributes nothing to
 * the sum but also is not invented as zero in the cell; when neither direction
 * was reported the total is absent, not zero.
 */
export function clientThroughput(client: ClientConnection): number | undefined {
  const { down, up } = clientDirections(client)
  if (down == null && up == null) return undefined
  return (down ?? 0) + (up ?? 0)
}

export function clientConnections(client: ClientConnection): number | undefined {
  const value = client.connections
  return value != null && Number.isFinite(value) && value >= 0 ? value : undefined
}

export function clientP50(client: ClientConnection): number | undefined {
  return rate(client.per_connection?.p50_bps)
}

/** The name to show, falling back to an address when no name was reported. */
export function clientName(client: ClientConnection): string | undefined {
  const name = client.name?.trim()
  if (name) return name
  const ip = client.ip?.trim()
  if (ip) return ip
  const mac = client.mac?.trim()
  return mac || undefined
}

/** The client page is keyed by hardware address. */
export function clientHref(client: ClientConnection): string | undefined {
  const mac = client.mac?.trim()
  return mac ? `/clients/${encodeURIComponent(mac)}` : undefined
}

/**
 * Whether a later reading describes the same client row.
 *
 * The hardware address is the stable identity; an address or name is the
 * fallback for a row that did not report one. This keeps an open drawer on the
 * current figures rather than on the moment it was opened.
 */
export function sameClient(a: ClientConnection, b: ClientConnection): boolean {
  const am = a.mac?.trim()
  const bm = b.mac?.trim()
  if (am && bm) return am === bm
  const ai = a.ip?.trim()
  const bi = b.ip?.trim()
  if (ai && bi) return ai === bi
  const an = a.name?.trim()
  const bn = b.name?.trim()
  return Boolean(an && bn && an === bn)
}

/** The address line under the name: whichever identifiers the endpoint sent. */
export function clientAddressLabel(client: ClientConnection): string | undefined {
  const parts = [client.ip?.trim(), client.mac?.trim()]
    .filter((part): part is string => Boolean(part))
  return parts.length ? [...new Set(parts)].join(' · ') : undefined
}

function sortValue(client: ClientConnection, key: ClientSortKey): string | number | undefined {
  switch (key) {
    case 'name':
      return clientName(client)?.toLowerCase()
    case 'connections':
      return clientConnections(client)
    case 'throughput':
      return clientThroughput(client)
    case 'p50':
      return clientP50(client)
  }
}

function tiebreak(a: ClientConnection, b: ClientConnection): number {
  const an = clientName(a)?.toLowerCase() ?? ''
  const bn = clientName(b)?.toLowerCase() ?? ''
  if (an !== bn) return an < bn ? -1 : 1
  const aa = clientAddressLabel(a) ?? ''
  const ba = clientAddressLabel(b) ?? ''
  if (aa !== ba) return aa < ba ? -1 : 1
  return 0
}

/**
 * A stable sort by the chosen column.
 *
 * Missing values are always last, in both directions: a client whose rate the
 * endpoint did not report is not the quietest client, and reversing the sort
 * must not put it first. Ties fall back to the client's name so the row order
 * does not shuffle between reads.
 */
export function sortClients(clients: readonly ClientConnection[], sort: ClientSort = DEFAULT_CLIENT_SORT): ClientConnection[] {
  return [...clients].sort((a, b) => {
    const av = sortValue(a, sort.key)
    const bv = sortValue(b, sort.key)
    if (av == null && bv == null) return tiebreak(a, b)
    if (av == null) return 1
    if (bv == null) return -1
    const compared = typeof av === 'number' && typeof bv === 'number'
      ? av - bv
      : String(av) < String(bv) ? -1 : String(av) > String(bv) ? 1 : 0
    if (compared !== 0) return sort.direction === 'asc' ? compared : -compared
    return tiebreak(a, b)
  })
}

/** The next sort state for a column header: a new column starts on its natural end. */
export function nextClientSort(sort: ClientSort, key: ClientSortKey): ClientSort {
  if (sort.key === key) {
    return { key, direction: sort.direction === 'desc' ? 'asc' : 'desc' }
  }
  return { key, direction: key === 'name' ? 'asc' : 'desc' }
}

/** The rows shown, and how many the cap holds back. */
export function visibleClients(sorted: readonly ClientConnection[], expanded: boolean, limit = CLIENT_ROW_LIMIT): VisibleClients {
  if (expanded || sorted.length <= limit) return { visible: [...sorted], hidden: 0 }
  return { visible: sorted.slice(0, limit), hidden: sorted.length - limit }
}

function validTime(iso: string | undefined): string | undefined {
  return iso && Number.isFinite(Date.parse(iso)) ? iso : undefined
}

function positive(value: number | undefined): number | undefined {
  return value != null && Number.isFinite(value) && value > 0 ? value : undefined
}

export interface ConnectionsReading {
  clients: ClientConnection[]
  windowSeconds?: number
  asOf?: string
  state?: CensusState
  /** The daemon's own words for a census that is not a measurement. */
  error?: string
}

/** An unknown non-empty state is not a measurement. */
export function readCensusState(value: string | undefined): CensusState | undefined {
  if (value == null || value === '') return undefined
  if (value === 'warming' || value === 'ready' || value === 'disabled' || value === 'unavailable') return value
  return 'unavailable'
}

/** Absent state is the contract before the sampler published one; only `ready` is measured. */
export function isCensusMeasured(state: CensusState | undefined): boolean {
  return state == null || state === 'ready'
}

/**
 * A usable reading, or nothing.
 *
 * An empty `clients` list is a measured empty answer and stays one. A response
 * without a clients array is not an empty answer, so it does not render a table
 * that would claim there were no connections.
 */
export function usableConnections(data: GatewayConnections | undefined): ConnectionsReading | undefined {
  if (!data || !Array.isArray(data.clients)) return undefined
  return {
    clients: data.clients.filter((client): client is ClientConnection => client != null && typeof client === 'object'),
    windowSeconds: positive(data.window_seconds),
    asOf: validTime(data.as_of),
    state: readCensusState(data.state),
    error: typeof data.error === 'string' && data.error ? data.error : undefined,
  }
}
