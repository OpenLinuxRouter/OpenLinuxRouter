import assert from 'node:assert/strict'
import { test } from 'node:test'

import { wire } from './wiring.ts'

/**
 * The cables are an inference from tables that never say "plugged into", so
 * these tests hand it the tables a small home network produces — a switch
 * between the router and two access points, a modem upstream — and check that
 * each address ends up under the box nearest to it and nowhere else.
 */
const router = name => ({ mac: `r-${name}`, kind: 'router', name })
const box = id => ({ mac: `m-${id}`, kind: 'device', name: id, deviceId: id })
const client = mac => ({ mac, kind: 'client', name: mac })

const key = w => `${w.from}${w.fromPort ? `:${w.fromPort}` : ''}>${w.to}${w.toPort ? `:${w.toPort}` : ''} ${w.kind}`

test('a home network: modem above, switch under the router, access points and clients under the switch', () => {
  const result = wire({
    devices: ['modem', 'sw', 'ap1', 'ap2'],
    sweeps: new Map([['sw', new Map([
      ['1', [router('ens19'), client('aa'), client('zz')]],
      ['3', [box('ap1'), client('w1'), client('w2')]],
      ['5', [box('ap2'), client('w3')]],
      ['7', [client('nas')]],
    ])]]),
    associations: new Map([['ap1', ['w1', 'w2']], ['ap2', ['w3']]]),
    uplink: { deviceId: 'modem', port: 'ens18' },
  })
  assert.deepEqual(result.wires.map(key).sort(), [
    'ap1>mac:w1 wireless',
    'ap1>mac:w2 wireless',
    'ap2>mac:w3 wireless',
    'modem>self:ens18 wired',
    'self:ens19>sw:1 wired',
    'sw:3>ap1 wired',
    'sw:5>ap2 wired',
    'sw:7>mac:nas wired',
  ])
  assert.deepEqual(result.above, ['modem'])
})

test('a client on a port with one managed box is behind that box', () => {
  const result = wire({
    devices: ['sw', 'ap'],
    sweeps: new Map([['sw', new Map([['2', [box('ap'), client('pc')]]])]]),
    associations: new Map(),
  })
  assert.ok(result.wires.some(w => w.from === 'ap' && w.to === 'mac:pc' && w.kind === 'wired'))
})

test('between two switches, the port that learned fewer addresses is nearer', () => {
  const result = wire({
    devices: ['a', 'b'],
    sweeps: new Map([
      ['a', new Map([['1', [client('x'), client('y'), client('z')]]])],
      ['b', new Map([['4', [client('x')]]])],
    ]),
    associations: new Map(),
  })
  const x = result.wires.find(w => w.to === 'mac:x')
  assert.equal(x.from, 'b')
  assert.equal(x.fromPort, '4')
})

test('a box nothing places is joined to the router as unknown', () => {
  const result = wire({ devices: ['ap'], sweeps: new Map(), associations: new Map() })
  assert.deepEqual(result.wires.map(key), ['self>ap unknown'])
})

test('a modem the sweep already placed is not lifted above the router', () => {
  const result = wire({
    devices: ['sw', 'modem'],
    sweeps: new Map([['sw', new Map([['1', [router('ens19')]], ['8', [box('modem')]]])]]),
    associations: new Map(),
    uplink: { deviceId: 'modem', port: 'ens18' },
  })
  assert.deepEqual(result.above, [])
  assert.ok(result.wires.some(w => w.from === 'sw' && w.to === 'modem'))
})

test('two NICs on the same switch are two cables', () => {
  const result = wire({
    devices: ['sw'],
    sweeps: new Map([['sw', new Map([['2', [router('ens19')]], ['3', [router('ens20')]]])]]),
    associations: new Map(),
  })
  assert.deepEqual(result.wires.map(key).sort(), ['self:ens19>sw:2 wired', 'self:ens20>sw:3 wired'])
})
