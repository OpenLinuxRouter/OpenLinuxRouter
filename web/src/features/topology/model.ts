import { effectiveParents } from '@/features/clients/group-tree'
import { compareClientRanks, type ClientRank } from '@/features/topology/device-rank'
import { magnitude, sumFlows, type Flow, type TrafficView } from '@/features/topology/traffic'
import type { ClientRow } from '@/lib/api-types'
import type { ClientsGroup } from '@/lib/config-types'

/** The network filter's value for devices on none of this router's networks. */
export const NO_NETWORK = ' none'

/**
 * The key of the display-only bucket for devices in no group. It cannot
 * collide with a group: names are trimmed on the server, so none starts with a
 * space.
 */
export const OTHER_KEY = ' other'

export const groupKey = (name: string) => `g:${name}`
export const deviceKey = (mac: string) => `d:${mac}`

/**
 * One group as the map draws it: what it holds after the filters, in the
 * order it is drawn, and what it holds before them, for the counts.
 */
export interface MapGroup {
  key: string
  /** The group's name, or undefined for the bucket. */
  name?: string
  title: string
  /** The parent's key, or undefined at the top. */
  parentKey?: string
  /** 0 at the top. */
  depth: number
  /** Clients directly in it that survived the filters, active first, then recently heard. */
  devices: ClientRow[]
  /** Groups directly inside it that survived the filters, in name order. */
  groups: MapGroup[]
  /** Clients directly in it, before the filters — what a delete would move. */
  directCount: number
  /** Groups directly inside it, before the filters. */
  subgroupCount: number
  /** Every device at any depth below it, before the filters. */
  total: number
  /** Traffic of every device at any depth below it. */
  flow?: Flow
  /** Its live traffic as one number, for link and layout sizing. */
  weight: number
}

export interface MapTree {
  top: MapGroup[]
  byKey: Map<string, MapGroup>
  /** Whether the operator has made any groups at all. */
  grouped: boolean
  filtering: boolean
  /** Filtering, and nothing matched anywhere. */
  nothing: boolean
  /** Live traffic weights for layout sizing. */
  weights: Map<string, number>
  /** Client sort keys, to hold the order still while the pointer is over the map. */
  ranks: Map<string, ClientRank>
}

/**
 * The operator's groups as a tree, with the filters applied and everything in
 * drawing order.
 *
 * **Groups keep their places.** A group is a place on the map the operator
 * learns — Home on the left, IoT on the right — and ordering groups by traffic
 * moved them every time the counters were read, which is the map rearranging
 * the house. They are in name order, the same as every group menu, at every
 * level.
 *
 * **Moving devices first, then recently heard.** Current download + upload
 * rate orders devices with measurable traffic. Everything else is ordered by
 * when the router last heard it, never by historical byte totals. This also
 * decides what the map folds into "+N more".
 *
 * The caller samples `frozen` ranks every five seconds and pauses sampling
 * during pointer, touch or keyboard interaction. Rates on screen keep updating;
 * only the order holds still. New devices follow the existing rows until the
 * next sample, and removed devices disappear immediately.
 *
 * **Filtering hides, where it used to empty.** A search used to leave every
 * group drawn with "Nothing here matches" inside, which was fine for three
 * cards and is not for twelve containers of which two hold the answer. A group
 * survives a filter when anything below it does, or when its own name is
 * what was typed. Under the filters every group is also shown whole — a hit
 * folded into "+N more" is a search that looks like it found nothing.
 */
export function buildTree({
  devices: all,
  groups,
  traffic,
  filter,
  network,
  frozen,
}: {
  devices: ClientRow[]
  groups: ClientsGroup[]
  traffic: TrafficView
  filter: string
  network: string
  frozen?: Map<string, ClientRank> | null
}): MapTree {
  const q = filter.trim().toLowerCase()
  const filtering = q !== '' || network !== ''
  const matches = (d: ClientRow) =>
    (!network || (network === NO_NETWORK ? !d.network : d.network === network)) &&
    (!q ||
      [d.name, d.mac, d.hostname ?? '', d.vendor ?? '', d.network ?? '', d.group ?? '', ...(d.ips ?? [])]
        .join(' ')
        .toLowerCase()
        .includes(q))

  const weights = new Map<string, number>()
  const ranks = new Map<string, ClientRank>()
  const rankDevices = (list: ClientRow[]) => {
    for (const d of list) {
      const flow = traffic.flowOf(d)
      weights.set(deviceKey(d.mac), magnitude(flow, traffic.rated))
      // The activity indicator calls rates below 1 byte/s quiet as well.
      const rate = traffic.rated && traffic.counting && d.online
        ? (flow?.downRate ?? 0) + (flow?.upRate ?? 0)
        : 0
      ranks.set(deviceKey(d.mac), {
        rate: rate >= 1 ? rate : 0,
        lastSeen: d.last_seen ? Date.parse(d.last_seen) || 0 : 0,
      })
    }
    return list.sort((a, b) => {
      const ar = frozen?.get(deviceKey(a.mac)) ?? ranks.get(deviceKey(a.mac))!
      const br = frozen?.get(deviceKey(b.mac)) ?? ranks.get(deviceKey(b.mac))!
      // A new device while frozen belongs after the existing rows.
      if (frozen) {
        const known = Number(frozen.has(deviceKey(b.mac))) - Number(frozen.has(deviceKey(a.mac)))
        if (known) return known
      }
      return compareClientRanks(ar, br) ||
        (a.name || a.mac).localeCompare(b.name || b.mac) || a.mac.localeCompare(b.mac)
    })
  }
  const rankGroups = (list: MapGroup[]) => {
    for (const g of list) weights.set(g.key, g.weight)
    return list.sort((a, b) => a.title.localeCompare(b.title))
  }

  const parents = effectiveParents(groups)
  const known = new Set(groups.map((g) => g.name))
  const membersOf = new Map<string, ClientRow[]>()
  const loose: ClientRow[] = []
  for (const d of all) {
    // A device naming a group that no longer exists cannot happen through
    // the API, but a hand-edited olr.json can say anything, and such a device
    // is better shown in the bucket than not shown.
    if (d.group && known.has(d.group)) membersOf.set(d.group, [...(membersOf.get(d.group) ?? []), d])
    else loose.push(d)
  }
  const childrenOf = new Map<string, string[]>()
  for (const g of groups) {
    const p = parents.get(g.name) ?? ''
    childrenOf.set(p, [...(childrenOf.get(p) ?? []), g.name])
  }

  const byKey = new Map<string, MapGroup>()

  const build = (
    key: string,
    name: string | undefined,
    title: string,
    members: ClientRow[],
    subNames: string[],
    depth: number,
    parentKey: string | undefined,
  ): MapGroup => {
    const subs = subNames.map((n) =>
      build(groupKey(n), n, n, membersOf.get(n) ?? [], childrenOf.get(n) ?? [], depth + 1, key),
    )
    const everyone = [...members]
    const flows = members.map((d) => traffic.flowOf(d))
    let total = members.length
    for (const s of subs) {
      total += s.total
      flows.push(s.flow)
    }
    const flow = sumFlows(flows)
    const shown = members.filter(matches)
    const node: MapGroup = {
      key,
      name,
      title,
      parentKey,
      depth,
      devices: rankDevices(shown),
      groups: [],
      directCount: everyone.length,
      subgroupCount: subNames.length,
      total,
      flow,
      weight: magnitude(flow, traffic.rated),
    }
    // Subgroups that the filters emptied are dropped here, after they have
    // counted toward this group's totals: "12 devices" should not change
    // because the operator typed something.
    node.groups = rankGroups(subs.filter(survives))
    if (survives(node)) byKey.set(key, node)
    return node
  }
  const survives = (g: MapGroup) =>
    !filtering ||
    g.devices.length > 0 ||
    g.groups.length > 0 ||
    (q !== '' && g.name !== undefined && g.name.toLowerCase().includes(q))

  const grouped = groups.length > 0
  const top = rankGroups(
    (childrenOf.get('') ?? [])
      .map((n) => build(groupKey(n), n, n, membersOf.get(n) ?? [], childrenOf.get(n) ?? [], 0, undefined))
      .filter(survives),
  )

  // The bucket is last whatever its traffic: it is not a group, and a
  // display-only card jumping ahead of the operator's own would make it look
  // like one. With no groups at all it is the whole map, so it is always there.
  if (loose.length > 0 || !grouped) {
    const bucket = build(OTHER_KEY, undefined, grouped ? 'Other devices' : 'All devices', loose, [], 0, undefined)
    // With no groups, the bucket stays even when the filters empty it: it is
    // the only container, and the map keeps its shape around "nothing
    // matches" rather than collapsing to the router alone.
    if (survives(bucket) || !grouped) {
      byKey.set(OTHER_KEY, bucket)
      top.push(bucket)
    }
  }

  const nothing = filtering && top.every((g) => g.devices.length === 0 && g.groups.length === 0)
  return { top, byKey, grouped, filtering, nothing, weights, ranks }
}

/** The chain from the top down to a group, both included. */
export function chainTo(tree: MapTree, key: string): MapGroup[] {
  const out: MapGroup[] = []
  for (let g = tree.byKey.get(key); g; g = g.parentKey ? tree.byKey.get(g.parentKey) : undefined) {
    out.unshift(g)
    if (out.length > 8) break
  }
  return out
}
