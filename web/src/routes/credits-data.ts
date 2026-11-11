// What the credits page lists is a feature's supplier, not the whole closure:
// a Go or web package earns a row only where it is the thing that carries a
// router capability. The complete lists stay in go.mod and web/package.json.
//
// One flat list, in the order a request meets the box. Versions for Go modules
// mirror go.mod; update both together. `distro` is a descriptor rather than a
// version: OLR does not pin what the box installs.
export type Credit = {
  name: string
  version: string
  url: string
  use: string
  logo?: string
}

export const CREDITS: readonly Credit[] = [
  { name: 'Linux kernel', version: 'distro', url: 'https://kernel.org/', use: 'Interfaces, routing, neighbours and packet filtering', logo: '/credit-logos/linux.svg' },
  { name: 'iproute2', version: 'distro', url: 'https://git.kernel.org/pub/scm/network/iproute2/iproute2.git', use: 'Traffic control for priorities and speed limits' },
  { name: 'vishvananda/netlink', version: 'v1.3.1', url: 'https://github.com/vishvananda/netlink', use: 'Interface, address, route and neighbour changes' },
  { name: 'systemd', version: 'distro', url: 'https://systemd.io/', use: 'Service supervision and unit state', logo: '/credit-logos/systemd.svg' },
  { name: 'go-systemd', version: 'v22.7.0', url: 'https://github.com/coreos/go-systemd', use: 'Starting, stopping and inspecting units over D-Bus' },
  { name: 'dnsmasq', version: 'distro', url: 'https://thekelleys.org.uk/dnsmasq/doc.html', use: 'DHCP and IPv6 router advertisements' },
  { name: 'Unbound', version: 'distro', url: 'https://nlnetlabs.nl/projects/unbound/about/', use: 'Recursion, DNSSEC validation and caching', logo: '/credit-logos/unbound.svg' },
  { name: 'unbound-anchor', version: 'distro', url: 'https://nlnetlabs.nl/projects/unbound/about/', use: 'Root trust anchor refresh for DNSSEC', logo: '/credit-logos/unbound.svg' },
  { name: 'nftables', version: 'distro', url: 'https://netfilter.org/projects/nftables/', use: 'NAT, firewall and DNS enforcement' },
  { name: 'google/nftables', version: 'v0.3.0', url: 'https://github.com/google/nftables', use: 'Netfilter rules written directly from Go' },
  { name: 'WireGuard', version: 'in-tree', url: 'https://www.wireguard.com/', use: 'VPN tunnels; wg loads keys and peers', logo: '/access-logos/wireguard.svg' },
  { name: '3proxy', version: 'distro', url: 'https://3proxy.org/', use: 'SOCKS5 server' },
  { name: 'tun2socks', version: 'v2.7.0', url: 'https://github.com/xjasonlyu/tun2socks', use: 'Carries proxied traffic through a TUN device' },
  { name: 'mitmproxy', version: 'distro', url: 'https://mitmproxy.org/', use: 'Optional device request inspection', logo: '/credit-logos/mitmproxy.svg' },
  { name: 'Caddy', version: 'v2', url: 'https://caddyserver.com/', use: 'HTTPS ingress, certificates and reverse proxy', logo: '/service-logos/caddy.svg' },
  { name: 'igmpproxy', version: 'distro', url: 'https://github.com/pali/igmpproxy', use: 'IGMP and IPTV multicast routing' },
  { name: 'ddns-go', version: '7aad574', url: 'https://github.com/jeessy2/ddns-go', use: 'DDNS provider code, ported' },
  { name: 'ip.sb', version: 'API', url: 'https://ip.sb', use: 'Public addresses and city locations' },
  { name: 'IEEE OUI registry', version: 'registry', url: 'https://standards-oui.ieee.org/', use: 'Client vendor identification', logo: '/credit-logos/ieee.svg' },
  { name: 'pro-bing', version: 'v0.9.1', url: 'https://github.com/prometheus-community/pro-bing', use: 'Ping, loss and reachability checks' },
  { name: 'cloudflare-speed-cli', version: 'v1.0.10', url: 'https://github.com/kavehtehrani/cloudflare-speed-cli', use: 'Reference speed transfer for network quality' },
  { name: 'fast-cli', version: 'v0.3.10', url: 'https://github.com/mikkelam/fast-cli', use: 'fast.com speed test service' },
  { name: 'speedtest-go', version: 'v1.8.3', url: 'https://github.com/showwin/speedtest-go', use: 'Selectable-server internet speed test' },
  { name: 'NextTrace', version: 'unpinned', url: 'https://github.com/nxtrace/NTrace-core', use: 'Traceroute and route maps', logo: '/tool-logos/nexttrace.webp' },
  { name: 'LibreSpeed', version: '6.3.1', url: 'https://github.com/librespeed/speedtest', use: 'LibreSpeed tool: browser-based LAN speed test (LGPLv3)' },
  { name: 'iperf3', version: 'distro', url: 'https://software.es.net/iperf/', use: 'Sustained wired throughput test' },
  { name: 'modernc.org/sqlite', version: 'v1.60.1', url: 'https://sqlite.org/', use: 'Runtime history and traffic samples', logo: '/credit-logos/sqlite.svg' },
]
