import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative, sep } from 'node:path'

/**
 * Keeps the control language from drifting back into page-local decisions.
 *
 * `docs/ui-control-language.md` says a state's colour belongs to the word, a row
 * glyph means one verb, and a switch is labelled by something the operator can
 * see. Prose alone did not hold that line for the last ten pages, so the rules
 * that can be checked mechanically are checked here.
 *
 * Run with `node scripts/test-control-language.mjs`. It reads JSX as text on
 * purpose: the point is to catch a class string or a prop that was typed by
 * hand, which a type checker cannot see.
 */
const root = new URL('../src', import.meta.url).pathname
const problems = []

/** Files allowed to name a tone colour, because their hue is not a status. */
const colourOwners = new Set([
  'components/ui/status.tsx',
  'components/ui/badge.tsx',
  'components/ui/alert.tsx',
  'components/ui/button.tsx',
  'components/ui/primary-metric.tsx',
  // Chart and map scales: hue is data, not state.
  'features/network-quality/network-quality.tsx',
  'features/topology/nodes.tsx',
  'features/setup/first-run.tsx',
  // The Overview verdict is a sentence with one icon, not a status glyph.
  'routes/overview.tsx',
])

/**
 * Sizes outside the type scale, allowed only where the text is part of a
 * picture: initials inside an icon tile, a dial's figure, a radar's axis
 * labels, a chart's channel axis.
 */
const artworkSizes = {
  'features/clients/client-icon.tsx': ['0.6rem', '0.7rem'],
  'features/gateway/latency-site-icon.tsx': ['10px'],
  'features/topology/nodes.tsx': ['10px'],
  'routes/credits.tsx': ['0.6875rem'],
  'components/layout/launcher.tsx': ['28px'],
  'features/tools/speed-dial.tsx': ['40px', '12px'],
  'features/wireless/wireless-page.tsx': ['10px'],
  // The phone tab bar: seven labels across 375px.
  'components/layout/app-shell.tsx': ['11px'],
}

const files = []
function walk(dir) {
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry)
    if (statSync(path).isDirectory()) walk(path)
    else if (path.endsWith('.tsx')) files.push(path)
  }
}
walk(root)

const rel = (path) => relative(root, path).split(sep).join('/')

/**
 * The opening tag that starts at `start`, ending at the `>` outside braces and
 * quotes. JSX props contain `=>` and comments contain quotes, so neither a
 * regular expression nor the first `>` can find the end.
 */
function openingTag(source, start) {
  let depth = 0
  let quote = null
  for (let i = start; i < source.length; i += 1) {
    const ch = source[i]
    if (depth > 0) {
      if (ch === '{') depth += 1
      else if (ch === '}') depth -= 1
      continue
    }
    if (quote) {
      if (ch === quote) quote = null
      continue
    }
    if (ch === '"' || ch === "'" || ch === '`') quote = ch
    else if (ch === '{') depth = 1
    else if (ch === '>') return source.slice(start, i + 1)
  }
  return ''
}

for (const file of files) {
  const name = rel(file)
  const source = readFileSync(file, 'utf8')
  const lines = source.split('\n')

  lines.forEach((line, index) => {
    const at = `${name}:${index + 1}`

    // 1. A status glyph or a tinted status word may be drawn only where the
    //    tone map lives. Warning surfaces (Alert borders and fills) are a
    //    different job and are left to the alert component.
    const statusDot = /\bbg-(success|warning|destructive)(\/\d+)?\b/.test(line) && /rounded-full/.test(line)
    const neutralDot = /\bbg-muted-foreground\/\d+\b/.test(line)
    if (!colourOwners.has(name) && (statusDot || neutralDot)) {
      problems.push(`${at} writes a status colour; name a word from lib/state.ts and use StatusDot/StatusInline/StatusPill`)
    }

    // 2. One dot scale: 6px and 8px, nothing in between.
    if (/\bsize-2\.5\b/.test(line) && /rounded-full/.test(line)) {
      problems.push(`${at} draws a 10px status dot; use StatusDot (6px inline, 8px verdict)`)
    }

    // 7. Text sizes come from the type scale (docs/ui-semantic-components.md):
    //    text-xs, text-meta, text-sm and up. Artwork and chart axes are the
    //    documented exception, listed by file and size.
    for (const match of line.matchAll(/\btext-\[(\d[\d.]*(?:px|rem))\]/g)) {
      if (!(artworkSizes[name] ?? []).includes(match[1])) {
        problems.push(`${at} sets text-[${match[1]}]; use a type-scale size (text-xs, text-meta, text-sm, …)`)
      }
    }
  })

  // 3. Every switch names its subject somewhere on screen. JSX props contain
  //    `=>`, so the tag ends at its self-closing `/>` rather than at the first
  //    `>`.
  for (const match of source.matchAll(/<Switch\b/g)) {
    const rest = source.slice(match.index)
    const end = rest.indexOf('/>')
    if (end < 0) continue
    const tag = rest.slice(0, end)
    if (!/\b(aria-label|aria-labelledby|id)=/.test(tag)) {
      const line = source.slice(0, match.index).split('\n').length
      problems.push(`${name}:${line} <Switch> has no aria-label, aria-labelledby or id`)
    }
  }

  // 4. Accepting a plan warning is not a destructive action.
  for (const match of source.matchAll(/<Button\b([\s\S]*?)>([\s\S]*?)<\/Button>/g)) {
    const [, attrs, body] = match
    if (attrs.includes('variant="danger"') && /anyway/i.test(body)) {
      const line = source.slice(0, match.index).split('\n').length
      problems.push(`${name}:${line} paints "anyway" danger; it accepts a warning and is the primary action`)
    }
  }

  // 6. Button ranks are semantic; the paint names are gone.
  for (let at = source.indexOf('<Button'); at >= 0; at = source.indexOf('<Button', at + 7)) {
    const tag = openingTag(source, at)
    if (/variant="(outline|ghost|destructive)"/.test(tag)) {
      const line = source.slice(0, at).split('\n').length
      problems.push(`${name}:${line} uses a paint name for a button rank; use primary/secondary/quiet/danger/link`)
    }
  }

  // 5. Green is not a badge: a status is a dot and a word, not a tinted pill in a row.
  if (!name.startsWith('features/plan/') && !name.startsWith('features/nat/plan-preview')) {
    if (/<Badge[^>]*variant="success"/.test(source)) {
      problems.push(`${name} renders a state as a success Badge; use StatusInline or StatusPill`)
    }
  }
}

if (problems.length > 0) {
  console.error('Control language violations:\n' + problems.map((problem) => `  ${problem}`).join('\n'))
  process.exit(1)
}
console.log(`Control language: ${files.length} files checked, no violations.`)
