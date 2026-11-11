import assert from 'node:assert/strict'
import { test } from 'node:test'

import { servicesByClient, servicesOf } from './services.ts'

test('published services stay with the same device after rename and name reuse', () => {
  const mac = 'aa:bb:cc:00:00:01'
  const service = { name: 'files', upstream: { client: mac, port: 80 } }
  const devices = [
    { mac, name: 'storage' },
    { mac: 'aa:bb:cc:00:00:02', name: 'nas' },
  ]
  const result = servicesByClient({ enabled: true, services: [service] }, devices)
  assert.deepEqual(result.get(mac), [service])
  assert.equal(result.has(devices[1].mac), false)
})

test('a device page matches its services by MAC, stored name and address', () => {
  const mac = 'aa:bb:cc:00:00:01'
  const services = [
    { name: 'files', upstream: { client: mac, port: 80 } },
    { name: 'legacy', upstream: { client: 'storage', port: 80 } },
    { name: 'nas', upstream: { host: '192.168.1.20', port: 5001 } },
    { name: 'other', upstream: { host: '192.168.1.99', port: 80 } },
  ]
  const matched = servicesOf(services, { mac, name: 'storage', addresses: ['192.168.1.20'] })
  assert.deepEqual(matched.map((s) => s.name), ['files', 'legacy', 'nas'])
})

test('a device that dialled in matches on its tunnel address alone', () => {
  const services = [
    { name: 'grafana', upstream: { host: '10.6.0.3', port: 3000 } },
    { name: 'files', upstream: { client: 'aa:bb:cc:00:00:01', port: 80 } },
  ]
  assert.deepEqual(servicesOf(services, { addresses: ['10.6.0.3'] }).map((s) => s.name), ['grafana'])
})
