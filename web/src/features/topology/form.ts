import type { Chrome } from './face.ts'

/**
 * The kind of box a face is drawn as.
 *
 * A form is a class, not a model: a switch, a fiber gateway, an access point, a
 * machine that runs this router. The ports are always the ports the reading
 * listed — the form only decides what the chassis around them looks like and
 * what class mark it carries. Nothing about a specific model is claimed.
 */
export type DeviceForm = 'switch' | 'ont' | 'ap' | 'router' | 'generic'

/**
 * Which form a box is drawn as, from the two facts that name it: what the
 * inventory calls its adapter, and the roles the operator gave it. Roles win,
 * because they are a statement about this box in this network rather than about
 * a firmware family.
 */
export function formOf(adapter: string | undefined, roles?: readonly string[]): DeviceForm {
  for (const role of roles ?? []) {
    if (role === 'ont') return 'ont'
    if (role === 'switch') return 'switch'
    if (role === 'ap') return 'ap'
    if (role === 'router') return 'router'
  }
  if (adapter === 'mercury') return 'switch'
  if (adapter === 'huawei') return 'ont'
  // An OpenWrt box with no role stated is a router someone put on the LAN far
  // more often than anything else — but saying so would be a guess, so the
  // face stays plain until a role or an adapter names it.
  return 'generic'
}

/** The room each form needs for its own marks, in port units. */
export const FORM_CHROME: Record<DeviceForm, Chrome> = {
  switch: {},
  ont: { lead: 62 },
  ap: { lead: 56, top: 26 },
  router: { lead: 62 },
  generic: {},
}
