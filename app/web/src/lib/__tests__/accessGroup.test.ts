import { describe, expect, it } from 'vitest'
import { accessGroupOf, accessLabel } from '@/api/search'

/**
 * 站点访问类型：公开 / 非公开。
 *
 * 后端把类型分为 public / private / semi-private。界面提供两档筛选：
 * 「公开」只含 public，「非公开」含 private 与 semi-private ——因为用户真正
 * 关心的是"要不要注册或邀请才能用"。
 */
describe('access grouping', () => {
  it('labels every known type in plain Chinese', () => {
    expect(accessLabel('public')).toBe('公开')
    expect(accessLabel('private')).toBe('私有（需邀请）')
    expect(accessLabel('semi-private')).toBe('半私有（需注册）')
    expect(accessLabel('something-else')).toBe('未知')
    expect(accessLabel('')).toBe('未知')
  })

  it('treats anything not public as non-public', () => {
    expect(accessGroupOf('public')).toBe('public')
    expect(accessGroupOf('private')).toBe('nonPublic')
    expect(accessGroupOf('semi-private')).toBe('nonPublic')
    // 未知类型也必须归入某一侧，否则筛选会漏掉它们。
    expect(accessGroupOf('')).toBe('nonPublic')
  })
})
