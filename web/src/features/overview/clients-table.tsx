import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router'

import { ChevronRight } from 'lucide-react'

import { OverviewCard, OverviewSection } from './card'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { DeviceIcon } from '@/features/clients/client-icon'
import { lastHeardLabel, lastHeardTitle } from '@/features/clients/last-heard'
import { useClientList } from '@/features/clients/queries'
import type { TrafficView } from '@/features/topology/traffic'
import type { ClientRow } from '@/lib/api-types'
import { t, useLocale } from '@/lib/i18n'
import { formatRate } from '@/lib/utils'

/** Rows shown before the table is expanded. */
const FIRST = 10

/** How often the busiest-first order is allowed to change. */
const RERANK_MS = 5000

function seenAt(device: ClientRow) {
  return device.last_seen ? Date.parse(device.last_seen) : 0
}

/**
 * The devices on the network, as one plain table.
 *
 * Busiest first, because the question a visitor brings to the bottom of the
 * overview is "who is using it"; the rest follow by when they were last heard.
 * The trailing column is that time and never an Online/Offline word, which a
 * device on the LAN has no evidence for (features/clients/last-heard.ts). The
 * first ten rows show; the rest are one click away in place, and the device's
 * own page is its name.
 */
export function ClientsTable({ flows }: { flows: TrafficView }) {
  useLocale()
  const clients = useClientList()
  const [expanded, setExpanded] = useState(false)

  const rate = (device: ClientRow) => {
    const flow = flows.rated ? flows.flowOf(device) : undefined
    return flow ? { down: flow.downRate ?? 0, up: flow.upRate ?? 0 } : undefined
  }
  const live = (clients.data?.clients ?? [])
    .filter(device => device.seen)
    .map(device => ({ device, rate: rate(device) }))

  // The rates move every second and a table that re-sorts every second cannot
  // be read or clicked. The order is sampled every five seconds instead, and
  // held while the pointer is over the table, as the network map does; the
  // figures inside the rows stay live. A device that appears between samples
  // joins at the end until the next one, and one that leaves goes at once.
  const [order, setOrder] = useState<string[]>([])
  const [hovering, setHovering] = useState(false)
  const latest = useRef(live)
  useEffect(() => { latest.current = live })
  const ready = live.length > 0
  useEffect(() => {
    if (hovering || !ready) return
    const rerank = () => setOrder([...latest.current].sort((a, b) => {
      const busy = ((b.rate?.down ?? 0) + (b.rate?.up ?? 0)) - ((a.rate?.down ?? 0) + (a.rate?.up ?? 0))
      return busy || seenAt(b.device) - seenAt(a.device) || (a.device.name || a.device.mac).localeCompare(b.device.name || b.device.mac)
    }).map(row => row.device.mac))
    rerank()
    const timer = setInterval(rerank, RERANK_MS)
    return () => clearInterval(timer)
  }, [hovering, ready])
  const rank = new Map(order.map((mac, index) => [mac, index]))
  const rows = [...live].sort((a, b) => (rank.get(a.device.mac) ?? Infinity) - (rank.get(b.device.mac) ?? Infinity))
  const shown = expanded ? rows : rows.slice(0, FIRST)
  // Zero is a reading, not an absence, but it is the least interesting one in
  // the column, so it steps back rather than disappearing.
  const figure = (value?: number) => value == null ? <span className="text-muted-foreground">—</span>
    : value > 0 ? formatRate(value)
      : <span className="text-muted-foreground/70">0</span>

  return <OverviewSection title={t('Clients')} aside={<Link to="/topology" className="inline-flex items-center gap-0.5 hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring">
    {clients.data ? t('{count} devices', { count: rows.length }) : t('Topology')}
    <ChevronRight aria-hidden className="size-3.5" />
  </Link>}>
  <OverviewCard bodyClassName="gap-0 p-0">

    {clients.isPending
      ? <div className="p-4"><Skeleton className="h-64 w-full" /></div>
      : clients.isError
        ? <p className="p-4 text-xs text-muted-foreground">{t('Unavailable')}</p>
        : rows.length === 0
          ? <p className="p-4 text-xs text-muted-foreground">{t('No clients yet')}</p>
          : <>
              <div onPointerEnter={() => setHovering(true)} onPointerLeave={() => setHovering(false)}>
                <Table className="text-sm [&_td]:px-3 [&_th]:px-3 [&_td:first-child]:pl-4 [&_th:first-child]:pl-4 [&_td:last-child]:pr-4 [&_th:last-child]:pr-4">
                  <TableHeader>
                    <TableRow className="hover:bg-transparent [&>th]:h-9 [&>th]:text-xs [&>th]:font-medium [&>th]:text-muted-foreground">
                      <TableHead className="pl-4">{t('Device')}</TableHead>
                      <TableHead className="hidden md:table-cell">{t('IP address')}</TableHead>
                      <TableHead className="hidden lg:table-cell">{t('Group')}</TableHead>
                      <TableHead className="text-right">↓ {t('Download')}</TableHead>
                      <TableHead className="hidden text-right sm:table-cell">↑ {t('Upload')}</TableHead>
                      <TableHead className="pr-4 text-right">{t('Last heard')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {shown.map(({ device, rate }) => <TableRow key={device.mac}>
                      <TableCell className="w-full max-w-0 pl-4 md:w-auto md:max-w-none">
                        <Link to={`/clients/${encodeURIComponent(device.mac)}`} className="group flex min-w-0 items-center gap-2.5 focus-visible:outline-2 focus-visible:outline-ring">
                          <DeviceIcon icon={device.icon} category={device.category} vendor={device.vendor} vendorKey={device.vendor_key} size="sm" />
                          <span className="min-w-0">
                            <span className="block truncate font-medium group-hover:text-primary">{device.name || device.mac}</span>
                            {(device.model || device.vendor) && <span className="block truncate text-xs text-muted-foreground">{device.model || device.vendor}</span>}
                          </span>
                        </Link>
                      </TableCell>
                      <TableCell className="hidden font-mono text-meta md:table-cell">{device.ips?.[0] ?? '—'}</TableCell>
                      <TableCell className="hidden text-muted-foreground lg:table-cell">{device.group ?? '—'}</TableCell>
                      <TableCell className="text-right tabular-nums">{figure(rate?.down)}</TableCell>
                      <TableCell className="hidden text-right tabular-nums sm:table-cell">{figure(rate?.up)}</TableCell>
                      <TableCell className="pr-4 text-right text-muted-foreground tabular-nums" title={lastHeardTitle(device)}>{lastHeardLabel(device)}</TableCell>
                    </TableRow>)}
                  </TableBody>
                </Table>
              </div>
              {rows.length > FIRST && <div className="flex justify-center border-t border-border p-2">
                <Button variant="quiet" size="sm" onClick={() => setExpanded(!expanded)}>
                  {expanded ? t('Show fewer') : t('Show all {count}', { count: rows.length })}
                </Button>
              </div>}
            </>}
  </OverviewCard>
  </OverviewSection>
}
