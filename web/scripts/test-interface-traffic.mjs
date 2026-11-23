import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { pathToFileURL } from 'node:url'
import ts from 'typescript'

// Execute the actual TypeScript rate calculation without a separate test runner.
const require = createRequire(import.meta.url)
const source = readFileSync(new URL('../src/features/link/interface-traffic.ts', import.meta.url), 'utf8')
const js = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText
  .replace("from 'react'", `from '${pathToFileURL(require.resolve('react')).href}'`)
const { nextInterfaceReadings } = await import(`data:text/javascript;base64,${Buffer.from(js).toString('base64')}`)
const row = { name: 'lan0', index: 2, mac: 'aa:bb:cc:dd:ee:ff', present: true, up: true, running: true, rx_bytes: 1000, tx_bytes: 2000 }
const reading = (seconds, change = {}, rows) => ({
  as_of: new Date(seconds * 1000).toISOString(), interfaces: rows ?? [{ ...row, ...change }], networks: [],
})
let state = nextInterfaceReadings(reading(0), new Map())
assert.deepEqual(state.get('lan0').history, [], 'one sample does not imply zero traffic')
state = nextInterfaceReadings(reading(5, { rx_bytes: 6000, tx_bytes: 12000 }), state)
assert.deepEqual(state.get('lan0').history, [{ at: 5000, rx: 1000, tx: 2000 }])
state = nextInterfaceReadings(reading(12, { rx_bytes: 13000, tx_bytes: 12000 }), state)
assert.equal(state.get('lan0').history.at(-1).rx, 1000, 'divide by actual seven-second interval')
assert.equal(state.get('lan0').history.at(-1).tx, 0, 'observed idle is zero')
const cases = [
  ['counter reset', reading(17, { rx_bytes: 100, tx_bytes: 100 })],
  ['interface recreation', reading(17, { index: 3, rx_bytes: 14000, tx_bytes: 14000 })],
  ['missing counter', reading(17, { rx_bytes: undefined })],
  ['unsafe integer', reading(17, { rx_bytes: Number.MAX_SAFE_INTEGER + 1 })],
  ['unplugged', reading(17, { running: false })],
  ['missing device', reading(17, { present: false })],
  ['long polling gap', reading(40, { rx_bytes: 14000, tx_bytes: 14000 })],
  ['clock reversal', reading(10)],
  ['invalid timestamp', { ...reading(17), as_of: 'invalid' }],
]
for (const [label, data] of cases) assert.equal(nextInterfaceReadings(data, state).get('lan0')?.history.length ?? 0, 0, label)
assert.deepEqual(nextInterfaceReadings(reading(12, { rx_bytes: 13000, tx_bytes: 12000 }), state).get('lan0').history, state.get('lan0').history, 'duplicate timestamp preserves history')
assert.equal(nextInterfaceReadings(reading(17, {}, []), state).size, 0, 'removed interface is forgotten')
state = new Map()
for (let second = 0; second <= 120; second += 5) state = nextInterfaceReadings(reading(second, { rx_bytes: second * 1000, tx_bytes: second * 500 }), state)
assert.equal(state.get('lan0').history.length, 12, 'history is bounded to 60 seconds')
assert.ok(state.get('lan0').history.every(sample => sample.at > 60000))
let drops = new Map()
drops = nextInterfaceReadings(reading(0, { rx_dropped: 0, tx_dropped: 0 }), drops)
drops = nextInterfaceReadings(reading(5, { rx_bytes: 6000, tx_bytes: 12000, rx_dropped: 2, tx_dropped: 3 }), drops)
assert.equal(drops.get('lan0').history.at(-1).dropped, 1, 'drops are totaled across both directions per second')
drops = nextInterfaceReadings(reading(10, { rx_bytes: 12000, tx_bytes: 24000, rx_dropped: 2, tx_dropped: 3 }), drops)
assert.equal(drops.get('lan0').history.at(-1).dropped, 0, 'a measured zero is a value')
drops = nextInterfaceReadings(reading(15, { rx_bytes: 18000, tx_bytes: 36000, rx_dropped: 1, tx_dropped: 3 }), drops)
assert.equal(drops.get('lan0').history.at(-1).dropped, undefined, 'a counter reset is missing, not negative')
drops = nextInterfaceReadings(reading(20, { rx_bytes: 24000, tx_bytes: 48000, rx_dropped: 1 }), drops)
assert.equal(drops.get('lan0').history.at(-1).dropped, undefined, 'one missing direction leaves the total unknown')
console.log('Interface rates: actual elapsed time, idle, reset, recreation, missing data, disconnect, stale samples, duplicate timestamps, bounded history, drops passed.')
