import { t, useLocale } from '@/lib/i18n'
import { Wifi } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Heading } from '@/components/ui/heading'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'

import { useClientList } from '@/features/clients/queries'
import { adapterLabel, addressLabel, roleLabels } from '@/features/devices/device'
import { readWhen } from '@/features/devices/device-format'
import { deviceKeys, useDeviceRead, useDevicesConfig, useReadDevice } from '@/features/devices/queries'
import type { Device } from '@/features/devices/types'
import { useInterfaces } from '@/features/link/queries'
import { DeviceFace } from './device-face'
import { formOf } from './form'
import { attachedClients, equipmentFacts, farEnd, learnedByPort, planReads, type FarEndSources } from './equipment'
import { runSweep, usePortSweep } from './sweep'
import { hostOf, placeDevice } from './placement'
import type { ClientRow } from '@/lib/api-types'
import type { FDBResult } from '@/features/devices/types'

/**
 * The boxes this router reads, and the clients each one reports.
 *
 * This page reads: opening it asks every box it has no answer from this session
 * yet, one at a time, and each card fills in as its answer arrives. That is a
 * deliberate difference from the group map on Overview, which costs nothing to
 * draw — the map's subject is what the router already knows, and this page's
 * subject is what the hardware says about itself. A person asking "what is
 * plugged into what" will not click four times to assemble the answer, so the
 * page does the clicking.
 *
 * The one thing it will not do is read the same box twice. An answer this
 * session already holds is reused and carries the time it arrived; a box that
 * does not answer says so and is left alone until the page is opened again.
 *
 * Placement is a join, not a claim. A box carries the network whose range holds
 * its management address, and the page says so once: that is where it is
 * managed from, not where its cables are.
 */
export function EquipmentBand({ highlight }: { highlight?: string }) {
  useLocale()
  const config = useDevicesConfig()
  const interfaces = useInterfaces()
  const clients = useClientList()
  const client = useQueryClient()
  const { mutateAsync: readDevice } = useReadDevice()
  const started = useRef(false)
  const [reading, setReading] = useState<string | undefined>(undefined)
  const [failed, setFailed] = useState<Record<string, string>>({})
  const [readsDone, setReadsDone] = useState(false)

  const devices = useMemo(() => config.data?.devices ?? [], [config.data])
  const byMac = useMemo(
    () => new Map((clients.data?.clients ?? []).map(client => [client.mac.toLowerCase(), client])),
    [clients.data],
  )
  const networks = interfaces.data?.networks ?? []

  // One pass over the inventory, on arrival only. `started` is a ref rather
  // than state so a second render cannot start a second pass, and the pass
  // itself skips anything this session already answered — including a box that
  // was just added, which the Devices page read before sending us here.
  useEffect(() => {
    if (started.current || config.data === undefined) return
    started.current = true
    let cancelled = false
    void (async () => {
      for (const device of planReads(config.data?.devices ?? [], id => client.getQueryData(deviceKeys.snapshot(id)) !== undefined)) {
        setReading(device.id)
        try {
          await readDevice(device.id)
        } catch (error) {
          if (!cancelled) {
            setFailed(current => ({ ...current, [device.id]: error instanceof Error ? error.message : t('no answer') }))
          }
        }
        if (cancelled) return
      }
      setReading(undefined)
      setReadsDone(true)
    })()
    return () => { cancelled = true }
    // The ref, not the dependencies, is what keeps this to one pass: the
    // mutation object is new on every render, and the inventory refetches.
  }, [client, config.data, readDevice])

  // Every address worth asking about: the clients this router knows, and its
  // own interfaces — a switch's port that leads back here is a cable too.
  const sources: FarEndSources = useMemo(() => {
    const self = new Map<string, string>()
    // A bridge carries the MAC of one of its ports; the port's name is the one
    // the map can draw a cable from, so physical interfaces claim it first.
    const rows = [...(interfaces.data?.interfaces ?? [])].sort((a, b) => Number(b.physical) - Number(a.physical))
    for (const row of rows) {
      const mac = row.mac?.toLowerCase()
      if (mac !== undefined && mac !== '' && mac !== '00:00:00:00:00:00' && !self.has(mac)) self.set(mac, row.name)
    }
    return {
      self,
      clients: new Map((clients.data?.clients ?? []).map(client => [client.mac.toLowerCase(), client])),
      devices: config.data?.devices ?? [],
    }
  }, [clients.data, config.data, interfaces.data])

  // Every address worth asking about: this router's own interfaces first (the
  // cable back here), then the boxes we manage, then whoever is online now. An
  // offline client has aged out of any forwarding table, and asking about it is
  // a lookup that cannot answer.
  const macs = useMemo(() => {
    const hosts = new Set(sources.devices.map(device => hostOf(device.address)).filter(host => host !== undefined))
    const all = clients.data?.clients ?? []
    return [...new Set([
      ...sources.self.keys(),
      ...all.filter(row => (row.ips ?? []).some(ip => hosts.has(ip))).map(row => row.mac.toLowerCase()),
      ...all.filter(row => row.online).map(row => row.mac.toLowerCase()),
    ].filter(mac => mac !== ''))]
  }, [clients.data, sources])

  const sweep = useCallback((device: Device, force: boolean) => runSweep({
    deviceId: device.id,
    macs,
    force,
    resolve: mac => farEnd(mac, sources),
    lookup: async mac => {
      const result = await api.get<FDBResult>(`/api/devices/${encodeURIComponent(device.id)}/fdb?${new URLSearchParams({ mac })}`)
      return result.entries.map(entry => ({ mac: entry.mac, port_id: entry.port_id }))
    },
  }), [macs, sources])

  // Once every box has been read, ask each switch that answered where the
  // addresses are — without being asked to: what is plugged into what is the
  // question this page exists for. Switches run side by side; each one is
  // asked one address at a time, which is all its firmware takes.
  const swept = useRef(false)
  useEffect(() => {
    if (swept.current || !readsDone || clients.data === undefined || interfaces.data === undefined) return
    swept.current = true
    for (const device of devices) {
      if (!isSweepable(device) || failed[device.id] !== undefined) continue
      if ((client.getQueryData<{ ports?: unknown[] }>(deviceKeys.snapshot(device.id))?.ports?.length ?? 0) === 0) continue
      void sweep(device, false)
    }
  }, [readsDone, clients.data, interfaces.data, devices, failed, client, sweep])

  return (
    <section aria-label={t('Network equipment')} className="space-y-4">
      <p className="max-w-prose text-xs leading-5 text-muted-foreground">
        {t('The network on a card is the one its management address belongs to. Opening this page reads every box that has not answered yet in this session.')}
      </p>

      {config.isError ? (
        <p className="rounded-xl border border-border bg-card px-4 py-3 text-sm text-muted-foreground">
          {t('The device inventory could not be read, so no box can be placed here.')}
        </p>
      ) : config.isPending ? (
        <Skeleton className="h-24 w-full rounded-xl" />
      ) : devices.length === 0 ? (
        <Empty />
      ) : (
        <ul className="grid items-start gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {devices.map(device => (
            <EquipmentCard
              key={device.id}
              device={device}
              networks={networks}
              byMac={byMac}
              highlight={highlight === device.id}
              reading={reading !== undefined}
              failed={failed[device.id]}
              knownAddresses={macs.length}
              sources={sources}
              waiting={!readsDone}
              onMapPorts={() => void sweep(device, true)}
            />
          ))}
        </ul>
      )}

      <p className="max-w-prose text-xs leading-5 text-muted-foreground">
        {t('The cables are inferred: a switch says which port an address arrived on, an access point says who is on its radio, and the default route says which box is upstream. An address goes under the box nearest to it.')}
      </p>
    </section>
  )
}

/**
 * Nobody has added a box, and the page has nothing to read. It is the one place
 * in this picture where a person can make it better, so it says what adding one
 * buys instead of showing an empty grid.
 */
function Empty() {
  useLocale()
  return (
    <div className="rounded-xl border border-dashed border-border px-4 py-5">
      <Heading as="h2" size="sm">{t('No boxes yet')}</Heading>
      <p className="mt-1 max-w-prose text-xs leading-5 text-muted-foreground">
        {t('Add a switch or an access point on Devices and this page will read it, place it, and list the clients it reports.')}
      </p>
      <p className="mt-3 text-xs">
        <Link to="/devices" className="font-medium underline-offset-4 hover:underline">{t('Add a device')}</Link>
      </p>
    </div>
  )
}

function EquipmentCard({
  device,
  networks,
  byMac,
  highlight,
  reading,
  failed,
  knownAddresses,
  sources,
  waiting,
  onMapPorts,
}: {
  device: Device
  networks: readonly { name: string; subnet?: string }[]
  byMac: ReadonlyMap<string, ClientRow>
  highlight: boolean
  reading: boolean
  failed?: string
  knownAddresses: number
  sources: FarEndSources
  waiting: boolean
  onMapPorts: () => void
}) {
  useLocale()
  const state = useDeviceRead(device.id)
  const snapshot = state?.data
  const facts = equipmentFacts(snapshot)
  const attached = attachedClients(snapshot, byMac)
  const sweep = usePortSweep(device.id)
  const sweeping = sweep?.partial === true ? { done: sweep.asked, total: sweep.total ?? sweep.asked } : undefined
  // What each port has at the other end. A sweep is fresher than the read that
  // preceded it, so it wins; a reading that carried the table itself needs no
  // sweep at all, and the two are resolved the same way.
  const endpoints = useMemo(() => {
    if (sweep !== undefined) return sweep.byPort
    if (snapshot === undefined) return undefined
    const table = snapshot.collections?.fdb
    const read = table ? table.state === 'ok' : snapshot.fdb !== undefined
    if (!read || snapshot.fdb === undefined) return undefined
    return new Map([...learnedByPort(snapshot.fdb)].map(([port, addresses]) => [port, addresses.map(mac => farEnd(mac, sources))]))
  }, [sweep, snapshot, sources])
  const addresses = useMemo(
    () => endpoints === undefined ? undefined : new Map([...endpoints].map(([port, ends]) => [port, ends.length])),
    [endpoints],
  )
  const plugged = useMemo(() => [...(endpoints ?? [])]
    .flatMap(([port, ends]) => ends.map(end => ({ port, end })))
    .sort((a, b) => a.port.localeCompare(b.port, undefined, { numeric: true })), [endpoints])
  // A switch is anything we might ask: an explicit switch role, or the adapter
  // we know implements the lookup even when the operator left roles empty.
  const sweepable = isSweepable(device)
  const network = placeDevice(device, networks)
  const subtitle = [
    roleLabels(device).map(role => t(role)).join(' · '),
    t(adapterLabel(device.adapter)),
    addressLabel(device.address),
  ].filter(part => part !== '').join(' · ')

  // A callback ref, not an effect: the inventory arrives after this component
  // first renders, so an effect on `highlight` alone would run while there was
  // no card to scroll to.
  const ref = useCallback((node: HTMLLIElement | null) => {
    if (node !== null && highlight) node.scrollIntoView({ block: 'nearest' })
  }, [highlight])

  return (
    <li
      ref={ref}
      aria-current={highlight ? 'true' : undefined}
      className={`flex flex-col gap-2 rounded-xl border border-border bg-card p-3 ${highlight ? 'ring-2 ring-ring/60' : ''}`}
    >
      <div className="flex items-start gap-2">
        <div className="min-w-0 flex-1">
          <Link
            to={`/devices/${encodeURIComponent(device.id)}`}
            className="block truncate text-sm font-medium underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring"
          >
            {device.name || device.id}
          </Link>
          <p className="truncate text-xs text-muted-foreground">{subtitle}</p>
        </div>
        {network === undefined ? (
          <Badge variant="outline" className="text-muted-foreground">{t('No network here')}</Badge>
        ) : (
          <Badge variant="secondary">{network}</Badge>
        )}
      </div>

      {/* The box itself, drawn from the ports it reported: one shield per port,
          a lamp per port, lit only where the box said there was a carrier. */}
      {snapshot?.ports !== undefined && snapshot.ports.length > 0 && (
        <DeviceFace ports={snapshot.ports} addresses={addresses} form={formOf(device.adapter, device.roles)} />
      )}

      {snapshot === undefined ? (
        <p className="text-xs text-muted-foreground">
          {failed !== undefined
            ? t('Could not read it: {reason}', { reason: failed })
            : reading ? t('Reading…') : t('Not read this session')}
        </p>
      ) : (
        <div className="space-y-1.5">
          <ReadLine facts={facts} at={state?.dataUpdatedAt} />
          {attached.length > 0 && (
            <ul className="space-y-1 border-t border-border/60 pt-1.5">
              {attached.slice(0, 4).map(client => (
                <li key={client.mac}>
                  <Link
                    to={`/clients/${encodeURIComponent(client.mac)}`}
                    className="flex items-center gap-1.5 rounded text-xs hover:bg-accent/40 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring"
                  >
                    <Wifi aria-hidden className="size-3 shrink-0 text-muted-foreground" />
                    <span className="min-w-0 flex-1 truncate">{client.name}</span>
                    {client.signal !== undefined && (
                      <span className="text-muted-foreground tabular-nums">{client.signal} dBm</span>
                    )}
                  </Link>
                </li>
              ))}
              {attached.length > 4 && (
                <li className="text-xs text-muted-foreground">{t('and {count} more', { count: attached.length - 4 })}</li>
              )}
            </ul>
          )}
          {sweepable && (snapshot.ports?.length ?? 0) > 0 && (
            <div className="space-y-1 border-t border-border/60 pt-1.5">
              <div className="flex items-center gap-2">
                <span className="min-w-0 flex-1 text-xs text-muted-foreground">
                  {sweeping !== undefined
                    ? t('asking {done} of {total}', { done: sweeping.done, total: sweeping.total })
                    : sweep !== undefined
                      ? t('plugged in, read {when}', { when: readWhen(sweep.at) })
                      : waiting
                        ? t('Waiting for the boxes to be read')
                      : knownAddresses === 0
                        ? t('No address this router knows to ask about')
                        : t('Not asked yet')}
                </span>
                <Button
                  variant="secondary"
                  size="sm"
                  className="shrink-0"
                  onClick={onMapPorts}
                  disabled={waiting || sweeping !== undefined || knownAddresses === 0}
                >
                  {sweeping !== undefined ? t('Asking…') : sweep !== undefined ? t('Ask again') : t('Map ports')}
                </Button>
              </div>
              {plugged.length > 0 && (
                <ul className="space-y-0.5">
                  {plugged.slice(0, 8).map(({ port, end }) => (
                    <li key={`${port}-${end.mac}`} className="flex items-center gap-2 text-xs">
                      <span className="w-7 shrink-0 rounded bg-muted py-0.5 text-center text-[10px] leading-4 tabular-nums text-muted-foreground">
                        {port}
                      </span>
                      {end.deviceId !== undefined ? (
                        <Link to={`/devices/${encodeURIComponent(end.deviceId)}`} className="min-w-0 flex-1 truncate underline-offset-4 hover:underline">
                          {end.name}
                        </Link>
                      ) : end.kind === 'client' ? (
                        <Link to={`/clients/${encodeURIComponent(end.mac)}`} className="min-w-0 flex-1 truncate underline-offset-4 hover:underline">
                          {end.name}
                        </Link>
                      ) : (
                        <span className="min-w-0 flex-1 truncate">{t('this router')}</span>
                      )}
                      <span className="shrink-0 text-muted-foreground">
                        {end.kind === 'router' ? end.name : end.detail}
                      </span>
                    </li>
                  ))}
                  {plugged.length > 8 && (
                    <li className="pl-9 text-xs text-muted-foreground">{t('and {count} more', { count: plugged.length - 8 })}</li>
                  )}
                </ul>
              )}
              {sweep !== undefined && sweeping === undefined && plugged.length === 0 && (
                <p className="text-xs text-muted-foreground">{t('The switch placed none of them on a port.')}</p>
              )}
            </div>
          )}
        </div>
      )}
    </li>
  )
}

/**
 * The facts a card is allowed, in the order that survives truncation: ports,
 * then clients, then when this reading arrived. A reading with none of them
 * says so rather than printing three dashes.
 */
function ReadLine({ facts, at }: { facts: ReturnType<typeof equipmentFacts>; at?: number }) {
  useLocale()
  const parts: string[] = []
  if (facts?.ports) parts.push(t('{up} of {total} ports up', { up: facts.ports.up, total: facts.ports.total }))
  if (facts?.wireless !== undefined) {
    parts.push(facts.wireless === 0
      ? t('no clients')
      : facts.wireless === 1 ? t('1 client') : t('{count} clients', { count: facts.wireless }))
  }
  if (parts.length === 0) parts.push(t('It answered, and had nothing to say about its ports or clients.'))
  return (
    <p className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-xs">
      <span>{parts.join(' · ')}</span>
      {at !== undefined && <span className="text-muted-foreground">{t('read {when}', { when: readWhen(at) })}</span>}
    </p>
  )
}

/** A switch is anything we might ask: an explicit switch role, or the adapter we know implements the lookup. */
function isSweepable(device: Device): boolean {
  return device.roles?.includes('switch') === true || device.adapter === 'mercury'
}
