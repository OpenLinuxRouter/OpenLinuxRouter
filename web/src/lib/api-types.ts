// Response shapes for the read and apply surfaces.
//
// KNOWN GAP, worth fixing rather than living with: these are hand-written,
// while config-types.ts is generated. design.md §3.2 rule 3 makes the Go struct
// the single source for every surface, and §6.2 says the read surface includes
// observed resources "declared alongside config" — but core reflects only the
// module's *config* struct today, so the view types in internal/dhcp/view.go
// reach TypeScript by being retyped here. A field renamed in Go breaks this
// file silently at runtime rather than loudly at compile time.
//
// The fix is to have core publish response schemas next to the config schema
// and extend scripts/gen-types.mjs to consume them. Until then, this file and
// internal/dhcp/view.go, internal/clients/view.go and internal/dns/view.go must
// be changed together — three copies now, not two.
//
// What closing it actually costs, recorded here so it is not re-derived:
//   - The view structs are unexported in all three modules (planView, queryView,
//     …), so core needs them exported, or a registration point beside the config
//     value Mount already takes.
//   - Impact is an int with MarshalText, so reflection publishes a bare `string`
//     and the unions below would *regress* unless Impact, ChangeKind and
//     ServiceAction each grow a JSONSchema() method — the move
//     internal/dns/schema.go already makes for UpstreamMode.
//   - `Plan` collides across modules in one flat generated file, so the
//     generator needs a per-module namespace it does not have today.

import type { Problem } from '@/lib/api'
import type {
  ClientCategory,
  FirewallConfig,
  IngressConfig,
  UpstreamScheme,
  ClientsConfig,
  ExitForm,
  LinkConfig,
  Protocol,
  GatewayConfig,
  RemoteConfig,
} from '@/lib/config-types'

/** What applying a change will cost (design.md §5.3.3, internal/dhcp Impact). */
export type Impact = 'none' | 'reload' | 'restart' | 'disruptive'

/**
 * Something about the box, rather than the configuration, standing between a
 * module and its job — internal/core Blocker.
 *
 * Distinct from {@link Problem}, which reports a field that failed validation.
 * Nothing the operator can type into olr clears a blocker, so the shape carries
 * the command that does. `fix` is shell, verbatim and possibly multi-line:
 * render it as a block and never reflow it.
 *
 * Shared rather than per-module because one incumbent can block two modules —
 * dnsmasq.service takes both :53 and UDP/67 — and the operator must not have to
 * work out whether two pages are describing one problem or two.
 */
export interface Blocker {
  kind: string
  unit?: string
  summary: string
  detail?: string
  fix?: string
  /**
   * What olr will do about this on request, absent where it will not.
   *
   * A blocker without one renders exactly as every blocker did before this
   * existed — text and a command to paste — which is what an unrecognised
   * distribution, or an incumbent that is none of olr's business, still gets.
   */
  action?: BlockerAction
}

/**
 * olr clearing a blocker itself — internal/core Action.
 *
 * Deliberately opaque. `id` is a handle to send back and nothing else: the
 * client cannot name a package or a unit for olr to act on, because the code
 * that does the acting is looked up in the daemon by this id. `runs` is what
 * will happen, listed before it happens, and it must be shown — a tool reaching
 * outside its own scope has to say what it is about to do.
 */
export interface BlockerAction {
  id: string
  /** The button. Says what will happen, not "fix". */
  label: string
  /** Every command, in order, in the words the operator would have used. */
  runs: string[]
}

/** One unit of work in a fix and how it went, mirroring core.Step. */
export interface FixStep {
  description: string
  done: boolean
  error?: string
}

/** What POST /blockers/fix answers with, successful or not (§5.3.2). */
export interface FixResult {
  steps?: FixStep[]
  error?: { message: string }
}

/** What the backend needs after the files are written. */
export type ServiceAction = 'none' | 'start' | 'stop' | 'reload' | 'restart'

export type ChangeKind = 'create' | 'update' | 'delete'

export interface Change {
  path: string
  kind: ChangeKind
  impact: Impact
  /**
   * Which backend reads this file. Only modules driving more than one daemon
   * set it — dns writes unbound.conf and the relay's config, and signalling the
   * wrong one is the bug this field exists to prevent.
   */
  unit?: string
  /**
   * Whether this file's contents are withheld. Set by ingress alone, on the file
   * holding the DNS provider credential: the change is reported and compared
   * byte for byte, and only its *display* is suppressed. A diff viewer must not
   * decide on its own what is safe to render — `diff` already contains a
   * placeholder rather than the bytes, so nothing here has to remember.
   */
  secret?: boolean
  diff: string
}

/**
 * What every module's plan carries.
 *
 * Split out because the tail is not shared: a module with one daemon reports a
 * single `action`, and one with two reports a list. Flattening them into a
 * single `Plan` is how a dns page ends up reading `plan.action`, getting
 * `undefined`, and reporting a bare "Applied" for a change that restarted the
 * resolver.
 */
export interface PlanCore {
  backend: string
  changes: Change[]
  impact: Impact
  reasons?: string[]
  empty: boolean
  warnings?: Problem[]
}

/**
 * The answer to "what would applying this do?" for a module with one backend —
 * internal/dhcp planView, and internal/devices, whose view.go says it mirrors
 * dhcp's shape deliberately so one renderer serves both.
 */
export interface Plan extends PlanCore {
  action: ServiceAction
  /**
   * The boot-time state the unit will be moved to, absent when it already
   * matches. Separate from `action` because "running now" and "running after a
   * reboot" are different promises: a unit that is active but not enabled looks
   * healthy until the power goes out.
   */
  enable?: boolean
}

/** One unit's worth of pending work — internal/dns ServicePlan. */
export interface ServicePlan {
  unit: string
  action: ServiceAction
  /** As {@link Plan.enable}, but per unit. */
  enable?: boolean
}

/**
 * internal/dns planView.
 *
 * `services` rather than `action` because this module drives two daemons:
 * unbound resolves and olr-dnsd owns :53. They fail differently and are
 * signalled independently — editing a blocklist reloads the relay and must not
 * touch the resolver at all — so a single action could not say what happened.
 */
export interface DnsPlan extends PlanCore {
  services: ServicePlan[]

  /**
   * What the module filled in for a caller who did not say — today, the address
   * DNS will answer on when it is switched on for the first time (internal/dns
   * derive.go). One line each, already written for a human.
   *
   * A change olr made on the operator's behalf has to be visible in the same
   * breath as the change they asked for (design.md §5.6), which for a switch
   * means the toast that confirms it.
   */
  derived?: string[]
}

/** One unit of work and how it went. */
export interface Step {
  description: string
  done: boolean
  error?: string
  /**
   * The step could not be attempted rather than having failed — ingress checking
   * a rendered config with no proxy binary to check it with. `done` stays true
   * because nothing is outstanding, so a reader that only looks at `done` is not
   * misled about the outcome, only about whether a check happened.
   */
  skipped?: boolean
}

/**
 * The result of an apply. Carries the steps whether it succeeded or not:
 * design.md §5.3.2 has no rollback, so a half-finished change stays
 * half-finished and the honest thing is to show which steps landed.
 */
export interface ApplyResult {
  plan: Plan
  steps?: Step[]
  error?: { message: string; problems?: Problem[] }
}

/** What systemd knows about a unit — core.UnitStatus. */
export interface UnitStatus {
  unit: string
  active: boolean
  /** Whether it starts at boot — not the same question as `active`. */
  enabled: boolean
  /**
   * Whether the unit file exists at all. Distinguished from `enabled` because
   * the two need different answers: disabled is something olr fixes on the next
   * apply, missing means the package is incomplete and applying will not help.
   */
  installed: boolean
  state: string
  sub_state?: string
  since?: string
  main_pid?: number
}

export interface DhcpStatus {
  enabled: boolean
  service?: UnitStatus
  service_error?: string
  drifted: boolean
  drift?: Plan
  drift_error?: string
  /** Read on every status request, including while the module is off. */
  blockers?: Blocker[]
  as_of: string
}

export interface Lease {
  ip: string
  mac?: string
  iaid?: string
  hostname?: string
  client_id?: string
  /** null for a lease that never expires. */
  expires: string | null
  active: boolean
}

export interface PoolUsage {
  /** The network this describes — internal/dhcp Usage.Network. */
  network: string
  size: number
  active: number
  expired: number
  free: number
  percent: number
}

export interface DhcpLeases {
  leases: Lease[]
  usage: PoolUsage[]
  problems?: Problem[]
  as_of: string
}

// --- link ------------------------------------------------------------------

/**
 * One row of the interface list (internal/link interfaceView).
 *
 * The observed half — addresses, state — is read from the kernel on every
 * request and never stored, so this is what is true now rather than what was
 * last written (design.md §4.5).
 */
export interface InterfaceRow {
  name: string

  /** The operator has handed this interface to olr. */
  adopted: boolean

  /**
   * The kernel currently has it. False with `adopted` true is the typo case —
   * a name that was stored and has nothing behind it — and the UI has to say
   * so rather than showing a row that looks like any other.
   */
  present: boolean

  /**
   * `up` is administrative state, `running` is carrier. Both, because "up with
   * no cable in it" is the most common reason a freshly configured DHCP server
   * appears to do nothing, and one boolean cannot say it.
   */
  up: boolean
  running: boolean

  loopback: boolean
  /** Backed by a device in sysfs; virtual links remain available in advanced settings. */
  physical: boolean
  mac?: string

  /** Kernel identity and optional live NIC measurements, not gateway counters. */
  index?: number
  /** Negotiated link speed; unavailable on some virtual interfaces/drivers. */
  speed_mbps?: number
  rx_bytes?: number
  tx_bytes?: number

  /** Every address on the interface, link-local excluded. */
  prefixes?: string[]

  /**
   * The first IPv4 prefix split into the two halves an operator reads
   * separately. Absent when the interface has no IPv4 address — which is
   * exactly when no address range can be served on it.
   */
  address?: string
  subnet?: string

  /**
   * The network this interface carries, absent if it carries none. The reverse
   * of a network's member list, and what lets a row say what the NIC is *for*.
   */
  network?: string
}

/**
 * One network as the API publishes it — internal/link networkView.
 *
 * `subnet`/`router` are intent; `InterfaceRow.subnet` is observation. The two
 * disagreeing is drift, which is why both are published rather than one being
 * derived from the other.
 */
export interface NetworkRow {
  name: string
  members: string[]
  subnet?: string
  router?: string

  /** The operator pinned the router address, rather than it being derived. */
  router_explicit?: boolean

  /** The static IPv6 /64 and this router's address in it; absent without one. */
  subnet6?: string
  router6?: string
  router6_explicit?: boolean
  /** The /64 number taken out of the delegated prefix; with no subnet6, not delegated yet. */
  delegated?: number

  /**
   * The range dhcp derives when nobody types one. A hint for prefilling, never
   * a second opinion: dhcp validates whatever range it is given regardless.
   */
  suggested_start?: string
  suggested_end?: string

  /** Every member exists on this machine. */
  present: boolean
}

export interface InterfaceList {
  interfaces: InterfaceRow[]
  networks: NetworkRow[]
  problems?: Problem[]
  as_of: string
}

/**
 * The result of storing adoption and networks.
 *
 * `steps` exists now. Adoption is still one atomic document write with nothing
 * half-finished to report, but a network change continues into the kernel and
 * that part can land halfway — one address added and the next refused. There
 * is no rollback (§5.2), which only works if what did happen is reported.
 */
export interface LinkApplyResult {
  plan: Plan
  config: LinkConfig
  steps?: LinkStep[]
  error?: { message: string; problems?: Problem[] }
}

export interface LinkStep {
  description: string
  done: boolean
  error?: string
}

// --- dial: the uplink --------------------------------------------------------

/**
 * How this router itself reaches the internet — the body of `PUT
 * /api/dial/uplink`, mirroring `internal/dial.Uplink`.
 *
 * This one is a *config* shape and so does not belong in this file: it should
 * arrive as `DialConfig.uplink` from config-types.ts, which is generated from
 * the Go struct. It is here because the generator needs a running olrd to talk
 * to (`make types`), and that could not be run in the environment this landed
 * from. Regenerating is the fix, and it deletes this interface rather than
 * changing it — `Uplink` is the name the generator will produce.
 */
export interface Uplink {
  interface: string
  ipv4?: UplinkIPv4
  /** Prefix delegation or a 6in4 tunnel; absent leaves IPv6 on the uplink alone. */
  ipv6?: UplinkIPv6
  /**
   * The resolvers this router itself looks names up through, written for the
   * box whenever the uplink is static. Only recorded when it is not.
   */
  dns?: string[]
}

export interface UplinkIPv4 {
  /**
   * This box's own address *with the mask of its link* — 192.168.2.9/24. Not
   * masked, which is the opposite of a network's subnet: the host bits are the
   * address, and the server refuses 192.168.2.0/24 rather than silently
   * accepting the network itself.
   */
  address: string
  /** The modem's address on that link, and the next hop of the default route. */
  gateway: string
}

export interface UplinkIPv6 {
  via: 'dhcpv6-pd' | '6in4'
  /** dhcpv6-pd: the prefix size to ask for; absent takes what the ISP gives. */
  prefix_length?: number
  /** 6in4: the broker's IPv4 endpoint — "Server IPv4 Address" on its details page. */
  server?: string
  /** 6in4: this box's address inside the tunnel, with its mask — usually ending ::2. */
  address?: string
  /** 6in4: absent means 1480. */
  mtu?: number
}

/** The prefix-delegation loop — internal/dial DelegationState. */
export interface DelegationStatus {
  interface: string
  phase: 'soliciting' | 'bound' | 'renewing' | 'rebinding' | 'waiting'
  prefix?: string
  obtained?: string
  renews?: string
  expires?: string
  dns?: string[]
  /** Why the last attempt failed, in the ISP's or the kernel's words. */
  error?: string
}

/** The uplink's tunnel as the status publishes it — internal/dial tunnelView. */
export interface TunnelStatus {
  interface: string
  server: string
  address: string
  mtu: number
  present: boolean
  up: boolean
  /** The IPv4 address the device actually leaves from; absent for "whatever the route picks". */
  local?: string
  /** Where the IPv6 default route leaves by. Anything but the tunnel means IPv6 is not using it. */
  route_dev?: string
}

/**
 * The uplink as `GET /api/dial/uplink` and `GET /api/dial/status` publish it —
 * internal/dial uplinkView.
 *
 * Intent and fact side by side, never collapsed into a verdict. `address` and
 * `gateway` are what olr was told; `addresses` and `route_via`/`route_dev` are
 * what the kernel has right now. The failure worth catching is the one where
 * the first pair looks perfect and the default route leaves by a different
 * interface, and a single "ok" boolean is exactly what hides it.
 */
export interface UplinkStatus {
  interface: string
  address?: string
  gateway?: string
  dns?: string[]

  present: boolean
  up: boolean

  /**
   * Every IPv4 address actually on the interface. More than one means
   * something else is addressing it too — usually the distribution's DHCP
   * client. olr reports that and removes nothing.
   */
  addresses?: string[]

  /**
   * The default route actually in the main table, whichever interface it
   * leaves by. Absent means this box has no way out at all, which is a
   * different sentence from "the route goes somewhere else".
   */
  route_via?: string
  route_dev?: string

  /**
   * Whether route_via answers on route_dev, from the kernel's neighbour table.
   * Absent when nothing has tried to reach it yet. A route that matches what
   * was set and a route that works are different facts, and only this says
   * the second.
   */
  gateway_state?: 'answers' | 'silent'
  /** Another interface route_via does answer on — usually the one that faces the modem. */
  gateway_seen_on?: string

  /**
   * The name servers this box really looks names up through, whoever set them
   * — beside `dns`, which is what olr was told.
   */
  resolving_through?: string[]

  tunnel?: TunnelStatus

  /** Set when the uplink asks for prefix delegation. */
  prefix_length?: number
  delegation?: DelegationStatus
  /** The uplink's accept_ra as the kernel has it; prefix delegation needs 2. */
  accept_ra?: string

  problems?: Problem[]
}

export interface UplinkResponse {
  /** Absent when olr does not own the way out, which is most boxes. */
  uplink?: UplinkStatus
  as_of: string
}

/** The result of storing the uplink — internal/dial applyResponse. */
export interface DialApplyResult {
  plan: Plan
  steps?: Step[]
  error?: { message: string; problems?: Problem[] }
}

// --- dial: dynamic DNS -------------------------------------------------------

/**
 * One public name as `GET /api/dial/status` reports it — internal/dial
 * recordView.
 *
 * Flat, and deliberately: docs/ddns.md §6 asks three separate questions — was
 * the address read, what was it, did the provider take it — and the failure
 * worth seeing is the one where the first two are fine and the third has been
 * refusing for days. Every timestamp is absent until the thing has happened,
 * never a zero time.
 */
export interface RecordStatus {
  name: string
  provider: string
  source: 'interface' | 'reflector'
  /** The interface or endpoint the address is read from. */
  from?: string

  checked?: string
  check_error?: string

  address?: string
  /** The address read is in 100.64.0.0/10: the ISP's NAT, not this router. */
  cgnat?: boolean

  published?: string
  published_address?: string
  publish_error?: string

  failures?: number
  retry?: string

  problems?: Problem[]
  /**
   * The publisher has a loop for this record. False with no problems is the
   * moment right after a write, before olrd has picked the config up.
   */
  watched: boolean
}

/**
 * The way out as the kernel has it, on any box — internal/dial routeView.
 *
 * Beside `uplink` rather than inside it: `uplink` is absent when olr does not
 * own the way out, which is most boxes, and this is what the overview draws
 * above the router on every one of them.
 */
export interface RouteStatus {
  /** The interface the default route leaves by. */
  dev: string
  /** Its next hop; absent on a point-to-point uplink such as PPPoE. */
  via?: string
  /** dev's IPv4 addresses, as prefixes. */
  addresses?: string[]
  gateway_state?: 'answers' | 'silent'
}

export interface DialStatus {
  /** Observed host resolvers, including when OLR does not manage the uplink. */
  resolvers?: string[]
  uplink?: UplinkStatus
  /** Absent only when the box has no default route at all. */
  route?: RouteStatus
  /** Observed main-table IPv4 and IPv6 default routes, including multipath legs. */
  default_routes: { family: 4 | 6; dev: string; via?: string; metric: number; gateway_state?: 'answers' | 'silent'; gateway_mac?: string }[]
  records: RecordStatus[]
  as_of: string
}

// --- devices ---------------------------------------------------------------

/**
 * Where a resolved value came from — internal/devices Origin.
 *
 * The distinction is the point of the screen: what an operator was told and
 * what we inferred must never look the same, or a guess reads as a fact.
 * `remembered` is history rather than a current observation: a name the device
 * announced the last time it was here, kept because no source can be asked now.
 */
export type Origin = 'operator' | 'detected' | 'observed' | 'remembered' | ''

/** Which source saw a device — internal/devices Source. */
export type PresenceSource = 'dhcp-lease' | 'arp'

/**
 * A vendor we might have artwork for — internal/devices VendorKey.
 *
 * Hand-kept like the rest of this file, because it rides on the response and
 * only *config* is reflected into config-types.ts. Staleness is survivable in
 * the one direction it can happen: a key added in Go and missing here means
 * that vendor never gets a picture, which is the same benign gap a category
 * without artwork already has. What the union does catch is a key mistyped
 * *here*, and that matters because these strings are filenames.
 *
 * Deliberately not every vendor. The registry knows thirty thousand; this is
 * the few dozen whose hardware looks like something in particular.
 */
export type VendorKey =
  | 'acer'
  | 'amazon'
  | 'apple'
  | 'asus'
  | 'brother'
  | 'canon'
  | 'cisco'
  | 'dell'
  | 'epson'
  | 'espressif'
  | 'google'
  | 'hp'
  | 'hpe'
  | 'huawei'
  | 'intel'
  | 'lenovo'
  | 'lg'
  | 'microsoft'
  | 'mikrotik'
  | 'netgear'
  | 'nintendo'
  | 'philips-hue'
  | 'qnap'
  | 'raspberry-pi'
  | 'roku'
  | 'samsung'
  | 'sonos'
  | 'sony'
  | 'synology'
  | 'tp-link'
  | 'ubiquiti'
  | 'xiaomi'
  | 'zyxel'

/**
 * One row of the device list: identity joined to presence
 * (design.md §4.4, internal/devices deviceView).
 */
export interface ClientRow {
  mac: string

  /**
   * Already resolved: stored name, else observed hostname, else the name the
   * device last announced while it was here, else empty.
   */
  name: string
  name_origin?: Origin

  /**
   * When a remembered name was heard, present only for name_origin
   * `remembered`. A surface that shows the name without it would be reporting
   * last week's claim as this minute's.
   */
  name_as_of?: string

  category: ClientCategory
  category_origin?: Origin

  /** What detection produced, whether or not it won. */
  detected_category?: ClientCategory
  detect_reason?: string

  /**
   * Who built the hardware, read from the MAC's registry prefix. Absent for a
   * randomised address, which has no vendor to find and is now most phones and
   * laptops.
   *
   * `vendor` is the label; `vendor_key` selects the picture and is absent for
   * the great majority of vendors. Two fields rather than one because a label
   * is allowed to be improved and a filename is not.
   */
  vendor?: string
  vendor_key?: VendorKey

  model?: string

  /** The operator's chosen picture — see Client.icon. Absent means it follows category and vendor. */
  icon?: string

  notes?: string

  /**
   * The operator's group for this device, absent for none. Never inferred: a
   * device is in a group only because somebody put it there, so a missing
   * field is "ungrouped", not "not yet worked out".
   */
  group?: string

  /** Whether a human has described this device, as opposed to it merely being seen. */
  stored: boolean

  /** Any source considers it current. */
  online: boolean

  /**
   * Any source has ever seen it. Distinct from `online`: a stored device that
   * has never been seen is a typo'd MAC or a machine that has not been plugged
   * in, and neither is the same as "away".
   */
  seen: boolean

  ips?: string[]
  hostname?: string
  sources?: PresenceSource[]

  /** null for no lease, and for a lease that never expires. */
  expires: string | null

  /** The reserved address. Owned by dhcp, joined here so no client repeats it. */
  fixed_ip?: string

  /**
   * Which of this router's networks the device is on, and how we know.
   *
   * `observed` is the neighbour table reporting the interface the device
   * answered on; `detected` is it having been placed by which pool range its
   * address falls in. Absent means neither could answer — a stored device
   * nothing has seen, or one whose address sits outside every range — and the
   * map has to say so rather than file it under a plausible network.
   */
  network?: string
  network_origin?: Origin

  /**
   * When the router last heard from it: the neighbour table confirming it
   * answers, which any traffic through the router keeps doing. Absent when it
   * has not been heard since olrd started keeping the record. Moves only on
   * evidence, so it can say "an hour ago" while `online` still says yes off a
   * stale entry.
   */
  last_seen?: string
}

export interface ClientList {
  clients: ClientRow[]
  problems?: Problem[]
  as_of: string
}

/**
 * What the network equipment says about where one client is attached.
 *
 * A separate request from /list on purpose: answering costs a round trip to
 * every switch that can report a forwarding table, and a list of forty clients
 * would pay that forty times on every repaint. Empty `locations` with no
 * `problems` means every device was asked and none of them has the client.
 */
export interface ClientLocation {
  mac: string
  locations: ClientLocationEntry[]

  /** The devices that could not answer: no forwarding table, or no reply. */
  problems?: Problem[]
  as_of: string
}

export interface ClientLocationEntry {
  device_id: string
  device?: string

  /** The device's own port identifier, verbatim: "5" or "lan2". */
  port: string
  vlan?: number
  observed_at: string
}

/**
 * The result of storing identity.
 *
 * No `steps`, unlike dhcp's ApplyResult: this module writes one document
 * atomically, so there is no half-finished state to report.
 */
export interface ClientsApplyResult {
  plan: Plan
  config: ClientsConfig
  error?: { message: string; problems?: Problem[] }
}

// --- dns -------------------------------------------------------------------
//
// Mirrors internal/dns/view.go. Everything below `DnsStatus.stats` is observed:
// never stored, never cached, read through the relay's socket on every request
// and stamped with `as_of` (design.md §4.5).

/** As {@link ApplyResult}, for the module whose plan carries two units. */
export interface DnsApplyResult {
  plan: DnsPlan
  steps?: Step[]
  error?: { message: string; problems?: Problem[] }
}

/**
 * One backend's liveness — internal/dns serviceView.
 *
 * `status` is absent with `error` set when the query itself failed, which is
 * normal on a box with no system bus and must not read as "stopped".
 */
export interface DnsService {
  unit: string
  status?: UnitStatus
  error?: string
}

export interface DnsStatus {
  enabled: boolean
  /**
   * Both backends, always resolver first then relay. Reported separately on
   * purpose: "DNS is broken" has two very different causes and only one of them
   * is ours.
   */
  services: DnsService[]
  drifted: boolean
  drift?: DnsPlan
  drift_error?: string
  /**
   * Read on every status request, including while the module is off — off is
   * exactly when it is worth knowing, because the alternative is learning it
   * from a failed apply after the switch has been flipped.
   */
  blockers?: Blocker[]
  /** Absent with `stats_error` set when the relay is not answering — which is
   *  itself the most useful thing the reply can say. */
  stats?: DnsStats
  stats_error?: string
  as_of: string
}

/** One address the relay has recently answered — internal/dns clientView. */
export interface DnsClient {
  address: string
  queries: number
  last_seen: string
}

/**
 * The relay's account of itself, gaps included — internal/dns statsView.
 *
 * `dropped` and `unparsed` are published rather than kept internal, and that is
 * the point: a query log that silently shed entries under load would be worse
 * than none, because it would look complete.
 */
export interface DnsStats {
  /** When the relay started. The log does not survive a restart. */
  since: string
  queries: number
  blocked: number
  failed: number
  /** Observations lost because the tee was full. */
  dropped: number
  /** Responses the observer could not read. */
  unparsed: number
  /** Entries the log currently holds, and the bound it holds them under. */
  held: number
  capacity: number
  clients?: DnsClient[]
}

/** One answered query — internal/dns queryView. */
export interface QueryRow {
  at: string
  client: string
  name: string
  type: string
  rcode: string
  /**
   * `blocked` and `policy` together answer "why can this device not reach that
   * site". A blocked entry without the rule that blocked it sends the operator
   * hunting.
   */
  blocked: boolean
  policy?: string
  upstream?: string
  answers?: string[]
  /** The CNAME chain. Its tail is why a device that asked for one name shows up
   *  talking to a CDN. */
  chain?: string[]
}

export interface DnsQueries {
  queries: QueryRow[]
  stats?: DnsStats
  as_of: string
}

/** One domain→address pairing — internal/dns nameView. */
export interface NameRow {
  client: string
  name: string
  address: string
  chain?: string[]
  /** When an answer last confirmed the pairing; also the map's eviction order. */
  last_seen: string
}

export interface DnsNames {
  names: NameRow[]
  stats?: DnsStats
  as_of: string
}

// --- gateway ---------------------------------------------------------------

/**
 * A change to the kernel's gateway state — internal/gateway changeView.
 *
 * A line rather than a file path and a diff, because this module configures the
 * kernel rather than a backend's config file. The text is the same canonical
 * form `olr gateway show --dry-run` prints and the same one stored in each
 * nftables rule's comment, so what the screen shows, what the CLI shows and
 * what `nft list ruleset` shows are one string.
 */
export interface GatewayChange {
  kind: 'add' | 'remove'
  line: string
}

/**
 * Somebody else's `ip rule` — internal/gateway ForeignRule.
 *
 * Reported rather than hidden (design.md §3.4): a hand-rolled setup that is
 * visible is one an operator can reason about, and a second owner of the
 * gateway table is something they have to go and resolve elsewhere.
 */
export interface ForeignRule {
  priority: number
  family: string
  table: number
  selector: string
  has_default: boolean
}

/** What applying a gateway change would do — internal/gateway planView. */
export interface GatewayPlan {
  changes: GatewayChange[]
  impact: Impact
  foreign?: ForeignRule[]
  reasons?: string[]
  /**
   * Set when the change cannot proceed at all. A string rather than a boolean,
   * because the operator has to find another program's configuration file and
   * "blocked: true" does not say where to look.
   */
  blocked?: string
  empty: boolean
  /**
   * Whether the kernel could be read. Without it a client cannot tell "nothing
   * to do" from "we could not look", which need different words on screen.
   */
  known: boolean
  diff?: string
  warnings?: Problem[]
}

export interface GatewayApplyResult {
  plan: GatewayPlan
  steps?: Step[]
  config: GatewayConfig
  error?: { message: string; problems?: Problem[] }
}

/** One exit and what is true of it right now — internal/gateway exitStatusView. */
export interface ExitStatus {
  name: string
  via: ExitForm

  /**
   * `up` and `probed` together: an exit nobody probes reads as up, and saying
   * so without saying it was never checked would claim knowledge we do not have
   * (design.md §5.6 — faults must not hide inside a default).
   */
  up: boolean
  probed: boolean

  used_by?: string[]

  /** The kernel resources it holds, published so they can be planned around. */
  mark: string
  table: number
  priority: number
}

/**
 * One network's effective exit and where it came from — internal/gateway
 * assignmentStatusView.
 *
 * The source is what makes inheritance usable: an effective value with no
 * visible origin is one nobody can reason about, which is the whole argument
 * for a property rather than an ordered rule list.
 */
export interface AssignmentStatus {
  interface: string
  exit: string
  source: 'default' | 'interface'
  reason?: string
}

export interface GatewayStatus {
  enabled: boolean
  known: boolean
  exits: ExitStatus[]
  assignments: AssignmentStatus[]
  drifted: boolean
  foreign?: ForeignRule[]
  problems?: Problem[]
  as_of: string
}

/**
 * One route as the kernel reports it — internal/gateway routeRowView.
 *
 * `default` is the route that matches every destination, carried separately
 * because a destination of 0.0.0.0/0 is a fact about the table rather than
 * about the prefix, and the card counts them.
 */
export interface RouteRow {
  dst: string
  default?: boolean
  via?: string
  dev?: string
  metric?: number
  kind?: string
  proto?: string
}

/**
 * One route table and who it belongs to — internal/gateway routeTableView.
 *
 * Owner separates the box's main table from the tables OLR writes, which is the
 * distinction the whole page exists to keep: OLR does not squat the main table
 * (design.md §3.4), and a view that listed both as "routes" would say it does.
 */
export interface RouteTable {
  family: 4 | 6
  table: number
  name: string
  owner: 'main' | 'exit' | 'other'
  exit?: string
  priority?: number
  routes: RouteRow[]
}

/** GET /api/gateway/routes — internal/gateway routesView. */
export interface GatewayRoutes {
  /** False when the kernel could not be read at all; `error` says why. */
  known: boolean
  error?: string
  tables: RouteTable[]
  as_of: string
}

/** One device's traffic through one way out — internal/gateway usageView. */
export interface Usage {
  address: string

  /**
   * Empty for the residual — traffic no assignment matched. A row rather than
   * an omission: per-exit totals only reconcile against the box total if what
   * matched nothing is visible too.
   */
  exit: string

  /** Traffic still carrying the mark of an exit that has since been removed. */
  unknown?: boolean

  up_bytes: number
  down_bytes: number
  up_packets: number
  down_packets: number
}

/** internal/gateway trafficView. */
export interface GatewayTraffic {
  /** Intent. `counting` is whether the kernel actually has the table. */
  enabled: boolean
  counting: boolean

  usage: Usage[]

  /**
   * How full the accounting is. A full table stops recording devices it has
   * not seen before while going on counting the ones it has, so `held` nearing
   * `capacity` means rows are probably missing — not that nothing is using the
   * network.
   */
  held: number
  capacity: number

  /**
   * What these numbers cannot see, from the server rather than written into
   * this app — so the CLI and any agent reading the endpoint get the same
   * caveats. Every one explains a number being smaller than expected.
   */
  limits?: string[]

  as_of: string
}

// --- firewall --------------------------------------------------------------

/**
 * A change to the kernel's NAT table — internal/gateway/nat changeView.
 *
 * A line rather than a file path and a diff, for the same reason gateway's is:
 * this module configures the kernel rather than a backend's config file. The
 * text is the same canonical form `olr gateway show forwards --dry-run` prints and the
 * same one stored in each nftables rule's comment, so what the screen shows,
 * what the CLI shows and what `nft list table inet olr_nat` shows are one
 * string.
 */
export interface ForwardsChangeLine {
  kind: 'add' | 'remove'
  line: string
}

/**
 * Somebody else's chain on the forward hook — internal/gateway/nat ForeignFilter.
 *
 * Reported rather than hidden (design.md §3.4), and — unlike gateway's
 * ForeignRule — it never blocks the change. In nftables a drop is final, so olr
 * cannot override one; but the foreign chain may also be accepting exactly this
 * traffic in a rule olr cannot evaluate, so refusing would block a legitimate
 * setup on a guess (docs/port-forwarding.md §5.2).
 */
export interface ForeignFilter {
  table: string
  family: string
  chain: string
  policy: string
}

/** What applying a port-forwarding change would do — internal/gateway/nat planView. */
export interface ForwardsPlan {
  changes: ForwardsChangeLine[]
  impact: Impact
  foreign?: ForeignFilter[]
  reasons?: string[]
  empty: boolean
  /**
   * Whether the kernel could be read. Without it a client cannot tell "nothing
   * to do" from "we could not look", which need different words on screen.
   */
  known: boolean
  diff?: string
  warnings?: Problem[]
}

export interface ForwardsApplyResult {
  plan: ForwardsPlan
  steps?: Step[]
  config: GatewayConfig
  error?: { message: string; problems?: Problem[] }
}

/** One forward and what is true of it right now — internal/gateway/nat forwardStatusView. */
export interface ForwardStatus {
  name: string
  in: string
  protocol: Protocol
  port: string
  to: string
  hairpin: boolean

  /**
   * `counted` and the totals together. A forward nobody could count reads as
   * zero, and saying so without saying it was never measured would claim
   * knowledge we do not have (design.md §5.6 — faults must not hide inside a
   * default). The two states also point in opposite directions when a port does
   * not work: "nothing arrived" points outward at the ISP or somebody else's
   * filter, "not counted" points at this box.
   */
  counted: boolean

  /**
   * Since the table was last built, not since boot: any change to this module
   * rebuilds it and resets these (docs/port-forwarding.md §3.6).
   */
  packets: number
  bytes: number
}

export interface ForwardsStatus {
  enabled: boolean
  known: boolean
  forwards: ForwardStatus[]
  drifted: boolean
  foreign?: ForeignFilter[]
  problems?: Problem[]
  as_of: string
}

// --- ingress --------------------------------------------------------------

/**
 * What the ingress plan carries. The same shape as `Plan` — one backend, files
 * on disk, a unit to signal — so the shared renderer in features/plan serves it
 * unchanged.
 */
export type IngressPlan = Plan

/**
 * One published service, joined with where it currently points.
 *
 * `upstream` is not in the config: the config names a device, and where that
 * device is belongs to another module (design.md §4.1). It is read per request,
 * which is why it can be absent with `upstream_error` set while the service
 * itself is perfectly well configured.
 */
export interface IngressServiceView {
  name: string
  /** Rendered by the daemon rather than assembled here, so the client does not
   *  have to fetch the local domain to know what a service's address is. */
  url: string
  client?: string
  host?: string
  port: number
  scheme: UpstreamScheme
  upstream?: string
  upstream_error?: string
}

export interface IngressServices {
  services: IngressServiceView[]
  domain?: string
  as_of: string
}

/** The certificate this module's whole clock-dependent half hangs off. */
export interface IngressCertState {
  found: boolean
  certificate?: {
    path: string
    names: string[]
    issued_at: string
    expires_at: string
  }
  expires_in_days?: number
  /**
   * The certificate should already have been replaced and was not — so renewal
   * has been failing quietly, and there is still time to fix it. This is the
   * field the module exists to be able to set: everything about this failure
   * looks fine until it suddenly does not.
   */
  renewal_overdue: boolean
}

export interface IngressStatus {
  problems?: Problem[]
  enabled: boolean
  /** The suffix published names live under, owned by the dns module. */
  domain?: string
  service?: UnitStatus
  service_error?: string
  /**
   * The proxy olr found, and why it found none. olr ships no proxy, so "is one
   * present" is part of this module's health rather than something to discover
   * when an apply fails.
   */
  binary?: string
  binary_error?: string
  certificate: IngressCertState
  certificate_error?: string
  certificate_warnings?: string[]
  /**
   * The second certificate, for the public suffix. Its own field because it
   * fails for its own reason: the private one keeps renewing while this one
   * never appears, because the provider token is still scoped to one zone.
   */
  public_certificate?: IngressCertState
  public_certificate_warnings?: string[]
  /** The suffix exposed services answer on. Empty means nothing is exposed. */
  public_domain?: string
  /** How many published services answer outside this network. */
  public?: number
  published: number
  drifted: boolean
  drift?: IngressPlan
  drift_error?: string
  as_of: string
}

export interface IngressApplyResult {
  pending?: boolean
  problems?: Problem[]
  plan: IngressPlan
  steps?: Step[]
  error?: { message: string; problems?: Problem[] }
  /** What is stored now, redacted. Present on the refusal path especially: it
   *  says the document did not move. */
  config?: IngressConfig
}

export interface IngressProviders {
  binary?: string
  providers: string[]
}

// --- remote -----------------------------------------------------------------

/**
 * One line of kernel state — internal/remote Change.
 *
 * The same shape gateway's changes have, and for the same reason: what this
 * module configures *is* the kernel, so a plan is lines to add and remove
 * rather than files to write.
 */
export interface RemoteChange {
  kind: 'add' | 'remove'
  line: string
}

/** What applying a remote-access change would do — internal/remote planView. */
export interface RemotePlan {
  changes: RemoteChange[]
  impact: Impact
  reasons?: string[]
  /**
   * Set when the interface name belongs to something that is not olr's. A
   * string rather than a boolean, because the answer is to pick a different
   * name and the message is what says so.
   */
  blocked?: string
  empty: boolean
  /**
   * Whether the kernel could be read. Without it a client cannot tell "nothing
   * to do" from "we could not look", which need different words on screen.
   */
  known: boolean
  diff?: string
  warnings?: Problem[]
}

/**
 * One device that may dial in — internal/remote peerView.
 *
 * Stored intent joined to what the kernel knows right now. The join is the
 * point: the name and address are configuration and answer nothing about
 * whether remote access works, while `last_handshake` is the only liveness
 * signal WireGuard has and is not in the configuration at all.
 */
export interface RemotePeer {
  name: string
  /**
   * What the operator calls it, when the name is not what they would write.
   * Absent means the name is shown. The name stays the key: it is what every
   * command and the URL take, and it has to be a DNS label.
   */
  display_name?: string
  address?: string
  routes: 'home' | 'everything'
  public_key?: string
  /**
   * Absent for a device that has never connected, and that absence is
   * load-bearing: "never" almost always means the configuration was not
   * imported or the port is not reachable from outside — a setup problem —
   * while "an hour ago" means it works and the device is asleep.
   */
  last_handshake?: string
  online: boolean
  /**
   * How the device is drawn — what it is and the picture that overrides the one
   * its category would pick. Labels rather than tunnel state, like the name:
   * WireGuard knows a key, so neither reaches the kernel and changing one
   * re-issues nothing.
   */
  category?: ClientCategory
  icon?: string
  /** Where the device was last heard from: its address out in the world. */
  endpoint?: string
  rx_bytes?: number
  tx_bytes?: number
  /**
   * The kernel could not be read, so every observed field above is absent
   * rather than zero. Without it a developer box would report every device as
   * having never connected, which is a claim rather than an absence of one.
   */
  unknown?: boolean
}

export interface RemotePeers {
  peers: RemotePeer[]
  subnet?: string
  as_of: string
}

export interface RemoteStatus {
  enabled: boolean
  interface: string
  listen_port: number
  endpoint?: string
  subnet?: string
  address?: string
  /**
   * This box's public key. Not a secret — it is in every client configuration
   * already — and the one value an operator completing a file by hand needs.
   */
  public_key?: string
  kernel_known: boolean
  interface_present: boolean
  interface_up: boolean
  /** The name is taken by an interface that is not WireGuard. olr will not
   *  touch it (design.md §3.4). */
  interface_foreign?: boolean
  blockers?: Blocker[]
  peers: RemotePeer[]
  drifted: boolean
  drift?: RemotePlan
  drift_error?: string
  as_of: string
}

/**
 * What comes back from writing a device — internal/remote peerResult.
 *
 * `client_config` is the only response in olr that cannot be reproduced. The
 * private key in it is stored nowhere, so a screen that receives one has to
 * treat it as the last chance to hand it over, and must not offer to show *that
 * file* again — there is nothing left to render it from.
 *
 * A device can be given a second one, and that is a different request with a
 * different consequence: `POST /wireguard/peers/{name}/reissue` generates a new
 * key pair, so the file it hands back replaces the old one rather than repeating
 * it.
 */
export interface RemotePeerResult {
  name: string
  address?: string
  client_config?: string
  note?: string
  /** Commands in other modules that this one deliberately does not run. */
  next_steps?: string[]
}

export interface RemoteApplyResult {
  plan: RemotePlan
  steps?: Step[]
  error?: { message: string; problems?: Problem[] }
  /** What is stored now, redacted. Present on the refusal path especially: it
   *  says the document did not move. */
  config?: RemoteConfig
  peer?: RemotePeerResult
}

// --- remote: the proxies -----------------------------------------------------
//
// The tunnel's types above describe kernel state — lines added and removed. A
// proxy's describe a file and a unit, so they are a different set rather than
// the same one widened, which mirrors internal/remote's own split: the two
// objects share the Impact vocabulary and nothing else.

/** One file's worth of pending work — internal/remote proxyChangeView. */
export interface ProxyChange {
  path: string
  kind: 'create' | 'update' | 'delete'
  impact: Impact
  /** Suppresses the contents everywhere this is displayed: the file holds a
   *  credential. It is still written and compared byte for byte. */
  secret?: boolean
  diff: string
}

/** What the daemon needs after the files are written. */
export type ProxyServiceAction = 'none' | 'start' | 'stop' | 'restart'

/** What applying a proxy change would do — internal/remote proxyPlanView. */
export interface ProxyPlan {
  changes: ProxyChange[]
  action: ProxyServiceAction
  impact: Impact
  /** The boot-time state the unit will be moved to, when that has to change. */
  enable?: boolean
  reasons?: string[]
  /** The drift answer, precomputed so a client does not have to reimplement
   *  what counts as "no change". */
  empty: boolean
  /** olr filled in or replaced the password as part of this change, which means
   *  every client has to be handed a new link. */
  password_generated?: boolean
  warnings?: Problem[]
}

/**
 * Whether the SOCKS5 proxy is installed, running and undrifted —
 * internal/remote socksStatus. No cipher and no UDP flag, and no scope either:
 * the proxy has one shape, and it is the one anything that can route here can
 * reach.
 */
export interface SocksStatus {
  enabled: boolean
  listen_port: number
  /** The daemon's one wording of the plaintext-on-the-internet warning. */
  warning?: string
  service?: UnitStatus
  service_error?: string
  binary?: string
  binary_error?: string
  drifted: boolean
  drift?: ProxyPlan
  drift_error?: string
  as_of: string
}

/**
 * The client link, from the one route that hands over a credential.
 *
 * Every other read in olr redacts. This one is the exception the redaction
 * exists to make safe — a password that appears in no `show`, no plan and no log
 * has to appear somewhere, or the feature cannot be used.
 */
export interface ProxyLink {
  url: string
  label?: string
  as_of: string
}

export interface ProxyApplyResult {
  plan: ProxyPlan
  steps?: Step[]
  error?: { message: string; problems?: Problem[] }
  /** What is stored now, redacted. Present on the refusal path especially. */
  config?: unknown
}

// --- remote: who is connected -------------------------------------------------

/** Where one address is — internal/geoip Place. */
export interface GeoPlace {
  city?: string
  region?: string
  latitude?: number
  longitude?: number
  /** ISO 3166 code. */
  country?: string
  country_name?: string
  asn?: number
  /** Who runs the network: usually an ISP or a mobile carrier. */
  org?: string
  /** Not on the internet at all: a private, carrier-grade NAT or link-local address. */
  local?: boolean
}

/** How complete the places are, and whose they are — internal/geoip Status. */
export interface GeoStatus {
  state: 'ready' | 'fetching' | 'unavailable'
  error?: string
  updated?: string
  /** The attribution the databases' licence asks every surface showing a place to carry. */
  source: string
  source_url: string
}

/** One address connected to a proxy — internal/remote clientView. An address, not a device. */
export interface ProxyClient {
  address: string
  connections: number
  /** What it has sent; absent when the kernel does not count bytes. */
  bytes?: number
}

export interface ProxyClients {
  proxy: 'socks5'
  port: number
  clients: ProxyClient[]
}

/** GET /api/remote/clients — internal/remote clientsResponse. */
export interface RemoteClients {
  /** One per proxy that is switched on. */
  proxies: ProxyClients[]
  /** Keyed by bare address: every proxy client, and every tunnel device's last endpoint. */
  places: Record<string, GeoPlace>
  locations: GeoStatus
  /** Whether the kernel counts bytes; without it scanners are listed too. */
  counted: boolean
  error?: string
  as_of: string
}

// --- firewall ---------------------------------------------------------------

/** internal/firewall Plan. Its own shape: lines of a ruleset, not files or units. */
export interface FirewallPlan {
  changes: { kind: 'add' | 'remove'; line: string }[] | null
  impact: 'none' | 'reload' | 'disruptive'
  empty: boolean
  warnings?: Problem[]
}

/** A port this router serves to the outside, and the object that asked for it. */
export interface FirewallOpening {
  for: string
  protocol: 'tcp' | 'udp' | '6in4'
  /** Absent for 6in4, which has no port. */
  port?: number
  /** The one source a 6in4 opening lets in: the tunnel broker. */
  from?: string
}

/** One sourced first-match rule counter — internal/firewall Traffic. */
export interface FirewallTraffic {
  id: string
  source: 'outside' | 'inside' | 'local'
  category: string
  description: string
  destination: 'router' | 'forwarded'
  verdict: 'allowed' | 'blocked'
  packets: number
  bytes: number
}

/** GET /api/firewall/status — internal/firewall Status. */
export interface FirewallStatus {
  native?: boolean
  enabled: boolean
  known: boolean
  drifted: boolean
  plan: FirewallPlan
  inside: string[]
  openings: FirewallOpening[]
  iptv?: { upstream: string; downstream: string[] | null }
  blocked_input: number
  blocked_forward: number
  traffic_known?: boolean
  traffic?: FirewallTraffic[]
  /** Why no plan could be built — today only "there are no networks". */
  problem?: string
}

export interface FirewallApplyResult {
  plan: FirewallPlan
  config: FirewallConfig
  steps?: Step[]
  error?: { message: string; problems?: Problem[] }
}

/** Router-originated HTTPS response latency, excluding DNS, TCP and TLS setup. */
export interface GatewayLatency {
  /** Current router wall time, including its UTC offset. */
  local_time?: string
  target_url: string
  state: 'measuring' | 'ok' | 'unreachable' | 'unavailable'
  milliseconds: number | null
  target: string
  checked_at: string | null
  dns_milliseconds: number | null
  sites: { name: string; url: string; milliseconds: number | null; checked_at: string; selected: boolean }[]
  custom: { name: string; url: string; exit?: string; icon?: string; category?: string; builtin?: boolean; pinned?: boolean; milliseconds: number | null; checked_at: string; error?: string; measuring?: boolean; samples?: { milliseconds: number | null; error?: string; checked_at: string; hour_start?: string }[] }[]
}
