import { t } from '@/lib/i18n'

/**
 * The Gateway directory: a strip of the pages every choice is written against,
 * then one group per part of the router and one tile per way of building it.
 *
 * A router is four things an operator arrives to change — how it reaches the
 * internet, who it hands addresses to, what it resolves, and what it lets
 * through. Each of those is served by a choice, and more than one will be
 * offered per group: a second resolver, a second DHCP server. That is why this
 * is a launcher and not a settings list: the page's subject is the choice, and
 * the switches behind it belong to the page the tile opens.
 *
 * The interfaces, the route table and the rules OLR did not write are not
 * choices — nobody picks between two route tables — so they travel in the strip
 * above the groups instead of a group whose tiles all leave the page.
 *
 * A choice OLR does not configure yet keeps its tile, muted and inert, so a
 * group states what it will contain instead of growing a tile at a time.
 *
 * `to` is the page that configures the choice; a tile owns no state of its own
 * and never opens a dialog.
 */
export interface GatewayChoice {
  id: string
  name: string
  /** The initial drawn on the tile, in the group's hue. */
  letter: string
  /** One sentence for the tooltip; never the only place a function is stated. */
  description: string
  /** The page that configures this choice. Absent while it is planned. */
  to?: string
  /** A choice this group will carry, not yet configurable. */
  planned?: boolean
}

export interface GatewayGroup {
  id: string
  title: string
  /** An oklch hue; the launcher draws `oklch(0.52 0.12 <hue>)`. */
  hue: number
  choices: GatewayChoice[]
}

/**
 * A page the choices are written against: navigation, not a choice.
 *
 * The strip runs in the order a packet meets them — the interfaces it arrives
 * on, the table it is routed by, and the rules outside OLR's management — and
 * it sits above the groups because every group below writes into one of them.
 */
export interface GatewayPage {
  id: string
  get name(): string
  to: string
}

export const GATEWAY_PAGES: GatewayPage[] = [
  { id: 'interfaces', get name() { return t('Interfaces') }, to: '/gateway/interfaces' },
  { id: 'routes', get name() { return t('Routes') }, to: '/gateway/routes' },
  { id: 'unmanaged', get name() { return t('External routing rules') }, to: '/gateway/unmanaged' },
]

// Groups run in the order an operator arrives at them: the one that is set up
// once and then left alone (how it reaches the internet) before the three whose
// defaults are the ones most installs keep.
export const GATEWAY_GROUPS: GatewayGroup[] = [
  {
    id: 'internet',
    get title() { return t('Internet') },
    hue: 215,
    choices: [
      {
        id: 'dhcp',
        name: 'DHCP',
        letter: 'D',
        description: 'Take an address, a default route and the provider’s resolvers automatically.',
        planned: true,
      },
      {
        id: 'pppoe',
        name: 'PPPoE',
        letter: 'P',
        description: 'Dial the provider with the account it issued.',
        planned: true,
      },
      {
        id: 'static',
        get name() { return t('Static address') },
        letter: 'S',
        description: 'Set the address, the next hop and the resolvers yourself.',
        to: '/gateway/interfaces',
      },
    ],
  },
  {
    id: 'dhcp',
    title: 'DHCP',
    hue: 40,
    choices: [
      {
        id: 'dnsmasq',
        name: 'dnsmasq',
        letter: 'D',
        description: 'Addresses, reservations and DHCP options on every network this box serves.',
        to: '/gateway/dhcp',
      },
      {
        id: 'kea',
        name: 'Kea',
        letter: 'K',
        description: 'ISC’s DHCPv4 and DHCPv6 server, configured from a database.',
        planned: true,
      },
    ],
  },
  {
    id: 'dns',
    title: 'DNS',
    hue: 285,
    choices: [
      {
        id: 'unbound',
        name: 'unbound',
        letter: 'U',
        description: 'A recursive resolver: local names, blocking and DNS-over-TLS upstreams.',
        to: '/gateway/dns',
      },
      {
        id: 'dnsmasq',
        name: 'dnsmasq',
        letter: 'D',
        description: 'Answer from the DHCP leases and forward everything else.',
        planned: true,
      },
      {
        id: 'mosdns',
        name: 'mosdns',
        letter: 'M',
        description: 'Split and rewrite queries by rule set before forwarding them.',
        planned: true,
      },
      {
        id: 'adguard',
        name: 'AdGuard Home',
        letter: 'A',
        description: 'Blocking, per-client rules and a query log on its own interface.',
        planned: true,
      },
    ],
  },
  {
    id: 'firewall',
    get title() { return t('Firewall') },
    hue: 25,
    choices: [
      {
        id: 'nftables',
        name: 'nftables',
        letter: 'N',
        description: 'Ordered rules over what may reach this box and the devices behind it.',
        to: '/firewall',
      },
      {
        id: 'forwards',
        get name() { return t('Port forwarding') },
        letter: 'P',
        description: 'Reach a service inside this network through an external port.',
        to: '/advanced/forwards',
      },
      {
        id: 'filtering',
        get name() { return t('External filtering') },
        letter: 'E',
        description: 'Forwarding policies outside OLR management, shown read-only.',
        to: '/advanced/filtering',
      },
    ],
  },
]
