import { t, useLocale } from '@/lib/i18n'
import { Pencil, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router'

import { PageHeading } from '@/components/layout/page-heading'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import { DeviceDialog, type DeviceDraft } from './device-dialog'
import { adapterLabel, roleLabels } from './device'
import { Loading, Missing, PageError } from './device-parts'
import { ONTPage } from './ont-page'
import { OpenWrtPage } from './openwrt-page'
import { useDeviceEditor, useDevicesConfig, useNetworkSnapshot } from './queries'
import { SwitchPage } from './switch-page'
import type { Device, Snapshot } from './types'

/**
 * One box in the inventory, read on demand.
 *
 * The route, the inventory entry, the credential dialog and the snapshot are
 * shared; the page below the title is not. An adapter is a device type, and
 * each type gets its own view (`switch-page`, `ont-page`, `openwrt-page`) with
 * its own tabs and its own vocabulary — a switch answers "which VLAN is this
 * port in", an ONT answers "what is my optical budget". Sharing one tab strip
 * meant every page carried the union of all three and used `unsupported`
 * capabilities to hide what the box was never going to have; that is a type
 * distinction leaking into a capability list, not a feature.
 *
 * Nothing here writes to the device. Reading is what contacts it.
 */
export function DevicePage() {
  useLocale()
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const config = useDevicesConfig()
  const snapshot = useNetworkSnapshot(id)
  const editor = useDeviceEditor()
  const [dialog, setDialog] = useState(false)

  const device = config.data?.devices?.find(candidate => candidate.id === id)

  if (config.isPending) return <Loading />
  if (config.isError) return <PageError error={config.error} title={t('Could not read the device inventory')} />
  if (!device) return <Missing id={id} />

  const submit = async (draft: DeviceDraft) => {
    if (!(await editor.save(draft))) return
    setDialog(false)
  }
  const forget = async () => {
    if (!(await editor.remove(device.id))) return
    setDialog(false)
    navigate('/devices', { replace: true })
  }
  const model = snapshot.data?.identity.model

  return (
    <div className="space-y-6">
      <PageHeading
        breadcrumbs={[{ to: '/devices', label: t('Devices') }]}
        title={device.name || model || device.id}
        description={[t(adapterLabel(device.adapter)), ...roleLabels(device).map(role => t(role)), device.address].join(' · ')}
        action={
          <div className="flex flex-wrap items-center gap-2">
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

      {snapshot.isPending ? <Loading />
        : snapshot.isError ? <PageError error={snapshot.error} title={t('Could not read this device')} />
          : snapshot.data ? <DeviceView device={device} snapshot={snapshot.data} />
            : null}

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

/** The adapter is the device type. A box whose adapter we cannot read has no page. */
function DeviceView({ device, snapshot }: { device: Device; snapshot: Snapshot }) {
  switch (device.adapter) {
    case 'mercury':
      return <SwitchPage device={device} snapshot={snapshot} />
    case 'huawei':
      return <ONTPage device={device} snapshot={snapshot} />
    case 'openwrt':
      return <OpenWrtPage device={device} snapshot={snapshot} />
    default:
      return null
  }
}
