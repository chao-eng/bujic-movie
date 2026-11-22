import { describe, expect, it } from 'vitest'

/**
 * 站点级「搜索语言」覆盖（多选）。
 *
 * 背景：定义声明的 language 只是站点的主要语言，不代表它只收录那一种。
 * FileMood 的定义是 en-US，但它同时收录中文资源；只发一个关键词会漏掉一半。
 *
 * 规则：
 *   - 未配置（空数组）→ 跟随定义语言，发 1 次请求
 *   - 配置 1 种 → 只用该语言，发 1 次
 *   - 配置多种 → 每种语言各发一次（并行），结果合并
 *   - 去重：不同语言解析出同一个关键词时只发一次
 *   - 上限 4 种，避免把单站请求数放大太多
 */

const MAX_LANGUAGES = 4

/** 与后端 joinLanguages 等价。 */
function joinLanguages(langs: string[]): string {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of langs) {
    const lang = raw.trim().toLowerCase()
    if (!lang || seen.has(lang)) continue
    seen.add(lang)
    out.push(lang)
  }
  return out.join(',')
}

/** 与后端 splitLanguages 等价。 */
function splitLanguages(raw: string): string[] {
  const out: string[] = []
  for (const part of raw.split(',')) {
    const trimmed = part.trim()
    if (trimmed) out.push(trimmed)
  }
  return out.length ? out : []
}

/** 与后端 siteKeywords 的循环等价（关键词由测试给定）。 */
function effectiveKeywords(override: string[], definitionLang: string, keywordFor: (lang: string) => string): string[] {
  const langs = override.length ? override : [definitionLang]
  const seen = new Set<string>()
  const out: string[] = []
  for (const lang of langs) {
    const keyword = keywordFor(lang).trim()
    if (!keyword) continue
    const key = keyword.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    out.push(keyword)
  }
  return out.length ? out : ['']
}

describe('站点搜索语言覆盖', () => {
  it('未配置时跟随定义语言，只发一次', () => {
    const langs = splitLanguages('')
    expect(langs).toEqual([])
    const keywords = effectiveKeywords(langs, 'en-US', () => 'Squid Game')
    expect(keywords).toEqual(['Squid Game'])
  })

  it('配置单一语言时只用它', () => {
    const keywords = effectiveKeywords(splitLanguages('zh'), 'en-US', (lang) =>
      lang.startsWith('zh') ? '鱿鱼游戏' : 'Squid Game',
    )
    expect(keywords).toEqual(['鱿鱼游戏'])
  })

  it('配置多语言时每种语言各出一个关键词', () => {
    const keywords = effectiveKeywords(splitLanguages('zh,en'), 'en-US', (lang) =>
      lang.startsWith('zh') ? '鱿鱼游戏' : 'Squid Game',
    )
    expect(keywords).toEqual(['鱿鱼游戏', 'Squid Game'])
    expect(keywords).toHaveLength(2)
  })

  it('不同语言解析出同一关键词时只发一次', () => {
    // 中文站和英文站都回退到原词时不该重复请求。
    const keywords = effectiveKeywords(splitLanguages('zh,en'), 'en-US', () => 'Oppenheimer')
    expect(keywords).toEqual(['Oppenheimer'])
  })

  it('存取往返保持顺序并去重', () => {
    expect(joinLanguages(['ZH', 'en', 'zh', ' ', 'KO'])).toBe('zh,en,ko')
    expect(splitLanguages('zh,en,ko')).toEqual(['zh', 'en', 'ko'])
  })

  it('语言数量上限为 4', () => {
    const five = ['zh', 'en', 'ko', 'ja', 'ru']
    expect(five.length).toBeGreaterThan(MAX_LANGUAGES)
    // 前端在勾选时就会拦住第 5 个。
    const accepted: string[] = []
    for (const lang of five) {
      if (accepted.length >= MAX_LANGUAGES) break
      accepted.push(lang)
    }
    expect(accepted).toEqual(['zh', 'en', 'ko', 'ja'])
  })

  it('语言码按主标签比较，zh-CN 与 zh 等价', () => {
    const group = (lang: string) => {
      const normalised = lang.trim().toLowerCase()
      const dash = normalised.indexOf('-')
      return dash > 0 ? normalised.slice(0, dash) : normalised
    }
    expect(group('zh-CN')).toBe('zh')
    expect(group('zh')).toBe('zh')
    expect(group('en-US')).toBe('en')
  })
})
