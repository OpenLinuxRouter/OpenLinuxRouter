// The gallery's source of truth. One entry per page in the console; the build
// script emits four variants per entry (en/zh × light/dark) and skips any the
// capture library does not hold, so a missing theme never breaks the build.

export type ScreenGroup =
  | 'overview'
  | 'network'
  | 'devices'
  | 'security'
  | 'services'
  | 'diagnostics'
  | 'system'

export interface Screen {
  id: string
  group: ScreenGroup
  title: { en: string; zh: string }
  blurb: { en: string; zh: string }
}

export const groups: { id: ScreenGroup; en: string; zh: string }[] = [
  { id: 'overview', en: 'Overview', zh: '总览' },
  { id: 'network', en: 'Network', zh: '网络' },
  { id: 'devices', en: 'Clients', zh: '客户端' },
  { id: 'security', en: 'Access & firewall', zh: '访问与防火墙' },
  { id: 'services', en: 'Services', zh: '服务' },
  { id: 'diagnostics', en: 'Diagnostics', zh: '诊断' },
  { id: 'system', en: 'System', zh: '系统' },
]

export const screens: Screen[] = [
  {
    id: 'overview',
    group: 'overview',
    title: { en: 'Overview', zh: '总览' },
    blurb: {
      en: 'Status, traffic, latency and quality at a glance, on one calm page.',
      zh: '状态、流量、延迟与质量，一页看完。',
    },
  },
  {
    id: 'overview-full',
    group: 'overview',
    title: { en: 'Overview, full width', zh: '总览（宽屏）' },
    blurb: {
      en: 'The same page on a wide display, where every card gets its full row.',
      zh: '宽屏下的同一页，每张卡片占满整行。',
    },
  },
  {
    id: 'map',
    group: 'overview',
    title: { en: 'World traffic', zh: '世界流量' },
    blurb: {
      en: 'Where this network’s traffic actually goes, drawn as arcs over geography.',
      zh: '这个网络的流量实际去了哪里，用弧线画在地理上。',
    },
  },

  {
    id: 'interfaces',
    group: 'network',
    title: { en: 'Interfaces', zh: '接口' },
    blurb: {
      en: 'Every link the box has, and the network olr serves on it.',
      zh: '这台机器上的每条链路，以及 olr 在其上提供的网络。',
    },
  },
  {
    id: 'gateway-interfaces',
    group: 'network',
    title: { en: 'Uplinks', zh: '上行链路' },
    blurb: {
      en: 'Static addressing for the link that faces the internet.',
      zh: '面向互联网那条链路的静态地址配置。',
    },
  },
  {
    id: 'gateway-routes',
    group: 'network',
    title: { en: 'Routes', zh: '路由' },
    blurb: {
      en: 'The routing table as the kernel holds it, not as a form remembers it.',
      zh: '内核实际持有的路由表，而不是某个表单记住的版本。',
    },
  },
  {
    id: 'gateway-dhcp',
    group: 'network',
    title: { en: 'DHCP', zh: 'DHCP' },
    blurb: {
      en: 'Address ranges, reservations and the leases dnsmasq actually handed out.',
      zh: '地址范围、保留地址，以及 dnsmasq 实际发出的租约。',
    },
  },
  {
    id: 'gateway-dns',
    group: 'network',
    title: { en: 'DNS', zh: 'DNS' },
    blurb: {
      en: 'Unbound in front of the LAN, with the zones and forwards you declared.',
      zh: '面向内网的 Unbound，以及你声明的区域与转发。',
    },
  },
  {
    id: 'unmanaged',
    group: 'network',
    title: { en: 'Unmanaged links', zh: '未托管链路' },
    blurb: {
      en: 'Links olr can see but has not been handed. Nothing is touched until you adopt it.',
      zh: 'olr 看得到、但还没被交付的链路。你没有 adopted 之前，什么都不会动。',
    },
  },

  {
    id: 'device-mac',
    group: 'devices',
    title: { en: 'A client', zh: '一个客户端' },
    blurb: {
      en: 'Identity, presence, activity and the services it reaches, for one MAC.',
      zh: '按一个 MAC 看身份、在线状态、活动，以及它访问的服务。',
    },
  },
  {
    id: 'device-nuc',
    group: 'devices',
    title: { en: 'A server client', zh: '一台服务器客户端' },
    blurb: {
      en: 'The same view for a machine that is always on, where upload history matters.',
      zh: '同样是客户端视图，但这台常开，上传历史才是重点。',
    },
  },
  {
    id: 'wireless',
    group: 'devices',
    title: { en: 'Wireless', zh: '无线' },
    blurb: {
      en: 'Radios, linked clients, and the standard each link actually runs.',
      zh: '射频、已连接的客户端，以及每条链路实际跑的标准。',
    },
  },

  {
    id: 'firewall',
    group: 'security',
    title: { en: 'Firewall', zh: '防火墙' },
    blurb: {
      en: 'Named rules over nftables, readable without knowing nft syntax.',
      zh: '架在 nftables 之上的具名规则，不懂 nft 语法也读得懂。',
    },
  },
  {
    id: 'filtering',
    group: 'security',
    title: { en: 'Filtering', zh: '过滤' },
    blurb: {
      en: 'Blocklists and per-client policy, applied at the resolver and the packet filter.',
      zh: '黑名单与按客户端的策略，在解析器和包过滤层同时生效。',
    },
  },
  {
    id: 'dns-blocking',
    group: 'security',
    title: { en: 'DNS blocking', zh: 'DNS 拦截' },
    blurb: {
      en: 'Block ads and trackers once, for every client, without touching each device.',
      zh: '在一处拦掉广告和追踪，全体客户端生效，不用逐台设备装东西。',
    },
  },
  {
    id: 'forwards',
    group: 'security',
    title: { en: 'Port forwarding', zh: '端口转发' },
    blurb: {
      en: 'A named forward from the outside to one service inside.',
      zh: '一条具名的转发：从外部到内部某台服务。',
    },
  },
  {
    id: 'access',
    group: 'security',
    title: { en: 'Remote access', zh: '远程访问' },
    blurb: {
      en: 'The way back in, and who is currently on it.',
      zh: '回家的路，以及现在有谁在路上。',
    },
  },
  {
    id: 'access-gateway',
    group: 'security',
    title: { en: 'Remote access · gateway', zh: '远程访问 · 网关' },
    blurb: {
      en: 'The gateway side of remote access, including the route home it advertises.',
      zh: '远程访问的网关侧设置，包括它向外通告的回程路由。',
    },
  },
  {
    id: 'access-wireguard',
    group: 'security',
    title: { en: 'WireGuard peers', zh: 'WireGuard 对等体' },
    blurb: {
      en: 'Peers, keys, handshakes and transfer counts, straight from the interface.',
      zh: '对等体、密钥、握手与流量，直接来自接口本身。',
    },
  },

  {
    id: 'caddy',
    group: 'services',
    title: { en: 'HTTPS ingress', zh: 'HTTPS 入口' },
    blurb: {
      en: 'Certificates and reverse proxies for the services you host at home.',
      zh: '给家里自托管服务用的证书与反向代理。',
    },
  },
  {
    id: 'socks5',
    group: 'services',
    title: { en: 'SOCKS5 exit', zh: 'SOCKS5 出口' },
    blurb: {
      en: 'One client can leave through a different door than the rest of the LAN.',
      zh: '让某一台客户端从另一扇门出去，其余照旧。',
    },
  },
  {
    id: 'tunnelbroker',
    group: 'services',
    title: { en: 'IPv6 tunnel', zh: 'IPv6 隧道' },
    blurb: {
      en: 'A 6in4 tunnel when the provider gives you no native IPv6.',
      zh: '运营商不给原生 IPv6 时，用一条 6in4 隧道接上。',
    },
  },

  {
    id: 'ping',
    group: 'diagnostics',
    title: { en: 'Ping', zh: 'Ping' },
    blurb: {
      en: 'Run from the router itself, so the result describes the router’s path.',
      zh: '从路由器本机发起，测的是路由器这条路。',
    },
  },
  {
    id: 'traceroute',
    group: 'diagnostics',
    title: { en: 'Traceroute', zh: '路由追踪' },
    blurb: {
      en: 'Hop by hop, with the location of each address it can place.',
      zh: '逐跳展开，能定位的地址标出位置。',
    },
  },
  {
    id: 'speedtest',
    group: 'diagnostics',
    title: { en: 'Speed test', zh: '测速' },
    blurb: {
      en: 'Throughput between a client and the router, measured by LibreSpeed.',
      zh: '客户端到路由器之间的吞吐，由 LibreSpeed 实测。',
    },
  },
  {
    id: 'quality',
    group: 'diagnostics',
    title: { en: 'Quality', zh: '质量' },
    blurb: {
      en: 'Speed, latency, stability, wireless and services scored into one reading.',
      zh: '速度、延迟、稳定性、无线与服务，合成一个可读的分数。',
    },
  },

  {
    id: 'advanced',
    group: 'system',
    title: { en: 'System', zh: '系统' },
    blurb: {
      en: 'The escape hatch: raw backend configuration, versioned like everything else.',
      zh: '逃生门：原始后端配置，和其他一切一样受版本管理。',
    },
  },
  {
    id: 'backup',
    group: 'system',
    title: { en: 'Backup', zh: '备份' },
    blurb: {
      en: 'Export intent, restore it after validation, and see the scope before you commit.',
      zh: '导出意图，校验后再恢复，动手之前先看清范围。',
    },
  },
]

export const variants = [
  { lang: 'en', theme: 'light' },
  { lang: 'zh', theme: 'light' },
  { lang: 'en', theme: 'dark' },
  { lang: 'zh', theme: 'dark' },
] as const
