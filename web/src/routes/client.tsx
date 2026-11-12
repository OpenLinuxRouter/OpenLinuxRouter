import { t, useLocale } from '@/lib/i18n'
import { Heading } from '@/components/ui/heading'
import { Link, useParams } from 'react-router'

import { Button } from '@/components/ui/button'
import { ClientInspection } from '@/features/clients/inspection'
import { ActivityTotal, ClientActivity } from '@/features/clients/activity'
import { ClientInlineEditor } from '@/features/clients/client-inline-editor'
import { useClientList } from '@/features/clients/queries'
import { useGatewayTraffic } from '@/features/gateway/queries'
import { ClientServices } from '@/features/ingress/client-services'
import { ClientPolicy } from '@/features/qos/client-policy'
import { useTrafficView } from '@/features/topology/traffic'

export function ClientPage() {
  useLocale()
  const { mac } = useParams()
  const devices = useClientList()
  const traffic = useGatewayTraffic()
  const flow = useTrafficView(traffic.data, traffic.isError)
  const device = devices.data?.clients.find((d) => d.mac.toLowerCase() === mac?.toLowerCase())
  const usage = device && flow.flowOf(device)

  if (devices.isPending) return <p className="py-16 text-sm text-muted-foreground">{t('Loading client…')}</p>
  if (devices.isError) return <State title={t('Could not load clients')} detail="Try again when the router is reachable." />
  if (!device) return <State title={t('Client not found')} detail="It may have been forgotten or is no longer in the device list." />

  return (
    <div className="space-y-4 pb-12">
      <ClientInlineEditor key={device.mac} device={device} services={
        <ClientServices
          mac={device.mac}
          name={device.name}
          addresses={[device.fixed_ip ?? '', ...(device.ips ?? [])]}
          add={`/advanced/caddy?device=${encodeURIComponent(device.mac)}`}
        />
      } total={<ActivityTotal target={{ mac: device.mac }} />} activity={<ClientActivity target={{ mac: device.mac }} downRate={usage?.downRate} upRate={usage?.upRate} />} traffic={
        <ClientPolicy mac={device.mac} downRate={usage?.downRate} upRate={usage?.upRate}>
            {!flow.counting && (
              <p className="text-sm text-muted-foreground">{traffic.isError ? 'Traffic counters are unavailable right now, so live rates are missing.' : 'Counting is off, so there are no live rates. The total below shows what was recorded while it was on.'}</p>
            )}
        </ClientPolicy>
      } />

      <ClientInspection key={device.mac} mac={device.mac} addresses={device.ips ?? []} />
    </div>
  )
}

function State({ title, detail }: { title: string; detail: string }) {
  useLocale()
  return <div className="space-y-3 py-16 text-center">
    <Heading as="h1" size="lg">{title}</Heading>
    <p className="text-sm text-muted-foreground">{detail}</p>
    <Button variant="secondary" nativeButton={false} render={<Link to="/">{t('Back to overview')}</Link>} />
  </div>
}
