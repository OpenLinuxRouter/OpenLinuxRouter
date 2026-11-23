import assert from 'node:assert/strict'
import { test } from 'node:test'

import {
  apiCapReached,
  clientFlows,
  flowEndpoints,
  flowPeer,
  flowRate,
  flowShare,
  peerLabel,
  shareOfTotal,
  sortFlows,
  usableFlows,
  visibleFlows,
} from './flows.ts'

const client = {
  ip: '172.16.1.32',
  mac: 'aa:bb:cc:dd:ee:32',
  name: 'DS1522plus',
  rx_bps: 400_000_000,
  tx_bps: 0,
}

const flow = (over = {}) => ({
  proto: 'tcp',
  src: '172.16.1.32',
  dst: '223.73.85.15',
  sport: 56789,
  dport: 13913,
  state: 'ESTABLISHED',
  rx_bps: 0,
  tx_bps: 42_000_000,
  client: 'DS1522plus',
  ...over,
})

test('a flow share is its measured rate over the client total', () => {
  assert.equal(shareOfTotal(42_000_000, 400_000_000), 0.105)
  assert.equal(shareOfTotal(0, 400_000_000), 0)
  assert.equal(shareOfTotal(42_000_000, 0), undefined)
  assert.equal(shareOfTotal(42_000_000, undefined), undefined)
  assert.equal(shareOfTotal(undefined, 400_000_000), undefined)
  // An over-100% share is the arithmetic, not a value to hide.
  assert.equal(shareOfTotal(500_000_000, 400_000_000), 1.25)
})

test('a flow rate is the sum of the measured directions', () => {
  assert.equal(flowRate(flow()), 42_000_000)
  assert.equal(flowRate(flow({ rx_bps: undefined, tx_bps: undefined })), undefined)
  assert.equal(flowRate(flow({ rx_bps: 8, tx_bps: undefined })), 8)
  assert.equal(flowShare(flow({ rx_bps: 0, tx_bps: 42_000_000 }), client), 0.105)
})

test('flows sort by rate with the unmeasured last', () => {
  const flows = [
    flow({ dst: 'slow', tx_bps: 1_000_000 }),
    flow({ dst: 'unknown', rx_bps: undefined, tx_bps: undefined }),
    flow({ dst: 'fast', tx_bps: 90_000_000 }),
  ]
  assert.deepEqual(sortFlows(flows).map(item => item.dst), ['fast', 'slow', 'unknown'])
})

test('the drawer cap holds rows back and counts them', () => {
  const flows = Array.from({ length: 11 }, (_, index) => flow({ dport: 1000 + index }))
  const collapsed = visibleFlows(flows, false)
  assert.equal(collapsed.visible.length, 8)
  assert.equal(collapsed.hidden, 3)
  assert.deepEqual(visibleFlows(flows, true), { visible: flows, hidden: 0 })
  assert.deepEqual(visibleFlows(flows.slice(0, 2), false), { visible: flows.slice(0, 2), hidden: 0 })
})

test('the API cap is reported only when the response reached it', () => {
  assert.equal(apiCapReached(20, 20), true)
  assert.equal(apiCapReached(21, 20), true)
  assert.equal(apiCapReached(19, 20), false)
  assert.equal(apiCapReached(Number.NaN, 20), false)
})

test('a client owns the flows the gateway attributed to it, or that carry its address', () => {
  const flows = [
    flow({ client: 'DS1522plus' }),
    flow({ client: undefined, dst: '172.16.1.32', src: '8.8.8.8' }),
    flow({ client: 'Other', src: '172.16.1.99', dst: '1.1.1.1' }),
    flow({ client: undefined, src: '172.16.1.32' }),
  ]
  assert.equal(clientFlows(flows, client).length, 3)
})

test('the peer is the side that is not this client, with its port', () => {
  assert.deepEqual(flowPeer(flow({ src: '172.16.1.32', dst: '223.73.85.15', dport: 13913 }), client), {
    address: '223.73.85.15',
    port: 13913,
  })
  assert.deepEqual(flowPeer(flow({ src: '223.73.85.15', dst: '172.16.1.32', sport: 443 }), client), {
    address: '223.73.85.15',
    port: 443,
  })
  assert.deepEqual(flowPeer(flow(), { ...client, ip: undefined }), {})
  assert.equal(peerLabel({ address: '223.73.85.15', port: 13913 }), '223.73.85.15:13913')
  assert.equal(peerLabel({ address: '2001:db8::1', port: 443 }), '[2001:db8::1]:443')
  assert.equal(flowEndpoints(flow()), '172.16.1.32 → 223.73.85.15')
})

test('an empty flows array is a measured empty answer; a missing one is not', () => {
  assert.deepEqual(usableFlows({ as_of: '2026-01-01T00:00:00Z', window_seconds: 10, flows: [] }), {
    flows: [],
    windowSeconds: 10,
    asOf: '2026-01-01T00:00:00Z',
    state: undefined,
    error: undefined,
  })
  assert.equal(usableFlows({ flows: undefined }), undefined)
  assert.equal(usableFlows(undefined), undefined)
})
