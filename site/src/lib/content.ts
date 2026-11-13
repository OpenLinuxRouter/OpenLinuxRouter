// The site's content model: what the product does, grouped the way an operator
// thinks about it. Shared by the home page (condensed) and /capabilities (full).
// Wording follows docs/ui-content-guide.md: decision first, no adjectives that
// do not carry information.

export type Lang = 'en' | 'zh'

export interface Capability {
  id: string
  icon: 'network' | 'clients' | 'traffic' | 'security' | 'services' | 'surfaces'
  title: { en: string; zh: string }
  summary: { en: string; zh: string }
  points: { en: string[]; zh: string[] }
  screen?: string
  /** The backend a reader would ask about, so the page can name it once. */
  backend: string
}

export const capabilities: Capability[] = [
  {
    id: 'network',
    icon: 'network',
    title: { en: 'Network and addressing', zh: '网络与地址' },
    summary: {
      en: 'Interfaces, subnets, DHCP, DNS and routing, declared once and rendered into the daemons that already do the work.',
      zh: '接口、子网、DHCP、DNS 与路由，声明一次，渲染进那些本来就在干活的守护进程。',
    },
    points: {
      en: [
        'adopt an interface, then name the network it serves',
        'DHCP ranges, reservations and live leases',
        'Unbound with zones, forwards and DNSSEC',
        'IPv6 router advertisements and 6in4 tunnels',
        'static uplinks, policy routes and prefix delegation',
      ],
      zh: [
        '交付一条接口，再给它的网络起名',
        'DHCP 地址范围、保留地址与实时租约',
        'Unbound 的区域、转发与 DNSSEC',
        'IPv6 路由通告与 6in4 隧道',
        '静态上行、策略路由与前缀委派',
      ],
    },
    screen: 'interfaces',
    backend: 'dnsmasq · unbound · netlink',
  },
  {
    id: 'clients',
    icon: 'clients',
    title: { en: 'Clients', zh: '客户端' },
    summary: {
      en: 'Every thing on the LAN, keyed by MAC: who it is, when it is here, what it talks to, and what it is allowed to do.',
      zh: '内网里的每一样东西，按 MAC 记账：它是谁、什么时候在、在跟谁说话、被允许做什么。',
    },
    points: {
      en: [
        'identity from leases, neighbours and the IEEE OUI registry',
        'fixed addresses without editing a config file',
        'activity, DNS history and service names per client',
        'groups, priority and per-client speed limits',
        'an exit per client when one device needs a different door',
      ],
      zh: [
        '身份来自租约、邻居表与 IEEE OUI 注册表',
        '固定地址，不用改配置文件',
        '按客户端看活动、DNS 历史与服务名',
        '分组、优先级与按客户端的限速',
        '某台设备需要另一扇门时，单独给它一个出口',
      ],
    },
    screen: 'device-mac',
    backend: 'kernel neighbours · dnsmasq leases',
  },
  {
    id: 'traffic',
    icon: 'traffic',
    title: { en: 'Traffic and quality', zh: '流量与质量' },
    summary: {
      en: 'Where the traffic goes and how good the link is, measured rather than guessed — then kept only as long as a screen can show it.',
      zh: '流量去了哪里、这条链路到底多好——测出来的，不是猜的；而且只留下屏幕能显示的那部分。',
    },
    points: {
      en: [
        'an activity matrix per client, day by day',
        'a world map of destinations, not a list of IPs',
        'latency samples to the sites you care about',
        'one quality score from speed, latency, stability, wireless and services',
        'LAN and internet throughput from the router’s side',
      ],
      zh: [
        '按客户端展开的活动矩阵，逐日看',
        '目的地画在世界地图上，而不是一串 IP',
        '对你关心的站点持续采样延迟',
        '速度、延迟、稳定性、无线与服务合成一个质量分',
        '从路由器一侧测内网与公网吞吐',
      ],
    },
    screen: 'quality',
    backend: 'conntrack · in-tree probe scheduler',
  },
  {
    id: 'security',
    icon: 'security',
    title: { en: 'Access and firewall', zh: '访问与防火墙' },
    summary: {
      en: 'Named rules over nftables, a way back in from outside, and filtering that applies once for every client.',
      zh: '架在 nftables 之上的具名规则、一条从外面回来的路，以及只配一次就全体生效的过滤。',
    },
    points: {
      en: [
        'firewall rules you can read without knowing nft syntax',
        'port forwards from a public port to one service',
        'DNS blocking and content filtering per network or client',
        'WireGuard peers and an easy way back home',
        'SOCKS5 and Shadowsocks exits for one client or all of them',
      ],
      zh: [
        '不懂 nft 语法也读得懂的防火墙规则',
        '从公网端口到某个服务的端口转发',
        '按网络或客户端生效的 DNS 拦截与内容过滤',
        'WireGuard 对等体，以及一条简单的回家路',
        '给一台或全部客户端用的 SOCKS5 与 Shadowsocks 出口',
      ],
    },
    screen: 'firewall',
    backend: 'nftables · WireGuard · 3proxy',
  },
  {
    id: 'services',
    icon: 'services',
    title: { en: 'Services you host', zh: '你托管的服务' },
    summary: {
      en: 'The things that make a home network usable from outside: certificates, names and a backup you can actually restore.',
      zh: '让家庭网络从外面也能用的东西：证书、域名，以及一份真的能恢复的备份。',
    },
    points: {
      en: [
        'HTTPS ingress with certificates managed by Caddy',
        'dynamic DNS that follows the address you actually get',
        'configuration export and validated restore',
        'systemd units you can read and change',
      ],
      zh: [
        '由 Caddy 管理证书的 HTTPS 入口',
        '跟着实际拿到的地址走的动态 DNS',
        '配置导出，以及先校验再恢复',
        '读得懂、改得动的 systemd 单元',
      ],
    },
    screen: 'caddy',
    backend: 'Caddy · ddns-go',
  },
  {
    id: 'surfaces',
    icon: 'surfaces',
    title: { en: 'One schema, four surfaces', zh: '一份 schema，四个表面' },
    summary: {
      en: 'The same configuration is a web form, a CLI flag, a REST body and an MCP tool. The UI is one client of the API, not a privileged path.',
      zh: '同一份配置，同时是网页表单、CLI 参数、REST 请求体和 MCP 工具定义。界面只是 API 的一个客户端，不是特权通道。',
    },
    points: {
      en: [
        'web UI — what this site screenshots',
        'CLI — one binary, same schema, scriptable',
        'REST API — the only thing the UI actually talks to',
        'MCP server — so an agent can read and change the network',
      ],
      zh: [
        '网页界面 —— 也就是这个站截图里的东西',
        'CLI —— 一个二进制，同一份 schema，可写脚本',
        'REST API —— 界面真正对话的唯一对象',
        'MCP server —— 让 agent 能读写这个网络',
      ],
    },
    backend: 'Go · JSON Schema',
  },
]

export const beliefs: { title: { en: string; zh: string }; body: { en: string; zh: string } }[] = [
  {
    title: { en: 'A package, not an operating system', zh: '是一个包，不是一个操作系统' },
    body: {
      en: 'No custom ISO, no reformatting. You keep your distro, your packages, your SSH keys — and apt remove gives the box back.',
      zh: '没有定制 ISO，不用重装。发行版、软件包、SSH 密钥都留着，apt remove 就能把机器还回去。',
    },
  },
  {
    title: { en: 'Drive the daemons; do not reimplement them', zh: '驱动守护进程，而不是重写它们' },
    body: {
      en: 'dnsmasq, unbound and nftables have decades of correctness in them. olr renders their configuration and supervises them through systemd.',
      zh: 'dnsmasq、unbound、nftables 里有几十年的正确性积累。olr 渲染它们的配置，用 systemd 托管它们。',
    },
  },
  {
    title: { en: 'Nothing happens until you say so', zh: '你不说，就什么都不动' },
    body: {
      en: 'Installing olr starts a control plane and changes nothing else. You hand it an interface explicitly before any module serves on it.',
      zh: '装上 olr 只会启动一个控制面，别的什么都不改。任何模块要在某条接口上提供服务，都得你先明确交付给它。',
    },
  },
  {
    title: { en: 'Hide complexity, never capability', zh: '隐藏复杂度，不隐藏能力' },
    body: {
      en: 'The default surface speaks your vocabulary. Every module also has a declared escape hatch that passes raw backend config through.',
      zh: '默认界面说的是你的语言。但每个模块都留了一个明确声明的逃生门，可以把原始后端配置直接喂进去。',
    },
  },
  {
    title: { en: 'Never store a fact twice', zh: '同一个事实只存一次' },
    body: {
      en: 'olr keeps your intent. The kernel keeps the interfaces, dnsmasq keeps the leases, nftables keeps the rules. Nothing can drift.',
      zh: 'olr 只记你的意图。接口在内核里，租约在 dnsmasq 里，规则在 nftables 里。两边不会各自漂移。',
    },
  },
  {
    title: { en: 'Linux is the extension surface', zh: 'Linux 就是扩展面' },
    body: {
      en: 'Need something we do not ship? apt install it, drop in a systemd unit, add your own nftables table. The distro is a better plugin system than any API.',
      zh: '我们需要但没自带的东西？apt install 它，写个 systemd 单元，或者加一张自己的 nftables 表。发行版比任何 API 都更像插件系统。',
    },
  },
]

export const install = {
  en: 'sudo apt install ./olr_<version>_<arch>.deb',
  zh: 'sudo apt install ./olr_<version>_<arch>.deb',
}

export const facts = {
  en: [
    { label: 'Runs on', value: 'Debian 13 · Ubuntu' },
    { label: 'Installed as', value: 'a .deb package' },
    { label: 'Surfaces', value: 'Web · CLI · REST · MCP' },
    { label: 'License', value: 'MIT' },
  ],
  zh: [
    { label: '运行于', value: 'Debian 13 · Ubuntu' },
    { label: '安装方式', value: '一个 .deb 包' },
    { label: '表面', value: '网页 · CLI · REST · MCP' },
    { label: '许可证', value: 'MIT' },
  ],
}
