import assert from 'node:assert/strict'
import { test } from 'node:test'

import { FORM_CHROME, formOf } from './form.ts'
import { faceLayout } from './face.ts'

/**
 * A form is a class, not a model. These tests pin the two rules that matter:
 * a role stated for this box beats the adapter family, and a box nothing has
 * named is drawn plain rather than guessed into a shape.
 */
test('a stated role decides the form', () => {
  assert.equal(formOf('openwrt', ['ap']), 'ap')
  assert.equal(formOf('openwrt', ['router']), 'router')
  assert.equal(formOf('huawei', ['switch']), 'switch')
  assert.equal(formOf('mercury', ['ont']), 'ont')
  // Several roles: the first that names a shape wins, ONT before gateway.
  assert.equal(formOf('huawei', ['ont', 'router']), 'ont')
})

test('without a role, the adapter names what it can', () => {
  assert.equal(formOf('mercury'), 'switch')
  assert.equal(formOf('huawei'), 'ont')
  // An OpenWrt box with no role could be an access point, a router or neither.
  assert.equal(formOf('openwrt'), 'generic')
  assert.equal(formOf(undefined), 'generic')
  assert.equal(formOf('something-else'), 'generic')
})

test('every form has chrome, and only some of it takes room', () => {
  for (const form of ['switch', 'ont', 'ap', 'router', 'generic']) {
    assert.ok(FORM_CHROME[form], form)
  }
  assert.equal(FORM_CHROME.switch.lead ?? 0, 0)
  assert.equal(FORM_CHROME.generic.top ?? 0, 0)
  // An access point is the one form that needs space above the chassis.
  assert.ok((FORM_CHROME.ap.top ?? 0) > 0)
  assert.equal(FORM_CHROME.ont.top ?? 0, 0)
})

test('a form mark widens the chassis but does not touch the ports', () => {
  const ports = [{ id: '1', kind: 'ethernet' }, { id: '2', kind: 'ethernet' }]
  const plain = faceLayout(ports)
  const marked = faceLayout(ports, FORM_CHROME.router)
  assert.equal(marked.rows[0].slots.length, plain.rows[0].slots.length)
  assert.equal(marked.width - plain.width, FORM_CHROME.router.lead)
  assert.equal(marked.rows[0].slots[0].x, plain.rows[0].slots[0].x)
})
