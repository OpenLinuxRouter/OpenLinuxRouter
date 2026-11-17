import assert from 'node:assert/strict'
import { test } from 'node:test'

import { hostOf, parseIPv4, placeDevice } from './placement.ts'

/**
 * Placement is a join between two facts the console already holds, so these
 * tests hand it an inventory entry and a list of ranges and check the one
 * answer it can support. The interesting cases are the ones where it must
 * refuse: a name, an address outside every range, and a box managed from
 * another segment all have to come back empty rather than be filed somewhere
 * plausible.
 */
const device = (address, over = {}) => ({ id: 'box', adapter: 'mercury', address, ...over })
const lan = { name: 'lan', subnet: '172.16.1.0/24' }

test('a management address inside a range places the box there', () => {
  assert.equal(placeDevice(device('http://172.16.1.238'), [lan]), 'lan')
  assert.equal(placeDevice(device('https://172.16.1.250:8443'), [lan]), 'lan')
})

test('an address outside every range places nothing', () => {
  assert.equal(placeDevice(device('http://192.168.1.1'), [lan]), undefined)
  assert.equal(placeDevice(device('http://172.16.2.238'), [lan]), undefined)
})

test('a network without a range places nothing', () => {
  assert.equal(placeDevice(device('http://172.16.1.238'), [{ name: 'lan' }]), undefined)
  assert.equal(placeDevice(device('http://172.16.1.238'), [{ name: 'lan', subnet: 'not-a-range' }]), undefined)
})

test('the narrowest matching range wins', () => {
  const wide = { name: 'everywhere', subnet: '172.16.0.0/16' }
  const narrow = { name: 'lan', subnet: '172.16.1.0/24' }
  assert.equal(placeDevice(device('http://172.16.1.238'), [wide, narrow]), 'lan')
  assert.equal(placeDevice(device('http://172.16.1.238'), [narrow, wide]), 'lan')
  assert.equal(placeDevice(device('http://172.16.9.238'), [wide, narrow]), 'everywhere')
})

test('a management address reached by name places nothing', () => {
  assert.equal(placeDevice(device('http://switch.lan'), [lan]), undefined)
  assert.equal(placeDevice(device('http://[fd00::1]'), [lan]), undefined)
})

test('the edge ranges behave: /32 is one address, /0 is everything', () => {
  assert.equal(placeDevice(device('http://172.16.1.238'), [{ name: 'one', subnet: '172.16.1.238/32' }]), 'one')
  assert.equal(placeDevice(device('http://172.16.1.239'), [{ name: 'one', subnet: '172.16.1.238/32' }]), undefined)
  assert.equal(placeDevice(device('http://203.0.113.9'), [{ name: 'all', subnet: '0.0.0.0/0' }]), 'all')
})

test('hostOf keeps a literal un bracketed and refuses a non-URL', () => {
  assert.equal(hostOf('http://172.16.1.238'), '172.16.1.238')
  assert.equal(hostOf('http://[fd00::1]:8080'), 'fd00::1')
  assert.equal(hostOf('172.16.1.238'), undefined)
})

test('parseIPv4 refuses what a dotted quad is not', () => {
  assert.equal(parseIPv4('172.16.1.238'), 0xac1001ee)
  assert.equal(parseIPv4('0.0.0.0'), 0)
  assert.equal(parseIPv4('255.255.255.255'), 0xffffffff)
  for (const bad of ['172.16.1', '172.16.1.256', '172.16.01.1', '172.16.1.-1', '', 'a.b.c.d', '172.16.1.238.1']) {
    assert.equal(parseIPv4(bad), undefined, bad)
  }
})
