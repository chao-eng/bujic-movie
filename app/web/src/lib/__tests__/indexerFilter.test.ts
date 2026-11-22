import { describe, expect, it } from 'vitest'
import type { DefinitionItem, IndexerItem } from '@/api/search'
import {
  accessCounts,
  buildIndexerRows,
  categoryOptions,
  compatCounts,
  compatOf,
  filterIndexerRows,
  languageOptions,
  enabledCounts,
} from '../indexerFilter'

/** 造一个最小可用模板。 */
function def(over: Partial<DefinitionItem> & { id: string }): DefinitionItem {
  return {
    id: over.id,
    name: over.name ?? over.id,
    source: 'remote',
    version: '1',
    type: over.type ?? 'public',
    language: 'en-US',
    compat: over.compat ?? 'full',
    detail: null,
    capabilities: '',
    updated_at: '',
    needs_login: false,
    needs_captcha: false,
    used_count: 0,
    language_label: over.language_label ?? '英语',
    category_groups: over.category_groups ?? null,
    mode_summary: '',
    site_url: '',
  }
}

function idx(over: Partial<IndexerItem> & { id: number; name: string }): IndexerItem {
  return {
    id: over.id,
    name: over.name,
    connector: over.connector ?? 'cardigann',
    definition_id: over.definition_id ?? '',
    definition_version: '1',
    base_url: '',
    api_key_mask: '',
    secrets_mask: {},
    proxy_mask: '',
    cookie_names: null,
    cookie_mask: '',
    user_agent: '',
    settings: '',
    categories: '',
    min_interval_ms: 2000,
    priority: 50,
    download_client_id: 0,
    enabled: over.enabled ?? true,
    status: undefined,
    session_state: 'none',
    definition_status: 'ok',
    site_url: '',
  } as IndexerItem
}

const definitions = [
  def({ id: 'pirates', name: 'Pirate Bay', type: 'public', category_groups: ['电影', '剧集'], language_label: '英语' }),
  def({ id: 'nyaa', name: 'Nyaa', type: 'semi-public', category_groups: ['动画'], language_label: '日语' }),
  def({ id: 'broken', name: 'Broken', compat: 'invalid' }),
]

const indexers = [
  idx({ id: 1, name: 'Pirate Bay', definition_id: 'pirates' }),
  idx({ id: 2, name: 'Nyaa', definition_id: 'nyaa' }),
  idx({ id: 3, name: 'Pointless', definition_id: 'broken' }),
  idx({ id: 4, name: 'Torznab', connector: 'torznab' }),
  idx({ id: 5, name: 'Orphan', definition_id: 'gone' }),
]

const rows = buildIndexerRows(indexers, definitions)

describe('站点筛选：关联', () => {
  it('cardigann 站点关联到模板，其他连接器不关联', () => {
    expect(rows[0].definition?.id).toBe('pirates')
    expect(rows[3].definition).toBeUndefined()
  })

  it('模板被删掉的站点标为定义缺失', () => {
    expect(compatOf(rows[4])).toBe('missing')
  })

  it('模板兼容性为 invalid 也按缺失处理', () => {
    expect(compatOf(rows[2])).toBe('missing')
  })
})

describe('站点筛选：过滤', () => {
  it('无条件时全部返回', () => {
    expect(filterIndexerRows(rows, {})).toHaveLength(5)
  })

  it('按名称搜索', () => {
    const got = filterIndexerRows(rows, { term: 'nyaa' })
    expect(got.map((r) => r.name)).toEqual(['Nyaa'])
  })

  it('按模板名搜索', () => {
    const got = filterIndexerRows(rows, { term: 'pointless' })
    expect(got.map((r) => r.name)).toEqual(['Pointless'])
  })

  it('按定义 id 搜索', () => {
    const got = filterIndexerRows(rows, { term: 'pirates' })
    expect(got.map((r) => r.name)).toEqual(['Pirate Bay'])
  })

  it('按分类筛选，只保留引用了该分类模板的站点', () => {
    const got = filterIndexerRows(rows, { category: '剧集' })
    expect(got.map((r) => r.name)).toEqual(['Pirate Bay'])
  })

  it('按语言筛选', () => {
    const got = filterIndexerRows(rows, { language: '日语' })
    expect(got.map((r) => r.name)).toEqual(['Nyaa'])
  })

  it('按访问类型筛选', () => {
    const got = filterIndexerRows(rows, { access: 'nonPublic' })
    expect(got.map((r) => r.name)).toEqual(['Nyaa'])
  })

  it('按兼容性筛选：完全兼容排除定义缺失的站点', () => {
    const got = filterIndexerRows(rows, { compat: 'full' })
    expect(got.map((r) => r.name)).toEqual(['Pirate Bay', 'Nyaa'])
  })

  it('定义缺失可以把有问题的站点单独挑出来', () => {
    const got = filterIndexerRows(rows, { compat: 'missing' })
    expect(got.map((r) => r.name)).toEqual(['Pointless', 'Orphan'])
  })

  it('多个条件同时生效（与关系）', () => {
    const got = filterIndexerRows(rows, { access: 'public', category: '电影' })
    expect(got.map((r) => r.name)).toEqual(['Pirate Bay'])
  })

  it('条件互斥时返回空', () => {
    expect(filterIndexerRows(rows, { language: '日语', category: '电影' })).toEqual([])
  })

  it('搜索不区分大小写并忽略首尾空白', () => {
    expect(filterIndexerRows(rows, { term: '  NYAA  ' }).map((r) => r.name)).toEqual(['Nyaa'])
  })
})

describe('站点筛选：选项计数', () => {
  it('分类选项带数量且降序', () => {
    expect(categoryOptions(rows)).toEqual([
      { label: '电影', count: 1 },
      { label: '剧集', count: 1 },
      { label: '动画', count: 1 },
    ])
  })

  it('语言选项只统计有模板的站点', () => {
    const opts = languageOptions(rows)
    expect(opts.map((o) => o.label).sort()).toEqual(['日语', '英语'])
    // Torznab 与 Orphan 没有模板，不计入
    expect(opts.reduce((sum, o) => sum + o.count, 0)).toBe(2)
  })

  it('访问类型计数跳过没有模板的站点', () => {
    expect(accessCounts(rows)).toEqual({ public: 1, nonPublic: 1 })
  })

  it('兼容性计数包含定义缺失，但排除不使用模板的连接器', () => {
    // Torznab 本来就没有模板，不该算成「定义缺失」——
    // 否则这个筛选结果里会混进永远修不好的条目。
    expect(compatCounts(rows)).toEqual({ full: 2, partial: 0, missing: 2 })
  })

  it('不使用模板的连接器在任何兼容性条件下都不出现', () => {
    for (const compat of ['full', 'partial', 'missing'] as const) {
      expect(filterIndexerRows(rows, { compat }).map((r) => r.name)).not.toContain('Torznab')
    }
  })
})

describe('站点筛选：启用状态', () => {
  // 停用两个站点，用来验证筛选与计数。
  const mixed = buildIndexerRows(
    [
      idx({ id: 1, name: 'On1', definition_id: 'pirates', enabled: true }),
      idx({ id: 2, name: 'Off1', definition_id: 'nyaa', enabled: false }),
      idx({ id: 3, name: 'Off2', connector: 'torznab', enabled: false }),
      idx({ id: 4, name: 'On2', connector: 'torznab', enabled: true }),
    ],
    definitions,
  )

  it('默认不过滤，返回全部站点', () => {
    expect(filterIndexerRows(mixed, {}).map((r) => r.name)).toEqual(['On1', 'Off1', 'Off2', 'On2'])
  })

  it('只筛已启用', () => {
    expect(filterIndexerRows(mixed, { enabled: 'enabled' }).map((r) => r.name)).toEqual(['On1', 'On2'])
  })

  it('只筛已停用', () => {
    expect(filterIndexerRows(mixed, { enabled: 'disabled' }).map((r) => r.name)).toEqual(['Off1', 'Off2'])
  })

  it('启用状态与模板无关，非 cardigann 连接器同样参与', () => {
    // 这是与语言/分类/访问类型筛选的关键差别：那些维度需要模板，
    // 而启停是站点自身的属性。
    const disabled = filterIndexerRows(mixed, { enabled: 'disabled' })
    expect(disabled.map((r) => r.name)).toContain('Off2')
    expect(disabled.find((r) => r.name === 'Off2')?.definition).toBeUndefined()
  })

  it('与其它条件叠加时取交集', () => {
    expect(filterIndexerRows(mixed, { enabled: 'enabled', term: 'on1' }).map((r) => r.name)).toEqual(['On1'])
    expect(filterIndexerRows(mixed, { enabled: 'disabled', term: 'on1' })).toEqual([])
  })

  it('计数覆盖全部站点', () => {
    expect(enabledCounts(mixed)).toEqual({ enabled: 2, disabled: 2 })
    expect(enabledCounts([])).toEqual({ enabled: 0, disabled: 0 })
  })
})
