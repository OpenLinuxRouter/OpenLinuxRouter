import { AnimatePresence, motion, useReducedMotion, type Transition } from 'motion/react'
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router'

import { Skeleton } from '@/components/ui/skeleton'
import { useInterfaces } from '@/features/link/queries'
import type { ClientRank } from '@/features/topology/device-rank'
import { layout, type Box, type Density, type Item, type Layout } from '@/features/topology/layout'
import { Links } from '@/features/topology/links'
import { buildTree } from '@/features/topology/model'
import {
  ClientNode,
  DialInNode,
  GroupNode,
  MoreNode,
  NoteNode,
  PickNode,
  RouterNode,
  type MapActions,
  type NetworkInfo,
} from '@/features/topology/nodes'
import type { DialIn } from '@/features/topology/dial-in'
import { magnitude, type TrafficView } from '@/features/topology/traffic'
import type { AssignmentStatus, ClientRow, ExitStatus, NetworkRow } from '@/lib/api-types'
import type { ClientsGroup, Pool, Service } from '@/lib/config-types'

/**
 * The network, drawn top-down: what reached this router from outside, the
 * router, then the operator's groups as lists of devices and nested groups.
 *
 * What is outside is the devices that dialed in over WireGuard — the one kind
 * of box that is not on a network this router serves and connects to it
 * directly. A peer is there rather than under the router because everything
 * under the router hangs off a group, and a card of its own down there would
 * put every group's line through it (docs/remote-access.md §3.4). The default
 * routes observed in the kernel used to be drawn beside them, one card each,
 * and that was a row above the router for things that are not boxes in it: a
 * next hop is one line of Link's status, and the modem it names belongs in its
 * group like any other device.
 *
 * It is organised by *group*, not by network. A network is how a device is
 * attached; a group is what the operator thinks it is for ("Serving",
 * "Personal", "IoT"), and the second is what somebody looking at their house
 * wants organised. The network did not disappear: it is a chip on every detail
 * node, it is a filter above the map, and the router carries each one with the
 * way it goes out.
 *
 * **One layout that adapts, not modes.** The same picture has to hold a box
 * with no groups, one with two, one with fifteen, groups inside groups, a
 * group of three and a group of a hundred. It does that with one rule applied
 * at every level — show at most K, busiest first, fold the rest into "+N
 * more" that opens in place — and with every box sized by a scoring rule
 * rather than by whatever its content pushes it to. layout.ts has both.
 *
 * Clicking a named group's header opens its detail page. The unnamed device
 * bucket keeps its in-place focus or fold because it has no group detail page.
 *
 * **Strictly top-down.** Every line leaves the bottom of one box and enters the
 * top of another. Nothing is drawn left to right, because a diagram that mixes
 * the two makes the reader stop to work out which axis means "is inside"; and
 * no line passes between two boxes to reach a third, because that is the line
 * nobody can follow.
 *
 * What it does not claim, still: this is logical structure. "In Serving" says
 * what the operator decided, not what is plugged into what.
 */
export function NetworkMap({
  devices: all,
  services = new Map(),
  serviceDomain,
  serviceDomainKnown,
  groups = [],
  traffic,
  assignments,
  exits,
  dialIn,
  pools,
  pending,
  filter = '',
  network = '',
  density,
  onDensity,
  ...actions
}: {
  devices: ClientRow[]
  services?: Map<string, Service[]>
  serviceDomain?: string
  /** Whether that suffix has been answered for, or is still on its way. */
  serviceDomainKnown?: boolean
  groups?: ClientsGroup[]
  traffic: TrafficView
  assignments?: AssignmentStatus[]
  exits?: ExitStatus[]
  /** Clients dialed in over WireGuard, when the tunnel has any. */
  dialIn?: DialIn
  pools?: Pool[]
  pending: boolean
  filter?: string
  /** A network name, NO_NETWORK, or empty for every network. */
  network?: string
  /**
   * The operator's choice, or 'auto': detail when the detail layout shows
   * everything comfortably at this width, compact when it does not.
   */
  density: Density | 'auto'
  /** Told the density actually drawn, so a toggle can show it. */
  onDensity?: (density: Density) => void
} & MapActions) {
  // The networks, for joining a pool (keyed by network) to a gateway assignment
  // (keyed by interface). Read here rather than passed in because every caller
  // of this component would otherwise have to fetch it only to hand it back.
  const interfaces = useInterfaces()
  const networkRows = interfaces.data?.networks
  const networks = useMemo(
    () => buildNetworks(all, assignments, exits, pools, networkRows),
    [all, assignments, exits, pools, networkRows],
  )

  if (pending) {
    return (
      <div className="flex flex-col items-center gap-12">
        <Skeleton className="h-16 w-60 rounded-2xl" />
        <div className="flex w-full justify-center gap-4">
          <Skeleton className="h-56 w-full max-w-72 rounded-xl" />
          <Skeleton className="hidden h-56 w-72 rounded-xl sm:block" />
          <Skeleton className="hidden h-56 w-72 rounded-xl sm:block" />
        </div>
      </div>
    )
  }

  return (
    <Canvas
      devices={all}
      services={services}
      serviceDomain={serviceDomain}
      serviceDomainKnown={serviceDomainKnown}
      groups={groups}
      traffic={traffic}
      networks={networks}
      dialIn={dialIn}
      filter={filter}
      network={network}
      density={density}
      onDensity={onDensity}
      actions={actions}
    />
  )
}

/**
 * The same curve for every box and every line, over the same time. A line's
 * control points are affine in its two end boxes' coordinates, so
 * interpolating the path and interpolating the boxes with one easing keeps the
 * line's ends on the boxes through the whole move — and through an interrupted
 * one, since both restart from where they are. That is the whole of how lines
 * follow moving nodes here: nothing is measured while anything moves.
 *
 * A tween rather than a spring for exactly that reason. Springs of equal
 * settings still diverge the moment one is interrupted with velocity and
 * another is not, and a line that lags its box by a few pixels reads as
 * broken, not as lively.
 */
const MOVE: Transition = { type: 'tween', ease: [0.22, 1, 0.36, 1], duration: 0.32 }
const STILL: Transition = { duration: 0 }

function Canvas({
  devices: all,
  services,
  serviceDomain,
  serviceDomainKnown,
  groups,
  traffic,
  networks,
  dialIn,
  filter,
  network,
  density: wanted,
  onDensity,
  actions,
}: {
  devices: ClientRow[]
  services: Map<string, Service[]>
  serviceDomain?: string
  serviceDomainKnown?: boolean
  groups: ClientsGroup[]
  traffic: TrafficView
  networks: NetworkInfo[]
  dialIn?: DialIn
  filter: string
  network: string
  density: Density | 'auto'
  onDensity?: (density: Density) => void
  actions: MapActions
}) {
  const navigate = useNavigate()
  // Every device is drawn where it belongs. The box a default route points at
  // — the ISP modem, typically — used to be taken out of its group so it could
  // be drawn above the router as a next hop, which meant the one device that
  // was definitely on this network was the one the map did not list.
  // Sample only sort keys. Live rates and device data keep refreshing normally.
  const [frozen, setFrozen] = useState<Map<string, ClientRank>>(() =>
    buildTree({ devices: all, groups, traffic, filter, network }).ranks,
  )
  const interacting = useRef({ pointer: false, focus: false, touch: false })
  const tree = useMemo(
    () => buildTree({ devices: all, groups, traffic, filter, network, frozen }),
    [all, groups, traffic, filter, network, frozen],
  )
  const latestRanks = useRef(tree.ranks)
  // Initialize when the first devices arrive after an empty/loading map.
  if (!frozen.size && tree.ranks.size) setFrozen(tree.ranks)
  useLayoutEffect(() => {
    latestRanks.current = tree.ranks
  }, [tree.ranks])
  useEffect(() => {
    const releaseTouch = () => { interacting.current.touch = false }
    window.addEventListener('pointerup', releaseTouch)
    window.addEventListener('pointercancel', releaseTouch)
    const timer = window.setInterval(() => {
      const { pointer, focus, touch } = interacting.current
      if (!pointer && !focus && !touch) setFrozen(latestRanks.current)
    }, 5000)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('pointerup', releaseTouch)
      window.removeEventListener('pointercancel', releaseTouch)
    }
  }, [])

  // Bars are scaled against the busiest device on the whole map, not the busiest
  // in the group and not the busiest that survived the search box: a bar has to
  // mean the same length everywhere on screen, and it should not grow because
  // the operator typed something.
  const busiest = useMemo(
    () => Math.max(0, ...all.map((d) => magnitude(traffic.flowOf(d), traffic.rated))),
    [all, traffic],
  )

  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set())
  const [focus, setFocus] = useState<string | undefined>()
  const toggle = useCallback((key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }, [])

  // The two things the browser is asked: how wide the map is, and how big the
  // router came out (its network chips wrap). Read synchronously before the
  // first paint and again whenever either changes.
  const wrap = useRef<HTMLDivElement>(null)
  const routerRef = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  const [routerSize, setRouterSize] = useState({ w: 240, h: 64 })
  const measure = useCallback(() => {
    if (wrap.current) setWidth(wrap.current.clientWidth)
    const r = routerRef.current
    if (r) {
      setRouterSize((prev) =>
        prev.w === r.offsetWidth && prev.h === r.offsetHeight ? prev : { w: r.offsetWidth, h: r.offsetHeight },
      )
    }
  }, [])
  useLayoutEffect(() => {
    measure()
    const observer = new ResizeObserver(measure)
    if (wrap.current) observer.observe(wrap.current)
    if (routerRef.current) observer.observe(routerRef.current)
    return () => observer.disconnect()
  }, [measure])

  // Nothing animates until the first real layout has been painted: the first
  // render has no width to lay out against, and everything gliding in from
  // the top-left corner on page load would be motion that means nothing.
  const reduced = useReducedMotion()
  const [settled, setSettled] = useState(false)
  useEffect(() => {
    if (width === 0 || settled) return
    const id = requestAnimationFrame(() => setSettled(true))
    return () => cancelAnimationFrame(id)
  }, [width, settled])
  const transition = settled && !reduced ? MOVE : STILL

  // Keys only: whether a peer is connected changes on every poll, and the map
  // should move when the set of peers changes rather than when one wakes up.
  const dialInInput = useMemo(
    () => (dialIn ? { peers: dialIn.peers.map((p) => ({ key: p.name })) } : undefined),
    [dialIn],
  )

  const [geo, density] = useMemo((): [Layout | null, Density] => {
    if (width === 0) return [null, wanted === 'auto' ? 'detail' : wanted]
    const at = (d: Density, f = focus) =>
      layout({
        tree,
        width,
        density: d,
        router: routerSize,
        expanded,
        focus: f,
        counting: traffic.counting,
        rated: traffic.rated,
        dialIn: dialInInput,
        serviceDevices: new Set(services.keys()),
      })
    if (wanted !== 'auto') return [at(wanted), wanted]
    // Decided on the unfocused map. Opening a "+N more" does not count against detail either: it is the
    // operator asking for more, not the map failing to fit.
    // Compact on a phone, and while a group is focused: detail is the
    // denser read, and on a narrow screen it turns the map into a column of
    // small print, while a focused group wants as many of its devices as
    // possible on the one row its lines can reach.
    const whole = at('detail', undefined)
    const d: Density = !whole.narrow && !focus && (whole.fits || expanded.size > 0) ? 'detail' : 'compact'
    return [d === 'detail' ? whole : at(d), d]
  }, [tree, width, wanted, routerSize, expanded, focus, traffic.counting, traffic.rated, dialInInput, services])

  useEffect(() => {
    onDensity?.(density)
  }, [density, onDensity])
  const narrow = geo?.narrow ?? false
  const focused = !narrow && focus !== undefined && tree.byKey.has(focus) ? focus : undefined

  // Esc leaves focus, from anywhere on the page: the map is what the page is
  // about, and a key that only worked with the map's own element focused would
  // not work after clicking a header, which is exactly when it is wanted.
  useEffect(() => {
    if (!focused) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !e.defaultPrevented) setFocus(undefined)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [focused])

  const onGroup = (key: string) => {
    const group = tree.byKey.get(key)
    if (group?.name !== undefined) {
      navigate(`/groups/${encodeURIComponent(group.name)}`)
      return
    }
    if (narrow) toggle(key)
    else setFocus((prev) => (prev === key ? undefined : key))
  }

  const render = (item: Item) => {
    switch (item.kind) {
      case 'group':
        return (
          <GroupNode
            group={item.group}
            variant={item.variant}
            nested={item.nested}
            dashed={item.group.name === undefined && tree.grouped}
            groups={groups}
            actions={actions}
            onFocus={() => onGroup(item.key)}
          />
        )
      case 'device':
        return (
          <ClientNode
            device={item.device}
            services={services.get(item.device.mac)}
            serviceDomain={serviceDomain}
            serviceDomainKnown={serviceDomainKnown}
            density={density}
            row={item.row}
            traffic={traffic}
            busiest={busiest}
            actions={actions}
          />
        )
      case 'more':
        return (
          <MoreNode
            variant={item.variant}
            open={item.open}
            hidden={item.hidden}
            others={item.others}
            devices={item.devices}
            onToggle={() => toggle(item.target)}
          />
        )
      case 'pick':
        return <PickNode groups={item.groups} onFocus={onGroup} />
      case 'note':
        return <NoteNode note={item.note} filtering={tree.filtering} onCreateGroup={actions.onCreateGroup} />
      case 'dialin': {
        const peer = dialIn?.peers.find((p) => `p:${p.name}` === item.key)
        return peer && dialIn
          ? <DialInNode peer={peer} iface={dialIn.interface} onOpen={() => actions.onOpenDialIn?.(peer)} />
          : null
      }

    }
  }

  return (
    <div
      ref={wrap}
      className="relative"
      onPointerEnter={(e) => { if (e.pointerType !== 'touch') interacting.current.pointer = true }}
      onPointerLeave={() => { interacting.current.pointer = false }}
      onPointerDown={(e) => {
        if (e.pointerType === 'touch') interacting.current.touch = true
      }}
      onFocusCapture={() => { interacting.current.focus = true }}
      onBlurCapture={(e) => {
        interacting.current.focus = e.currentTarget.contains(e.relatedTarget)
      }}
      onClick={(e) => {
        // Clicking empty space leaves focus. Anything that is a node is not
        // empty space, including the router.
        if (focused && !(e.target as Element).closest('[data-map-node]')) setFocus(undefined)
      }}
    >
      <motion.div
        className="relative"
        initial={false}
        style={{ height: geo?.height ?? 160 }}
        transition={transition}
      >
        {geo && <Links links={geo.links} rail={geo.rail} transition={transition} />}

        <motion.div
          ref={routerRef}
          data-map-node
          className="absolute top-0 left-0 z-10"
          style={{ maxWidth: width || undefined }}
          initial={false}
          animate={{ x: geo?.router.x ?? 0, y: geo?.router.y ?? 0, opacity: geo ? 1 : 0 }}
          transition={transition}
        >
          <RouterNode networks={networks} traffic={traffic} />
        </motion.div>

        {geo && (
          <AnimatePresence initial={false}>
            {geo.items.map((item) => (
              <Placed key={item.key} box={item.box} transition={transition}>
                {render(item)}
              </Placed>
            ))}
          </AnimatePresence>
        )}
      </motion.div>
    </div>
  )
}

/**
 * One box on the map, at the position and size the layout gave it.
 *
 * Arriving boxes fade in where they will stay rather than flying in from
 * somewhere; departing ones fade where they are, faster than things move, so
 * the eye follows what is going somewhere rather than what is leaving.
 */
function Placed({ box, transition, children }: { box: Box; transition: Transition; children: React.ReactNode }) {
  const at = { x: box.x, y: box.y, width: box.w, height: box.h }
  return (
    <motion.div
      data-map-node
      className="absolute top-0 left-0"
      initial={{ ...at, opacity: 0, scale: 0.97 }}
      animate={{ ...at, opacity: 1, scale: 1 }}
      exit={{ opacity: 0, scale: 0.97, transition: { duration: 0.15 } }}
      transition={transition}
    >
      {children}
    </motion.div>
  )
}

/**
 * Every network, with where it goes out.
 *
 * Networks are the union of what link declares, what gateway routes, what dhcp
 * serves and what devices were seen on: any one alone drops a network the
 * others know about. Everything is keyed by *network name*; gateway still names
 * a kernel interface, so its assignments are translated through the network's
 * members on the way in — the one place the two vocabularies meet, and it has
 * to be exactly one place: two chips for one network, called `lan` and
 * `bridge0`, is the bug this shape prevents.
 */
function buildNetworks(
  devices: ClientRow[],
  assignments?: AssignmentStatus[],
  exits?: ExitStatus[],
  pools?: Pool[],
  networks?: NetworkRow[],
): NetworkInfo[] {
  const networkOfInterface = new Map(
    (networks ?? []).flatMap((n) => n.members.map((m) => [m, n.name] as const)),
  )
  const assignmentOf = new Map(
    (assignments ?? []).map((a) => [networkOfInterface.get(a.interface) ?? a.interface, a] as const),
  )

  const names = new Set<string>()
  for (const n of networks ?? []) names.add(n.name)
  for (const name of assignmentOf.keys()) names.add(name)
  for (const p of pools ?? []) names.add(p.network)
  for (const d of devices) if (d.network) names.add(d.network)

  const count = (name: string) => devices.filter((d) => d.network === name).length
  return [...names]
    .map((name) => {
      const assignment = assignmentOf.get(name)
      const exit = assignment?.exit || ''
      const status = exit ? exits?.find((e) => e.name === exit) : undefined
      return {
        name,
        exit,
        inherited: assignment?.source === 'default',
        down: Boolean(status && status.probed && !status.up),
        unchecked: Boolean(exit) && !status?.probed,
      }
    })
    .sort((a, b) => count(b.name) - count(a.name) || a.name.localeCompare(b.name))
}
