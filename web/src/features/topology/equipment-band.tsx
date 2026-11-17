import { t, useLocale } from '@/lib/i18n'
import { Plus, Wifi } from 'lucide-react'
import { useEffect, useMemo, useRef } from 'react'
import { Link } from 'react-router'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Heading } from '@/components/ui/heading'
import { Skeleton } from '@/components/ui/skeleton'

import { useClientList } from '@/features/clients/queries'
import { adapterLabel, addressLabel, roleLabels } from '@/features/devices/device'
import { readWhen } from '@/features/devices/device-format'
import { useDeviceRead, useDevicesConfig, useReadDevice } from '@/features/devices/queries'
import type { Device } from '@/features/devices/types'
import { useInterfaces } from '@/features/link/queries'
import { attachedClients, equipmentFacts } from './equipment'
import { placeDevice } from './placement'
import type { ClientRow } from '@/lib/api-types'

/**
 * The boxes this router reads, as a strip above the map.
 *
 * It is a strip and not a row of the map on purpose. The map hangs the
 * operator's groups off the router and says, in its own legend, that a line is
 * membership; a card between the router and those columns would need every
 * group's line to run through it or squeeze past it, which is the one thing
 * that layout refuses to do (features/topology/layout.ts). A switch is also not
 * a client in anybody's group, so it has no place inside one.
 *
 * **Drawing this costs no reading.** Every card comes from the inventory, the
 * networks Link already reports, and — only for a box this session has already
 * read — the snapshot in the query cache. Opening the map must never start
 * eight HTTP conversations with eight boxes; the button on a card is how an
 * operator asks, one box at a time, and the card says when the answer arrived.
 *
 * Placement is a join, not a claim. A box carries the network whose range holds
 * its management address, and the section says so once: it is where the box is
 * managed from, not where its cables are. What the map still cannot draw — a
 * cable between two boxes — stays outside this component's vocabulary.
 */
export function EquipmentBand({ highlight }: { highlight?: string }) {
  useLocale()
  const config = useDevicesConfig()
  const interfaces = useInterfaces()
  const clients = useClientList()
  const byMac = useMemo(
    () => new Map((clients.data?.clients ?? []).map(client => [client.mac.toLowerCase(), client])),
    [clients.data],
  )
  const networks = interfaces.data?.networks ?? []
  const devices = config.data?.devices ?? []

  return (
    <section aria-labelledby="topology-equipment" className="space-y-3">
      <div className="space-y-1">
        <Heading as="h2" size="sm" id="topology-equipment">{t('Network equipment')}</Heading>
        <p className="max-w-prose text-xs leading-5 text-muted-foreground">
          {t('Where each box is managed from, and what this session read. A card carries the network its management address belongs to; drawing the page asks no box anything.')}
        </p>
      </div>

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
            />
          ))}
        </ul>
      )}
    </section>
  )
}

/**
 * Nobody has added a box, which is the normal state of a fresh install and the
 * only place in this picture where a person can make it better. It is a link
 * and not a second copy of the dialog: adding lives on Devices, where the
 * credential, the type and the first read already are.
 */
function Empty() {
  useLocale()
  return (
    <div className="rounded-xl border border-dashed border-border px-4 py-5">
      <p className="text-sm font-medium">{t('No boxes yet')}</p>
      <p className="mt-1 max-w-prose text-xs leading-5 text-muted-foreground">
        {t('Add a switch or an access point and it appears here with the clients it reports. Until then this page can only draw the networks this router serves.')}
      </p>
      <div className="mt-3">
        <Button
          variant="secondary"
          size="sm"
          nativeButton={false}
          render={<Link to="/devices"><Plus className="size-4" />{t('Add a device')}</Link>}
        />
      </div>
    </div>
  )
}

function EquipmentCard({
  device,
  networks,
  byMac,
  highlight,
}: {
  device: Device
  networks: readonly { name: string; subnet?: string }[]
  byMac: ReadonlyMap<string, ClientRow>
  highlight: boolean
}) {
  useLocale()
  const state = useDeviceRead(device.id)
  const read = useReadDevice()
  const snapshot = state?.data
  const facts = equipmentFacts(snapshot)
  const attached = attachedClients(snapshot, byMac)
  const network = placeDevice(device, networks)
  const subtitle = [
    roleLabels(device).map(role => t(role)).join(' · '),
    t(adapterLabel(device.adapter)),
    addressLabel(device.address),
  ].filter(part => part !== '').join(' · ')

  const ref = useRef<HTMLLIElement>(null)
  useEffect(() => {
    if (highlight) ref.current?.scrollIntoView({ block: 'nearest' })
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

      {snapshot === undefined ? (
        <div className="flex items-center gap-2">
          <p className="min-w-0 flex-1 text-xs text-muted-foreground">
            {read.isError
              ? t('Could not read it: {reason}', { reason: read.error instanceof Error ? read.error.message : t('no answer') })
              : t('Not read in this session')}
          </p>
          <Button variant="secondary" size="sm" onClick={() => read.mutate(device.id)} disabled={read.isPending}>
            {read.isPending ? t('Reading…') : t('Read')}
          </Button>
        </div>
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
        </div>
      )}
    </li>
  )
}

/**
 * The three facts a card is allowed, in the order that survives truncation:
 * ports, then clients, then when this reading arrived. A reading with none of
 * them says so rather than printing three dashes.
 */
function ReadLine({ facts, at }: { facts: ReturnType<typeof equipmentFacts>; at?: number }) {
  useLocale()
  const parts: string[] = []
  if (facts?.ports) parts.push(t('{up} of {total} ports up', { up: facts.ports.up, total: facts.ports.total }))
  if (facts?.wireless !== undefined) {
    parts.push(facts.wireless === 0 ? t('no clients') : t('{count} clients', { count: facts.wireless }))
  }
  if (parts.length === 0) parts.push(t('It answered, and had nothing to say about its ports or clients.'))
  return (
    <p className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-xs">
      <span>{parts.join(' · ')}</span>
      {at !== undefined && <span className="text-muted-foreground">{t('read {when}', { when: readWhen(at) })}</span>}
    </p>
  )
}
