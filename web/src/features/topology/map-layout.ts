/**
 * Where each box goes on the topology map.
 *
 * The graph is a set of port-to-port cables, so it can have any shape: a box may
 * have one link or five, and a box can sit between two others. What it is not is
 * a star, and nothing here assumes one. Layers are BFS depth from this router —
 * the box the page belongs to — and inside a layer the order comes from the
 * parent's port numbers, which is what keeps cables from crossing: port 2 is to
 * the left of port 3, so the box on port 2 is drawn to the left of the box on
 * port 3.
 *
 * A box nothing is connected to yet still needs a place: it goes on the first
 * layer, because that is where an unplaced box on the LAN most plausibly is.
 * The map says "not asked yet" about it elsewhere; the layout does not invent a
 * parent for it.
 */

export interface MapNode {
  id: string
  width: number
  height: number
}

export interface MapEdge {
  from: string
  to: string
  /** The port the cable leaves from, when that end named one. */
  fromPort?: string
  /** The port it arrives at, when that end named one. */
  toPort?: string
  /** A wireless association, rather than a forwarding entry. */
  dashed?: boolean
}

export interface PlacedNode extends MapNode {
  layer: number
  x: number
  y: number
}

export interface MapLayout {
  nodes: PlacedNode[]
  width: number
  height: number
}

export interface MapSpacing {
  gapX: number
  gapY: number
  padding: number
}

const DEFAULT: MapSpacing = { gapX: 32, gapY: 78, padding: 24 }

export function placeMap(
  nodes: readonly MapNode[],
  edges: readonly MapEdge[],
  rootId: string,
  spacing: Partial<MapSpacing> = {},
): MapLayout {
  const { gapX, gapY, padding } = { ...DEFAULT, ...spacing }
  const byId = new Map(nodes.map(node => [node.id, node]))

  // Who is next to whom, and through which port — for ordering and layering.
  const neighbours = new Map<string, { id: string; port?: string }[]>()
  const link = (a: string, b: string, port?: string) => {
    if (!neighbours.has(a)) neighbours.set(a, [])
    neighbours.get(a)?.push({ id: b, port })
  }
  for (const edge of edges) {
    if (!byId.has(edge.from) || !byId.has(edge.to)) continue
    link(edge.from, edge.to, edge.fromPort)
    link(edge.to, edge.from)
  }

  // Breadth first from this router: the layer is how many cables away a box is.
  const layer = new Map<string, number>()
  if (byId.has(rootId)) layer.set(rootId, 0)
  const queue = byId.has(rootId) ? [rootId] : []
  while (queue.length > 0) {
    const id = queue.shift() as string
    const depth = layer.get(id) ?? 0
    for (const next of neighbours.get(id) ?? []) {
      if (layer.has(next.id)) continue
      layer.set(next.id, depth + 1)
      queue.push(next.id)
    }
  }

  const orderOf = (port: string | undefined): number => {
    if (port === undefined) return 10_000
    const numeric = Number(port)
    return Number.isFinite(numeric) ? numeric : 5_000
  }

  const rows = new Map<number, MapNode[]>()
  for (const node of nodes) {
    const at = layer.get(node.id) ?? 1
    if (!rows.has(at)) rows.set(at, [])
    rows.get(at)?.push(node)
  }

  const placed: PlacedNode[] = []
  let y = padding
  const ordered = new Map<number, MapNode[]>()
  let widest = 0

  for (const depth of [...rows.keys()].sort((a, b) => a - b)) {
    const row = rows.get(depth) ?? []
    // Inside a layer, a box sits beside the port of its parent that leads to
    // it; a box with no parent in the layer above keeps its inventory order.
    const parentPort = new Map<string, number>()
    for (const [id, pairs] of neighbours) {
      if (layer.get(id) !== depth - 1) continue
      for (const pair of pairs) {
        if (layer.get(pair.id) !== depth) continue
        if (!parentPort.has(pair.id)) parentPort.set(pair.id, orderOf(pair.port))
      }
    }
    const orderedRow = [...row].sort((a, b) => {
      const pa = parentPort.get(a.id) ?? 10_000
      const pb = parentPort.get(b.id) ?? 10_000
      if (pa !== pb) return pa - pb
      return a.id.localeCompare(b.id)
    })

    ordered.set(depth, orderedRow)
    const rowWidth = orderedRow.reduce((sum, node) => sum + node.width, 0) + gapX * Math.max(0, orderedRow.length - 1)
    widest = Math.max(widest, rowWidth)
  }

  const width = widest + padding * 2
  for (const depth of [...ordered.keys()].sort((a, b) => a - b)) {
    const row = ordered.get(depth) ?? []
    const rowWidth = row.reduce((sum, node) => sum + node.width, 0) + gapX * Math.max(0, row.length - 1)
    let x = padding + (widest - rowWidth) / 2
    let tallest = 0
    for (const node of row) {
      placed.push({ ...node, layer: depth, x, y })
      x += node.width + gapX
      tallest = Math.max(tallest, node.height)
    }
    y += tallest + gapY
  }

  return { nodes: placed, width, height: Math.max(0, y - gapY + padding) }
}
