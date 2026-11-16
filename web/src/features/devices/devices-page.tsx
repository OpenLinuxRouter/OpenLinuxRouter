import { t, useLocale } from '@/lib/i18n'
import { EthernetPort, Plus, RefreshCw, TriangleAlert } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { toast } from 'sonner'

import { PageHeading } from '@/components/layout/page-heading'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Disclosure } from '@/components/ui/disclosure'
import { Heading } from '@/components/ui/heading'
import { List } from '@/components/ui/list'
import { RowChevron } from '@/components/ui/row-action'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusInline } from '@/components/ui/status'

import { deviceHealth } from '@/lib/state'
import { cn } from '@/lib/utils'

import { DeviceDialog, type DeviceDraft, type DeviceType } from './device-dialog'
import { DevicePortrait } from './device-parts'
import { readWhen } from './device-format'
import { addressLabel, isThisRouter, roleLabels } from './device'
import { useDeviceEditor, useDeviceRead, useDevicesConfig, useLocalInterfaces, useReadDevices } from './queries'
import type { Device, Snapshot } from './types'

/**
 * The inventory of boxes olr reads but does not configure.
 *
 * It is not the client list (that is MAC-keyed and lives on Overview) and not
 * Gateway's networks: those are subnets this router serves. A switch, an access
 * point and a router that happens to run OpenWrt are the same kind of entry
 * here, distinguished by roles; the adapter is the type, and it decides which
 * page the box gets: `switch-page`, `openwrt-page` or `ont-page`.
 *
 * The list is configuration only. Nothing here is polled: a device is read when
 * somebody opens it or presses Read all, and the status beside it says what
 * this session has actually learned. That is why a fresh load says Not checked
 * rather than drawing a green column that means "in a file somewhere".
 */
export function DevicesPage() {
  useLocale()
  const navigate = useNavigate()
  const config = useDevicesConfig()
  const editor = useDeviceEditor()
  const interfaces = useLocalInterfaces()
  const reads = useReadDevices()
  const [dialog, setDialog] = useState<'add' | null>(null)
  const devices = config.data?.devices ?? []
  const types: DeviceType[] = [
    { label: t('Huawei fiber ONT'), adapter: 'huawei', roles: ['ont'] },
    { label: t('Mercury switch'), adapter: 'mercury', roles: ['switch'] },
    { label: t('OpenWrt access point'), adapter: 'openwrt', roles: ['ap'] },
    { label: t('OpenWrt router'), adapter: 'openwrt', roles: ['router'] },
  ]

  // The console is served by the box whose inventory entry is "this router".
  // The host it was loaded from is one local address; the interface list is
  // the rest, so a console opened by name still recognises the box by address.
  const localHosts = useMemo(() => {
    const hosts = new Set<string>()
    if (typeof window !== 'undefined' && window.location.hostname) hosts.add(window.location.hostname)
    for (const row of interfaces.data?.interfaces ?? []) {
      if (row.address) hosts.add(row.address)
      for (const prefix of row.prefixes ?? []) hosts.add(prefix)
    }
    return hosts
  }, [interfaces.data])

  const submit = async (draft: DeviceDraft) => {
    if (!(await editor.save(draft))) return
    setDialog(null)
    navigate(`/devices/${encodeURIComponent(draft.id)}`)
  }
  const readAll = async () => {
    const failed = await reads.run(devices)
    if (failed > 0) toast.warning(t('{failed} of {total} devices did not answer', { failed, total: devices.length }))
  }
  const dialogElement = dialog === null ? null : (
    <DeviceDialog
      open
      onOpenChange={open => { if (!open) setDialog(null) }}
      types={types}
      existing={devices.map(device => device.id)}
      busy={editor.busy}
      onSubmit={submit}
    />
  )

  return (
    <div className="space-y-6">
      <PageHeading
        title={t('Devices')}
        description={t('Switches, access points and other boxes olr reads over their management interface. The list is configuration; a device is contacted only when you read it.')}
        action={
          <div className="flex flex-wrap items-center gap-2">
            {devices.length > 0 && (
              <Button variant="secondary" onClick={() => { void readAll() }} disabled={reads.progress !== null}>
                <RefreshCw className={cn('size-4', reads.progress !== null && 'animate-spin')} />
                {reads.progress
                  ? t('Reading {done} of {total}', { done: reads.progress.done, total: reads.progress.total })
                  : t('Read all')}
              </Button>
            )}
            <Button onClick={() => setDialog('add')}><Plus className="size-4" />{t('Add')}</Button>
          </div>
        }
      />

      {config.isError ? (
        <Alert variant="destructive">
          <TriangleAlert />
          <AlertTitle>{t('Could not read the device inventory')}</AlertTitle>
          <AlertDescription>{config.error.message}</AlertDescription>
        </Alert>
      ) : config.isPending ? (
        <Skeleton className="h-32 w-full" />
      ) : devices.length === 0 ? (
        <NoDevices onAdd={() => setDialog('add')} />
      ) : (
        <>
          <List>
            {devices.map(device => <DeviceRow key={device.id} device={device} localHosts={localHosts} />)}
          </List>
          <p className="max-w-prose text-xs leading-5 text-muted-foreground">
            {t('“Not answering” is not offline: the last read did not complete, and the link or the credential may be fine. Read times are from this session — a reload starts over at Not checked, because olrd keeps no snapshot history.')}
          </p>
        </>
      )}

      {devices.length > 0 && (
        <Disclosure summary={t('What can be read here')}>
          <div className="max-w-prose space-y-2 text-sm leading-6 text-muted-foreground">
            <p>{t('Every reading is dated evidence from the device’s own management interface, never a background poll. A section the adapter cannot read says so instead of showing an empty table.')}</p>
            <p>{t('A Mercury switch has a page for its ports, counters, VLANs, PVIDs and a MAC lookup. An OpenWrt box has a page for identity, ports and counters, with its radios and clients on the Wireless page.')}</p>
            <p>{t('A Huawei ONT has a page for the fiber line it terminates: registration, the optical budget, the carrier services on it and its LAN ports. It has no switch VLAN table or MAC table to read, so it has no page for them.')}</p>
            <p>{t('Nothing on these pages writes to the device: no port, VLAN, radio or reboot control.')}</p>
          </div>
        </Disclosure>
      )}

      {dialogElement}
    </div>
  )
}

/** How long ago this session read the device. "Now" is a word, not a duration. */
/**
 * One inventory entry, with the picture its roles imply and the state this
 * session has read for it.
 *
 * The row is a grid rather than a stack of text because roles, status and read
 * time have to line up down the list, and an operator scanning for the one box
 * that needs them should not have to read every line to find the odd one out.
 * Phones fold those three under the name; the portrait and the chevron stay
 * where they are.
 */
function DeviceRow({ device, localHosts }: { device: Device; localHosts: ReadonlySet<string> }) {
  useLocale()
  const read = useDeviceRead(device.id)
  const health = deviceHealth(read, obtainedNothing(read?.data))
  const roles = roleLabels(device)
  const model = read?.data?.identity.model
  const identity = [model, addressLabel(device.address), `${device.adapter} adapter`].filter(Boolean).join(' · ')
  // A failed read still happened at a time; that attempt is what the row is
  // reporting, so the later of the two answers is the honest one.
  const readAt = Math.max(read?.dataUpdatedAt ?? 0, read?.errorUpdatedAt ?? 0)
  const mine = isThisRouter(device, localHosts)

  return (
    <li className="bg-card">
      <Link
        to={`/devices/${encodeURIComponent(device.id)}`}
        className={cn(
          'grid min-h-16 items-center gap-x-3 gap-y-1.5 px-4 py-3 text-left transition-colors duration-150',
          'hover:bg-accent/60 active:bg-accent focus-visible:-outline-offset-2 focus-visible:outline-2 focus-visible:outline-ring sm:px-5',
          'grid-cols-[2.75rem_minmax(0,1fr)_1rem] lg:grid-cols-[2.75rem_minmax(0,1fr)_7.5rem_9rem_12rem_1rem] lg:gap-x-4',
        )}
      >
        <span className="col-start-1 row-span-2 row-start-1 self-start lg:row-span-1 lg:self-center">
          <DevicePortrait device={device} />
        </span>

        <span className="col-start-2 row-start-1 min-w-0">
          <span className="flex min-w-0 items-center gap-2">
            <span className="min-w-0 truncate text-sm font-medium text-foreground">{device.name || device.id}</span>
            {mine && (
              <Badge variant="outline" className="h-5 min-h-5 shrink-0 rounded px-1.5 py-0 text-[0.6875rem] font-medium text-muted-foreground">
                {t('This router')}
              </Badge>
            )}
          </span>
          <span className="mt-0.5 block text-[0.8125rem] leading-relaxed text-muted-foreground [overflow-wrap:anywhere]">{identity}</span>
        </span>

        <RowChevron className="col-start-3 row-span-2 row-start-1 self-center lg:col-start-6 lg:row-span-1" />

        <span className="col-start-2 row-start-2 flex flex-wrap items-center gap-x-2 gap-y-1.5 lg:contents">
          {roles.length > 0 && (
            <span className="flex flex-wrap gap-1 lg:col-start-3 lg:row-start-1">
              {roles.map(role => (
                <Badge key={role} variant="outline" className="h-5 min-h-5 rounded px-1.5 py-0 text-[0.6875rem] font-medium tracking-wide text-muted-foreground uppercase">
                  {t(role)}
                </Badge>
              ))}
            </span>
          )}
          <span className="lg:col-start-4 lg:row-start-1">
            <StatusInline tone={health.tone}>{health.label}</StatusInline>
          </span>
          <span className="text-xs leading-4 text-muted-foreground lg:col-start-5 lg:row-start-1 lg:text-right">
            {readAt > 0
              ? t('read {when}', { when: readWhen(readAt) })
              : <>
                  <span aria-hidden>—</span>
                  <span className="sr-only">{t('not read in this session')}</span>
                </>}
          </span>
        </span>
      </Link>
    </li>
  )
}

/**
 * The first box has to be added from here, not from a shell command: the
 * password belongs in this router's configuration, and a person using the page
 * cannot set a variable in olrd's environment.
 */
function NoDevices({ onAdd }: { onAdd: () => void }) {
  useLocale()
  return (
    <Card className="gap-0 py-0">
      <div className="space-y-3 p-6">
        <div className="flex items-center gap-2">
          <EthernetPort className="size-4 text-muted-foreground" />
          <Heading as="h2" size="sm">{t('No devices yet')}</Heading>
        </div>
        <p className="max-w-prose text-sm leading-6 text-muted-foreground">
          {t('Add a switch or access point by its management address. olr stores the credential here and reads the device on demand; it never configures it.')}
        </p>
        <Button onClick={onAdd}>
          <Plus className="size-4" />
          {t('Add a device')}
        </Button>
        <Disclosure summary={t('Add it from the router shell instead')}>
          <pre className="overflow-x-auto rounded-lg border border-border bg-muted/50 p-3 text-xs leading-5">
{`olr network add device core-switch \\
  --adapter mercury --address http://192.168.1.2 \\
  --username admin --password-env OLR_MERCURY_PASSWORD`}
          </pre>
        </Disclosure>
      </div>
    </Card>
  )
}

/** Whether a finished read obtained no usable section at all. */
function obtainedNothing(snapshot?: Snapshot): boolean {
  if (!snapshot) return false
  return !Object.values(snapshot.collections ?? {}).some(collection => collection.state === 'ok')
}
