import { FOOT, FRAME_H, FRAME_W, faceLayout, rowLeft, rowTop, type FacePort } from './face.ts'
import type { DeviceForm } from './form.ts'
import { FORM_CHROME } from './form.ts'
import { boxGeometry, boxPortCentre } from './box.ts'

/**
 * Where a face's ports are, in pixels, for a drawing rendered at a given width.
 *
 * The map draws a cable from the port a switch named, so it needs the same
 * numbers the drawing used. Rather than repeat them, both read the layout here:
 * a row of ports for a switch, a panel for the product forms.
 */
export interface PortPoint {
  id: string
  /** Centre of the port, from the drawing's left edge. */
  x: number
  /** Centre of the port, from the drawing's top edge. */
  y: number
}

export interface FaceGeometry {
  width: number
  height: number
  ports: PortPoint[]
}

export function faceGeometry(
  faces: readonly FacePort[],
  form: DeviceForm,
  renderedWidth: number,
): FaceGeometry {
  if (form === 'switch' || form === 'generic') {
    const chrome = FORM_CHROME[form]
    const layout = faceLayout(faces, chrome)
    const scale = renderedWidth / layout.width
    const ports: PortPoint[] = []
    layout.rows.forEach((row, rowIndex) => {
      for (const slot of row.slots) {
        ports.push({
          id: slot.port.id,
          x: (rowLeft(chrome) + slot.x + slot.width / 2) * scale,
          y: (rowTop(rowIndex, chrome) + FRAME_H / 2) * scale,
        })
      }
    })
    return { width: renderedWidth, height: (layout.height + FOOT) * scale, ports }
  }

  const geometry = boxGeometry(faces, form)
  const scale = renderedWidth / geometry.width
  const ports = faces
    .filter(port => port.kind === 'ethernet' || port.kind === 'fiber')
    .map((port, index) => {
      const centre = boxPortCentre(geometry, index)
      return {
        id: port.id,
        x: centre.x * scale,
        y: (geometry.antennaTop + centre.y) * scale,
      }
    })
  return {
    width: renderedWidth,
    height: (geometry.height + geometry.antennaTop) * scale,
    ports,
  }
}

/** The width at which one shield is `portWidth` across, for either form. */
export function naturalWidth(faces: readonly FacePort[], form: DeviceForm, portWidth: number): number {
  if (form === 'switch' || form === 'generic') {
    const layout = faceLayout(faces, FORM_CHROME[form])
    return layout.width * (portWidth / FRAME_W)
  }
  const geometry = boxGeometry(faces, form)
  return geometry.width * (portWidth / (FRAME_W * geometry.portScale))
}
