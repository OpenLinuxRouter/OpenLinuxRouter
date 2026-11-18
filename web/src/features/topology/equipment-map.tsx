import { useLocale, t } from '@/lib/i18n'
import { RefreshCw } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router'

import { Button } from '@/components/ui/button'
import { Heading } from '@/components/ui/heading'
import { Skeleton } from '@/components/ui/skeleton'

import { useClientList } from '@/features/clients/queries'
import { useQueries } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useDialStatus } from '@/features/dial/queries'
import { deviceKeys, useDevicesConfig } from '@/features/devices/queries'
import type { Device, Snapshot } from '@/features/devices/types'
import { useInterfaces } from '@/features/link/queries'

import { isPhysical } from './equipment'
import { useEquipmentReads } from './equipment-reads'
import { placeMap, type PlacedNode } from './map-layout'
import { hostOf } from './placement'
import { panelGeometry, type PanelGeometry, type PanelPort } from './panel'
import { PortPanel, headWidth } from './port-panel'
import { useAllSweeps } from './sweep'
import { findUpstream } from './upstream'
import { SELF, wire, type Wire, type WireKind } from './wiring'

/**
 * The boxes and the cables between them, laid out as a tree from this router:
 * the modem above it, the switch under it, the access points under the switch,
 * the clients under whichever box is nearest to them.
 *
 * The unit is a port, not a box: a cable leaves the port a switch named and
 * lands on whatever that port learned. Where the switch named a port and the
 * far end is a box, the cable ends on the box, not on one of its sockets — the
 * switch knows the box, not which port the wire is in. Wireless associations
 * are dashed and belong to the box. How each cable is inferred is in wiring.ts.
 *
 * The map draws at its natural size and only shrinks to fit down to a scale
 * where the names stay readable; past that it scrolls sideways instead.
 */
const CHIP_W = 138
const CHIP_H = 34
const MORE_W = 92
/** Under this the 11px names fall below legible, so the map scrolls instead. */
const MIN_SCALE = 0.8
/** A box with more clients than this shows the first few and a "+N" to open the rest. */
const FOLD_OVER = 4
const FOLD_SHOW = 3

interface Box {
  id: string
  kind: 'self' | 'device' | 'client' | 'more'
  name: string
  detail?: string
  ports: PanelPort[]
  /** Where the panel's ports are; absent for a chip. */
  geometry?: PanelGeometry
  width: number
  height: number
  href?: string
  /** For a "+N" chip: the box it opens. */
  parent?: string
}

export function EquipmentMap({ highlight }: { highlight?: string }) {
  useLocale()
  const reads = useEquipmentReads()
  const config = useDevicesConfig()
  const clients = useClientList()
  const interfaces = useInterfaces()
  const dial = useDialStatus()
  const sweeps = useAllSweeps()
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set())
  const devices = useMemo(() => config.data?.devices ?? [], [config.data])
  const ids = useMemo(() => devices.map(device => device.id), [devices])
  // Observers, not fetches: the page reads a box once on arrival, and the map
  // draws whatever that read has already put in the cache.
  const observations = useQueries({
    queries: ids.map(id => ({
      queryKey: deviceKeys.snapshot(id),
      queryFn: () => api.get<Snapshot>(`/api/devices/${encodeURIComponent(id)}/snapshot`),
      enabled: false,
      gcTime: Infinity,
    })),
  })
  const snapshots = useMemo(() => {
    const map = new Map<string, Snapshot>()
    observations.forEach((observation, index) => {
      if (observation.data !== undefined) map.set(ids[index], observation.data)
    })
    return map
  }, [observations, ids])
  const byMac = useMemo(
    () => new Map((clients.data?.clients ?? []).map(client => [client.mac.toLowerCase(), client])),
    [clients.data],
  )

  // The router is a node like any other, and the one the map is rooted at. Its
  // ports are its own interfaces — the only box on this map whose socket names
  // we hold first-hand.
  const selfFaces: PanelPort[] = useMemo(
    () => (interfaces.data?.interfaces ?? [])
      .filter(row => row.mac !== undefined && row.mac !== '' && row.physical && !row.loopback)
      .map(row => ({ id: row.name, kind: 'ethernet', linkUp: row.running, speedMbps: row.speed_mbps })),
    [interfaces.data],
  )

  // Upstream: a box between this router and the internet goes above it. The
  // rule — a private way out, or a bridged ONT off the LAN — is in upstream.ts.
  const uplink = useMemo(() => findUpstream({
    devices,
    route: dial.data?.route,
    uplinkInterface: dial.data?.uplink?.interface,
    lan: (interfaces.data?.networks ?? []).map(network => network.subnet).filter((subnet): subnet is string => subnet !== undefined),
    ports: selfFaces.map(face => face.id),
  }), [dial.data, devices, interfaces.data, selfFaces])

  const wiring = useMemo(() => wire({
    devices: ids,
    sweeps: new Map([...sweeps].map(([id, sweep]) => [id, sweep.byPort])),
    associations: new Map([...snapshots].map(([id, snapshot]) => [id, (snapshot.associations ?? []).map(a => a.mac.toLowerCase())])),
    uplink,
  }), [ids, sweeps, snapshots, uplink])

  const boxes = useMemo(() => {
    // This router by the address its LAN knows it by.
    const lanAddress = (interfaces.data?.interfaces ?? [])
      .find(row => row.network !== undefined && row.address !== undefined)?.address?.split('/')[0]
    const self: Box = { ...sized(SELF, 'Open Linux Router', selfFaces, '/devices', lanAddress), kind: 'self' }
    const rest = devices.map(device => sized(
      device.id,
      device.name || device.id,
      portsOf(snapshots.get(device.id)),
      `/devices/${encodeURIComponent(device.id)}`,
      hostOfDevice(device),
    ))
    return [self, ...rest]
  }, [devices, snapshots, selfFaces, interfaces.data])

  // Clients, folded per box: a box with many of them shows the first few, in
  // port order, and a chip that opens the rest.
  const { terminals, wires } = useMemo(() => {
    const terminals: Box[] = []
    const wires: Wire[] = wiring.wires.filter(w => !wiring.clients.has(w.to))
    const byParent = new Map<string, Wire[]>()
    for (const w of wiring.wires) {
      if (!wiring.clients.has(w.to)) continue
      byParent.set(w.from, [...(byParent.get(w.from) ?? []), w])
    }
    for (const [parent, list] of byParent) {
      const named = list.map(w => {
        const end = wiring.clients.get(w.to)
        const mac = end?.mac ?? w.to.slice(4)
        const row = byMac.get(mac)
        return { w, mac, name: row?.name || (end?.name !== mac ? end?.name : undefined) || mac, detail: row?.ips?.[0] ?? end?.detail }
      }).sort((a, b) =>
        (a.w.fromPort ?? '').localeCompare(b.w.fromPort ?? '', undefined, { numeric: true }) || a.name.localeCompare(b.name))
      const folded = named.length > FOLD_OVER && !open.has(parent)
      for (const entry of folded ? named.slice(0, FOLD_SHOW) : named) {
        terminals.push({
          id: entry.w.to,
          kind: 'client',
          name: entry.name,
          detail: entry.detail,
          ports: [],
          width: CHIP_W,
          height: CHIP_H,
          href: `/clients/${encodeURIComponent(entry.mac)}`,
        })
        wires.push(entry.w)
      }
      if (folded) {
        const id = `more:${parent}`
        const hidden = named.slice(FOLD_SHOW)
        terminals.push({ id, kind: 'more', name: `+${hidden.length}`, ports: [], width: MORE_W, height: CHIP_H, parent })
        wires.push({ from: parent, to: id, kind: hidden.every(entry => entry.w.kind === 'wireless') ? 'wireless' : 'wired' })
      }
    }
    return { terminals, wires }
  }, [wiring, byMac, open])

  const all = useMemo(() => [...boxes, ...terminals], [boxes, terminals])
  const layout = useMemo(
    () => placeMap(
      all.map(box => ({ id: box.id, width: box.width, height: box.height })),
      wires.map(w => ({ from: w.from, to: w.to, fromPort: w.fromPort, toPort: w.toPort, dashed: w.kind !== 'wired' })),
      SELF,
      {},
      wiring.above,
      new Set(terminals.map(box => box.id)),
    ),
    [all, wires, wiring.above, terminals],
  )
  const byId = useMemo(() => new Map(all.map(box => [box.id, box])), [all])
  const placed = useMemo(() => {
    const map = new Map<string, { node: PlacedNode; box: Box }>()
    for (const node of layout.nodes) {
      const box = byId.get(node.id)
      if (box !== undefined) map.set(node.id, { node, box })
    }
    return map
  }, [layout, byId])

  const frame = useRef<HTMLDivElement>(null)
  const [scale, setScale] = useState(1)
  useEffect(() => {
    const element = frame.current
    if (element === null) return
    const measure = () => setScale(Math.max(MIN_SCALE, Math.min(1, element.clientWidth / Math.max(1, layout.width))))
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [layout.width])

  const asking = [...sweeps.values()].filter(sweep => sweep.partial === true)

  const highlighted = useRef<HTMLDivElement | null>(null)
  useEffect(() => {
    highlighted.current?.scrollIntoView({ block: 'nearest', inline: 'center' })
  }, [highlight, layout])

  if (config.isError) {
    return (
      <p className="rounded-xl border border-border bg-card px-4 py-3 text-sm text-muted-foreground">
        {t('The device inventory could not be read, so no box can be placed here.')}
      </p>
    )
  }
  if (config.isPending) return <Skeleton className="h-64 w-full rounded-xl" />
  if (devices.length === 0) return <Empty />

  const failures = Object.keys(reads.failed).length
  return (
    <div className="relative rounded-xl border border-border bg-card">
      <div className="flex min-h-11 items-center gap-3 border-b border-border/60 px-3 py-1.5 text-xs text-muted-foreground">
        <span className="min-w-0 flex-1 truncate">
          {reads.reading !== undefined
            ? t('Reading {name}…', { name: devices.find(device => device.id === reads.reading)?.name || reads.reading })
            : asking.length > 0
              ? t('Finding which port each device is on… {done} of {total}', {
                done: asking.reduce((sum, sweep) => sum + sweep.asked, 0),
                total: asking.reduce((sum, sweep) => sum + (sweep.total ?? sweep.asked), 0),
              })
              : failures > 0
                ? t('{count} devices did not answer', { count: failures })
                : t('Cables are inferred from what each switch and access point reports.')}
        </span>
        <Button
          variant="quiet"
          size="sm"
          className="shrink-0"
          disabled={!reads.readsDone || asking.length > 0}
          onClick={reads.rescan}
        >
          <RefreshCw aria-hidden className="size-3.5" />
          {t('Ask again')}
        </Button>
      </div>
      <div ref={frame} className="w-full overflow-x-auto">
        {/* Sized to the scaled drawing, so centring and scrolling see its real width. */}
        <div className="mx-auto" style={{ width: layout.width * scale, height: layout.height * scale }}>
          <div className="relative origin-top-left" style={{ width: layout.width, height: layout.height, transform: `scale(${scale})` }}>
            <svg className="absolute inset-0 overflow-visible" width={layout.width} height={layout.height} aria-hidden>
              {wires.map(w => {
                const a = placed.get(w.from)
                const b = placed.get(w.to)
                if (a === undefined || b === undefined) return null
                // The cable runs from the upper box down to the lower one,
                // whichever end the evidence came from.
                const [upper, upperPort, lower, lowerPort] = a.node.layer <= b.node.layer
                  ? [a, w.fromPort, b, w.toPort]
                  : [b, w.toPort, a, w.fromPort]
                return (
                  <Cable
                    key={`${w.from}-${w.fromPort ?? ''}-${w.to}-${w.toPort ?? ''}`}
                    from={anchorOf(upper, upperPort, 'bottom')}
                    to={anchorOf(lower, lowerPort, 'top')}
                    lanes={layout.lanes.get(`${w.from}>${w.to}`)}
                    kind={w.kind}
                  />
                )
              })}
            </svg>

            {[...placed.values()].map(({ node, box }) => (
              <div
                key={node.id}
                ref={node.id === highlight ? highlighted : undefined}
                className={`absolute ${node.id === highlight ? 'rounded-lg ring-2 ring-ring/60 ring-offset-4 ring-offset-card' : ''}`}
                style={{ left: node.x, top: node.y, width: node.width }}
              >
                {box.kind === 'client' ? (
                  <Chip box={box} />
                ) : box.kind === 'more' ? (
                  <button
                    type="button"
                    onClick={() => setOpen(current => new Set([...current, box.parent ?? '']))}
                    className="flex w-full items-center justify-center rounded-full border border-dashed border-border bg-card text-xs text-muted-foreground hover:bg-accent/40"
                    style={{ height: box.height }}
                    title={t('Show all')}
                  >
                    {box.name}
                  </button>
                ) : (
                  <Node box={box} failed={reads.failed[box.id]} />
                )}
              </div>
            ))}
          </div>
        </div>
      </div>
      <Legend />
    </div>
  )
}

function Legend() {
  useLocale()
  const item = (kind: WireKind, label: string) => (
    <span className="flex items-center gap-1.5">
      <svg width="22" height="6" aria-hidden><line x1="1" y1="3" x2="21" y2="3" {...cableStyle(kind)} /></svg>
      {label}
    </span>
  )
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1 border-t border-border/60 px-3 py-2 text-[11px] text-muted-foreground">
      {item('wired', t('Cable'))}
      {item('wireless', t('Wireless'))}
      {item('unknown', t('Not placed yet'))}
      <span className="basis-full sm:ml-auto sm:basis-auto">
        {t('Port lamps: left of the latch is the link, right is the speed — green at a gigabit and up, amber below. A fiber port has one lamp, for the link.')}
      </span>
    </div>
  )
}

/** A box with its face, its caption, and the ports' names underneath. */
function Node({ box, failed }: { box: Box; failed?: string }) {
  useLocale()
  if (box.geometry === undefined) return null
  return (
    <PortPanel
      name={<Link to={box.href ?? '#'} className="underline-offset-4 hover:underline">{box.name}</Link>}
      detail={box.detail}
      ports={box.ports}
      geometry={box.geometry}
      empty={failed !== undefined ? t('Did not answer') : t('Not read yet')}
      title={failed}
    />
  )
}

function Chip({ box }: { box: Box }) {
  return (
    <Link
      to={box.href ?? '#'}
      className="flex flex-col justify-center rounded-full border border-border bg-card px-3 text-center text-[11px] leading-4 hover:bg-accent/40"
      style={{ height: box.height }}
      title={box.detail === undefined ? box.name : `${box.name} · ${box.detail}`}
    >
      <span className="truncate">{box.name}</span>
      {box.detail !== undefined && <span className="truncate text-[10px] leading-3 text-muted-foreground">{box.detail}</span>}
    </Link>
  )
}

function sized(
  id: string,
  name: string,
  ports: PanelPort[],
  href?: string,
  detail?: string,
): Box {
  const up = ports.filter(port => port.linkUp === true).length
  const stat = ports.length > 0 ? t('{up} of {total} up', { up, total: ports.length }) : undefined
  const geometry = panelGeometry(ports, headWidth(name, detail, stat))
  return { id, kind: 'device', name, detail, ports, geometry, width: geometry.width, height: geometry.height, href }
}

/**
 * Where a cable leaves or enters a box. Into the lower box at its port, behind
 * the chassis, so it seems to plug in from above; out of the upper box under
 * its port's column, from the bottom edge. A box with no named port: the
 * middle of the edge.
 */
function anchorOf(
  entry: { node: PlacedNode; box: Box },
  port: string | undefined,
  side: 'top' | 'bottom',
): { x: number; y: number } {
  const point = port === undefined ? undefined : entry.box.geometry?.ports.find(candidate => candidate.id === port)
  const x = entry.node.x + (point?.x ?? entry.node.width / 2)
  if (side === 'bottom') return { x, y: entry.node.y + entry.node.height }
  return { x, y: entry.node.y + (point?.y ?? 0) }
}

function cableStyle(kind: WireKind) {
  if (kind === 'wired') return { stroke: '#7d95ad', strokeWidth: 1.8 }
  if (kind === 'wireless') return { stroke: '#a9b8c7', strokeWidth: 1.4, strokeDasharray: '5 4' }
  return { stroke: '#c9d1da', strokeWidth: 1.4, strokeDasharray: '1.5 4', strokeLinecap: 'round' as const }
}

/**
 * One cable: a smooth S between two points, and where it skips rows, straight
 * down each lane the layout kept for it, with an S between lanes.
 */
function Cable({ from, to, lanes = [], kind }: {
  from: { x: number; y: number }
  to: { x: number; y: number }
  lanes?: readonly { x: number; top: number; bottom: number }[]
  kind: WireKind
}) {
  const s = (a: { x: number; y: number }, b: { x: number; y: number }) => {
    const dy = Math.max(18, Math.abs(b.y - a.y) * 0.5)
    return `C${a.x} ${a.y + dy} ${b.x} ${b.y - dy} ${b.x} ${b.y}`
  }
  let d = `M${from.x} ${from.y}`
  let at = from
  for (const lane of lanes) {
    d += ` ${s(at, { x: lane.x, y: lane.top })} L${lane.x} ${lane.bottom}`
    at = { x: lane.x, y: lane.bottom }
  }
  d += ` ${s(at, to)}`
  return <path d={d} fill="none" {...cableStyle(kind)} />
}

function portsOf(snapshot: Snapshot | undefined): PanelPort[] {
  // Only holes a cable goes into: a bridge, a VLAN or a radio is not one.
  return (snapshot?.ports ?? []).filter(isPhysical).map(port => ({
    id: port.id,
    name: port.name,
    kind: port.kind,
    linkUp: port.link_up,
    speedMbps: port.speed_mbps,
  }))
}

function hostOfDevice(device: Device): string | undefined {
  return hostOf(device.address.trim())
}

/**
 * Nobody has added a box, and the page has nothing to read. It is the one place
 * in this picture where a person can make it better, so it says what adding one
 * buys instead of showing an empty frame.
 */
function Empty() {
  useLocale()
  return (
    <div className="rounded-xl border border-dashed border-border px-4 py-5">
      <Heading as="h2" size="sm">{t('No boxes yet')}</Heading>
      <p className="mt-1 max-w-prose text-xs leading-5 text-muted-foreground">
        {t('Add a switch or an access point on Devices and this page will read it, place it, and list the clients it reports.')}
      </p>
      <p className="mt-3 text-xs">
        <Link to="/devices" className="font-medium underline-offset-4 hover:underline">{t('Add a device')}</Link>
      </p>
    </div>
  )
}
