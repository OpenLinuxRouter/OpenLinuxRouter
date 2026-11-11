import { t, useLocale } from '@/lib/i18n'
import { Link, Navigate, Route, Routes, useLocation } from 'react-router'
import { Heading } from '@/components/ui/heading'

import { AppShell } from '@/components/layout/app-shell'
import { SubPage } from '@/components/layout/sub-page'
import { EquipmentDevicePage, EquipmentPage } from '@/routes/equipment'
import { WirelessPage } from '@/routes/wireless'
import { Button } from '@/components/ui/button'
import { AuthGate } from '@/components/layout/auth-gate'
import { ConnectivityRecovery } from '@/components/layout/connectivity-recovery'
import { BackupPage } from '@/routes/advanced/backup'
import { DhcpAdvancedPage } from '@/routes/dhcp/advanced'
import { DhcpDetailsPage } from '@/routes/dhcp/details'
import { DhcpRangesPage } from '@/routes/dhcp/ranges'
import { DhcpReservationsPage } from '@/routes/dhcp/reservations'
import { DnsAdvancedPage } from '@/routes/dns/advanced'
import { DnsDetailsPage } from '@/routes/dns/details'
import { DnsBlockingPage } from '@/routes/dns/blocking'
import { DnsEnforcementPage } from '@/routes/dns/enforcement'
import { DnsListeningPage } from '@/routes/dns/listening'
import { DnsNamesPage } from '@/routes/dns/names'
import { DnsResolvingPage } from '@/routes/dns/resolving'
import { AccessPage } from '@/routes/access'
import { SocksOutPage } from '@/routes/access/socks-out'
import { AdministratorAccessPage } from '@/routes/advanced/access'
import { AdvancedPage } from '@/routes/advanced/index'
import { CaddyPage } from '@/routes/advanced/caddy'
import { DdnsPage } from '@/routes/advanced/ddns'
import { FilteringPage } from '@/routes/advanced/filtering'
import { ForwardsPage } from '@/routes/advanced/forwards'
import { IptvPage } from '@/routes/advanced/iptv'
import { RemotePage } from '@/routes/access/remote'
import { OverviewPage } from '@/routes/overview'
import { LatencyPage } from '@/routes/latency'
import { QualityPage } from '@/routes/quality'
import { MapPage } from '@/routes/map'
import { ToolPage } from '@/routes/tool-page'
import { DevicePage } from '@/routes/device'
import { WireguardPeerPage } from '@/routes/peer'
import { GroupPage } from '@/routes/group'
import { GatewayPage } from '@/routes/gateway/index'
import { GatewayInterfacesPage } from '@/routes/gateway/interfaces'
import { TunnelBrokerPage } from '@/routes/gateway/tunnelbroker'
import { GatewayInterfacePage } from '@/routes/gateway/interface'
import { GatewayExitsPage } from '@/routes/gateway/exits'
import { GatewayRoutesPage } from '@/routes/gateway/routes'
import { FirewallPage } from '@/routes/firewall'
import { FirewallRulesPage } from '@/routes/firewall-rules'
import { GatewayUnmanagedPage } from '@/routes/gateway/unmanaged'
import { CreditsPage } from '@/routes/credits'

export function App() {
  useLocale()
  return (
    <AuthGate>
      <ConnectivityRecovery />
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<OverviewPage />} />
          <Route path="latency" element={<LatencyPage />} />
          <Route path="map" element={<MapPage />} />
          <Route path="quality" element={<QualityPage />} />
          {/* Static segment before the MAC route: a device that dialed in is not
              in the device list, so it gets its own path rather than a made-up
              hardware address (docs/remote-access.md §3.3). */}
          <Route path="devices/wireguard/:name" element={<WireguardPeerPage />} />
          <Route path="devices/:mac" element={<DevicePage />} />
          <Route path="groups/:name" element={<GroupPage />} />
          <Route path="gateway">
            <Route index element={<GatewayPage />} />
            <Route path="interfaces" element={<GatewayInterfacesPage />} />
            <Route path="tunnelbroker" element={<RedirectWithState to="/access/tunnelbroker" />} />
            <Route path="interfaces/:name" element={<GatewayInterfacePage />} />
            <Route path="networks" element={<Navigate to="/gateway/dhcp" replace />} />
            <Route path="exits" element={<RedirectWithState to="/access/gateway" />} />
            <Route path="usage" element={<Navigate to="/gateway" replace />} />
            <Route path="ipv6" element={<Navigate to="/gateway/interfaces#forwarding" replace />} />
            <Route path="unmanaged" element={<GatewayUnmanagedPage />} />
            <Route path="routes" element={<GatewayRoutesPage />} />
            {/* The section root is the section's own page. `details` was the
                only address that rendered it, so it redirects here instead of
                leaving two URLs for one page. */}
            <Route path="dhcp">
              <Route index element={<DhcpDetailsPage />} />
              <Route path="details" element={<RedirectWithState to="/gateway/dhcp" />} />
              <Route path="ranges" element={<DhcpRangesPage />} />
              <Route path="reservations" element={<DhcpReservationsPage />} />
              <Route path="advanced" element={<DhcpAdvancedPage />} />
            </Route>

            {/* Detailed settings remain nested; the module's own page is where
                its status and the way into each setting lives. */}
            <Route path="dns">
              <Route index element={<DnsDetailsPage />} />
              <Route path="details" element={<RedirectWithState to="/gateway/dns" />} />
              <Route path="blocking" element={<DnsBlockingPage />} />
              <Route path="names" element={<DnsNamesPage />} />
              <Route path="resolving" element={<DnsResolvingPage />} />
              <Route path="listening" element={<DnsListeningPage />} />
              <Route path="enforcement" element={<DnsEnforcementPage />} />
              <Route path="advanced" element={<DnsAdvancedPage />} />
            </Route>
          </Route>
          <Route path="wireless" element={<WirelessPage />} />
          <Route path="equipment" element={<EquipmentPage />} />
          <Route path="equipment/:id" element={<EquipmentDevicePage />} />
          <Route path="firewall" element={<SubPage section="/advanced" slug="firewall"><FirewallPage /></SubPage>} />
          <Route path="firewall/rules" element={<FirewallRulesPage />} />
          <Route path="access" element={<AccessPage />} />
          <Route path="settings/access" element={<RedirectWithState to="/advanced/access" />} />
          <Route path="access/socks5" element={<SocksOutPage />} />
          <Route path="access/gateway" element={<GatewayExitsPage />} />
          <Route path="access/tunnelbroker" element={<TunnelBrokerPage />} />
          <Route path="access/wireguard" element={<RemotePage way="wireguard" />} />
          <Route path="access/socks-server" element={<RemotePage way="socks-server" />} />
          <Route path="access/ingress" element={<RedirectWithState to="/advanced/caddy" />} />
          <Route path="access/forwards" element={<RedirectWithState to="/advanced/forwards" />} />
          <Route path="access/filtering" element={<RedirectWithState to="/advanced/filtering" />} />
          <Route path="tools" element={<RedirectWithState to="/advanced" />} />
          <Route path="tools/speedtest" element={<ToolPage tool="speedtest" />} />
          <Route path="tools/cloudflare" element={<ToolPage tool="cloudflare" />} />
          <Route path="tools/fastcom" element={<ToolPage tool="fastcom" />} />
          <Route path="tools/librespeed" element={<ToolPage tool="librespeed" />} />
          <Route path="tools/iperf3" element={<ToolPage tool="iperf3" />} />
          <Route path="tools/nat" element={<ToolPage tool="nat" />} />
          <Route path="tools/ping" element={<ToolPage tool="ping" />} />
          <Route path="tools/traceroute" element={<ToolPage tool="traceroute" />} />
          <Route path="credits" element={<CreditsPage />} />
          <Route path="advanced">
            <Route path="backup" element={<BackupPage />} />
            <Route index element={<AdvancedPage />} />
            <Route path="caddy" element={<CaddyPage />} />
            <Route path="forwards" element={<ForwardsPage />} />
            <Route path="filtering" element={<FilteringPage />} />
            <Route path="access" element={<AdministratorAccessPage />} />
            <Route path="ddns" element={<DdnsPage />} />
            <Route path="remote" element={<RedirectWithState to="/access/wireguard" />} />
            <Route path="iptv" element={<IptvPage />} />
          </Route>

          {/* The addresses that moved. Redirected rather than deleted: a
              bookmark or a link in someone's notes should land where the thing
              went, not on a 404 that makes it look removed. docs/install.md has
              named /dhcp/interfaces in print since 0.1.0, so that one will be
              followed by people reading an older copy for a while yet. */}
          <Route path="internet" element={<Navigate to="/gateway" replace />} />
          <Route path="networks" element={<Navigate to="/gateway" replace />} />
          <Route path="networks/settings" element={<Navigate to="/gateway/interfaces" replace />} />
          <Route path="gateway/firewall" element={<Navigate to="/firewall" replace />} />
          <Route path="firewall/unmanaged" element={<RedirectWithState to="/advanced/filtering" />} />
          <Route path="gateway/forwards" element={<RedirectWithState to="/advanced/forwards" />} />
          <Route path="gateway/filtering" element={<RedirectWithState to="/advanced/filtering" />} />
          <Route path="remote" element={<RedirectWithState to="/access/wireguard" />} />
          <Route path="access/remote" element={<RedirectWithState to="/access/wireguard" />} />
          <Route path="ingress" element={<RedirectWithState to="/advanced/caddy" />} />
          <Route path="advanced/ingress" element={<RedirectWithState to="/advanced/caddy" />} />
          <Route path="devices" element={<Navigate to="/" replace />} />
          <Route path="dhcp/interfaces" element={<Navigate to="/gateway/interfaces" replace />} />
          <Route path="dhcp/*" element={<LegacySectionRedirect />} />
          <Route path="dns/*" element={<LegacySectionRedirect />} />

          <Route path="*" element={<NotFound />} />
        </Route>
      </Routes>
    </AuthGate>
  )
}

function NotFound() {
  useLocale()
  return (
    <div className="space-y-3 py-16 text-center">
      <Heading as="h1" size="lg">{t('Page not found')}</Heading>
      {/* An error page with no way out is a dead end, which on a phone means
          reaching for the URL bar. */}
      <Button variant="secondary" size="sm" nativeButton={false} render={<Link to="/">{t('Go to Overview')}</Link>} />
    </div>
  )
}

function LegacySectionRedirect() {
  useLocale()
  const { pathname, search, hash } = useLocation()
  return <Navigate to={`/gateway${pathname}${search}${hash}`} replace />
}

/** Carries a query string and fragment across an address that moved. */
function RedirectWithState({ to }: { to: string }) {
  useLocale()
  const { search, hash } = useLocation()
  return <Navigate to={`${to}${search}${hash}`} replace />
}
