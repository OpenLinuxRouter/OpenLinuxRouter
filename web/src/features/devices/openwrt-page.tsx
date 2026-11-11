import { t, useLocale } from '@/lib/i18n'
import { Wifi } from 'lucide-react'
import { Link, useSearchParams } from 'react-router'

import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Heading } from '@/components/ui/heading'

import { formatUptime } from './device'
import { clock } from './device-format'
import {
  CapabilitiesDisclosure,
  CollectionCard,
  FactList,
  PortsTable,
  Unread,
  ViewHeader,
} from './device-parts'
import type { Device, Snapshot } from './types'

const TABS = ['overview', 'ports'] as const
type Tab = typeof TABS[number]

function tabOf(value: string | null): Tab {
  return TABS.includes(value as Tab) ? value as Tab : 'overview'
}

/**
 * A box running OpenWrt, read over ubus: an access point, a router, or both.
 *
 * Radios and clients belong to `/wireless`, which is the view built for them;
 * this page counts them and links there rather than drawing a second, poorer
 * copy. Ports, on the other hand, are its own: netifd's bridges and interfaces
 * are what this firmware calls its ports.
 */
export function OpenWrtPage({ device, snapshot }: { device: Device; snapshot: Snapshot }) {
  useLocale()
  const [params, setParams] = useSearchParams()
  const tab = tabOf(params.get('tab'))
  const setTab = (next: string) => setParams(next === 'overview' ? {} : { tab: next }, { replace: true })
  const wifi = (snapshot.radios?.length ?? 0) > 0 || Boolean(device.roles?.includes('ap'))

  return (
    <div className="space-y-4">
      <ViewHeader
        tabs={[
          { id: 'overview', label: t('Overview') },
          { id: 'ports', label: t('Ports') },
        ]}
        tab={tab}
        onChange={setTab}
        actions={
          <>
            <p className="text-xs text-muted-foreground">{t('Read {time}', { time: clock(snapshot.observed_at) })}</p>
            {wifi && (
              <Button variant="secondary" size="sm" nativeButton={false} render={
                <Link to={`/wireless?device=${encodeURIComponent(device.id)}`}><Wifi className="size-4" />{t('Wireless view')}</Link>
              } />
            )}
          </>
        }
      />
      {tab === 'ports' ? <PortsTable snapshot={snapshot} /> : <OverviewTab device={device} snapshot={snapshot} />}
    </div>
  )
}

function OverviewTab({ device, snapshot }: { device: Device; snapshot: Snapshot }) {
  useLocale()
  const identity = snapshot.identity
  const radiosRead = snapshot.collections.radios
  const radios = snapshot.radios ?? []

  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <Card className="gap-0 py-0">
        <div className="space-y-2 p-5">
          <Heading as="h2" size="sm">{t('Identity')}</Heading>
          <FactList
            facts={[
              { label: t('Model'), value: identity.model },
              { label: t('Hostname'), value: identity.hostname },
              { label: t('Firmware'), value: identity.firmware },
              { label: t('Uptime'), value: identity.uptime_seconds != null ? formatUptime(identity.uptime_seconds) : undefined },
              { label: t('Interface MACs'), value: identity.macs?.join(' · ') },
            ]}
          />
        </div>
      </Card>

      <Card className="gap-0 py-0">
        <div className="space-y-3 p-5">
          <Heading as="h2" size="sm">{t('Wireless')}</Heading>
          {radiosRead && radiosRead.state !== 'ok' ? (
            <Unread collection={radiosRead} what={t('radio state')} />
          ) : radios.length === 0 ? (
            <p className="text-sm leading-6 text-muted-foreground">{t('This box reported no radios. Its page is about the wire.')}</p>
          ) : (
            <FactList
              facts={[
                { label: t('Radios'), value: String(radios.length) },
                { label: t('Wireless networks'), value: String(snapshot.wireless?.length ?? 0) },
                { label: t('Connected clients'), value: String(snapshot.associations?.length ?? 0) },
              ]}
            />
          )}
        </div>
      </Card>

      <CollectionCard device={device} snapshot={snapshot} />

      <div className="lg:col-span-2">
        <CapabilitiesDisclosure snapshot={snapshot} />
      </div>
    </div>
  )
}
