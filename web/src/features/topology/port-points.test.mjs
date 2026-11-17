import assert from 'node:assert/strict'
import { test } from 'node:test'

import { faceGeometry, naturalWidth } from './port-points.ts'
import { FRAME_W } from './face.ts'

/**
 * The map draws a cable from the port the switch named, so the port's position
 * has to come out of the same layout the drawing used. These tests pin the two
 * things that would silently break the map: a port's x increasing with its
 * index, and every port sitting inside the drawing it belongs to.
 */
const ports = (n, kind = 'ethernet') =>
  Array.from({ length: n }, (_, i) => ({ id: `p${i + 1}`, kind, linkUp: true }))

test('a switch row puts its ports left to right, inside the drawing', () => {
  const faces = ports(9)
  const width = naturalWidth(faces, 'switch', 44)
  const geometry = faceGeometry(faces, 'switch', width)
  assert.equal(geometry.ports.length, 9)
  assert.equal(geometry.width, width)
  for (const [index, port] of geometry.ports.entries()) {
    assert.ok(port.x > 0 && port.x < width, `port ${index} inside`)
    assert.ok(port.y > 0 && port.y < geometry.height, `port ${index} inside vertically`)
    if (index > 0) assert.ok(port.x > geometry.ports[index - 1].x, 'left to right')
  }
  // One shield is 44px across at that natural width.
  const spacing = geometry.ports[1].x - geometry.ports[0].x
  assert.ok(spacing > 44 && spacing < 50, `spacing ${spacing}`)
})

test('the port count changes the drawing, not the port size', () => {
  const two = faceGeometry(ports(2), 'switch', naturalWidth(ports(2), 'switch', 44))
  const nine = faceGeometry(ports(9), 'switch', naturalWidth(ports(9), 'switch', 44))
  const stepOf = geometry => geometry.ports[1].x - geometry.ports[0].x
  assert.ok(Math.abs(stepOf(two) - stepOf(nine)) < 0.5)
  assert.ok(nine.width > two.width)
})

test('a product form keeps its ports inside the body', () => {
  for (const form of ['ont', 'ap', 'router']) {
    const faces = ports(form === 'router' ? 2 : 4)
    const width = naturalWidth(faces, form, 44)
    const geometry = faceGeometry(faces, form, width)
    assert.equal(geometry.ports.length, faces.length, form)
    for (const port of geometry.ports) {
      assert.ok(port.x > 0 && port.x < width, `${form} x`)
      assert.ok(port.y > 0 && port.y < geometry.height, `${form} y`)
    }
  }
})

test('an access point reserves room for its antennas above the ports', () => {
  const faces = ports(4)
  const width = naturalWidth(faces, 'ap', 44)
  const ap = faceGeometry(faces, 'ap', width).ports[0]
  const ont = faceGeometry(faces, 'ont', naturalWidth(faces, 'ont', 44)).ports[0]
  assert.ok(ap.y > ont.y, 'the antenna strip pushes the panel down')
})

test('a fibre port still has a place to draw a cable from', () => {
  const faces = [{ id: 'sfp', kind: 'fiber', linkUp: false }, ...ports(3)]
  const geometry = faceGeometry(faces, 'switch', naturalWidth(faces, 'switch', 44))
  assert.equal(geometry.ports.length, 4)
  assert.ok(geometry.ports[0].x < geometry.ports[1].x)
  void FRAME_W
})
