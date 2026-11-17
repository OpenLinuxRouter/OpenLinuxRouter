import assert from 'node:assert/strict'
import { test } from 'node:test'

import { categoryEvidence, median, serviceEvidence } from './measurements.ts'

/**
 * These helpers describe samples, not the score, so the fixtures are the
 * smallest snapshots a reader can reason about: one service, one family, and
 * timings whose median is obvious by hand.
 */
const sample = (over = {}) => ({
  started_at: '2026-10-19T21:06:00Z',
  completed_at: '2026-10-19T21:06:01Z',
  state: 'ok',
  stage: 'https',
  dns_ms: 4,
  tcp_ms: 10,
  response_ms: 20,
  ...over,
})

const target = (over = {}) => ({
  id: 'google',
  name: 'Google',
  url: 'https://google.com',
  family: 'ipv4',
  samples: [sample(), sample(), sample(), sample()],
  ...over,
})

const snapshot = (targets) => ({ targets })

const category = (over = {}) => ({
  id: 'latency',
  label: 'Latency',
  budget: 25,
  state: 'partial',
  score: 70,
  earned_points: 17.5,
  available_points: 25,
  components: [],
  ...over,
})

test('median ignores non-finite values and averages the middle pair', () => {
  assert.equal(median([30, 10, 20]), 20)
  assert.equal(median([10, 20, 30, 40]), 25)
  assert.equal(median([Number.NaN, 10, 20, 30]), 20)
  assert.equal(median([]), null)
})

test('a service reports medians and the variation between consecutive timings', () => {
  const [google] = serviceEvidence(snapshot([
    target({ samples: [sample({ tcp_ms: 10 }), sample({ tcp_ms: 14 }), sample({ tcp_ms: 12 }), sample({ tcp_ms: 12 })] }),
  ]))
  assert.equal(google.tcp, 12)
  assert.equal(google.response, 20)
  assert.equal(google.responded, 4)
  // |14-10| + |12-14| + |12-12| over three pairs.
  assert.equal(google.variation, 2)
})

test('failed attempts count as attempted but not as responded', () => {
  const [google] = serviceEvidence(snapshot([
    target({ samples: [sample(), sample(), sample({ state: 'failed', tcp_ms: null }), sample({ state: 'error', tcp_ms: null })] }),
  ]))
  assert.equal(google.responded, 2)
  assert.equal(google.failed, 1)
  assert.equal(google.samples, 4)
})

test('one row per service: the family with more completed responses wins', () => {
  const services = serviceEvidence(snapshot([
    target({ family: 'ipv4', samples: [sample({ state: 'failed', tcp_ms: null }), sample({ state: 'error', tcp_ms: null }), sample({ state: 'failed', tcp_ms: null })] }),
    target({ family: 'ipv6', samples: [sample(), sample(), sample()] }),
  ]))
  assert.equal(services.length, 1)
  assert.equal(services[0].family, 'ipv6')
})

test('a tie on responses falls back to the faster family', () => {
  const services = serviceEvidence(snapshot([
    target({ family: 'ipv4', samples: [sample({ response_ms: 90 }), sample({ response_ms: 90 })] }),
    target({ family: 'ipv6', samples: [sample({ response_ms: 30 }), sample({ response_ms: 30 })] }),
  ]))
  assert.equal(services[0].family, 'ipv6')
})

test('latency evidence medians the per-service medians', () => {
  const services = serviceEvidence(snapshot([
    target({ id: 'a', samples: [sample({ response_ms: 10 }), sample({ response_ms: 10 })] }),
    target({ id: 'b', samples: [sample({ response_ms: 40 }), sample({ response_ms: 40 })] }),
    target({ id: 'c', samples: [sample({ response_ms: 30 }), sample({ response_ms: 30 })] }),
  ]))
  const out = categoryEvidence(category(), services)
  assert.equal(out.kind, 'latency')
  assert.equal(out.latency.response, 30)
})

test('stability evidence sums the best family of every service', () => {
  const services = serviceEvidence(snapshot([
    target({ id: 'a', samples: [sample(), sample(), sample({ state: 'failed', tcp_ms: null })] }),
    target({ id: 'b', samples: [sample(), sample()] }),
  ]))
  const out = categoryEvidence(category({ id: 'stability' }), services)
  assert.equal(out.kind, 'stability')
  assert.equal(out.stability.attempted, 5)
  assert.equal(out.stability.responded, 4)
})

test('functionality counts services that answered at least once', () => {
  const services = serviceEvidence(snapshot([
    target({ id: 'a', samples: [sample()] }),
    target({ id: 'b', samples: [sample({ state: 'failed', tcp_ms: null })] }),
  ]))
  const out = categoryEvidence(category({ id: 'functionality' }), services)
  assert.equal(out.kind, 'functionality')
  assert.deepEqual(out.functionality, { reachable: 1, measured: 2 })
})

test('an unmeasured category carries no evidence', () => {
  const out = categoryEvidence(category({ id: 'wifi' }), [])
  assert.equal(out.kind, 'wireless')
  assert.equal(out.wireless, null)
})

test('wireless evidence counts links and the access points that answered', () => {
  const out = categoryEvidence(category({ id: 'wifi' }), [], {
    targets: [],
    wireless: {
      observed_at: '2026-11-17T04:41:16Z',
      aps: [
        { id: 'ap-1', state: 'ok', links: 2 },
        { id: 'ap-2', state: 'error', error: 'connection refused', links: 0 },
      ],
      links: [
        { ap_id: 'ap-1', mac: 'aa:bb:cc:dd:ee:01', signal_dbm: -52, snr_db: 48 },
        { ap_id: 'ap-1', mac: 'aa:bb:cc:dd:ee:02', signal_dbm: -71 },
      ],
    },
  })
  assert.equal(out.kind, 'wireless')
  assert.deepEqual(out.wireless, { links: 2, accessPoints: 2, answered: 1, weakest: -71 })
})

test('speed evidence comes from the transfer, not from samples', () => {
  const out = categoryEvidence(category({ id: 'speed' }), [], {
    targets: [],
    speed: { server: 'cloudflare', location: 'Singapore', download_mbps: 480.2, upload_mbps: 41.7 },
  })
  assert.equal(out.kind, 'speed')
  assert.equal(out.speed.download, 480.2)
})
