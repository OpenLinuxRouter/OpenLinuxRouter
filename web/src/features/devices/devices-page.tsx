import { t, useLocale } from '@/lib/i18n'
import { EthernetPort, Plus, TriangleAlert } from 'lucide-react'
import { useState } from 'react'
import { useNavigate } from 'react-router'

import { PageHeading } from '@/components/layout/page-heading'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Disclosure } from '@/components/ui/disclosure'
import { Heading } from '@/components/ui/heading'
import { List, ListRow } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'

import { DeviceDialog, type DeviceDraft, type DeviceType } from './device-dialog'
import { adapterLabel, roleLabels } from './device'
import { useDeviceEditor, useDevicesConfig } from './queries'

/**
 * The inventory of boxes olr reads but does not configure.
 *
 * It is not the client list (that is MAC-keyed and lives on Overview) and not
 * Gateway's networks: those are subnets this router serves. A switch, an access
 * point and a router that happens to run OpenWrt are the same kind of entry
 * here, distinguished by roles; the adapter is the type, and it decides which
 * page the box gets: `switch-page`, `openwrt-page` or `ont-page`.
 *
 * The list is configuration only. Reading a device means contacting it, so
 * nothing on this page does that: a device is read when somebody opens it.
 */
export function DevicesPage() {
  useLocale()
  const navigate = useNavigate()
  const config = useDevicesConfig()
  const editor = useDeviceEditor()
  const [dialog, setDialog] = useState<'add' | null>(null)
  const devices = config.data?.devices ?? []
  const types: DeviceType[] = [
    { label: t('Huawei fiber ONT'), adapter: 'huawei', roles: ['ont'] },
    { label: t('Mercury switch'), adapter: 'mercury', roles: ['switch'] },
    { label: t('OpenWrt access point'), adapter: 'openwrt', roles: ['ap'] },
    { label: t('OpenWrt router'), adapter: 'openwrt', roles: ['router'] },
  ]

  const submit = async (draft: DeviceDraft) => {
    if (!(await editor.save(draft))) return
    setDialog(null)
    navigate(`/devices/${encodeURIComponent(draft.id)}`)
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
        description={t('Switches, access points and other boxes olr reads over their management interface. The list is configuration; a device is contacted only when you open it.')}
        action={<Button onClick={() => setDialog('add')}><Plus className="size-4" />{t('Add')}</Button>}
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
        <List>
          {devices.map(device => {
            const roles = roleLabels(device).map(role => t(role))
            return (
              <ListRow
                key={device.id}
                to={`/devices/${encodeURIComponent(device.id)}`}
                title={device.name || device.id}
                subtitle={[t(adapterLabel(device.adapter)), device.address].join(' · ')}
                trailing={roles.length > 0 ? roles.join(' · ') : undefined}
              />
            )
          })}
        </List>
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
