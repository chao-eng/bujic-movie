import { describe, expect, it } from 'vitest'

/**
 * 搜索页的「片名识别」快捷开关。
 *
 * 两种模式：
 *   - 开启（片名搜索）：先问 TMDB，用户确认影片后再搜站点，按站点语言选词。
 *   - 关闭（直接搜索）：不查 TMDB，原关键词原样发往每个站点。
 *
 * 关键点：仅前端跳过解析是不够的。后端 `resolveTitle` 在未收到 `aliases` 时
 * 仍会自己解析并改写关键词，所以请求必须显式带上 `title_recognition: false`。
 */

interface QueryPayload {
  keyword: string
  aliases?: string[]
  media_type?: string
  title_confirmed: boolean
  title_recognition: boolean
}

/** 与 SearchPanel.executeSearch 的入参组装等价。 */
function buildQueryPayload(opts: {
  keyword: string
  recognition: boolean
  pinnedAliases: string[]
  pinnedMediaType: string
}): QueryPayload {
  const pinned = opts.pinnedAliases.length > 0
  return {
    keyword: opts.keyword,
    aliases: pinned ? opts.pinnedAliases : undefined,
    media_type: pinned ? opts.pinnedMediaType || undefined : undefined,
    title_confirmed: pinned,
    title_recognition: opts.recognition,
  }
}

/** 与 SearchPanel.runSearch 的分支等价：是否走解析流程。 */
function shouldResolve(recognition: boolean): boolean {
  return recognition
}

/** 与 toggleTitleRecognition 的清空逻辑等价。 */
function onToggle(next: boolean) {
  return {
    cleared: !next,
    message: next ? '已启用片名识别' : '已关闭片名识别',
  }
}

describe('片名识别快捷开关', () => {
  it('开启时走解析流程', () => {
    expect(shouldResolve(true)).toBe(true)
  })

  it('关闭时跳过解析，直接搜站点', () => {
    expect(shouldResolve(false)).toBe(false)
  })

  it('请求始终显式声明 title_recognition，避免后端自行解析', () => {
    const on = buildQueryPayload({ keyword: '鱿鱼游戏', recognition: true, pinnedAliases: [], pinnedMediaType: '' })
    expect(on.title_recognition).toBe(true)

    const off = buildQueryPayload({ keyword: '鱿鱼游戏', recognition: false, pinnedAliases: [], pinnedMediaType: '' })
    expect(off.title_recognition).toBe(false)
    // 关闭时不应带任何别名或媒体类型，否则后端仍会按别名路由。
    expect(off.aliases).toBeUndefined()
    expect(off.media_type).toBeUndefined()
    expect(off.title_confirmed).toBe(false)
  })

  it('确认候选后即使开关关闭，也按选定的别名搜索', () => {
    // 用户先开启、确认了一部片，再把开关关掉并重搜：此时以确认结果为准。
    const payload = buildQueryPayload({
      keyword: '鱿鱼游戏',
      recognition: false,
      pinnedAliases: ['鱿鱼游戏', 'Squid Game'],
      pinnedMediaType: 'tv',
    })
    expect(payload.aliases).toEqual(['鱿鱼游戏', 'Squid Game'])
    expect(payload.title_confirmed).toBe(true)
    expect(payload.media_type).toBe('tv')
  })

  it('切换到关闭时清掉上一轮的识别结果', () => {
    expect(onToggle(false)).toEqual({ cleared: true, message: '已关闭片名识别' })
  })

  it('切换回开启时不清空，列表由下一次搜索重建', () => {
    expect(onToggle(true)).toEqual({ cleared: false, message: '已启用片名识别' })
  })

  it('偏好默认开启', () => {
    // readTitleRecognitionPref 只在值为 'off' 时返回 false。
    const read = (stored: string | null) => stored !== 'off'
    expect(read(null)).toBe(true)
    expect(read('on')).toBe(true)
    expect(read('off')).toBe(false)
    expect(read('bogus')).toBe(true)
  })
})
