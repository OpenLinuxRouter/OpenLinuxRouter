import assert from 'node:assert/strict'
import { test } from 'node:test'

import {
  coveredWindow,
  coverageCopy,
  formatFrameRate,
  formatShare,
  formatPercent,
  linkPoints,
  sampleCount,
  usableCapacity,
  usableCapacityData,
  utilisation,
} from './samples.ts'

test('utilisation converts measured bytes per second against a bit-rate baseline', () => {
  assert.equal(utilisation(12_500_000, 100_000_000), 1)
  assert.equal(utilisation(6_250_000, 100_000_000), 0.5)
  assert.equal(utilisation(undefined, 100_000_000), undefined)
  assert.equal(utilisation(1, 0), undefined)
  assert.equal(utilisation(1, undefined), undefined)
})

test('a missing capacity stays missing rather than becoming a number', () => {
  assert.equal(usableCapacity(undefined), undefined)
  assert.equal(usableCapacity({ p99_bps: 0, peak_bps: 10, points: 1, since: '2026-01-01T00:00:00Z' }), undefined)
  assert.equal(usableCapacity({ p99_bps: 10, peak_bps: 10, points: 1, since: '2026-01-01T00:00:00Z' }), 10)
  assert.equal(usableCapacityData(undefined), undefined)
  assert.equal(usableCapacityData({ uplink: 'ens18', rx: { p99_bps: 0 }, tx: { p99_bps: 0 }, state: 'measured', as_of: '2026-01-01T00:00:00Z' }), undefined)
})

test('percentages keep a decimal only where it changes the reading', () => {
  assert.equal(formatPercent(0.041), '4.1%')
  assert.equal(formatPercent(0.42), '42%')
  assert.equal(formatPercent(1.2), '120%')
  assert.equal(formatPercent(undefined), undefined)
})

test('a zero drop count reads as zero, while an absent one is a dash', () => {
  assert.equal(formatFrameRate(0), '0')
  assert.equal(formatFrameRate(1.25), '1.3')
  assert.equal(formatFrameRate(12.4), '12')
  assert.equal(formatFrameRate(undefined), undefined)
  assert.equal(formatShare(0.00058), '<0.1%')
  assert.equal(formatShare(0.0123), '1.2%')
  assert.equal(formatShare(0), '0%')
  assert.equal(formatShare(undefined), undefined)
})

test('the provisional sentence names the window it actually covered', () => {
  assert.deepEqual(coverageCopy(6 * 3_600_000), { key: 'Provisional baseline. Based on {count} hours of samples.', count: 6 })
  assert.deepEqual(coverageCopy(12 * 60_000), { key: 'Provisional baseline. Based on {count} minutes of samples.', count: 12 })
  assert.deepEqual(coverageCopy(90 * 60_000), { key: 'Provisional baseline. Based on {count} minutes of samples.', count: 90 })
  assert.equal(coverageCopy(0), undefined)
  const covered = coveredWindow({
    uplink: 'ens18',
    rx: { p99_bps: 1, peak_bps: 1, points: 2, since: '2026-01-01T00:00:00Z' },
    tx: { p99_bps: 1, peak_bps: 1, points: 3, since: '2026-01-01T01:00:00Z' },
    state: 'warming',
    as_of: '2026-01-01T06:00:00Z',
  })
  // The shorter direction window is the one both directions can claim.
  assert.equal(covered, 5 * 3_600_000)
})

test('the published sample count is the conservative direction', () => {
  assert.equal(sampleCount({
    uplink: 'ens18',
    rx: { p99_bps: 1, peak_bps: 1, points: 12, since: '2026-01-01T00:00:00Z' },
    tx: { p99_bps: 1, peak_bps: 1, points: 7, since: '2026-01-01T00:00:00Z' },
    state: 'measured',
    as_of: '2026-01-01T00:00:00Z',
  }), 7)
})

test('latency is attached to near throughput samples and never invents a point', () => {
  const history = [
    { at: 10_000, rx: 100, tx: 50 },
    { at: 11_000, rx: 200, tx: 60 },
    { at: 12_000, rx: 300, tx: 70 },
  ]
  const points = linkPoints(history, [{ at: 11_400, ms: 24 }, { at: 25_000, ms: 30 }], 0)
  assert.deepEqual(points, [
    { at: 10_000, down: 100, up: 50 },
    { at: 11_000, down: 200, up: 60, latency: 24 },
    { at: 12_000, down: 300, up: 70 },
  ])
  assert.deepEqual(linkPoints([{ at: 5_000, rx: 1, tx: 1 }], [{ at: 9_000, ms: 5 }], 0), [{ at: 5_000, down: 1, up: 1 }])
})
