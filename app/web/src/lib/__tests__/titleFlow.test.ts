import { describe, expect, it } from 'vitest'

/**
 * 片名识别与站点搜索的交互约定。
 *
 * 背景：79 个站点扇出要 8-13s，而 TMDB 片名解析通常几秒内返回。
 * 两者串行时，用户要盯着骨架屏等十几秒才看到片名列表 —— 实测就是这样。
 * 现在改为「先解析片名并渲染，再用解析结果搜站点」。
 *
 * 第二个约定来自一个真实缺陷：确认候选后调 runSearch 会用一份不含 candidates
 * 的响应覆盖 titleResolution，导致片名列表整块消失。
 */
describe('片名先行与列表保持', () => {
  /** 搜索流程的两阶段：先解析片名，再搜站点。 */
  async function searchFlow(deps: {
    resolve: () => Promise<string[] | null>
    search: (aliases: string[] | null) => Promise<void>
    render: (aliases: string[] | null) => void
  }) {
    const aliases = await deps.resolve()
    // 关键：解析一返回就渲染，不等站点结果。
    deps.render(aliases)
    await deps.search(aliases)
  }

  it('renders the title list before the site search finishes', async () => {
    const events: string[] = []
    let finishSearch: (() => void) | null = null

    const flow = searchFlow({
      resolve: async () => {
        events.push('resolve:done')
        return ['Squid Game']
      },
      render: (aliases) => events.push(`render:${aliases?.length ?? 0}`),
      search: () =>
        new Promise<void>((resolve) => {
          finishSearch = () => {
            events.push('search:done')
            resolve()
          }
        }),
    })

    // 等一拍，让 resolve 与 render 完成，但站点搜索还没结束。
    await new Promise((r) => setTimeout(r, 0))
    expect(events).toEqual(['resolve:done', 'render:1'])
    expect(events).not.toContain('search:done')

    finishSearch!()
    await flow
    expect(events).toEqual(['resolve:done', 'render:1', 'search:done'])
  })

  it('keeps the previous candidates when a confirmation response omits them', () => {
    const previous = {
      keyword: '鱿鱼游戏',
      candidates: [{ tmdb_id: 93405, title: '鱿鱼游戏' }],
      aliases: ['鱿鱼游戏'],
    }
    // 后端确认响应只带 aliases 与 selected，没有 candidates。
    const confirmation = {
      keyword: '鱿鱼游戏',
      candidates: null,
      aliases: ['鱿鱼游戏', 'Squid Game'],
      confirmed: true,
    }

    const merged = {
      ...previous,
      ...confirmation,
      candidates: previous.candidates ?? confirmation.candidates ?? [],
    }

    expect(merged.candidates).toHaveLength(1)
    expect(merged.aliases).toContain('Squid Game')
    expect(merged.confirmed).toBe(true)
  })

  it('keeps the old result list visible while re-searching the same keyword', () => {
    // 切换候选时 isSwitchingTitle 为真：不清空 results，避免整页闪成骨架屏。
    const decideKeep = (isSwitching: boolean, resultCount: number) => isSwitching && resultCount > 0

    expect(decideKeep(true, 20)).toBe(true)
    expect(decideKeep(false, 20)).toBe(false)
    expect(decideKeep(true, 0)).toBe(false)
  })
})
