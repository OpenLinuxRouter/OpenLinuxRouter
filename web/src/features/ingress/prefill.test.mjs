import assert from 'node:assert/strict'
import { test } from 'node:test'

import { serviceTargetPrefill } from './prefill.ts'

// The daemon decodes this body with DisallowUnknownFields, so a key that is not
// the upstream's own is not a cosmetic difference: the save is refused and the
// operator is told about a field they never typed. Asserting the whole object,
// keys included, is what catches a rename the type checker cannot see.
test('a client with a reserved address is prefilled by MAC, under the current key', () => {
  assert.deepEqual(
    serviceTargetPrefill({ mac: 'aa:bb:cc:dd:ee:ff', name: 'nuc', fixed_ip: '172.16.1.20', ips: ['172.16.1.20'] }),
    { client: 'aa:bb:cc:dd:ee:ff', port: 0 },
  )
})

test('a client with no reserved address is prefilled by the address it answered on', () => {
  assert.deepEqual(
    serviceTargetPrefill({ mac: 'aa:bb:cc:dd:ee:ff', name: 'laptop', ips: ['fe80::1', '172.16.1.31'] }),
    { host: '172.16.1.31', port: 0 },
  )
})

test('no address at all leaves the host empty rather than guessing one', () => {
  assert.deepEqual(serviceTargetPrefill({ mac: 'aa:bb:cc:dd:ee:ff', name: 'phone' }), { host: '', port: 0 })
})

test('a target the client list does not know is taken from the query string', () => {
  assert.deepEqual(serviceTargetPrefill(undefined, ' 172.16.1.9 '), { host: '172.16.1.9', port: 0 })
})

test('nothing is prefilled when the page was not opened on a target', () => {
  assert.equal(serviceTargetPrefill(undefined, undefined), undefined)
  assert.equal(serviceTargetPrefill(undefined, '   '), undefined)
})
