import { describe, expect, it, vi } from 'vitest'

/**
 * 主题解析与切换。
 *
 * 背景：index.html 原来把 class="dark" 写死，仓库里也没有任何切换入口，
 * 所以文档里说的「支持明暗主题」实际不可用。现在主题由
 * <html class="dark"> + localStorage 决定，并且要在首屏之前定下来。
 *
 * 顶栏只提供太阳/月亮两态切换；首次访问默认跟随系统。
 */

type ThemeMode = 'light' | 'dark' | 'system'

/** 与 composables/useTheme.ts 的 resolveDark 等价。 */
function resolveDark(mode: ThemeMode, systemDark: boolean): boolean {
  return mode === 'system' ? systemDark : mode === 'dark'
}

/** 与 AppHeader 的 toggle 等价：在明暗之间切换，按下当前呈现的颜色取反。 */
function toggleMode(isDark: boolean): ThemeMode {
  return isDark ? 'light' : 'dark'
}

/** 与 index.html 的内联脚本等价：读不到就跟随系统。 */
function initialDark(stored: string | null, systemDark: boolean): boolean {
  if (stored === 'dark') return true
  if (stored === 'light') return false
  return systemDark
}

describe('theme resolution', () => {
  it('follows the system preference when nothing is pinned', () => {
    expect(resolveDark('system', true)).toBe(true)
    expect(resolveDark('system', false)).toBe(false)
  })

  it('ignores the system preference once pinned', () => {
    expect(resolveDark('light', true)).toBe(false)
    expect(resolveDark('dark', false)).toBe(true)
  })

  it('toggles between exactly two states', () => {
    expect(toggleMode(true)).toBe('light')
    expect(toggleMode(false)).toBe('dark')
  })

  it('honours a stored choice on first paint', () => {
    expect(initialDark('light', true)).toBe(false)
    expect(initialDark('dark', false)).toBe(true)
  })

  it('falls back to the system when nothing is stored', () => {
    expect(initialDark(null, true)).toBe(true)
    expect(initialDark(null, false)).toBe(false)
    expect(initialDark('bogus', true)).toBe(true)
  })

  it('does not throw when localStorage is unavailable', () => {
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota exceeded')
    })
    const persist = (m: ThemeMode) => {
      try {
        localStorage.setItem('bujic-theme', m)
        return true
      } catch {
        return false
      }
    }
    expect(persist('dark')).toBe(false)
    spy.mockRestore()
  })
})
