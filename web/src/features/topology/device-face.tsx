import { useId } from 'react'

import { t, useLocale } from '@/lib/i18n'

import type { Port } from '@/features/devices/types'

import {
  CAGE_BAIL,
  CAGE_MOUTH,
  CAGE_W,
  FOOT,
  FRAME_W,
  LAMP_GAP,
  LAMP_H,
  MOUTH_PATH,
  PAD,
  PRONG_BODY,
  PRONG_TIP,
  faceLayout,
  rowTop,
  type FacePort,
} from './face'

/**
 * The box, drawn.
 *
 * A reading says which ports have a carrier, and until now the page said it in
 * a sentence. A sentence cannot show *which* port, and "which port" is the whole
 * of what somebody standing in front of the switch is asking. So the page draws
 * the hardware: a light chassis, one shield per RJ45 port, a cage for each fiber
 * port, and a lamp above every one.
 *
 * Flat on purpose. Two tones and one accent, no gradients, no shadows, no
 * reflection: at the size a card gives it, shading reads as dirt, and a box that
 * is supposed to say "eight of nine ports are live" should not need a light rig
 * to say it. What is left is the shape, which is the part that carries meaning —
 * the port outline is the traced one, so the picture is still that model's
 * connector and not a generic glyph.
 *
 * The only things the drawing claims are the things the reading says. A port the
 * box listed gets a shield whether or not it is linked; its lamp is lit only
 * where the box reported a carrier. A port the box did not list is not drawn at
 * all, so the row on screen is exactly the row in the answer — and a bridge, a
 * VLAN or a radio is filtered out before it gets here, because those are not
 * holes a cable goes into.
 */
export function DeviceFace({
  ports,
  addresses,
  className,
}: {
  ports?: readonly Port[]
  /** How many addresses each port learned, when that is known. */
  addresses?: ReadonlyMap<string, number>
  className?: string
}) {
  useLocale()
  const uid = useId().replace(/[^a-zA-Z0-9]/g, '')
  const faces: FacePort[] = (ports ?? []).map(port => ({
    id: port.id,
    name: port.name,
    kind: port.kind,
    linkUp: port.link_up,
    speedMbps: port.speed_mbps,
    clients: addresses?.get(port.id),
  }))
  const layout = faceLayout(faces)
  if (layout.rows.length === 0) return null

  const up = faces.filter(port => port.linkUp === true).length
  const total = faces.filter(port => port.kind === 'ethernet' || port.kind === 'fiber').length
  const label = t('{up} of {total} ports up', { up, total })

  return (
    // A port has a natural size: past it the face stops growing and centres,
    // so a four-port box is drawn as the smaller thing it is instead of having
    // its jacks blown up to fill the card.
    <div
      className={className}
      style={{ maxWidth: layout.width * (MAX_PORT / FRAME_W) }}
    >
    <svg
      viewBox={`0 0 ${layout.width} ${layout.height + FOOT}`}
      className="block h-auto w-full"
      role="img"
      aria-label={label}
      preserveAspectRatio="xMidYMid meet"
    >
      <title>{label}</title>
      <clipPath id={`mouth${uid}`}><path d={MOUTH_PATH} /></clipPath>
      <clipPath id={`cage${uid}`}><path d={CAGE_MOUTH} /></clipPath>

      <Chassis width={layout.width} height={layout.height} />

      {layout.rows.map((row, index) => (
        <g key={index} transform={`translate(0 ${rowTop(index) - LAMP_H - LAMP_GAP})`}>
          {row.slots.map(slot => {
            const lit = slot.port.linkUp === true
            const centre = PAD + slot.x + slot.width / 2
            return (
              <g key={slot.port.id}>
                <Lamp x={centre} lit={lit} />
                <g transform={`translate(${PAD + slot.x} ${LAMP_H + LAMP_GAP})`}>
                  <title>{describe(slot.port)}</title>
                  {slot.fiber
                    ? <Cage uid={uid} />
                    : <Shield uid={uid} />}
                </g>
              </g>
            )
          })}
        </g>
      ))}
    </svg>
    {/* The numbers under the shields are the box's own names for its ports,
        drawn in HTML rather than in the picture: type has to stay legible when
        the row of ports is only a thumbnail, and SVG text would shrink with
        it. One row of ports only — a second row is too dense to letter. */}
    {layout.rows.length === 1 && (
      <div className="relative mt-1 h-4 text-[10px] leading-4">
        {layout.rows[0].slots.map(slot => (
          <span
            key={slot.port.id}
            className="absolute flex -translate-x-1/2 items-center gap-1 whitespace-nowrap tabular-nums"
            style={{ left: `${((PAD + slot.x + slot.width / 2) / layout.width) * 100}%` }}
          >
            <span className="text-muted-foreground">{slot.port.id}</span>
            {/* Filled means something is on the port; a bare dot for one, a
                count when the table put more than one address there. */}
            {slot.port.clients !== undefined && slot.port.clients > 0 && (
              slot.port.clients === 1
                ? <span className="size-1.5 shrink-0 rounded-full bg-foreground/45" title={t('{count} address learned here', { count: 1 })} />
                : <span className="shrink-0 rounded bg-muted px-1 text-[9px] leading-4 text-foreground/75" title={t('{count} addresses learned here', { count: slot.port.clients })}>
                    {slot.port.clients}
                  </span>
            )}
          </span>
        ))}
      </div>
    )}
    </div>
  )
}

/** The words a hovering pointer gets for one port: its name, then what it says. */
function describe(port: FacePort): string {
  const parts = [port.name || port.id]
  if (port.linkUp === true) parts.push(t('link up'))
  else if (port.linkUp === false) parts.push(t('no link'))
  else parts.push(t('link not reported'))
  if (port.linkUp === true && port.speedMbps !== undefined) parts.push(`${port.speedMbps} Mbps`)
  return parts.join(' · ')
}

/** The widest a port is ever drawn, in pixels. */
const MAX_PORT = 44

const BODY = '#ffffff'
const BODY_EDGE = '#e4e4e7'
const PART = '#f4f4f5'
const PART_EDGE = '#dcdcdf'
const HOLE = '#3f3f46'
const HOLE_SHADE = '#34343a'
const HOLE_SHELF = '#4a4a52'
const GOLD = '#d3b183'
const GOLD_TIP = '#efe0c2'
const LIT = '#22c55e'
const LIT_EDGE = '#16a34a'

function Chassis({ width, height }: { width: number; height: number }) {
  return (
    <g>
      <rect x={0.75} y={0.75} width={width - 1.5} height={height - 1.5} rx={9} fill={BODY} stroke={BODY_EDGE} strokeWidth={1.5} />
    </g>
  )
}

/** One RJ45 shield: the traced port, at the size the row gave it. */
function Shield({ uid }: { uid: string }) {
  return (
    <g>
      <rect x={0.75} y={0.75} width={144.5} height={118.5} rx={6} fill={PART} stroke={PART_EDGE} strokeWidth={1.5} />
      <path d={MOUTH_PATH} fill={HOLE} />
      {/* Two flat tones inside the opening: the keying slot above, the shelf the
          prongs stand on below. Depth without a single gradient. */}
      <g clipPath={`url(#mouth${uid})`}>
        <rect x={18} y={0} width={118} height={31} fill={HOLE_SHADE} />
        <rect x={18} y={61} width={118} height={44} fill={HOLE_SHELF} />
        <rect x={18} y={103} width={118} height={2} fill={GOLD_TIP} opacity={0.5} />
      </g>
      <path d={PRONG_BODY} fill={GOLD} />
      <path d={PRONG_TIP} fill={GOLD_TIP} />
    </g>
  )
}

/** One SFP cage: a chamfered opening with its latch bail. */
function Cage({ uid }: { uid: string }) {
  return (
    <g>
      <rect x={0.75} y={0.75} width={CAGE_W - 1.5} height={118.5} rx={6} fill={PART} stroke={PART_EDGE} strokeWidth={1.5} />
      <path d={CAGE_MOUTH} fill={HOLE} />
      <g clipPath={`url(#cage${uid})`}>
        <rect x={18} y={26} width={160} height={12} fill={HOLE_SHADE} />
        <path d={CAGE_BAIL} fill="none" stroke={PART_EDGE} strokeWidth={2.4} strokeLinejoin="round" />
      </g>
    </g>
  )
}

/**
 * The lamp above a port. Unlit is an empty window, not an absent one: the window
 * is hardware, the light is the reading, and a box that never answered must not
 * look like a box with nothing plugged in.
 */
function Lamp({ x, lit }: { x: number; lit: boolean }) {
  return (
    <rect
      x={x - 11.5}
      y={0}
      width={23}
      height={17}
      rx={3.5}
      fill={lit ? LIT : '#eeeef0'}
      stroke={lit ? LIT_EDGE : PART_EDGE}
      strokeWidth={1.5}
    />
  )
}
