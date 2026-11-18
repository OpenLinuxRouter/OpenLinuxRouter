import assert from 'node:assert/strict'
import { test } from 'node:test'

import { findUpstream } from './upstream.ts'

/**
 * The modem goes above this router only when the uplink says there is a box in
 * front of it: a private way out with a managed box at the next hop, or a
 * public way out through an ONT that is not on the LAN. Everything else stays
 * where the cables put it.
 */
const modem = { id: 'huawei', address: 'http://192.168.1.1', adapter: 'huawei', roles: ['ont'] }
const sw = { id: 'sw', address: 'http://172.16.1.238', adapter: 'mercury', roles: ['switch'] }
const base = { devices: [modem, sw], lan: ['172.16.1.0/24'], ports: ['ens18', 'ens19', 'ens20'] }

test('a private way out puts the box at the next hop above, on the port the route leaves by', () => {
  assert.deepEqual(
    findUpstream({ ...base, route: { dev: 'ens18', via: '192.168.1.1', addresses: ['192.168.1.2/24'] } }),
    { deviceId: 'huawei', port: 'ens18' },
  )
})

test('a private way out with no box at the hop falls back to a box in the same subnet', () => {
  const found = findUpstream({ ...base, route: { dev: 'ens18', via: '192.168.1.254', addresses: ['192.168.1.2/24'] } })
  assert.equal(found?.deviceId, 'huawei')
})

test('a public way out lifts a bridged ONT that is not on the LAN', () => {
  const found = findUpstream({ ...base, route: { dev: 'pppoe-wan', addresses: ['58.32.1.9/32'] }, uplinkInterface: 'ens18' })
  assert.deepEqual(found, { deviceId: 'huawei', port: 'ens18' })
})

test('an ONT on the LAN is not upstream', () => {
  const lanModem = { ...modem, address: 'http://172.16.1.1' }
  assert.equal(findUpstream({ ...base, devices: [lanModem, sw], route: { dev: 'pppoe-wan', addresses: ['58.32.1.9/32'] } }), undefined)
})

test('carrier-grade NAT is not a box in the house', () => {
  assert.equal(findUpstream({ ...base, devices: [sw], route: { dev: 'ens18', via: '100.64.0.1', addresses: ['100.64.3.4/10'] } }), undefined)
})

test('no default route, nothing upstream', () => {
  assert.equal(findUpstream({ ...base, route: undefined }), undefined)
})
