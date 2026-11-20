import assert from 'node:assert/strict'
import { test } from 'node:test'

import { summarizeLatency } from './latency.ts'

const slots = (samples) => {
  const out = Array.from({ length: 24 }, () => null)
  for (const [hour, milliseconds] of samples) out[hour] = { milliseconds, checked_at: '', hour_start: '' }
  return out
}

test('one site contributes its hourly sample to that hour', () => {
  const today = summarizeLatency([slots([[3, 12]])])
  assert.equal(today.points[3].ms, 12)
  assert.equal(today.points[4].ms, null)
  assert.equal(today.sites, 1)
  assert.equal(today.samples, 1)
})

test('two sites at one hour are averaged and p95 is taken from hourly means', () => {
  const today = summarizeLatency([
    slots([[1, 10], [2, 20]]),
    slots([[1, 30], [2, 40]]),
  ])
  assert.equal(today.points[1].ms, 20)
  assert.equal(today.points[2].ms, 30)
  assert.equal(today.samples, 2)
  assert.equal(today.max, 30)
})
