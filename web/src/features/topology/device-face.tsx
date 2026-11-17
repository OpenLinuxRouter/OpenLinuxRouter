import { useId } from 'react'

import { t, useLocale } from '@/lib/i18n'

import type { Port } from '@/features/devices/types'

import {
  CAGE_BAIL,
  CAGE_MOUTH,
  CAGE_W,
  CROWN_PATH,
  FOOT,
  LAMP_GAP,
  LAMP_H,
  LAMP_W,
  MOUTH_PATH,
  PAD,
  PRONG_BODY,
  PRONG_SHADE,
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
 * the hardware: a dark brushed chassis, one chrome shield per RJ45 port, a cage
 * for each fiber port, and a lamp above every one.
 *
 * The only things the drawing claims are the things the reading says. A port the
 * box listed gets a shield whether or not it is linked; its lamp is lit only
 * where the box reported a carrier. A port the box did not list is not drawn at
 * all, so the row on screen is exactly the row in the answer — and a bridge, a
 * VLAN or a radio is filtered out before it gets here, because those are not
 * holes a cable goes into.
 *
 * Everything is vector and drawn at the traced picture's own scale, so the same
 * markup is a thumbnail on a card and a legible port at 4x.
 */
export function DeviceFace({ ports, className }: { ports?: readonly Port[]; className?: string }) {
  useLocale()
  const uid = useId().replace(/[^a-zA-Z0-9]/g, '')
  const faces: FacePort[] = (ports ?? []).map(port => ({
    id: port.id,
    name: port.name,
    kind: port.kind,
    linkUp: port.link_up,
    speedMbps: port.speed_mbps,
  }))
  const layout = faceLayout(faces)
  if (layout.rows.length === 0) return null

  const label = t('{up} of {total} ports up', {
    up: faces.filter(port => port.linkUp === true).length,
    total: faces.filter(port => port.kind === 'ethernet' || port.kind === 'fiber').length,
  })

  return (
    <svg
      viewBox={`0 0 ${layout.width} ${layout.height + FOOT}`}
      className={className ?? 'block h-auto w-full'}
      role="img"
      aria-label={label}
      preserveAspectRatio="xMidYMid meet"
    >
      <title>{label}</title>
      <Defs uid={uid} />
      <Chassis uid={uid} width={layout.width} height={layout.height} />

      {layout.rows.map((row, index) => (
        <g key={index} transform={`translate(0 ${rowTop(index) - LAMP_H - LAMP_GAP})`}>
          {row.slots.map(slot => {
            const lit = slot.port.linkUp === true
            const centre = PAD + slot.x + slot.width / 2
            return (
              <g key={slot.port.id}>
                <Lamp uid={uid} x={centre} lit={lit} />
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

function Defs({ uid }: { uid: string }) {
  return (
    <defs>
      {/* The chassis: anodised, lighter where the light falls on the top edge. */}
      <linearGradient id={`body${uid}`} x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stopColor="#63676b" />
        <stop offset=".05" stopColor="#585c60" />
        <stop offset=".5" stopColor="#4d5053" />
        <stop offset=".93" stopColor="#43464a" />
        <stop offset="1" stopColor="#36393c" />
      </linearGradient>
      <linearGradient id={`edges${uid}`} x1="0" y1="0" x2="1" y2="0">
        <stop offset="0" stopColor="#000" stopOpacity=".45" />
        <stop offset=".05" stopColor="#000" stopOpacity="0" />
        <stop offset=".95" stopColor="#000" stopOpacity="0" />
        <stop offset="1" stopColor="#000" stopOpacity=".45" />
      </linearGradient>
      {/* Brushed aluminium: vertical hairlines, barely there. */}
      <pattern id={`brush${uid}`} width="2.5" height="6" patternUnits="userSpaceOnUse">
        <rect width=".8" height="6" fill="#fff" opacity=".022" />
        <rect x="1.5" width=".8" height="6" fill="#000" opacity=".03" />
      </pattern>

      {/* The chrome shield around a port, and the light that plays across it. */}
      <linearGradient id={`frameV${uid}`} x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stopColor="#d3ccc4" />
        <stop offset=".18" stopColor="#bdb6ae" />
        <stop offset=".55" stopColor="#a8a199" />
        <stop offset=".86" stopColor="#948d86" />
        <stop offset="1" stopColor="#c4bcb4" />
      </linearGradient>
      <linearGradient id={`frameH${uid}`} x1="0" y1="0" x2="1" y2="0">
        <stop offset="0" stopColor="#000" stopOpacity=".2" />
        <stop offset=".12" stopColor="#fff" stopOpacity=".14" />
        <stop offset=".5" stopColor="#fff" stopOpacity=".02" />
        <stop offset=".9" stopColor="#000" stopOpacity=".07" />
        <stop offset="1" stopColor="#000" stopOpacity=".3" />
      </linearGradient>

      {/* The chamber: black plastic, with a little light bouncing into the mouth. */}
      <linearGradient id={`chamber${uid}`} x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stopColor="#0b0a0a" />
        <stop offset=".45" stopColor="#171616" />
        <stop offset=".8" stopColor="#101010" />
        <stop offset="1" stopColor="#0a0909" />
      </linearGradient>
      <radialGradient id={`bounce${uid}`} cx=".28" cy=".12" r=".85">
        <stop offset="0" stopColor="#4b4f54" stopOpacity=".2" />
        <stop offset=".4" stopColor="#2c2f32" stopOpacity=".06" />
        <stop offset="1" stopColor="#000" stopOpacity="0" />
      </radialGradient>
      <linearGradient id={`crownShade${uid}`} x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stopColor="#000" stopOpacity=".9" />
        <stop offset="1" stopColor="#000" stopOpacity="0" />
      </linearGradient>

      {/* Gold, lit at the tip because that is where the plug touches it. */}
      <linearGradient id={`prong${uid}`} x1="0" y1="1" x2="0" y2="0">
        <stop offset="0" stopColor="#fff0b8" />
        <stop offset=".12" stopColor="#e8c07a" />
        <stop offset=".45" stopColor="#a97f3c" />
        <stop offset=".78" stopColor="#6b4f24" />
        <stop offset="1" stopColor="#3d2c12" />
      </linearGradient>

      {/* A lit port: a lens, not a dot — bright core, coloured rim. */}
      <radialGradient id={`lens${uid}`} cx=".4" cy=".35" r=".75">
        <stop offset="0" stopColor="#f7ffd4" />
        <stop offset=".35" stopColor="#d6ff8e" />
        <stop offset=".72" stopColor="#57e158" />
        <stop offset="1" stopColor="#117f22" />
      </radialGradient>
      <radialGradient id={`glow${uid}`} cx=".5" cy=".5" r=".5">
        <stop offset="0" stopColor="#96ff8a" stopOpacity=".55" />
        <stop offset=".45" stopColor="#4ddd5a" stopOpacity=".22" />
        <stop offset="1" stopColor="#2bbb3a" stopOpacity="0" />
      </radialGradient>

      <clipPath id={`mouth${uid}`}><path d={MOUTH_PATH} /></clipPath>
      <clipPath id={`cage${uid}`}><path d={CAGE_MOUTH} /></clipPath>
    </defs>
  )
}

function Chassis({ uid, width, height }: { uid: string; width: number; height: number }) {
  return (
    <g>
      {/* The feet it stands on, then the body over them. */}
      <rect x={PAD + 14} y={height - 3} width={54} height={FOOT + 3} rx={4} fill="#08090a" />
      <rect x={width - PAD - 68} y={height - 3} width={54} height={FOOT + 3} rx={4} fill="#08090a" />

      <rect x="0" y="0" width={width} height={height} rx="9" fill={`url(#body${uid})`} />
      <rect x="0" y="0" width={width} height={height} rx="9" fill={`url(#brush${uid})`} />
      <rect x="0" y="0" width={width} height={height} rx="9" fill={`url(#edges${uid})`} />
      {/* The machined top edge catches the light; the bottom edge falls away. */}
      <rect x="9" y="1.2" width={width - 18} height="1.6" rx=".8" fill="#9aa1a8" opacity=".5" />
      <rect x="9" y="2.8" width={width - 18} height="1" rx=".5" fill="#000" opacity=".35" />
      <rect x="9" y={height - 2.8} width={width - 18} height="1.6" rx=".8" fill="#000" opacity=".6" />
      <rect x="0" y="0" width={width} height={height} rx="9" fill="none" stroke="#000" strokeOpacity=".5" strokeWidth="1" />
    </g>
  )
}

/** One RJ45 shield: the traced port, at the size the row gave it. */
function Shield({ uid }: { uid: string }) {
  return (
    <g>
      <rect x={1} y={1} width={144} height={118} rx={4.4} fill={`url(#frameV${uid})`} />
      <rect x={1} y={1} width={144} height={118} rx={4.4} fill={`url(#frameH${uid})`} />
      {/* The shield's own dark rim, then the polished inner edge a plug passes. */}
      <rect x={1} y={1} width={144} height={118} rx={4.4} fill="none" stroke="#1c1c1c" strokeOpacity={0.6} strokeWidth={2.2} />
      <rect x={3.4} y={3.4} width={139.2} height={113.2} rx={3} fill="none" stroke="#fffdf7" strokeOpacity={0.22} strokeWidth={1} />

      <path d={MOUTH_PATH} fill={`url(#chamber${uid})`} />
      <g clipPath={`url(#mouth${uid})`}>
        <rect x={11} y={0} width={130} height={108} fill={`url(#bounce${uid})`} />
        <rect x={11} y={2} width={130} height={16} fill={`url(#crownShade${uid})`} />
        {/* The shelf the prongs stand on: a front edge that catches light, and
            a floor that bounces a little back up. Without them the chamber
            reads as one flat hole. */}
        <rect x={20} y={60.4} width={114} height={1.2} fill="#9aa0a6" opacity={0.18} />
        <rect x={20} y={61.6} width={114} height={43} fill="#fff" opacity={0.015} />
        <rect x={20} y={100.6} width={116} height={4.4} fill="#9c9c98" opacity={0.16} />
      </g>
      <path d={MOUTH_PATH} fill="none" stroke="#000" strokeWidth={1.5} strokeOpacity={0.55} />
      {/* The lip of the mouth, drawn open so it does not cut across the chamber. */}
      <path d={CROWN_PATH} fill="none" stroke="#8f9499" strokeWidth={1} strokeOpacity={0.4} />

      <path d={PRONG_SHADE} fill="#000" opacity={0.5} />
      <path d={PRONG_BODY} fill={`url(#prong${uid})`} />
      <path d={PRONG_TIP} fill="#ffe1a0" opacity={0.85} />

      {/* The clips that hold the shield into the panel. */}
      <rect x={4} y={56} width={2} height={8} rx={1} fill="#8d8781" />
      <rect x={140} y={56} width={2} height={8} rx={1} fill="#8d8781" />
    </g>
  )
}

/** One SFP cage: a chamfered opening with its latch bail. */
function Cage({ uid }: { uid: string }) {
  return (
    <g>
      <rect x={1} y={1} width={CAGE_W - 2} height={118} rx={4.4} fill={`url(#frameV${uid})`} />
      <rect x={1} y={1} width={CAGE_W - 2} height={118} rx={4.4} fill={`url(#frameH${uid})`} />
      <rect x={1} y={1} width={CAGE_W - 2} height={118} rx={4.4} fill="none" stroke="#1c1c1c" strokeOpacity={0.6} strokeWidth={2.2} />
      <path d={CAGE_MOUTH} fill={`url(#chamber${uid})`} />
      <g clipPath={`url(#cage${uid})`}>
        <rect x={18} y={26} width={160} height={68} fill={`url(#bounce${uid})`} />
        <rect x={18} y={26} width={160} height={12} fill={`url(#crownShade${uid})`} />
        <path d={CAGE_BAIL} fill={`url(#frameV${uid})`} opacity={0.85} />
        <path d={CAGE_BAIL} fill="none" stroke="#5c5752" strokeWidth={0.8} />
        {/* The rails a module rides in. */}
        <rect x={40} y={68} width={106} height={2.4} rx={1.2} fill="#000" opacity={0.55} />
      </g>
      <path d={CAGE_MOUTH} fill="none" stroke="#000" strokeWidth={1.5} strokeOpacity={0.55} />
      <path d={CAGE_MOUTH} fill="none" stroke="#8f9499" strokeWidth={0.9} strokeOpacity={0.3} />
      <rect x={4} y={56} width={2} height={8} rx={1} fill="#8d8781" />
      <rect x={CAGE_W - 6} y={56} width={2} height={8} rx={1} fill="#8d8781" />
    </g>
  )
}

/**
 * The lamp above a port. Unlit is a dark window, not an absent one: the window
 * is hardware, the light is the reading, and a box that never answered must not
 * look like a box with nothing plugged in.
 */
function Lamp({ uid, x, lit }: { uid: string; x: number; lit: boolean }) {
  const left = x - LAMP_W / 2
  return (
    <g>
      {lit && <ellipse cx={x} cy={LAMP_H / 2} rx={LAMP_W * 1.1} ry={LAMP_H * 1.5} fill={`url(#glow${uid})`} />}
      {/* A recessed window in the panel: dark ring, thin bright lip below. */}
      <rect x={left - 1.2} y={-1.2} width={LAMP_W + 2.4} height={LAMP_H + 2.4} rx={3.6} fill="#232527" />
      <rect x={left - 1.2} y={LAMP_H + 0.6} width={LAMP_W + 2.4} height={1} rx={0.5} fill="#9aa0a6" opacity={0.5} />
      <rect x={left} y={0} width={LAMP_W} height={LAMP_H} rx={2.6} fill={lit ? `url(#lens${uid})` : '#3f4144'} />
      {lit ? (
        <rect x={left + 3} y={2} width={LAMP_W - 7} height={LAMP_H - 6} rx={2} fill="#eaffc9" opacity={0.9} />
      ) : (
        <>
          <rect x={left} y={0} width={LAMP_W} height={LAMP_H * 0.42} rx={2.4} fill="#2f3133" opacity={0.85} />
          <rect x={left + 1} y={0.8} width={LAMP_W - 2} height={1.2} rx={0.6} fill="#75797d" opacity={0.45} />
        </>
      )}
    </g>
  )
}
