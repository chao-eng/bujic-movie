<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { toast } from 'vue-sonner'
import {
  ChevronDownIcon,
  CircleCheckIcon,
  FlaskConicalIcon,
  PencilIcon,
  PlusIcon,
  PowerIcon,
  RefreshCwIcon,
  SearchIcon,
  SnowflakeIcon,
  Trash2Icon,
  TriangleAlertIcon,
} from 'lucide-vue-next'
import { useConfirm } from '@/composables/useConfirm'
import {
  buildIndexerRows,
  filterIndexerRows,
  compatCounts as indexerCompatCounts,
  categoryOptions as indexerCategoryOptions,
  languageOptions as indexerLanguageOptions,
  accessCounts as indexerAccessCounts,
  enabledCounts as indexerEnabledCounts,
  type CompatFilter,
} from '@/lib/indexerFilter'
import TemplatePicker from '@/components/search/TemplatePicker.vue'
import SearchModal from '@/components/search/SearchModal.vue'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import {
  searchApi,
  accessLabel,
  type DefinitionItem,
  type IndexerItem,
  type BulkTestItem,
  type DownloadClientItem,
  type TestResult,
  formatTime,
} from '@/api/search'

const indexers = ref<IndexerItem[]>([])
const definitions = ref<DefinitionItem[]>([])
const clients = ref<DownloadClientItem[]>([])
const isLoading = ref(false)
const testing = ref<Record<number, boolean>>({})
const { confirm } = useConfirm()

const showForm = ref(false)
const showPicker = ref(false)
// 本地追加过条目后，提示用户可以手动刷新以拿到完整状态（测试结果等）。
const needsRefresh = ref(false)
const editing = ref<IndexerItem | null>(null)
const form = reactive({
  name: '',
  connector: 'cardigann' as 'cardigann' | 'torznab' | 'knaben' | 'subsplease',
  definition_id: '',
  base_url: '',
  api_key: '',
  categories: '',
  proxy_url: '',
  min_interval_ms: 2000,
  priority: 50,
  download_client_id: 0,
  extra_cookies: '',
  user_agent: '',
  settings: {} as Record<string, string>,
  secrets: {} as Record<string, string>,
  search_languages: [] as string[],
})
const isSaving = ref(false)

// 站点可选的搜索语言：取自已启用站点的语言分布，另加常见语言兜底，
// 这样即使是新站点也能选到尚未出现的语言。
const COMMON_LANGUAGES: { language: string; label: string }[] = [
  { language: 'zh', label: '中文' },
  { language: 'en', label: '英语' },
  { language: 'ko', label: '韩语' },
  { language: 'ja', label: '日语' },
  { language: 'ru', label: '俄语' },
  { language: 'fr', label: '法语' },
  { language: 'de', label: '德语' },
  { language: 'es', label: '西班牙语' },
  { language: 'pt', label: '葡萄牙语' },
  { language: 'it', label: '意大利语' },
  { language: 'th', label: '泰语' },
  { language: 'vi', label: '越南语' },
]

const formLanguageOptions = computed(() => {
  const seen = new Map<string, string>()
  for (const lang of COMMON_LANGUAGES) seen.set(lang.language, lang.label)
  // 已添加站点里出现过的语言也补进来（按中文标签反查语言码）。
  for (const opt of languageOptions.value) {
    const code = codeForLabel(opt.label)
    if (code && !seen.has(code)) seen.set(code, opt.label)
  }
  return [...seen.entries()].map(([language, label]) => ({ language, label }))
})

/** 把语言的中文标签反查成语言码；未知标签则跳过。 */
const codeForLabel = (label: string): string => {
  for (const lang of COMMON_LANGUAGES) {
    if (lang.label === label) return lang.language
  }
  // 后端把 zh-CN / zh-TW 都渲染成中文名，这里按前缀兜底。
  if (label.includes('中文')) return 'zh'
  return ''
}

const formLanguageLabel = computed(() => {
  if (form.search_languages.length === 0) {
    // 显示定义声明的语言，让用户知道默认值是什么。
    const def = availableDefinitions.value.find((d) => d.id === form.definition_id)
    const fallback = def?.language_label || '定义默认'
    return `跟随定义（${fallback}）`
  }
  const labels = form.search_languages.map(
    (lang) => formLanguageOptions.value.find((o) => o.language === lang)?.label ?? lang,
  )
  return labels.join(' + ')
})

const toggleFormLanguage = (language: string) => {
  const at = form.search_languages.indexOf(language)
  if (at >= 0) {
    form.search_languages.splice(at, 1)
    return
  }
  if (form.search_languages.length >= 4) {
    toast.error('搜索语言最多选择 4 种')
    return
  }
  form.search_languages.push(language)
}

const captchaFor = ref<IndexerItem | null>(null)
const captchaValue = ref('')

const availableDefinitions = computed(() => definitions.value.filter((def) => def.compat !== 'invalid'))
const selectedDefinition = computed(() => definitions.value.find((def) => def.id === form.definition_id))

const load = async () => {
  isLoading.value = true
  try {
    const [indexerRes, defRes, clientRes]: any[] = await Promise.all([
      searchApi.listIndexers(),
      searchApi.listDefinitions(),
      searchApi.listClients(),
    ])
    if (indexerRes.code === 0) indexers.value = indexerRes.data.items ?? []
    if (defRes.code === 0) definitions.value = defRes.data.items ?? []
    if (clientRes.code === 0) clients.value = clientRes.data.items ?? []
  } catch {
    toast.error('读取站点失败')
  } finally {
    isLoading.value = false
  }
}

const openPicker = () => {
  showPicker.value = true
  showForm.value = false
}

const openManualCreate = () => {
  showPicker.value = false
  editing.value = null
  Object.assign(form, {
    name: '',
    connector: 'cardigann',
    definition_id: availableDefinitions.value[0]?.id ?? '',
    base_url: '',
    api_key: '',
    categories: '',
    proxy_url: '',
    min_interval_ms: 2000,
    priority: 50,
    download_client_id: 0,
    extra_cookies: '',
    user_agent: '',
    settings: {},
    secrets: {},
    search_languages: [] as string[],
  })
  showForm.value = true
}

const openEdit = (indexer: IndexerItem) => {
  editing.value = indexer
  Object.assign(form, {
    name: indexer.name,
    connector: indexer.connector,
    definition_id: indexer.definition_id,
    base_url: indexer.base_url,
    api_key: '',
    categories: indexer.categories,
    proxy_url: '',
    min_interval_ms: indexer.min_interval_ms,
    priority: indexer.priority,
    download_client_id: indexer.download_client_id,
    extra_cookies: '',
    user_agent: indexer.user_agent || '',
    settings: {},
    secrets: {},
    search_languages: [...(indexer.search_languages ?? [])],
  })
  showForm.value = true
}

const save = async () => {
  if (!form.name.trim()) {
    toast.error('请填写名称')
    return
  }
  if (form.connector === 'torznab' && !form.base_url.trim()) {
    toast.error('请填写接口地址')
    return
  }
  if (form.connector === 'cardigann' && !form.definition_id) {
    toast.error('请选择站点定义')
    return
  }
  if ((form.connector === 'knaben' || form.connector === 'subsplease') && !form.name.trim()) {
    toast.error('请填写名称')
    return
  }
  isSaving.value = true
  try {
    const payload = {
      ...form,
      api_key: form.api_key,
      secrets: Object.keys(form.secrets).length ? form.secrets : undefined,
      settings: Object.keys(form.settings).length ? form.settings : undefined,
    }
    if (editing.value) {
      await searchApi.updateIndexer(editing.value.id, payload)
      toast.success('已保存')
    } else {
      await searchApi.createIndexer(payload)
      toast.success('已添加站点')
    }
    showForm.value = false
    await load()
  } catch (err: any) {
    toast.error(err?.msg || '保存失败')
  } finally {
    isSaving.value = false
  }
}

const testResult = ref<{ id: number; ok: boolean; message: string } | null>(null)
const isTestingAll = ref(false)

/** 就地更新一行，避免测试后整页重新拉取把滚动位置和展开状态刷掉。 */
const patchRow = (
  id: number,
  payload: { status?: IndexerItem['status']; session_state?: string; definition_status?: string },
) => {
  const target = indexers.value.find((item) => item.id === id)
  if (!target) return
  if (payload.status !== undefined) target.status = payload.status
  if (payload.session_state) target.session_state = payload.session_state
  if (payload.definition_status) target.definition_status = payload.definition_status
}

/** 失败响应不带状态，补一次单站点读取（不是整页刷新）。 */
const refreshRow = async (id: number) => {
  try {
    const res: any = await searchApi.getIndexer(id)
    if (res.code === 0 && res.data) patchRow(id, res.data)
  } catch {
    // 状态没读到不影响测试结论，保留旧值。
  }
}

/** 全部测试：一次性拿回每个站点的结果，逐行就地更新。 */
const testAll = async () => {
  if (isTestingAll.value) return
  if (!indexers.value.some((item) => item.id > 0)) {
    toast.info('还没有可测试的站点')
    return
  }
  isTestingAll.value = true
  try {
    const res: any = await searchApi.testAllIndexers()
    if (res.code !== 0) {
      toast.error(res.msg || '全部测试失败')
      return
    }
    const items = (res.data?.items ?? []) as BulkTestItem[]
    for (const item of items) patchRow(item.indexer_id, item)

    const ok = items.filter((item) => item.ok).length
    const skipped = items.filter((item) => !item.ok && (item.message ?? '').includes('已跳过')).length
    const failed = items.length - ok - skipped
    const parts = [`成功 ${ok} 个`]
    if (failed) parts.push(`失败 ${failed} 个`)
    if (skipped) parts.push(`跳过 ${skipped} 个`)
    const message = `全部测试完成：${parts.join('，')}`
    if (failed) toast.warning(message)
    else toast.success(message)
  } catch (err: any) {
    toast.error(err?.msg || '全部测试失败')
  } finally {
    isTestingAll.value = false
  }
}

const test = async (indexer: IndexerItem) => {
  testing.value[indexer.id] = true
  testResult.value = null
  try {
    const res: any = await searchApi.testIndexer(indexer.id)
    const data = res.data as TestResult
    const message = `连接正常：${data.indexer_name || indexer.name}，分类 ${data.categories} 个，耗时 ${data.elapsed_ms}ms`
    testResult.value = { id: indexer.id, ok: true, message }
    patchRow(indexer.id, data)
    toast.success(message)
  } catch (err: any) {
    if (err?.code === 42201) {
      captchaFor.value = indexer
      captchaValue.value = ''
      return
    }
    const message = err?.msg || '测试失败'
    testResult.value = { id: indexer.id, ok: false, message }
    toast.error(message)
    await refreshRow(indexer.id)
  } finally {
    testing.value[indexer.id] = false
  }
}

const submitCaptcha = async () => {
  const indexer = captchaFor.value
  if (!indexer) return
  try {
    await searchApi.submitCaptcha(indexer.id, captchaValue.value)
    toast.success('登录成功')
    captchaFor.value = null
    await load()
  } catch (err: any) {
    toast.error(err?.msg || '验证码校验失败')
  }
}

// 启停只补丁受影响的这一行，不重新拉整张列表。
// 重载会让列表滚动位置、展开状态、筛选条件全部回到初始，用户连点几行就得重新找位置。
const toggleEnabled = async (indexer: IndexerItem) => {
  const next = !indexer.enabled
  // 乐观更新：接口失败时回滚，避免开关抖动。
  const previous = indexer.enabled
  applyEnabledState(indexer.id, next)
  try {
    const res: any = await searchApi.setIndexerEnabled(indexer.id, next)
    // 服务端可能返回完整视图（含重新计算的状态），有就合并进来。
    const updated = res?.data as IndexerItem | undefined
    if (updated && typeof updated === 'object' && 'enabled' in updated) {
      patchIndexer(indexer.id, updated)
    }
  } catch (err: any) {
    applyEnabledState(indexer.id, previous)
    toast.error(err?.msg || '操作失败')
  }
}

/** 就地改一行的启用标记；行对象整体替换，保证 Vue 能感知。 */
const applyEnabledState = (id: number, enabled: boolean) => {
  const at = indexers.value.findIndex((i) => i.id === id)
  if (at < 0) return
  indexers.value.splice(at, 1, { ...indexers.value[at], enabled })
}

/** 合并服务端返回的字段，只覆盖这一行。 */
const patchIndexer = (id: number, patch: Partial<IndexerItem>) => {
  const at = indexers.value.findIndex((i) => i.id === id)
  if (at < 0) return
  indexers.value.splice(at, 1, { ...indexers.value[at], ...patch })
}

const clearCooldown = async (indexer: IndexerItem) => {
  try {
    await searchApi.clearCooldown(indexer.id)
    toast.success('已恢复调用')
    await load()
  } catch (err: any) {
    toast.error(err?.msg || '操作失败')
  }
}

const refreshDefinition = async (indexer: IndexerItem) => {
  try {
    await searchApi.refreshDefinition(indexer.id)
    toast.success('已刷新到最新定义')
    await load()
  } catch (err: any) {
    toast.error(err?.msg || '刷新失败')
  }
}

const remove = async (indexer: IndexerItem) => {
  const ok = await confirm({
    title: '删除站点',
    message: `删除「${indexer.name}」后不再参与搜索，历史下载记录仍可查询。`,
    confirmText: '删除',
  })
  if (!ok) return
  try {
    await searchApi.deleteIndexer(indexer.id)
    toast.success('已删除')
    await load()
  } catch (err: any) {
    toast.error(err?.msg || '删除失败')
  }
}

const cooldownText = (indexer: IndexerItem): string => {
  const remaining = indexer.status?.cooldown_remaining_ms ?? 0
  if (remaining <= 0) return ''
  const minutes = Math.ceil(remaining / 60000)
  return `冷却中，剩余 ${minutes} 分钟`
}

const connectorLabel = (indexer: IndexerItem): string => {
  switch (indexer.connector) {
    case 'cardigann':
      return indexer.definition_id
    case 'torznab':
      return '通用协议'
    case 'knaben':
      return '内置 Knaben'
    case 'subsplease':
      return '内置 SubsPlease'
    default:
      return indexer.connector
  }
}

/**
 * 站点本身不带分类/语言/兼容性，这些在它引用的模板上。
 * 筛选逻辑在 lib/indexerFilter.ts 里，有单测覆盖。
 */
const rows = computed(() => buildIndexerRows(indexers.value, definitions.value))

const filterText = ref('')
const categoryFilter = ref('all')
const languageFilter = ref('all')
const accessFilter = ref('all')
const compatFilter = ref<CompatFilter>('all')
const enabledFilter = ref('all')

const filterState = computed(() => ({
  term: filterText.value,
  compat: compatFilter.value,
  category: categoryFilter.value,
  language: languageFilter.value,
  access: accessFilter.value,
  enabled: enabledFilter.value,
}))

const filtered = computed(() => filterIndexerRows(rows.value, filterState.value))

const categoryOptions = computed(() => indexerCategoryOptions(rows.value))
const languageOptions = computed(() => indexerLanguageOptions(rows.value))
const accessCounts = computed(() => indexerAccessCounts(rows.value))
const compatCounts = computed(() => indexerCompatCounts(rows.value))
const enabledCounts = computed(() => indexerEnabledCounts(rows.value))

const enabledCount = computed(() => indexers.value.filter((i) => i.enabled).length)

/** 下拉触发器文案自己算：模板异步加载，SelectValue 会在挂载时缓存旧的空文案。 */
const categoryLabel = computed(() => {
  if (categoryFilter.value === 'all') return '全部分类'
  const hit = categoryOptions.value.find((opt) => opt.label === categoryFilter.value)
  return hit ? `${hit.label}（${hit.count}）` : '全部分类'
})

const languageLabel = computed(() => {
  if (languageFilter.value === 'all') return '全部语言'
  const hit = languageOptions.value.find((opt) => opt.label === languageFilter.value)
  return hit ? `${hit.label}（${hit.count}）` : '全部语言'
})

const accessFilterLabel = computed(() => {
  if (accessFilter.value === 'public') return `公开（${accessCounts.value.public}）`
  if (accessFilter.value === 'nonPublic') return `非公开（${accessCounts.value.nonPublic}）`
  return '公开与非公开'
})

const compatLabel = computed(() => {
  if (compatFilter.value === 'full') return `完全兼容（${compatCounts.value.full}）`
  if (compatFilter.value === 'partial') return `部分兼容（${compatCounts.value.partial}）`
  if (compatFilter.value === 'missing') return `定义缺失（${compatCounts.value.missing}）`
  return `全部（${rows.value.length}）`
})

const enabledFilterLabel = computed(() => {
  if (enabledFilter.value === 'enabled') return `已启用（${enabledCounts.value.enabled}）`
  if (enabledFilter.value === 'disabled') return `已停用（${enabledCounts.value.disabled}）`
  return '启用状态'
})

const hasFilters = computed(
  () =>
    filterText.value.trim() !== '' ||
    categoryFilter.value !== 'all' ||
    languageFilter.value !== 'all' ||
    accessFilter.value !== 'all' ||
    compatFilter.value !== 'all' ||
    enabledFilter.value !== 'all',
)

const clearFilters = () => {
  filterText.value = ''
  categoryFilter.value = 'all'
  languageFilter.value = 'all'
  accessFilter.value = 'all'
  compatFilter.value = 'all'
  enabledFilter.value = 'all'
}

/** 状态用 Badge 表达，文案与语气分开，不只靠颜色。 *//** 状态用 Badge 表达，文案与语气分开，不只靠颜色。 */
const statusOf = (indexer: IndexerItem) => {
  if (!indexer.enabled) return { label: '已停用', variant: 'outline' as const, tone: '' }
  if (indexer.definition_status === 'missing') return { label: '定义缺失', variant: 'destructive' as const, tone: '' }
  const cooldown = indexer.status?.cooldown_remaining_ms ?? 0
  if (cooldown > 0) return { label: '冷却中', variant: 'secondary' as const, tone: 'text-warning' }
  if (indexer.session_state === 'failed') return { label: '需重新登录', variant: 'destructive' as const, tone: '' }
  if (indexer.session_state === 'logged-in') return { label: '已登录', variant: 'secondary' as const, tone: 'text-success' }
  if ((indexer.status?.consecutive_failures ?? 0) > 0) return { label: '降级', variant: 'secondary' as const, tone: 'text-warning' }
  if (indexer.status?.last_success_at) return { label: '可用', variant: 'secondary' as const, tone: 'text-success' }
  return { label: '未测试', variant: 'outline' as const, tone: '' }
}

/**
 * 模板弹层关闭：把新增的站点就地补进列表，不发请求。
 * 之前这里调用 load() 重新拉取，用户会看到整页刷新、滚动位置丢失。
 */
const onPickerDone = (payload: { count: number; added: string[] }) => {
  showPicker.value = false
  if (!payload.added.length) return
  const known = new Set(indexers.value.map((i) => i.name))
  const appended = payload.added
    .filter((name) => !known.has(name))
    .map((name) => ({ id: -1, name, enabled: true, status: undefined }) as unknown as IndexerItem)
  indexers.value = [...appended, ...indexers.value]
  needsRefresh.value = true
}

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex flex-wrap items-center gap-2">
      <p class="text-muted-foreground text-sm">
        共 {{ indexers.length }} 个站点，启用 {{ enabledCount }} 个
        <template v-if="hasFilters"> · 筛选后 {{ filtered.length }} 个</template>
      </p>
      <div class="ml-auto flex flex-wrap gap-2">
        <Button variant="outline" size="sm" :disabled="isTestingAll" @click="testAll">
          <Spinner v-if="isTestingAll" data-icon="inline-start" />
          <FlaskConicalIcon v-else data-icon="inline-start" />
          {{ isTestingAll ? '测试中…' : '全部测试' }}
        </Button>
        <Button variant="outline" size="sm" :disabled="isLoading" @click="load">
          <Spinner v-if="isLoading" data-icon="inline-start" />
          <RefreshCwIcon v-else data-icon="inline-start" />
          刷新
        </Button>
        <Button size="sm" @click="openPicker">
          <PlusIcon data-icon="inline-start" />
          添加站点
        </Button>
        <Button variant="ghost" size="sm" @click="openManualCreate">手动配置</Button>
      </div>
    </div>

    <FieldGroup class="gap-3">
      <div class="flex flex-wrap items-center gap-2">
        <Input v-model="filterText" placeholder="搜索站点名称" class="h-8 min-w-52 max-w-xs flex-1" />

        <Select v-model="compatFilter">
          <SelectTrigger class="w-36"><SelectValue placeholder="全部">{{ compatLabel }}</SelectValue></SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value="all">全部（{{ rows.length }}）</SelectItem>
              <SelectItem value="full">完全兼容（{{ compatCounts.full }}）</SelectItem>
              <SelectItem value="partial">部分兼容（{{ compatCounts.partial }}）</SelectItem>
              <SelectItem value="missing">定义缺失（{{ compatCounts.missing }}）</SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>

        <Select v-model="categoryFilter">
          <SelectTrigger class="w-32"><SelectValue placeholder="全部分类">{{ categoryLabel }}</SelectValue></SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value="all">全部分类</SelectItem>
              <SelectItem v-for="opt in categoryOptions" :key="opt.label" :value="opt.label">{{ opt.label }}（{{ opt.count }}）</SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>

        <Select v-model="languageFilter">
          <SelectTrigger class="w-36"><SelectValue placeholder="全部语言">{{ languageLabel }}</SelectValue></SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value="all">全部语言</SelectItem>
              <SelectItem v-for="opt in languageOptions" :key="opt.label" :value="opt.label">{{ opt.label }}（{{ opt.count }}）</SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>

        <Select v-model="accessFilter">
          <SelectTrigger class="w-36"><SelectValue placeholder="公开与非公开">{{ accessFilterLabel }}</SelectValue></SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value="all">公开与非公开</SelectItem>
              <SelectItem value="public">公开（{{ accessCounts.public }}）</SelectItem>
              <SelectItem value="nonPublic">非公开（{{ accessCounts.nonPublic }}）</SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>

        <Select v-model="enabledFilter">
          <SelectTrigger class="w-32"><SelectValue placeholder="启用状态">{{ enabledFilterLabel }}</SelectValue></SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value="all">启用状态</SelectItem>
              <SelectItem value="enabled">已启用（{{ enabledCounts.enabled }}）</SelectItem>
              <SelectItem value="disabled">已停用（{{ enabledCounts.disabled }}）</SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>

        <Button v-if="hasFilters" variant="ghost" size="sm" class="ml-auto" @click="clearFilters">
          清除筛选
        </Button>
      </div>
    </FieldGroup>

    <Alert v-if="needsRefresh" class="text-warning">
      <TriangleAlertIcon />
      <AlertTitle>刚添加的站点还没有测试状态</AlertTitle>
      <AlertDescription>点「刷新」可以拿到完整状态。</AlertDescription>
    </Alert>

    <!-- 默认的添加方式：从内置模板里挑一个 -->
    <SearchModal
      v-if="showPicker"
      title="添加站点"
      subtitle="从内置模板里选一个；不需要懂配置"
      wide
      @close="showPicker = false"
    >
      <TemplatePicker @done="onPickerDone" @cancel="showPicker = false" />
    </SearchModal>

    <SearchModal
      v-if="showForm"
      :title="editing ? '编辑站点' : '手动配置站点'"
      subtitle="通用协议或自定义定义时使用；留空的密钥与 Cookie 会保持原值"
      wide
      @close="showForm = false"
    >
      <FieldGroup class="gap-4">
        <div class="grid gap-4 sm:grid-cols-2">
          <Field>
            <FieldLabel for="indexer-name">名称</FieldLabel>
            <Input id="indexer-name" v-model="form.name" placeholder="自定义名称" />
          </Field>

          <Field>
            <FieldLabel for="indexer-connector">类型</FieldLabel>
            <Select v-model="form.connector">
              <SelectTrigger id="indexer-connector" class="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value="cardigann">站点定义（YAML）</SelectItem>
                  <SelectItem value="torznab">通用协议（Torznab/Newznab）</SelectItem>
                  <SelectItem value="knaben">Knaben（内置，元搜索）</SelectItem>
                  <SelectItem value="subsplease">SubsPlease（内置，动画）</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>

          <Field v-if="form.connector === 'cardigann'" class="sm:col-span-2">
            <FieldLabel for="indexer-definition">站点定义</FieldLabel>
            <Select v-model="form.definition_id">
              <SelectTrigger id="indexer-definition" class="w-full"><SelectValue placeholder="选择一个模板" /></SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem v-for="def in availableDefinitions" :key="def.id" :value="def.id">{{ def.name }}{{ def.compat === 'partial' ? '（部分兼容）' : '' }}</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
            <FieldDescription v-if="selectedDefinition?.detail?.length" class="text-warning">
              缺失能力：{{ selectedDefinition.detail.join('、') }}
            </FieldDescription>
          </Field>

          <Field v-if="form.connector === 'torznab'">
            <FieldLabel for="indexer-base-url">接口地址</FieldLabel>
            <Input id="indexer-base-url" v-model="form.base_url" placeholder="https://example.com" />
          </Field>
          <Field v-if="form.connector === 'torznab'">
            <FieldLabel for="indexer-api-key">API Key</FieldLabel>
            <Input id="indexer-api-key" v-model="form.api_key" type="password" placeholder="留空表示不需要" />
          </Field>

          <Field v-if="form.connector === 'knaben' || form.connector === 'subsplease'">
            <FieldLabel for="indexer-official">接口地址（可选，留空用官方地址）</FieldLabel>
            <Input
              id="indexer-official"
              v-model="form.base_url"
              :placeholder="form.connector === 'knaben' ? 'https://api.knaben.org/v1' : 'https://subsplease.org'"
            />
          </Field>

          <Field>
            <FieldLabel for="indexer-proxy">代理（可选）</FieldLabel>
            <Input id="indexer-proxy" v-model="form.proxy_url" placeholder="http://127.0.0.1:1080 或 socks5://…" />
          </Field>
          <Field>
            <FieldLabel for="indexer-priority">优先级（1 最高）</FieldLabel>
            <Input id="indexer-priority" v-model.number="form.priority" type="number" min="1" max="100" />
          </Field>
          <Field>
            <FieldLabel for="indexer-interval">请求间隔（毫秒）</FieldLabel>
            <Input id="indexer-interval" v-model.number="form.min_interval_ms" type="number" min="200" max="60000" />
          </Field>

          <Field class="md:col-span-2">
            <FieldLabel>搜索语言（可多选）</FieldLabel>
            <FieldDescription>
              留空则跟随定义声明的语言。选中多种语言时，会用每种语言各搜一次并合并结果——
              适合像 FileMood 这种同时收录中文和英文资源的站点。最多 4 种。
            </FieldDescription>
            <DropdownMenu>
              <DropdownMenuTrigger as-child>
                <Button variant="outline" class="w-full justify-between font-normal">
                  <span class="truncate">{{ formLanguageLabel }}</span>
                  <ChevronDownIcon class="text-muted-foreground size-4 shrink-0" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" class="max-h-72 w-56 overflow-y-auto">
                <DropdownMenuCheckboxItem
                  :model-value="form.search_languages.length === 0"
                  @update:model-value="form.search_languages = []"
                >
                  跟随定义
                </DropdownMenuCheckboxItem>
                <DropdownMenuSeparator v-if="formLanguageOptions.length" />
                <DropdownMenuCheckboxItem
                  v-for="opt in formLanguageOptions"
                  :key="opt.language"
                  :model-value="form.search_languages.includes(opt.language)"
                  @select.prevent
                  @update:model-value="toggleFormLanguage(opt.language)"
                >
                  <span class="flex-1">{{ opt.label }}</span>
                </DropdownMenuCheckboxItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </Field>
          <Field>
            <FieldLabel for="indexer-client">绑定下载器</FieldLabel>
            <Select v-model.number="form.download_client_id">
              <SelectTrigger id="indexer-client" class="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem :value="0">不指定（按优先级选择）</SelectItem>
                  <SelectItem v-for="client in clients" :key="client.id" :value="client.id">{{ client.name }}</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>

          <template v-if="form.connector === 'cardigann'">
            <Field>
              <FieldLabel for="indexer-username">登录账号</FieldLabel>
              <Input
                id="indexer-username"
                v-model="form.secrets.username"
                :placeholder="editing?.secrets_mask?.username || '按定义要求填写'"
              />
            </Field>
            <Field>
              <FieldLabel for="indexer-password">登录密码</FieldLabel>
              <Input
                id="indexer-password"
                v-model="form.secrets.password"
                type="password"
                :placeholder="editing?.secrets_mask?.password ? '已保存，留空表示不修改' : '按定义要求填写'"
              />
            </Field>
          </template>
        </div>

        <Field class="border-border gap-3 rounded-lg border p-3">
          <div>
            <FieldLabel>站点要求浏览器验证时（Cloudflare 挑战）</FieldLabel>
            <FieldDescription>
              在浏览器打开该站点并完成验证，然后从开发者工具复制 Cookie（至少包含 <code>cf_clearance</code>）和同一浏览器的
              User-Agent。服务端与浏览器必须使用同一个出口 IP，否则 Cookie 无效。
            </FieldDescription>
          </div>
          <Field>
            <FieldLabel for="indexer-cookies">Cookie</FieldLabel>
            <Textarea
              id="indexer-cookies"
              v-model="form.extra_cookies"
              rows="3"
              :placeholder="editing?.cookie_mask || 'name=value; name2=value2'"
              class="font-mono text-xs"
            />
            <FieldDescription v-if="editing?.cookie_mask" class="text-success">{{ editing.cookie_mask }}</FieldDescription>
          </Field>
          <Field>
            <FieldLabel for="indexer-agent">User-Agent</FieldLabel>
            <Input id="indexer-agent" v-model="form.user_agent" placeholder="与完成验证的浏览器一致" />
          </Field>
        </Field>

        <FieldDescription>
          留空的密码、密钥、Cookie 都会保持原值，不会被清掉；如需彻底清除，请在字段里填 __clear__。
        </FieldDescription>

        <div class="flex justify-end gap-2">
          <Button variant="ghost" @click="showForm = false">取消</Button>
          <Button :disabled="isSaving" @click="save">
            <Spinner v-if="isSaving" data-icon="inline-start" />
            保存
          </Button>
        </div>
      </FieldGroup>
    </SearchModal>

    <Alert v-if="testResult" :class="testResult.ok ? 'text-success' : 'text-destructive'">
      <CircleCheckIcon v-if="testResult.ok" />
      <TriangleAlertIcon v-else />
      <AlertTitle>{{ testResult.ok ? '连接正常' : '测试失败' }}</AlertTitle>
      <AlertDescription>{{ testResult.message }}</AlertDescription>
    </Alert>

    <div v-if="isLoading" class="border-border flex items-center justify-center rounded-lg border px-4 py-10">
      <Spinner class="text-muted-foreground" />
    </div>

    <Empty v-else-if="!indexers.length">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <PlusIcon />
        </EmptyMedia>
        <EmptyTitle>还没有添加站点</EmptyTitle>
        <EmptyDescription>
          站点就是资源搜索的来源。点「添加站点」，从内置模板里挑一个（例如 The Pirate Bay、Nyaa），填个名字就行。
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button @click="openPicker">
          <PlusIcon data-icon="inline-start" />
          添加第一个站点
        </Button>
      </EmptyContent>
    </Empty>

    <Empty v-else-if="!filtered.length">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SearchIcon />
        </EmptyMedia>
        <EmptyTitle>没有符合当前筛选的站点</EmptyTitle>
        <EmptyDescription>试着放宽条件，或清除筛选查看全部。</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" size="sm" @click="clearFilters">清除筛选</Button>
      </EmptyContent>
    </Empty>

    <div v-else class="border-border divide-border divide-y overflow-hidden rounded-lg border">
      <div
        v-for="indexer in filtered"
        :key="indexer.id"
        class="hover:bg-muted/50 flex flex-wrap items-start justify-between gap-3 p-3 transition-colors"
      >
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <a
              v-if="indexer.site_url"
              :href="indexer.site_url"
              target="_blank"
              rel="noopener noreferrer"
              class="truncate text-sm hover:text-primary hover:underline"
              :title="'打开站点主页：' + indexer.site_url"
            >{{ indexer.name }}</a>
            <span v-else class="truncate text-sm">{{ indexer.name }}</span>
            <Badge :variant="statusOf(indexer).variant" :class="statusOf(indexer).tone">
              {{ statusOf(indexer).label }}
            </Badge>
            <Badge variant="outline">{{ connectorLabel(indexer) }}</Badge>
            <!-- 模板属性：筛选就是按这些字段走的，显示出来用户才能理解结果。 -->
            <Badge v-if="indexer.definition" variant="outline">{{ accessLabel(indexer.definition.type) }}</Badge>
            <Badge v-if="indexer.definition?.language_label" variant="outline" class="text-info">
              {{ indexer.definition.language_label }}
            </Badge>
            <!-- 多语言搜索是站点级覆盖，和模板声明的语言是两件事，单独标出。 -->
            <Badge
              v-if="(indexer.search_languages?.length ?? 0) > 1"
              variant="secondary"
              class="text-primary"
              :title="'按这些语言各搜一次：' + (indexer.search_languages ?? []).join(' + ')"
            >
              多语言 {{ (indexer.search_languages ?? []).length }}
            </Badge>
            <Badge
              v-for="group in (indexer.definition?.category_groups ?? []).slice(0, 4)"
              :key="group"
              variant="outline"
            >{{ group }}</Badge>
          </div>
          <div class="text-muted-foreground mt-1 flex flex-wrap gap-x-3 text-[11px]">
            <span v-if="indexer.status?.last_success_at" class="text-success">可用</span>
            <span>优先级 {{ indexer.priority }}</span>
            <span>间隔 {{ indexer.min_interval_ms }}ms</span>
            <span v-if="indexer.status?.last_success_at">最近成功 {{ formatTime(indexer.status.last_success_at) }}</span>
            <span v-if="indexer.status?.avg_elapsed_ms">平均 {{ indexer.status.avg_elapsed_ms }}ms</span>
            <span v-if="(indexer.status?.consecutive_failures ?? 0) > 0" class="text-warning">
              连续失败 {{ indexer.status?.consecutive_failures }}
            </span>
            <span v-if="cooldownText(indexer)" class="text-warning">{{ cooldownText(indexer) }}</span>
          </div>
        </div>
        <div class="flex shrink-0 flex-wrap items-center gap-1">
          <Button variant="ghost" size="xs" :disabled="testing[indexer.id]" @click="test(indexer)">
            <Spinner v-if="testing[indexer.id]" data-icon="inline-start" />
            <FlaskConicalIcon v-else data-icon="inline-start" />
            测试
          </Button>
          <Button
            v-if="indexer.connector === 'cardigann'"
            variant="ghost"
            size="xs"
            @click="refreshDefinition(indexer)"
          >
            <RefreshCwIcon data-icon="inline-start" />
            刷新定义
          </Button>
          <Button
            v-if="(indexer.status?.cooldown_remaining_ms ?? 0) > 0"
            variant="ghost"
            size="xs"
            @click="clearCooldown(indexer)"
          >
            <SnowflakeIcon data-icon="inline-start" />
            恢复
          </Button>
          <Button variant="ghost" size="xs" @click="toggleEnabled(indexer)">
            <PowerIcon data-icon="inline-start" />
            {{ indexer.enabled ? '停用' : '启用' }}
          </Button>
          <Button variant="ghost" size="icon-xs" aria-label="编辑" @click="openEdit(indexer)">
            <PencilIcon />
          </Button>
          <Button variant="ghost" size="icon-xs" aria-label="删除" @click="remove(indexer)">
            <Trash2Icon />
          </Button>
        </div>
      </div>
    </div>

    <Dialog :open="Boolean(captchaFor)" @update:open="(value) => { if (!value) captchaFor = null }">
      <DialogContent class="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>需要验证码</DialogTitle>
          <DialogDescription>{{ captchaFor?.name }} 要求输入验证码才能登录</DialogDescription>
        </DialogHeader>
        <Input v-model="captchaValue" placeholder="请输入验证码" />
        <DialogFooter>
          <Button variant="ghost" @click="captchaFor = null">取消</Button>
          <Button @click="submitCaptcha">提交</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
