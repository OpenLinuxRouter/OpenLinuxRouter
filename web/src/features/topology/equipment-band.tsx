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
import { attachedClients, equipmentFacts, farEnd, learnedByPort, planReads, type FarEndSources, type LearnedAddress } from './equipment'
import { rememberSweep, usePortSweep } from './sweep'
import { placeDevice } from './placement'
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
  const [sweeping, setSweeping] = useState<{ id: string; done: number; total: number } | undefined>(undefined)

  const devices = config.data?.devices ?? []
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
    })()
    return () => { cancelled = true }
    // The ref, not the dependencies, is what keeps this to one pass: the
    // mutation object is new on every render, and the inventory refetches.
  }, [client, config.data, readDevice])

  // Every address worth asking about: the clients this router knows, and its
  // own interfaces — a switch's port that leads back here is a cable too.
  const sources: FarEndSources = useMemo(() => {
    const self = new Map<string, string>()
    for (const row of interfaces.data?.interfaces ?? []) {
      const mac = row.mac?.toLowerCase()
      if (mac !== undefined && mac !== '' && mac !== '00:00:00:00:00:00') self.set(mac, row.name)
    }
    return {
      self,
      clients: new Map((clients.data?.clients ?? []).map(client => [client.mac.toLowerCase(), client])),
      devices: config.data?.devices ?? [],
    }
  }, [clients.data, config.data, interfaces.data])

  const macs = useMemo(() => [...new Set([
    ...(clients.data?.clients ?? []).map(client => client.mac.toLowerCase()),
    ...sources.self.keys(),
  ].filter(mac => mac !== ''))], [clients.data, sources])

  // Which port each known address is on is one lookup per address — the firmware
  // searches a MAC, it does not dump a table — so this is a deliberate sweep,
  // never part of opening the page, and its result stays for the session.
  const mapPorts = useCallback(async (device: Device) => {
    setSweeping({ id: device.id, done: 0, total: macs.length })
    const learned: LearnedAddress[] = []
    let failedLookups = 0
    for (const [index, mac] of macs.entries()) {
      try {
        const result = await api.get<FDBResult>(`/api/devices/${encodeURIComponent(device.id)}/fdb?${new URLSearchParams({ mac })}`)
        for (const entry of result.entries) learned.push({ mac: entry.mac, port_id: entry.port_id })
      } catch {
        failedLookups += 1
      }
      setSweeping(current => current?.id === device.id ? { ...current, done: index + 1 } : current)
    }
    const byPort = new Map([...learnedByPort(learned)].map(([port, addresses]) => [port, addresses.map(mac => farEnd(mac, sources))]))
    rememberSweep(device.id, {
      at: Date.now(),
      byPort,
      placed: [...byPort.values()].reduce((sum, ends) => sum + ends.length, 0),
      asked: macs.length,
      failed: failedLookups,
    })
    setSweeping(undefined)
  }, [macs, sources])

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
              sweeping={sweeping?.id === device.id ? sweeping : undefined}
              busy={sweeping !== undefined}
              onMapPorts={() => void mapPorts(device)}
            />
          ))}
        </ul>
      )}

      <p className="max-w-prose text-xs leading-5 text-muted-foreground">
        {t('What a port leads to is the switch\'s own word: it saw those addresses arrive there. A device behind another switch is learned on the uplink with everything else behind it, and no line is drawn between two boxes, because nothing on this wire reports what it is plugged into.')}
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
  sweeping,
  busy,
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
  sweeping?: { done: number; total: number }
  busy: boolean
  onMapPorts: () => void
}) {
  useLocale()
  const state = useDeviceRead(device.id)
  const snapshot = state?.data
  const facts = equipmentFacts(snapshot)
  const attached = attachedClients(snapshot, byMac)
  const sweep = usePortSweep(device.id)
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
  const sweepable = device.roles?.includes('switch') === true || device.adapter === 'mercury'
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
                      : knownAddresses === 0
                        ? t('No address this router knows to ask about')
                        : t('Not asked yet')}
                </span>
                <Button
                  variant="secondary"
                  size="sm"
                  className="shrink-0"
                  onClick={onMapPorts}
                  disabled={busy || knownAddresses === 0}
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
              {sweep !== undefined && plugged.length === 0 && (
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
