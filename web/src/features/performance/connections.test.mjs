import assert from 'node:assert/strict'
import { test } from 'node:test'

import {
  clientAddressLabel,
  clientHref,
  clientName,
  clientThroughput,
  DEFAULT_CLIENT_SORT,
  isCensusMeasured,
  nextClientSort,
  sameClient,
  sortClients,
  usableConnections,
  visibleClients,
} from './connections.ts'

const client = (over = {}) => ({
  ip: '172.16.1.10',
  mac: 'aa:bb:cc:dd:ee:01',
  name: 'Laptop',
  connections: 10,
  rx_bps: 10_000_000,
  tx_bps: 1_000_000,
  per_connection: { p50_bps: 500_000 },
  ...over,
})

test('throughput sorts busiest first by default and missing values last', () => {
  const clients = [
    client({ name: 'quiet', rx_bps: 1_000_000, tx_bps: 0 }),
    client({ name: 'unknown', rx_bps: undefined, tx_bps: undefined }),
    client({ name: 'busy', rx_bps: 40_000_000, tx_bps: 2_000_000 }),
    client({ name: 'half', rx_bps: 9_000_000, tx_bps: undefined }),
  ]
  assert.deepEqual(sortClients(clients).map(item => item.name), ['busy', 'half', 'quiet', 'unknown'])
  assert.equal(DEFAULT_CLIENT_SORT.key, 'throughput')
  assert.equal(DEFAULT_CLIENT_SORT.direction, 'desc')
  // Reversing the numeric sort must not put the unreported client first.
  assert.deepEqual(sortClients(clients, { key: 'throughput', direction: 'asc' }).map(item => item.name), ['quiet', 'half', 'busy', 'unknown'])
})

test('sorting by name and per-connection p50 is deterministic', () => {
  const clients = [
    client({ name: 'Zeta', per_connection: { p50_bps: 100 } }),
    client({ name: 'alpha', per_connection: { p50_bps: 900 } }),
    client({ name: 'beta', per_connection: {} }),
  ]
  assert.deepEqual(sortClients(clients, { key: 'name', direction: 'asc' }).map(item => item.name), ['alpha', 'beta', 'Zeta'])
  assert.deepEqual(sortClients(clients, { key: 'p50', direction: 'desc' }).map(item => item.name), ['alpha', 'Zeta', 'beta'])
})

test('a header starts a new numeric column descending and toggles the current one', () => {
  assert.deepEqual(nextClientSort({ key: 'throughput', direction: 'desc' }, 'throughput'), { key: 'throughput', direction: 'asc' })
  assert.deepEqual(nextClientSort({ key: 'throughput', direction: 'desc' }, 'name'), { key: 'name', direction: 'asc' })
  assert.deepEqual(nextClientSort({ key: 'name', direction: 'asc' }, 'connections'), { key: 'connections', direction: 'desc' })
})

test('the visible cap holds rows back and reports how many', () => {
  const clients = Array.from({ length: 12 }, (_, index) => client({ name: `client-${index}` }))
  const collapsed = visibleClients(clients, false)
  assert.equal(collapsed.visible.length, 8)
  assert.equal(collapsed.hidden, 4)
  const expanded = visibleClients(clients, true)
  assert.equal(expanded.visible.length, 12)
  assert.equal(expanded.hidden, 0)
  assert.deepEqual(visibleClients(clients.slice(0, 3), false), { visible: clients.slice(0, 3), hidden: 0 })
})

test('a missing rate is absent, not a zero', () => {
  assert.equal(clientThroughput(client({ rx_bps: undefined, tx_bps: undefined })), undefined)
  assert.equal(clientThroughput(client({ rx_bps: 8, tx_bps: undefined })), 8)
  assert.equal(clientThroughput(client({ rx_bps: 0, tx_bps: 0 })), 0)
})

test('names and links fall back to what the endpoint reported', () => {
  assert.equal(clientName(client({ name: '  ' })), '172.16.1.10')
  assert.equal(clientName(client({ name: undefined, ip: undefined, mac: 'aa:bb' })), 'aa:bb')
  assert.equal(clientName(client({ name: undefined, ip: undefined, mac: undefined })), undefined)
  assert.equal(clientHref(client()), '/clients/aa%3Abb%3Acc%3Add%3Aee%3A01')
  assert.equal(clientHref(client({ mac: undefined })), undefined)
  assert.equal(clientAddressLabel(client()), '172.16.1.10 · aa:bb:cc:dd:ee:01')
})

test('the same client is recognised across readings by the stable identifier', () => {
  assert.equal(sameClient(client(), client({ rx_bps: 99 })), true)
  assert.equal(sameClient(client({ mac: undefined, ip: '10.0.0.1' }), client({ mac: undefined, ip: '10.0.0.1', name: 'Renamed' })), true)
  assert.equal(sameClient(client({ mac: undefined, ip: undefined, name: 'Renamed' }), client({ mac: undefined, ip: undefined, name: 'Renamed' })), true)
  assert.equal(sameClient(client({ mac: 'aa:bb' }), client({ mac: 'cc:dd' })), false)
  assert.equal(sameClient(client({ mac: undefined, ip: undefined, name: undefined }), client({ mac: undefined, ip: undefined, name: undefined })), false)
})

test('an empty clients array is a measured empty answer; a missing one is not', () => {
  assert.deepEqual(usableConnections({ as_of: '2026-01-01T00:00:00Z', window_seconds: 10, clients: [] }), {
    clients: [],
    windowSeconds: 10,
    asOf: '2026-01-01T00:00:00Z',
    state: undefined,
    error: undefined,
  })
  assert.equal(usableConnections({ clients: undefined }), undefined)
  assert.equal(usableConnections(undefined), undefined)
  // A malformed companion field is omitted rather than making the table unusable.
  assert.deepEqual(usableConnections({ as_of: 'not a time', window_seconds: 0, clients: [client()] }), {
    clients: [client()],
    windowSeconds: undefined,
    asOf: undefined,
    state: undefined,
    error: undefined,
  })
})

test('only ready is a measurement; warming, disabled and unreadable are named states', () => {
  assert.equal(isCensusMeasured(undefined), true)
  assert.equal(isCensusMeasured('ready'), true)
  assert.equal(isCensusMeasured('warming'), false)
  assert.equal(isCensusMeasured('disabled'), false)
  assert.equal(isCensusMeasured('unavailable'), false)
  const disabled = usableConnections({ state: 'disabled', error: 'Statistics are off', clients: [] })
  assert.equal(disabled.state, 'disabled')
  assert.equal(disabled.error, 'Statistics are off')
  // A state this build does not understand is not a measurement.
  assert.equal(usableConnections({ state: 'something-new', clients: [] }).state, 'unavailable')
  const ready = usableConnections({ state: 'ready', error: '', clients: [] })
  assert.equal(ready.state, 'ready')
  assert.equal(ready.error, undefined)
})
