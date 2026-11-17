import assert from 'node:assert/strict'
import { test } from 'node:test'

import {
  CAGE_W,
  FRAME_W,
  ROW_MAX,
  faceLayout,
  isFiber,
  isPhysical,
  rowTop,
} from './face.ts'

/**
 * The face is drawn from the reading, so the layout has two jobs: put the
 * physical ports where the chassis has room for them, and refuse to draw a port
 * the hardware does not have. The cases that matter are the ones where a box
 * reports more than a jack strip: a bridge, a VLAN and a radio are interfaces,
 * not holes, and a 24-port switch really is two rows of twelve.
 */
const port = (id, kind = 'ethernet', over = {}) => ({ id, kind, ...over })

test('only ports with a connector are drawn', () => {
  assert.equal(isPhysical(port('p1', 'ethernet')), true)
  assert.equal(isPhysical(port('p1', 'fiber')), true)
  assert.equal(isPhysical(port('br-lan', 'bridge')), false)
  assert.equal(isPhysical(port('eth0.10', 'vlan')), false)
  assert.equal(isPhysical(port('phy0-ap0', 'wireless')), false)
  assert.equal(isPhysical(port('bond0', 'lag')), false)
  assert.equal(isFiber(port('p9', 'fiber')), true)
})

test('a switch with no ports lays out nothing, so nothing is drawn', () => {
  assert.deepEqual(faceLayout([]).rows, [])
  assert.deepEqual(faceLayout([port('br-lan', 'bridge')]).rows, [])
})

test('a row lays shields edge to edge, a cage wider and set apart', () => {
  const { rows } = faceLayout([port('p1'), port('p2'), port('p9', 'fiber')])
  assert.equal(rows.length, 1)
  const [first, second, cage] = rows[0].slots
  assert.equal(first.x, 0)
  assert.equal(second.x, FRAME_W + 6)
  // The cage keeps its own, wider gap, and is drawn wider than a shield.
  assert.equal(cage.x, second.x + FRAME_W + 34)
  assert.equal(cage.width, CAGE_W)
  assert.equal(cage.fiber, true)
})

test('a row is as wide as what it holds', () => {
  const two = faceLayout([port('p1'), port('p2')]).width
  const one = faceLayout([port('p1')]).width
  assert.ok(two > one)
  assert.equal(two - one, FRAME_W + 6)
})

test('twenty-four ports are two rows, not one wide one', () => {
  const ports = Array.from({ length: 24 }, (_, i) => port(`p${i + 1}`))
  const { rows } = faceLayout(ports)
  assert.equal(rows.length, 2)
  assert.equal(rows[0].slots.length, ROW_MAX)
  assert.equal(rows[1].slots.length, 12)
  // Both rows start at the same left edge and end at the same width.
  assert.equal(rows[0].slots[0].x, rows[1].slots[0].x)
  assert.equal(rows[0].width, rows[1].width)
  // The second row clears the lamps of its own row, which sit above the shields.
  assert.ok(rowTop(1) - rowTop(0) > 120 + 26)
})

test('an odd count keeps the row as wide as its widest row', () => {
  const ports = Array.from({ length: 13 }, (_, i) => port(`p${i + 1}`))
  const layout = faceLayout(ports)
  assert.equal(layout.rows.length, 2)
  assert.equal(layout.rows[1].slots.length, 1)
  assert.ok(layout.width >= faceLayout(ports.slice(0, 12)).width)
})

test('the lamp above a port is not drawn over the port', () => {
  // The shields of the second row begin below the first row's shields.
  assert.ok(rowTop(1) > rowTop(0) + 120)
})
