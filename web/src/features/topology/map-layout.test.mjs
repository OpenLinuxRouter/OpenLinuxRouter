import assert from 'node:assert/strict'
import { test } from 'node:test'

import { placeMap } from './map-layout.ts'

/**
 * The map has to hold a graph that is not a star: this router can have several
 * links, and a box can sit between two others. These tests pin the layout rules
 * that make it readable and stable — depth from the router, port order inside a
 * layer, and a place for a box nothing has placed yet.
 */
const node = (id, width = 200, height = 60) => ({ id, width, height })

test('layers are the depth from this router', () => {
  const layout = placeMap(
    [node('self'), node('sw'), node('nas'), node('phone')],
    [
      { from: 'self', to: 'sw' },
      { from: 'sw', to: 'nas' },
      { from: 'nas', to: 'phone' },
    ],
    'self',
  )
  const layerOf = Object.fromEntries(layout.nodes.map(n => [n.id, n.layer]))
  assert.deepEqual(layerOf, { self: 0, sw: 1, nas: 2, phone: 3 })
  const yOf = Object.fromEntries(layout.nodes.map(n => [n.id, n.y]))
  assert.ok(yOf.self < yOf.sw && yOf.sw < yOf.nas && yOf.nas < yOf.phone)
})

test('a box with several links is placed once, at its shallowest depth', () => {
  const layout = placeMap(
    [node('self'), node('sw'), node('nas')],
    [
      { from: 'self', to: 'sw' },
      { from: 'self', to: 'nas' },
      { from: 'sw', to: 'nas' },
    ],
    'self',
  )
  assert.equal(layout.nodes.filter(n => n.id === 'nas').length, 1)
  assert.equal(layout.nodes.find(n => n.id === 'nas').layer, 1)
})

test('inside a layer, port order decides the order', () => {
  const layout = placeMap(
    [node('self'), node('sw'), node('a'), node('b'), node('c')],
    [
      { from: 'self', to: 'sw' },
      { from: 'sw', to: 'c', fromPort: '3' },
      { from: 'sw', to: 'a', fromPort: '9' },
      { from: 'sw', to: 'b', fromPort: '5' },
    ],
    'self',
  )
  const xs = id => layout.nodes.find(n => n.id === id).x
  assert.ok(xs('c') < xs('b') && xs('b') < xs('a'), 'port 3 left of 5 left of 9')
})

test('a box nothing connects yet still gets a place', () => {
  const layout = placeMap([node('self'), node('lonely')], [], 'self')
  assert.equal(layout.nodes.length, 2)
  assert.equal(layout.nodes.find(n => n.id === 'lonely').layer, 1)
})

test('the layout is deterministic and grows with what it holds', () => {
  const nodes = [node('self'), node('sw'), node('a'), node('b')]
  const edges = [
    { from: 'self', to: 'sw' },
    { from: 'sw', to: 'a', fromPort: '2' },
    { from: 'sw', to: 'b', fromPort: '4' },
  ]
  const first = placeMap(nodes, edges, 'self')
  const second = placeMap(nodes, edges, 'self')
  assert.deepEqual(first, second)
  const wider = placeMap([...nodes, node('c', 300)], edges, 'self')
  assert.ok(wider.width >= first.width)
})

test('a node keeps its own height in a row of mixed boxes', () => {
  const layout = placeMap(
    [node('self', 200, 60), node('tall', 100, 200), node('short', 100, 40)],
    [{ from: 'self', to: 'tall' }, { from: 'self', to: 'short' }],
    'self',
  )
  const tall = layout.nodes.find(n => n.id === 'tall')
  const short = layout.nodes.find(n => n.id === 'short')
  assert.equal(tall.y, short.y, 'same row, same top')
  assert.ok(layout.height > 200)
})

test('an upstream box sits in a row above the root', () => {
  const layout = placeMap(
    [node('self'), node('modem'), node('sw')],
    [{ from: 'modem', to: 'self' }, { from: 'self', to: 'sw' }],
    'self',
    {},
    ['modem'],
  )
  const at = id => layout.nodes.find(n => n.id === id)
  assert.equal(at('modem').layer, -1)
  assert.ok(at('modem').y < at('self').y && at('self').y < at('sw').y)
})

test('children sit under their own parent, not under the row centre', () => {
  const layout = placeMap(
    [node('self'), node('a'), node('b'), node('a1', 80), node('a2', 80), node('b1', 80)],
    [
      { from: 'self', to: 'a', fromPort: '1' },
      { from: 'self', to: 'b', fromPort: '2' },
      { from: 'a', to: 'a1' },
      { from: 'a', to: 'a2' },
      { from: 'b', to: 'b1' },
    ],
    'self',
  )
  const at = id => layout.nodes.find(n => n.id === id)
  const centre = n => n.x + n.width / 2
  assert.ok(at('a2').x + at('a2').width <= at('b1').x, 'a family stays left of b family')
  assert.ok(Math.abs(centre(at('b1')) - centre(at('b'))) < 1, 'b1 under b')
  for (const n of layout.nodes) assert.ok(n.x >= 0)
})

test('end devices share the bottom row, in their parent port order', () => {
  const layout = placeMap(
    [node('self'), node('sw'), node('ap'), node('nas', 80), node('phone', 80)],
    [
      { from: 'self', to: 'sw' },
      { from: 'sw', to: 'ap', fromPort: '4' },
      { from: 'sw', to: 'nas', fromPort: '7' },
      { from: 'ap', to: 'phone' },
    ],
    'self',
    {},
    [],
    new Set(['nas', 'phone']),
  )
  const at = id => layout.nodes.find(n => n.id === id)
  assert.equal(at('nas').layer, at('phone').layer)
  assert.ok(at('nas').layer > at('ap').layer)
  assert.ok(at('phone').x < at('nas').x, 'the phone under port 4 is left of the NAS on port 7')
})

test('a cable that skips a row runs down a lane clear of the boxes in it', () => {
  const layout = placeMap(
    [node('self'), node('sw'), node('ap1'), node('ap2'), node('nas', 80), node('phone', 80)],
    [
      { from: 'self', to: 'sw' },
      { from: 'sw', to: 'ap1', fromPort: '4' },
      { from: 'sw', to: 'ap2', fromPort: '6' },
      { from: 'sw', to: 'nas', fromPort: '5' },
      { from: 'ap1', to: 'phone' },
    ],
    'self',
    {},
    [],
    new Set(['nas', 'phone']),
  )
  const lane = layout.lanes.get('sw>nas')
  assert.equal(lane.length, 1)
  const row = layout.nodes.filter(n => n.id === 'ap1' || n.id === 'ap2')
  for (const box of row) assert.ok(lane[0].x < box.x || lane[0].x > box.x + box.width, `lane clear of ${box.id}`)
  const ap1 = layout.nodes.find(n => n.id === 'ap1')
  const ap2 = layout.nodes.find(n => n.id === 'ap2')
  assert.ok(ap1.x < lane[0].x && lane[0].x < ap2.x, 'port 5 runs between port 4 and port 6')
  assert.ok(!layout.nodes.some(n => n.id.includes('lane')), 'lanes are not boxes')
})

test('a lane past the root sits on the side its port is on', () => {
  const layout = placeMap(
    [node('self'), node('modem'), node('sw', 400)],
    [
      { from: 'self', to: 'sw', fromPort: 'ens19', toPort: '2' },
      { from: 'sw', to: 'modem', fromPort: '5' },
    ],
    'self',
    {},
    ['modem'],
  )
  const self = layout.nodes.find(n => n.id === 'self')
  const lane = layout.lanes.get('sw>modem')
  assert.equal(lane.length, 1)
  assert.ok(lane[0].x > self.x + self.width, 'port 5 is right of port 2, so the lane is right of the router')
})
