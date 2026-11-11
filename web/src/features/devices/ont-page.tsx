import { t, useLocale } from '@/lib/i18n'
import { useSearchParams } from 'react-router'

import { Card } from '@/components/ui/card'
import { Heading } from '@/components/ui/heading'
import { ListEmpty } from '@/components/ui/list'
import { StatusInline } from '@/components/ui/status'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { healthOf, state } from '@/lib/state'

import { formatCount, formatUptime } from './device'
import { clock, counters } from './device-format'
import {
  CapabilitiesDisclosure,
  CollectionCard,
  Fact,
  FactList,
  PortsTable,
  Unread,
  ViewHeader,
} from './device-parts'
import type { Device, Snapshot, WANConnection } from './types'

const TABS = ['line', 'wan', 'ports'] as const
type Tab = typeof TABS[number]

function tabOf(value: string | null): Tab {
  return TABS.includes(value as Tab) ? value as Tab : 'line'
}

/**
 * The fiber ONT: the box the carrier hands over.
 *
 * Its page is about the line — is it registered, what is the optical budget,
 * which carrier services ride on it — because that is what an ONT knows and no
 * other device here can answer. Its VLAN is a service tag with a priority, so it
 * is shown on the WAN row it belongs to and never as a switch port membership.
 */
export function ONTPage({ device, snapshot }: { device: Device; snapshot: Snapshot }) {
  useLocale()
  const [params, setParams] = useSearchParams()
  const tab = tabOf(params.get('tab'))
  const setTab = (next: string) => setParams(next === 'line' ? {} : { tab: next }, { replace: true })

  return (
    <div className="space-y-4">
      <ViewHeader
        tabs={[
          { id: 'line', label: t('Line') },
          { id: 'wan', label: 'WAN' },
          { id: 'ports', label: t('Ports') },
        ]}
        tab={tab}
        onChange={setTab}
        actions={<p className="text-xs text-muted-foreground">{t('Read {time}', { time: clock(snapshot.observed_at) })}</p>}
      />
      {tab === 'wan' ? <WANTab snapshot={snapshot} />
        : tab === 'ports' ? <PortsTable snapshot={snapshot} />
          : <LineTab device={device} snapshot={snapshot} />}
    </div>
  )
}

/**
 * What the line delivers, and what the transceiver reports about it.
 *
 * Every reading comes with the window the ONT publishes for its own optics, so
 * a receive power is only called out when it falls outside the range the device
 * reports — not against a number this page invented. Nothing here writes to the
 * ONT.
 */
function LineTab({ device, snapshot }: { device: Device; snapshot: Snapshot }) {
  useLocale()
  const ont = snapshot.ont
  if (!ont) {
    return (
      <div className="space-y-6">
        <ListEmpty>{t('This device did not report its fiber line.')}</ListEmpty>
        <CollectionCard device={device} snapshot={snapshot} />
      </div>
    )
  }

  const registered = ont.pon.registered === true
    ? state('Connected')
    : ont.pon.registered === false ? state('Down') : state('Unknown')
  const optical = ont.optical
  const opticalRead = snapshot.collections.optical
  const identity = snapshot.identity

  return (
    <div className="space-y-6">
      <div className="grid gap-6 lg:grid-cols-2">
        <Card className="gap-0 py-0">
          <div className="space-y-2 p-5">
            <Heading as="h2" size="sm">{t('Line')}</Heading>
            <dl className="divide-y divide-border/60">
              <Fact label={t('PON mode')} value={ont.pon.mode} />
              <Fact
                label={t('Registration')}
                value={
                  <span className="flex items-center justify-end gap-2">
                    {ont.pon.state && <span className="text-muted-foreground">{ont.pon.state}</span>}
                    <StatusInline tone={registered.tone}>{registered.label}</StatusInline>
                  </span>
                }
              />
              <Fact label={t('Carrier build')} value={ont.customization} />
              {optical?.los === true && (
                <Fact label={t('Signal lost')} value={<StatusInline tone={healthOf('fault', t('LOS flagged')).tone}>{t('LOS flagged')}</StatusInline>} />
              )}
              <Fact label={t('CPU usage')} value={percentText(ont.health?.cpu_percent)} />
              <Fact label={t('Memory usage')} value={percentText(ont.health?.memory_percent)} />
              <Fact label={t('NAT sessions')} value={ont.health?.nat_sessions != null ? formatCount(ont.health.nat_sessions) : undefined} />
            </dl>
          </div>
        </Card>

        <Card className="gap-0 py-0">
          <div className="space-y-2 p-5">
            <div className="flex items-baseline justify-between gap-3">
              <Heading as="h2" size="sm">{t('Optical module')}</Heading>
              {optical?.module_vendor && <span className="text-xs text-muted-foreground">{optical.module_vendor}</span>}
            </div>
            {opticalRead && opticalRead.state !== 'ok' ? (
              <Unread collection={opticalRead} what={t('optical module')} />
            ) : (
              <dl className="divide-y divide-border/60">
                <Reading label={t('Receive power')} value={optical?.rx_dbm} range={optical?.rx_range_dbm} unit="dBm" />
                <Reading label={t('Transmit power')} value={optical?.tx_dbm} range={optical?.tx_range_dbm} unit="dBm" />
                <Fact label={t('Temperature')} value={optical?.temperature_c != null ? `${optical.temperature_c} °C` : undefined} />
                <Fact label={t('Voltage')} value={optical?.voltage_mv != null ? `${optical.voltage_mv} mV` : undefined} />
                <Fact label={t('Bias current')} value={optical?.current_ma != null ? `${optical.current_ma} mA` : undefined} />
                <Fact label={t('Wavelengths')} value={wavelengths(optical)} />
                <Fact label={t('Module serial')} value={optical?.module_serial} />
                <Fact label={t('Manufactured')} value={optical?.date_code} />
              </dl>
            )}
          </div>
        </Card>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <Card className="gap-0 py-0">
          <div className="space-y-2 p-5">
            <Heading as="h2" size="sm">{t('Device')}</Heading>
            <FactList
              facts={[
                { label: t('Model'), value: identity.model },
                { label: t('Serial'), value: identity.serial },
                { label: t('Firmware'), value: identity.firmware },
                { label: t('Uptime'), value: identity.uptime_seconds != null ? formatUptime(identity.uptime_seconds) : undefined },
                { label: t('Interface MACs'), value: identity.macs?.join(' · ') },
              ]}
            />
          </div>
        </Card>
        <CollectionCard device={device} snapshot={snapshot} />
      </div>

      <CapabilitiesDisclosure snapshot={snapshot} />
    </div>
  )
}

function WANTab({ snapshot }: { snapshot: Snapshot }) {
  useLocale()
  const ont = snapshot.ont
  const wanRead = snapshot.collections.wan
  const wan = ont?.wan ?? []

  return (
    <div className="space-y-3">
      <p className="text-xs text-muted-foreground">{t('Received and sent are counted from the ONT, so received is what arrived from the carrier.')}</p>
      {wanRead && wanRead.state !== 'ok' ? (
        <Unread collection={wanRead} what={t('WAN services')} />
      ) : wan.length === 0 ? (
        <ListEmpty>{t('This ONT reported no WAN connection.')}</ListEmpty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Connection')}</TableHead>
              <TableHead>{t('State')}</TableHead>
              <TableHead className="hidden text-right sm:table-cell">{t('VLAN / priority')}</TableHead>
              <TableHead className="text-right">{t('Address')}</TableHead>
              <TableHead className="hidden text-right md:table-cell">{t('Uptime')}</TableHead>
              <TableHead className="hidden text-right md:table-cell">{t('Received')}</TableHead>
              <TableHead className="hidden text-right md:table-cell">{t('Sent')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {wan.map(connection => <WANRow key={connection.path} connection={connection} />)}
          </TableBody>
        </Table>
      )}
    </div>
  )
}

function WANRow({ connection }: { connection: WANConnection }) {
  useLocale()
  const connected = connection.status === 'Connected'
  const status = connected ? state('Connected') : connection.status === 'Disconnected' ? healthOf('off', t('Disconnected')) : state('Unknown')
  // A carrier service tag with its 802.1p priority. This is not a switch VLAN.
  const vlan = connection.vlan != null ? connection.priority != null ? `${connection.vlan}/${connection.priority}` : String(connection.vlan) : undefined
  return (
    <TableRow>
      <TableCell>
        <div className="font-medium">{connection.service || connection.name || '—'}</div>
        <div className="font-mono text-[0.6875rem] text-muted-foreground">{connection.name ?? connection.path}</div>
      </TableCell>
      <TableCell>
        <StatusInline tone={status.tone}>{status.label}</StatusInline>
        {connection.interface && <div className="text-[0.6875rem] text-muted-foreground">{connection.interface}</div>}
      </TableCell>
      <TableCell className="hidden text-right tabular-nums sm:table-cell">{vlan ?? '—'}</TableCell>
      <TableCell className="text-right">
        <div className="tabular-nums">{connection.address ?? '—'}</div>
        {connection.ipv6_prefix && <div className="font-mono text-[0.6875rem] text-muted-foreground">{connection.ipv6_prefix}</div>}
      </TableCell>
      <TableCell className="hidden text-right tabular-nums md:table-cell">{connection.uptime_seconds != null ? formatUptime(connection.uptime_seconds) : '—'}</TableCell>
      <TableCell className="hidden text-right tabular-nums md:table-cell">{counters(connection.rx_bytes, connection.rx_packets)}</TableCell>
      <TableCell className="hidden text-right tabular-nums md:table-cell">{counters(connection.tx_bytes, connection.tx_packets)}</TableCell>
    </TableRow>
  )
}

/** A reading beside the window the device itself considers acceptable. */
function Reading({ label, value, range, unit }: { label: string; value?: number; range?: { min: number; max: number }; unit: string }) {
  useLocale()
  const outside = value != null && range != null && (value < range.min || value > range.max)
  const verdict = outside ? healthOf('warn', t('Outside the range')) : undefined
  return (
    <Fact
      label={label}
      value={
        <span className="flex flex-wrap items-center justify-end gap-x-2 gap-y-1">
          <span className="tabular-nums">{value != null ? `${value} ${unit}` : '—'}</span>
          {range && <span className="text-xs text-muted-foreground">{range.min} – {range.max} {unit}</span>}
          {verdict && <StatusInline tone={verdict.tone}>{verdict.label}</StatusInline>}
        </span>
      }
    />
  )
}

function percentText(value?: number): string | undefined {
  return value != null ? `${value}%` : undefined
}

function wavelengths(optical?: { tx_wavelength_nm?: number; rx_wavelength_nm?: number }): string | undefined {
  if (optical?.tx_wavelength_nm == null && optical?.rx_wavelength_nm == null) return undefined
  return `${optical?.tx_wavelength_nm ?? '—'} / ${optical?.rx_wavelength_nm ?? '—'} nm`
}
