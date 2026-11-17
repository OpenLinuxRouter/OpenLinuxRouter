import { useId } from 'react'

import { t, useLocale } from '@/lib/i18n'

import type { Port } from '@/features/devices/types'

import {
  CAGE_BAIL,
  CAGE_MOUTH,
  CAGE_W,
  FOOT,
  FRAME_H,
  FRAME_W,
  LAMP_GAP,
  LAMP_H,
  LAMP_W,
  MOUTH_PATH,
  PRONG_BODY,
  PRONG_TIP,
  faceLayout,
  rowLeft,
  rowTop,
  type FacePort,
} from './face'
import { FORM_CHROME, type DeviceForm } from './form'

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
  form = 'switch',
  className,
}: {
  ports?: readonly Port[]
  /** How many addresses each port learned, when that is known. */
  addresses?: ReadonlyMap<string, number>
  /** What kind of box this is; it changes the chassis, never the ports. */
  form?: DeviceForm
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
  const up = faces.filter(port => port.linkUp === true).length
  const total = faces.filter(port => port.kind === 'ethernet' || port.kind === 'fiber').length
  if (form !== 'switch' && form !== 'generic') {
    return (
      <DeviceBox
        faces={faces}
        addresses={addresses}
        form={form}
        uid={uid}
        className={className}
        label={t('{up} of {total} ports up', { up, total })}
      />
    )
  }
  const chrome = FORM_CHROME[form]
  const top = chrome.top ?? 0
  const layout = faceLayout(faces, chrome)
  if (layout.rows.length === 0) return null

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
      viewBox={`0 ${-top} ${layout.width} ${layout.height + top + FOOT}`}
      className="block h-auto w-full"
      role="img"
      aria-label={label}
      preserveAspectRatio="xMidYMid meet"
    >
      <title>{label}</title>
      <clipPath id={`mouth${uid}`}><path d={MOUTH_PATH} /></clipPath>
      <clipPath id={`cage${uid}`}><path d={CAGE_MOUTH} /></clipPath>

      <Chassis form={form} width={layout.width} height={layout.height} lead={chrome.lead ?? 0} top={top} />

      {layout.rows.map((row, index) => (
        <g key={index} transform={`translate(0 ${rowTop(index, chrome) - LAMP_H - LAMP_GAP})`}>
          {row.slots.map(slot => {
            const lit = slot.port.linkUp === true
            const centre = rowLeft(chrome) + slot.x + slot.width / 2
            return (
              <g key={slot.port.id}>
                <Lamp x={centre} lit={lit} />
                <g transform={`translate(${rowLeft(chrome) + slot.x} ${LAMP_H + LAMP_GAP})`}>
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
            style={{ left: `${((rowLeft(chrome) + slot.x + slot.width / 2) / layout.width) * 100}%` }}
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

/**
 * The chassis under the ports, in the shape of the class the box belongs to.
 *
 * The differences are the ones you would see across a room: a switch is a flat
 * slab of ports, a fiber gateway is a small box with a lamp cluster, an access
 * point has antennas and a radio mark, and the machine that runs this router is
 * a vented little box with a power button. None of it is a claim about a model —
 * every port on the face still comes from the reading.
 */
function Chassis({
  form,
  width,
  height,
  lead,
  top,
}: {
  form: DeviceForm
  width: number
  height: number
  lead: number
  top: number
}) {
  const body = form === 'router' ? '#f6f6f7' : '#ffffff'
  const radius = form === 'ap' ? 18 : form === 'router' ? 6 : form === 'ont' ? 8 : 9
  return (
    <g>
      {form === 'ap' && (
        /* Antennas first, so the chassis covers their roots. */
        <g fill="#ececef" stroke={PART_EDGE} strokeWidth="1.5">
          <rect x={lead + 26} y={-top - 2} width={16} height={top + 10} rx={8} />
          <rect x={lead + 62} y={-top - 2} width={16} height={top + 10} rx={8} />
        </g>
      )}
      <rect x={0.75} y={0.75} width={width - 1.5} height={height - 1.5} rx={radius} fill={body} stroke={BODY_EDGE} strokeWidth={1.5} />
      {form === 'router' && (
        /* A machined top edge and a shadow line: brushed cases read by their edge. */
        <rect x={radius + 6} y={1.6} width={width - radius * 2 - 12} height={1.4} rx={0.7} fill="#c9c9cf" opacity={0.7} />
      )}
      {form === 'ont' && (
        /* The base it stands on. */
        <rect x={1} y={height - 7} width={width - 2} height={6} rx={3} fill={PART} />
      )}
      <ClassMarks form={form} lead={lead} height={height} />
    </g>
  )
}

/** The class mark in the panel to the left of the ports. Nothing about state. */
function ClassMarks({ form, lead, height }: { form: DeviceForm; lead: number; height: number }) {
  if (lead === 0) return null
  const cx = lead / 2 + 6
  const cy = height / 2
  const mark = '#c7c7cd'
  const soft = '#e8e8ec'
  if (form === 'ont') {
    // Three status windows, unlit: the shape of a gateway's front panel.
    return (
      <g>
        {[0, 1, 2].map(index => (
          <rect key={index} x={cx - 13} y={cy - 33 + index * 23} width={26} height={15} rx={4}
            fill={soft} stroke={mark} strokeWidth={1.6} />
        ))}
      </g>
    )
  }
  if (form === 'ap') {
    // A radio: a dot and two arcs.
    return (
      <g fill="none" stroke={mark} strokeWidth="3" strokeLinecap="round">
        <circle cx={cx - 2} cy={cy + 9} r={2.4} fill={mark} stroke="none" />
        <path d={`M${cx - 12} ${cy - 1} A13 13 0 0 1 ${cx + 8} ${cy - 1}`} />
        <path d={`M${cx - 19} ${cy - 11} A21 21 0 0 1 ${cx + 15} ${cy - 11}`} />
      </g>
    )
  }
  if (form === 'router') {
    // A power button, and the vents beside it.
    return (
      <g>
        <circle cx={cx - 4} cy={cy} r={12} fill="none" stroke={mark} strokeWidth="3" />
        <path d={`M${cx - 4} ${cy - 9} L${cx - 4} ${cy - 2}`} stroke={mark} strokeWidth="3" strokeLinecap="round" />
        {[0, 1, 2].map(index => (
          <rect key={index} x={cx + 12} y={cy - 15 + index * 12} width={18} height={5} rx={2.5} fill={mark} opacity={0.8} />
        ))}
      </g>
    )
  }
  return null
}

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

/**
 * A box drawn as a product: a front panel with its ports, the top face above it
 * and the side falling away to the left, plus the one mark its class is known
 * for — three status windows on a gateway, antennas and a radio on an access
 * point, a power button and vents on the small machine that runs this router.
 *
 * Only the switch is a flat row of ports. The others are drawn the way you would
 * recognise them on a shelf, because that is how a person finds them: the port
 * count is still the reading, but the shape says which box to walk over to.
 */
const BOX = {
  scale: 0.4,
  pad: 16,
  gap: 7,
  depth: 24,
  panel: 17,
  lead: { ont: 40, ap: 34, router: 44, switch: 0, generic: 0 } as Record<DeviceForm, number>,
  minWidth: { ont: 250, ap: 196, router: 200, switch: 0, generic: 0 } as Record<DeviceForm, number>,
  antenna: 42,
}

/** The width at which one shield is MAX_PORT across, whatever the form. */
function naturalWidth(width: number, scale: number): number {
  return width * (MAX_PORT / (FRAME_W * scale))
}

function DeviceBox({
  faces,
  form,
  uid,
  className,
  label,
}: {
  faces: readonly FacePort[]
  addresses?: ReadonlyMap<string, number>
  form: DeviceForm
  uid: string
  className?: string
  label: string
}) {
  const s = BOX.scale
  const n = faces.filter(port => port.kind === 'ethernet' || port.kind === 'fiber').length
  if (n === 0) return null
  const physical = faces.filter(port => port.kind === 'ethernet' || port.kind === 'fiber')
  const lead = BOX.lead[form] ?? 0
  const pitch = (FRAME_W + BOX.gap) * s
  const inner = lead + n * FRAME_W * s + (n - 1) * BOX.gap * s
  const w = Math.max(inner + BOX.pad * 2, BOX.minWidth[form] ?? 0)
  const portTop = BOX.panel + (LAMP_H + LAMP_GAP) * s
  const h = portTop + FRAME_H * s + BOX.panel
  const left = (w - inner) / 2 + lead
  const antennas = form === 'ap'
  const totalH = h
  const antennaTop = antennas ? BOX.antenna : 0

  const face = form === 'router' ? '#f4f4f5' : '#ffffff'
  const radius = form === 'ap' ? 12 : form === 'ont' ? 8 : 6

  return (
    <div className={className} style={{ maxWidth: naturalWidth(w, s) }}>
      <svg
        viewBox={`0 ${-antennaTop} ${w} ${totalH + antennaTop}`}
        className="block h-auto w-full"
        role="img"
        aria-label={label}
        preserveAspectRatio="xMidYMid meet"
      >
        <title>{label}</title>
        <clipPath id={`mouth${uid}`}><path d={MOUTH_PATH} /></clipPath>
        <clipPath id={`cage${uid}`}><path d={CAGE_MOUTH} /></clipPath>

        {/* Antennas are part of the outline: two strokes on the body, not a
            perspective drawing of one. Nothing here is three-dimensional. */}
        {antennas && (
          <g fill="#f4f4f5" stroke={PART_EDGE} strokeWidth={1.4}>
            <rect x={w * 0.15} y={-BOX.antenna} width={19} height={BOX.antenna + 12} rx={9.5} />
            <rect x={w * 0.45} y={-BOX.antenna + 9} width={19} height={BOX.antenna + 3} rx={9.5} />
            <rect x={w * 0.75} y={-BOX.antenna} width={19} height={BOX.antenna + 12} rx={9.5} />
          </g>
        )}

        <rect x={0.75} y={0.75} width={w - 1.5} height={h - 1.5} rx={radius} fill={face} stroke={BODY_EDGE} strokeWidth={1.5} />
        {form === 'ont' && (
          /* the plinth a gateway sits on */
          <rect x={1} y={h - 7} width={w - 2} height={6} rx={3} fill={PART} />
        )}
        {form === 'router' && (
          /* vents, on the front panel where a small machine has them */
          <g stroke="#d6d6db" strokeWidth={2} strokeLinecap="round">
            {[0, 1, 2].map(index => (
              <line key={index} x1={w * 0.42} y1={h * 0.2 + index * 7} x2={w * 0.86} y2={h * 0.2 + index * 7} />
            ))}
          </g>
        )}

        <ClassMarks form={form} lead={lead} height={h} />
        {physical.map((port, index) => {
          const x = left + index * pitch
          const cx = x + (FRAME_W / 2) * s
          return (
            <g key={port.id}>
              <g transform={`translate(${cx} ${BOX.panel}) scale(${s})`}>
                <g transform={`translate(${-FRAME_W / 2} 0)`}>
                  <rect x={-11.5} y={0} width={LAMP_W} height={LAMP_H} rx={3.5}
                    fill={port.linkUp === true ? LIT : PART}
                    stroke={port.linkUp === true ? LIT_EDGE : PART_EDGE} strokeWidth={2} />
                </g>
              </g>
              <g transform={`translate(${x} ${portTop}) scale(${s})`}>
                <title>{describe(port)}</title>
                {port.kind === 'fiber' ? <Cage uid={uid} /> : <Shield uid={uid} />}
              </g>
            </g>
          )
        })}
      </svg>
      {/* The names go under the box in HTML, at the same size as the switch's:
          a label has to stay readable when the drawing is a thumbnail. */}
      <div className="relative mt-1 h-4 text-[10px] leading-4">
        {physical.map((port, index) => {
          const cx = left + index * pitch + (FRAME_W / 2) * s
          return (
            <span
              key={port.id}
              className="absolute flex -translate-x-1/2 items-center gap-1 whitespace-nowrap tabular-nums"
              style={{ left: `${(cx / w) * 100}%` }}
            >
              <span className="text-muted-foreground">{port.id}</span>
              {port.clients !== undefined && port.clients > 0 && (
                port.clients === 1
                  ? <span className="size-1.5 shrink-0 rounded-full bg-foreground/45" title={t('{count} address learned here', { count: 1 })} />
                  : <span className="shrink-0 rounded bg-muted px-1 text-[9px] leading-4 text-foreground/75" title={t('{count} addresses learned here', { count: port.clients })}>
                      {port.clients}
                    </span>
              )}
            </span>
          )
        })}
      </div>
    </div>
  )
}
