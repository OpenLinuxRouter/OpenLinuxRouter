// Regenerate the bundled, public-domain Natural Earth map from v5.1.2 GeoJSON:
// https://github.com/nvkelso/natural-earth-vector/tree/v5.1.2/geojson
// node scripts/build-region-map.mjs /path/to/natural-earth/geojson
// No network access or additional dependencies are needed by this script.
import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'

const source = process.argv[2]
if (!source) throw new Error('Pass the directory containing the Natural Earth v5.1.2 GeoJSON files')
const read = name => JSON.parse(readFileSync(resolve(source, `${name}.geojson`), 'utf8')).features
const round = point => point.map(value => Math.round(value * 1000) / 1000)

// Douglas–Peucker at ~2 km: retain administrative shapes, omit street detail.
function simplify(points, tolerance = 0.02) {
  if (points.length <= 2) return points.map(round)
  const keep = new Set([0, points.length - 1])
  const pending = [[0, points.length - 1]]
  while (pending.length) {
    const [first, last] = pending.pop()
    const a = points[first], b = points[last]
    const dx = b[0] - a[0], dy = b[1] - a[1], length = dx * dx + dy * dy
    let furthest = -1, distance = tolerance * tolerance
    for (let i = first + 1; i < last; i++) {
      const point = points[i]
      const t = length ? Math.max(0, Math.min(1, ((point[0] - a[0]) * dx + (point[1] - a[1]) * dy) / length)) : 0
      const squared = (point[0] - a[0] - t * dx) ** 2 + (point[1] - a[1] - t * dy) ** 2
      if (squared > distance) { furthest = i; distance = squared }
    }
    if (furthest !== -1) { keep.add(furthest); pending.push([first, furthest], [furthest, last]) }
  }
  const result = [...keep].sort((a, b) => a - b).map(index => round(points[index]))
  // Closed polygon rings must retain at least three distinct vertices.
  if (points[0][0] === points.at(-1)[0] && points[0][1] === points.at(-1)[1] && result.length < 4) return points.map(round)
  return result
}

const feature = (properties, geometry) => ({ type: 'Feature', properties, geometry })
const point = (properties, coordinates) => feature(properties, { type: 'Point', coordinates: round(coordinates) })
const features = []
// Countries carry outlines and names only. The map paints one neutral land
// tone on one sea tone, so a per-country fill index would be data nothing
// reads; hue on this map belongs to traffic.
for (const { properties: p, geometry } of read('ne_50m_admin_0_countries')) {
  const country = p.ISO_A2_EH !== '-99' ? p.ISO_A2_EH : p.ADM0_A3
  const polygons = geometry.type === 'Polygon' ? [geometry.coordinates] : geometry.coordinates
  const coordinates = polygons.map(polygon => polygon.map(ring => simplify(ring)))
  features.push(feature({ kind: 'country', country }, { type: 'MultiPolygon', coordinates }))
  features.push(point({ kind: 'country-label', name: p.NAME, rank: p.LABELRANK, minzoom: p.MIN_LABEL }, [p.LABEL_X, p.LABEL_Y]))
}
const boundaries = new Map()
for (const { properties: p, geometry } of read('ne_10m_admin_1_states_provinces_lines')) {
  if (!geometry) continue
  const lines = geometry.type === 'LineString' ? [geometry.coordinates] : geometry.coordinates
  if (!boundaries.has(p.ADM0_A3)) boundaries.set(p.ADM0_A3, [])
  boundaries.get(p.ADM0_A3).push(...lines.map(line => simplify(line)))
}
for (const [country, coordinates] of boundaries) features.push(feature({ kind: 'boundary', country }, { type: 'MultiLineString', coordinates }))
for (const { properties: p } of read('ne_10m_admin_1_states_provinces')) {
  const name = p.name_en || p.name
  if (name && Number.isFinite(p.longitude) && Number.isFinite(p.latitude)) {
    // This regional map stops at z6. Bring admin names forward rather than
    // inheriting thresholds intended for a basemap that zooms into streets.
    const minzoom = p.scalerank <= 2 ? 3 : p.scalerank <= 4 ? 4 : 5
    features.push(point({ kind: 'region-label', country: p.iso_a2, name, rank: p.labelrank, minzoom }, [p.longitude, p.latitude]))
  }
}
for (const { properties: p, geometry } of read('ne_10m_populated_places_simple')) {
  if (p.scalerank > 5 && p.adm0cap !== 1) continue
  if (!p.name || !geometry || !geometry.coordinates.every(Number.isFinite)) continue
  const minzoom = p.adm0cap === 1 || p.worldcity === 1 ? 3 : p.pop_max >= 1000000 ? 4 : 5
  features.push(point({ kind: 'city-label', country: p.iso_a2, name: p.name, rank: -p.pop_max - p.adm0cap * 1000000000, minzoom }, geometry.coordinates))
}
const output = resolve(import.meta.dirname, '../src/features/world-traffic/regions.geojson')
const data = JSON.stringify({ type: 'FeatureCollection', features }) + '\n'
writeFileSync(output, data)
console.log(`${features.length} features, ${(Buffer.byteLength(data) / 1024).toFixed(0)} KiB → ${output}`)
