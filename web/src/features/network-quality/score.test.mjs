import assert from 'node:assert/strict'
import { test } from 'node:test'

import { scoreText } from './score.ts'

test('a score keeps one decimal and drops a trailing zero', () => {
  assert.equal(scoreText(67.3440317), '67.3')
  assert.equal(scoreText(67), '67')
  assert.equal(scoreText(99.96), '100')
  assert.equal(scoreText(0), '0')
})
