import assert from 'node:assert/strict'
import { test } from 'node:test'
import { getLocale, resolveLocale, setLocale, t } from './i18n.ts'

test('saved preference wins, Chinese browser preferences work and other languages fall back', () => {
  assert.equal(resolveLocale('en', ['zh-TW']), 'en')
  assert.equal(resolveLocale('zh-CN', ['en']), 'zh-CN')
  assert.equal(resolveLocale(null, ['zh-Hans-CN']), 'zh-CN')
  assert.equal(resolveLocale('invalid', ['de-DE', 'zh-CN']), 'en')
  assert.equal(resolveLocale(null, []), 'en')
})

test('switching updates shared copy, falls back safely and interpolates named values', () => {
  const previous = getLocale()
  try {
    setLocale('zh-CN')
    assert.equal(t('Gateway'), '网关')
    assert.equal(t('Unknown backend message'), 'Unknown backend message')
    assert.equal(t('Client {name}', { name: 'NAS $& <test>' }), 'Client NAS $& <test>')
    setLocale('en')
    assert.equal(t('Gateway'), 'Gateway')
  } finally { setLocale(previous) }
})
