import assert from 'node:assert/strict'
import { test } from 'node:test'

import {
  bandLabel,
  channelFrequency,
  channelOverlap,
  channelsFor,
  findingsFor,
  formatDuration,
  idFromAddress,
  isPrivateAddress,
  linkTech,
  occupancyByChannel,
  scanSummary,
  signalBars,
  signalLevel,
  snr,
  sortClients,
  sortNeighbours,
  standardLabel,
} from './wireless.ts'

const radio = (over = {}) => ({ id: 'radio0', band: '2g', protocols: ['n', 'ax'], ...over })
const client = (over = {}) => ({ mac: '02:AA:BB:CC:DD:EE', network_id: 'phy0-ap0', radio_id: 'radio0', ...over })
const snapshot = (over = {}) => ({ device_id: 'ap', observed_at: '2026-01-01T00:00:00Z', identity: {}, capabilities: {}, collections: {}, radios: [radio()], wireless: [], associations: [], ...over })

test('bands and standards are named the way people ask for them', () => {
  assert.equal(bandLabel('2g'), '2.4 GHz')
  assert.equal(bandLabel('5g'), '5 GHz')
  assert.equal(bandLabel(undefined), 'Unknown band')
  assert.equal(standardLabel(['n', 'ac', 'ax']), 'Wi-Fi 6')
  assert.equal(standardLabel(['n', 'ac', 'ax', 'be']), 'Wi-Fi 7')
  assert.equal(standardLabel(['n']), 'Wi-Fi 4')
  assert.equal(standardLabel([]), '')
})

test('signal thresholds and bars follow one scale', () => {
  assert.equal(signalLevel(-50), 'good')
  assert.equal(signalLevel(-70), 'fair')
  assert.equal(signalLevel(-80), 'weak')
  assert.equal(signalLevel(undefined), 'unknown')
  assert.deepEqual([-50, -60, -70, -80, undefined].map(signalBars), [4, 3, 2, 1, 0])
})

test('the negotiated link names its MCS family and width', () => {
  assert.equal(linkTech(client({ tx_mcs: 9, tx_width_mhz: 80 }), radio({ band: '5g', protocols: ['ax'] })), 'HE-MCS 9 · 80 MHz')
  assert.equal(linkTech(client({ tx_mcs: 7, tx_width_mhz: 20 }), radio({ protocols: ['n'] })), 'MCS 7 · 20 MHz')
  assert.equal(linkTech(client({}), radio()), '')
  assert.equal(snr(client({ signal_dbm: -60, noise_dbm: -95 })), 35)
  assert.equal(snr(client({ signal_dbm: -60 })), undefined)
})

test('duration compresses to hours, minutes and seconds', () => {
  assert.equal(formatDuration(45), '45s')
  assert.equal(formatDuration(90), '2m')
  assert.equal(formatDuration(5400), '1h 30m')
  assert.equal(formatDuration(7200), '2h')
  assert.equal(formatDuration(3599), '1h')
  assert.equal(formatDuration(3599 * 2), '2h')
  assert.equal(formatDuration(undefined), '—')
})

test('a locally administered address is not a vendor identity', () => {
  assert.equal(isPrivateAddress('02:AA:BB:CC:DD:EE'), true)
  assert.equal(isPrivateAddress('00:11:22:33:44:55'), false)
})

test('a width the driver did not honour is the first finding', () => {
  const findings = findingsFor(snapshot({ radios: [radio({ configured_width_mhz: 40, width_mhz: 20 })] }))
  assert.equal(findings.length, 1)
  assert.equal(findings[0].id, 'width-radio0')
  assert.equal(findings[0].tone, 'warning')
  assert.deepEqual(findings[0].values, { band: '2.4 GHz', configured: 40, actual: 20 })
})

test('weak clients are listed with their addresses, and strong ones are not', () => {
  const weak = findingsFor(snapshot({ associations: [client({ signal_dbm: -80 }), client({ mac: '02:AA:BB:CC:DD:EF', signal_dbm: -55 })] }))
  assert.equal(weak.length, 1)
  assert.equal(weak[0].id, 'weak-clients')
  assert.equal(weak[0].values.clients, '02:AA:BB:CC:DD:EE')
  assert.equal(findingsFor(snapshot({ associations: [client({ signal_dbm: -55 })] })).length, 0)
})

test('high utilization is a notice, not a fault', () => {
  const findings = findingsFor(snapshot({ radios: [radio({ utilization_percent: 72 })] }))
  assert.equal(findings[0].id, 'utilization-radio0')
  assert.equal(findings[0].tone, 'notice')
  assert.equal(findingsFor(snapshot({ radios: [radio({ utilization_percent: 30 })] })).length, 0)
  assert.equal(findingsFor(snapshot({ radios: [radio()] })).length, 0)
})

test('clients sort by band first, then by signal', () => {
  const radios = [radio({ id: 'r24', band: '2g' }), radio({ id: 'r5', band: '5g' })]
  const sorted = sortClients([
    client({ mac: '02:AA:BB:CC:DD:01', radio_id: 'r24', signal_dbm: -50 }),
    client({ mac: '02:AA:BB:CC:DD:02', radio_id: 'r5', signal_dbm: -70 }),
    client({ mac: '02:AA:BB:CC:DD:03', radio_id: 'r5', signal_dbm: -60 }),
  ], snapshot({ radios }))
  assert.deepEqual(sorted.map(item => item.mac), ['02:AA:BB:CC:DD:03', '02:AA:BB:CC:DD:02', '02:AA:BB:CC:DD:01'])
})

test('each band lists the channels a reader would name', () => {
  assert.deepEqual(channelsFor('2g'), [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13])
  assert.equal(channelsFor('5g')[0], 36)
  assert.equal(channelsFor('5g').at(-1), 165)
  assert.deepEqual(channelsFor('6g'), [])
  assert.equal(channelFrequency(6, '2g'), 2437)
  assert.equal(channelFrequency(14, '2g'), 2484)
  assert.equal(channelFrequency(149, '5g'), 5745)
})

test('overlap is judged on occupied air, not on equal channel numbers', () => {
  // 80 MHz at 149 reaches 153, so a 40 MHz neighbour at 153 shares the air.
  assert.equal(channelOverlap({ channel: 149, width_mhz: 80 }, { channel: 153, width_mhz: 40 }, '5g'), true)
  assert.equal(channelOverlap({ channel: 149, width_mhz: 80 }, { channel: 165, width_mhz: 20 }, '5g'), false)
  // 2.4 GHz channels six apart are 30 MHz apart: 20 MHz each, no overlap.
  assert.equal(channelOverlap({ channel: 1, width_mhz: 20 }, { channel: 7, width_mhz: 20 }, '2g'), false)
  assert.equal(channelOverlap({ channel: 1, width_mhz: 40 }, { channel: 4, width_mhz: 20 }, '2g'), true)
  // Unknown positions are not evidence of overlap.
  assert.equal(channelOverlap({ width_mhz: 20 }, { channel: 6 }, '2g'), false)
})

test('a scan summary separates co-channel from wider overlap', () => {
  const summary = scanSummary([
    { bssid: 'A', channel: 36, width_mhz: 80, signal_dbm: -70 },
    { bssid: 'B', channel: 40, width_mhz: 40, signal_dbm: -55 },
    { bssid: 'C', channel: 149, width_mhz: 80, signal_dbm: -80 },
  ], radio({ band: '5g', channel: 36, width_mhz: 80 }))
  assert.equal(summary.heard, 3)
  assert.equal(summary.coChannel, 1)
  assert.equal(summary.overlapping, 2)
  assert.equal(summary.strongest.bssid, 'B')
})

test('neighbours sort by signal and occupy the strip by primary channel', () => {
  const neighbours = [
    { bssid: 'A', channel: 1, signal_dbm: -80 },
    { bssid: 'B', channel: 11, signal_dbm: -50 },
    { bssid: 'C', channel: 1, signal_dbm: -60 },
    { bssid: 'D', signal_dbm: -40 },
  ]
  assert.deepEqual(sortNeighbours(neighbours).map(item => item.bssid), ['D', 'B', 'C', 'A'])
  const strip = occupancyByChannel(neighbours, '2g')
  assert.equal(strip.length, 13)
  assert.deepEqual(strip.filter(item => item.count > 0), [{ channel: 1, count: 2 }, { channel: 11, count: 1 }])
})

test('a device ID comes from the management address without inventing one', () => {
  assert.equal(idFromAddress('http://192.168.1.2'), '192-168-1-2')
  assert.equal(idFromAddress('https://ap.lan/'), 'ap-lan')
  assert.equal(idFromAddress('192.168.1.2'), '192-168-1-2')
  assert.equal(idFromAddress(''), '')
  assert.equal(idFromAddress('http://[fd00::1]'), 'fd00-1')
})
