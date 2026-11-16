import { t, useLocale } from '@/lib/i18n'
import { Info } from 'lucide-react'
import { useMemo } from 'react'
import { Link, useNavigate } from 'react-router'

import { useGroupActions } from '@/features/clients/group-actions'
import { useClientList, useClientsConfig } from '@/features/clients/queries'
import { useDhcpConfig } from '@/features/dhcp/queries'
import { useGatewayStatus, useGatewayTraffic } from '@/features/gateway/queries'
import { useIngressConfig, useIngressStatus } from '@/features/ingress/queries'
import { useRemoteStatus } from '@/features/remote/queries'
import { servicesByClient } from '@/features/topology/services'

import { buildDialIn } from './dial-in'
import { NetworkMap } from './network-map'
import { useTrafficView, type TrafficView } from './traffic'
import type { GatewayTraffic } from '@/lib/api-types'

/**
 * The map, with the module reads it needs and the actions it offers.
 *
 * It lives beside the map rather than in a page because two pages draw it —
 * Overview, where it answers "what is on my network", and Topology, where it is
 * the whole answer. Both go through the same query cache, so the second page
 * costs no extra requests; the wiring is here once so the two cannot drift.
 */
export function NetworkMapPanel({ density = 'auto' }: { density?: 'auto' | 'detail' | 'compact' }) {
  const devices = useClientList()
  const identity = useClientsConfig()
  const gateway = useGatewayStatus()
  const dhcpConfig = useDhcpConfig()
  const ingress = useIngressConfig()
  const ingressStatus = useIngressStatus()
  const remote = useRemoteStatus()
  // Only the live rates need a fast sample; the other observed data stays at 5s.
  const traffic = useGatewayTraffic(1000)
  const flows = useTrafficView(traffic.data, traffic.isError)
  const services = useMemo(
    () => servicesByClient(ingress.data, devices.data?.clients ?? []),
    [ingress.data, devices.data],
  )
  // The tunnel's own list, not a second reading of the kernel: a peer is not a
  // device and does not belong in `devices` (docs/remote-access.md §3.3).
  const dialIn = useMemo(() => buildDialIn(remote.data), [remote.data])
  const navigate = useNavigate()
  const groupActions = useGroupActions()

  return (
    <div className="space-y-4">
      <NetworkMap
        devices={devices.data?.clients ?? []}
        services={services}
        serviceDomain={ingressStatus.data?.domain}
        serviceDomainKnown={!ingressStatus.isPending}
        groups={identity.data?.groups}
        traffic={flows}
        assignments={gateway.data?.assignments}
        exits={gateway.data?.exits}
        dialIn={dialIn}
        pools={dhcpConfig.data?.pools}
        pending={devices.isPending}
        density={density}
        onSelect={(device) => navigate(`/clients/${device.mac}`)}
        onOpenDialIn={(peer) => navigate(`/clients/wireguard/${encodeURIComponent(peer.name)}`)}
        onCreateGroup={groupActions.create}
        onRenameGroup={groupActions.rename}
        onDeleteGroup={groupActions.remove}
        onMoveGroup={groupActions.moveGroup}
      />

      <TrafficNote traffic={traffic.data} failed={traffic.isError} flows={flows} />
      {groupActions.dialogs}
    </div>
  )
}

function TrafficNote({
  traffic,
  failed,
  flows,
}: {
  traffic?: GatewayTraffic
  failed: boolean
  flows: TrafficView
}) {
  useLocale()
  if ((!traffic && !failed) || flows.counting) return null
  return (
    <p className="flex items-start gap-2 text-xs text-muted-foreground">
      <Info className="mt-px size-3.5 shrink-0" aria-hidden />
      <span>
        {failed
          ? 'Traffic counts could not be read, so the lines do not show how busy each group is.'
          : 'Traffic is not being counted, so the lines do not show how busy each group is.'}{' '}
        <Link to="/gateway/interfaces#gateway-stats" className="font-medium text-foreground underline-offset-4 hover:underline"> {t('Traffic settings')} </Link>
      </span>
    </p>
  )
}
