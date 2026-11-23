import assert from 'node:assert/strict'
import { test } from 'node:test'

import { NEAR_CAPACITY, verdict } from './verdict.ts'

const measured = (over = {}) => verdict({
  capacity: 'measured',
  throughput: true,
  downloadUtil: 0.2,
  uploadUtil: 0.2,
  cpuPercent: 40,
  dropsPerSecond: 0,
  ...over,
})

test('a missing baseline is its own answer, never a zero', () => {
  assert.equal(verdict({ capacity: 'missing', throughput: true }).code, 'no-capacity')
  // The line cannot be scored before the baseline has even been read.
  assert.equal(verdict({ capacity: 'loading', throughput: true }).code, 'checking')
  // A measured baseline with no current rate is a different missing thing.
  assert.equal(verdict({ capacity: 'measured', throughput: false }).code, 'no-throughput')
})

test('the busiest direction decides which capacity is being approached', () => {
  assert.equal(measured({ uploadUtil: NEAR_CAPACITY, downloadUtil: 0.1 }).code, 'upload-near-capacity')
  assert.equal(measured({ downloadUtil: NEAR_CAPACITY, uploadUtil: 0.1 }).code, 'download-near-capacity')
  assert.equal(measured({ uploadUtil: NEAR_CAPACITY - 0.01 }).code, 'link-free')
})

test('a dropping router answers the question while the line has headroom', () => {
  assert.equal(measured({ dropsPerSecond: 2 }).code, 'router-dropping')
  // A measured zero is not a drop.
  assert.equal(measured({ dropsPerSecond: 0 }).code, 'link-free')
  // Drops are unknown, not zero: CPU alone decides, and low CPU is idle.
  assert.equal(measured({ dropsPerSecond: undefined, cpuPercent: 4 }).code, 'link-free-router-idle')
})

test('CPU decides the router side only where the numbers support it', () => {
  assert.equal(measured({ cpuPercent: 80 }).code, 'link-free-router-busy')
  assert.equal(measured({ cpuPercent: 4 }).code, 'link-free-router-idle')
  assert.equal(measured({ cpuPercent: 40 }).code, 'link-free')
  assert.equal(measured({ cpuPercent: undefined }).code, 'link-free')
})

test('a warming baseline is marked provisional without changing the evidence', () => {
  assert.deepEqual(verdict({ capacity: 'warming', throughput: true, uploadUtil: 0.9, downloadUtil: 0.1, cpuPercent: 4 }), {
    code: 'upload-near-capacity',
    provisional: true,
  })
  assert.equal(measured().provisional, false)
})

test('throughput with no usable denominator does not claim the line is free', () => {
  assert.equal(verdict({ capacity: 'measured', throughput: true }).code, 'no-utilisation')
})
