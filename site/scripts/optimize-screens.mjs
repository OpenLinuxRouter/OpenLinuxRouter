// Turns the capture library into the gallery's image set.
//
// The raw shots live in marketing/, which is gitignored on purpose — it is
// working material, not a repository artefact. The site needs a committed copy
// it can deploy, so this script resizes each capture to a screen-width WebP and
// writes it into src/assets/screens. Run it when the capture library changes.
//
//   node scripts/optimize-screens.mjs
//
// SHOT_SRC overrides where the raw captures are read from.

import { existsSync, mkdirSync, readdirSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import sharp from 'sharp'

const here = path.dirname(fileURLToPath(import.meta.url))
const siteRoot = path.resolve(here, '..')
const source =
  process.env.SHOT_SRC ??
  path.resolve(siteRoot, '../../../marketing/shared/library/shots/raw')
const outDir = path.resolve(siteRoot, 'src/assets/screens')
const manifestPath = path.resolve(siteRoot, 'src/lib/screens.manifest.json')

const ids = [
  'overview',
  'overview-full',
  'map',
  'gateway',
  'interfaces',
  'gateway-interfaces',
  'gateway-routes',
  'gateway-dhcp',
  'gateway-dns',
  'unmanaged',
  'device-mac',
  'device-nuc',
  'wireless',
  'firewall',
  'filtering',
  'dns-blocking',
  'forwards',
  'access',
  'access-gateway',
  'access-wireguard',
  'caddy',
  'socks5',
  'tunnelbroker',
  'ping',
  'traceroute',
  'speedtest',
  'quality',
  'advanced',
  'backup',
]

// The capture library marks Chinese as zh-CN; the site's routes use zh.
const variants = [
  { lang: 'en', source: 'en', theme: 'light' },
  { lang: 'zh', source: 'zh-CN', theme: 'light' },
  { lang: 'en', source: 'en', theme: 'dark' },
  { lang: 'zh', source: 'zh-CN', theme: 'dark' },
]

if (!existsSync(source)) {
  console.error(`No capture library at ${source}`)
  process.exit(1)
}

mkdirSync(outDir, { recursive: true })

const available = new Set(readdirSync(source))
const manifest = {}
let written = 0
let skipped = 0

for (const id of ids) {
  for (const { lang, source: sourceLang, theme } of variants) {
    const name = `${id}--${sourceLang}--${theme}--1440x900@2.png`
    const target = `${id}--${lang}--${theme}.webp`
    const dest = path.join(outDir, target)

    if (!available.has(name)) {
      skipped++
      continue
    }

    await sharp(path.join(source, name))
      .resize({ width: 1440, withoutEnlargement: true })
      .webp({ quality: 76, effort: 5 })
      .toFile(dest)

    ;(manifest[id] ??= []).push({ lang, theme, file: target })
    written++
  }
}

writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`)

console.log(
  `screens: ${written} written, ${skipped} absent from the library, ${Object.keys(manifest).length} pages`,
)
