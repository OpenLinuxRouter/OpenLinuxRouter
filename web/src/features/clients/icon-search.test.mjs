import assert from 'node:assert/strict'
import { test } from 'node:test'

import { iconMatch, searchIcons, searchWords } from './icon-search.ts'

const items = (...texts) => texts.map((text) => ({ text }))
const found = (query, ...texts) => searchIcons(items(...texts), (item) => item.text, query).map((item) => item.text)

test('words are lowercase and punctuation is only a gap', () => {
  assert.deepEqual(searchWords('Mac mini'), ['mac', 'mini'])
  assert.deepEqual(searchWords('nas/4bay'), ['nas', '4bay'])
  assert.deepEqual(searchWords('4-bay NAS'), ['4', 'bay', 'nas'])
  assert.deepEqual(searchWords('   '), [])
  assert.deepEqual(searchWords('—'), [])
})

test('a tile whose name starts with the query leads', () => {
  const first = 'MacBook apple laptop'
  const second = 'iMac apple desktop'
  assert.equal(iconMatch(first, 'mac'), 0)
  assert.equal(iconMatch(second, 'mac'), 2)
  assert.deepEqual(found('mac', second, first), [first, second])
})

test('a tag that is the whole query outranks a word that merely starts with it', () => {
  const tagged = 'iMac apple desktop mac'
  const hardware = 'Workshop machinery'
  assert.equal(iconMatch(tagged, 'mac'), 0)
  assert.equal(iconMatch(hardware, 'mac'), 1)
  assert.deepEqual(found('mac', hardware, tagged), [tagged, hardware])
})

test('a word inside a word still answers, behind the ones found by name', () => {
  const named = 'Small TV television'
  const buried = 'Plasma speaker sound'
  assert.equal(iconMatch(buried, 'sma'), 2)
  assert.deepEqual(found('sma', buried, named), [named, buried])
})

test('every term has to appear, so two words narrow', () => {
  const mini = 'Mac mini apple desktop'
  const laptop = 'MacBook apple laptop'
  assert.deepEqual(found('mac mini', laptop, mini), [mini])
  assert.deepEqual(found('mac', laptop, mini), [laptop, mini])
})

test('case and punctuation do not change what matches', () => {
  const bay = '4-bay NAS nas network storage'
  for (const query of ['4-bay', '4bay', '4 bay', '  NAS  ']) {
    assert.deepEqual(found(query, bay), [bay], query)
  }
})

test('a query with no words matches nothing at all', () => {
  const any = 'Phone smartphone'
  for (const query of ['', '   ', '-']) {
    assert.deepEqual(found(query, any), [], JSON.stringify(query))
    assert.equal(iconMatch(any, query), -1)
  }
})

test('equal matches keep the order they were given', () => {
  const texts = ['Brother printer', 'Epson printer', 'HP printer']
  assert.deepEqual(found('printer', ...texts), texts)
})
