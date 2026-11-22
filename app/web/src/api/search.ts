import client from '@/api/client'

export interface DefinitionItem {
  id: string
  name: string
  source: 'remote' | 'local'
  version: string
  type: string
  language: string
  compat: 'full' | 'partial' | 'invalid'
  detail: string[] | null
  capabilities: string
  updated_at: string
  needs_login: boolean
  needs_captcha: boolean
  used_count: number
  language_label: string
  category_groups: string[] | null
  mode_summary: string
  site_url: string
}

export interface IndexerStatus {
  last_success_at: string
  consecutive_failures: number
  cooldown_until: string
  cooldown_remaining_ms: number
  avg_elapsed_ms: number
  last_error_kind: string
}

export interface IndexerItem {
  id: number
  name: string
  connector: 'cardigann' | 'torznab' | 'knaben' | 'subsplease'
  definition_id: string
  definition_version: string
  base_url: string
  api_key_mask: string
  secrets_mask: Record<string, string>
  proxy_mask: string
  cookie_names: string[] | null
  cookie_mask: string
  user_agent: string
  settings: string
  categories: string
  min_interval_ms: number
  priority: number
  download_client_id: number
  enabled: boolean
  status?: IndexerStatus
  session_state: string
  definition_status: string
  site_url: string
  /** 该站点要搜索的语言（空表示跟随定义）。 */
  search_languages?: string[] | null
}

export interface CategoryMapping {
  source_category: number
  client_category: string
}

export interface ClientCategory {
  name: string
  save_path: string
}

export interface DownloadClientItem {
  id: number
  name: string
  kind: string
  host: string
  port: number
  use_ssl: boolean
  url_base: string
  api_key_mask: string
  password_mask: string
  username: string
  default_category: string
  initial_state: string
  sequential: boolean
  first_last_first: boolean
  content_layout: string
  priority: number
  is_default: boolean
  enabled: boolean
  category_mappings: CategoryMapping[] | null
  unavailable_remaining_ms: number
}

export interface SearchSource {
  indexer_id: number
  indexer_name: string
  download_url: string
  info_hash: string
  seeders: number
  size_bytes: number
  details_url: string
}

export interface SearchItem {
  fingerprint: string
  title: string
  indexer_id: number
  indexer_name: string
  sources: SearchSource[] | null
  categories: number[] | null
  size_bytes: number
  publish_date: string
  seeders: number
  peers: number
  download_type: string
  download_url: string
  info_hash: string
  details_url: string
  category_labels: string[] | null
}

export interface SearchFailure {
  indexer_id: number
  indexer_name: string
  kind: string
  message: string
}

export interface SiteQuery {
  indexer_id: number
  indexer_name: string
  keyword: string
  language: string
}

/** One title variant with the language it belongs to. */
export interface TitleAliasEntry {
  title: string
  lang: string
  source: 'localized' | 'original' | 'alternative' | 'manual' | string
}

/** One TMDB match offered for manual confirmation. */
export interface TitleCandidate {
  tmdb_id: number
  media_type: 'movie' | 'tv' | string
  title: string
  original_title: string
  year: number
  overview: string
  poster_path: string
  vote_average: number
  aliases: TitleAliasEntry[] | null
}

/** How a keyword was resolved into the titles indexers actually publish. */
export interface TitleResolution {
  keyword: string
  media_type: string
  selected?: TitleCandidate | null
  candidates: TitleCandidate[] | null
  aliases: string[] | null
  needs_confirmation: boolean
  confirmed: boolean
  cached: boolean
  degraded: boolean
  message?: string
}

export interface SearchResponse {
  search_id: string
  request_id: string
  elapsed_ms: number
  total: number
  total_found: number
  truncated: boolean
  failures: SearchFailure[] | null
  items: SearchItem[]
  page: number
  page_size: number
  skipped: number
  /** 每个站点实际使用的检索词。 */
  site_queries?: SiteQuery[] | null
  /** Alias resolution behind this query, when enabled. */
  title?: TitleResolution | null
}

export interface TestResult {
  ok: boolean
  indexer_name: string
  protocol: string
  categories: number
  search_modes: string[] | null
  elapsed_ms: number
  login_state: string
  error_kind?: string
  message?: string
  field?: string
  needs_captcha?: boolean
  categories_ready?: string[] | null
  categories_failed?: string[] | null
  status?: IndexerStatus
  session_state?: string
}

/** 全部测试的逐站点结果，用于就地更新列表而不用整页刷新。 */
export interface BulkTestItem {
  indexer_id: number
  ok: boolean
  message?: string
  error_kind?: string
  elapsed_ms: number
  needs_captcha?: boolean
  status?: IndexerStatus
  session_state?: string
  definition_status?: string
}

export interface TaskItem {
  hash: string
  name: string
  size_bytes: number
  progress: number
  speed_bps: number
  state: string
  eta_seconds: number
  category: string
}

export interface TaskGroup {
  client_id: number
  client_name: string
  state: 'ok' | 'unreachable'
  data_updated_at: string
  tasks: TaskItem[] | null
  message?: string
}

export interface RecordItem {
  id: number
  title: string
  indexer_name: string
  downloader_name: string
  size_bytes: number
  client_category: string
  status: string
  error_kind: string
  created_at: string
}

export interface HistoryItem {
  keyword: string
  use_count: number
  last_used_at: string
}

export interface GrabPreview {
  client_id: number
  client_name: string
  category: string
  category_from: 'mapping' | 'parent' | 'default' | 'none'
  categories: string[] | null
  save_path_hint: string
  warning?: string
}

export interface GrabResult {
  record_id: number
  client_id: number
  client_name: string
  category: string
  info_hash: string
  status: string
  duplicate: boolean
  message?: string
}

const base = '/api/v1/search'

export const searchApi = {
  status: () => client.get(`${base}/status`),

  listDefinitions: () => client.get(`${base}/definitions`),
  syncDefinitions: () => client.post(`${base}/definitions/sync`, {}, { timeout: 180000 }),
  testProxy: () => client.post(`${base}/proxy-test`, {}, { timeout: 20000 }),
  importDefinition: (fileName: string, content: string) =>
    client.post(`${base}/definitions/import`, { file_name: fileName, content }),
  deleteDefinition: (id: string) => client.delete(`${base}/definitions/${encodeURIComponent(id)}`),

  listIndexers: () => client.get(`${base}/indexers`),
  getIndexer: (id: number) => client.get(`${base}/indexers/${id}`),
  createIndexer: (payload: unknown) => client.post(`${base}/indexers`, payload),
  updateIndexer: (id: number, payload: unknown) => client.put(`${base}/indexers/${id}`, payload),
  deleteIndexer: (id: number) => client.delete(`${base}/indexers/${id}`),
  setIndexerEnabled: (id: number, enabled: boolean) => client.put(`${base}/indexers/${id}/enabled`, { enabled }),
  testIndexer: (id: number) => client.post(`${base}/indexers/${id}/test`),
  testAllIndexers: () => client.post(`${base}/indexers/test-all`, {}, { timeout: 180000 }),
  submitCaptcha: (id: number, captcha: string) => client.post(`${base}/indexers/${id}/captcha`, { captcha }),
  refreshDefinition: (id: number) => client.post(`${base}/indexers/${id}/refresh-definition`),
  clearCooldown: (id: number) => client.post(`${base}/indexers/${id}/clear-cooldown`),
  importCookies: (payload: {
    host?: string
    url?: string
    cookies: string
    user_agent: string
    indexer_id?: number
  }) => client.post(`${base}/cookie-import`, payload),

  listClients: () => client.get(`${base}/download-clients`),
  createClient: (payload: unknown) => client.post(`${base}/download-clients`, payload),
  updateClient: (id: number, payload: unknown) => client.put(`${base}/download-clients/${id}`, payload),
  deleteClient: (id: number) => client.delete(`${base}/download-clients/${id}`),
  setClientEnabled: (id: number, enabled: boolean) => client.put(`${base}/download-clients/${id}/enabled`, { enabled }),
  testClient: (id: number) => client.post(`${base}/download-clients/${id}/test`),
  clientCategories: (id: number) => client.get(`${base}/download-clients/${id}/categories`),

  query: (payload: unknown) =>
    // 服务端搜索预算默认 15s，客户端的等待必须比它更长，否则服务端还在收尾
    // （合并、去重、写历史）时前端就先超时，用户看到「搜索失败」但其实有结果。
    client.post(`${base}/query`, payload, { timeout: 25000 }),
  titleCandidates: (payload: { keyword: string; media_type?: string; refresh?: boolean }) =>
    // 解析包含 TMDB 搜索 + 候选别名，冷启动可能接近 4s 预算。
    client.post(`${base}/title-candidates`, payload, { timeout: 20000 }),
  confirmTitle: (payload: { keyword: string; media_type?: string; tmdb_id: number }) =>
    // 确认在缓存未命中时会先解析片名（一次 TMDB 往返），比普通写接口慢。
    client.post(`${base}/title-confirm`, payload, { timeout: 20000 }),
  clearTitleAlias: (keyword: string) =>
    client.delete(`${base}/title-alias`, { params: { keyword } }),
  grabPreview: (payload: unknown) => client.post(`${base}/grab/preview`, payload),
  grab: (payload: unknown) => client.post(`${base}/grab`, payload),
  tasks: () => client.get(`${base}/tasks`),
  taskAction: (clientId: number, hash: string, action: string, deleteFiles = false) =>
    client.post(`${base}/tasks/${clientId}/${hash}/action`, { action, delete_files: deleteFiles }),
  records: () => client.get(`${base}/records`),
  history: () => client.get(`${base}/history`),
  deleteHistory: (keyword?: string) =>
    client.delete(`${base}/history`, { params: keyword ? { keyword } : {} }),
  health: () => client.get(`${base}/health`),
}

/** 站点访问类型的中文名。semi-private 指需要注册但不强制邀请的站点。 */
export function accessLabel(type: string): string {
  switch (type) {
    case 'public':
      return '公开'
    case 'private':
      return '私有（需邀请）'
    case 'semi-private':
      return '半私有（需注册）'
    default:
      return '未知'
  }
}

/** 访问类型筛选的分组：公开 / 非公开（含私有与半私有）。 */
export type AccessGroup = 'all' | 'public' | 'nonPublic'

/** accessGroupOf 把定义的类型归入筛选分组。 */
export function accessGroupOf(type: string): 'public' | 'nonPublic' {
  return type === 'public' ? 'public' : 'nonPublic'
}

/**
 * NEWZNAB 分类的中文名，与后端 connector/category.go 保持一致。
 * 界面用这些名字而不是数字，用户才知道"208"其实代表剧集/HD。
 */
const NEWZNAB_LABELS: Record<number, string> = {
  1000: '主机游戏', 1010: '主机游戏/PS4', 1030: '主机游戏/Switch', 1080: '主机游戏/PS4',
  2000: '电影', 2010: '电影/外语', 2030: '电影/SD', 2040: '电影/HD',
  2045: '电影/4K', 2050: '电影/蓝光', 2060: '电影/3D', 2070: '电影/剧照',
  3000: '音乐', 3010: '音乐/MP3', 3040: '音乐/无损',
  4000: '软件', 4030: '软件/Mac', 4050: '软件/游戏',
  5000: '剧集', 5020: '剧集/外语', 5030: '剧集/SD', 5040: '剧集/HD',
  5045: '剧集/4K', 5060: '剧集/体育', 5070: '剧集/动画', 5080: '剧集/纪录片',
  6000: '成人',
  7000: '书籍', 7020: '书籍/电子书', 7030: '书籍/漫画', 7040: '书籍/杂志',
  8000: '其它',
}

/** newznabLabel 返回分类的中文名，未知分类回退到最近的父类。 */
export function newznabLabel(id: number): string {
  if (NEWZNAB_LABELS[id]) return NEWZNAB_LABELS[id]
  for (let level = 100; level <= 1000; level *= 10) {
    const parent = Math.floor(id / level) * level
    if (NEWZNAB_LABELS[parent]) return NEWZNAB_LABELS[parent]
  }
  return id > 0 ? `其它（${id}）` : '其它'
}

/** 可选的来源分类，按常见用途排序。 */
export const SOURCE_CATEGORY_PRESETS: { value: number; label: string }[] = [
  { value: 2000, label: '电影（含全部子类）' },
  { value: 2040, label: '电影/HD' },
  { value: 2045, label: '电影/4K' },
  { value: 2030, label: '电影/SD' },
  { value: 5000, label: '剧集（含全部子类）' },
  { value: 5040, label: '剧集/HD' },
  { value: 5045, label: '剧集/4K' },
  { value: 5030, label: '剧集/SD' },
  { value: 5070, label: '剧集/动画' },
  { value: 3000, label: '音乐' },
  { value: 7000, label: '书籍' },
  { value: 4000, label: '软件' },
  { value: 1000, label: '主机游戏' },
  { value: 8000, label: '其它' },
]

/** formatBytes renders a byte count the way the rest of the UI does. */
export function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value.toFixed(value >= 100 || unit === 0 ? 0 : 1)} ${units[unit]}`
}

/** formatSpeed renders bytes per second. */
export function formatSpeed(bytesPerSecond: number): string {
  if (!bytesPerSecond || bytesPerSecond <= 0) return '—'
  return `${formatBytes(bytesPerSecond)}/s`
}

/** formatETA renders a remaining-time hint. */
export function formatETA(seconds: number): string {
  if (!seconds || seconds <= 0 || seconds > 864000) return '—'
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (hours > 0) return `${hours} 小时 ${minutes} 分`
  return `${minutes} 分`
}

/** formatTime renders an ISO timestamp in local time. */
export function formatTime(raw: string): string {
  if (!raw) return '—'
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString('zh-CN', { hour12: false })
}
