import { Link } from 'react-router'

import { OverviewCard } from './card'
import { Heading } from '@/components/ui/heading'
import { useClientList } from '@/features/clients/queries'
import { useDhcpLeases } from '@/features/dhcp/queries'
import { useRemoteStatus } from '@/features/remote/queries'
import { usePublicAddresses } from '@/features/system/queries'
import { t, useLocale } from '@/lib/i18n'

function Row({ label, value, to, mono = false }: { label: string; value: string; to?: string; mono?: boolean }) {
  return <div className="flex items-baseline justify-between gap-3 border-t border-border/60 py-1.5 first:border-t-0 first:pt-0 last:pb-0">
    <span className="shrink-0 text-xs text-muted-foreground">
      {to ? <Link to={to} className="underline-offset-4 hover:text-foreground hover:underline focus-visible:outline-2 focus-visible:outline-ring">{label}</Link> : label}
    </span>
    <span className={`min-w-0 truncate text-right text-xs tabular-nums ${mono ? 'font-mono' : ''}`}>{value}</span>
  </div>
}

/** The addresses and counts that describe the network, not the clients on it. */
export function NetworkCard() {
  useLocale()
  const addresses = usePublicAddresses()
  const leases = useDhcpLeases()
  const remote = useRemoteStatus()
  const clients = useClientList()

  const online = clients.data ? clients.data.clients.filter(client => client.online).length : undefined
  const usage = leases.data?.usage ?? []
  const active = usage.length > 0
    ? usage.reduce((sum, pool) => sum + pool.active, 0)
    : leases.data?.leases.filter(lease => lease.active).length
  const poolSize = usage.reduce((sum, pool) => sum + pool.size, 0)
  const peers = remote.data?.peers?.length
  const isp = addresses.data?.ipv4?.isp || addresses.data?.ipv4?.organization

  return <OverviewCard>
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <Heading as="h2" size="sm">{t('Network')}</Heading>
      {isp && <span className="ml-auto text-xs text-muted-foreground">{isp}</span>}
    </div>

    <div>
      <Row label={t('WAN IPv4')} value={addresses.data?.ipv4?.ip || '—'} mono />
      <Row label={t('IPv6')} value={addresses.data?.ipv6?.ip || '—'} mono />
      <Row label={t('DHCP leases')} to="/gateway/dhcp" value={active == null ? '—' : poolSize > 0 ? `${active} / ${poolSize}` : String(active)} />
      <Row label={t('Clients online')} to="/topology" value={online == null ? '—' : String(online)} />
      <Row label={t('Tunnel peers')} to="/access/wireguard" value={peers == null ? '—' : String(peers)} />
    </div>
  </OverviewCard>
}
