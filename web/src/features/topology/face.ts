/**
 * The geometry of one switch face, and where its ports sit.
 *
 * The port outline is traced from a photograph of the reference switch and kept
 * in that picture's own coordinates, 146 x 120 units per port: a chrome shield,
 * a black chamber whose mouth is narrowed twice on the way to the keying slot,
 * eight gold prongs leaning out of the floor, and two retaining clips on the
 * sides. Drawing it at 1:1 in those units is what lets the same code render a
 * legible port at any size the layout gives it.
 *
 * Everything here is arithmetic on the reading. A port that reports a carrier
 * gets a lit lamp; a port that reports no carrier, or does not report at all,
 * gets a dark one. Nothing is invented for a port the box did not list, which is
 * why the count on the face is always the count in the answer.
 */

export interface FacePort {
  id: string
  name?: string
  kind: string
  linkUp?: boolean
  speedMbps?: number
  /** MAC addresses this port has learned, when the reading carried a table. */
  clients?: number
}

/** One drawn port: where it sits, how wide it is, and whether it takes a cage. */
export interface FaceSlot {
  port: FacePort
  x: number
  width: number
  fiber: boolean
}

export interface FaceRow {
  slots: FaceSlot[]
  /** Row width in port units, padding included. */
  width: number
}

export interface FaceLayout {
  rows: FaceRow[]
  width: number
  height: number
}

/** A port's own box, in the traced picture's units. */
export const FRAME_W = 146
export const FRAME_H = 120
/** The metal cage that takes an SFP module is wider than an RJ45 shield. */
export const CAGE_W = 196
/** Space between two shields, and the wider gap before a cage. */
export const GAP = 6
export const CAGE_GAP = 34
/** Margin inside the chassis, left and right of the row. */
export const PAD = 26
/** The lamp above a port: a small window in the chassis, wider than it is tall. */
export const LAMP_W = 23
export const LAMP_H = 17
export const LAMP_GAP = 12
/** Chassis margin above the lamps and below the shields. */
export const TOP = 16
export const BOTTOM = 26
/** How tall the feet are, and how far they show below the chassis. */
export const FOOT = 10
/** Ports per row. A 24-port switch really is two rows of twelve. */
export const ROW_MAX = 12

/**
 * Rows are physical ports only. A bridge, a VLAN or a radio is an interface the
 * box reports, not a hole a cable goes into, and drawing one as a jack would
 * claim a connector the hardware does not have.
 */
export function isPhysical(port: { kind: string }): boolean {
  return port.kind === 'ethernet' || port.kind === 'fiber'
}

export function isFiber(port: { kind: string }): boolean {
  return port.kind === 'fiber'
}

/**
 * Lay the physical ports out as the hardware carries them: one row up to
 * twelve, then the next row, cages treated as wider than shields.
 */
export function faceLayout(ports: readonly FacePort[]): FaceLayout {
  const physical = ports.filter(isPhysical)
  const rows: FaceRow[] = []
  for (let start = 0; start < physical.length; start += ROW_MAX) {
    const slots: FaceSlot[] = []
    let x = 0
    for (const port of physical.slice(start, start + ROW_MAX)) {
      const fiber = isFiber(port)
      const width = fiber ? CAGE_W : FRAME_W
      if (slots.length > 0) x += fiber || slots[slots.length - 1].fiber ? CAGE_GAP : GAP
      slots.push({ port, x, width, fiber })
      x += width
    }
    rows.push({ slots, width: x })
  }
  const width = Math.max(0, ...rows.map(row => row.width)) + PAD * 2
  const height = rows.length * rowHeight() + (rows.length - 1) * rowGap() + TOP + BOTTOM
  return { rows, width, height }
}

/** A shield's top edge, where the lamp row stops and the row proper begins. */
export function rowHeight(): number {
  return LAMP_H + LAMP_GAP + FRAME_H
}

/** Vertical space between two rows of shields, lamps included. */
export function rowGap(): number {
  return 26
}

/** Where a row's shields start, measured from the top of the chassis. */
export function rowTop(index: number): number {
  return TOP + LAMP_H + LAMP_GAP + index * (rowHeight() + rowGap())
}

// --- the traced port -------------------------------------------------------

type Point = readonly [number, number]

function formatter(n: number): string {
  return n.toFixed(2)
}

/** A closed polygon with every corner rounded, radius one value per vertex. */
function roundedClosed(points: readonly Point[], radii: readonly number[]): string {
  const n = points.length
  const trims = points.map((point, i) => {
    const [px, py] = points[(i - 1 + n) % n]
    const [cx, cy] = point
    const [nx, ny] = points[(i + 1) % n]
    const l1 = Math.hypot(cx - px, cy - py) || 1
    const l2 = Math.hypot(nx - cx, ny - cy) || 1
    const r1 = Math.min(radii[i], l1 / 2)
    const r2 = Math.min(radii[i], l2 / 2)
    return {
      enter: [cx - ((cx - px) / l1) * r1, cy - ((cy - py) / l1) * r1] as Point,
      exit: [cx + ((nx - cx) / l2) * r2, cy + ((ny - cy) / l2) * r2] as Point,
    }
  })
  const d = [`M${formatter(trims[0].enter[0])} ${formatter(trims[0].enter[1])}`]
  for (let i = 0; i < n; i++) {
    const [cx, cy] = points[i]
    d.push(`Q${formatter(cx)} ${formatter(cy)} ${formatter(trims[i].exit[0])} ${formatter(trims[i].exit[1])}`)
    const next = trims[(i + 1) % n]
    d.push(`L${formatter(next.enter[0])} ${formatter(next.enter[1])}`)
  }
  d.push('Z')
  return d.join('')
}

/**
 * A rounded line that stops at its last point instead of closing: used for the
 * lip of the mouth, where closing the path would draw a stroke straight across
 * the chamber.
 */
function roundedOpen(points: readonly Point[], radii: readonly number[]): string {
  const n = points.length
  const trims = points.map((point, i) => {
    const [px, py] = points[Math.max(i - 1, 0)]
    const [cx, cy] = point
    const [nx, ny] = points[Math.min(i + 1, n - 1)]
    const l1 = Math.hypot(cx - px, cy - py) || 1
    const l2 = Math.hypot(nx - cx, ny - cy) || 1
    return {
      enter: [cx - ((cx - px) / l1) * Math.min(radii[i], l1 / 2), cy - ((cy - py) / l1) * Math.min(radii[i], l1 / 2)] as Point,
      exit: [cx + ((nx - cx) / l2) * Math.min(radii[i], l2 / 2), cy + ((ny - cy) / l2) * Math.min(radii[i], l2 / 2)] as Point,
    }
  })
  const d = [`M${formatter(trims[0].enter[0])} ${formatter(trims[0].enter[1])}`]
  for (let i = 0; i < n; i++) {
    const [cx, cy] = points[i]
    d.push(`Q${formatter(cx)} ${formatter(cy)} ${formatter(trims[i].exit[0])} ${formatter(trims[i].exit[1])}`)
    if (i + 1 < n) d.push(`L${formatter(trims[i + 1].enter[0])} ${formatter(trims[i + 1].enter[1])}`)
  }
  return d.join('')
}

const MOUTH: Point[] = [
  [18, 31], [48, 31], [48, 18], [56, 18], [56, 10], [98, 10], [98, 18], [107, 18], [107, 31], [136, 31],
  [136, 105], [18, 105],
]
const MOUTH_RADII = [2, 1, 1, 1, 0.8, 0.8, 0.8, 0.8, 1, 1, 2, 2]

/** The chamber: the dark opening a plug goes into. */
export const MOUTH_PATH = roundedClosed(MOUTH, MOUTH_RADII)
/** The same outline without its floor, stroked as the mouth's lip. */
export const CROWN_PATH = roundedOpen(MOUTH.slice(0, 10), MOUTH_RADII.slice(0, 10))

const PRONGS = [48, 56.6, 65.2, 73.8, 82.4, 91, 99.6, 108.2]

/** The eight prongs: body, lit tip, and the shadow each throws on the next. */
export const PRONG_BODY = PRONGS.map(x =>
  `M${x} 100.6 L${x + 4} 101.4 L${x + 7.5} 70.0 L${x + 3.5} 69.2 Z`).join('')
export const PRONG_TIP = PRONGS.map(x =>
  `M${x} 98.2 L${x + 4} 99.0 L${x + 4} 101.4 L${x} 100.6 Z`).join('')
export const PRONG_SHADE = PRONGS.map(x =>
  `M${x + 4} 101.4 L${x + 7.5} 70.0 L${x + 8.2} 69.8 L${x + 4.7} 101.2 Z`).join('')

/**
 * The cage an SFP module slides into: a chamfered opening, the latch bail
 * across its mouth, and the two guide rails. Drawn in a box `CAGE_W` wide and
 * `FRAME_H` tall so a row can place it like a shield.
 */
export const CAGE_MOUTH = 'M18 26 L166 26 L178 38 L178 94 L18 94 Z'
export const CAGE_BAIL = 'M40 74 L146 74 L146 84 L40 84 Z'
