import { t, useLocale } from '@/lib/i18n'
import { StatusInline } from '@/components/ui/status'
import { Plus } from 'lucide-react'
import { Link } from 'react-router'

import { useIngressConfig, useIngressStatus } from '@/features/ingress/queries'
import { ServiceIcon, serviceIconVersion } from '@/features/topology/nodes'
import { servicesOf } from '@/features/topology/services'

/**
 * The services this device is published as, and the way to publish another.
 *
 * One row, on two pages: a device on this network and a device that dialled in
 * over the tunnel. The match is the same for both because the module's two
 * upstream forms are the same — a `device` reference for something `devices`
 * knows by MAC, and a literal `host` for everything else — so a peer, which has
 * no MAC and is published by its tunnel address (docs/ingress.md §1.2), matches
 * on the address and nothing else.
 *
 * That is also why the add action takes its own link: a device prefills
 * `/advanced/caddy?device=<mac>`, a peer `?host=<address>`, and the dialog lands
 * on whichever target form the service will actually have.
 */
export function ClientServices({
  mac,
  name,
  addresses,
  add,
}: {
  /** The device's MAC, when `devices` knows it. */
  mac?: string
  /** A legacy service that names its upstream device by name rather than MAC. */
  name?: string
  /** Every address it has been seen at; a published host is matched against these. */
  addresses?: string[]
  /** Where the add action goes, prefilled for this target. */
  add: string
}) {
  useLocale()
  const ingress = useIngressConfig()
  const status = useIngressStatus()
  const domain = status.data?.domain
  const services = servicesOf(ingress.data?.services, { mac, name, addresses })

  return (
    <div className="flex flex-wrap items-center justify-end gap-2">
      {ingress.isError ? (
        <StatusInline tone="unknown">{t('Unavailable')}</StatusInline>
      ) : ingress.isPending ? (
        <span className="text-muted-foreground">{t('Loading…')}</span>
      ) : (
        services.map((service) => {
          // No version until the suffix that goes into it is known; the status
          // arrives a beat after the config, and a tile drawn before it would ask
          // for an icon under a name that is about to change.
          const version = status.isPending ? undefined : serviceIconVersion(service, domain)
          const icon = <ServiceIcon key={version ?? ''} name={service.name} version={version} choice={service.icon} />
          const label = `${service.name}.${domain}${service.link_path ?? ''}`
          const className =
            'flex size-7 shrink-0 items-center justify-center rounded-md bg-muted transition-colors hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring'
          return domain ? (
            <a
              key={service.name}
              href={`https://${label}`}
              target="_blank"
              rel="noopener noreferrer"
              title={`Open ${label}`}
              aria-label={`Open ${label}`}
              className={className}
            >
              {icon}
            </a>
          ) : (
            <span key={service.name} title={status.isError ? `${service.name}: service address unavailable` : service.name} className={className}>
              {icon}
            </span>
          )
        })
      )}
      <Link
        to={add}
        aria-label={t('Add service for this client')}
        title={t('Add service')}
        className="flex size-7 shrink-0 items-center justify-center rounded-md border transition-colors hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring"
      >
        <Plus className="size-4" />
      </Link>
    </div>
  )
}
