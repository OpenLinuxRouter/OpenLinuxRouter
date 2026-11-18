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
  above: readonly string[] = [],
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
  // Upstream boxes — the modem — sit in a row of their own above the root, and
  // the walk below does not pull them under it.
  for (const id of above) if (byId.has(id) && id !== rootId) layer.set(id, -1)
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

  // Each box's parent: the neighbour one layer up, through the port nearest
  // the left. A box with none (the root, an unplaced box) has no parent.
  const parentOf = new Map<string, { id: string; port: number }>()
  for (const [id, pairs] of neighbours) {
    for (const pair of pairs) {
      if ((layer.get(pair.id) ?? 1) !== (layer.get(id) ?? 1) + 1) continue
      const port = orderOf(pair.port)
      const current = parentOf.get(pair.id)
      if (current === undefined || port < current.port) parentOf.set(pair.id, { id, port })
    }
  }

  // Rows top to bottom. Inside a row, children follow their parents' order and
  // then the parent's port numbers, so cables leave a box in the order its
  // ports run and do not cross; a box with no parent keeps id order, after.
  const depths = [...rows.keys()].sort((a, b) => a - b)
  const ordered = new Map<number, MapNode[]>()
  const indexOf = new Map<string, number>()
  for (const depth of depths) {
    const row = rows.get(depth) ?? []
    const key = (node: MapNode): [number, number] => {
      const parent = parentOf.get(node.id)
      return parent === undefined ? [Number.MAX_SAFE_INTEGER, 0] : [indexOf.get(parent.id) ?? Number.MAX_SAFE_INTEGER, parent.port]
    }
    const orderedRow = [...row].sort((a, b) => {
      const [pa, qa] = key(a)
      const [pb, qb] = key(b)
      return pa - pb || qa - qb || a.id.localeCompare(b.id)
    })
    orderedRow.forEach((node, index) => indexOf.set(node.id, index))
    ordered.set(depth, orderedRow)
  }

  // Columns: each family of siblings is centred under its parent, then pushed
  // right where it would overlap the family before it. A row with no parents
  // above it is centred on the rows already placed.
  const x = new Map<string, number>()
  const placedRows: { depth: number; row: MapNode[] }[] = []
  for (const depth of depths) {
    const row = ordered.get(depth) ?? []
    const families: { parent?: string; nodes: MapNode[] }[] = []
    for (const node of row) {
      const parent = parentOf.get(node.id)?.id
      const last = families[families.length - 1]
      if (last !== undefined && last.parent === parent && parent !== undefined) last.nodes.push(node)
      else families.push({ parent, nodes: [node] })
    }
    let cursor = -Infinity
    for (const family of families) {
      const span = family.nodes.reduce((sum, node) => sum + node.width, 0) + gapX * (family.nodes.length - 1)
      const parent = family.parent === undefined ? undefined : byId.get(family.parent)
      const parentX = family.parent === undefined ? undefined : x.get(family.parent)
      let left = parent !== undefined && parentX !== undefined ? parentX + parent.width / 2 - span / 2 : cursor === -Infinity ? 0 : cursor
      if (left < cursor) left = cursor
      for (const node of family.nodes) {
        x.set(node.id, left)
        left += node.width + gapX
      }
      cursor = left
    }
    placedRows.push({ depth, row })
  }

  // A parent is centred over its children when they ended up pushed right of it,
  // walking up from the bottom, so a wide family does not hang off to one side.
  for (const { row } of [...placedRows].reverse()) {
    for (const node of row) {
      const children = [...parentOf].filter(([, p]) => p.id === node.id).map(([id]) => id)
      if (children.length === 0) continue
      const lo = Math.min(...children.map(id => x.get(id) ?? 0))
      const hi = Math.max(...children.map(id => (x.get(id) ?? 0) + (byId.get(id)?.width ?? 0)))
      const want = (lo + hi) / 2 - node.width / 2
      // Only ever move right, and never into the next box in the row.
      const index = row.indexOf(node)
      const next = row[index + 1]
      const limit = next === undefined ? Infinity : (x.get(next.id) ?? 0) - gapX - node.width
      const current = x.get(node.id) ?? 0
      if (want > current) x.set(node.id, Math.min(want, limit))
    }
  }

  // Rows without a parent (the root, the row above it) centre on the whole.
  let min = Infinity
  let max = -Infinity
  for (const node of nodes) {
    const at = x.get(node.id)
    if (at === undefined) continue
    min = Math.min(min, at)
    max = Math.max(max, at + node.width)
  }
  if (min === Infinity) { min = 0; max = 0 }
  for (const { row } of placedRows) {
    if (row.some(node => parentOf.has(node.id))) continue
    const children = row.flatMap(node => [...parentOf].filter(([, p]) => p.id === node.id).map(([id]) => id))
    const lo = children.length > 0 ? Math.min(...children.map(id => x.get(id) ?? 0)) : min
    const hi = children.length > 0 ? Math.max(...children.map(id => (x.get(id) ?? 0) + (byId.get(id)?.width ?? 0))) : max
    const span = row.reduce((sum, node) => sum + node.width, 0) + gapX * (row.length - 1)
    let left = (lo + hi) / 2 - span / 2
    for (const node of row) {
      x.set(node.id, left)
      left += node.width + gapX
    }
  }

  min = Infinity
  max = -Infinity
  for (const node of nodes) {
    const at = x.get(node.id) ?? 0
    min = Math.min(min, at)
    max = Math.max(max, at + node.width)
  }
  if (min === Infinity) { min = 0; max = 0 }

  const placed: PlacedNode[] = []
  let y = padding
  for (const { depth, row } of placedRows) {
    let tallest = 0
    for (const node of row) {
      placed.push({ ...node, layer: depth, x: padding + (x.get(node.id) ?? 0) - min, y })
      tallest = Math.max(tallest, node.height)
    }
    y += tallest + gapY
  }

  return { nodes: placed, width: max - min + padding * 2, height: Math.max(0, y - gapY + padding) }
}
