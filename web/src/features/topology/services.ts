import type { ClientRow } from '@/lib/api-types'
import type { IngressConfig, Service } from '@/lib/config-types'

/** Only attach a manually addressed service when its target identifies one device. */
export function servicesByClient(config: IngressConfig | undefined, devices: ClientRow[]): Map<string, Service[]> {
  const result = new Map<string, Service[]>()
  if (!config?.enabled) return result

  const byName = new Map<string, string | null>()
  const byAddress = new Map<string, string | null>()
  for (const device of devices) {
    byName.set(device.mac.toLowerCase(), device.mac)
    for (const name of new Set([device.name, device.hostname].filter((name): name is string => Boolean(name)))) {
      const key = name.toLowerCase()
      if (!byName.has(key)) byName.set(key, device.mac)
      else if (byName.get(key) !== device.mac) byName.set(key, null)
    }
    for (const ip of new Set([device.fixed_ip, ...(device.ips ?? [])].filter((ip): ip is string => Boolean(ip)))) {
      if (!byAddress.has(ip)) byAddress.set(ip, device.mac)
      else if (byAddress.get(ip) !== device.mac) byAddress.set(ip, null)
    }
  }

  for (const service of config.services ?? []) {
    const mac = service.upstream.client
      ? byName.get(service.upstream.client.toLowerCase())
      : byAddress.has(service.upstream.host ?? '')
        ? byAddress.get(service.upstream.host ?? '')
        : byName.get((service.upstream.host ?? '').toLowerCase())
    if (!mac) continue
    result.set(mac, [...(result.get(mac) ?? []), service])
  }
  return result
}

/**
 * The services one target is the upstream of, on that target's own page.
 *
 * Looser than `servicesByClient` on purpose: that one decides which device a
 * manually addressed service belongs to *among all of them* and refuses when the
 * answer is ambiguous, while this one is asked about a device already named by
 * the URL, so its addresses and its stored name are answers rather than
 * candidates.
 *
 * Both upstream forms are here for the same reason: `device` is stored as a MAC
 * now and as a name in older documents, and `host` is the literal address an
 * upstream that is not a device is published at — which is how a device that
 * dialled in over the tunnel is published at all (docs/ingress.md §1.2).
 */
export function servicesOf(
  services: Service[] | undefined,
  target: { mac?: string; name?: string; addresses?: string[] },
): Service[] {
  const mac = target.mac?.toLowerCase()
  const addresses = new Set((target.addresses ?? []).filter(Boolean).map((address) => address.toLowerCase()))
  return (services ?? []).filter((service) => {
    const upstream = service.upstream
    if (mac && upstream.client?.toLowerCase() === mac) return true
    if (target.name && upstream.client === target.name) return true
    return Boolean(upstream.host && addresses.has(upstream.host.toLowerCase()))
  })
}
