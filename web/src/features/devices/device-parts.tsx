import { t, useLocale } from '@/lib/i18n'
import { TriangleAlert } from 'lucide-react'
import { Link } from 'react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Disclosure } from '@/components/ui/disclosure'
import { Heading } from '@/components/ui/heading'
import { ListEmpty } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusInline } from '@/components/ui/status'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { state } from '@/lib/state'
import { cn } from '@/lib/utils'

import { counters } from './device-format'
import {
  adapterLabel,
  capabilityWord,
  collectionWord,
  duplexLabel,
  formatCount,
  formatSpeed,
  portKindLabel,
  portLabel,
  portState,
  sortPorts,
} from './device'
import type { Collection, Device, Port, Snapshot } from './types'

/**
 * The pieces a device page shares with its siblings, and nothing else.
 *
 * A port is a port on a switch, an access point and a fiber ONT, so the table
 * that draws one is shared. Tabs, columns and vocabulary are not: each view
 * below (`switch-page`, `openwrt-page`, `ont-page`) owns those, because a
 * carrier VLAN on a WAN service and a switch-port VLAN membership are different
 * facts that happen to share a word.
 */

export interface ViewTab {
  id: string
  label: string
}

/**
 * The tab strip plus whatever the view puts at its right end. The read time is
 * an action here rather than a page subtitle because it belongs to the tab you
 * are looking at, not to the device.
 */
export function ViewHeader({ tabs, tab, onChange, actions }: {
  tabs: ViewTab[]
  tab: string
  onChange: (id: string) => void
  actions?: React.ReactNode
}) {
  useLocale()
  return (
    <div className="flex flex-wrap items-end justify-between gap-3">
      <div role="tablist" aria-label={t('Device views')} className="flex gap-1 border-b border-border">
        {tabs.map(item => (
          <button
            key={item.id}
            role="tab"
            type="button"
            aria-selected={tab === item.id}
            onClick={() => onChange(item.id)}
            className={cn(
              '-mb-px border-b-2 px-3 py-2 text-sm transition-colors duration-150 focus-visible:outline-2 focus-visible:outline-ring',
              tab === item.id ? 'border-primary font-medium text-primary' : 'border-transparent text-muted-foreground hover:text-foreground',
            )}
          >
            {item.label}
          </button>
        ))}
      </div>
      {actions && <div className="flex items-center gap-3 pb-2">{actions}</div>}
    </div>
  )
}

/** One label/value row. An absent value is a row that does not exist. */
export function Fact({ label, value }: { label: string; value?: React.ReactNode }) {
  if (value === undefined || value === null || value === '') return null
  return (
    <div className="grid min-h-9 grid-cols-[8rem_minmax(0,1fr)] items-center gap-3 py-1 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-right [overflow-wrap:anywhere]">{value}</dd>
    </div>
  )
}

export interface FactRow {
  label: string
  value?: React.ReactNode
}

/** Facts as one divided column; what a reading did not report is simply absent. */
export function FactList({ facts, empty }: { facts: FactRow[]; empty?: string }) {
  useLocale()
  const present = facts.filter(fact => fact.value !== undefined && fact.value !== null && fact.value !== '')
  if (present.length === 0) {
    return <p className="text-sm leading-6 text-muted-foreground">{empty ?? t('This device did not report its identity.')}</p>
  }
  return (
    <dl className="divide-y divide-border/60">
      {present.map(fact => <Fact key={fact.label} label={fact.label} value={fact.value} />)}
    </dl>
  )
}

/** A section this read could not obtain, with the adapter's own reason. */
export function Unread({ collection, what }: { collection: Collection; what: string }) {
  useLocale()
  const health = state(collectionWord(collection.state))
  return (
    <Alert>
      <TriangleAlert />
      <AlertTitle>{health.label}</AlertTitle>
      <AlertDescription>{collection.error ?? t('This device did not report its {what}.', { what })}</AlertDescription>
    </Alert>
  )
}

const COLLECTION_LABELS: Record<string, string> = {
  identity: 'Identity',
  ports: 'Ports',
  optical: 'Optical module',
  wan: 'WAN services',
  statistics: 'Counter statistics',
  vlans: 'VLANs',
  pvids: 'Port PVIDs',
  fdb: 'MAC address table',
  neighbors: 'Neighbours',
  radios: 'Radios',
  wireless: 'Wireless networks',
  associations: 'Wireless clients',
}

/** The sections in the order a reader looks for them, not alphabetically. */
const COLLECTION_ORDER = ['identity', 'ports', 'optical', 'wan', 'statistics', 'vlans', 'pvids', 'fdb', 'neighbors', 'radios', 'wireless', 'associations']

function collectionRank(name: string): number {
  const index = COLLECTION_ORDER.indexOf(name)
  return index === -1 ? COLLECTION_ORDER.length : index
}

/**
 * What this read obtained, section by section. A failed section is named as
 * failed: a switch whose VLAN page the adapter could not parse is not a switch
 * without VLANs.
 */
export function CollectionCard({ device, snapshot }: { device: Device; snapshot: Snapshot }) {
  useLocale()
  const collections = Object.entries(snapshot.collections).sort(([a], [b]) => collectionRank(a) - collectionRank(b))
  return (
    <Card className="gap-0 py-0">
      <div className="space-y-3 p-5">
        <div className="flex items-baseline justify-between gap-3">
          <Heading as="h2" size="sm">{t('What this read could obtain')}</Heading>
          <span className="text-xs text-muted-foreground">{t(adapterLabel(device.adapter))}</span>
        </div>
        <div className="space-y-2">
          {collections.map(([name, collection]) => <CollectionRead key={name} name={name} collection={collection} />)}
        </div>
      </div>
    </Card>
  )
}

function CollectionRead({ name, collection }: { name: string; collection: Collection }) {
  useLocale()
  const health = state(collectionWord(collection.state))
  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between gap-3">
        <span className="text-sm">{t(COLLECTION_LABELS[name] ?? name)}</span>
        <StatusInline tone={health.tone}>{health.label}</StatusInline>
      </div>
      {collection.error && <p className="text-xs leading-4 text-muted-foreground">{collection.error}</p>}
    </div>
  )
}

/**
 * The adapter's own answer to "why is that missing". Kept as a disclosure so it
 * is available without being the first thing a reader has to get through.
 */
export function CapabilitiesDisclosure({ snapshot }: { snapshot: Snapshot }) {
  useLocale()
  const capabilities = Object.entries(snapshot.capabilities).sort(([a], [b]) => a.localeCompare(b))
  if (capabilities.length === 0) return null
  return (
    <Disclosure summary={t('What this device can report')}>
      <div className="divide-y divide-border rounded-lg border bg-card">
        {capabilities.map(([key, capability]) => {
          const health = state(capabilityWord(capability.state))
          return (
            <div key={key} className="px-3 py-2">
              <div className="flex items-center justify-between gap-3">
                <span className="min-w-0 truncate font-mono text-[0.8125rem]">{key}</span>
                <StatusInline tone={health.tone}>{health.label}</StatusInline>
              </div>
              {capability.reason && <p className="mt-1 text-xs leading-4 text-muted-foreground">{capability.reason}</p>}
            </div>
          )
        })}
      </div>
    </Disclosure>
  )
}

/**
 * The port table. Speeds and counters are the device's own; a port with no
 * reported counter shows a dash, because "unknown" and "zero" are different
 * claims about a switch.
 *
 * A column no row fills is dropped rather than drawn as a stack of dashes: an
 * ONT's LAN ports report counters and link state but never error counters, and
 * an empty Errors column would claim it had looked.
 */
export function PortsTable({ snapshot }: { snapshot: Snapshot }) {
  useLocale()
  const ports = sortPorts(snapshot.ports ?? [])
  const read = snapshot.collections.ports
  if (read && read.state !== 'ok') return <Unread collection={read} what={t('port table')} />
  if (ports.length === 0) return <ListEmpty>{t('This device reported no ports.')}</ListEmpty>

  const columns: PortColumns = {
    speed: ports.some(port => port.speed_mbps != null || Boolean(port.duplex)),
    received: ports.some(port => port.rx_bytes != null || port.rx_packets != null),
    sent: ports.some(port => port.tx_bytes != null || port.tx_packets != null),
    errors: ports.some(port => port.rx_errors != null || port.tx_errors != null),
  }

  return (
    <div className="space-y-3">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Port')}</TableHead>
            <TableHead className="hidden sm:table-cell">{t('Type')}</TableHead>
            <TableHead>{t('State')}</TableHead>
            {columns.speed && <TableHead className="text-right">{t('Speed')}</TableHead>}
            {columns.received && <TableHead className="hidden text-right md:table-cell">{t('Received')}</TableHead>}
            {columns.sent && <TableHead className="hidden text-right md:table-cell">{t('Sent')}</TableHead>}
            {columns.errors && <TableHead className="hidden text-right sm:table-cell">{t('Errors')}</TableHead>}
          </TableRow>
        </TableHeader>
        <TableBody>
          {ports.map(port => <PortRow key={port.id} port={port} columns={columns} />)}
        </TableBody>
      </Table>
      <p className="text-xs leading-5 text-muted-foreground">{t('Counters are the device’s own totals. They can reset or wrap, and a counter the device does not report is unknown rather than zero.')}</p>
    </div>
  )
}

/** Which of the optional port columns at least one row would fill. */
interface PortColumns {
  speed: boolean
  received: boolean
  sent: boolean
  errors: boolean
}

function PortRow({ port, columns }: { port: Port; columns: PortColumns }) {
  useLocale()
  const health = state(portState(port))
  const duplex = duplexLabel(port.duplex)
  const errors = port.rx_errors != null || port.tx_errors != null
    ? formatCount((port.rx_errors ?? 0) + (port.tx_errors ?? 0))
    : '—'
  return (
    <TableRow>
      <TableCell>
        <div className="font-medium">{portLabel(port)}</div>
        {port.mac && <div className="font-mono text-[0.6875rem] text-muted-foreground">{port.mac}</div>}
        {port.poe && <div className="text-[0.6875rem] text-muted-foreground">{poeText(port)}</div>}
      </TableCell>
      <TableCell className="hidden text-muted-foreground sm:table-cell">{t(portKindLabel(port.kind))}</TableCell>
      <TableCell><StatusInline tone={health.tone}>{health.label}</StatusInline></TableCell>
      {columns.speed && (
        <TableCell className="text-right tabular-nums">
          {port.speed_mbps != null ? formatSpeed(port.speed_mbps) : '—'}
          {duplex && <span className="hidden sm:inline"> · {t(duplex)}</span>}
        </TableCell>
      )}
      {columns.received && <TableCell className="hidden text-right tabular-nums md:table-cell">{counters(port.rx_bytes, port.rx_packets)}</TableCell>}
      {columns.sent && <TableCell className="hidden text-right tabular-nums md:table-cell">{counters(port.tx_bytes, port.tx_packets)}</TableCell>}
      {columns.errors && <TableCell className="hidden text-right tabular-nums sm:table-cell">{errors}</TableCell>}
    </TableRow>
  )
}

function poeText(port: Port): string {
  const poe = port.poe
  if (!poe) return ''
  if (poe.delivering && poe.power_watts != null) return `PoE · ${poe.power_watts} W`
  if (poe.enabled === false) return 'PoE off'
  return 'PoE'
}

export function Loading() {
  return (
    <div className="space-y-3">
      <Skeleton className="h-5 w-40" />
      <Skeleton className="h-40 w-full" />
    </div>
  )
}

export function PageError({ error, title }: { error: unknown; title: string }) {
  useLocale()
  return (
    <Alert variant="destructive">
      <TriangleAlert />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{error instanceof Error ? error.message : t('The device did not answer.')}</AlertDescription>
    </Alert>
  )
}

export function Missing({ id }: { id: string }) {
  useLocale()
  return (
    <div className="space-y-3 py-16 text-center">
      <Heading as="h1" size="lg">{t('Device not found')}</Heading>
      <p className="text-sm text-muted-foreground">{t('{id} is not in the device inventory. It may have been forgotten.', { id })}</p>
      <Button variant="secondary" nativeButton={false} render={<Link to="/devices">{t('Back to devices')}</Link>} />
    </div>
  )
}
