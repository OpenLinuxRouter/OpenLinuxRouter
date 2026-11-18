import { useLocale, t } from '@/lib/i18n'
import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router'

import { useClientList } from '@/features/clients/queries'
import { useQueries } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useDialStatus } from '@/features/dial/queries'
import { deviceKeys, useDevicesConfig } from '@/features/devices/queries'
import type { Device, Snapshot } from '@/features/devices/types'
import { useInterfaces } from '@/features/link/queries'

import { DeviceFace } from './device-face'
import type { FacePort } from './face'
import { formOf, type DeviceForm } from './form'
import { placeMap, type PlacedNode } from './map-layout'
import { hostOf } from './placement'
import { faceGeometry, naturalWidth } from './port-points'
import { useAllSweeps } from './sweep'
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
const PORT_WIDTH = 44
const MAX_NODE_WIDTH = 430
const CHIP_W = 138
const CHIP_H = 34
const MORE_W = 92
const CAPTION_H = 22
const UNREAD_H = 58
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
  form: DeviceForm
  faces: FacePort[]
  width: number
  height: number
  href?: string
  /** For a "+N" chip: the box it opens. */
  parent?: string
}

export function EquipmentMap() {
  useLocale()
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
  const selfFaces: FacePort[] = useMemo(
    () => (interfaces.data?.interfaces ?? [])
      .filter(row => row.mac !== undefined && row.mac !== '' && row.physical && !row.loopback)
      .map(row => ({ id: row.name, kind: 'ethernet', linkUp: row.running })),
    [interfaces.data],
  )

  // Upstream: the managed box the default route's next hop belongs to, or else
  // the one the operator called a modem, joined to the interface the route
  // leaves by when that is one of the router's own ports.
  const uplink = useMemo(() => {
    const status = dial.data
    const via = status?.route?.via ?? status?.default_routes.find(route => route.family === 4)?.via
    const modem = devices.find(device => via !== undefined && hostOfDevice(device) === via)
      ?? devices.find(device => formOf(device.adapter, device.roles) === 'ont')
    if (modem === undefined) return undefined
    const names = [status?.uplink?.interface, status?.route?.dev].filter((name): name is string => name !== undefined)
    const port = names.find(name => selfFaces.some(face => face.id === name))
    return { deviceId: modem.id, port }
  }, [dial.data, devices, selfFaces])

  const wiring = useMemo(() => wire({
    devices: ids,
    sweeps: new Map([...sweeps].map(([id, sweep]) => [id, sweep.byPort])),
    associations: new Map([...snapshots].map(([id, snapshot]) => [id, (snapshot.associations ?? []).map(a => a.mac.toLowerCase())])),
    uplink,
  }), [ids, sweeps, snapshots, uplink])

  const boxes = useMemo(() => {
    const self: Box = { ...sized(SELF, t('this router'), selfFaces, 'router'), kind: 'self', href: '/devices' }
    const rest = devices.map(device => sized(
      device.id,
      device.name || device.id,
      facesOf(snapshots.get(device.id)),
      formOf(device.adapter, device.roles),
      `/devices/${encodeURIComponent(device.id)}`,
      hostOfDevice(device),
    ))
    return [self, ...rest]
  }, [devices, snapshots, selfFaces])

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
          form: 'generic',
          faces: [],
          width: CHIP_W,
          height: CHIP_H,
          href: `/clients/${encodeURIComponent(entry.mac)}`,
        })
        wires.push(entry.w)
      }
      if (folded) {
        const id = `more:${parent}`
        const hidden = named.slice(FOLD_SHOW)
        terminals.push({ id, kind: 'more', name: `+${hidden.length}`, form: 'generic', faces: [], width: MORE_W, height: CHIP_H, parent })
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
    ),
    [all, wires, wiring.above],
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

  if (devices.length === 0) return null

  return (
    <div className="relative rounded-xl border border-border bg-card">
      {asking.length > 0 && (
        <p className="absolute left-3 top-2 z-10 text-xs text-muted-foreground">
          {t('Finding which port each device is on… {done} of {total}', {
            done: asking.reduce((sum, sweep) => sum + sweep.asked, 0),
            total: asking.reduce((sum, sweep) => sum + (sweep.total ?? sweep.asked), 0),
          })}
        </p>
      )}
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
                    kind={w.kind}
                  />
                )
              })}
            </svg>

            {[...placed.values()].map(({ node, box }) => (
              <div key={node.id} className="absolute" style={{ left: node.x, top: node.y, width: node.width }}>
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
                  <Node box={box} />
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
    </div>
  )
}

/** A box with its face, its caption, and the ports' names underneath. */
function Node({ box }: { box: Box }) {
  const ports = box.faces.map(face => ({ id: face.id, name: face.name, kind: face.kind, link_up: face.linkUp }))
  return (
    <div>
      {box.faces.length > 0 ? (
        <DeviceFace ports={ports} form={box.form} labels="card" />
      ) : (
        <div className="flex h-[58px] items-center justify-center rounded-lg border border-dashed border-border bg-card text-[11px] text-muted-foreground">
          {t('Not read yet')}
        </div>
      )}
      <div className="mt-1 text-center text-xs leading-4">
        <span className="rounded bg-card px-1">
          <Link to={box.href ?? '#'} className="font-medium underline-offset-4 hover:underline">{box.name}</Link>
          {box.detail !== undefined && <span className="ml-1 text-muted-foreground">{box.detail}</span>}
        </span>
      </div>
    </div>
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
  faces: FacePort[],
  form: DeviceForm,
  href?: string,
  detail?: string,
): Box {
  const width = faces.length > 0 ? Math.min(MAX_NODE_WIDTH, naturalWidth(faces, form, PORT_WIDTH)) : 220
  const height = faces.length > 0 ? faceGeometry(faces, form, width).height + CAPTION_H + 16 : UNREAD_H
  return { id, kind: 'device', name, detail, form, faces, width, height, href }
}

/** Where a cable leaves or enters a box: its port when named, else its edge. */
function anchorOf(
  entry: { node: PlacedNode; box: Box },
  port: string | undefined,
  side: 'top' | 'bottom',
): { x: number; y: number } {
  const box = entry.box
  if (port !== undefined && box.faces.length > 0) {
    const geometry = faceGeometry(box.faces, box.form, entry.node.width)
    const point = geometry.ports.find(candidate => candidate.id === port)
    // Into the lower box at the port itself, behind its face; out of the upper
    // box in the port's column but under its caption, so a cable never runs
    // across the names.
    if (point !== undefined) return { x: entry.node.x + point.x, y: entry.node.y + (side === 'top' ? point.y : entry.node.height) }
  }
  // A box without a named port: under the face for the upper end (the caption
  // is below it and the cable passes behind its background), the top edge for
  // the lower one.
  const faceBottom = box.faces.length > 0 ? entry.node.height - CAPTION_H - 16 : entry.node.height
  return { x: entry.node.x + entry.node.width / 2, y: entry.node.y + (side === 'top' ? 0 : faceBottom) }
}

function cableStyle(kind: WireKind) {
  if (kind === 'wired') return { stroke: '#7d95ad', strokeWidth: 1.8 }
  if (kind === 'wireless') return { stroke: '#a9b8c7', strokeWidth: 1.4, strokeDasharray: '5 4' }
  return { stroke: '#c9d1da', strokeWidth: 1.4, strokeDasharray: '1.5 4', strokeLinecap: 'round' as const }
}

function Cable({ from, to, kind }: { from: { x: number; y: number }; to: { x: number; y: number }; kind: WireKind }) {
  const dy = Math.max(30, Math.abs(to.y - from.y) * 0.5)
  return (
    <path
      d={`M${from.x} ${from.y} C${from.x} ${from.y + dy} ${to.x} ${to.y - dy} ${to.x} ${to.y}`}
      fill="none"
      {...cableStyle(kind)}
    />
  )
}

function facesOf(snapshot: Snapshot | undefined): FacePort[] {
  return (snapshot?.ports ?? []).map(port => ({
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
