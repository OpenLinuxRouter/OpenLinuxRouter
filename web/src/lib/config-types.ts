// Code generated from olrd's published JSON Schema. DO NOT EDIT.
//
// Regenerate with `make types` (olrd must be running).
// Source of truth: the Go config structs — see design.md §3.2 rule 3.
//
// Hand-edited again for `IngressConfig.domain` and `DnsConfig.private_domains`:
// a real `make types` run was available and produced both fields correctly, but
// replacing the file also renamed the generated group type (`ClientsGroup` →
// `Group`) and marked `NetworkIPv6.subnet` required, which the device and
// topology screens do not compile against. Those two are a separate piece of
// work — the schema and those screens currently disagree, and that predates this
// change — so this file stays hand-edited until it is done.
//
// HAND-EDITED, and it should not have been. The `firewall` module was deleted
// and its forwards moved into `gateway`; the generator needs a running olrd to
// talk to, and the environment that landed the move had none. The edits are the
// three the Go change implies — `FirewallConfig` gone, `GatewayConfig.forwards`
// and `.snat` added — and `make types` replaces the whole file with the real
// answer, which is what should happen before this ships.
//
// Device groups were added the same way and for the same reason:
// `ClientsConfig.groups`, `Client.group` and `ClientsGroup`, spelled the way
// internal/clients/config.go marshals them. So was `ClientsGroup.parent`, when
// groups learned to nest. `Client.icon` and its `ClientPicture` type, and the
// appliance categories, were checked against a real `make types` run and match
// it; that run could not replace the file because its group type is named
// differently from the `ClientsGroup` the UI imports.
//
// `FirewallConfig` is back, for a module that now does filter: one field,
// spelled the way internal/firewall/config.go marshals it.
//
// `GatewayConfig.ipv6_forwarding` was added the same way, spelled as
// internal/gateway/config.go marshals it, and so were `Network.ipv6` and
// `NetworkIPv6`, as internal/link/config.go marshals them, and `PoolIPv6.mtu`.
//
// The `modem` category is the same story again, and it was checked against a
// real run rather than assumed: a 2026-10-01 `make types` against a live olrd
// put 'modem' between 'sbc' and 'router', which is where it is below. That run
// could not replace the file, and the reason is worth writing down — it is the
// same reason as before, and it is now the whole file rather than one type.
// Regenerating renames `ClientsGroup` to `Group` and drops `ClientsGroup.parent`
// with it, which breaks `npm run build` for every module that imports it. So the
// file still cannot be regenerated wholesale until the UI is moved onto the
// generated names; until then this header is the record of what is hand-kept.
// `Service.link_path` follows internal/ingress/config.go for the same reason.
// `UpstreamIngress.request_headers` and `RequestHeaderIngress` follow it too.
// `TunnelBroker` was extracted from a real schema generation; optional omitzero
// fields are marked optional to match their JSON representation. Other modules
// retain the compatibility names documented above.
//
// `Peer.category` and `Peer.icon` are the latest of these: a device that dials
// in can be drawn like one at home, and internal/remote/wireguard.go borrows
// `clients.Category` and `clients.Icon` to say so — which a real run resolves
// to the `ClientCategory` and `ClientPicture` types spelled here.

/**
 * What kind of device this is. It selects the picture shown in the device list, and an operator-set value always beats a detected one. Empty means nothing has been set, so detection may answer; "unknown" means the device was looked at and could not be placed.
 */
export type ClientCategory =
  | ''
  | 'unknown'
  | 'phone'
  | 'tablet'
  | 'laptop'
  | 'desktop'
  | 'watch'
  | 'ereader'
  | 'tv'
  | 'speaker'
  | 'console'
  | 'camera'
  | 'doorbell'
  | 'thermostat'
  | 'sensor'
  | 'plug'
  | 'light'
  | 'vacuum'
  | 'airpurifier'
  | 'waterpurifier'
  | 'humidifier'
  | 'aircon'
  | 'airer'
  | 'fridge'
  | 'washer'
  | 'printer'
  | 'nas'
  | 'server'
  | 'sbc'
  | 'modem'
  | 'router'
  | 'accesspoint'
  | 'switch'
  | 'hub'
/**
 * A picture chosen for this device, overriding the one its category and vendor would pick: <vendor>/<category> such as "apple/laptop", optionally followed by /<variant> such as "apple/desktop/mini", <category>/<variant> such as "server/tower", os/<system> such as "os/debian", or thesvg:<slug> such as "thesvg:proxmox". Empty means the picture follows category and vendor.
 */
export type ClientPicture = string
export type IPAddress = string
export type IPAddress1 = string
/**
 * How IPv6 is served on this pool's interface. off serves no IPv6; slaac advertises the prefix and answers DHCPv6 information requests; stateful additionally hands out addresses over DHCPv6. Empty means off.
 */
export type RouterAdvertisementMode = '' | 'off' | 'slaac' | 'stateful'
/**
 * A duration written the way an operator would say it: a number and a unit, optionally repeated. Units are s, m, h, d and w. A bare number means seconds, matching dnsmasq.
 */
export type Duration = string
export type IPAddress2 = string
/**
 * An IPv4 or IPv6 address, such as 192.168.1.1 or 2001:db8::1.
 */
export type IPAddress3 = string
/**
 * An IPv4 or IPv6 address, such as 192.168.1.1 or 2001:db8::1.
 */
export type IPAddress4 = string
export type IPAddress5 = string
/**
 * Which provider hosts the zone this name lives in. cloudflare takes an API token. alidns is Alibaba Cloud DNS and tencentcloud is Tencent Cloud DNSPod; both take a key ID and a secret. callback is the generic form: olr requests a URL you supply with the address substituted into it, which is how the DynDNS-style endpoints — No-IP, DuckDNS, Dynu — are reached. The list grows by request rather than by completeness; a name outside it is refused rather than guessed at.
 */
export type DNSProvider = 'alidns' | 'callback' | 'cloudflare' | 'tencentcloud'
/**
 * interface reads the address off an uplink this router owns, which is right when olr terminates the WAN — PPPoE, or DHCP from the ISP. reflector asks an HTTPS endpoint what address the internet sees, which is right when olr sits behind a modem, and is the only form that can tell you your ISP has put you behind carrier-grade NAT. There is no default: olr will not choose between talking to a third party and not.
 */
export type WhereTheAddressComesFrom = 'interface' | 'reflector'
/**
 * A duration with a unit, such as 5m, 30s or 1m30s. Units are ns, us, ms, s, m and h.
 */
export type Duration1 = string
/**
 * An address and port, such as 192.168.1.1:53 or [2001:db8::1]:53.
 */
export type AddressAndPort = string
/**
 * An address and prefix length in CIDR form, such as 192.168.1.0/24.
 */
export type IPPrefix = string
/**
 * How names are resolved. recurse walks the DNS from the root, so no third party sees everything this network looks up and there is no forwarder to be down. forward sends every query to the servers listed instead, which is faster from cold and the only option where an upstream's own filtering is wanted. Empty means recurse.
 */
export type UpstreamMode = '' | 'recurse' | 'forward'
/**
 * An address and port, such as 192.168.1.1:53 or [2001:db8::1]:53.
 */
export type AddressAndPort1 = string
/**
 * An IPv4 or IPv6 address, such as 192.168.1.1 or 2001:db8::1.
 */
export type IPAddress6 = string
/**
 * An address and prefix length in CIDR form, such as 192.168.1.0/24.
 */
export type IPPrefix1 = string
/**
 * What a blocked name answers with. nxdomain says the name does not exist, which is the honest answer and the one clients cache and back off from. zero answers 0.0.0.0 and ::, which some networks prefer because an app that reads NXDOMAIN as "the network is down" will retry forever, where a refused connection fails at once. Empty means nxdomain.
 */
export type BlockedNameResponse = '' | 'nxdomain' | 'zero'
/**
 * Which kind of traffic this forward carries. tcp covers web, SSH and most services. udp covers games, voice and VPNs. both carries each, as two rules sharing one counter. Empty means tcp.
 */
export type Protocol = '' | 'tcp' | 'udp' | 'both'
/**
 * A port, such as 8080, or an inclusive range, such as 30000-30010. A range is forwarded to the identical range inside; only a single port may be remapped to a different one.
 */
export type PortOrPortRange = string
export type AddressAndPort2 = string
/**
 * How this exit delivers traffic. interface sends it out a device — a WireGuard or Tailscale interface, a PPPoE session, a proxy's TUN. next_hop hands it to another box on the network, such as the modem or a machine running a proxy. blocked refuses it, so applications fail immediately and visibly rather than hanging.
 */
export type ExitForm = 'interface' | 'next_hop' | 'blocked'
export type IPAddress7 = string
/**
 * What happens to IPv6 traffic from sources assigned to this exit. via carries it through the exit, which needs the exit to actually have IPv6. block refuses it, so clients fall back to IPv4 immediately. direct lets it take the box's normal path, which leaks every site with an AAAA record around the exit. Empty means block.
 */
export type IPv6Handling = '' | 'via' | 'block' | 'direct'
/**
 * What happens to assigned traffic when the health check fails. block stops it, so the problem is visible and diagnosable. direct sends it out the box's normal path instead, which silently leaks exactly the traffic that was meant to be routed. Empty means block.
 */
export type BehaviourWhenTheExitIsDown = '' | 'block' | 'direct'
export type AddressAndPort3 = string
/**
 * A duration with a unit, such as 30s, 5s or 1m30s. Units are ns, us, ms, s, m and h.
 */
export type Duration2 = string
/**
 * Who hosts the DNS for your domain. olr writes a temporary record through their API to prove the domain is yours, which is the only way to get a certificate for a name that does not resolve from the internet. The names your proxy supports are listed by `olr ingress show providers`.
 */
export type DNSProvider1 = string
/**
 * An API credential for the DNS provider. Scope it to this one zone if the provider allows it: it is stored on the router and can change your DNS. It is never shown again after it is set.
 */
export type ProviderAPIToken = string
/**
 * Optional. Used by the certificate authority to warn you before a certificate expires.
 */
export type ContactAddress = string
/**
 * Public DNS servers used only to confirm the challenge record has published. This must not be this router: olr answers your local domain authoritatively, so asking it would return "no such record" forever. Leave empty for sensible defaults.
 */
export type PropagationCheckResolvers = string[]
/**
 * How olr speaks to the service being published. http is almost always right: the connection runs over your own LAN to a device you named, and the HTTPS a browser sees is terminated here. Use https only when the service refuses plain HTTP — many NAS and hypervisor UIs do — in which case its own certificate is not checked, because those are self-signed and demanding a valid one would make the case this option exists for impossible. Empty means http.
 */
export type UpstreamScheme = '' | 'http' | 'https'
export type IPPrefix2 = string
export type IPAddress8 = string
export type IPPrefix3 = string
export type IPAddress9 = string
export type IPAddress10 = string
/**
 * `home` sends only your home networks, so the device reaches the things on them while everything else keeps going out of whatever network the device is actually on. `everything` sends all of the device's traffic here, so it appears to be at home for every purpose including its public address — at the cost of every byte crossing your home upload twice, and of needing address translation olr does not yet write. Empty means home.
 */
export type WhatTheDeviceSendsThroughTheTunnel = '' | 'home' | 'everything'
/**
 * The whole box's configuration, one property per module — the shape of /etc/open-linux-router/olr.json.
 */
export interface OlrDocument {
  clients?: ClientsConfig
  dhcp?: DhcpConfig
  dial?: DialConfig
  dns?: DnsConfig
  firewall?: FirewallConfig
  gateway?: GatewayConfig
  ingress?: IngressConfig
  link?: LinkConfig
  remote?: RemoteConfig
  system?: SystemConfig
}
export interface ClientsConfig {
  clients?: Client[]
  /**
   * The operator's sets of devices. Nothing is seeded and nothing is assigned automatically: a group exists because somebody made it.
   */
  groups?: ClientsGroup[]
}
export interface Client {
  mac: string
  name?: string
  category?: ClientCategory
  model?: string
  icon?: ClientPicture
  notes?: string
  /**
   * The one group this device is in, or absent for none. Must name an existing group.
   */
  group?: string
}
/**
 * One set of devices. Renaming carries every member and subgroup along.
 */
export interface ClientsGroup {
  name: string
  /**
   * The group this one sits inside, or absent for a group at the top. At most four levels deep, and never inside itself.
   */
  parent?: string
}
export interface DhcpConfig {
  enabled: boolean
  pools?: Pool[]
  reservations?: Reservation[]
  extra_dnsmasq_conf?: string
}
export interface Pool {
  network: string
  ipv4?: PoolIPv4
  ipv6?: PoolIPv6
  lease_time?: Duration
  gateway?: IPAddress2
  dns?: IPAddress3[]
  domain?: string
  ntp?: IPAddress4[]
  options?: Option[]
}
export interface PoolIPv4 {
  start?: IPAddress
  end?: IPAddress1
}
export interface PoolIPv6 {
  mode?: RouterAdvertisementMode
  mtu?: number
}
export interface Option {
  option: string
  value: string
}
export interface Reservation {
  mac: string
  ip: IPAddress5
  hostname?: string
  lease_time?: Duration
}
export interface DialConfig {
  tunnelbroker?: TunnelBroker
  records?: Record[]
}
export interface TunnelBroker {
  enabled: boolean
  tunnel_id?: string
  server_ipv4: string
  server_ipv6?: string
  client_ipv4?: string
  client_ipv6: string
  routed_64?: string
  routed_48?: string
  mtu?: number
}
export interface Record {
  name: string
  zone?: string
  provider: DNSProvider
  provider_key_id?: string
  provider_token?: string
  callback_url?: string
  source: WhereTheAddressComesFrom
  interface?: string
  reflector_url?: string
  interval?: Duration1
  ttl?: number
}
export interface DnsConfig {
  filter_aaaa?: boolean
  enabled: boolean
  listen?: AddressAndPort[]
  upstream: Upstream
  local_domain?: string
  /**
   * Zones whose public records are allowed to point at private addresses — in practice, the domain you publish your own services under in your provider's DNS.
   *
   * unbound rejects such answers by default, and that is anti-rebinding working as intended (render.go, rebindPrefixes): a name on the internet resolving to an address on your LAN is how a web page reaches a device it should not. The rule cannot tell your zone from an attacker's, so this is where you say which zones are yours. Without it, the record you just created in your DNS provider works for anyone using a public resolver and silently does not exist for anyone using this one.
   *
   * Only this resolver is affected. A device configured with a public resolver never asks us and never needs this.
   */
  private_domains?: string[]
  hosts?: Host[]
  policies?: Policy[]
  hijack: Hijack
  query_log: QueryLog
  extra_unbound_conf?: string
}
export interface Upstream {
  mode?: UpstreamMode
  servers?: AddressAndPort1[]
  tls?: boolean
  tls_name?: string
}
export interface Host {
  name: string
  addresses: IPAddress6[]
}
export interface Policy {
  name: string
  clients?: IPPrefix1[]
  block?: string[]
  allow?: string[]
  response?: BlockedNameResponse
}
export interface Hijack {
  enabled: boolean
  interfaces?: string[]
  block_dot?: boolean
}
export interface QueryLog {
  enabled: boolean
  entries?: number
}
export interface Forward {
  name: string
  in: string
  protocol?: Protocol
  port: PortOrPortRange
  to: AddressAndPort2
  hairpin?: boolean
  slot: number
}
export interface GatewayConfig {
  latency_target?: string
  enabled: boolean
  exits?: Exit[]
  forwards?: Forward[]
  snat?: boolean
  default?: string
  stats?: boolean
  ipv6_forwarding?: boolean
  interfaces?: Assignment[]
  clients?: GatewayClientAssignment[]
}
export interface GatewayClientAssignment {
  mac: string
  exit?: string
}
export interface Exit {
  name: string
  via: Via
  slot: number
  ipv6?: IPv6Handling
  on_failure?: BehaviourWhenTheExitIsDown
  snat?: boolean
  probe?: Probe
  dns?: ExitDNS
}
export interface ExitDNS {
  mode: 'olr' | 'exit' | 'custom'
  server?: string
}
export interface Via {
  kind: ExitForm
  interface?: string
  next_hop?: IPAddress7
  dev?: string
}
export interface Probe {
  target: AddressAndPort3
  interval?: Duration2
  timeout?: Duration2
  failures?: number
  successes?: number
}
export interface Assignment {
  interface: string
  exit?: string
}
export interface FirewallConfig {
  /** Native nftables JSON for inet olr_filter; replaces automatic rules. */
  ruleset?: { [k: string]: unknown }
  /**
   * Block connections from outside that nothing here asked for.
   */
  enabled: boolean
}
export interface IngressConfig {
  enabled: boolean
  /**
   * The suffix published names live under, e.g. `home.example.com`.
   *
   * Owned here rather than by `dns`, and deliberately not the same value: the wildcard certificate is for `*.<this>`, the records the operator writes point at this box, and neither has anything to do with what the resolver calls the clients in the house.
   *
   * One label, though: the wildcard covers `grafana.<domain>` and not `grafana.lab.<domain>`, so a deeper suffix would only move the problem.
   */
  domain?: string
  certificate: Certificate
  services?: Service[]
  raw_caddyfile?: string
}
/**
 * How the one wildcard certificate that serves every published name is obtained.
 */
export interface Certificate {
  provider?: DNSProvider1
  provider_token?: ProviderAPIToken
  acme_email?: ContactAddress
  resolvers?: PropagationCheckResolvers
}
export interface Service {
  name: string
  upstream: UpstreamIngress
  link_path?: string
  icon?: string
}
export interface UpstreamIngress {
  client?: string
  host?: string
  port: number
  scheme?: UpstreamScheme
  request_headers?: RequestHeaderIngress[]
}
export interface RequestHeaderIngress {
  name: string
  value?: string
  remove?: boolean
}
export interface LinkConfig {
  ipv6_auto?: { [interfaceName: string]: boolean }
  adopted?: string[]
  networks?: Network[]
}
export interface Network {
  name: string
  members: string[]
  ipv4?: NetworkIPv4
  ipv6?: NetworkIPv6
}
export interface NetworkIPv4 {
  subnet: IPPrefix2
  router?: IPAddress8
}
export interface NetworkIPv6 {
  subnet?: string
  /** The Nth /64 of the uplink's delegated prefix, instead of a static subnet. */
  delegated?: number
  router?: string
}
export interface RemoteConfig {
  endpoint?: string
  wireguard: WireGuard
}
export interface WireGuard {
  enabled: boolean
  interface?: string
  listen_port?: number
  public_port?: number
  subnet?: IPPrefix3
  address?: IPAddress9
  /**
   * Generated by olr and never shown. It is redacted on every surface; sending the mask back means 'unchanged'.
   */
  private_key?: string
  peers?: Peer[]
  raw_wireguard_conf?: string
}
export interface Peer {
  name: string
  public_key: string
  address?: IPAddress10
  routes?: WhatTheDeviceSendsThroughTheTunnel
  /**
   * What kind of device this is. It selects the picture shown in the device list, and an operator-set value always beats a detected one. Empty means nothing has been set, so detection may answer; "unknown" means the device was looked at and could not be placed.
   */
  category?: ClientCategory
  /**
   * A picture chosen for this device, overriding the one its category and vendor would pick: <vendor>/<category> such as "apple/laptop", optionally followed by /<variant> such as "apple/desktop/mini", <category>/<variant> such as "server/tower", os/<system> such as "os/debian", or thesvg:<slug> such as "thesvg:proxmox". Empty means the picture follows category and vendor.
   */
  icon?: ClientPicture
}
export interface SystemConfig {
  access?: Access
}
export interface Access {
  /**
   * When this box was claimed.
   */
  claimed_at: string
  password: Password
}
/**
 * The password required over the network
 */
export interface Password {
  /**
   * Derived form of the password. Never the password.
   */
  hash: string
}
