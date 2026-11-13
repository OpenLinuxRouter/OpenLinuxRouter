import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

const data = readFileSync(new URL('./regions.geojson', import.meta.url))
const { features } = JSON.parse(data)

test('regional map includes global first-order boundaries and names', () => {
  // Natural Earth's 50m admin-1 data only covers the US and Canada. Guard
  // against silently replacing the global 10m source with that smaller file.
  const boundaries = new Set(features.filter(f => f.properties.kind === 'boundary').map(f => f.properties.country))
  for (const country of ['CHN', 'USA', 'CAN', 'AUS', 'IND', 'BRA', 'DEU', 'ZAF']) assert.ok(boundaries.has(country), country)
  const regions = features.filter(f => f.properties.kind === 'region-label')
  for (const [country, name] of [['CN', 'Beijing'], ['US', 'California'], ['DE', 'Bavaria'], ['AU', 'Queensland']]) {
    assert.ok(regions.some(f => f.properties.country === country && f.properties.name === name), name)
  }
  assert.ok(features.some(f => f.properties.kind === 'country' && f.properties.country === 'SG'), 'Singapore has a land outline')
})

test('map geometry is valid at the dateline and contains no street detail', () => {
  const allowed = new Set(['country', 'country-label', 'boundary', 'region-label', 'city-label'])
  const position = coordinates => {
    assert.equal(coordinates.length, 2)
    assert.ok(Number.isFinite(coordinates[0]) && Math.abs(coordinates[0]) <= 180, String(coordinates))
    assert.ok(Number.isFinite(coordinates[1]) && Math.abs(coordinates[1]) <= 90, String(coordinates))
  }
  for (const { properties, geometry } of features) {
    assert.ok(allowed.has(properties.kind))
    if (geometry.type === 'Point') {
      assert.ok(properties.name)
      position(geometry.coordinates)
    } else if (geometry.type === 'MultiPolygon') {
      for (const polygon of geometry.coordinates) for (const ring of polygon) {
        assert.ok(ring.length >= 4)
        assert.deepEqual(ring[0], ring.at(-1))
        assert.ok(new Set(ring.map(p => p.join(','))).size >= 3)
        ring.forEach(position)
      }
    } else {
      assert.equal(geometry.type, 'MultiLineString')
      for (const line of geometry.coordinates) {
        assert.ok(line.length >= 2)
        line.forEach(position)
        for (let i = 1; i < line.length; i++) assert.ok(Math.abs(line[i][0] - line[i - 1][0]) <= 180, 'split boundaries at the dateline')
      }
    }
  }
  assert.ok(data.length < 4 * 1024 * 1024, 'regional data must remain a lightweight local asset')
})

test('province names and major cities are available before maximum zoom', () => {
  const regions = features.filter(f => f.properties.kind === 'region-label')
  for (const name of ['Henan', 'Hubei', 'Guangdong', 'California', 'Bavaria']) {
    const region = regions.find(f => f.properties.name === name)
    assert.ok(region, name)
    assert.ok(region.properties.minzoom <= 4, `${name} should appear at regional zoom`)
  }
  const cities = features.filter(f => f.properties.kind === 'city-label')
  for (const name of ['Beijing', 'Wuhan', 'Guangzhou', 'London', 'New York', 'Tokyo', 'San Francisco']) {
    const city = cities.find(f => f.properties.name === name)
    assert.ok(city, name)
    assert.ok(city.properties.minzoom <= 5, `${name} should appear before maximum zoom`)
  }
  assert.ok(cities.length > 1000 && cities.length < 1500, 'limit the dataset to capitals and major cities')
  for (const f of [...regions, ...cities]) {
    assert.ok(Number.isInteger(f.properties.minzoom), 'symbol layout zooms are evaluated at integer levels')
    assert.ok(f.properties.minzoom >= 3 && f.properties.minzoom <= 5)
    assert.ok(Number.isFinite(f.properties.rank))
  }
})

test('countries carry outlines and names, not fill indices', () => {
  // Geometry is enough to draw the map: land and sea are each one neutral
  // tone, so a per-country palette index would be data nothing reads, and
  // colour on this map belongs to traffic alone.
  const countries = features.filter(f => f.properties.kind === 'country')
  assert.ok(countries.length > 200, 'every country keeps its outline')
  for (const { properties } of features) assert.ok(!('color' in properties), 'no leftover fill index')
})

test('neighbouring countries still meet exactly at shared borders', () => {
  const countries = features.filter(f => f.properties.kind === 'country')
  const edges = new Map()
  let shared = 0
  for (const { properties: p, geometry } of countries) for (const polygon of geometry.coordinates) for (const ring of polygon) {
    for (let i = 1; i < ring.length; i++) {
      const edge = [ring[i - 1].join(','), ring[i].join(',')].sort().join(':')
      const other = edges.get(edge)
      if (other && other !== p.country) shared++
      edges.set(edge, p.country)
    }
  }
  assert.ok(shared > 1000, 'shared borders still meet at identical vertices')
})
