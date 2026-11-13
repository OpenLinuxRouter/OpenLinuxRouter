// The documentation index, read from the pre-rendered JSON that
// scripts/prepare-docs.mjs writes. Keeping the Markdown parser out of the page
// graph is what lets the site be plain static HTML.

import generated from './docs.generated.json'

export interface Heading {
  id: string
  text: string
  level: number
}

export interface DocEntry {
  slug: string
  title: string
  zhTitle: string
  summary: string
  group: string
  html: string
  headings: Heading[]
  zhHtml: string | null
  zhHeadings: Heading[] | null
}

export const docGroups = generated.groups as { id: string; en: string; zh: string; slugs: string[] }[]
export const docs = generated.docs as DocEntry[]

export function docsInGroup(groupId: string): DocEntry[] {
  const group = docGroups.find((candidate) => candidate.id === groupId)
  if (!group) return []
  return group.slugs.map((slug) => docs.find((doc) => doc.slug === slug)).filter(Boolean) as DocEntry[]
}

export function docTitle(doc: DocEntry, lang: 'en' | 'zh'): string {
  return lang === 'zh' ? doc.zhTitle : doc.title
}

export function docBody(doc: DocEntry, lang: 'en' | 'zh'): { html: string; headings: Heading[] } {
  if (lang === 'zh' && doc.zhHtml) return { html: doc.zhHtml, headings: doc.zhHeadings ?? [] }
  return { html: doc.html, headings: doc.headings }
}

/** Previous/next within the labelled groups, so reading order is deliberate. */
export function neighbours(slug: string): { previous?: DocEntry; next?: DocEntry } {
  const ordered = docGroups.flatMap((group) => docsInGroup(group.id))
  const at = ordered.findIndex((doc) => doc.slug === slug)
  if (at < 0) return {}
  return { previous: ordered[at - 1], next: ordered[at + 1] }
}
