import { describe, expect, it } from 'vitest'
import { formatAge, formatCount, seedHealth } from '@/lib/torrentHealth'

describe('seedHealth', () => {
  it('marks a zero-seeder torrent as dead', () => {
    const health = seedHealth(0)
    expect(health.tone).toBe('error')
    expect(health.label).toBe('无人做种')
  })

  it('warns on a handful of seeders', () => {
    expect(seedHealth(3).tone).toBe('warning')
  })

  it('accepts a healthy seeder count without a label', () => {
    const health = seedHealth(42)
    expect(health.tone).toBe('success')
    expect(health.label).toBe('')
  })

  it('does not call a site that never reported seeders dead', () => {
    const health = seedHealth(0, false)
    expect(health.tone).toBe('muted')
    expect(health.label).toBe('做种未知')
  })
})

describe('formatCount', () => {
  it('keeps small numbers as-is', () => {
    expect(formatCount(7)).toBe('7')
    expect(formatCount(0)).toBe('0')
  })

  it('abbreviates thousands and millions', () => {
    expect(formatCount(1234)).toBe('1.2k')
    expect(formatCount(23_456)).toBe('23k')
    expect(formatCount(2_400_000)).toBe('2.4M')
  })
})

describe('formatAge', () => {
  const now = Date.parse('2026-11-22T12:00:00Z')

  it('renders relative time for absolute timestamps', () => {
    expect(formatAge('2026-11-22T09:00:00Z', now)).toBe('3 小时前')
    expect(formatAge('2026-11-20T12:00:00Z', now)).toBe('2 天前')
    expect(formatAge('2026-03-01T12:00:00Z', now)).toBe('8 个月前')
    expect(formatAge('2024-11-22T12:00:00Z', now)).toBe('2 年前')
  })

  it('maps the literal words some trackers emit', () => {
    expect(formatAge('now', now)).toBe('今天')
    expect(formatAge('yesterday', now)).toBe('昨天')
  })

  it('returns empty for unparsable text so the UI hides it', () => {
    expect(formatAge('', now)).toBe('')
    expect(formatAge('Каталог файлов » Сэмплы', now)).toBe('')
  })
})
