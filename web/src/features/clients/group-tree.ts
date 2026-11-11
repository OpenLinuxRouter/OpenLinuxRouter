import type { ClientsGroup } from '@/lib/config-types'

/** internal/clients/config.go MaxGroupDepth. The server is still the judge. */
export const MAX_GROUP_DEPTH = 4

/**
 * Groups nest, and every list of them on screen has to say so.
 *
 * The server stores groups flat, sorted by name, each naming its parent. Every
 * picker in the UI — the device's Group select, "Move to group", "Move
 * inside…", the new-group dialog's parent — wants the same thing from that: the
 * tree, depth-first, so a subgroup is listed under its parent and indented by
 * its depth. One function so the four lists cannot disagree about the order.
 *
 * A parent that does not exist, or a chain that loops back on itself, cannot
 * come out of the API — Validate refuses both — but a hand-edited olr.json can
 * say anything, and such a group is listed at the top rather than dropped: a
 * group the picker leaves out is a group nobody can move a device into.
 */
export interface GroupOption {
  name: string
  parent?: string
  /** 0 at the top. */
  depth: number
}

export function groupOptions(groups: ClientsGroup[] = []): GroupOption[] {
  const parents = effectiveParents(groups)
  const children = new Map<string, ClientsGroup[]>()
  for (const g of groups) {
    const p = parents.get(g.name) ?? ''
    children.set(p, [...(children.get(p) ?? []), g])
  }
  const out: GroupOption[] = []
  const walk = (parent: string, depth: number) => {
    const list = [...(children.get(parent) ?? [])].sort((a, b) => a.name.localeCompare(b.name))
    for (const g of list) {
      out.push({ name: g.name, parent: parents.get(g.name), depth })
      walk(g.name, depth + 1)
    }
  }
  walk('', 0)
  return out
}

/**
 * Each group's parent as the UI should treat it: the stored one when it exists
 * and does not lead back round to the group, otherwise none.
 */
export function effectiveParents(groups: ClientsGroup[] = []): Map<string, string | undefined> {
  const stored = new Map(groups.map((g) => [g.name, g.parent || undefined] as const))
  const out = new Map<string, string | undefined>()
  for (const g of groups) {
    let parent = g.parent || undefined
    if (parent && !stored.has(parent)) parent = undefined
    // Walk up from the parent; arriving back at the group is a cycle.
    const seen = new Set([g.name])
    for (let at = parent; at; at = stored.get(at)) {
      if (seen.has(at)) {
        parent = undefined
        break
      }
      seen.add(at)
    }
    out.set(g.name, parent)
  }
  return out
}

/** A group and everything nested under it, at any depth. */
export function subtreeOf(groups: ClientsGroup[], name: string): Set<string> {
  const parents = effectiveParents(groups)
  const out = new Set([name])
  let grew = true
  while (grew) {
    grew = false
    for (const [g, p] of parents) {
      if (p && out.has(p) && !out.has(g)) {
        out.add(g)
        grew = true
      }
    }
  }
  return out
}

/**
 * Where a group may be moved: the top, or inside any group that is not itself
 * or below it, and deep enough to leave room for everything it carries.
 *
 * Checked here as well as on the server so the menu can grey out a choice
 * rather than offer it and then print the refusal — the server's message is
 * right, but a menu item that exists only to fail is a worse way to learn the
 * rule than one that says it cannot.
 */
export function parentChoices(
  groups: ClientsGroup[],
  name: string,
): { name: string; depth: number; allowed: boolean }[] {
  const options = groupOptions(groups)
  const below = subtreeOf(groups, name)
  const depthOf = new Map(options.map((o) => [o.name, o.depth]))
  // Levels this group occupies, counting itself: 1 for a leaf.
  let height = 1
  for (const g of below) height = Math.max(height, (depthOf.get(g) ?? 0) - (depthOf.get(name) ?? 0) + 1)
  return options
    .filter((o) => !below.has(o.name))
    .map((o) => ({ name: o.name, depth: o.depth, allowed: o.depth + 1 + height <= MAX_GROUP_DEPTH }))
}
