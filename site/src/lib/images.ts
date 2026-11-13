// Gallery images are committed WebP files; this module maps a screen id and a
// preferred variant to a hashed URL Astro can serve. Pages ask for what they
// want and get the closest variant that exists, because the capture library
// does not hold all four for every page.

import manifest from './screens.manifest.json'

type Variant = { lang: string; theme: string; file: string }

const urls = import.meta.glob<string>('../assets/screens/*.webp', {
  eager: true,
  query: '?url',
  import: 'default',
})

const byFile = new Map<string, string>()
for (const [path, url] of Object.entries(urls)) {
  byFile.set(path.split('/').pop()!, url)
}

export interface ScreenAsset {
  url: string
  lang: string
  theme: string
  /** True when the requested language/theme was not captured and a fallback was used. */
  fallback: boolean
}

export function screenUrl(id: string, lang = 'en', theme = 'light'): ScreenAsset | null {
  const variants = (manifest as Record<string, Variant[]>)[id]
  if (!variants?.length) return null

  const exact = variants.find((v) => v.lang === lang && v.theme === theme)
  const chosen =
    exact ??
    variants.find((v) => v.lang === 'en' && v.theme === theme) ??
    variants.find((v) => v.theme === theme) ??
    variants[0]

  const url = byFile.get(chosen.file)
  return url ? { url, lang: chosen.lang, theme: chosen.theme, fallback: !exact } : null
}

/** Variants actually present for a screen, for the lightbox to offer. */
export function variantsOf(id: string): Variant[] {
  return (manifest as Record<string, Variant[]>)[id] ?? []
}

export function hasScreen(id: string): boolean {
  return Boolean((manifest as Record<string, Variant[]>)[id]?.length)
}
