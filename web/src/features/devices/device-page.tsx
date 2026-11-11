import { t, useLocale } from '@/lib/i18n'
import { Pencil, RefreshCw, TriangleAlert, Wifi } from 'lucide-react'
import { useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router'

import { PageHeading } from '@/components/layout/page-heading'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Disclosure } from '@/components/ui/disclosure'
import { Heading } from '@/components/ui/heading'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ListEmpty } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusInline } from '@/components/ui/status'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { state } from '@/lib/state'
import { cn, formatBytes } from '@/lib/utils'

import { DeviceDialog, type DeviceDraft } from './device-dialog'
import {
  adapterLabel,
  capabilityWord,
  collectionWord,
  duplexLabel,
  formatCount,
  formatSpeed,
  formatUptime,
  macValid,
  portKindLabel,
  portLabel,
  portName,
  portState,
  roleLabels,
  sortPorts,
  vlanValid,
} from './device'
import { useDeviceEditor, useFDBLookup, useDevicesConfig, useNetworkSnapshot } from './queries'
import type { Collection, Device, Port, Snapshot, VLAN } from './types'

type Tab = 'overview' | 'ports' | 'vlans' | 'mac'

/**
 * One box in the inventory, read on demand.
 *
 * The tabs are not a device type. They are the evidence this device happens to
 * report: a Mercury switch has ports, VLANs and a MAC table, an OpenWrt access
 * point has ports and radios, and a section its adapter cannot read says
 * "unavailable" with the adapter's reason instead of vanishing or rendering an
 * empty table. Nothing here writes to the device.
 */
export function DevicePage() {
  useLocale()
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const tab = tabOf(params.get('tab'))
  const setTab = (next: Tab) => setParams(next === 'overview' ? {} : { tab: next }, { replace: true })
  const config = useDevicesConfig()
  const snapshot = useNetworkSnapshot(id)
  const editor = useDeviceEditor()
  const [dialog, setDialog] = useState(false)

  const device = config.data?.devices?.find(candidate => candidate.id === id)

  if (config.isPending) return <Loading />
  if (config.isError) return <PageError error={config.error} title={t('Could not read the device inventory')} />
  if (!device) return <Missing id={id} />

  const data = snapshot.data
  const model = data?.identity.model
  const wifi = (data?.radios?.length ?? 0) > 0 || Boolean(device.roles?.includes('ap'))
  const observed = data?.observed_at

  const submit = async (draft: DeviceDraft) => {
    if (!(await editor.save(draft))) return
    setDialog(false)
  }
  const forget = async () => {
    if (!(await editor.remove(device.id))) return
    setDialog(false)
    navigate('/devices', { replace: true })
  }

  return (
    <div className="space-y-6">
      <PageHeading
        breadcrumbs={[{ to: '/devices', label: t('Devices') }]}
        title={device.name || model || device.id}
        description={[t(adapterLabel(device.adapter)), ...roleLabels(device).map(role => t(role)), device.address].join(' · ')}
        action={
          <div className="flex flex-wrap items-center gap-2">
            {wifi && (
              <Button variant="secondary" size="sm" nativeButton={false} render={
                <Link to={`/wireless?device=${encodeURIComponent(device.id)}`}><Wifi className="size-4" />{t('Wireless view')}</Link>
              } />
            )}
            <Button variant="secondary" size="sm" onClick={() => snapshot.refetch()} disabled={snapshot.isFetching}>
              <RefreshCw className={cn('size-4', snapshot.isFetching && 'animate-spin')} />
              {t('Refresh')}
            </Button>
            <Button variant="secondary" size="sm" onClick={() => setDialog(true)}>
              <Pencil className="size-4" />
              {t('Edit')}
            </Button>
          </div>
        }
      />

      <div className="flex flex-wrap items-end justify-between gap-3">
        <Tabs tab={tab} onChange={setTab} />
        {observed && <p className="pb-2 text-xs text-muted-foreground">{t('Read {time}', { time: clock(observed) })}</p>}
      </div>

      {snapshot.isPending ? (
        <Loading />
      ) : snapshot.isError ? (
        <PageError error={snapshot.error} title={t('Could not read this device')} />
      ) : data ? (
        tab === 'ports' ? <PortsTab snapshot={data} />
          : tab === 'vlans' ? <VLANsTab snapshot={data} />
            : tab === 'mac' ? <MACLookupTab id={device.id} snapshot={data} />
              : <OverviewTab device={device} snapshot={data} />
      ) : null}

      {dialog && (
        <DeviceDialog
          open
          onOpenChange={open => { if (!open) setDialog(false) }}
          initial={device}
          types={[]}
          busy={editor.busy}
          onSubmit={submit}
          onRemove={forget}
        />
      )}
    </div>
  )
}

function Tabs({ tab, onChange }: { tab: Tab; onChange: (tab: Tab) => void }) {
  useLocale()
  const items: { id: Tab; label: string }[] = [
    { id: 'overview', label: t('Overview') },
    { id: 'ports', label: t('Ports') },
    { id: 'vlans', label: 'VLAN' },
    { id: 'mac', label: t('MAC lookup') },
  ]
  return (
    <div role="tablist" aria-label={t('Device views')} className="flex gap-1 border-b border-border">
      {items.map(item => (
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
  )
}

/**
 * What the device is, and which of its sections this read could obtain.
 *
 * The collection list is the honest part: a switch whose VLAN page the adapter
 * could not parse is not a switch without VLANs.
 */
function OverviewTab({ device, snapshot }: { device: Device; snapshot: Snapshot }) {
  useLocale()
  const identity = snapshot.identity
  const facts: { label: string; value?: React.ReactNode }[] = [
    { label: t('Manufacturer'), value: identity.manufacturer },
    { label: t('Model'), value: identity.model },
    { label: t('Serial'), value: identity.serial },
    { label: t('Firmware'), value: identity.firmware },
    { label: t('Hostname'), value: identity.hostname },
    { label: t('Uptime'), value: identity.uptime_seconds != null ? formatUptime(identity.uptime_seconds) : undefined },
    { label: t('Interface MACs'), value: identity.macs?.join(' · ') },
  ].filter(fact => fact.value)

  const collections = Object.entries(snapshot.collections).sort(
    ([a], [b]) => collectionRank(a) - collectionRank(b),
  )

  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <Card className="gap-0 py-0">
        <div className="space-y-2 p-5">
          <Heading as="h2" size="sm">{t('Identity')}</Heading>
          {facts.length === 0 ? (
            <p className="text-sm leading-6 text-muted-foreground">{t('This device did not report its identity.')}</p>
          ) : (
            <dl className="divide-y divide-border/60">
              {facts.map(fact => (
                <div key={fact.label} className="grid min-h-9 grid-cols-[8rem_minmax(0,1fr)] items-center gap-3 py-1 text-sm">
                  <dt className="text-muted-foreground">{fact.label}</dt>
                  <dd className="min-w-0 text-right [overflow-wrap:anywhere]">{fact.value}</dd>
                </div>
              ))}
            </dl>
          )}
        </div>
      </Card>

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

      <div className="lg:col-span-2">
        <Disclosure summary={t('What this device can report')}>
          <div className="divide-y divide-border rounded-lg border bg-card">
            {Object.entries(snapshot.capabilities).sort(([a], [b]) => a.localeCompare(b)).map(([key, capability]) => {
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
      </div>
    </div>
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

const COLLECTION_LABELS: Record<string, string> = {
  identity: 'Identity',
  ports: 'Ports',
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
const COLLECTION_ORDER = ['identity', 'ports', 'statistics', 'vlans', 'pvids', 'fdb', 'neighbors', 'radios', 'wireless', 'associations']

function collectionRank(name: string): number {
  const index = COLLECTION_ORDER.indexOf(name)
  return index === -1 ? COLLECTION_ORDER.length : index
}

/**
 * The port table. Speeds and counters are the device's own; a port with no
 * reported counter shows a dash, because "unknown" and "zero" are different
 * claims about a switch.
 */
function PortsTab({ snapshot }: { snapshot: Snapshot }) {
  useLocale()
  const ports = sortPorts(snapshot.ports ?? [])
  const read = snapshot.collections.ports
  if (read && read.state !== 'ok') return <Unread collection={read} what={t('port table')} />
  if (ports.length === 0) return <ListEmpty>{t('This device reported no ports.')}</ListEmpty>

  return (
    <div className="space-y-3">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Port')}</TableHead>
            <TableHead className="hidden sm:table-cell">{t('Type')}</TableHead>
            <TableHead>{t('State')}</TableHead>
            <TableHead className="text-right">{t('Speed')}</TableHead>
            <TableHead className="hidden text-right md:table-cell">{t('Received')}</TableHead>
            <TableHead className="hidden text-right md:table-cell">{t('Sent')}</TableHead>
            <TableHead className="hidden text-right sm:table-cell">{t('Errors')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {ports.map(port => <PortRow key={port.id} port={port} />)}
        </TableBody>
      </Table>
      <p className="text-xs leading-5 text-muted-foreground">{t('Counters are the device’s own totals. They can reset or wrap, and a counter the device does not report is unknown rather than zero.')}</p>
    </div>
  )
}

function PortRow({ port }: { port: Port }) {
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
      <TableCell className="text-right tabular-nums">
        {port.speed_mbps != null ? formatSpeed(port.speed_mbps) : '—'}
        {duplex && <span className="hidden sm:inline"> · {t(duplex)}</span>}
      </TableCell>
      <TableCell className="hidden text-right tabular-nums md:table-cell">{counters(port.rx_bytes, port.rx_packets)}</TableCell>
      <TableCell className="hidden text-right tabular-nums md:table-cell">{counters(port.tx_bytes, port.tx_packets)}</TableCell>
      <TableCell className="hidden text-right tabular-nums sm:table-cell">{errors}</TableCell>
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

/** Bytes when the device reports them, packets when it only counts packets. */
function counters(bytes?: number, packets?: number): string {
  if (bytes != null) return formatBytes(bytes)
  if (packets != null) return `${formatCount(packets)} ${t('pkts')}`
  return '—'
}

/**
 * VLAN membership, device-local. Equal VIDs on two switches do not prove
 * connectivity, so the page never draws a VLAN as a network.
 */
function VLANsTab({ snapshot }: { snapshot: Snapshot }) {
  useLocale()
  const read = snapshot.collections.vlans
  const vlans = [...(snapshot.vlans ?? [])].sort((a, b) => a.id - b.id)
  if (read && read.state !== 'ok') return <Unread collection={read} what={t('VLAN table')} />

  return (
    <div className="space-y-3">
      {snapshot.vlan_enabled === false && (
        <Alert>
          <TriangleAlert />
          <AlertTitle>{t('VLAN mode is off')}</AlertTitle>
          <AlertDescription>{t('The groups below are configured on this device but are not filtering traffic right now.')}</AlertDescription>
        </Alert>
      )}
      {vlans.length === 0 ? (
        <ListEmpty>{t('This device reported no VLANs.')}</ListEmpty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>VLAN</TableHead>
              <TableHead>{t('Name')}</TableHead>
              <TableHead>{t('Tagged ports')}</TableHead>
              <TableHead>{t('Untagged ports')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {vlans.map(vlan => <VLANRow key={vlan.id} vlan={vlan} ports={snapshot.ports} />)}
          </TableBody>
        </Table>
      )}
    </div>
  )
}

function VLANRow({ vlan, ports }: { vlan: VLAN; ports?: Port[] }) {
  useLocale()
  const tagged = vlan.tagged_ports.map(id => portName(ports, id)).join(', ')
  const untagged = vlan.untagged_ports.map(id => portName(ports, id)).join(', ')
  return (
    <TableRow>
      <TableCell className="tabular-nums font-medium">{vlan.id}</TableCell>
      <TableCell>{vlan.name || '—'}</TableCell>
      <TableCell className="text-muted-foreground">{tagged || '—'}</TableCell>
      <TableCell className="text-muted-foreground">{untagged || '—'}</TableCell>
    </TableRow>
  )
}

/**
 * Ask a switch where one MAC is reachable. The lookup is bounded to a known
 * address on purpose: exporting the whole forwarding table is a different,
 * heavier operation that these adapters do not implement.
 */
function MACLookupTab({ id, snapshot }: { id: string; snapshot: Snapshot }) {
  useLocale()
  const lookup = useFDBLookup(id)
  const [mac, setMac] = useState('')
  const [vlan, setVlan] = useState('')
  const capability = snapshot.capabilities['fdb.lookup']
  const supported = capability === undefined || capability.state === 'supported'
  const vlanNumber = vlan.trim() === '' ? undefined : Number(vlan)
  const canSearch = supported && macValid(mac) && (vlanNumber === undefined || vlanValid(vlanNumber)) && !lookup.isPending

  return (
    <div className="space-y-4">
      {!supported && (
        <Alert>
          <TriangleAlert />
          <AlertTitle>{t('MAC lookup is not available here')}</AlertTitle>
          <AlertDescription>{capability?.reason ?? t('This adapter has no address-table lookup.')}</AlertDescription>
        </Alert>
      )}
      <form
        className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_8rem_auto] sm:items-end"
        onSubmit={event => {
          event.preventDefault()
          if (canSearch) lookup.mutate({ mac: mac.trim(), vlan: vlanNumber })
        }}
      >
        <div className="space-y-1.5">
          <Label htmlFor="fdb-mac">{t('MAC address')}</Label>
          <Input id="fdb-mac" value={mac} autoComplete="off" placeholder="02:11:22:33:44:55" className="font-mono" onChange={event => setMac(event.target.value)} />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="fdb-vlan">{t('VLAN')}</Label>
          <Input id="fdb-vlan" value={vlan} inputMode="numeric" autoComplete="off" placeholder={t('Any')} onChange={event => setVlan(event.target.value)} />
        </div>
        <Button type="submit" disabled={!canSearch}>{lookup.isPending ? t('Searching…') : t('Find')}</Button>
      </form>

      {lookup.isError && (
        <Alert variant="destructive">
          <TriangleAlert />
          <AlertTitle>{t('The lookup failed')}</AlertTitle>
          <AlertDescription>{lookup.error instanceof Error ? lookup.error.message : String(lookup.error)}</AlertDescription>
        </Alert>
      )}

      {lookup.data && (
        lookup.data.entries.length === 0 ? (
          <ListEmpty>{t('No port answered for this address. It may be behind another switch, or the device has not learned it yet.')}</ListEmpty>
        ) : (
          <div className="space-y-3">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('MAC address')}</TableHead>
                  <TableHead>VLAN</TableHead>
                  <TableHead>{t('Port')}</TableHead>
                  <TableHead>{t('Type')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {lookup.data.entries.map(entry => (
                  <TableRow key={`${entry.mac}-${entry.vlan ?? ''}-${entry.port_id}`}>
                    <TableCell className="font-mono text-[0.8125rem]">{entry.mac}</TableCell>
                    <TableCell className="tabular-nums">{entry.vlan ?? '—'}</TableCell>
                    <TableCell>{portName(snapshot.ports, entry.port_id)}</TableCell>
                    <TableCell className="text-muted-foreground">{entry.type || '—'}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <p className="text-xs leading-5 text-muted-foreground">
              {t('Read {time}. A lookup shows where the address is reachable, not proof of a direct cable.', { time: clock(lookup.data.observed_at) })}
            </p>
          </div>
        )
      )}
    </div>
  )
}

/** A section this read could not obtain, with the adapter's own reason. */
function Unread({ collection, what }: { collection: Collection; what: string }) {
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

function Missing({ id }: { id: string }) {
  useLocale()
  return (
    <div className="space-y-3 py-16 text-center">
      <Heading as="h1" size="lg">{t('Device not found')}</Heading>
      <p className="text-sm text-muted-foreground">{t('{id} is not in the device inventory. It may have been forgotten.', { id })}</p>
      <Button variant="secondary" nativeButton={false} render={<Link to="/devices">{t('Back to devices')}</Link>} />
    </div>
  )
}

function Loading() {
  return (
    <div className="space-y-3">
      <Skeleton className="h-5 w-40" />
      <Skeleton className="h-40 w-full" />
    </div>
  )
}

function PageError({ error, title }: { error: unknown; title: string }) {
  useLocale()
  return (
    <Alert variant="destructive">
      <TriangleAlert />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{error instanceof Error ? error.message : t('The device did not answer.')}</AlertDescription>
    </Alert>
  )
}

function tabOf(value: string | null): Tab {
  if (value === 'ports' || value === 'vlans' || value === 'mac') return value
  return 'overview'
}

function clock(iso: string): string {
  const date = new Date(iso)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}
