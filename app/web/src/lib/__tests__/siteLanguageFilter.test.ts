import { describe, expect, it } from 'vitest'

/**
 * 搜索时的「站点语言」多选筛选。
 *
 * 用途：明确知道目标片名属于哪个语区时，不必让全部 79 个站都发一遍请求。
 *   - 黄石（Yellowstone）→ 只搜英文站
 *   - 百日谎言（100일의 거짓말）→ 只搜韩文站
 *
 * 关键约定：
 *   - 空选择 = 不限语言，后端照旧全站扇出
 *   - 多选时命中任一语言即参与
 *   - 语言按主标签归组，zh-CN 与 zh-TW 都算 "zh"
 */

interface Facet {
  language: string
  label: string
  count: number
}

/** 与后端 languageGroup 等价。 */
function languageGroup(lang: string): string {
  const normalised = lang.trim().toLowerCase()
  if (!normalised) return ''
  const dash = normalised.indexOf('-')
  return dash > 0 ? normalised.slice(0, dash) : normalised
}

/** 与 SearchPanel.toggleLanguage 等价。 */
function toggle(list: string[], language: string): string[] {
  const next = list.slice()
  const at = next.indexOf(language)
  if (at >= 0) next.splice(at, 1)
  else next.push(language)
  return next
}

/** 与后端 candidatesFor 的语言命中判断等价。 */
function siteMatches(siteLang: string, selected: string[]): boolean {
  if (selected.length === 0) return true
  const wanted = new Set(selected.filter((s) => s && s !== 'all').map(languageGroup))
  if (wanted.size === 0) return true
  return wanted.has(languageGroup(siteLang))
}

/** 与 SearchPanel.languageFilterLabel 等价。 */
function label(selected: string[], facets: Facet[]): string {
  if (selected.length === 0) return '全部站点语言'
  if (selected.length === 1) {
    const hit = facets.find((f) => f.language === selected[0])
    return hit?.label ?? selected[0]
  }
  return `${selected.length} 种语言`
}

const facets: Facet[] = [
  { language: 'en', label: '英语', count: 40 },
  { language: 'zh', label: '简体中文', count: 15 },
  { language: 'ko', label: '韩语', count: 2 },
  { language: 'ru', label: '俄语', count: 8 },
]

describe('站点语言筛选', () => {
  it('不选任何语言时所有站点都参与', () => {
    for (const lang of ['en-US', 'ko-KR', 'zh-CN', 'ru-RU', '']) {
      expect(siteMatches(lang, [])).toBe(true)
    }
  })

  it('单选时只保留该语区的站点', () => {
    expect(siteMatches('en-US', ['en'])).toBe(true)
    expect(siteMatches('ko-KR', ['en'])).toBe(false)
    expect(siteMatches('zh-CN', ['en'])).toBe(false)
  })

  it('多选时命中任一即参与', () => {
    expect(siteMatches('en-US', ['en', 'ko'])).toBe(true)
    expect(siteMatches('ko-KR', ['en', 'ko'])).toBe(true)
    expect(siteMatches('zh-CN', ['en', 'ko'])).toBe(false)
  })

  it('地区变体归入同一语言组', () => {
    expect(siteMatches('zh-CN', ['zh'])).toBe(true)
    expect(siteMatches('zh-TW', ['zh'])).toBe(true)
    expect(siteMatches('en-GB', ['en'])).toBe(true)
    expect(siteMatches('en-AU', ['en'])).toBe(true)
  })

  it('未声明语言的站点只在「不限语言」时参与', () => {
    expect(siteMatches('', [])).toBe(true)
    expect(siteMatches('', ['en'])).toBe(false)
  })

  it('切换语言是增删语义', () => {
    expect(toggle([], 'en')).toEqual(['en'])
    expect(toggle(['en'], 'ko')).toEqual(['en', 'ko'])
    expect(toggle(['en', 'ko'], 'en')).toEqual(['ko'])
    expect(toggle(['en'], 'en')).toEqual([])
  })

  it('按钮文字随选择变化', () => {
    expect(label([], facets)).toBe('全部站点语言')
    expect(label(['en'], facets)).toBe('英语')
    expect(label(['ko'], facets)).toBe('韩语')
    expect(label(['en', 'ko'], facets)).toBe('2 种语言')
  })

  it('未知语言标签回退到原始代码', () => {
    expect(label(['xx'], facets)).toBe('xx')
  })
})
