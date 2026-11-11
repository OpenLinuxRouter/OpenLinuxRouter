import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import ts from 'typescript'

// The module is deliberately import-free so the rule can be exercised as-is,
// without a bundler resolving asset imports; see its own header.
const source = readFileSync(new URL('./brand-groups.ts', import.meta.url), 'utf8')
const js = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext } }).outputText
const { brandGroups } = await import(`data:text/javascript;base64,${Buffer.from(js).toString('base64')}`)

const vendorOf = (tile) => tile.icon.split('/')[0]
const nameOf = (vendor) => vendor.toUpperCase()
const group = (tiles) => brandGroups(tiles, vendorOf, nameOf, 'Other brands')
const labels = (groups) => groups.map((section) => section.label)
const icons = (groups) => groups.flatMap((section) => section.choices.map((choice) => choice.icon))

const tile = (icon) => ({ icon })
const APPLE = ['apple/phone', 'apple/tablet', 'apple/laptop', 'apple/desktop']
const SAMSUNG = ['samsung/phone', 'samsung/tablet']
const SINGLES = ['brother/printer', 'hp/printer', 'epson/printer']

test('a maker with several pictures gets its own section, named by the caller', () => {
  const groups = group([...APPLE, ...SAMSUNG].map(tile))
  assert.deepEqual(labels(groups), ['APPLE', 'SAMSUNG'])
  assert.deepEqual(groups[0].choices, APPLE.map(tile))
  assert.deepEqual(groups[1].choices, SAMSUNG.map(tile))
})

test('a maker with one picture joins the trailing bucket instead of earning a heading', () => {
  const groups = group([...APPLE, ...SINGLES].map(tile))
  assert.deepEqual(labels(groups), ['APPLE', 'Other brands'])
  assert.deepEqual(groups[1].choices, SINGLES.map(tile))
})

test('sections are ordered by how many pictures they hold, ties in artwork order', () => {
  const groups = group([
    tile('sony/tv'), tile('sony/phone'), tile('sony/console'), // 3
    ...SAMSUNG.map(tile), // 2, declared later
    ...APPLE.map(tile), // 4
    tile('google/phone'), tile('google/speaker'), // 2
  ])
  assert.deepEqual(labels(groups), ['APPLE', 'SONY', 'SAMSUNG', 'GOOGLE'])
})

test('the bucket always comes last, even when it holds the most tiles', () => {
  const groups = group([...APPLE.map(tile), ...SINGLES.map(tile)])
  assert.equal(groups.at(-1).label, 'Other brands')
})

test('every picture appears exactly once, whichever section holds it', () => {
  const tiles = [...APPLE, ...SAMSUNG, ...SINGLES, 'xiaomi/phone', 'xiaomi/vacuum', 'intel/desktop'].map(tile)
  const placed = icons(group(tiles))
  assert.equal(placed.length, tiles.length)
  assert.deepEqual(new Set(placed), new Set(tiles.map((one) => one.icon)))
})

test('one picture per maker leaves a single section, and no maker leaves none', () => {
  assert.deepEqual(group([]), [])
  const groups = group(SINGLES.map(tile))
  assert.deepEqual(labels(groups), ['Other brands'])
  assert.deepEqual(groups[0].choices, SINGLES.map(tile))
})

test('a single maker needs no bucket beside it', () => {
  const groups = group(APPLE.map(tile))
  assert.deepEqual(labels(groups), ['APPLE'])
  assert.equal(groups.length, 1)
})
