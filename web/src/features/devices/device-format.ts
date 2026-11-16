import { t } from '@/lib/i18n'
import { formatAgo, formatBytes } from '@/lib/utils'
import type { ReactNode } from 'react'

import { formatCount, formatUptime } from './device'
import type { Device, Snapshot } from './types'

/**
 * Numbers and times as a device page writes them, kept out of the component
 * modules so those export components only.
 */

/** Bytes when the device reports them, packets when it only counts packets. */
export function counters(bytes?: number, packets?: number): string {
  if (bytes != null) return formatBytes(bytes)
  if (packets != null) return `${formatCount(packets)} ${t('pkts')}`
  return '—'
}

/** The clock time of one dated observation, in the reader's own locale. */
export function clock(iso: string): string {
  const date = new Date(iso)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}

/** How long ago this session read the device, in the words the list and the page share. */
export function readWhen(readAt: number): string {
  const when = t(formatAgo(new Date(readAt).toISOString()))
  return when === 'now' ? t('just now') : when
}

export interface DeviceFact {
  label: string
  value?: ReactNode
  /** A unit or a qualifier that must not be read as part of the number. */
  note?: string
}

export function deviceFacts(device: Device, snapshot: Snapshot): DeviceFact[] {
  const identity = snapshot.identity
  const uptime = identity.uptime_seconds != null ? formatUptime(identity.uptime_seconds) : undefined
  const firmware = identity.firmware

  switch (device.adapter) {
    case 'mercury': {
      const ports = snapshot.ports ?? []
      const up = ports.filter(port => port.link_up === true).length
      const watts = ports.reduce((sum, port) => sum + (port.poe?.delivering ? port.poe.power_watts ?? 0 : 0), 0)
      return [
        { label: t('Ports up'), value: ports.length > 0 ? `${up} of ${ports.length}` : undefined },
        { label: t('PoE delivered'), value: watts > 0 ? watts.toFixed(1) : undefined, note: 'W' },
        { label: t('Uptime'), value: uptime },
        { label: t('Firmware'), value: firmware },
      ]
    }
    case 'openwrt': {
      const radios = snapshot.radios ?? []
      const associated = snapshot.associations?.length
      return [
        { label: t('Radios up'), value: radios.length > 0 ? `${radios.filter(radio => radio.enabled !== false).length} of ${radios.length}` : undefined },
        { label: t('Clients'), value: associated },
        { label: t('Uptime'), value: uptime },
        { label: t('Firmware'), value: firmware },
      ]
    }
    case 'huawei': {
      const ont = snapshot.ont
      // A snapshot is a wire format: a field the adapter did not send is absent,
      // whatever the type promises, and a page must not crash on one.
      const pon = ont?.pon?.state ?? (ont?.pon?.registered === true ? t('Registered') : undefined)
      return [
        { label: t('PON'), value: pon },
        { label: t('Optical'), value: ont?.optical?.rx_dbm != null ? ont.optical.rx_dbm.toFixed(1) : undefined, note: 'dBm' },
        { label: t('WAN address'), value: ont?.wan?.[0]?.address },
        { label: t('Uptime'), value: uptime },
      ]
    }
    default:
      return [{ label: t('Uptime'), value: uptime }]
  }
}
