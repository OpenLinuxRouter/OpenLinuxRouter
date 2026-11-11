/**
 * Which pictures a typed query is asking for.
 *
 * The device picker's search box used to be theSVG's alone: six thousand brand
 * marks were matched on the daemon while the hundred-odd pictures that ship
 * inside `olrd` could only be reached by scrolling to them. That is backwards
 * for a router. The common answer to "what is this thing" is a NAS, a phone or
 * a printer — a kind of device, not a maker — and the device pictures are the
 * ones that cover it. One query now answers from both catalogues, and this is
 * the half that runs in the browser, because the bundle is already loaded and
 * a keystroke should not be a request.
 *
 * What it matches is *words*, not substrings of a path: `nas` finds the
 * four-bay NAS, `mac mini` finds the Mac mini, and `storage` finds the NAS by a
 * word none of its labels say. The words a tile answers to are assembled by
 * features/clients/icons.ts, which is where the artwork and its tags live; this
 * module only decides which of them answer a query and in what order.
 *
 * Kept free of imports so the rule can be exercised without a bundler; see
 * icon-search.test.mjs.
 */

/** A string's words: lowercased, with punctuation and slashes treated as gaps. */
export function searchWords(text: string): string[] {
  return text.toLowerCase().split(/[^a-z0-9]+/).filter(Boolean)
}

/**
 * How well `text` answers `query`, or -1 when it does not.
 *
 * The query is matched as a phrase, with the gaps between its words optional
 * on both sides — so "mac mini" finds "Mac mini" and "4bay" finds "4-bay NAS",
 * and neither has to know how the other spells the space. Anything more clever
 * than that (every word somewhere, in any order) starts answering queries with
 * tiles that merely share a word, which on a list this small is noise.
 *
 * Three grades, and the order is what makes a short result list usable:
 *
 *   0. The text *starts* with the query — "mac" opening "MacBook" — or a whole
 *      word *is* it, which is how the tags promote a picture to the front when
 *      someone types a word that is a household name for it.
 *   1. A word starts with each term — "mac" inside "iMac", "mini" inside
 *      "Mini PC". Still a real answer, found by name rather than by position.
 *   2. The query is merely somewhere inside — "nas" in "Samsung", for someone
 *      who typed it meaning the storage box. Worth offering; not worth leading
 *      with.
 *
 * A query of nothing but punctuation has no terms and matches nothing, which
 * is the honest answer.
 */
export function iconMatch(text: string, query: string): number {
  const terms = searchWords(query)
  if (terms.length === 0) return -1
  const words = searchWords(text)
  if (words.length === 0) return -1
  // Both sides squashed, so a gap in one is a gap in neither.
  const phrase = terms.join('')
  const squashed = words.join('')
  if (!squashed.includes(phrase)) return -1
  if (squashed.startsWith(phrase) || words.includes(phrase)) return 0
  if (terms.every((term) => words.some((word) => word.startsWith(term)))) return 1
  return 2
}

/**
 * The items answering `query`, best first, ties in the order they were given.
 *
 * `textOf` is passed in rather than known here because this has no business
 * knowing what a picture is: the caller has the artwork, the labels and the
 * tags, and this only ranks them. Equal scores keep their original order,
 * which is the order the picker would have shown them in anyway — so a search
 * that merely filters does not also reshuffle the grid.
 */
export function searchIcons<T>(items: readonly T[], textOf: (item: T) => string, query: string): T[] {
  if (searchWords(query).length === 0) return []
  return items
    .map((item, index) => ({ item, index, score: iconMatch(textOf(item), query) }))
    .filter((scored) => scored.score >= 0)
    .sort((a, b) => a.score - b.score || a.index - b.index)
    .map((scored) => scored.item)
}
