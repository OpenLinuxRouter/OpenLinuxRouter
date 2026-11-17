import { useLocale, t } from '@/lib/i18n'
import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router'

import { useClientList } from '@/features/clients/queries'
import { useQueries } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { deviceKeys, useDevicesConfig } from '@/features/devices/queries'
import type { Device, Snapshot } from '@/features/devices/types'
import { useInterfaces } from '@/features/link/queries'

import { DeviceFace } from './device-face'
import type { FacePort } from './face'
import { formOf, type DeviceForm } from './form'
import { placeMap, type MapEdge, type PlacedNode } from './map-layout'
import { faceGeometry, naturalWidth } from './port-points'
import { useAllSweeps } from './sweep'
import type { FarEnd } from './equipment'

/**
 * The boxes and the cables between them, laid out by depth from this router.
 *
 * The unit is a port, not a box: a cable leaves the port a switch named and
 * lands on whatever that port learned. That is the only model that survives a
 * box with several links — this router has two NICs on the same switch — so
 * nothing here assumes a star, and nothing assumes one cable per box.
 *
 * A cable is drawn only where the evidence is. Where a switch named a port and
 * the far end is a box, the cable ends on the box, not on one of its sockets:
 * the switch knows the box, not which port the wire is in. Wireless
 * associations are dashed and belong to the box, not to a port.
 */
const PORT_WIDTH = 44
const MAX_NODE_WIDTH = 430
const CHIP_W = 138
const CHIP_H = 32
const CAPTION_H = 22
const UNREAD_H = 58

interface Box {
  id: string
  kind: 'self' | 'device' | 'client'
  name: string
  detail?: string
  form: DeviceForm
  faces: FacePort[]
  width: number
  height: number
  href?: string
}

export function EquipmentMap() {
  useLocale()
  const config = useDevicesConfig()
  const clients = useClientList()
  const interfaces = useInterfaces()
  const sweeps = useAllSweeps()
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

  const boxes = useMemo(() => {
    const self: Box = sized('self', 'this router', selfFaces, 'router', '/devices')
    const rest = devices.map(device => {
      const faces = facesOf(snapshots.get(device.id))
      return sized(device.id, device.name || device.id, faces, formOf(device.adapter, device.roles), `/devices/${encodeURIComponent(device.id)}`, hostOfDevice(device))
    })
    return [self, ...rest]
  }, [devices, snapshots, selfFaces])

  // Every far end a switch placed, and every client an access point reported.
  const { edges, terminals } = useMemo(() => {
    const edges: MapEdge[] = []
    const terminals = new Map<string, Box>()
    const addClient = (end: FarEnd, dashed: boolean, from: string, port?: string) => {
      const id = `mac:${end.mac}`
      if (!terminals.has(id)) {
        const row = byMac.get(end.mac)
        terminals.set(id, {
          id,
          kind: 'client',
          name: row?.name || end.name,
          detail: row?.ips?.[0] ?? end.detail,
          form: 'generic',
          faces: [],
          width: CHIP_W,
          height: CHIP_H,
          href: `/clients/${encodeURIComponent(end.mac)}`,
        })
      }
      edges.push({ from, to: id, fromPort: port, dashed })
    }

    for (const device of devices) {
      const sweep = sweeps.get(device.id)
      for (const [port, ends] of sweep?.byPort ?? []) {
        for (const end of ends) {
          if (end.kind === 'router') edges.push({ from: device.id, to: 'self', fromPort: port, toPort: end.name })
          else if (end.kind === 'device' && end.deviceId !== undefined) edges.push({ from: device.id, to: end.deviceId, fromPort: port })
          else if (end.kind === 'client') addClient(end, false, device.id, port)
        }
      }
      for (const association of snapshots.get(device.id)?.associations ?? []) {
        addClient({ mac: association.mac, kind: 'client', name: association.mac }, true, device.id)
      }
    }
    return { edges, terminals: [...terminals.values()] }
  }, [devices, sweeps, snapshots, byMac])

  const all = useMemo(() => [...boxes, ...terminals], [boxes, terminals])
  const layout = useMemo(
    () => placeMap(all.map(box => ({ id: box.id, width: box.width, height: box.height })), edges, 'self'),
    [all, edges],
  )
  const byId = useMemo(() => new Map(all.map(box => [box.id, box])), [all])
  const frame = useRef<HTMLDivElement>(null)
  const [scale, setScale] = useState(1)
  useEffect(() => {
    const element = frame.current
    if (element === null) return
    const measure = () => setScale(Math.min(1, element.clientWidth / Math.max(1, layout.width)))
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [layout.width])
  const placed = useMemo(
    () => layout.nodes.flatMap(node => {
      const box = byId.get(node.id)
      return box === undefined ? [] : [{ node, box }]
    }),
    [layout, byId],
  )

  if (devices.length === 0) return null

  return (
    <div ref={frame} className="w-full">
      <div
        className="relative mx-auto origin-top"
        style={{ width: layout.width, height: layout.height, transform: `scale(${scale})`, marginBottom: layout.height * (scale - 1) }}
      >
        <svg className="absolute inset-0" width={layout.width} height={layout.height} aria-hidden>
          {edges.map((edge, index) => {
            const from = placed.find(entry => entry.node.id === edge.from)
            const to = placed.find(entry => entry.node.id === edge.to)
            if (from === undefined || to === undefined) return null
            return (
              <Cable
                key={`${edge.from}-${edge.fromPort ?? index}-${edge.to}`}
                from={anchorOf(from, edge.fromPort, 'bottom')}
                to={anchorOf(to, edge.toPort, 'top')}
                dashed={edge.dashed === true}
              />
            )
          })}
        </svg>

        {placed.map(({ node, box }) => (
          <div key={node.id} className="absolute" style={{ left: node.x, top: node.y, width: node.width }}>
            {box.kind === 'client' ? <Chip box={box} /> : <Node box={box} />}
          </div>
        ))}
      </div>
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
        <div className="flex h-[58px] items-center justify-center rounded-lg border border-dashed border-border text-[11px] text-muted-foreground">
          {t('Not read yet')}
        </div>
      )}
      <div className="mt-1 text-center text-[11px] leading-4">
        <span className="rounded bg-background px-1">
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
    >
      <span className="truncate">{box.name}</span>
      {box.detail !== undefined && <span className="truncate text-[9px] text-muted-foreground">{box.detail}</span>}
    </Link>
  )
}

function sized(
  id: string,
  name: string,
  faces: FacePort[],
  form: DeviceForm,
  href: string,
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
    if (point !== undefined) return { x: entry.node.x + point.x, y: entry.node.y + point.y }
  }
  return { x: entry.node.x + entry.node.width / 2, y: entry.node.y + (side === 'top' ? 0 : entry.node.height) }
}

function Cable({ from, to, dashed }: { from: { x: number; y: number }; to: { x: number; y: number }; dashed: boolean }) {
  const dy = Math.max(36, Math.abs(to.y - from.y) * 0.45)
  return (
    <path
      d={`M${from.x} ${from.y} C${from.x} ${from.y + dy} ${to.x} ${to.y - dy} ${to.x} ${to.y}`}
      fill="none"
      stroke={dashed ? '#b6c2cf' : '#8fa6bd'}
      strokeWidth={dashed ? 1.4 : 1.7}
      strokeDasharray={dashed ? '5 4' : undefined}
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
  const match = /^[a-z][a-z0-9+.-]*:\/\/([^/]+)/i.exec(device.address.trim())
  return match?.[1]
}
