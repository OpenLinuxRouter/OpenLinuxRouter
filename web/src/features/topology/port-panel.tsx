import { t, useLocale } from '@/lib/i18n'
import type { ReactNode } from 'react'

import { PANEL, panelRows, speedLamp, type PanelGeometry, type PanelPort } from './panel'

/**
 * A box on the map: its name across the top, its ports in a bay underneath.
 *
 * Every port is drawn the same whatever its state — a jack does not change
 * colour when the cable comes out. State is in the lamps: an RJ45 jack has two
 * beside its latch slot, as on the hardware — the left one the link, the right
 * one the speed (green at a gigabit and up, amber below, dark when the box did
 * not say); a fiber cage has one, for the link.
 * The panel is sized by `geometry`, which the map also uses to start cables at
 * the ports, so the two cannot disagree.
 */
export function PortPanel({
  name,
  detail,
  ports,
  geometry,
  empty,
  title,
}: {
  name: ReactNode
  detail?: string
  ports: readonly PanelPort[]
  geometry: PanelGeometry
  /** What the bay says when there are no ports to draw. */
  empty?: string
  title?: string
}) {
  useLocale()
  const up = ports.filter(port => port.linkUp === true).length
  const bayH = geometry.height - PANEL.padTop - PANEL.headH - PANEL.headGap - PANEL.padBottom
  return (
    <div
      className="rounded-xl bg-card shadow-[0_0_0_1px_var(--color-border),0_1px_2px_rgb(15_23_42/0.05),0_4px_14px_rgb(15_23_42/0.06)]"
      style={{ width: geometry.width, height: geometry.height, padding: `${PANEL.padTop}px ${PANEL.padX}px 0` }}
      title={title}
    >
      <div className="flex items-baseline gap-2 whitespace-nowrap" style={{ height: PANEL.headH, marginBottom: PANEL.headGap }}>
        <span className="text-[13px] font-semibold leading-[18px]">{name}</span>
        {detail !== undefined && <span className="text-[11px] tabular-nums text-muted-foreground">{detail}</span>}
        {ports.length > 0 && (
          <span className="ml-auto pl-3 text-[10.5px] text-muted-foreground">
            {t('{up} of {total} up', { up, total: ports.length })}
          </span>
        )}
      </div>
      <div
        className="flex flex-col items-center rounded-lg bg-muted/80 shadow-[inset_0_1px_2px_rgb(15_23_42/0.08)]"
        style={{ height: bayH, paddingTop: PANEL.bayPadTop, gap: PANEL.rowGap }}
      >
        {ports.length === 0 ? (
          <span className="flex h-full items-center pb-1 text-[11px] text-muted-foreground">{empty}</span>
        ) : panelRows(ports).map((row, index) => (
          <div key={index} className="flex" style={{ gap: PANEL.portGap }}>
            {row.map(port => (
              <div key={port.id} className="flex flex-col items-center" style={{ width: port.kind === 'fiber' ? PANEL.fiberW : PANEL.jackW }}>
                {port.kind === 'fiber' ? <Cage port={port} /> : <Jack port={port} />}
                <span
                  className="w-full truncate text-center text-[10px] text-muted-foreground"
                  style={{ marginTop: PANEL.labelGap, lineHeight: `${PANEL.labelH}px` }}
                >
                  {port.name || port.id}
                </span>
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  )
}

const HOUSING = '#334155'
const CAVITY = '#0b1220'
const UNLIT = '#4b5563'
const GREEN = '#22c55e'
const AMBER = '#f59e0b'
const GOLD = '#d4a72c'

/** How tall a lamp is, and how far its halo reaches past it when lit. */
const LAMP_H = 2.4
const HALO = 0.8

/** A lamp: a small rounded light, with a soft halo when it is lit. */
function Lamp({ x, y, width, colour }: { x: number; y: number; width: number; colour: string }) {
  return (
    <>
      {colour !== UNLIT && (
        <rect x={x - HALO} y={y - HALO} width={width + HALO * 2} height={LAMP_H + HALO * 2} rx={1.6} fill={colour} opacity={0.35} />
      )}
      <rect x={x} y={y} width={width} height={LAMP_H} rx={0.8} fill={colour} />
    </>
  )
}

/**
 * An RJ45 jack, 30 × 24, latch up: the narrow latch slot on top, the wide
 * opening under it with the eight contacts along its bottom, and the two
 * lamps either side of the latch slot — link on the left, speed on the right.
 *
 * Everything is laid out from the vertical centre line `c`, and the opening
 * sits with the same margin above the slot as below the opening, so the jack
 * is centred and balanced by construction. The lamps share the slot's band
 * and line up with the opening's outer edges.
 */
function Jack({ port }: { port: PanelPort }) {
  const w = PANEL.jackW
  const h = PANEL.jackH
  const c = w / 2
  const margin = 4 // housing edge to cavity, top and bottom
  const half = 9 // half the opening's width
  const latch = 4 // half the latch slot's width
  const slotTop = margin
  const bodyTop = 9
  const bodyBottom = h - margin
  // Each lamp, halo included, stays inside the opening's width below it and
  // clear of the latch slot.
  const lampW = 2.8
  const lampInset = 1.2
  const lampY = (slotTop + bodyTop) / 2 - LAMP_H / 2
  const pitch = 1.8
  const pin = 0.9
  const first = c - (7 * pitch + pin) / 2
  const speed = speedLamp(port)
  return (
    <svg width={w} height={h} viewBox={`0 0 ${w} ${h}`} aria-label={port.name || port.id} role="img">
      <rect x={0.5} y={0.5} width={w - 1} height={h - 1} rx={3} fill={HOUSING} />
      <path
        d={`M${c - latch} ${slotTop} H${c + latch} V${bodyTop} H${c + half} V${bodyBottom} H${c - half} V${bodyTop} H${c - latch} Z`}
        fill={CAVITY}
      />
      <Lamp x={c - half + lampInset} y={lampY} width={lampW} colour={port.linkUp === true ? GREEN : UNLIT} />
      <Lamp x={c + half - lampInset - lampW} y={lampY} width={lampW} colour={speed === 'fast' ? GREEN : speed === 'slow' ? AMBER : UNLIT} />
      {Array.from({ length: 8 }, (_, i) => (
        <rect key={i} x={first + i * pitch} y={bodyBottom - 4} width={pin} height={3.2} fill={GOLD} />
      ))}
    </svg>
  )
}

/**
 * A fiber port, 40 × 24: an empty cage — a metal frame round a dark slot. A
 * cage has no lamps of its own; the panel beside it usually has one, for the
 * link, so there is one, above the slot.
 */
function Cage({ port }: { port: PanelPort }) {
  const w = PANEL.fiberW
  const h = PANEL.jackH
  const c = w / 2
  const frame = 13 // half the frame's width
  return (
    <svg width={w} height={h} viewBox={`0 0 ${w} ${h}`} aria-label={port.name || port.id} role="img">
      <rect x={0.5} y={0.5} width={w - 1} height={h - 1} rx={3} fill={HOUSING} />
      <Lamp x={c - 2.4} y={4.3} width={4.8} colour={port.linkUp === true ? GREEN : UNLIT} />
      <rect x={c - frame} y={9} width={frame * 2} height={11} rx={1.5} fill="#94a3b8" />
      <rect x={c - frame + 1.5} y={10.5} width={frame * 2 - 3} height={8} rx={0.8} fill={CAVITY} />
    </svg>
  )
}

/** How wide a panel's header is, measured in the font it will be drawn in. */
export function headWidth(name: string, detail: string | undefined, stat: string | undefined): number {
  const measure = textMeasurer()
  let width = measure(name, '600 13px')
  if (detail !== undefined) width += 8 + measure(detail, '400 11px')
  if (stat !== undefined) width += 12 + 8 + measure(stat, '400 10.5px')
  // Canvas and layout disagree by a few pixels across fonts and CJK fallback;
  // a little room is cheaper than a clipped count.
  return width + 16
}

let canvas: CanvasRenderingContext2D | null | undefined
function textMeasurer(): (text: string, font: string) => number {
  if (canvas === undefined) canvas = typeof document === 'undefined' ? null : document.createElement('canvas').getContext('2d')
  const family = typeof document === 'undefined' ? 'sans-serif' : getComputedStyle(document.body).fontFamily || 'sans-serif'
  return (text, font) => {
    if (canvas === null || canvas === undefined) return text.length * 7.5
    canvas.font = `${font} ${family}`
    return canvas.measureText(text).width
  }
}
