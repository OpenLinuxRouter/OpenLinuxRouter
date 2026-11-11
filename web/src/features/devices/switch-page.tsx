import { t, useLocale } from '@/lib/i18n'
import { TriangleAlert } from 'lucide-react'
import { useState } from 'react'
import { useSearchParams } from 'react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Heading } from '@/components/ui/heading'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ListEmpty } from '@/components/ui/list'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

import { formatUptime, macValid, portName, vlanValid } from './device'
import { clock } from './device-format'
import {
  CapabilitiesDisclosure,
  CollectionCard,
  FactList,
  PortsTable,
  Unread,
  ViewHeader,
} from './device-parts'
import { useFDBLookup } from './queries'
import type { Device, Port, Snapshot, VLAN } from './types'

const TABS = ['overview', 'ports', 'vlans', 'mac'] as const
type Tab = typeof TABS[number]

function tabOf(value: string | null): Tab {
  return TABS.includes(value as Tab) ? value as Tab : 'overview'
}

/**
 * A managed switch: the ports it forwards between, the VLANs that divide them,
 * and where one address was learned.
 *
 * This is a switch page, not a general device page with the switch parts
 * switched on. A switch has no radios and no fiber line, so it has no tab for
 * them and no vocabulary from another device family; the tab strip is the list
 * of questions a switch can answer.
 */
export function SwitchPage({ device, snapshot }: { device: Device; snapshot: Snapshot }) {
  useLocale()
  const [params, setParams] = useSearchParams()
  const tab = tabOf(params.get('tab'))
  const setTab = (next: string) => setParams(next === 'overview' ? {} : { tab: next }, { replace: true })

  return (
    <div className="space-y-4">
      <ViewHeader
        tabs={[
          { id: 'overview', label: t('Overview') },
          { id: 'ports', label: t('Ports') },
          { id: 'vlans', label: 'VLAN' },
          { id: 'mac', label: t('MAC lookup') },
        ]}
        tab={tab}
        onChange={setTab}
        actions={<p className="text-xs text-muted-foreground">{t('Read {time}', { time: clock(snapshot.observed_at) })}</p>}
      />
      {tab === 'ports' ? <PortsTable snapshot={snapshot} />
        : tab === 'vlans' ? <VLANsTab snapshot={snapshot} />
          : tab === 'mac' ? <MACLookupTab id={device.id} snapshot={snapshot} />
            : <OverviewTab device={device} snapshot={snapshot} />}
    </div>
  )
}

/** What the switch is, and which of its sections this read could obtain. */
function OverviewTab({ device, snapshot }: { device: Device; snapshot: Snapshot }) {
  useLocale()
  const identity = snapshot.identity
  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <Card className="gap-0 py-0">
        <div className="space-y-2 p-5">
          <Heading as="h2" size="sm">{t('Identity')}</Heading>
          <FactList
            facts={[
              { label: t('Manufacturer'), value: identity.manufacturer },
              { label: t('Model'), value: identity.model },
              { label: t('Serial'), value: identity.serial },
              { label: t('Firmware'), value: identity.firmware },
              { label: t('Hostname'), value: identity.hostname },
              { label: t('Uptime'), value: identity.uptime_seconds != null ? formatUptime(identity.uptime_seconds) : undefined },
              { label: t('Interface MACs'), value: identity.macs?.join(' · ') },
            ]}
          />
        </div>
      </Card>

      <CollectionCard device={device} snapshot={snapshot} />

      <div className="lg:col-span-2">
        <CapabilitiesDisclosure snapshot={snapshot} />
      </div>
    </div>
  )
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
 * Ask the switch where one MAC is reachable. The lookup is bounded to a known
 * address on purpose: exporting the whole forwarding table is a different,
 * heavier operation that these adapters do not implement.
 */
function MACLookupTab({ id, snapshot }: { id: string; snapshot: Snapshot }) {
  useLocale()
  const lookup = useFDBLookup(id)
  const [mac, setMac] = useState('')
  const [vlan, setVlan] = useState('')
  const vlanNumber = vlan.trim() === '' ? undefined : Number(vlan)
  const canSearch = macValid(mac) && (vlanNumber === undefined || vlanValid(vlanNumber)) && !lookup.isPending

  return (
    <div className="space-y-4">
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
