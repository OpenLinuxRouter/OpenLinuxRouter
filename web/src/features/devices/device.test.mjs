import assert from 'node:assert/strict'
import { test } from 'node:test'

import {
  adapterLabel,
  capabilityWord,
  collectionWord,
  duplexLabel,
  formatCount,
  formatSpeed,
  formatUptime,
  idFromAddress,
  macValid,
  portKindLabel,
  portName,
  portState,
  roleLabels,
  sortPorts,
  vlanValid,
} from './device.ts'

test('ports are listed the way the hardware labels them', () => {
  const ports = [{ id: '10' }, { id: '2' }, { id: 'lan' }, { id: '1' }, { id: 'wan' }, { id: 'eth0' }]
  assert.deepEqual(sortPorts(ports).map((port) => port.id), ['1', '2', '10', 'eth0', 'lan', 'wan'])
})

test('a port that is off, unplugged and unreported are three different answers', () => {
  assert.equal(portState({ id: '1', enabled: false, link_up: false }), 'Disabled')
  assert.equal(portState({ id: '1', enabled: true, link_up: true }), 'Connected')
  assert.equal(portState({ id: '1', enabled: true, link_up: false }), 'No cable')
  assert.equal(portState({ id: '1' }), 'Unknown')
})

test('an unsupported collection is not a fault and not an empty answer', () => {
  assert.equal(collectionWord('ok'), 'OK')
  assert.equal(collectionWord('unsupported'), 'Unavailable')
  assert.equal(collectionWord('error'), 'Not answering')
  assert.equal(capabilityWord('supported'), 'OK')
  assert.equal(capabilityWord('unsupported'), 'Not in use')
  assert.equal(capabilityWord('unknown'), 'Unknown')
})

test('speeds, counters and uptimes read the way an operator says them', () => {
  assert.equal(formatSpeed(100), '100 Mbps')
  assert.equal(formatSpeed(1000), '1 Gbps')
  assert.equal(formatSpeed(2500), '2.5 Gbps')
  assert.equal(formatSpeed(10_000), '10 Gbps')
  assert.equal(duplexLabel('full'), 'Full duplex')
  assert.equal(duplexLabel(undefined), '')
  assert.equal(formatCount(999), '999')
  assert.equal(formatCount(1234), '1.2k')
  assert.equal(formatCount(1_200_000), '1.2M')
  assert.equal(formatUptime(45), '0m')
  assert.equal(formatUptime(3 * 3600 + 12 * 60), '3h 12m')
  assert.equal(formatUptime(3 * 86_400 + 4 * 3600), '3d 4h')
})

test('the equipment words name the adapter and every role a box carries', () => {
  assert.equal(adapterLabel('mercury'), 'Mercury')
  assert.equal(adapterLabel('openwrt'), 'OpenWrt')
  assert.equal(adapterLabel('huawei'), 'Huawei ONT')
  assert.deepEqual(roleLabels({ id: 'core', adapter: 'mercury', address: 'http://x', roles: ['ap', 'switch'] }), ['Switch', 'Access point'])
  assert.deepEqual(roleLabels({ id: 'ap', adapter: 'openwrt', address: 'http://x' }), [])
  assert.deepEqual(roleLabels({ id: 'ont', adapter: 'huawei', address: 'http://x', roles: ['ont', 'router'] }), ['ONT', 'Router'])
})

test('kinds and port IDs keep the device’s own names when the contract has none', () => {
  assert.equal(portKindLabel('ethernet'), 'Ethernet')
  assert.equal(portKindLabel('other'), 'Other')
  assert.equal(portKindLabel('custom-kind'), 'custom-kind')
  const ports = [{ id: '1', name: 'Port 1', kind: 'ethernet' }]
  assert.equal(portName(ports, '1'), 'Port 1')
  assert.equal(portName(ports, '2'), '2')
})

test('MAC and VLAN fields are checked before a read is asked for', () => {
  assert.equal(macValid('02:11:22:33:44:55'), true)
  assert.equal(macValid('02:11:22:33:44'), false)
  assert.equal(macValid('02-11-22-33-44-55'), false)
  assert.equal(vlanValid(1), true)
  assert.equal(vlanValid(4094), true)
  assert.equal(vlanValid(0), false)
  assert.equal(vlanValid(4095), false)
})

test('a device ID comes from the management address without inventing one', () => {
  assert.equal(idFromAddress('http://192.168.1.2'), '192-168-1-2')
  assert.equal(idFromAddress('https://ap.lan/'), 'ap-lan')
  assert.equal(idFromAddress('192.168.1.2'), '192-168-1-2')
  assert.equal(idFromAddress(''), '')
  assert.equal(idFromAddress('http://[fd00::1]'), 'fd00-1')
})
