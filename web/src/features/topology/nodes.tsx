import { t, useLocale } from '@/lib/i18n'
import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  ChevronUp,
  FolderInput,
  FolderPlus,
  Layers,
  Pencil,
  Plus,
  Router,
  Tag,
  Trash2,
  X,
} from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link } from 'react-router'

import { Button } from '@/components/ui/button'
import { RowMenu } from '@/components/ui/row-action'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { brandLogoHref } from '@/features/brand-logos/brand-logos'
import { DeviceIcon } from '@/features/clients/client-icon'
import type { GroupDeletion } from '@/features/clients/group-actions'
import { groupOptions, MAX_GROUP_DEPTH, parentChoices } from '@/features/clients/group-tree'
import { lastHeardLabel, lastHeardTitle } from '@/features/clients/last-heard'
import { WayBadge } from '@/features/remote/way-badge'

import type { DialInPeer } from '@/features/topology/dial-in'
import type { Density, GroupVariant, Hidden } from '@/features/topology/layout'
import type { MapGroup } from '@/features/topology/model'
import { magnitude, type Flow, type TrafficView } from '@/features/topology/traffic'
import type { ClientRow } from '@/lib/api-types'
import type { ClientsGroup, Service } from '@/lib/config-types'
import { getToken } from '@/lib/api'
import { cn, formatAgo, formatBytes, formatRate, formatRateCompact } from '@/lib/utils'

/**
 * What the map can ask its page to do. All optional: without them the map is
 * a picture, which is what it should be anywhere that cannot change groups.
 */
export interface MapActions {
  onSelect?: (device: ClientRow) => void
  /** Open one dialed-in device, whose page is the tunnel's answer about it. */
  onOpenDialIn?: (peer: DialInPeer) => void
  onCreateGroup?: (parent?: string) => void
  onRenameGroup?: (name: string) => void
  onDeleteGroup?: (deletion: GroupDeletion) => void
  onMoveGroup?: (name: string, parent: string) => void
}

/* -------------------------------------------------------------------------- */
/* The router                                                                 */
/* -------------------------------------------------------------------------- */

export interface NetworkInfo {
  name: string
  exit: string
  inherited: boolean
  down: boolean
  unchecked: boolean
}

/**
 * The router: its name and, when counting, its total rate — and a network
 * only when its way out is down. Inheritance is a dashed outline on that chip
 * and the red says it is not answering.
 */
export function RouterNode({ networks, traffic }: { networks: NetworkInfo[]; traffic: TrafficView }) {
  useLocale()
  return (
    <div className="min-w-60 rounded-2xl border bg-card px-4 py-3 shadow-sm">
      <div className="flex items-center gap-3">
        {/* The one dark tile on the map: this box is the hardware, everything
            else is a list of what it serves. */}
        <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-foreground text-background">
          <Router className="size-5" aria-hidden />
        </span>
        <div className="min-w-0">
          <div className="text-sm font-semibold">{t('This router')}</div>
          {traffic.rated && <Rates flow={traffic.total} className="text-xs" neutral />}
        </div>
      </div>

      {/* Only a network whose way out has stopped answering. Every network's
          route, on every visit, was small print nobody needed while it was
          fine — the Networks page has it — but one that is down is the most
          urgent fact on the map, and stays. */}
      {networks.some((n) => n.down) && (
        <ul className="mt-2.5 flex flex-wrap gap-1.5 border-t pt-2.5" aria-label={t('Networks that are down')}>
          {networks.filter((n) => n.down).map((n) => (
            <li key={n.name}>
              <NetworkChip network={n} />
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function NetworkChip({ network: n }: { network: NetworkInfo }) {
  useLocale()
  return (
    <span
      title={
        (n.down ? `${n.exit} is not responding. ` : '') +
        (n.inherited ? 'Follows the box-wide setting' : `Set on ${n.name}`)
      }
      className={cn(
        'inline-flex items-center gap-1.5 rounded-md border px-1.5 py-0.5 text-xs',
        n.inherited && 'border-dashed',
        n.down ? 'border-destructive/50 text-destructive' : 'text-muted-foreground',
      )}
    >
      <span
        aria-hidden
        className={cn(
          'size-1.5 rounded-full',
          n.down ? 'bg-destructive' : n.unchecked ? 'bg-muted-foreground/40' : 'bg-success',
        )}
      />
      <span className="font-mono text-foreground/85">{n.name}</span>
      <span aria-hidden>→</span>
      <span className="sr-only">goes out</span>
      {n.exit || 'direct'}
    </span>
  )
}

/* -------------------------------------------------------------------------- */
/* Clients dialed in over WireGuard                                            */
/* -------------------------------------------------------------------------- */

/**
 * A device that reached this router over the tunnel, drawn as a device is.
 *
 * A peer of the router, not a client of one of its networks: there is no MAC to
 * hang a device row on (docs/remote-access.md §3.3), so the card is the whole
 * of what the map says about it. What it looks like is nonetheless a device
 * node — the peer's own picture, its label over its tunnel address, a figure in
 * the same narrow column a device row keeps for its rate or its last-heard
 * time — because an operator reads a dialled-in phone as a device either way,
 * and a box that announced the protocol it arrived on told them which module to
 * edit rather than which machine this is.
 *
 * **The way in is a badge on the picture.** Drawn only as a device, these cards
 * were indistinguishable at a glance from the machines under the router, and
 * every mark tried in the card's own text (a protocol in the figure column, a
 * legend across the top border) competed with the name for the same 272 pixels.
 * The WireGuard mark rides the picture's corner instead: it is where the eye
 * already looks for what this machine is, it costs the card no width, and it is
 * the method in the brand's own colour rather than a second word to read. The
 * picture keeps its place — a badge on it is not a picture in place of the
 * machine, which is the mistake this module has always refused (§3.4).
 *
 * The figure is the one thing the tunnel can answer and a device cannot: the
 * four states in `dialInLabel` (docs/remote-access.md §3.4). No rate, and no
 * bandwidth bar, which is why the band's lines stay the thin ones (band.ts).
 */
export function DialInNode({ peer, iface, onOpen }: { peer: DialInPeer; iface: string; onOpen?: () => void }) {
  useLocale()
  const state = dialInLabel(peer)
  const where = peer.endpoint ? `, last heard from ${peer.endpoint}` : ''
  const moved = peer.rxBytes || peer.txBytes
    ? `, ↓ ${formatBytes(peer.rxBytes)} ↑ ${formatBytes(peer.txBytes)} since it connected`
    : ''
  const named = peer.label === peer.name ? peer.name : `${peer.label} (${peer.name})`
  const title = `${named} over ${iface}${peer.address ? ` at ${peer.address}` : ''}: ${state}${where}${moved}`
  // Away and never-connected dim the picture, the way a device's is dimmed on
  // its own page; `never` is not a fault, so nothing here takes the colour the
  // map keeps for a way out that has stopped answering.
  const here = peer.state === 'connected' || peer.state === 'unknown'
  return (
    <Shell
      onClick={onOpen}
      title={title}
      className={cn(
        'flex size-full min-w-0 items-center gap-2.5 rounded-lg border bg-card pr-2.5 pl-3 text-left shadow-xs transition-colors duration-150',
        onOpen && 'hover:bg-accent/60 active:bg-accent',
        focusRing,
      )}
    >
      {/* The picture, and the way it got here on the corner of it. */}
      <span className="relative flex shrink-0">
        <DeviceIcon
          icon={peer.icon}
          category={peer.category ?? 'unknown'}
          online={here ? undefined : false}
          size="sm"
        />
        <WayBadge />
      </span>
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-[13px] leading-[18px] font-medium">{peer.label}</span>
        {peer.address ? (
          <span className="truncate font-mono text-[11px] leading-4 text-muted-foreground">{peer.address}</span>
        ) : (
          // The tunnel has not said: a peer that never connected has no address
          // observed, which is not the same as one without a configuration.
          <span className="truncate text-[11px] leading-4 text-muted-foreground italic">{t('no address')}</span>
        )}
      </span>
      {peer.state === 'connected' ? (
        // Presence is a dot, not a word. "Connected" beside a picture that is
        // lit already said the same thing twice and read as a description of
        // the card; the dot is the status it is, and the word stays for the
        // screen reader and the card's title.
        <span className="flex w-16 shrink-0 items-center justify-end">
          <span aria-hidden className="size-2 rounded-full bg-success" />
          <span className="sr-only">{t('Connected')}</span>
        </span>
      ) : (
        <span className="w-16 shrink-0 text-right text-[11px] leading-4 text-muted-foreground">{state}</span>
      )}
    </Shell>
  )
}

/**
 * Four states in four words.
 *
 * "Never connected" is not a synonym for "offline": it is the answer to a setup
 * question — was the configuration imported, is the port reachable — and it is
 * the one state the operator can still fix (`PeerBadge` on the tunnel's page
 * draws the same distinction). "Status unavailable" exists so that a kernel
 * that could not be read is not reported as a device that never called.
 */
function dialInLabel(peer: DialInPeer): string {
  switch (peer.state) {
    case 'connected': return t('Connected')
    case 'away': return peer.lastHandshake ? formatAgo(peer.lastHandshake) : 'Away'
    case 'never': return t('Never connected')
    case 'unknown': return t('Status unavailable')
  }
}

/* -------------------------------------------------------------------------- */

/* -------------------------------------------------------------------------- */
/* Groups                                                                     */
/* -------------------------------------------------------------------------- */

/**
 * A group, in whichever of its four shapes the layout gave it.
 *
 * The container is only a frame and a header: the devices and subgroups
 * inside it are drawn as their own nodes on top, so that each can move on its
 * own when the layout changes — a device fanning out of its container when the
 * group is focused is the same element travelling, not one disappearing and
 * another appearing. The frame's job is to be quiet: a hairline, a faint fill
 * one step darker per level of nesting, and no colour of its own. Groups used
 * to be tinted by position, and the tints said nothing a title did not while
 * competing with the one colour that means something, the red of a way out
 * that is down.
 */
export function GroupNode({
  group,
  variant,
  nested,
  dashed,
  groups,
  actions,
  onFocus,
}: {
  group: MapGroup
  variant: GroupVariant
  /** A container inside another: its list sits on a quieter fill. */
  nested?: boolean
  /** For the bucket beside the operator's groups, which is not one of them. */
  dashed: boolean
  groups: ClientsGroup[]
  actions: MapActions
  /** Open a named group, or focus the unnamed device bucket. */
  onFocus: () => void
}) {
  useLocale()
  const bucket = group.name === undefined

  if (variant === 'chip') {
    return (
      <button
        type="button"
        onClick={onFocus}
        title={`${bucket ? 'Focus' : 'Open'} ${group.title}`}
        className={cn(
          'flex size-full items-center gap-2 rounded-lg border bg-card px-3 text-left shadow-xs transition-colors hover:bg-accent',
          dashed && 'border-dashed',
          focusRing,
        )}
      >
        <span className="min-w-0 flex-1 truncate text-xs font-medium">{group.title}</span>
      </button>
    )
  }

  if (variant === 'card') {
    return (
      <button
        type="button"
        onClick={onFocus}
        title={`${bucket ? 'Focus' : 'Open'} ${group.title}`}
        className={cn(
          'flex size-full items-center gap-2.5 rounded-lg border bg-muted/40 px-3 text-left transition-colors hover:bg-accent',
          focusRing,
        )}
      >
        <span className="flex size-7 shrink-0 items-center justify-center rounded-md border bg-card text-muted-foreground">
          <Layers className="size-3.5" aria-hidden />
        </span>
        <span className="min-w-0 flex-1 truncate text-[13px] font-medium">{group.title}</span>
      </button>
    )
  }

  const focused = variant === 'focused'
  return (
    <section
      aria-label={group.title}
      className={cn(
        '@container relative size-full',
        focused && 'rounded-xl border bg-card shadow-sm ring-1 ring-foreground/5',
        focused && dashed && 'border-dashed',
      )}
    >
      {/* The title sits above the list, not inside a frame round it: a
          container is its list, and a box round a box round rows was the
          nesting the eye had to count its way out of. */}
      {!focused && (
        <div
          aria-hidden
          className={cn(
            'absolute inset-x-0 top-10 bottom-0 rounded-xl',
            nested ? 'bg-muted/60 dark:bg-muted/40' : 'bg-card shadow-xs ring-1 ring-foreground/[0.07]',
            dashed && 'border border-dashed bg-transparent shadow-none ring-0',
          )}
        />
      )}
      <div
        className={cn(
          'relative flex h-10 items-center gap-2',
          focused ? 'pl-3.5' : 'pl-1.5',
          focused && !bucket ? 'pr-2' : 'pr-3.5',
        )}
      >
        {/* The whole header is the button, with the menu stacked above it —
            a button inside a button is not allowed, and a title that is the
            only click target is a small one. */}
        <button
          type="button"
          onClick={onFocus}
          aria-label={!bucket ? `Open ${group.title}` : focused ? 'Back to every group' : `Focus ${group.title}`}
          className={cn(
            'absolute inset-0 transition-colors hover:bg-foreground/[0.03]',
            focused ? 'rounded-xl' : 'rounded-lg',
            focusRing,
          )}
        />
        {/* The name and nothing else. Counts, rates and a menu on every card
            made the map a wall of small print; the line into the card says
            how busy it is, and what can be done to a group is offered once
            somebody has chosen it, by focusing. */}
        <div
          className={cn(
            'pointer-events-none relative min-w-0 flex-1 truncate',
            focused || nested ? 'text-[13px] font-medium' : 'text-[15px] font-semibold tracking-tight',
          )}
        >
          {group.title}
        </div>
        {focused && !bucket && <GroupMenu group={group} groups={groups} actions={actions} />}
        {focused && (
          <span className="pointer-events-none relative flex size-7 items-center justify-center text-muted-foreground">
            <X className="size-4" aria-hidden />
          </span>
        )}
      </div>
    </section>
  )
}

/**
 * A group's menu: rename, make a group inside it, move it inside another, and
 * delete it. "Move inside…" lists every group it could go into, in tree order,
 * with the ones it cannot — itself, anything below it, anything too deep to
 * take everything it carries — left out or greyed.
 */
function GroupMenu({ group, groups, actions }: { group: MapGroup; groups: ClientsGroup[]; actions: MapActions }) {
  useLocale()
  const name = group.name!
  const { onRenameGroup, onDeleteGroup, onMoveGroup, onCreateGroup } = actions
  if (!onRenameGroup && !onDeleteGroup && !onMoveGroup && !onCreateGroup) return null

  const stored = groups.find((g) => g.name === name)
  const parent = stored?.parent ?? ''
  const choices = parentChoices(groups, name)
  const depth = groupOptions(groups).find((g) => g.name === name)?.depth ?? 0

  return (
    <RowMenu label={`${group.title} options`}>
        <DropdownMenuItem render={<Link to={`/groups/${encodeURIComponent(name)}`} />}>
          <Layers /> {t('View group details')} </DropdownMenuItem>
        <DropdownMenuSeparator />
        {onRenameGroup && (
          <DropdownMenuItem onClick={() => onRenameGroup(name)}>
            <Pencil /> {t('Rename')} </DropdownMenuItem>
        )}
        {onCreateGroup && depth < MAX_GROUP_DEPTH - 1 && (
          <DropdownMenuItem onClick={() => onCreateGroup(name)}>
            <FolderPlus /> {t('New group inside')} </DropdownMenuItem>
        )}
        {onMoveGroup && (
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <FolderInput /> {t('Move inside…')} </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="min-w-44">
              <DropdownMenuRadioGroup
                value={parent || ' top'}
                onValueChange={(value: string) => {
                  const next = value === ' top' ? '' : value
                  if (next !== parent) onMoveGroup(name, next)
                }}
              >
                <DropdownMenuRadioItem value=" top">{t('Top level')}</DropdownMenuRadioItem>
                {choices.length > 0 && <DropdownMenuSeparator />}
                {choices.map((c) => (
                  <DropdownMenuRadioItem
                    key={c.name}
                    value={c.name}
                    disabled={!c.allowed}
                    title={c.allowed ? undefined : `Groups nest at most ${MAX_GROUP_DEPTH} levels deep`}
                  >
                    <span style={{ paddingLeft: c.depth * 12 }}>{c.name}</span>
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        )}
        {onDeleteGroup && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              variant="destructive"
              onClick={() =>
                onDeleteGroup({
                  name,
                  devices: group.directCount,
                  groups: group.subgroupCount,
                  parent: parent || undefined,
                })
              }
            >
              <Trash2 /> {t('Delete')} </DropdownMenuItem>
          </>
        )}
    </RowMenu>
  )
}

/* -------------------------------------------------------------------------- */
/* Clients                                                                    */
/* -------------------------------------------------------------------------- */

/**
 * The air between a row's service tiles and the column that says the rate or
 * when the device was last heard.
 *
 * Both are alignment columns: the tiles start at a fixed offset from the right
 * edge so they line up down a group, and the label sits in a box of its own so
 * rates line up too. A wide label ("16m ago") fills that box, so the offset has
 * to leave room beyond it; without the gap the two met on the row where a
 * device had a service but no live rate. See SERVICE_INSET for the sum.
 */
const SERVICE_GAP = 8

/**
 * Where the service tiles begin, in px from a row's right edge: the row's own
 * `px-1`, the shell's `px-3`, the `w-12` label column, and SERVICE_GAP.
 *
 * Spelled out rather than folded into a class name because three of the terms
 * are set elsewhere in this file; when one of them changes, this is the line
 * that should stop adding up.
 */
const SERVICE_INSET = 4 + 12 + 48 + SERVICE_GAP

/**
 * One device, at either density.
 *
 * Compact is an icon and a name — enough to see what is where, small enough
 * that sixty of them are still a picture. Detail adds the address, the network
 * it is on, and its live rate with a thin bar underneath; the bar is scaled
 * against the busiest device on the whole map, not in its group, so a bar means
 * the same length everywhere on screen.
 */
export function ClientNode({
  device,
  services,
  serviceDomain,
  serviceDomainKnown,
  density,
  row,
  traffic,
  busiest,
  actions,
}: {
  device: ClientRow
  services?: Service[]
  serviceDomain?: string
  /** Whether that suffix has been answered for, or is still on its way. */
  serviceDomainKnown?: boolean
  density: Density
  /** Drawn as a row of its container's list rather than as a card of its own. */
  row?: { first: boolean }
  traffic: TrafficView
  busiest: number
  actions: MapActions
}) {
  useLocale()
  const { onSelect } = actions
  const name = device.name || device.mac
  const flow = traffic.flowOf(device)
  const share = busiest > 0 ? magnitude(flow, traffic.rated) / busiest : 0
  const open = onSelect ? () => onSelect(device) : undefined
  const serviceWidth = Math.min(services?.length ?? 0, 3) * 28 - 4

  if (row) {
    return (
      <div className={cn(
        'group/node relative size-full px-1 py-0.5',
        onSelect && 'rounded-lg transition-colors hover:bg-foreground/[0.035]',
      )}>
        {/* Hairlines between rows, inset past the icon the way a settings list
            draws them, and none above the first. */}
        {!row.first && <span aria-hidden className="absolute top-0 right-0 left-[60px] h-px bg-border/70" />}
        <Shell
          onClick={open}
          title={name}
          className={cn(
            'flex size-full min-w-0 items-center gap-2 rounded-lg px-3 text-left transition-colors',
            focusRing,
          )}
        >
          <DeviceIcon
            icon={device.icon}
            category={device.category}
            vendor={device.vendor}
            vendorKey={device.vendor_key}
            size="sm"
          />
          <span className="flex min-w-0 flex-1 flex-col">
            <span className="truncate text-sm font-medium">{name}</span>
            {density === 'detail' && <Address device={device} />}
          </span>
          {services?.length ? <span aria-hidden className="shrink-0" style={{ width: serviceWidth + SERVICE_GAP }} /> : null}
          <span className="w-12 shrink-0 text-right text-xs text-muted-foreground tabular-nums whitespace-nowrap">
            {/* Away is said beside the dimmed name, so presence is never
                colour alone — and a green dot on every row that is fine was
                the one mark on the list that said nothing. */}
            <ClientActivity device={device} flow={flow} rated={traffic.rated} counting={traffic.counting} />
          </span>
        </Shell>
        <ServiceLinks services={services} domain={serviceDomain} domainKnown={serviceDomainKnown}
          className="absolute inset-y-0" inset={SERVICE_INSET} width={serviceWidth} compact />
      </div>
    )
  }

  const shell = cn(
    'flex size-full min-w-0 items-center rounded-lg border bg-card text-left shadow-xs transition-colors duration-150',
    onSelect && 'hover:bg-accent/60 active:bg-accent',
    focusRing,
    services?.length && 'pb-8',
  )

  const body =
    density === 'compact' ? (
      <Shell onClick={open} title={name} className={cn(shell, 'gap-2 pr-2.5 pl-2')}>
        <DeviceIcon
          icon={device.icon}
          category={device.category}
          vendor={device.vendor}
          vendorKey={device.vendor_key}
          size="sm"
          className="size-6"
        />
        <span className="min-w-0 flex-1 truncate text-xs font-medium">{name}</span>
      </Shell>
    ) : (
      <Shell onClick={open} className={cn(shell, 'gap-2.5 px-3')}>
        <DeviceIcon
          icon={device.icon}
          category={device.category}
          vendor={device.vendor}
          vendorKey={device.vendor_key}
          size="sm"
        />
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="flex min-w-0 items-center gap-1.5">
            <span className="truncate text-[13px] leading-[18px] font-medium">{name}</span>
          </span>
          <Address device={device} />
          {traffic.counting && device.seen && (
            <span className="mt-1.5 flex items-center gap-2">
              {/* Scaled against the busiest device on the whole map. A floor
                  of 2% so a device that moved *something* is visibly not the
                  same as one that moved nothing. */}
              <span aria-hidden className="h-0.5 min-w-6 flex-1 overflow-hidden rounded-full bg-muted">
                <span
                  className="block h-full rounded-full bg-foreground/35"
                  style={{ width: share > 0 ? `${Math.max(2, share * 100)}%` : 0 }}
                />
              </span>
              <span className="shrink-0 text-[10.5px] leading-3 text-muted-foreground tabular-nums">
                <ClientTraffic device={device} flow={flow} rated={traffic.rated} counting />
              </span>
            </span>
          )}
        </span>
      </Shell>
    )

  return (
    <div className="group/node relative size-full">
      {body}
      <ServiceLinks services={services} domain={serviceDomain} domainKnown={serviceDomainKnown}
        className={cn('absolute bottom-0.5 right-2', density === 'compact' ? 'left-10' : 'left-[54px]')} />
    </div>
  )
}

/** Links sit beside, not inside, the device detail button. */
function ServiceLinks({ services, domain, domainKnown = true, className, width, inset, compact = false }: {
  services?: Service[]
  domain?: string
  domainKnown?: boolean
  className: string
  width?: number
  /**
   * Distance from the right edge. Only the copy of a row's tiles, which is
   * placed absolutely so it lines up across rows, passes this; the card copy
   * sits in the flow.
   */
  inset?: number
  compact?: boolean
}) {
  useLocale()
  if (!services?.length) return null
  return (
    <span style={{ width, right: inset }} className={cn('flex min-w-0 items-center gap-1 overflow-x-auto overflow-y-hidden', compact ? 'h-full' : 'h-8', className)}>
      {services.slice(0, 8).map((service) => {
        const label = domain ? `${service.name}.${domain}` : service.name
        // No version until the suffix that goes into it is known. The suffix
        // arrives with the ingress status, a beat after the device list the map
        // waits on, so a tile drawn before it would ask for an icon under a
        // name it is about to abandon — and asking is not free: the daemon
        // discovers the icon by fetching the published site.
        const version = domainKnown ? serviceIconVersion(service, domain) : undefined
        const tile = <ServiceIcon key={`${service.name}:${version ?? ''}`} name={service.name} version={version} choice={service.icon} />
        return domain ? (
          <a key={service.name} href={`https://${label}${service.link_path ?? ''}`} target="_blank" rel="noopener noreferrer"
            title={`Open ${label}${service.link_path ?? ''}`} aria-label={`Open ${label}${service.link_path ?? ''}`}
            className={cn('flex shrink-0 items-center justify-center rounded-[6px] bg-muted transition-colors hover:bg-accent focus-visible:outline-2', compact ? 'size-6' : 'size-7')}>
            {tile}
          </a>
        ) : (
          <span key={service.name} title={service.name} className={cn('flex shrink-0 items-center justify-center rounded-[6px] bg-muted', compact ? 'size-6' : 'size-7')}>{tile}</span>
        )
      })}
      {services.length > 8 && (
        <span className="flex h-7 shrink-0 items-center text-[11px] text-muted-foreground" title={services.slice(8).map((s) => s.name).join(', ')}>
          +{services.length - 8}
        </span>
      )}
    </span>
  )
}

// The browser caches successful icons for a day; change the URL when the
// published destination or its icon-discovery path changes.
export function serviceIconVersion(service: Service, domain?: string): string {
  const { device, host, port, scheme } = service.upstream
  const identity = JSON.stringify([service.name, domain, service.link_path, service.icon, device, host, port, scheme])
  let hash = 2166136261
  for (let i = 0; i < identity.length; i++) {
    hash = Math.imul(hash ^ identity.charCodeAt(i), 16777619)
  }
  return (hash >>> 0).toString(36)
}

/**
 * Icons are shared, not owned.
 *
 * The same service is a tile on the map, a tile on its device's page, and the
 * same tile again after a density change or a visit back to a page. A fetch
 * owned by the tile would abort on every unmount, cancelling bytes the next
 * mount then asks for again — and the daemon discovers an icon by fetching the
 * published site, so each of those cancellations is a discovery thrown away.
 * So the request and its object URL belong to the module, keyed by the URL that
 * versions it, and unmounting only stops listening.
 */
const icons = new Map<string, Promise<string | undefined>>()

function loadIcon(url: string): Promise<string | undefined> {
  const started = icons.get(url)
  if (started) return started
  const token = getToken()
  const loading = fetch(url, { headers: token ? { Authorization: `Bearer ${token}` } : {} })
    .then(async (response) => (response.ok ? URL.createObjectURL(await response.blob()) : undefined))
    .catch(() => undefined)
    // A site with no icon is answering for itself; a failed request may just
    // be a flat network, so it is forgotten and the next tile asks again.
    .then((objectURL) => {
      if (!objectURL) icons.delete(url)
      return objectURL
    })
  icons.set(url, loading)
  return loading
}

/**
 * One published service's icon, or its initials while there is none to draw.
 *
 * `version` is absent until the published suffix is known — see ServiceLinks —
 * and an absent version means nothing is asked for yet.
 */
export function ServiceIcon({ name, version, choice }: { name: string; version?: string; choice?: string }) {
  useLocale()
  const [icon, setIcon] = useState<string>()
  const [broken, setBroken] = useState<string>()
  useEffect(() => {
    if (choice || !version) return
    let listening = true
    loadIcon(`/api/ingress/services/${encodeURIComponent(name)}/icon?v=${version}`).then((objectURL) => {
      if (listening && objectURL) setIcon(objectURL)
    })
    return () => { listening = false }
  }, [name, version, choice])
  const preview = choice?.startsWith('data:') ? choice : (brandLogoHref(choice) ?? icon)
  // A mark this build does not carry 404s like any missing picture, and the
  // tile goes back to being the service's own initials rather than a broken
  // image.
  const show = preview && preview !== broken
  return show ? (
    <img src={preview} alt="" className="size-5 object-contain" onError={() => setBroken(preview)} />
  ) : (
    <span className="text-[10px] font-semibold uppercase" aria-hidden>{name.slice(0, 2)}</span>
  )
}

/** A button when the node does something, and only then. */
function Shell({
  onClick,
  title,
  className,
  children,
}: {
  onClick?: () => void
  title?: string
  className: string
  children: React.ReactNode
}) {
  useLocale()
  return onClick ? (
    <button type="button" onClick={onClick} title={title} className={className}>
      {children}
    </button>
  ) : (
    <div title={title} className={className}>
      {children}
    </div>
  )
}

/** The second line of a detail node: the address, whole where it fits. */
function Address({ device }: { device: ClientRow }) {
  useLocale()
  const address = device.fixed_ip ?? device.ips?.[0]
  if (!device.seen) return <span className="truncate text-[11px] leading-4 text-muted-foreground italic">{t('never seen')}</span>
  if (!address) return <span className="text-[11px] leading-4 text-muted-foreground">{t('no address')}</span>
  return (
    <span
      className="flex min-w-0 items-center gap-1 text-muted-foreground"
      // The whole address on hover: a narrow node truncates it, and an
      // address is the one field nobody can guess the end of.
      title={device.fixed_ip ? `${address} (fixed)` : address}
    >
      {device.fixed_ip && <Tag className="size-3 shrink-0" aria-label={t('fixed')} />}
      <span className="truncate font-mono text-[11px] leading-4">{address}</span>
    </span>
  )
}

/**
 * A node's figure: both rates when something is moving, and until the second
 * sample the running total — one number, because "↓ 0 bps ↑ 0 bps" before it
 * would claim an idleness nobody measured.
 *
 * When nothing is moving, or the device is away, it is when the router last
 * heard from it: "4m ago", "2h ago". That is one answer for what used to be
 * three words — idle, away, and a dash for a device never counted — and it
 * is the one that says something, since a quiet device that is here and one
 * that left at breakfast are both "not moving".
 */
function ClientTraffic({
  device,
  flow,
  rated,
  counting,
}: {
  device: ClientRow
  flow?: Flow
  rated: boolean
  counting: boolean
}) {
  useLocale()
  const moving = device.online && counting && flow && (!rated || (flow.downRate ?? 0) + (flow.upRate ?? 0) >= 1)
  if (!moving) return <Quiet device={device} />
  if (!rated) return <>{formatBytes(flow.down + flow.up)}</>
  const down = flow.downRate ?? 0
  const up = flow.upRate ?? 0
  // Compact, with the exact figures on hover: the row's width belongs to the
  // device's name, and a rate only has to say "busy" or "quiet" at a glance.
  return (
    <span title={`↓ ${formatRate(down)}  ↑ ${formatRate(up)}`}>
      <span className="text-foreground/75">↓{formatRateCompact(down)}</span>
      <span className="ml-1">↑{formatRateCompact(up)}</span>
    </span>
  )
}

/** Live bit rates without the unit in the narrow list; full units remain on hover. */
function ClientActivity({ device, flow, rated, counting }: {
  device: ClientRow
  flow?: Flow
  rated: boolean
  counting: boolean
}) {
  useLocale()
  const moving = device.online && counting && flow && (!rated || (flow.downRate ?? 0) + (flow.upRate ?? 0) >= 1)
  if (!moving) return <Quiet device={device} />
  if (!rated) return <span title={`${formatBytes(flow.down + flow.up)} transferred`}>{t('active')}</span>
  const down = flow.downRate ?? 0
  const up = flow.upRate ?? 0
  const label = `Live speed: download ${formatRate(down)}, upload ${formatRate(up)}`
  return (
    <span className="inline-flex w-full flex-col align-middle" title={label} aria-label={label}>
      {([['↓', down], ['↑', up]] as const).map(([arrow, rate]) => (
        <span key={arrow} aria-hidden className="flex items-center justify-end gap-0.5 leading-[12px]">
          <span>{arrow}</span><span>{formatRateCompact(rate)}</span>
        </span>
      ))}
    </span>
  )
}

/**
 * When a device was last heard, or the word for why there is no time.
 *
 * The same three words as every other list (features/clients/last-heard) — this
 * node used to add "idle" and "away", which were the map's private names for
 * "we do not know" while the group list said "Offline" about the same device.
 */
function Quiet({ device }: { device: ClientRow }) {
  useLocale()
  return <span className="text-muted-foreground/60" title={lastHeardTitle(device)}>{lastHeardLabel(device)}</span>
}

/* -------------------------------------------------------------------------- */
/* Folds, picks and notes                                                     */
/* -------------------------------------------------------------------------- */

/** A fold counts devices and groups, but not the "Other devices" bucket. */
function describeHidden(h: Hidden, others?: boolean) {
  const devices = h.devices === 1 ? 'device' : 'devices'
  const groups = h.groups === 1 ? 'group' : 'groups'
  if (others) return h.groups > 0 ? `+${h.groups + 1} more` : 'Other devices'
  if (h.devices && h.groups) return `+${h.devices} more ${devices} and ${h.groups} ${groups}`
  if (h.groups) return `+${h.groups} more ${groups}`
  return `+${h.devices} more ${devices}`
}

export function MoreNode({
  variant,
  open,
  hidden,
  others,
  devices,
  onToggle,
}: {
  variant: 'row' | 'card'
  open: boolean
  hidden: Hidden
  others?: boolean
  devices?: number
  onToggle: () => void
}) {
  useLocale()
  const label = open ? 'Show fewer' : describeHidden(hidden, others)
  const Icon = open ? ChevronUp : ChevronDown
  if (variant === 'row') {
    return (
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className={cn(
          'flex size-full items-center justify-center gap-1.5 rounded-lg border border-dashed text-xs font-medium text-muted-foreground transition-colors hover:border-solid hover:bg-card hover:text-foreground',
          focusRing,
        )}
      >
        {label}
        <Icon className="size-3.5" aria-hidden />
      </button>
    )
  }
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-expanded={open}
      className={cn(
        'flex size-full flex-col items-center justify-center gap-0.5 rounded-xl border border-dashed px-3 text-center transition-colors hover:border-solid hover:bg-accent/60',
        focusRing,
      )}
    >
      <span className="flex items-center gap-1 text-[13px] font-medium">
        {label}
        <Icon className="size-3.5 text-muted-foreground" aria-hidden />
      </span>
      {!open && devices !== undefined && (
        <span className="text-xs text-muted-foreground tabular-nums">
          {devices} {devices === 1 ? 'device' : 'devices'}
        </span>
      )}
    </button>
  )
}

/**
 * The groups that did not fit beside a focused one, behind one chip. A menu
 * rather than a fold: opening them in place would push the focused group off
 * the centre it just moved to.
 */
export function PickNode({ groups, onFocus }: { groups: MapGroup[]; onFocus: (key: string) => void }) {
  useLocale()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            className={cn(
              'flex size-full items-center justify-center gap-1 rounded-lg border border-dashed px-3 text-xs font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground',
              focusRing,
            )}
          />
        }
      >
        +{groups.length} more
        <ChevronDown className="size-3.5" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-52">
        {groups.map((g) => (
          <DropdownMenuItem key={g.key} onClick={() => onFocus(g.key)}>
            <span className="min-w-0 flex-1 truncate">{g.title}</span>
            <span className="text-xs text-muted-foreground tabular-nums">{g.total}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function NoteNode({
  note,
  filtering,
  onCreateGroup,
}: {
  note: 'empty' | 'hint' | 'nothing'
  filtering: boolean
  onCreateGroup?: (parent?: string) => void
}) {
  useLocale()
  if (note === 'hint') {
    return (
      <div className="flex size-full flex-col items-start justify-center gap-2 rounded-lg border border-dashed px-3.5 text-xs text-muted-foreground sm:flex-row sm:items-center sm:gap-4">
        <p className="line-clamp-2 min-w-0 flex-1">
          Sort your devices into groups — Serving, Personal, IoT, whatever fits your house — and this map
          is drawn by them.
        </p>
        {onCreateGroup && (
          <Button size="sm" variant="secondary" className="shrink-0" onClick={() => onCreateGroup()}>
            <Plus /> {t('Create a group')} </Button>
        )}
      </div>
    )
  }
  return (
    <p className="flex size-full items-center justify-center text-xs text-muted-foreground">
      {note === 'nothing'
        ? 'No devices match.'
        : filtering
          ? 'Nothing here matches.'
          : 'Nothing in it yet. Use a device’s menu to move it here.'}
    </p>
  )
}

/* -------------------------------------------------------------------------- */
/* Numbers                                                                    */
/* -------------------------------------------------------------------------- */

/**
 * ↓ and ↑ as rates. Down first: it is the number people mean by "busy".
 *
 * `neutral` drops the arrows' colour, for the map, where no colour is spent on
 * anything that is not a fault. The stat tile above the map keeps them.
 */
export function Rates({ flow, className, neutral }: { flow: Flow; className?: string; neutral?: boolean }) {
  useLocale()
  return (
    <span className={cn('inline-flex shrink-0 items-center gap-2 tabular-nums', className)}>
      <span className="inline-flex items-center gap-0.5">
        <ArrowDown
          className={cn('size-3', neutral ? 'text-muted-foreground' : 'text-receive')}
          aria-label="down"
        />
        {formatRate(flow.downRate ?? 0)}
      </span>
      <span className="inline-flex items-center gap-0.5 text-muted-foreground">
        <ArrowUp
          className={cn('size-3', neutral ? 'text-muted-foreground' : 'text-transmit')}
          aria-label="up"
        />
        {formatRate(flow.upRate ?? 0)}
      </span>
    </span>
  )
}

const focusRing = 'outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring'
