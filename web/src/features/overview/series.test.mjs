import assert from 'node:assert/strict'
import { test } from 'node:test'

import { hourlyTraffic, trafficRhythm } from './series.ts'

const gib = n => n * 1024 ** 3
const bucket = (local, down, up) => ({
  at: new Date(...local).toISOString(),
  bytes: { down: gib(down), up: gib(up) },
})

test('half-hour buckets are summed into their local clock hour', () => {
  const rows = hourlyTraffic([
    bucket([2024, 4, 1, 8, 0], 1, 0.5),
    bucket([2024, 4, 1, 8, 30], 2, 0.5),
    bucket([2024, 4, 1, 9, 0], 3, 1),
  ])
  assert.deepEqual(rows.map(row => [row.label, row.downGiB, row.upGiB]), [
    ['08:00', 3, 1],
    ['09:00', 3, 1],
  ])
})

test('the hour list is bounded to the requested window', () => {
  const buckets = Array.from({ length: 30 }, (_, hour) => bucket([2024, 4, 1, hour % 24, 0], 1, 0))
  const rows = hourlyTraffic(buckets, 24)
  assert.equal(rows.length, 24)
  assert.deepEqual(rows.map(row => row.label), Array.from({ length: 24 }, (_, hour) => `${String(hour).padStart(2, '0')}:00`))
})

test('the rhythm grid anchors on local midnight and marks future hours as gaps', () => {
  const now = new Date(2024, 4, 1, 12, 30)
  const rhythm = trafficRhythm([
    bucket([2024, 3, 25, 8, 0], 1, 0.5),
    bucket([2024, 3, 25, 8, 30], 1, 0),
    bucket([2024, 4, 1, 11, 0], 2, 0),
  ], now)
  assert.equal(rhythm.days.length, 7)
  assert.equal(rhythm.days[0].date.getDate(), 25)
  assert.equal(rhythm.days[0].hours[8], 2.5)
  assert.equal(rhythm.days[6].hours[11], 2)
  assert.equal(rhythm.days[6].hours[13], null)
  assert.equal(rhythm.maxGiB, 2.5)
  assert.equal(rhythm.peakDayIndex, 0)
  assert.equal(rhythm.peakHour, 8)
  assert.equal(rhythm.nowDay, 6)
  assert.equal(rhythm.nowHour, 12)
})
