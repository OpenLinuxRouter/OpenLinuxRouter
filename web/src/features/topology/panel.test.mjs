import assert from 'node:assert/strict'
import { test } from 'node:test'

import { PANEL, panelGeometry, panelRows, speedLamp } from './panel.ts'

/**
 * The panel's geometry is what the cables start from, so these tests pin it:
 * ports in order and evenly spaced, centred in the bay, a fiber cage wider
 * than a jack, and the lamps saying only what the reading said.
 */
const eth = (id, over = {}) => ({ id, kind: 'ethernet', ...over })

test('ports run left to right, evenly spaced and centred in the panel', () => {
  const g = panelGeometry([eth('1'), eth('2'), eth('3')], 100)
  const xs = g.ports.map(p => p.x)
  assert.equal(xs[1] - xs[0], PANEL.jackW + PANEL.portGap)
  assert.equal(xs[2] - xs[1], PANEL.jackW + PANEL.portGap)
  assert.ok(Math.abs((xs[0] + xs[2]) / 2 - g.width / 2) < 0.01, 'the row is centred')
})

test('a long name widens the panel, and the ports stay centred under it', () => {
  const narrow = panelGeometry([eth('1')], 50)
  const wide = panelGeometry([eth('1')], 400)
  assert.ok(wide.width > narrow.width)
  assert.ok(Math.abs(wide.ports[0].x - wide.width / 2) < 0.01)
})

test('a fiber cage is wider than a jack', () => {
  const g = panelGeometry([eth('1'), { id: 'sfp', kind: 'fiber' }], 0)
  assert.equal(g.ports[1].width, PANEL.fiberW)
  assert.equal(g.ports[0].width, PANEL.jackW)
})

test('many ports fold into two rows', () => {
  const ports = Array.from({ length: 24 }, (_, i) => eth(String(i + 1)))
  assert.equal(panelRows(ports).length, 2)
  const g = panelGeometry(ports, 0)
  assert.ok(g.ports[12].y > g.ports[0].y)
})

test('a box with no ports still gets a panel', () => {
  const g = panelGeometry([], 80)
  assert.equal(g.ports.length, 0)
  assert.ok(g.width >= PANEL.emptyBayW)
})

test('the speed lamp is lit only for a link the box gave a speed for', () => {
  assert.equal(speedLamp(eth('1', { linkUp: true, speedMbps: 1000 })), 'fast')
  assert.equal(speedLamp(eth('1', { linkUp: true, speedMbps: 2500 })), 'fast')
  assert.equal(speedLamp(eth('1', { linkUp: true, speedMbps: 100 })), 'slow')
  assert.equal(speedLamp(eth('1', { linkUp: true })), 'off')
  assert.equal(speedLamp(eth('1', { linkUp: false, speedMbps: 1000 })), 'off')
})
