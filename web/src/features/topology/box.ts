import { FRAME_H, FRAME_W, LAMP_GAP, LAMP_H, type FacePort } from './face.ts'
import type { DeviceForm } from './form.ts'

/**
 * The geometry of a product-shaped face, in its own units.
 *
 * A switch is a row of ports and nothing else; the other classes are a body with
 * a panel, a class mark on the left and (for an access point) antennas above.
 * Those differences change where a port sits, so the maths lives here, once, and
 * both the drawing and anything that needs to point at a port read it from here.
 */
export const BOX = {
  /** A port inside a panel is smaller than one in a switch row. */
  scale: 0.4,
  pad: 16,
  gap: 7,
  panel: 17,
  lead: { ont: 40, ap: 34, router: 44, switch: 0, generic: 0 } as Record<DeviceForm, number>,
  minWidth: { ont: 250, ap: 196, router: 200, switch: 0, generic: 0 } as Record<DeviceForm, number>,
  antenna: 42,
}

export interface BoxGeometry {
  /** The body, in units. */
  width: number
  height: number
  /** Room the antennas take above the body. */
  antennaTop: number
  /** Where the first port starts, and the distance to the next. */
  left: number
  pitch: number
  portTop: number
  /** The scale a port is drawn at inside this body. */
  portScale: number
}

export function boxGeometry(faces: readonly FacePort[], form: DeviceForm): BoxGeometry {
  const physical = faces.filter(port => port.kind === 'ethernet' || port.kind === 'fiber')
  const n = physical.length
  const s = BOX.scale
  const lead = BOX.lead[form] ?? 0
  const pitch = (FRAME_W + BOX.gap) * s
  const inner = lead + n * FRAME_W * s + Math.max(0, n - 1) * BOX.gap * s
  const width = Math.max(inner + BOX.pad * 2, BOX.minWidth[form] ?? 0)
  const portTop = BOX.panel + (LAMP_H + LAMP_GAP) * s
  return {
    width,
    height: portTop + FRAME_H * s + BOX.panel,
    antennaTop: form === 'ap' ? BOX.antenna : 0,
    left: (width - inner) / 2 + lead,
    pitch,
    portTop,
    portScale: s,
  }
}

/** Where one port is drawn, as a fraction of the body's own units. */
export function boxPortCentre(geometry: BoxGeometry, index: number): { x: number; y: number } {
  return {
    x: geometry.left + index * geometry.pitch + (FRAME_W / 2) * geometry.portScale,
    y: geometry.portTop + (FRAME_H / 2) * geometry.portScale,
  }
}
