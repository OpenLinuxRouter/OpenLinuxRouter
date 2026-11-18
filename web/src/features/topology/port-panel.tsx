import { t, useLocale } from '@/lib/i18n'
import type { ReactNode } from 'react'

import { PANEL, panelRows, speedLamp, type PanelGeometry, type PanelPort } from './panel'

/**
 * A box on the map: its name across the top, its ports in a bay underneath.
 *
 * Every port is drawn the same whatever its state — a jack does not change
 * colour when the cable comes out. State is in the two lamps built into the
 * jack, as on the hardware: the left one is the link, the right one the speed
 * (green at a gigabit and up, amber below, dark when the box did not say).
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

/** The two lamps in the top corners of a jack or cage. */
function Lamps({ port, width }: { port: PanelPort; width: number }) {
  const speed = speedLamp(port)
  const link = port.linkUp === true ? GREEN : UNLIT
  const rate = speed === 'fast' ? GREEN : speed === 'slow' ? AMBER : UNLIT
  return (
    <>
      <Lamp x={3} colour={link} />
      <Lamp x={width - 8} colour={rate} />
    </>
  )
}

function Lamp({ x, colour }: { x: number; colour: string }) {
  const lit = colour !== UNLIT
  return (
    <>
      {lit && <rect x={x - 1} y={1.5} width={7} height={5} rx={2} fill={colour} opacity={0.35} />}
      <rect x={x} y={2.5} width={5} height={3} rx={1} fill={colour} />
    </>
  )
}

/**
 * An RJ45 jack, 30 × 24. The opening and its eight contacts are laid out from
 * the jack's centre line, so they are centred by construction.
 */
function Jack({ port }: { port: PanelPort }) {
  const w = PANEL.jackW
  const h = PANEL.jackH
  const c = w / 2
  const pitch = 1.8
  const pin = 0.9
  const first = c - (7 * pitch + pin) / 2
  return (
    <svg width={w} height={h} viewBox={`0 0 ${w} ${h}`} aria-label={port.name || port.id} role="img">
      <rect x={0.5} y={0.5} width={w - 1} height={h - 1} rx={3} fill={HOUSING} />
      <Lamps port={port} width={w} />
      <path d={`M${c - 9} 7.5 H${c + 9} V17 H${c + 4} V20.5 H${c - 4} V17 H${c - 9} Z`} fill={CAVITY} />
      {Array.from({ length: 8 }, (_, i) => (
        <rect key={i} x={first + i * pitch} y={8.3} width={pin} height={3.2} fill="#d4a72c" />
      ))}
    </svg>
  )
}

/** A fiber cage, 44 × 24: a metal frame with two LC sleeves, centred the same way. */
function Cage({ port }: { port: PanelPort }) {
  const w = PANEL.fiberW
  const h = PANEL.jackH
  const c = w / 2
  return (
    <svg width={w} height={h} viewBox={`0 0 ${w} ${h}`} aria-label={port.name || port.id} role="img">
      <rect x={0.5} y={0.5} width={w - 1} height={h - 1} rx={3} fill={HOUSING} />
      <Lamps port={port} width={w} />
      <rect x={c - 16} y={7} width={32} height={14} rx={1.5} fill="#94a3b8" />
      <rect x={c - 14} y={9} width={28} height={10} rx={1} fill={CAVITY} />
      <rect x={c - 10} y={11} width={8} height={6} rx={1} fill="#1e293b" stroke="#64748b" strokeWidth={0.6} />
      <rect x={c + 2} y={11} width={8} height={6} rx={1} fill="#1e293b" stroke="#64748b" strokeWidth={0.6} />
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
