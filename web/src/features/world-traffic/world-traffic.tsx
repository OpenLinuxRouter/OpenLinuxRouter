import { t, useLocale } from '@/lib/i18n'
import { Heading } from '@/components/ui/heading'
import { PageHeading } from '@/components/layout/page-heading'
import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { useTheme } from 'next-themes'
import * as maplibregl from 'maplibre-gl'
import type { ExpressionSpecification, GeoJSONSource } from 'maplibre-gl'
import type * as GeoJSON from 'geojson'
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url'
import 'maplibre-gl/dist/maplibre-gl.css'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { usePublicAddresses } from '@/features/system/queries'
import { api } from '@/lib/api'
import { cn, formatRate } from '@/lib/utils'
import type { GeoPlace } from '@/lib/api-types'
import countries from './countries.json'
import regionsUrl from './regions.geojson?url'

interface Peer { ip: string; upload: number; download: number; place: GeoPlace }
interface Origin { latitude?: number; longitude?: number }
interface Snapshot { state: string; error?: string; as_of: string; seconds: number; total: number; peers: Peer[]; locations: { state: string; error?: string } }
type Position = [number, number]
interface FlowTarget { feature: GeoJSON.Feature; coordinates: Position[] }
// One arc on screen. `opacity` is presence (the window), `glow` is whether this
// arc currently holds a turn of the travelling highlight.
interface Visual {
  target: FlowTarget
  opacity: number
  glide: number
  jitter: number
  wait: number
  from: number
  to: number
  fade: number
  leaving: boolean
  glow: number
  on: boolean
  turnEnd: number
  turns: number
  lastTurn: number
}
const empty: GeoJSON.FeatureCollection = { type: 'FeatureCollection', features: [] }
// Geography is neutral at both sizes: one land tone above one sea tone, with
// white country hairlines, and the same values in the card and on the page, so
// opening the card never repaints the world. Country fills from a palette
// would put hue on land, and on this map hue belongs to traffic.
const seaColor = (dark: boolean) => dark ? '#232731' : '#f6f7f9'
const landColor = (dark: boolean) => dark ? '#424b5b' : '#e2e6ec'
// Traffic is the map's only colour; land and sea carry none, so an arc can
// never be mistaken for geography. A line-progress gradient only fades the
// alpha in at the router, where every arc starts, so the shared pixel becomes
// a fan instead of a knot.
const flowColor = (dark: boolean) => dark ? '#fb923c' : '#ea580c'
// A lighter amber under the line, so overlap adds up as light instead of as a
// darker stain.
const haloColor = (dark: boolean) => dark ? '#fdba74' : '#fb923c'
// The highlight is pale warm light rather than white: it lifts the arc it rides
// on without becoming a second hue or a glare on the light map.
const glowColor = (dark: boolean) => dark ? '#ffe4bd' : '#ffd9a8'
const transparent = (hex: string, alpha = 0) => {
  const n = parseInt(hex.slice(1), 16)
  return `rgba(${n >> 16}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`
}
const flowGradient = (dark: boolean, halo = false) => {
  const color = halo ? haloColor(dark) : flowColor(dark)
  return ['interpolate', ['linear'], ['line-progress'], 0, transparent(color), 0.15, color, 1, color] as ExpressionSpecification
}
// The highlight's gradient runs along the short stroke it draws: nothing at the
// tail, the warm light at the head. Each stroke is its own line, so one paint
// expression gives every arc a comet, and the feathered tail carries direction.
const glowGradient = (dark: boolean) => {
  const color = glowColor(dark)
  return ['interpolate', ['linear'], ['line-progress'], 0, transparent(color), 0.55, transparent(color, 0.5), 1, color] as ExpressionSpecification
}
// One width for every arc at the world view. Zooming into a region thins it, so
// a country-sized view is not covered by a ribbon meant for a globe. Four
// thicknesses compared badly once dozens of arcs overlapped, so width no longer
// carries volume; the map shows which locations are active and which way their
// traffic runs, and the ranked rates live in the details dialog.
// The zoom a map fits the world at also measures how much world it holds, so
// the same ramp thins the arcs on a map too small for them. The card preview
// fits the world into a few hundred pixels: there, 2px covers more than a
// degree of longitude and the six-pixel halo under it merges the fan of East
// Asian destinations into one ribbon. Below the zoom a desktop map page fits
// at — the card, and a phone's map page — the arc thins to 1px, which is
// still two device pixels on a retina screen and keeps the gap between arcs.
// Zoom may only be the input of a top-level interpolate, so the scale wraps
// the width instead of multiplying it.
const arcWidth = 2
const scaledWidth = (base: number) => ['interpolate', ['linear'], ['zoom'], -0.5, base * 0.5, 1.5, base, 3, base * 0.9, 4.5, base * 0.7, 6, base * 0.55] as ExpressionSpecification
// The highlight is a short stretch of the arc drawn as two blurred strokes, so
// it has no hard edge and needs no extra hue. It travels out and back in four
// seconds each way; the window is a fixed number of the path's samples, so a
// short arc carries the same proportion of light as a long one.
const glowTail = 4
const glowSeconds = 8000
// A snapshot lands every two seconds, so locations enter and leave in batches.
// Staggering those fades across the gap keeps a batch from stepping the whole
// map at one instant. This is a rendering choice, not a claim about the
// connections.
const pollMs = 2000
// Presence fades over a fixed span rather than easing forever, so a batch can be
// guaranteed to settle before the next snapshot: the last fade starts inside
// this window and the fade itself is the remainder of the poll.
const fadeMs = 450
const marginMs = 200
const settleMs = pollMs - fadeMs - marginMs
const ease = (t: number) => 0.5 - 0.5 * Math.cos(Math.PI * t)
const staggerWindow = (count: number) => count < 2 ? 0 : Math.min(settleMs, count * 220)
// A highlight on every arc at once turns the map into a field of moving sparks.
// A few arcs hold a turn at a time; a new location takes the next free turn, and
// the rest queue by fewest turns and longest wait, so over a minute every arc
// has had its light. The limit grows with the number of arcs, never past five.
const glowTurnMs = 6000
const glowLimit = (count: number) => Math.min(5, Math.max(1, Math.round(count / 4)))
// Each location owns a slot in the window and jitters inside it, so the gaps
// look irregular without ever bunching the batch into one instant.
const staggerOffset = (index: number, count: number, window: number, jitter: number) => window * (index + 0.25 + 0.5 * jitter) / count
// The hash only decides who goes first; it is stable across snapshots.
const staggerOrder = (keys: string[]) => keys.slice().sort((a, b) => hashOf(a) - hashOf(b))
// Zoom may only appear as the input of a top-level step/interpolate expression.
// Layout expressions are evaluated at integer zooms; use integer thresholds
// for regions and cities so names don't wait for the final zoom level.
const labelText = ['step', ['zoom'], '', ...[1, 2, 3, 4, 5, 6].flatMap(zoom => [zoom, ['case', ['<=', ['get', 'minzoom'], zoom], ['get', 'name'], '']])] as ExpressionSpecification
function position(place: { latitude?: number; longitude?: number }): Position | undefined {
  if (typeof place.latitude === 'number' && typeof place.longitude === 'number' && Number.isFinite(place.latitude) && Number.isFinite(place.longitude) && Math.abs(place.latitude) <= 90 && Math.abs(place.longitude) <= 180) return [place.longitude, place.latitude]
}
maplibregl.setWorkerUrl(workerUrl)
maplibregl.setWorkerCount(1)

function arc(from: Position, to: Position, offset: number): Position[] {
  let end = to[0]
  while (end - from[0] > 180) end -= 360
  while (end - from[0] < -180) end += 360
  const bend = Math.min(22, Math.abs(end - from[0]) * 0.18) + offset
  return Array.from({ length: 49 }, (_, i) => {
    const t = i / 48
    return [from[0] + (end - from[0]) * t, Math.max(-78, Math.min(78, from[1] + (to[1] - from[1]) * t + Math.sin(Math.PI * t) * bend))]
  })
}
// A stable 0–1 value per location, used for the highlight's starting phase and
// for its place in a staggered batch.
function hashOf(key: string) {
  let hash = 0
  for (const char of key) hash = (hash * 131 + char.charCodeAt(0)) >>> 0
  return hash / 4294967296
}
// A window of the arc around the travelling head, in travel order so the head
// always comes last. Its endpoints are interpolated, keeping the window the
// same length when the head sits between samples.
function highlight(path: Position[], head: number, returning: boolean): Position[] {
  const lo = Math.max(0, head - glowTail), hi = Math.min(path.length - 1, head)
  const at = (index: number): Position => {
    const i = Math.max(0, Math.min(path.length - 1, Math.floor(index)))
    const j = Math.min(path.length - 1, i + 1)
    const t = index - i
    return [path[i][0] + (path[j][0] - path[i][0]) * t, path[i][1] + (path[j][1] - path[i][1]) * t]
  }
  const points = [at(lo)]
  for (let i = Math.floor(lo) + 1; i < Math.ceil(hi); i++) points.push(path[i])
  points.push(at(hi))
  return returning ? points.reverse() : points
}

function TrafficMap({ peers, origin, dark, expanded = false }: { peers: Peer[]; origin?: Origin; dark: boolean; expanded?: boolean }) {
  useLocale()
  const container = useRef<HTMLDivElement>(null)
  const map = useRef<maplibregl.Map | null>(null)
  const [ready, setReady] = useState(false)
  const [failed, setFailed] = useState(false)
  const targets = useRef(new Map<string, FlowTarget>())
  useEffect(() => {
    if (!container.current) return
    let instance: maplibregl.Map
    try {
      instance = new maplibregl.Map({ container: container.current,
        // The detailed administrative map is a fingerprinted local asset. Only
        // map pages fetch it; the small preview keeps the 110m outline.
        // Omitting glyphs uses local fonts, without a third-party glyph server.
        style: { version: 8, sources: { countries: { type: 'geojson', data: expanded ? regionsUrl : countries as GeoJSON.FeatureCollection, maxzoom: 6 } }, layers: [
          { id: 'ocean', type: 'background', paint: { 'background-color': '#f6f7f9' } },
          { id: 'land', type: 'fill', source: 'countries', filter: ['==', ['geometry-type'], 'Polygon'], paint: { 'fill-color': '#e2e6ec' } },
          { id: 'borders', type: 'line', source: 'countries', filter: ['==', ['geometry-type'], 'Polygon'], paint: { 'line-color': '#ffffff', 'line-width': 0.4 } },
        ] }, center: [25, 16], zoom: 0, minZoom: -3, maxZoom: 6, interactive: expanded, attributionControl: false,
        dragRotate: false, pitchWithRotate: false, touchZoomRotate: expanded ? { around: 'center' } : false,
        // Keep the whole world visible even in a tall phone viewport.
        transformConstrain: expanded ? (center, zoom) => ({ center, zoom: Math.max(-3, Math.min(6, zoom)) }) : undefined })
    } catch { setFailed(true); return }
    map.current = instance
    const fit = () => instance.fitBounds([[-180, -58], [180, 80]], { padding: 3, duration: 0 })
    // Resizing an already-open map must preserve the region the user chose.
    const observer = new ResizeObserver(() => { instance.resize(); if (!expanded) fit() })
    observer.observe(container.current)
    instance.on('load', () => {
      if (expanded) {
        instance.touchZoomRotate.disableRotation()
        instance.addLayer({ id: 'regions', type: 'line', source: 'countries', minzoom: 2, filter: ['==', ['get', 'kind'], 'boundary'], paint: { 'line-color': '#5b6472', 'line-width': ['interpolate', ['linear'], ['zoom'], 2, 0.3, 6, 0.8], 'line-opacity': 0.55 } })
        for (const kind of ['country', 'region', 'city']) {
          instance.addLayer({ id: `${kind}-labels`, type: 'symbol', source: 'countries', minzoom: kind === 'country' ? 1 : 3, filter: ['==', ['get', 'kind'], `${kind}-label`], layout: {
            'text-field': labelText,
            'text-font': ['sans-serif'], 'text-size': kind === 'country' ? 13 : 12, 'text-max-width': 9, 'text-padding': 6, 'symbol-sort-key': ['get', 'rank'],
          }, paint: { 'text-color': '#171b22', 'text-halo-color': '#ffffff', 'text-halo-width': 1 } })
        }
      }
      // lineMetrics computes the distances a line-progress gradient needs.
      instance.addSource('flows', { type: 'geojson', data: empty, lineMetrics: true })
      // lineMetrics again: the comet's gradient is measured along its own
      // stroke, from the feathered tail to the bright head.
      instance.addSource('glow', { type: 'geojson', data: empty, lineMetrics: true })
      // Each arc is a soft halo under a translucent line. Both composite per
      // feature, so where arcs bunch up — at the router, or along a busy
      // corridor — the halo accumulates into light.
      instance.addLayer({ id: 'flow-halo', type: 'line', source: 'flows', layout: { 'line-cap': 'round', 'line-join': 'round' }, paint: {
        'line-width': scaledWidth(arcWidth * 1.8 + 3),
        'line-blur': scaledWidth(arcWidth * 0.8 + 2),
        'line-opacity': ['*', 0.16, ['get', 'opacity']],
      } })
      instance.addLayer({ id: 'flows', type: 'line', source: 'flows', layout: { 'line-cap': 'round', 'line-join': 'round' }, paint: {
        'line-width': scaledWidth(arcWidth),
        'line-opacity': ['*', 0.6, ['get', 'opacity']],
      } })
      // Two stacked strokes: a wide, heavily blurred one for the bloom and a
      // narrow one for the core. Both take their shape from the same gradient,
      // so the highlight has no hard edge at either end.
      instance.addLayer({ id: 'glow-halo', type: 'line', source: 'glow', layout: { 'line-cap': 'round', 'line-join': 'round' }, paint: {
        'line-width': scaledWidth(12),
        'line-blur': scaledWidth(6),
        'line-opacity': ['*', 0.38, ['get', 'opacity']],
      } })
      instance.addLayer({ id: 'glow', type: 'line', source: 'glow', layout: { 'line-cap': 'round', 'line-join': 'round' }, paint: {
        'line-width': scaledWidth(3.2),
        'line-blur': scaledWidth(1.5),
        'line-opacity': ['*', 0.95, ['get', 'opacity']],
      } })
      fit()
      setReady(true)
    })
    instance.on('error', () => setFailed(true))
    return () => { observer.disconnect(); instance.remove(); map.current = null }
  }, [expanded])
  useEffect(() => {
    const instance = map.current
    if (!instance || !ready) return
    instance.setPaintProperty('ocean', 'background-color', seaColor(dark))
    instance.setPaintProperty('land', 'fill-color', landColor(dark))
    instance.setPaintProperty('borders', 'line-color', dark ? '#232731' : '#ffffff')
    instance.setPaintProperty('flow-halo', 'line-gradient', flowGradient(dark, true))
    instance.setPaintProperty('flows', 'line-gradient', flowGradient(dark))
    instance.setPaintProperty('glow-halo', 'line-gradient', glowGradient(dark))
    instance.setPaintProperty('glow', 'line-gradient', glowGradient(dark))
    if (expanded) {
      instance.setPaintProperty('regions', 'line-color', dark ? '#232731' : '#5b6472')
      for (const id of ['country-labels', 'region-labels', 'city-labels']) {
        instance.setPaintProperty(id, 'text-color', id === 'region-labels' ? (dark ? '#abb4c3' : '#5b6472') : (dark ? '#f1f3f7' : '#171b22'))
        instance.setPaintProperty(id, 'text-halo-color', dark ? '#232731' : '#ffffff')
      }
    }
    // Arcs carry the whole reading: no endpoint dots, because a handful of
    // nearby destinations turns them into a cluster of specks at the hub.
    const start = origin ? position(origin) : undefined
    const next = new Map<string, FlowTarget>()
    const locations = new Map<string, Peer>()
    for (const peer of peers) {
      const end = position(peer.place)
      if (!end) continue
      const key = end.join(',')
      const previous = locations.get(key)
      locations.set(key, { ...peer, upload: (previous?.upload ?? 0) + peer.upload, download: (previous?.download ?? 0) + peer.download })
    }
    for (const [location, peer] of locations) {
      const end = position(peer.place)
      if (!end) continue
      if (!start || (start[0] === end[0] && start[1] === end[1])) continue
      const total = peer.download + peer.upload
      if (total <= 0) continue
      const coordinates = arc(start, end, 0)
      next.set(`${location}:${start}`, { feature: { type: 'Feature', properties: {}, geometry: { type: 'LineString', coordinates } }, coordinates })
    }
    targets.current = next
  }, [peers, origin, dark, ready, expanded])
  useEffect(() => {
    const instance = map.current
    if (!instance || !ready) return
    const set = (id: string, features: GeoJSON.Feature[]) => (instance.getSource(id) as GeoJSONSource).setData({ type: 'FeatureCollection', features })
    // Keep identity and phase independent of snapshot order and rank changes.
    const visible = new Map<string, Visual>()
    const reduced = matchMedia('(prefers-reduced-motion: reduce)')
    const endTurn = (visual: Visual, at: number) => {
      visual.on = false
      visual.turns += 1
      visual.lastTurn = at
    }
    // Diffing against the previous snapshot, once per snapshot, is what lets a
    // batch share one stagger window. null forces the first frame to diff.
    let generation: Map<string, FlowTarget> | null = null
    let previous = performance.now(), lastDraw = 0, frame = 0
    const animate = (now: number) => {
      frame = requestAnimationFrame(animate)
      if (document.hidden) { previous = now; return }
      if (now - lastDraw < 32) return
      const elapsed = Math.min(64, now - previous)
      previous = now
      lastDraw = now
      const fade = reduced.matches ? 0 : fadeMs
      if (targets.current !== generation) {
        const next = targets.current
        const entering: string[] = [], leaving: string[] = []
        for (const [key, visual] of visible) {
          const target = next.get(key)
          if (target) { visual.target = target; continue }
          if (!visual.leaving) leaving.push(key)
        }
        for (const key of next.keys()) if (!visible.has(key)) entering.push(key)
        const enterWindow = reduced.matches ? 0 : staggerWindow(entering.length)
        const leaveWindow = reduced.matches ? 0 : staggerWindow(leaving.length)
        staggerOrder(entering).forEach((key, index) => {
          const jitter = hashOf(key)
          const wait = staggerOffset(index, entering.length, enterWindow, jitter)
          visible.set(key, { target: next.get(key)!, opacity: 0, glide: jitter, jitter, wait, from: 0, to: 1, fade: 0, leaving: false, glow: 0, on: false, turnEnd: 0, turns: 0, lastTurn: 0 })
        })
        staggerOrder(leaving).forEach((key, index) => {
          const visual = visible.get(key)!
          visual.leaving = true
          visual.wait = staggerOffset(index, leaving.length, leaveWindow, visual.jitter)
          visual.from = visual.opacity
          visual.to = 0
          visual.fade = 0
        })
        generation = next
      }
      const lines: GeoJSON.Feature[] = [], candidates: Visual[] = []
      for (const [key, visual] of visible) {
        const active = targets.current.has(key)
        const want = active ? 1 : 0
        // A location can leave while it is still arriving, or come back before
        // its fade has finished: restart from wherever the line stands.
        if (want !== visual.to) {
          visual.from = visual.opacity
          visual.to = want
          visual.fade = 0
          visual.wait = 0
          visual.leaving = !active
        }
        if (visual.wait > 0) visual.wait = Math.max(0, visual.wait - elapsed)
        else if (visual.fade < 1) {
          visual.fade = Math.min(1, visual.fade + elapsed / Math.max(1, fade))
          visual.opacity = visual.from + (visual.to - visual.from) * ease(visual.fade)
        }
        if (!active && visual.fade >= 1) { visible.delete(key); continue }
        // Presence is the only thing that changes a line. It fades in when its
        // location starts reporting traffic and out when the location leaves
        // the window; while the location stays, the line stays at full weight.
        const light = visual.opacity
        lines.push({ ...visual.target.feature, properties: { opacity: light } })
        // A line on its way out gives its turn back to the queue.
        if (visual.on && want === 0) endTurn(visual, now)
        if (want === 1 && light > 0.9) candidates.push(visual)
      }
      if (!reduced.matches) {
        // Finished turns free a slot for the next arc in the queue: fewest turns
        // first, then the longest wait, so a new location goes before the rest.
        for (const visual of candidates) if (visual.on && now >= visual.turnEnd) endTurn(visual, now)
        const limit = glowLimit(candidates.length)
        let running = 0
        for (const visual of candidates) if (visual.on) running++
        if (running < limit) {
          const waiting = candidates.filter(visual => !visual.on).sort((a, b) => a.turns - b.turns || a.lastTurn - b.lastTurn || a.jitter - b.jitter)
          for (const visual of waiting) {
            if (running >= limit) break
            visual.on = true
            // Uneven turns keep two arcs from swapping their slots forever.
            visual.turnEnd = now + glowTurnMs * (0.85 + 0.3 * visual.jitter)
            running++
          }
        }
      }
      const glows: GeoJSON.Feature[] = []
      for (const visual of visible.values()) {
        // Fade the light itself in and out, so a turn does not pop on.
        const goal = visual.on && !reduced.matches ? 1 : 0
        visual.glow += (goal - visual.glow) * (reduced.matches ? 1 : 1 - Math.exp(-elapsed / 200))
        if (visual.glow <= 0.01) continue
        visual.glide = (visual.glide + elapsed / glowSeconds) % 1
        const returning = visual.glide >= 0.5
        const progress = returning ? (1 - visual.glide) * 2 : visual.glide * 2
        // Fade near both ends. Every arc starts at the router, so a highlight
        // that lit up there would rebuild the cluster the endpoints used to make.
        const edge = Math.min(1, Math.min(progress, 1 - progress) / 0.08)
        if (edge <= 0) continue
        const points = highlight(visual.target.coordinates, progress * (visual.target.coordinates.length - 1), returning)
        if (points.length < 3) continue
        glows.push({ type: 'Feature', properties: { opacity: visual.opacity * edge * visual.glow }, geometry: { type: 'LineString', coordinates: points } })
      }
      set('flows', lines)
      set('glow', glows)
    }
    frame = requestAnimationFrame(animate)
    return () => cancelAnimationFrame(frame)
  }, [ready])
  return <div className={cn('relative h-full min-h-32 w-full overflow-hidden', !expanded && 'rounded-lg')} style={expanded ? { backgroundColor: seaColor(dark) } : undefined}><div ref={container} style={{ position: 'absolute', inset: 0 }} aria-hidden={!expanded} />{failed && <p className="absolute inset-0 flex items-center justify-center bg-muted p-3 text-xs text-muted-foreground">{t('Map unavailable.')}</p>}</div>
}

function useWorldTraffic() {
  const query = useQuery({ queryKey: ['gateway', 'world-traffic'], queryFn: () => api.get<Snapshot>('/api/gateway/world-traffic'), refetchInterval: pollMs })
  const data = query.data
  const stale = data?.as_of && Date.now() - Date.parse(data.as_of) > 10000
  const ready = !query.isError && !stale && data?.state === 'ready'
  const peers = ready ? data.peers : []
  const share = data?.total ? Math.round(peers.reduce((n, p) => n + p.download + p.upload, 0) / data.total * 100) : 0
  const unknown = peers.filter(p => !position(p.place)).length
  const status = query.isError || stale ? t('Unavailable') : !data ? t('Loading…') : data.state === 'disabled' ? 'Counting off' : data.state === 'warming' ? 'Measuring…' : data.state !== 'ready' ? t('Unavailable') : peers.length ? `Top ${peers.length} · ${share}%` : 'No observed traffic'
  return { data, ready, peers, unknown, status }
}

export function WorldTraffic({ origin, embedded = false }: {
  origin?: Origin
  embedded?: boolean
}) {
  useLocale()
  const [open, setOpen] = useState(false)
  const { resolvedTheme } = useTheme()
  const traffic = useWorldTraffic()
  return <section aria-label={t('Traffic map')} className={embedded ? 'flex min-h-0 w-full min-w-0 flex-col gap-1' : 'col-span-2 flex min-w-0 flex-col gap-2 panel-surface p-3 sm:p-4 lg:col-span-1 lg:row-span-3'}>
    {!embedded && <Heading size="sm" className="flex h-6 items-center"><button onClick={() => setOpen(true)} className="relative rounded-sm before:absolute before:inset-x-0 before:-inset-y-2.5 focus-visible:outline-2 focus-visible:outline-ring" aria-label={t('Map details')}>{t('Map')}</button></Heading>}
    <Link to="/map" aria-label={t('Map')} className={embedded ? 'flex min-h-32 flex-1 rounded-lg focus-visible:outline-2 focus-visible:outline-ring' : '-mx-1 -mb-1 flex min-h-44 flex-1 rounded-lg focus-visible:outline-2 focus-visible:outline-ring sm:-mx-2 sm:-mb-2'}><TrafficMap peers={traffic.peers} origin={origin} dark={resolvedTheme === 'dark'} /></Link>
    {(!traffic.ready || !traffic.peers.length) && <p className="text-xs text-muted-foreground">{traffic.status}</p>}
    <WorldTrafficDetails open={open} onOpenChange={setOpen} origin={origin} {...traffic} />
  </section>
}

export function WorldTrafficPage() {
  useLocale()
  const publicAddresses = usePublicAddresses()
  const origin = publicAddresses.data?.ipv4.ip ? publicAddresses.data.ipv4 : publicAddresses.data?.ipv6
  const { resolvedTheme } = useTheme()
  const [open, setOpen] = useState(false)
  const traffic = useWorldTraffic()

  return <div className="space-y-6">
    <PageHeading title={t('Map')} action={<Button variant="secondary" onClick={() => setOpen(true)}>{t('Map details')}</Button>} />
    <div className="panel-surface h-[clamp(20rem,65dvh,48rem)] overflow-hidden sm:h-[clamp(24rem,70dvh,48rem)]" role="region" aria-label={t('World traffic map, drag to pan and scroll to zoom')}>
      <TrafficMap peers={traffic.peers} origin={origin} dark={resolvedTheme === 'dark'} expanded />
    </div>
    {(!traffic.ready || !traffic.peers.length) && <p className="text-sm text-muted-foreground" role="status">{traffic.status}</p>}
    <WorldTrafficDetails open={open} onOpenChange={setOpen} origin={origin} {...traffic} />
  </div>
}

function WorldTrafficDetails({ open, onOpenChange, origin, data, peers, unknown, status }: {
  open: boolean
  onOpenChange: (open: boolean) => void
  origin?: Origin
  data?: Snapshot
  peers: Peer[]
  unknown: number
  status: string
}) {
  return <Dialog open={open} onOpenChange={onOpenChange}><DialogContent className="max-h-[85dvh] overflow-y-auto"><DialogHeader><DialogTitle>{t('Map')}</DialogTitle><DialogDescription>{t('Remote IPs by upload and download in the last 30 seconds. Every arc is the same width. A few arcs at a time carry a soft highlight that travels out and back — new locations first, then the rest take turns — and an arc fades out when its location leaves the window. IP locations are approximate.')}{unknown ? ` ${unknown} locations unknown.` : ''}</DialogDescription></DialogHeader>
    <p className="text-sm text-muted-foreground">{status}{data?.seconds ? ` · ${Math.round(data.seconds)}s sampled` : ''}. {data?.state === 'disabled' ? 'Connection byte accounting is off. Enable Gateway traffic counting and apply its configuration.' : 'Counts include router and forwarded connections observed in Linux conntrack. Connections ending between samples and traffic bypassing conntrack may be missed.'} {(!origin || !position(origin)) && 'Router location is unknown; arcs cannot be drawn.'}</p>
    {data?.error && <p className="break-words text-sm text-destructive">{data.error}</p>}
    {data?.locations?.error && <p className="break-words text-sm text-destructive">{data.locations.error}</p>}
    <ul className="divide-y">{peers.map(peer => <li key={peer.ip} className="space-y-1 py-3 text-sm"><div className="flex flex-wrap justify-between gap-2"><span className="break-all font-mono">{peer.ip}</span><span>{[peer.place.city || peer.place.region, peer.place.country_name].filter(Boolean).join(', ') || 'Location unknown'}</span></div><div className="flex justify-between gap-2 text-xs tabular-nums"><span className="text-receive">↓ {formatRate(peer.download)}</span><span className="text-transmit">↑ {formatRate(peer.upload)}</span></div></li>)}</ul>
    <p className="text-xs text-muted-foreground">{t('IP locations:')} {data?.locations?.state || 'Unavailable'} · <a href="https://ip.sb" target="_blank" rel="noreferrer" className="underline">ip.sb</a>. Map: <a href="https://www.naturalearthdata.com" target="_blank" rel="noreferrer" className="underline">Natural Earth</a> (public domain), rendered with MapLibre.</p>
  </DialogContent></Dialog>
}
