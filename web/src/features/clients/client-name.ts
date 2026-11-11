import type { ClientRow } from '@/lib/api-types'
import { t } from '@/lib/i18n'

/**
 * What a name is worth when no source is saying it right now.
 *
 * A device has three possible names, in the order the backend resolves them:
 * the operator's, the hostname the client announced and still announces, or the
 * hostname it announced the last time it was here (internal/clients/seen.go).
 * The third is why a phone left in a bag for two days keeps its name on the map
 * instead of turning into a MAC — but it is history, not something the device is
 * saying now, so the surfaces that can afford a word say which one it is.
 */

/** What an empty name field stands in for, said in the client's own terms. */
export function namePlaceholder(device: ClientRow): string {
  if (device.hostname) return `${device.hostname} (${t('what it calls itself')})`
  if (device.name_origin === 'remembered') return `${device.name} (${t('last known name')})`
  return device.mac
}

/** The sentence behind a remembered name, for a hint. Undefined when current. */
export function rememberedNameTitle(device: ClientRow): string | undefined {
  if (device.name_origin !== 'remembered') return undefined
  return device.name_as_of
    ? t('Last known name, heard {when}', { when: new Date(device.name_as_of).toLocaleString() })
    : t('Last known name')
}
