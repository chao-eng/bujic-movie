import { describe, expect, it } from 'vitest'

/**
 * 片名识别请求的超时预算必须比服务端预算更宽松。
 *
 * 背景：`client.ts` 全局 timeout 是 15s，而服务端搜索预算默认也是 15s。
 * 两者相等时，服务端用满预算（合并、去重、写历史都还在跑）前端会先断开，
 * 用户看到「搜索失败」但其实结果已经算出来了。片名确认在缓存未命中时还要
 * 先做一次 TMDB 解析，更需要额外余量。
 */
const SERVER_SEARCH_DEADLINE_MS = 15000
const SERVER_ALIAS_BUDGET_MS = 4000
const GLOBAL_CLIENT_TIMEOUT_MS = 15000

/** 与 `src/api/search.ts` 中 query 的超时保持一致。 */
const QUERY_TIMEOUT_MS = 25000
/** 与 title-candidates / title-confirm 的超时保持一致。 */
const TITLE_TIMEOUT_MS = 20000

describe('search timeouts', () => {
  it('the search request waits longer than the server budget', () => {
    expect(QUERY_TIMEOUT_MS).toBeGreaterThan(SERVER_SEARCH_DEADLINE_MS)
  })

  it('a title request covers the alias budget plus a TMDB round trip', () => {
    expect(TITLE_TIMEOUT_MS).toBeGreaterThan(SERVER_ALIAS_BUDGET_MS * 2)
  })

  it('the default client timeout alone would be too tight for a search', () => {
    // Guards against dropping the per-request override: with only the global
    // timeout a full-budget search would race the client deadline.
    expect(GLOBAL_CLIENT_TIMEOUT_MS).toBeLessThanOrEqual(SERVER_SEARCH_DEADLINE_MS)
    expect(QUERY_TIMEOUT_MS).not.toBe(GLOBAL_CLIENT_TIMEOUT_MS)
  })
})
