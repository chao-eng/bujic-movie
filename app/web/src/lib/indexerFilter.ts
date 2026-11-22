import { accessGroupOf, type DefinitionItem, type IndexerItem } from '@/api/search'

/**
 * 站点列表的筛选逻辑。
 *
 * 站点本身只有名称、连接器、状态；分类、语言、访问类型、兼容性都在它引用的
 * 模板上。所以先把两者关联成「行」，再用与「站点列表」页一致的四个维度筛选。
 *
 * 抽成纯函数以便单测——这类过滤条件一旦写错，界面只会表现为「筛选后少了几条」，
 * 很难在浏览器里肉眼发现。
 */

/** 列表行 = 站点 + 它引用的模板。 */
export type IndexerRow = IndexerItem & { definition?: DefinitionItem }

export type CompatFilter = 'all' | 'full' | 'partial' | 'missing'

/** 启用状态筛选。 */
export type EnabledFilter = 'all' | 'enabled' | 'disabled'

export interface IndexerFilterState {
  /** 按名称 / 定义 id / 模板名搜索，不区分大小写 */
  term?: string
  compat?: CompatFilter
  /** Newznab 分类组标签 */
  category?: string
  language?: string
  /** public / nonPublic */
  access?: string
  /** enabled / disabled */
  enabled?: string
}

/** 把站点与其模板关联起来。 */
export function buildIndexerRows(
  indexers: IndexerItem[],
  definitions: DefinitionItem[],
): IndexerRow[] {
  const byId = new Map<string, DefinitionItem>()
  for (const def of definitions) byId.set(def.id, def)
  return indexers.map((item) => ({
    ...item,
    definition: item.connector === 'cardigann' ? byId.get(item.definition_id) : undefined,
  }))
}

/**
 * 站点的兼容性口径。
 *
 * 与模板页的 `compat` 字段不同，这里多一个「missing」：站点引用的模板被删掉了。
 * 那是站点侧的问题（接口上体现为 `definition_status === 'missing'`），
 * 光看模板字段看不出来。
 *
 * 不使用模板的连接器（Torznab、Knaben、SubsPlease）返回 null：它们本来就没有
 * 兼容性可言，算成「定义缺失」会把真正要处理的问题淹没掉。
 */
export function compatOf(row: IndexerRow): 'full' | 'partial' | 'missing' | null {
  if (row.connector !== 'cardigann') return null
  if (!row.definition) return 'missing'
  if (row.definition.compat === 'invalid') return 'missing'
  return row.definition.compat
}

/**
 * 可以用于筛选的模板。
 *
 * 与「站点列表」页一致：兼容性为 invalid 的模板对站点没有任何用处，所以分类、
 * 语言、访问类型这些维度上也不该出现它的站点。否则用户按「电影」筛出来的
 * 列表里会混进跑不起来的站点。
 */
function usableDefinition(row: IndexerRow): DefinitionItem | undefined {
  const def = row.definition
  if (!def || def.compat === 'invalid') return undefined
  return def
}

/** 按条件过滤，条件为空即不过滤。 */
export function filterIndexerRows(rows: IndexerRow[], state: IndexerFilterState): IndexerRow[] {
  const term = (state.term ?? '').trim().toLowerCase()
  const compat = state.compat ?? 'all'
  const category = state.category ?? 'all'
  const language = state.language ?? 'all'
  const access = state.access ?? 'all'
  const enabled = state.enabled ?? 'all'

  return rows.filter((row) => {
    const def = usableDefinition(row)

    // 启用状态是站点自身的属性，不依赖模板，所以放在最前面判断。
    if (enabled === 'enabled' && !row.enabled) return false
    if (enabled === 'disabled' && row.enabled) return false

    if (compat !== 'all') {
      // 不使用模板的连接器没有兼容性可言，任何具体取值下都不应出现。
      const value = compatOf(row)
      if (value === null || value !== compat) return false
    }

    // 访问类型与分类/语言都依赖模板：没有模板就无从判断，直接排除。
    if (access !== 'all') {
      if (!def || accessGroupOf(def.type) !== access) return false
    }
    if (category !== 'all') {
      if (!(def?.category_groups ?? []).includes(category)) return false
    }
    if (language !== 'all') {
      if (!def) return false
      if ((def.language_label || '未知') !== language) return false
    }

    if (!term) return true
    return (
      row.name.toLowerCase().includes(term) ||
      (row.definition_id ?? '').toLowerCase().includes(term) ||
      (def?.name ?? '').toLowerCase().includes(term)
    )
  })
}

export interface CountOption {
  label: string
  count: number
}

/** 分类选项：统计口径与模板页一致，只算能关联到模板（且分类集非空）的站点。 */
export function categoryOptions(rows: IndexerRow[]): CountOption[] {
  const counts = new Map<string, number>()
  for (const row of rows) {
    for (const group of usableDefinition(row)?.category_groups ?? []) {
      counts.set(group, (counts.get(group) ?? 0) + 1)
    }
  }
  return sortCounts(counts)
}

/** 语言选项。 */
export function languageOptions(rows: IndexerRow[]): CountOption[] {
  const counts = new Map<string, number>()
  for (const row of rows) {
    const def = usableDefinition(row)
    if (!def) continue
    const label = def.language_label || '未知'
    counts.set(label, (counts.get(label) ?? 0) + 1)
  }
  return sortCounts(counts)
}

/** 访问类型计数。 */
export function accessCounts(rows: IndexerRow[]): { public: number; nonPublic: number } {
  let publicCount = 0
  let nonPublicCount = 0
  for (const row of rows) {
    const def = usableDefinition(row)
    if (!def) continue
    if (accessGroupOf(def.type) === 'public') publicCount++
    else nonPublicCount++
  }
  return { public: publicCount, nonPublic: nonPublicCount }
}

/** 启用状态计数：全部站点都参与，与模板无关。 */
export function enabledCounts(rows: IndexerRow[]): { enabled: number; disabled: number } {
  let enabled = 0
  let disabled = 0
  for (const row of rows) {
    if (row.enabled) enabled++
    else disabled++
  }
  return { enabled, disabled }
}

/** 兼容性计数，含「定义缺失」。 */
export function compatCounts(rows: IndexerRow[]): { full: number; partial: number; missing: number } {
  const out = { full: 0, partial: 0, missing: 0 }
  for (const row of rows) {
    const compat = compatOf(row)
    // 不使用模板的连接器不参与兼容性统计。
    if (compat !== null) out[compat]++
  }
  return out
}

function sortCounts(counts: Map<string, number>): CountOption[] {
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([label, count]) => ({ label, count }))
}
