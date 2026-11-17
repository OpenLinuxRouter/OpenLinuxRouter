import assert from 'node:assert/strict'
import { test } from 'node:test'

import { attachedClients, equipmentFacts, farEnd, learnedByPort, planReads } from './equipment.ts'

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

test('addresses are grouped per port, and a moved address lands once', () => {
  const byPort = learnedByPort([
    { mac: 'AA:BB:CC:00:00:01', port_id: '2' },
    { mac: 'aa:bb:cc:00:00:02', port_id: '2' },
    { mac: 'aa:bb:cc:00:00:03', port_id: '4' },
  ])
  assert.deepEqual([...byPort], [['2', ['aa:bb:cc:00:00:01', 'aa:bb:cc:00:00:02']], ['4', ['aa:bb:cc:00:00:03']]])
  // A second report for the same address is the table's latest word, not a
  // second address: two ports do not both hold it.
  const moved = learnedByPort([
    { mac: 'aa:bb:cc:00:00:01', port_id: '2' },
    { mac: 'aa:bb:cc:00:00:01', port_id: '5' },
  ])
  assert.deepEqual([...moved], [['5', ['aa:bb:cc:00:00:01']]])
})

test('a table with nothing in it groups nothing', () => {
  assert.equal(learnedByPort([]).size, 0)
})

test('an address is named by what it is: this router, a box, or a client', () => {
  const sources = {
    self: new Map([['aa:bb:cc:00:00:01', 'ens19']]),
    clients: new Map([
      ['aa:bb:cc:00:00:02', { name: 'DS1522plus', ip: '172.16.1.5' }],
      ['aa:bb:cc:00:00:03', { name: 'lis-mac-mini', ip: '172.16.1.9' }],
    ]),
    devices: [{ id: 'ont', name: 'Huawei ONT', adapter: 'huawei', address: 'http://172.16.1.5' }],
  }
  assert.deepEqual(farEnd('AA:BB:CC:00:00:01', sources), { mac: 'aa:bb:cc:00:00:01', kind: 'router', name: 'ens19' })
  const box = farEnd('aa:bb:cc:00:00:02', sources)
  assert.equal(box.kind, 'device')
  assert.equal(box.deviceId, 'ont')
  assert.equal(box.name, 'Huawei ONT')
  const client = farEnd('aa:bb:cc:00:00:03', sources)
  assert.equal(client.kind, 'client')
  assert.equal(client.name, 'lis-mac-mini')
  // An address nothing knows keeps the address instead of borrowing a name.
  assert.equal(farEnd('aa:bb:cc:00:00:99', sources).name, 'aa:bb:cc:00:00:99')
})
