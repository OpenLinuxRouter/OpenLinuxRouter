import type { ReactNode } from 'react'
import { Link } from 'react-router'

import { OverviewCard, OverviewCardHeader, OverviewMeter } from './card'
import { useDhcpLeases } from '@/features/dhcp/queries'
import { useRemoteStatus } from '@/features/remote/queries'
import { usePublicAddresses } from '@/features/system/queries'
import { t, useLocale } from '@/lib/i18n'

function Row({ label, children, to }: { label: string; children: ReactNode; to?: string }) {
  return <div className="flex min-h-8 items-center justify-between gap-3 border-t border-border/60 py-1 first:border-t-0 first:pt-0">
    <span className="shrink-0 text-sm text-muted-foreground">
      {to ? <Link to={to} className="underline-offset-4 hover:text-foreground hover:underline focus-visible:outline-2 focus-visible:outline-ring">{label}</Link> : label}
    </span>
    <span className="min-w-0 text-right text-sm tabular-nums">{children}</span>
  </div>
}

/** The addresses and counts that describe the network, not the clients on it. */
export function NetworkCard() {
  useLocale()
  const addresses = usePublicAddresses()
  const leases = useDhcpLeases()
  const remote = useRemoteStatus()

  const usage = leases.data?.usage ?? []
  const active = usage.length > 0
    ? usage.reduce((sum, pool) => sum + pool.active, 0)
    : leases.data?.leases.filter(lease => lease.active).length
  const poolSize = usage.reduce((sum, pool) => sum + pool.size, 0)
  const peers = remote.data?.peers?.length
  const ipv4 = addresses.data?.ipv4
  const isp = ipv4?.isp || ipv4?.organization
  const asn = ipv4?.asn

  return <OverviewCard bodyClassName="gap-4">
    <OverviewCardHeader title={t('Network')} meta={isp} />

    <div>
      <Row label={t('WAN IPv4')}><span className="block truncate font-mono">{ipv4?.ip || '—'}</span></Row>
      <Row label={t('IPv6')}><span className="block truncate font-mono">{addresses.data?.ipv6?.ip || '—'}</span></Row>
      {asn != null && <Row label="ASN"><span className="block truncate">AS{asn}</span></Row>}
      <Row label={t('DHCP leases')} to="/gateway/dhcp">
        <span className="flex items-center justify-end gap-3">
          {poolSize > 0 && <span className="w-16 sm:w-20"><OverviewMeter percent={(active ?? 0) / poolSize * 100} /></span>}
          {active == null ? '—' : poolSize > 0 ? `${active} / ${poolSize}` : String(active)}
        </span>
      </Row>
      <Row label={t('Tunnel peers')} to="/access/wireguard">{peers == null ? '—' : String(peers)}</Row>
    </div>
  </OverviewCard>
}
