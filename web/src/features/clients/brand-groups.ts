/**
 * The brand sections of the device icon picker, derived from the artwork that
 * exists rather than written down beside it.
 *
 * The picker used to show one flat "Brands" grid of every vendor picture. That
 * was fine at twenty tiles and is not at forty-six: a grid you scroll is a grid
 * you scan by shape, and the shape of "the phone I own" is an Apple phone, not
 * tile thirty-one. Grouping by vendor turns that scroll into a short list of
 * names — Apple, Samsung, Huawei — each of which is looked up in one place.
 *
 * Two rules, and both are about the set growing rather than about today:
 *
 *   1. **A vendor with two or more pictures gets its own section.** One picture
 *      does not make a section: a dozen headers over a dozen single tiles is
 *      longer to scan than the flat grid it replaced, and each header costs a
 *      line to say only what the tile's own label already says. Those tiles
 *      keep their vendor in the label ("Brother printer") and are collected
 *      under one last section instead.
 *   2. **Own sections are ordered by how many pictures they hold**, ties in
 *      artwork order. That is the order a household thinks in — the makers you
 *      own four things from, then the one-product vendors — and it is the same
 *      order the artwork was already declared in, so nothing moves today.
 *      Deriving it means a vendor does not have to be *moved* when it becomes
 *      interesting; a second picture moves it.
 *
 * Nothing is stored. A brand's own section and the singles bucket are one
 * function of the tiles passed in, so a new `xiaomi/tablet` appears under Xiaomi
 * the moment its line is added to IMAGES, and a brand leaves the bucket when it
 * earns a second picture. There is no list to keep in step, which is the whole
 * reason this is a function and not a table: a hand-kept grouping drifts from
 * the artwork, and drifts silently — a tile simply stops being reachable.
 *
 * Kept free of imports so the rule can be tested without a bundler; see
 * brand-groups.test.mjs.
 */

/** A section of the picker: what to call it, and what it holds, in order. */
export interface BrandGroup<T> {
  label: string
  choices: T[]
}

/**
 * Groups `tiles` into one section per vendor worth a section, plus a final
 * bucket for the vendors that are not.
 *
 * `vendorOf` and `nameOf` are passed in rather than known here because this
 * module has no business knowing what a VendorKey is: the caller has the
 * artwork, the key and the display name, and this only arranges them.
 *
 * `otherLabel` is a *message* rather than a fixed string — it is the one label
 * here that is a UI word instead of a brand the vendor chose, and so the one
 * the caller has to translate.
 */
export function brandGroups<T>(
  tiles: readonly T[],
  vendorOf: (tile: T) => string,
  nameOf: (vendor: string) => string,
  otherLabel: string,
): BrandGroup<T>[] {
  const byVendor = new Map<string, T[]>()
  for (const tile of tiles) {
    const vendor = vendorOf(tile)
    const held = byVendor.get(vendor)
    if (held) held.push(tile)
    else byVendor.set(vendor, [tile])
  }

  const groups: BrandGroup<T>[] = []
  const alone: T[] = []
  for (const [vendor, choices] of byVendor) {
    if (choices.length > 1) groups.push({ label: nameOf(vendor), choices })
    else alone.push(...choices)
  }

  // Array.prototype.sort is stable, so equal counts keep artwork order.
  groups.sort((a, b) => b.choices.length - a.choices.length)

  if (alone.length > 0) groups.push({ label: otherLabel, choices: alone })
  return groups
}
