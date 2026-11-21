import assert from 'node:assert/strict'
import { test } from 'node:test'
import { axisScale, formatTick } from './chart-ticks.ts'

/** Every scale a plot on this screen has to survive: bytes per hour, latency, a flat week. */
const maxima = [0.4, 1, 2.4, 3, 14, 41, 93, 512, 1024, 0.08, 7.5]

test('every tick gets its own label', () => {
  for (const max of maxima) {
    for (const count of [4, 5]) {
      const { ticks, decimals } = axisScale(max, count)
      const labels = ticks.map((tick) => formatTick(tick, decimals))
      assert.equal(new Set(labels).size, labels.length, `duplicate labels for max ${max}, count ${count}`)
    }
  }
})

test('ticks are evenly spaced, start at zero and cover the data', () => {
  for (const max of maxima) {
    const { ticks, top } = axisScale(max)
    assert.equal(ticks[0], 0, `axis does not start at zero for max ${max}`)
    assert.ok(top >= max, `axis top ${top} is under the data ${max}`)
    assert.equal(ticks.at(-1), top, `last tick is not the axis top for max ${max}`)
    assert.ok(ticks.length >= 3 && ticks.length <= 7, `${ticks.length} ticks for max ${max}`)
    const gaps = ticks.slice(1).map((tick, index) => Number((tick - ticks[index]).toFixed(6)))
    assert.ok(gaps.every((gap) => Math.abs(gap - gaps[0]) < 1e-9), `uneven steps for max ${max}`)
  }
})

test('a flat series still gets a readable axis', () => {
  assert.deepEqual(axisScale(0), { ticks: [0, 1], top: 1, decimals: 0 })
  assert.deepEqual(axisScale(Number.NaN), { ticks: [0, 1], top: 1, decimals: 0 })
})

test('the step decides the precision', () => {
  // 2.4 GiB per hour: half-gigabyte steps, one decimal, six labels.
  assert.deepEqual(axisScale(2.4).ticks, [0, 0.5, 1, 1.5, 2, 2.5])
  assert.equal(axisScale(2.4).decimals, 1)
  // 41 ms: whole milliseconds in 15s, which is what makes 0/15/30/45 read.
  assert.deepEqual(axisScale(41, 4), { ticks: [0, 15, 30, 45], top: 45, decimals: 0 })
  // A busy week in GiB: whole hundreds.
  assert.deepEqual(axisScale(512).ticks, [0, 150, 300, 450, 600])
  assert.equal(formatTick(0, 1), '0')
})
