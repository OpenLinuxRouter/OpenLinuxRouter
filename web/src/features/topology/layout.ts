import { chainTo, OTHER_KEY, type MapGroup, type MapTree } from '@/features/topology/model'
import type { ClientRow } from '@/lib/api-types'
import { chunk, edgeWidth, link, withBand, EDGE_MIN, type Box, type Link } from './band'

export type { Box, Link } from './band'

/**
 * Where everything on the network map goes, worked out as numbers.
 *
 * Nothing here touches the DOM. Every box on the map has a size this module
 * decides — nodes are fixed within a range, text inside them truncates rather
 * than wraps, and a container is exactly as big as what it holds — so the whole
 * picture can be computed before anything is drawn, and the one thing the
 * browser has to be asked is how wide the map is and how big the router's box
 * came out. That is what makes the rest possible:
 *
 *   - Lines are drawn from the same numbers the boxes are placed with, so a
 *     line cannot end anywhere but on its box, on the first frame or any
 *     other. The previous map measured its cards after layout and drew from
 *     the measurement, and the frame between the two was the one where lines
 *     pointed at nothing.
 *
 *   - Moving between layouts — expanding a fold, switching density, focusing a
 *     group — is interpolating between two sets of numbers, and every box and
 *     every line is interpolated with the same curve over the same time, so
 *     they arrive together. See network-map.tsx for why that is enough.
 *
 *   - Harmony is a rule that can be written down and enforced, rather than
 *     whatever CSS grid made of the content that week.
 *
 * **One rule at every level: show at most K, fold the rest.** A container
 * shows at most K children, busiest first, and the rest go into one full-width
 * "+N more" row that opens in place. A container at or under K is never
 * folded: a network of a few dozen devices is seen whole.
 *
 * **Harmony is a grid.** Every top-level container is the same width, on
 * columns the page shares, and the rows of the grid start level. Inside each,
 * devices are one list, a row apiece and edge to edge, and a subgroup is an
 * inset box of its own beneath it. An earlier version sized each container to
 * its contents by scoring shapes, and the result was a row of boxes that were
 * each reasonable and together a mess — no two edges lined up. Uniform is
 * calmer than optimal. A container is as tall as its list, though: stretching
 * a group of one to the height of its neighbour of ten was a card of empty
 * white.
 *
 * **Lines reach one row, then run down columns.** Each column of the grid gets
 * a curve from the router into its first container, thickness by the whole
 * column's traffic, and every container below hangs off the one above it by a
 * short straight line. No line passes between two cards to reach a third —
 * which is what a curve from the router to a second-row card would have to do.
 * An earlier version drew more than one row inside one outline with a single
 * link into it, which put a box around boxes around lists.
 *
 * **Everything outside the router is above it.** The devices dialed in over
 * WireGuard are boxes on the far side of this router, and the band is the only
 * place a box can join the router with a line of its own. A card hung under the
 * router beside a group, or on a row of its own between them, would need the
 * lines to those groups to run through it or squeeze past it. Observed default
 * routes are not boxes there: a next hop is one address on a line of Link's
 * status, and drawing each one cost a row over the map while saying less than
 * that line does. Gateway policy exits stay off the map for the same reason: an
 * exit this box chooses among is not a thing outside it.
 */

export type Density = 'compact' | 'detail'

export interface NodeSize {
  /** The narrowest a container of these may be: room for its header. */
  minW: number
  /** The natural width; nodes stretch from here up to maxW to fill a row. */
  w: number
  maxW: number
  h: number
  /** Children a container shows before folding the rest. */
  k: number
  /** A device's height as a row of a container's list. */
  row: number
  /** The narrowest a top-level container may be. */
  list: number
}

/**
 * The two node sizes. Compact is for seeing structure — icon and name — and
 * shows more before folding because each one costs less space; detail carries
 * the address and the rate, and folds sooner. A detail node loses its third
 * line, the traffic, when nothing is being counted.
 */
export function nodeSize(density: Density, counting: boolean, serviceDevices: ReadonlySet<string> = new Set()): NodeSize {
  const extra = serviceDevices.size > 0 ? 32 : 0
  return density === 'compact'
    ? { minW: 168, w: 144, maxW: 184, h: 40 + extra, k: 16, row: 44, list: extra ? 288 : 248 }
    : { minW: 208, w: 232, maxW: 288, h: (counting ? 68 : 56) + extra, k: 10, row: 56, list: extra ? 320 : 288 }
}

/** Below this the map stops branching and stacks. */
export const NARROW = 640

// Container anatomy.
const PAD = 12
/** The header band: title and count, including the padding above them. */
export const HEAD = 40
const GAP = 8
const SUB_GAP = 12
const MORE_H = 32
const EMPTY_H = 36
/**
 * A fold never hides fewer than this. "+2 more" costs a row to save two, and
 * reads as the map being stingy rather than tidy.
 */
const MIN_FOLD = 3

// The router's level.
const ROW_GAP = 16
const DROP = 64
const OVERFLOW_GAP = 24

// Focus.
export const CHIP_H = 36
const CHIP_W = 164
const CHIP_GAP = 12
const SIDE_GAP = 28
const FOCUS_W = 264
export const FOCUS_H = 56
const CHAIN_GAP = 36
const FAN_GAP = 16

// Narrow.
export const RAIL_X = 15
const RAIL_INSET = 32

/**
 * How a group is drawn. `container` holds its children; `chip` is a group set
 * aside while another is focused, or a rung on the way down to it; `focused`
 * is the group being looked at; `card` is a subgroup fanned out beneath it.
 * All four are the same element under the same key, so one turning into
 * another is an animation, not a swap.
 */
export type GroupVariant = 'container' | 'chip' | 'focused' | 'card'

export interface Hidden {
  devices: number
  groups: number
}

export type Item =
  | {
      kind: 'group'
      key: string
      group: MapGroup
      box: Box
      variant: GroupVariant
      open: boolean
      /** A container inside another, drawn one step quieter. */
      nested?: boolean
    }
  | {
      kind: 'device'
      key: string
      device: ClientRow
      box: Box
      /** Present when the device is a row of a container's list rather than a card of its own. */
      row?: { first: boolean }
    }
  | { kind: 'dialin'; key: string; box: Box }
  | {
      kind: 'more'
      key: string
      box: Box
      /** A full-width row inside a container, or a node-sized card in a row. */
      variant: 'row' | 'card'
      /** The expansion key it toggles. */
      target: string
      open: boolean
      hidden: Hidden
      /** Clients behind a top-level fold, for its second line. */
      devices?: number
      /** Whether "Other devices" is among what a top-level fold hides. */
      others?: boolean
    }
  | { kind: 'pick'; key: string; box: Box; groups: MapGroup[] }
  | { kind: 'note'; key: string; box: Box; note: 'empty' | 'hint' | 'nothing' }

export interface Rail {
  x: number
  y0: number
  y1: number
  dots: { key: string; y: number }[]
}

export interface Layout {
  width: number
  height: number
  narrow: boolean
  /**
   * Whether this is the whole picture, comfortably: nothing folded, and every
   * top-level container inside the shape band. The automatic density asks
   * this of the detail layout and falls back to compact when the answer is
   * no — detail that has to hide devices or squeeze a group into a column is
   * worse than compact that shows them all.
   */
  fits: boolean
  router: Box
  items: Item[]
  links: Link[]
  rail?: Rail
}

/**
 * The devices dialed in over WireGuard, keyed by name.
 *
 * Bare keys: the band only needs to know how many boxes there are and what to
 * call them. Whether a peer is connected, and where it last dialed from, is
 * the node's business, because it changes on every poll and the layout should
 * only move when the set of peers does.
 */
export interface DialInInput {
  peers: { key: string }[]
}

export interface LayoutInput {
  tree: MapTree
  width: number
  density: Density
  router: { w: number; h: number }
  /** Container keys, TOP_KEY and fanKey()s whose fold is open. */
  expanded: ReadonlySet<string>
  focus?: string
  counting: boolean
  rated: boolean
  /** Peers of the tunnel, above the router. */
  dialIn?: DialInInput
  serviceDevices?: ReadonlySet<string>
}

/** The expansion key of the router's own "+N more groups". */
export const TOP_KEY = ' top'
/** The expansion key of a focused group's fan. */
export const fanKey = (key: string) => `fan:${key}`

export function layout(input: LayoutInput): Layout {
  const below =
    input.width < NARROW
      ? layoutNarrow(input)
      : input.focus && input.tree.byKey.has(input.focus)
        ? layoutFocus(input, input.focus)
        : layoutTree(input)
  return withBand(below, input)
}

/* -------------------------------------------------------------------------- */
/* Containers                                                                  */
/* -------------------------------------------------------------------------- */

/** One way to arrange a container: which children, in how many columns. */
interface Plan {
  group: MapGroup
  w: number
  h: number
  cost: number
  devices: ClientRow[]
  /** Subgroups, as rows of plans. */
  shelves: Plan[][]
  more?: { hidden: Hidden; open: boolean }
  note?: 'empty' | 'hint'
  /** Whether it is showing everything because it was asked to. */
  open?: boolean
}

interface Ctx {
  node: NodeSize
  /** A container alone in the router's row, which may show twice as many. */
  solo?: string
  expanded: ReadonlySet<string>
  filtering: boolean
  narrow: boolean
  /** The container that gets the "make your first group" hint. */
  hint?: string
  cache: Map<string, Plan[]>
  serviceDevices?: ReadonlySet<string>
}

function hintHeight(narrow: boolean) {
  return narrow ? 92 : 56
}

/** One piece of a container's body. The list runs edge to edge; everything else is inset. */
type Block =
  | { kind: 'note'; h: number }
  | { kind: 'list'; h: number }
  | { kind: 'shelf'; h: number; shelf: Plan[] }
  | { kind: 'more'; h: number }

function blocks(plan: Pick<Plan, 'note' | 'devices' | 'shelves' | 'more'>, ctx: Ctx): Block[] {
  const out: Block[] = []
  if (plan.note) out.push({ kind: 'note', h: plan.note === 'hint' ? hintHeight(ctx.narrow) : EMPTY_H })
  if (plan.devices.length > 0) {
    const height = plan.devices.length * ctx.node.row
    out.push({ kind: 'list', h: height })
  }
  for (const shelf of plan.shelves) out.push({ kind: 'shelf', h: Math.max(...shelf.map((p) => p.h)), shelf })
  if (plan.more) out.push({ kind: 'more', h: MORE_H })
  return out
}

/**
 * Where each block starts, and how tall the body is. An inset block keeps PAD
 * from the edges and SUB_GAP from another inset block; the list touches the
 * body's edges, so a body that is only a list has no padding at all.
 */
function stack(list: Block[]): { tops: number[]; h: number } {
  let y = 0
  let prev: 'inset' | 'flush' | undefined
  const tops = list.map((b) => {
    const kind = b.kind === 'list' ? 'flush' : 'inset'
    if (kind === 'inset') y += prev === 'inset' ? SUB_GAP : PAD
    else if (prev === 'inset') y += PAD
    const top = y
    y += b.h
    prev = kind
    return top
  })
  if (prev === 'inset') y += PAD
  return { tops, h: y }
}

/**
 * The narrow version of a container: exactly `width` wide, as many columns
 * of nodes as fit, subgroups stacked full-width underneath. No scoring — at
 * this width there is one sensible answer.
 */
function narrowPlan(g: MapGroup, width: number, ctx: Ctx): Plan {
  const { node } = ctx
  const userOpen = ctx.expanded.has(g.key)
  const open = ctx.filtering || userOpen
  const nd = g.devices.length
  const ns = g.groups.length
  const n = nd + ns
  const k = g.key === ctx.solo ? node.k * 2 : node.k
  const s = open || n - k < MIN_FOLD ? n : k
  const sg = Math.min(ns, s)
  const sd = s - sg
  const hidden = { devices: nd - sd, groups: ns - sg }
  const folded = hidden.devices + hidden.groups > 0
  const more = folded ? { hidden, open: false } : userOpen ? { hidden, open: true } : undefined
  const note = g.key === ctx.hint ? 'hint' : n === 0 ? 'empty' : undefined

  const inner = width - 2 * PAD
  const subs = g.groups.slice(0, sg).map((sub) => narrowPlan(sub, inner, ctx))
  const plan: Plan = {
    group: g,
    w: width,
    h: 0,
    cost: 0,
    devices: g.devices.slice(0, sd),
    shelves: subs.map((p) => [p]),
    more,
    note,
  }
  plan.h = HEAD + stack(blocks(plan, ctx)).h
  return plan
}

/**
 * Puts a planned container and everything in it at (x, y), `w` wide — which
 * may be wider than the plan, in which case its list and subgroups stretch to
 * fill it — and at least `minH` tall.
 */
function place(plan: Plan, x: number, y: number, w: number, minH: number, ctx: Ctx, out: Item[], nested = false) {
  const { node } = ctx
  const body = blocks(plan, ctx)
  const { tops, h: bodyH } = stack(body)
  out.push({
    kind: 'group',
    key: plan.group.key,
    group: plan.group,
    box: { x, y, w, h: Math.max(HEAD + bodyH, minH) },
    variant: 'container',
    open: ctx.expanded.has(plan.group.key),
    nested,
  })

  const inner = w - 2 * PAD
  body.forEach((b, i) => {
    const top = y + HEAD + tops[i]
    switch (b.kind) {
      // The note leads: a hint about making groups is read before the
      // devices, and "nothing in it yet" is the whole body anyway.
      case 'note':
        out.push({ kind: 'note', key: `note:${plan.group.key}`, box: { x: x + PAD, y: top, w: inner, h: b.h }, note: plan.note! })
        break
      case 'list':
        let offset = 0
        plan.devices.forEach((d, j) => {
          const height = node.row
          out.push({
            kind: 'device',
            key: `d:${d.mac}`,
            device: d,
            box: { x, y: top + offset, w, h: height },
            row: { first: j === 0 },
          })
          offset += height
        })
        break
      case 'shelf': {
        // Spare width is shared in proportion, so a shelf fills its container
        // and the boxes in it keep their relative sizes.
        const natural = b.shelf.reduce((t, p) => t + p.w, 0)
        const spare = inner - natural - (b.shelf.length - 1) * SUB_GAP
        let sx = x + PAD
        for (const p of b.shelf) {
          const sw = p.w + (spare * p.w) / natural
          place(p, sx, top, sw, b.h, ctx, out, true)
          sx += sw + SUB_GAP
        }
        break
      }
      case 'more':
        out.push({
          kind: 'more',
          key: `more:${plan.group.key}`,
          box: { x: x + PAD, y: top, w: inner, h: b.h },
          variant: 'row',
          target: plan.group.key,
          open: plan.more!.open,
          hidden: plan.more!.hidden,
        })
        break
    }
  })
}

function makeCtx(input: LayoutInput, narrow: boolean): Ctx {
  const { tree } = input
  return {
    node: nodeSize(input.density, input.counting, input.serviceDevices),
    solo: tree.top.length === 1 ? tree.top[0].key : undefined,
    expanded: input.expanded,
    filtering: tree.filtering,
    narrow,
    hint: !tree.grouped && !tree.filtering ? OTHER_KEY : undefined,
    cache: new Map(),
    serviceDevices: input.serviceDevices,
  }
}

/* -------------------------------------------------------------------------- */
/* The tree                                                                    */
/* -------------------------------------------------------------------------- */

function layoutTree(input: LayoutInput): Layout {
  const { tree, width: A } = input
  const ctx = makeCtx(input, false)
  const items: Item[] = []
  const links: Link[] = []

  const router: Box = { x: (A - input.router.w) / 2, y: 0, w: input.router.w, h: input.router.h }
  const rowY = router.h + DROP
  const top = tree.top

  if (tree.nothing && top.length === 0) {
    items.push(nothingNote(A, rowY))
    return finish(input, false, router, items, links)
  }

  // Every container the same width, on one grid. The column count is what
  // fits at the density's container width, and never more than there are
  // containers — so two groups are two columns, centred, not two cards pinned
  // to the left of a three-column grid.
  const colMin = containerWidth(ctx)
  const fit = Math.max(1, Math.min(MAX_COLS, Math.floor((A + ROW_GAP) / (colMin + ROW_GAP))))
  const cols = Math.min(fit, Math.max(1, top.length))
  const colW = (Math.min(A, fit * COL_MAX + (fit - 1) * ROW_GAP) - (fit - 1) * ROW_GAP) / fit
  const rows = chunk(top, cols)

  // Past SHOW_ALL devices, rows beyond the first fold into "+N more groups"
  // until opened; below it, everything is shown.
  const everyone = top.reduce((t, g) => t + g.total, 0)
  const openTop = input.expanded.has(TOP_KEY) || tree.filtering
  const shownRows = everyone > SHOW_ALL && !openTop && rows.length > 1 ? rows.slice(0, 1) : rows
  const hidden = rows.slice(shownRows.length).flat()

  const gridW = cols * colW + (cols - 1) * ROW_GAP
  const x0 = (A - gridW) / 2
  // A line into a column carries the whole column's traffic: everything in it
  // hangs off that one line.
  const load = Array.from({ length: cols }, (_, c) =>
    shownRows.reduce((t, row) => t + Math.max(0, row[c]?.weight ?? 0), 0),
  )
  const chained = shownRows.length > 1
  const heaviest = chained ? Math.max(0, ...load) : Math.max(0, ...top.map((g) => g.weight))
  const above: Box[] = []
  let y = rowY

  for (const row of shownRows) {
    const plans = row.map((g) => gridPlan(g, colW, ctx))
    // A single row is centred under the router. Once there are columns to run
    // down, a short last row keeps to them instead, so each card sits straight
    // under the one it hangs from.
    let x = chained ? x0 : x0 + ((cols - row.length) * (colW + ROW_GAP)) / 2
    plans.forEach((p, c) => {
      place(p, x, y, colW, 0, ctx, items)
      const box = { x, y, w: colW, h: p.h }
      const dashed = tree.grouped && p.group.key === OTHER_KEY
      const weight = chained ? load[c] : p.group.weight
      links.push(
        above[c]
          ? link(p.group.key, above[c], box, EDGE_MIN, { dashed })
          : link(p.group.key, router, box, edgeWidth(weight, heaviest, input.rated), { dashed }),
      )
      above[c] = box
      x += colW + ROW_GAP
    })
    y += Math.max(...plans.map((p) => p.h)) + (chained ? CHAIN_GAP_ROWS : ROW_GAP)
  }
  if (chained) y += ROW_GAP - CHAIN_GAP_ROWS

  if (hidden.length > 0 || (openTop && everyone > SHOW_ALL && rows.length > 1)) {
    const box = { x: x0, y, w: gridW, h: MORE_H }
    items.push({
      kind: 'more',
      key: 'more: top',
      box,
      variant: 'row',
      target: TOP_KEY,
      open: openTop,
      hidden: { devices: 0, groups: hidden.filter((g) => g.key !== OTHER_KEY).length },
      others: hidden.some((g) => g.key === OTHER_KEY),
      devices: hidden.reduce((t, g) => t + g.total, 0),
    })
    y += MORE_H + ROW_GAP
  }

  return finish(input, false, router, items, links, hidden.length === 0 && !anyFold(items))
}

/** Past this many devices, rows of groups after the first start folded. */
const SHOW_ALL = 80
/** Between rows of containers, where the line from one card down to the next runs. */
const CHAIN_GAP_ROWS = 32
/** Containers per row at most, and how wide one may grow. */
const MAX_COLS = 4
const COL_MAX = 400

/** The narrowest a container may be: room for its list's names and rates. */
function containerWidth(ctx: Ctx) {
  return ctx.node.list
}

function anyFold(items: Item[]) {
  return items.some((i) => i.kind === 'more' && !i.open)
}

/**
 * A container at a given width: its devices as one list, then each subgroup
 * as a full-width box of its own, then the fold. Nothing is chosen by score — every container is laid out the same
 * way, which is what lets a row of them line up.
 */
function gridPlan(g: MapGroup, w: number, ctx: Ctx): Plan {
  const { node } = ctx
  const inner = w - 2 * PAD
  const userOpen = ctx.expanded.has(g.key)
  const open = ctx.filtering || userOpen
  const nd = g.devices.length
  const ns = g.groups.length
  const n = nd + ns
  const k = node.k

  // Subgroups first: folding a device hides one thing, a subgroup a branch.
  const shownN = open || n <= k ? n : Math.min(k, n - MIN_FOLD)
  const sg = Math.min(ns, shownN)
  const sd = shownN - sg
  const hidden = { devices: nd - sd, groups: ns - sg }
  const folded = hidden.devices + hidden.groups > 0
  const more = folded ? { hidden, open: false } : userOpen && n > k ? { hidden, open: true } : undefined

  const subs = g.groups.slice(0, sg).map((sub) => gridPlan(sub, inner, ctx))
  const note = g.key === ctx.hint ? 'hint' : n === 0 ? 'empty' : undefined
  const plan: Plan = {
    group: g,
    w,
    h: 0,
    cost: 0,
    devices: g.devices.slice(0, sd),
    shelves: subs.map((p) => [p]),
    more,
    note,
    open,
  }
  plan.h = HEAD + stack(blocks(plan, ctx)).h
  return plan
}

/* -------------------------------------------------------------------------- */
/* Focus                                                                       */
/* -------------------------------------------------------------------------- */

/**
 * One group, looked at closely.
 *
 * The focused group moves to the centre directly under the router, and what it
 * holds fans out beneath it as separate nodes, each on its own line whose
 * thickness is that child's traffic — the router's-eye view, one level down.
 * The other top-level groups shrink to chips either side of it, still hanging
 * off the router by thin lines, so the rest of the network is one click away
 * rather than gone.
 *
 * A focused subgroup is reached through its parents: the top-level group sits
 * under the router as a chip, each rung below it another chip, and the focused
 * group last — a breadcrumb drawn in the same direction as everything else.
 *
 * The fan obeys the same rule as a container, with one row standing in for K:
 * as many as fit side by side get lines, and the rest wait behind "+N more",
 * which opens into rows below without lines, for the same reason as the
 * router's own overflow.
 */
function layoutFocus(input: LayoutInput, focus: string): Layout {
  const { tree, width: A } = input
  const node = nodeSize(input.density, input.counting, input.serviceDevices)
  const items: Item[] = []
  const links: Link[] = []
  const chain = chainTo(tree, focus)
  const head = chain[0]
  const focused = chain[chain.length - 1]

  const router: Box = { x: (A - input.router.w) / 2, y: 0, w: input.router.w, h: input.router.h }
  const rowY = router.h + DROP
  const heaviestTop = Math.max(0, ...tree.top.map((g) => g.weight))

  // The row under the router: the head of the chain, centred, and the others
  // set aside as chips — busiest nearest, alternating left and right.
  const centerW = chain.length === 1 ? FOCUS_W : CHIP_W
  const centerH = chain.length === 1 ? FOCUS_H : CHIP_H
  const centerX = (A - centerW) / 2
  const others = tree.top.filter((g) => g.key !== head.key)
  const perSide = Math.max(0, Math.floor(((A - centerW) / 2 - SIDE_GAP + CHIP_GAP) / (CHIP_W + CHIP_GAP)))
  const slots = perSide * 2
  const shown = others.length > slots ? others.slice(0, Math.max(0, slots - 1)) : others
  const picked = others.length > slots ? others.slice(Math.max(0, slots - 1)) : []

  const chipY = rowY + (centerH - CHIP_H) / 2
  const leftX = (j: number) => centerX - SIDE_GAP - (j + 1) * CHIP_W - j * CHIP_GAP
  const rightX = (j: number) => centerX + centerW + SIDE_GAP + j * (CHIP_W + CHIP_GAP)
  let left = 0
  let right = 0
  shown.forEach((g, i) => {
    // Alternate while both sides have room; once one side is full, the other
    // takes the rest.
    const goLeft = (i % 2 === 0 && left < perSide) || right >= perSide
    const x = goLeft ? leftX(left++) : rightX(right++)
    const box = { x, y: chipY, w: CHIP_W, h: CHIP_H }
    items.push({ kind: 'group', key: g.key, group: g, box, variant: 'chip', open: false })
    links.push(link(g.key, router, box, EDGE_MIN, { faint: true, dashed: g.key === OTHER_KEY }))
  })
  if (picked.length > 0) {
    const box = { x: rightX(right), y: chipY, w: CHIP_W, h: CHIP_H }
    items.push({ kind: 'pick', key: 'pick: side', box, groups: picked })
    links.push(link('pick: side', router, box, EDGE_MIN, { faint: true, dashed: true }))
  }

  // The chain, top to bottom.
  let y = rowY
  let above: Box = router
  let aboveWeight = heaviestTop
  chain.forEach((g, i) => {
    const last = i === chain.length - 1
    const w = last ? FOCUS_W : CHIP_W
    const h = last ? FOCUS_H : CHIP_H
    const box = { x: (A - w) / 2, y, w, h }
    items.push({ kind: 'group', key: g.key, group: g, box, variant: last ? 'focused' : 'chip', open: false })
    links.push(link(g.key, above, box, edgeWidth(g.weight, aboveWeight, input.rated), { dashed: g.key === OTHER_KEY }))
    above = box
    aboveWeight = Math.max(g.weight, 0)
    y += h + CHAIN_GAP
  })

  // The fan.
  const fanY = above.y + above.h + DROP
  const children: ({ group: MapGroup } | { device: ClientRow })[] = [
    ...focused.groups.map((group) => ({ group })),
    ...focused.devices.map((device) => ({ device })),
  ]
  const weightOf = (c: (typeof children)[number]) => ('group' in c ? c.group.weight : (input.tree.weights.get(`d:${c.device.mac}`) ?? 0))

  if (children.length === 0) {
    items.push({
      kind: 'note',
      key: `note:${focused.key}`,
      box: { x: (A - 320) / 2, y: fanY - DROP / 2, w: 320, h: EMPTY_H },
      note: 'empty',
    })
    return finish(input, false, router, items, links)
  }

  const perRow = Math.max(1, Math.floor((A + FAN_GAP) / (node.w + FAN_GAP)))
  const limit = Math.min(node.k, perRow)
  // Past the limit, the last place goes to "+N more" — which then always
  // hides at least two.
  const first = children.length <= limit ? children : children.slice(0, limit - 1)
  const rest = children.slice(first.length)
  const count = first.length + (rest.length > 0 ? 1 : 0)
  const rowW = count * node.w + (count - 1) * FAN_GAP
  const heaviest = Math.max(0, ...first.map(weightOf))
  let fx = (A - rowW) / 2
  for (const c of first) {
    const box = { x: fx, y: fanY, w: node.w, h: node.h }
    if ('group' in c) {
      items.push({ kind: 'group', key: c.group.key, group: c.group, box, variant: 'card', open: false })
      links.push(link(c.group.key, above, box, edgeWidth(c.group.weight, heaviest, input.rated)))
    } else {
      const key = `d:${c.device.mac}`
      items.push({ kind: 'device', key, device: c.device, box })
      links.push(link(key, above, box, edgeWidth(weightOf(c), heaviest, input.rated)))
    }
    fx += node.w + FAN_GAP
  }

  if (rest.length > 0) {
    const target = fanKey(focused.key)
    const open = input.expanded.has(target) || tree.filtering
    const box = { x: fx, y: fanY, w: node.w, h: node.h }
    items.push({
      kind: 'more',
      key: `more:${target}`,
      box,
      variant: 'card',
      target,
      open,
      hidden: {
        devices: rest.filter((c) => 'device' in c).length,
        groups: rest.filter((c) => 'group' in c).length,
      },
    })
    links.push(link(`more:${target}`, above, box, EDGE_MIN, { dashed: true }))

    if (open) {
      let ry = fanY + node.h + OVERFLOW_GAP
      for (const row of chunk(rest, perRow)) {
        const w = row.length * node.w + (row.length - 1) * FAN_GAP
        let rx = (A - w) / 2
        for (const c of row) {
          const box = { x: rx, y: ry, w: node.w, h: node.h }
          if ('group' in c) items.push({ kind: 'group', key: c.group.key, group: c.group, box, variant: 'card', open: false })
          else items.push({ kind: 'device', key: `d:${c.device.mac}`, device: c.device, box })
          rx += node.w + FAN_GAP
        }
        ry += node.h + GAP + 4
      }
    }
  }

  return finish(input, false, router, items, links)
}

/* -------------------------------------------------------------------------- */
/* Narrow                                                                      */
/* -------------------------------------------------------------------------- */

/**
 * The phone layout: no branching at all. The router sits top left, the
 * containers stack full-width beneath it, and one rail runs down their left
 * edge with a dot level with each header. At this width a branch would have
 * nowhere to go but sideways.
 *
 * Focus does not exist here — there is no room to set anything aside — so the
 * header opens the container's fold in place instead.
 */
function layoutNarrow(input: LayoutInput): Layout {
  const { tree, width: A } = input
  const ctx = makeCtx(input, true)
  const items: Item[] = []
  const w = Math.min(input.router.w, A)
  const router: Box = { x: 0, y: 0, w, h: input.router.h }
  let y = router.h + 24
  const W = A - RAIL_INSET
  const dots: Rail['dots'] = []

  if (tree.nothing && tree.top.length === 0) {
    items.push({ kind: 'note', key: 'note: nothing', box: { x: RAIL_INSET, y, w: W, h: EMPTY_H }, note: 'nothing' })
    return finish(input, true, router, items, [])
  }

  const k = ctx.node.k
  const open = input.expanded.has(TOP_KEY) || tree.filtering
  const fold = !open && tree.top.length - k >= MIN_FOLD
  const shown = fold ? tree.top.slice(0, k) : tree.top
  for (const g of shown) {
    const plan = narrowPlan(g, W, ctx)
    place(plan, RAIL_INSET, y, W, 0, ctx, items)
    dots.push({ key: g.key, y: y + HEAD / 2 })
    y += plan.h + 12
  }
  if (fold || (open && tree.top.length - k >= MIN_FOLD && !tree.filtering)) {
    const hidden = tree.top.slice(k)
    items.push({
      kind: 'more',
      key: 'more: top',
      box: { x: RAIL_INSET, y, w: W, h: MORE_H + 8 },
      variant: 'row',
      target: TOP_KEY,
      open,
      hidden: { devices: 0, groups: hidden.length },
    })
    dots.push({ key: 'more: top', y: y + (MORE_H + 8) / 2 })
  }

  const rail =
    dots.length > 0 ? { x: RAIL_X, y0: router.h, y1: dots[dots.length - 1].y, dots } : undefined
  return { ...finish(input, true, router, items, []), rail }
}

/* -------------------------------------------------------------------------- */
/* Pieces                                                                      */
/* -------------------------------------------------------------------------- */

function nothingNote(A: number, y: number): Item {
  const w = Math.min(360, A)
  return { kind: 'note', key: 'note: nothing', box: { x: (A - w) / 2, y, w, h: EMPTY_H }, note: 'nothing' }
}

function finish(
  input: LayoutInput,
  narrow: boolean,
  router: Box,
  items: Item[],
  links: Link[],
  fits = true,
): Layout {
  const bottom = Math.max(router.y + router.h, ...items.map((i) => i.box.y + i.box.h))
  return { width: input.width, height: Math.ceil(bottom) + 2, narrow, fits, router, items, links }
}
