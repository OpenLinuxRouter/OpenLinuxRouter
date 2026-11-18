import type { UpstreamIngress } from '@/lib/config-types'

/**
 * The client fields a prefill reads, which is the subset of the client list's
 * entry that decides where a request should go.
 */
export interface PrefillClient {
  mac: string
  name?: string
  fixed_ip?: string
  ips?: string[]
}

/**
 * Where a service opened from `?device=` or `?host=` points.
 *
 * A client with a reserved address is named by MAC — the upstream's `client`
 * field — because the daemon resolves that reference on every request and the
 * published name keeps working when its address changes. Anything else is named
 * by the address it answered on, which is all the dialog has for a box that
 * dialled in over the tunnel or a container on this router.
 *
 * The keys are the upstream's *current* ones, and keeping this out of the
 * component is the point. `client` was called `device` until the two
 * inventories were told apart; the daemon refuses an unknown field by name, so
 * a prefill spelling the old one produced a dialog that failed with "unknown
 * field \"device\"" on a field nobody had typed. Every upstream field is
 * optional, so the type checker cannot see that mistake — the test beside this
 * file asserts the key set instead of the shape.
 */
export function serviceTargetPrefill(client?: PrefillClient, host?: string): UpstreamIngress | undefined {
  if (client) {
    return client.fixed_ip && client.name
      ? { client: client.mac, port: 0 }
      : { host: addressOf(client), port: 0 }
  }
  const named = host?.trim()
  return named ? { host: named, port: 0 } : undefined
}

/**
 * The address to prefill for a client with no reserved one.
 *
 * IPv4 first, so the dialog shows something an operator recognizes rather than
 * the link-local or temporary IPv6 address that usually comes back first.
 */
function addressOf(client: PrefillClient): string {
  return client.fixed_ip || client.ips?.find((ip) => !ip.includes(':')) || client.ips?.[0] || ''
}
