import assert from 'node:assert/strict'
import { test } from 'node:test'

import { compareClientRanks } from './device-rank.ts'

test('moving devices lead by current speed, not last-seen time', () => {
  const recent = { rate: 0, lastSeen: 300 }
  const slow = { rate: 2, lastSeen: 100 }
  const fast = { rate: 8, lastSeen: 50 }
  assert.deepEqual([recent, slow, fast].sort(compareClientRanks), [fast, slow, recent])
})

test('quiet devices sort by last heard, ignoring historical traffic', () => {
  const unseen = { rate: 0, lastSeen: 0 }
  const old = { rate: 0, lastSeen: 100 }
  const recent = { rate: 0, lastSeen: 300 }
  assert.deepEqual([old, unseen, recent].sort(compareClientRanks), [recent, old, unseen])
})
