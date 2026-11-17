import assert from 'node:assert/strict'
import { test } from 'node:test'

import { attachedClients, equipmentFacts, planReads } from './equipment.ts'

/**
 * The card shows what a reading said and nothing it did not. The cases that
 * matter are the quiet ones: a box with no port table, an access point with no
 * association list, and a client the inventory has never named.
 */
const snapshot = (over = {}) => ({
  device_id: 'box',
  observed_at: '2026-11-17T12:00:00Z',
  identity: {},
  capabilities: {},
  collections: {},
  ...over,
})

test('a reading with no identity, ports or clients has nothing to say', () => {
  assert.deepEqual(equipmentFacts(snapshot()), {})
})

test('ports count the ones with a carrier, and only when a table was read', () => {
  const ports = [
    { id: '1', kind: 'ethernet', link_up: false },
    { id: '2', kind: 'ethernet', link_up: true },
    { id: '3', kind: 'ethernet' },
  ]
  assert.deepEqual(equipmentFacts(snapshot({ ports })).ports, { up: 1, total: 3 })
  // An empty table is not a box with no ports; it is a box that listed none.
  assert.equal(equipmentFacts(snapshot({ ports: [] })).ports, undefined)
  assert.equal(equipmentFacts(snapshot()).ports, undefined)
})

test('an absent association list is unknown, an empty one is nobody', () => {
  assert.equal(equipmentFacts(snapshot()).wireless, undefined)
  assert.equal(equipmentFacts(snapshot({ associations: [] })).wireless, 0)
  assert.equal(equipmentFacts(snapshot({ associations: [{ mac: 'aa:bb:cc:dd:ee:ff' }] })).wireless, 1)
})

test('the model is manufacturer and model, never a bare separator', () => {
  assert.equal(equipmentFacts(snapshot({ identity: { manufacturer: 'Mercury', model: 'SE109 Pro' } })).model, 'Mercury SE109 Pro')
  assert.equal(equipmentFacts(snapshot({ identity: { model: 'SE109 Pro' } })).model, 'SE109 Pro')
  assert.equal(equipmentFacts(snapshot({ identity: { manufacturer: 'Mercury' } })).model, 'Mercury')
  assert.equal(equipmentFacts(snapshot({ identity: { manufacturer: '', model: '' } })).model, undefined)
})

test('an unread device has no facts at all', () => {
  assert.equal(equipmentFacts(undefined), undefined)
})

test('attached clients take the inventory name, and keep the address when it has none', () => {
  const clients = new Map([['aa:bb:cc:dd:ee:ff', { name: 'iPad' }]])
  const out = attachedClients(
    snapshot({
      associations: [
        { mac: 'AA:BB:CC:DD:EE:FF', signal_dbm: -51, radio_id: 'radio0' },
        { mac: '11:22:33:44:55:66' },
      ],
    }),
    clients,
  )
  assert.deepEqual(out, [
    { mac: 'AA:BB:CC:DD:EE:FF', name: 'iPad', signal: -51 },
    { mac: '11:22:33:44:55:66', name: '11:22:33:44:55:66', signal: undefined },
  ])
})

test('a device with no associations lists no clients', () => {
  assert.deepEqual(attachedClients(undefined, new Map()), [])
  assert.deepEqual(attachedClients(snapshot(), new Map()), [])
})

test('a box already answered for this session is not asked again', () => {
  const boxes = [{ id: 'a' }, { id: 'b' }, { id: 'c' }]
  const answered = new Set(['b'])
  assert.deepEqual(planReads(boxes, id => answered.has(id)).map(box => box.id), ['a', 'c'])
})

test('the pass keeps inventory order and skips nothing that has no answer', () => {
  const boxes = [{ id: 'z' }, { id: 'a' }]
  assert.deepEqual(planReads(boxes, () => false).map(box => box.id), ['z', 'a'])
  assert.deepEqual(planReads([], () => false), [])
})
