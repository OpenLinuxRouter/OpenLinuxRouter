import { t } from '@/lib/i18n'
import { formatBytes } from '@/lib/utils'

import { formatCount } from './device'

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
