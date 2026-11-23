import { useState } from 'react'
import { ArrowDown, ArrowUp } from 'lucide-react'
import { Link } from 'react-router'

import { Button } from '@/components/ui/button'
import { Heading } from '@/components/ui/heading'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { OverviewCard } from '@/features/overview/card'
import { t, useLocale } from '@/lib/i18n'
import { cn, formatRate } from '@/lib/utils'

import { ClientFlows } from './client-drawer'
import {
  clientAddressLabel,
  clientConnections,
  clientDirections,
  clientHref,
  clientName,
  clientP50,
  CLIENT_ROW_LIMIT,
  DEFAULT_CLIENT_SORT,
  isCensusMeasured,
  nextClientSort,
  sameClient,
  sortClients,
  usableConnections,
  visibleClients,
  type ClientSort,
  type ClientSortKey,
} from './connections'
import { when } from './live'
import { LaneEmpty } from './parts'
import { failureReason, queryFailure, useGatewayConnections, type ClientConnection } from './queries'

const SORT_KEYS: ClientSortKey[] = ['name', 'connections', 'throughput', 'p50']

/**
 * The clients behind the line's throughput, under the two lanes.
 *
 * The table's own endpoint reports a ten-second window, so it is not the same
 * window as the lanes above; the caption says which window it is. It opens on
 * measured throughput and holds back rows after the first screenful, because a
 * network of 1,600 clients would otherwise push the lanes off the page.
 */
export function ClientTable() {
  const locale = useLocale()
  const query = useGatewayConnections()
  const connections = query.isError ? undefined : usableConnections(query.data)
  const measured = connections != null && isCensusMeasured(connections.state)
  const [sort, setSort] = useState<ClientSort>(DEFAULT_CLIENT_SORT)
  const [expanded, setExpanded] = useState(false)
  // The clicked row is a snapshot. A fresh reading replaces its figures while
  // the client is still in the table; if it is not, the drawer keeps the
  // snapshot together with the window and time it came from.
  const [selected, setSelected] = useState<{ client: ClientConnection; windowSeconds?: number; asOf?: string }>()

  const failure = queryFailure(query.error)
  const live = selected && connections
    ? connections.clients.find(client => sameClient(client, selected.client))
    : undefined
  const drawer = live
    ? { client: live, windowSeconds: connections?.windowSeconds, asOf: connections?.asOf }
    : selected
  const open = (client: ClientConnection) => setSelected({ client, windowSeconds: connections?.windowSeconds, asOf: connections?.asOf })
  const sorted = sortClients(connections?.clients ?? [], sort)
  const { visible, hidden } = visibleClients(sorted, expanded)
  const measuredTime = connections?.asOf ? when(connections.asOf, locale) : undefined
  const measuredWindow = connections?.windowSeconds != null
    ? measuredTime
      ? t('Measured over the last {count} seconds, as of {time}.', { count: connections.windowSeconds, time: measuredTime })
      : t('Measured over the last {count} seconds.', { count: connections.windowSeconds })
    : measuredTime
      ? t('Measured as of {time}.', { time: measuredTime })
      : t('Measured by the gateway.')

  let unavailable: string | undefined
  if (query.isPending) unavailable = t('Reading client connections…')
  else if (failure === 'missing') unavailable = t('Client connections are unavailable: this daemon does not serve /api/gateway/connections. The lanes above are unaffected.')
  else if (query.isError) {
    const reason = failureReason(query.error)
    unavailable = reason
      ? t('Client connections could not be read: {reason}', { reason })
      : t('Client connections could not be read. The lanes above are unaffected.')
  }
  else if (!connections) unavailable = t('The client connections response could not be read. The lanes above are unaffected.')
  else if (connections.state === 'warming') unavailable = t('The connection census is warming up. A complete window is not available yet.')
  else if (connections.state === 'disabled') {
    unavailable = connections.error
      ? t('The connection census is disabled: {reason}', { reason: connections.error })
      : t('The connection census is disabled.')
  }
  else if (connections.state === 'unavailable') {
    unavailable = connections.error
      ? t('The connection census could not be read: {reason}', { reason: connections.error })
      : t('The connection census could not be read.')
  }
  else if (connections.clients.length === 0) {
    unavailable = connections.windowSeconds != null
      ? t('No client had an open connection in the last {count} seconds.', { count: connections.windowSeconds })
      : t('No client had an open connection in the measured window.')
  }

  return <>
    <OverviewCard bodyClassName="gap-3">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <Heading as="h2" size="sm">{t('Clients')}</Heading>
        {measured && <span className="text-xs text-muted-foreground/80">{t('Measured')}</span>}
      </div>
      {measured && !unavailable && <>
        <div className="space-y-0.5">
          <p className="text-xs leading-4 text-muted-foreground">{measuredWindow}</p>
          <p className="text-xs leading-4 text-muted-foreground">{t('Sorted by {column}. Select a client row for its top flows.', { column: sortLabel(sort.key) })}</p>
        </div>

        <div className="flex flex-wrap gap-1.5 sm:hidden">
          {SORT_KEYS.map(key => <Button
            key={key}
            variant={sort.key === key ? 'secondary' : 'quiet'}
            size="sm"
            className="px-2"
            aria-pressed={sort.key === key}
            onClick={() => setSort(nextClientSort(sort, key))}
          >
            {sortLabel(key)}
            {sort.key === key && (sort.direction === 'desc'
              ? <ArrowDown aria-hidden className="size-3" />
              : <ArrowUp aria-hidden className="size-3" />)}
          </Button>)}
        </div>

        <div className="hidden sm:block">
          <Table aria-label={t('Client connections')}>
            <TableHeader>
              <TableRow>
                <SortHead label={t('Client')} column="name" sort={sort} onSort={setSort} />
                <SortHead label={t('Connections')} column="connections" sort={sort} onSort={setSort} align="right" />
                <SortHead label={t('Throughput')} column="throughput" sort={sort} onSort={setSort} align="right" />
                <SortHead label={t('p50 per connection')} column="p50" sort={sort} onSort={setSort} align="right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {visible.map((client, index) => <ClientRow key={client.mac ?? client.ip ?? client.name ?? index} client={client} onSelect={open} />)}
            </TableBody>
          </Table>
        </div>

        <ul className="divide-y divide-border sm:hidden">
          {visible.map((client, index) => <ClientCard key={client.mac ?? client.ip ?? client.name ?? index} client={client} onSelect={open} />)}
        </ul>

        <div className="flex flex-wrap items-center justify-between gap-2">
          <p className="text-xs text-muted-foreground">
            {hidden > 0
              ? t('Showing {shown} of {total} clients.', { shown: visible.length, total: sorted.length })
              : t('Showing all {total} clients.', { total: sorted.length })}
          </p>
          {sorted.length > CLIENT_ROW_LIMIT && <Button variant="quiet" size="sm" onClick={() => setExpanded(value => !value)}>
            {expanded ? t('Show fewer') : t('{count} more', { count: sorted.length - CLIENT_ROW_LIMIT })}
          </Button>}
        </div>

        <p className="border-t border-border pt-2 text-xs leading-4 text-muted-foreground">
          {t('Connection counts, rates and per-connection p50 are measured over the window above. A dash means the gateway did not report that value.')}
        </p>
      </>}
      {unavailable && <LaneEmpty>{unavailable}</LaneEmpty>}
    </OverviewCard>

    <ClientFlows
      client={drawer?.client}
      windowSeconds={drawer?.windowSeconds}
      asOf={drawer?.asOf}
      onClose={() => setSelected(undefined)}
    />
  </>
}

function sortLabel(key: ClientSortKey): string {
  switch (key) {
    case 'name':
      return t('Client')
    case 'connections':
      return t('Connections')
    case 'throughput':
      return t('Throughput')
    case 'p50':
      return t('p50')
  }
}

function SortHead({ label, column, sort, onSort, align }: {
  label: string
  column: ClientSortKey
  sort: ClientSort
  onSort: (sort: ClientSort) => void
  align?: 'right'
}) {
  const active = sort.key === column
  return <TableHead
    aria-sort={active ? sort.direction === 'asc' ? 'ascending' : 'descending' : 'none'}
    className={cn('text-xs whitespace-normal sm:text-sm', align === 'right' && 'text-right')}
  >
    <button
      type="button"
      onClick={() => onSort(nextClientSort(sort, column))}
      className={cn(
        'inline-flex items-center gap-1 rounded-sm text-left font-medium text-muted-foreground transition-colors duration-150 hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
        active && 'text-foreground',
        align === 'right' && 'justify-end',
      )}
    >
      {label}
      {active && (sort.direction === 'desc'
        ? <ArrowDown aria-hidden className="size-3" />
        : <ArrowUp aria-hidden className="size-3" />)}
    </button>
  </TableHead>
}

/**
 * The same row, stacked for phones.
 *
 * A four-column table at 375px either scrolls the throughput column out of
 * sight or truncates the client's name. The stacked record keeps every value
 * and the sort key visible, and the table takes over from 640px.
 */
function ClientCard({ client, onSelect }: { client: ClientConnection; onSelect: (client: ClientConnection) => void }) {
  const locale = useLocale()
  const name = clientName(client) ?? '—'
  const href = clientHref(client)
  const { down, up } = clientDirections(client)
  const connections = clientConnections(client)
  const p50 = clientP50(client)
  const rate = (bps: number | undefined) => bps == null ? '—' : formatRate(bps / 8)
  const meta = [
    client.ip?.trim(),
    connections == null ? undefined : t('{count} connections', { count: connections.toLocaleString(locale) }),
  ].filter(Boolean).join(' · ') || '—'

  return <li className="py-0.5">
    <div
      tabIndex={0}
      onClick={() => onSelect(client)}
      onKeyDown={event => {
        if (event.target !== event.currentTarget) return
        if (event.key !== 'Enter' && event.key !== ' ') return
        event.preventDefault()
        onSelect(client)
      }}
      className="-mx-2 cursor-pointer rounded-md px-2 py-2 transition-colors duration-150 hover:bg-muted/50 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-ring"
    >
      <div className="flex items-baseline justify-between gap-3">
        {href
          ? <Link
              to={href}
              onClick={event => event.stopPropagation()}
              onKeyDown={event => event.stopPropagation()}
              className="block min-w-0 truncate font-medium text-foreground underline-offset-4 hover:text-primary hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            >
              {name}
            </Link>
          : <span className="block min-w-0 truncate font-medium text-foreground">{name}</span>}
        <span className="shrink-0 font-mono text-xs tabular-nums">
          <span className="text-receive">↓ {rate(down)}</span>
          <span className="ml-2 text-transmit">↑ {rate(up)}</span>
        </span>
      </div>
      <div className="mt-0.5 flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
        <span className="min-w-0 font-mono">{meta}</span>
        <span className="shrink-0 tabular-nums">{t('p50 per connection')}: {p50 == null ? '—' : formatRate(p50 / 8)}</span>
      </div>
    </div>
  </li>
}

function ClientRow({ client, onSelect }: { client: ClientConnection; onSelect: (client: ClientConnection) => void }) {
  const locale = useLocale()
  const name = clientName(client) ?? '—'
  const href = clientHref(client)
  const address = clientAddressLabel(client)
  const { down, up } = clientDirections(client)
  const connections = clientConnections(client)
  const p50 = clientP50(client)
  const rate = (bps: number | undefined) => bps == null ? '—' : formatRate(bps / 8)

  return <TableRow
    tabIndex={0}
    onClick={() => onSelect(client)}
    onKeyDown={event => {
      if (event.target !== event.currentTarget) return
      if (event.key !== 'Enter' && event.key !== ' ') return
      event.preventDefault()
      onSelect(client)
    }}
    className="cursor-pointer focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-ring"
  >
    <TableCell className="max-w-[14rem] py-2.5">
      <div className="min-w-0">
        {href
          ? <Link
              to={href}
              onClick={event => event.stopPropagation()}
              onKeyDown={event => event.stopPropagation()}
              className="block truncate font-medium text-foreground underline-offset-4 hover:text-primary hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            >
              {name}
            </Link>
          : <span className="block truncate font-medium text-foreground">{name}</span>}
        {address && <span className="mt-0.5 block truncate font-mono text-xs text-muted-foreground">{address}</span>}
      </div>
    </TableCell>
    <TableCell className="text-right tabular-nums">
      {connections == null ? '—' : connections.toLocaleString(locale)}
    </TableCell>
    <TableCell className="py-2.5 text-right">
      <div className="flex flex-col items-end font-mono text-xs leading-5 tabular-nums">
        <span className="text-receive">↓ {rate(down)}</span>
        <span className="text-transmit">↑ {rate(up)}</span>
      </div>
    </TableCell>
    <TableCell className="text-right font-mono text-xs tabular-nums">
      {p50 == null ? '—' : formatRate(p50 / 8)}
    </TableCell>
  </TableRow>
}
