/**
 * The one shape every box on the map is drawn as: a chassis with the box's
 * name across the top and its ports in a recessed bay underneath.
 *
 * There is no switch shape, router shape or modem shape any more. A type drawn
 * as a different silhouette is a claim about hardware we have not seen; the
 * name says what the box is, and the ports say what it has. What the map needs
 * from the shape is where each port is, so the cable can leave from it — and
 * that is computed here, once, for both the drawing and the cables.
 *
 * All sizes are CSS pixels at scale 1.
 */
export interface PanelPort {
  id: string
  name?: string
  kind: string
  linkUp?: boolean
  speedMbps?: number
}

export const PANEL = {
  padX: 12,
  padTop: 10,
  headH: 18,
  headGap: 8,
  bayPadX: 8,
  bayPadTop: 7,
  bayPadBottom: 5,
  rowGap: 8,
  padBottom: 10,
  jackW: 30,
  jackH: 24,
  fiberW: 40,
  portGap: 6,
  labelGap: 3,
  labelH: 11,
  /** The bay of a box that has not been read: room for one line of text. */
  emptyBayW: 168,
  emptyBayH: 38,
  /** Past this many ports a bay folds into two rows. */
  rowMax: 16,
} as const

export interface PanelGeometry {
  width: number
  height: number
  /** Centre of each port's jack, from the panel's top left. */
  ports: { id: string; x: number; y: number; width: number }[]
}

const portWidth = (port: PanelPort) => (port.kind === 'fiber' ? PANEL.fiberW : PANEL.jackW)
const cellH = PANEL.jackH + PANEL.labelGap + PANEL.labelH

/** The rows a bay holds its ports in. */
export function panelRows<T extends PanelPort>(ports: readonly T[]): T[][] {
  if (ports.length <= PANEL.rowMax) return [ports.slice()]
  const half = Math.ceil(ports.length / 2)
  return [ports.slice(0, half), ports.slice(half)]
}

/**
 * The panel's size and its ports' positions. `headWidth` is how wide the
 * name, address and count are, which the caller measures, since only it knows
 * the font.
 */
export function panelGeometry(ports: readonly PanelPort[], headWidth: number): PanelGeometry {
  const bayTop = PANEL.padTop + PANEL.headH + PANEL.headGap
  if (ports.length === 0) {
    const width = Math.ceil(Math.max(headWidth, PANEL.emptyBayW) + PANEL.padX * 2)
    return { width, height: bayTop + PANEL.emptyBayH + PANEL.padBottom, ports: [] }
  }
  const rows = panelRows(ports)
  const rowWidth = (row: readonly PanelPort[]) =>
    row.reduce((sum, port) => sum + portWidth(port), 0) + PANEL.portGap * Math.max(0, row.length - 1)
  const bayInner = Math.max(...rows.map(rowWidth))
  const bayW = bayInner + PANEL.bayPadX * 2
  const width = Math.ceil(Math.max(headWidth, bayW) + PANEL.padX * 2)
  const bayH = PANEL.bayPadTop + rows.length * cellH + (rows.length - 1) * PANEL.rowGap + PANEL.bayPadBottom
  const placed: PanelGeometry['ports'] = []
  // The bay spans the chassis; each row of ports is centred in it.
  const inner = width - PANEL.padX * 2
  rows.forEach((row, index) => {
    let x = PANEL.padX + (inner - rowWidth(row)) / 2
    const y = bayTop + PANEL.bayPadTop + index * (cellH + PANEL.rowGap) + PANEL.jackH / 2
    for (const port of row) {
      const w = portWidth(port)
      placed.push({ id: port.id, x: x + w / 2, y, width: w })
      x += w + PANEL.portGap
    }
  })
  return { width, height: bayTop + bayH + PANEL.padBottom, ports: placed }
}

/** What the speed lamp says: green at a gigabit and up, amber below it, dark when unknown. */
export function speedLamp(port: PanelPort): 'fast' | 'slow' | 'off' {
  if (port.linkUp !== true || port.speedMbps === undefined || port.speedMbps <= 0) return 'off'
  return port.speedMbps >= 1000 ? 'fast' : 'slow'
}
