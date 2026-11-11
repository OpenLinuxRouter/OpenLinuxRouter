import { t } from '@/lib/i18n'
import { Activity, ArrowLeftRight, EthernetPort, Shield, SlidersHorizontal, Waypoints, Wifi, Wrench } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

/**
 * Every section of the app, and everything inside one.
 *
 * This table describes top-level sections and nested Gateway destinations. It renders in five
 * places now — the top bar, the tab bar, the page title, the settings list on a
 * section's landing page, and the header of every sub-page — because writing a
 * heading four times is four ways for it to drift.
 *
 * ## Why there is a second level at all
 *
 * Every section used to be one page holding everything it could do: DNS stacked
 * eight cards, each opening with two to four sentences, so an operator asking
 * "is DNS working" read a paragraph about DNS-over-TLS on the way past. The
 * writing was not the problem — a setting that can take down a house's internet
 * deserves a sentence saying so. Putting all of it on one screen was.
 *
 * So a section's landing page now answers two questions and no others: is this
 * working, and what is it doing. Everything that is a *setting* moves one level
 * down, into a page of its own, and takes its explanation with it. The long
 * blurbs below are that explanation — they are not shown in the list, only on
 * the page the row leads to, which is the moment they are worth reading.
 *
 * Networks, DHCP and DNS live under Gateway: the interfaces and subnets it
 * serves, the addresses it hands out, and the names it resolves belong together.
 * Wireless has its own placeholder section; Access follows it.
 *
 * Access is the directory of remote-access technologies; configuration stays
 * with its module. Advanced is the low-level plumbing and the machine itself:
 * System (firewall, the way in to configure it, port forwarding), Services
 * (Caddy, DDNS, multicast) and Diagnostics (the tools). Firewall and
 * Tools keep their URLs; Tools is flattened into the directory, its detail
 * pages remain.
 *
 * Clients is absent because it is not a section: the client list is the body of
 * the overview. Filing it under DHCP was considered and rejected — the
 * statically-addressed printer has never held a lease, and would have lived on
 * a page named for the protocol that has never seen it.
 *
 * Caddy is named for the proxy it runs, which is what an operator troubleshoots;
 * the module, the config field and the CLI still say `ingress`.
 */
export interface SettingGroup {
  /** The last segment of the URL: /gateway/dns/blocking. */
  slug: string
  label: string
  /** Shown on the sub-page itself, never in the list that links to it. */
  blurb: string
  /** Override for destinations that retain an existing URL. */
  to?: string
}

export interface Section {
  to: string
  label: string
  icon: LucideIcon
  end: boolean
  /** A destination within a section, not a top-level navigation item. */
  nested?: boolean
  /** Top-level navigation owner for a destination with a separate URL. */
  navTo?: string
  groups: SettingGroup[]
}

export const SECTIONS: Section[] = [
  {
    to: '/',
    get label() { return t('Overview') },
    icon: Activity,
    end: true,
    groups: [],
  },
  {
    to: '/gateway',
    get label() { return t('Gateway') },
    icon: Waypoints,
    end: false,
    groups: [
      {
        slug: 'interfaces',
        get label() { return t('Interfaces & routing') },
        blurb: '',
      },
      {
        slug: 'routes',
        get label() { return t('Routes') },
        // Read-only, and named for the object rather than for a setting: the
        // routing table is where every route already is, including the ones
        // olr did not write and must not claim (design.md §3.4).
        get blurb() { return t('The routing table: the box’s own table, each exit’s table, and the rules outside OLR.') },
      },
      {
        slug: 'dhcp',
        label: 'DHCP',
        get blurb() { return t('Define served networks, hand out addresses and reserve them for clients that need a fixed one.') },
      },
      {
        slug: 'dns',
        label: 'DNS',
        get blurb() { return t('Resolve names, manage local names and control what clients can look up.') },
      },
      {
        slug: 'unmanaged',
        get label() { return t('External routing rules') },
        // design.md §3.4: pretending we are the only actor is a bug. Somebody
        // else's rules are shown so a hand-rolled setup is legible rather than
        // mysterious.
        get blurb() { return t('Read-only rules outside OLR management.') },
      },
    ],
  },
  {
    to: '/gateway/dhcp',
    label: 'DHCP',
    icon: Waypoints,
    nested: true,
    end: false,
    groups: [
      { slug: 'details', label: 'DHCP', blurb: '' },
      {
        slug: 'ranges',
        get label() { return t('Address ranges') },
        get blurb() { return t('Addresses handed out on each network.') },
      },
      {
        slug: 'reservations',
        get label() { return t('Reserved addresses') },
        get blurb() { return t('Fixed DHCP addresses for selected clients.') },
      },
      // Interface adoption stays under Gateway / Interfaces; served networks are beside DHCP.
      {
        slug: 'advanced',
        get label() { return t('Advanced') },
        get blurb() { return t('Custom dnsmasq settings. OLR-managed directives cannot be overridden.') },
      },
    ],
  },
  {
    to: '/gateway/dns',
    label: 'DNS',
    icon: Waypoints,
    nested: true,
    end: false,
    groups: [
      { slug: 'details', label: 'DNS', blurb: '' },
      {
        slug: 'blocking',
        get label() { return t('Blocking') },
        get blurb() { return t('DNS blocking rules for selected clients and networks.') },
      },
      {
        slug: 'names',
        get label() { return t('Local names') },
        get blurb() { return t('Names resolved locally by this router.') },
      },
      {
        slug: 'resolving',
        get label() { return t('Upstream DNS') },
        blurb: '',
      },
      {
        slug: 'listening',
        get label() { return t('DNS listening') },
        blurb: '',
      },
      {
        slug: 'enforcement',
        get label() { return t('DNS enforcement') },
        get blurb() { return t('Redirect external DNS requests on selected networks.') },
      },
      {
        slug: 'advanced',
        get label() { return t('Advanced') },
        blurb:
          '',
      },
    ],
  },
  {
    to: '/wireless',
    get label() { return t('Wireless') },
    icon: Wifi,
    end: false,
    groups: [],
  },
  {
    // The devices olr reads but does not configure: switches, access points and
    // routers. Not `/network`: in this product a network is a subnet Gateway
    // serves, and that is what `/gateway/networks` already means.
    to: '/devices',
    get label() { return t('Devices') },
    icon: EthernetPort,
    end: false,
    groups: [],
  },
  {
    to: '/firewall',
    get label() { return t('Firewall') },
    icon: Shield,
    nested: true,
    navTo: '/advanced',
    end: false,
    groups: [],
  },
  {
    to: '/access',
    get label() { return t('Access') },
    icon: ArrowLeftRight,
    end: false,
    groups: [
      { slug: 'gateway', get label() { return t('Gateway') }, get blurb() { return t('Use another gateway or an existing VPN interface.') } },
      { slug: 'tunnelbroker', label: 'Tunnel Broker', blurb: '' },
      { slug: 'wireguard', label: 'WireGuard', blurb: '' },
      { slug: 'socks-server', get label() { return t('SOCKS5 server') }, blurb: '' },
      {
        slug: 'socks5',
        get label() { return t('External SOCKS5 proxy') },
        blurb: '',
      },
    ],
  },
  {
    to: '/tools',
    get label() { return t('Tools') },
    icon: Wrench,
    nested: true,
    navTo: '/advanced',
    end: false,
    groups: [],
  },
  {
    to: '/advanced',
    get label() { return t('Advanced') },
    icon: SlidersHorizontal,
    end: false,
    // Only the labels and explanations: the directory's own page decides which
    // group each feature belongs to and whether it gets a tile at all.
    groups: [
      { slug: 'firewall', get label() { return t('Firewall') }, blurb: '' },
      {
        slug: 'access',
        get label() { return t('Administrator access') },
        blurb: 'Who may configure this router, and how they sign in.',
      },
      {
        // Reached from port forwarding when foreign filtering exists.
        slug: 'forwards',
        get label() { return t('Port forwarding') },
        get blurb() { return t('Forwarding a port also grants its firewall permission.') },
      },
      {
        slug: 'filtering',
        get label() { return t('External filtering') },
        get blurb() { return t('Read-only forwarding policies outside OLR management.') },
      },
      { slug: 'ddns', get label() { return t('DDNS') }, blurb: '' },
      { slug: 'iptv', get label() { return t('IPTV multicast') }, blurb: '' },
      {
        slug: 'caddy',
        label: 'Caddy',
        // The module and the CLI call this `ingress`; the page wears the proxy's
        // name because that is what an operator is running here.
        blurb: 'Publish a service on your own domain. Published addresses still need their own access controls.',
      },
    ],
  },
]

/** Overview detail pages share the section's navigation highlight. */
const OVERVIEW_PAGES = new Set(['/latency', '/map', '/quality'])

/** The section a path belongs to, section landing pages and sub-pages alike. */
export function sectionOf(pathname: string): Section | undefined {
  const path = OVERVIEW_PAGES.has(pathname) ? '/' : pathname
  return [...SECTIONS].reverse().find(({ to, end }) =>
    end ? path === to : path === to || path.startsWith(`${to}/`),
  )
}

/** One group, by the section path and slug the caller already knows. */
export function groupOf(sectionTo: string, slug: string): { section: Section; group: SettingGroup } {
  const section = SECTIONS.find((s) => s.to === sectionTo)
  const group = section?.groups.find((g) => g.slug === slug)
  if (!section || !group) {
    // A slug that is not in the table is a routing bug, not a user error: the
    // routes and this list are written together. Failing loudly in development
    // beats a page with an empty heading.
    throw new Error(`no settings group ${sectionTo}/${slug}`)
  }
  return { section, group }
}
