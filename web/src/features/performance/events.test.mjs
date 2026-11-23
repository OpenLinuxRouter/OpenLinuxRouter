import assert from 'node:assert/strict'
import { test } from 'node:test'

import { FALL_SHARE, median, scanEvents } from './events.ts'
import { BUSY_CPU, DROP_BUSY } from './verdict.ts'

const MiB = 1024 * 1024

const sample = (at, rx, tx = 0, dropped) => ({
  at,
  rx: rx * MiB,
  tx: tx * MiB,
  ...(dropped == null ? {} : { dropped }),
})

const cpu = (at, pct) => ({ at, pct })

const scan = (history, cpuSamples = []) => scanEvents({ start: 0, end: 60_000, history, cpuSamples })

test('a throughput fall below half the window median fires with the measured pair', () => {
  const history = [
    sample(0, 84), sample(1000, 84), sample(2000, 84),
    sample(3000, 84), sample(4000, 84), sample(5000, 84),
    sample(6000, 4),
  ]
  const { events } = scan(history)
  assert.equal(events.length, 1)
  const event = events[0]
  assert.equal(event.kind, 'throughput')
  assert.equal(event.direction, 'down')
  assert.equal(event.at, 6000)
  assert.equal(event.fromBps, 84 * MiB)
  assert.equal(event.toBps, 4 * MiB)
  assert.equal(event.overMs, 1000)
  assert.equal(FALL_SHARE, 0.5)
})

test('a smaller fall, a normal CPU and a few drops do not fire', () => {
  const history = [
    sample(0, 100, 1, 0), sample(1000, 100, 1, 2),
    sample(2000, 100, 1, 10), sample(3000, 60, 1, 20),
  ]
  const { events, watched } = scan(history, [cpu(0, 40), cpu(1000, 60)])
  assert.deepEqual(events, [])
  assert.deepEqual(watched, ['throughput', 'cpu', 'drops'])
})

test('a quiet window is quiet and watched, not an empty strip', () => {
  const history = [
    sample(0, 40, 2, 0), sample(1000, 41, 2, 0), sample(2000, 39, 2, 0),
  ]
  const { events, watched } = scan(history, [cpu(0, 4), cpu(1000, 5)])
  assert.deepEqual(events, [])
  assert.deepEqual(watched, ['throughput', 'cpu', 'drops'])
})

test('CPU and drops fire only when they cross their published thresholds', () => {
  const history = [
    sample(0, 40, 2, 2), sample(1000, 40, 2, DROP_BUSY - 1), sample(2000, 40, 2, DROP_BUSY + 34),
  ]
  const { events } = scan(history, [cpu(0, BUSY_CPU - 2), cpu(1000, BUSY_CPU + 4), cpu(2000, BUSY_CPU + 10)])
  const kinds = events.map(event => event.kind)
  assert.deepEqual(kinds, ['cpu', 'drops'])
  const cpuEvent = events.find(event => event.kind === 'cpu')
  assert.deepEqual(cpuEvent, { kind: 'cpu', at: 1000, fromPercent: BUSY_CPU - 2, toPercent: BUSY_CPU + 4 })
  const dropEvent = events.find(event => event.kind === 'drops')
  assert.deepEqual(dropEvent, { kind: 'drops', at: 2000, fromPerSecond: DROP_BUSY - 1, toPerSecond: DROP_BUSY + 34 })
})

test('a fall needs a previously high reading in the same window', () => {
  // All readings are low; the window's median is low too, so nothing was high.
  assert.deepEqual(scan([sample(0, 4), sample(1000, 4), sample(2000, 4)]).events, [])
  // A rise is not a fall.
  assert.deepEqual(scan([sample(0, 4), sample(1000, 100)]).events, [])
})

test('a sustained low is one event, not one per sample', () => {
  const history = [
    sample(0, 100), sample(1000, 100), sample(2000, 100), sample(3000, 100),
    sample(4000, 4), sample(5000, 4), sample(6000, 4),
  ]
  assert.equal(scan(history).events.length, 1)
})

test('a source with fewer than two samples is not watched', () => {
  const single = scan([sample(0, 100, 0, 0)], [cpu(0, 5)])
  assert.deepEqual(single.watched, [])
  assert.deepEqual(single.events, [])
  // The drop series alone can still be watched while throughput is too short.
  assert.deepEqual(scan([sample(0, 100, 0, 0)]).watched, [])
})

test('the median is the middle value, or the mean of the two middle values', () => {
  assert.equal(median([3, 1, 2]), 2)
  assert.equal(median([4, 1, 3, 2]), 2.5)
  assert.equal(median([]), undefined)
  assert.equal(median([1, Number.NaN, 3]), 2)
})
