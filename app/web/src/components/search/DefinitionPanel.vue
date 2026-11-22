<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { toast } from 'vue-sonner'
import {
  CheckCircle2Icon,
  ChevronDownIcon,
  ChevronUpIcon,
  CircleAlertIcon,
  CircleXIcon,
  ListPlusIcon,
  PlusIcon,
  RefreshCwIcon,
  Trash2Icon,
  UploadIcon,
} from 'lucide-vue-next'
import { useConfirm } from '@/composables/useConfirm'
import TemplatePicker from '@/components/search/TemplatePicker.vue'
import SearchModal from '@/components/search/SearchModal.vue'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import {
  searchApi,
  accessLabel,
  accessGroupOf,
  type AccessGroup,
  type DefinitionItem,
  type IndexerItem,
  formatTime,
} from '@/api/search'

const definitions = ref<DefinitionItem[]>([])
const indexers = ref<IndexerItem[]>([])
const picking = ref<DefinitionItem | null>(null)
const isLoading = ref(false)
const isSyncing = ref(false)
const isImporting = ref(false)
const expanded = ref<Record<string, boolean>>({})
const filterText = ref('')
const compatFilter = ref<'all' | 'full' | 'partial' | 'invalid'>('all')
const categoryFilter = ref('all')
const languageFilter = ref('all')
const accessFilter = ref<AccessGroup>('all')
const showImport = ref(false)
const isBulkAdding = ref(false)
const bulkProgress = ref({ done: 0, total: 0 })
const justAdded = ref<string[]>([])
const importName = ref('')
const importContent = ref('')
const { confirm } = useConfirm()

const categoryOptions = computed(() => {
  const counts = new Map<string, number>()
  for (const def of definitions.value) {
    for (const group of def.category_groups ?? []) {
      counts.set(group, (counts.get(group) ?? 0) + 1)
    }
  }
  return [...counts.entries()].sort((a, b) => b[1] - a[1]).map(([label, count]) => ({ label, count }))
})

const languageOptions = computed(() => {
  const counts = new Map<string, number>()
  for (const def of definitions.value) {
    const label = def.language_label || '未知'
    counts.set(label, (counts.get(label) ?? 0) + 1)
  }
  return [...counts.entries()].sort((a, b) => b[1] - a[1]).map(([label, count]) => ({ label, count }))
})

const filtered = computed(() => {
  const term = filterText.value.trim().toLowerCase()
  return definitions.value.filter((def) => {
    if (compatFilter.value !== 'all' && def.compat !== compatFilter.value) return false
    if (categoryFilter.value !== 'all' && !(def.category_groups ?? []).includes(categoryFilter.value)) return false
    if (languageFilter.value !== 'all' && (def.language_label || '未知') !== languageFilter.value) return false
    if (accessFilter.value !== 'all' && accessGroupOf(def.type) !== accessFilter.value) return false
    if (!term) return true
    return def.name.toLowerCase().includes(term) || def.id.toLowerCase().includes(term)
  })
})

const accessCounts = computed(() => {
  let pub = 0
  let nonPub = 0
  for (const def of definitions.value) {
    if (def.compat === 'invalid') continue
    if (accessGroupOf(def.type) === 'public') pub++
    else nonPub++
  }
  return { public: pub, nonPublic: nonPub }
})

const counts = computed(() => {
  const out = { full: 0, partial: 0, invalid: 0 }
  for (const def of definitions.value) {
    if (def.compat === 'full') out.full++
    else if (def.compat === 'partial') out.partial++
    else out.invalid++
  }
  return out
})

/**
 * 下拉选项里带实时数量。reka 的 SelectValue 在挂载时缓存选项文案，
 * 模板又是异步加载的，所以触发器文案自己算，避免先显示「全部（0）」。
 */
const compatLabel = computed(() => {
  if (compatFilter.value === 'full') return `完全兼容（${counts.value.full}）`
  if (compatFilter.value === 'partial') return `部分兼容（${counts.value.partial}）`
  if (compatFilter.value === 'invalid') return `不可用（${counts.value.invalid}）`
  return `全部（${definitions.value.length}）`
})

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

const load = async () => {
  isLoading.value = true
  try {
    const [defRes, idxRes]: any[] = await Promise.all([
      searchApi.listDefinitions(),
      searchApi.listIndexers(),
    ])
    if (defRes.code === 0) definitions.value = defRes.data.items ?? []
    if (idxRes.code === 0) indexers.value = idxRes.data.items ?? []
  } catch {
    toast.error('读取定义库失败')
  } finally {
    isLoading.value = false
  }
}

const sync = async () => {
  isSyncing.value = true
  try {
    const res: any = await searchApi.syncDefinitions()
    const summary = res.data ?? {}
    toast.success(
      `同步完成：新增 ${summary.added ?? 0}，更新 ${summary.updated ?? 0}，无效 ${summary.invalid ?? 0}`,
    )
    await load()
  } catch (err: any) {
    toast.error(err?.msg || '同步失败，已保留本地定义')
  } finally {
    isSyncing.value = false
  }
}

const importYaml = async () => {
  const content = importContent.value.trim()
  if (!content) {
    toast.error('请粘贴 YAML 内容')
    return
  }
  isImporting.value = true
  try {
    const res: any = await searchApi.importDefinition(importName.value.trim() || 'imported.yml', content)
    const summary = res.data ?? {}
    toast.success(`导入成功：新增 ${summary.added ?? 0}，更新 ${summary.updated ?? 0}`)
    showImport.value = false
    importName.value = ''
    importContent.value = ''
    await load()
  } catch (err: any) {
    toast.error(err?.msg || '导入失败')
  } finally {
    isImporting.value = false
  }
}

const pickFile = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  importName.value = file.name
  importContent.value = await file.text()
  input.value = ''
}

const remove = async (def: DefinitionItem) => {
  const ok = await confirm({
    title: '删除定义',
    message: `删除「${def.name}」后，使用它的站点将进入只读状态（不参与搜索）。已保存的配置不会被删除。`,
    confirmText: '删除',
  })
  if (!ok) return
  try {
    await searchApi.deleteDefinition(def.id)
    toast.success('已删除')
    await load()
  } catch (err: any) {
    toast.error(err?.msg || '删除失败')
  }
}

const usedNames = computed(() => new Set(indexers.value.map((i) => i.name)))

/** 当前筛选下还没添加过的模板。 */
const pending = computed(() => filtered.value.filter((d) => !usedNames.value.has(d.name)))

/**
 * 全部添加：把当前筛选结果里尚未添加的模板逐个建成站点。
 * 串行执行，失败通常只是名称重复，跳过继续。
 */
const addAll = async () => {
  const targets = pending.value
  if (!targets.length) {
    toast.info('当前筛选下没有待添加的模板')
    return
  }
  const ok = await confirm({
    title: '全部添加',
    message: `将为当前筛选下的 ${targets.length} 个模板各创建一个站点，名称取模板名。需要账号的站点建议之后单独配置。`,
    confirmText: '全部添加',
  })
  if (!ok) return

  isBulkAdding.value = true
  bulkProgress.value = { done: 0, total: targets.length }
  let success = 0
  let failed = 0
  let firstError = ''
  const created: string[] = []

  for (const def of targets) {
    try {
      await searchApi.createIndexer({ name: def.name, connector: 'cardigann', definition_id: def.id })
      created.push(def.name)
      success++
    } catch (err: any) {
      failed++
      if (!firstError) firstError = err?.msg || `「${def.name}」添加失败`
    }
    bulkProgress.value = { done: bulkProgress.value.done + 1, total: targets.length }
  }

  isBulkAdding.value = false
  if (created.length) {
    indexers.value = [
      ...created.map((name) => ({ id: -1, name }) as unknown as IndexerItem),
      ...indexers.value,
    ]
    justAdded.value = [...justAdded.value, ...created]
  }
  if (success) toast.success(`批量添加完成：成功 ${success} 个${failed ? `，失败 ${failed} 个` : ''}`)
  if (failed && firstError) toast.error(`${failed} 个未添加成功：${firstError}`)
}

const openPicker = (def: DefinitionItem) => {
  picking.value = def
}

/** 关闭模板弹层：只把新增站点补进本地列表，不重新拉取模板。 */
const onPickerDone = (payload: { count: number; added: string[] }) => {
  picking.value = null
  const known = new Set(indexers.value.map((i) => i.name))
  const appended = payload.added
    .filter((name) => !known.has(name))
    .map((name) => ({ id: -1, name }) as unknown as IndexerItem)
  if (appended.length) indexers.value = [...indexers.value, ...appended]
}

/** 兼容性用 Badge 表达：文案与语气分开，不只靠颜色。 */
const compatOf = (compat: string) => {
  if (compat === 'full') return { label: '完全兼容', variant: 'secondary' as const, tone: 'text-success', icon: CheckCircle2Icon }
  if (compat === 'partial') return { label: '部分兼容', variant: 'secondary' as const, tone: 'text-warning', icon: CircleAlertIcon }
  return { label: '不可用', variant: 'destructive' as const, tone: '', icon: CircleXIcon }
}

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-4">
    <p class="text-muted-foreground text-sm">
      这里是站点模板库，每个模板代表一个资源站点的接入方式（共 {{ definitions.length }} 个）。找到想用的站点，点「添加」，
      系统就去把它配好——你不需要理解模板内容。已经有站点在用的模板会标记出来。
    </p>

    <SearchModal
      v-if="picking"
      :title="`添加「${picking.name}」`"
      subtitle="确认名称；需要账号的站点填写账号密码即可保存"
      @close="picking = null"
    >
      <TemplatePicker :preset="picking" @done="onPickerDone" @cancel="picking = null" />
    </SearchModal>

    <FieldGroup class="gap-3">
      <div class="flex flex-wrap items-center gap-2">
        <Input v-model="filterText" placeholder="搜索站点名称" class="h-8 min-w-52 max-w-xs flex-1" />

        <Select v-model="compatFilter">
          <SelectTrigger class="w-36"><SelectValue placeholder="全部">{{ compatLabel }}</SelectValue></SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value="all">全部（{{ definitions.length }}）</SelectItem>
              <SelectItem value="full">完全兼容（{{ counts.full }}）</SelectItem>
              <SelectItem value="partial">部分兼容（{{ counts.partial }}）</SelectItem>
              <SelectItem value="invalid">不可用（{{ counts.invalid }}）</SelectItem>
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

        <div class="ml-auto flex flex-wrap items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            :disabled="isBulkAdding || !pending.length"
            :title="pending.length ? `把当前筛选下未添加的 ${pending.length} 个模板一次加上` : '当前筛选下都添加过了'"
            @click="addAll"
          >
            <Spinner v-if="isBulkAdding" data-icon="inline-start" />
            <ListPlusIcon v-else data-icon="inline-start" />
            全部添加（{{ pending.length }}）
          </Button>
          <Button variant="outline" size="sm" :disabled="isSyncing" @click="sync">
            <Spinner v-if="isSyncing" data-icon="inline-start" />
            <RefreshCwIcon v-else data-icon="inline-start" />
            {{ isSyncing ? '更新中' : '更新模板库' }}
          </Button>
          <Button variant="outline" size="sm" @click="showImport = !showImport">
            <UploadIcon data-icon="inline-start" />
            导入 YAML
          </Button>
        </div>
      </div>

      <Field v-if="showImport" class="bg-card/40 gap-3 rounded-lg border p-4">
        <div class="flex flex-wrap items-center gap-2">
          <Input v-model="importName" placeholder="文件名（可选）" class="h-8 max-w-xs" />
          <Button variant="outline" size="sm" as-child>
            <label class="cursor-pointer">
              选择文件
              <input type="file" accept=".yml,.yaml" class="hidden" @change="pickFile" />
            </label>
          </Button>
          <Button size="sm" class="ml-auto" :disabled="isImporting" @click="importYaml">
            <Spinner v-if="isImporting" data-icon="inline-start" />
            导入
          </Button>
        </div>
        <Field>
          <FieldLabel for="yaml-content">定义内容</FieldLabel>
          <Textarea
            id="yaml-content"
            v-model="importContent"
            rows="8"
            placeholder="粘贴 Cardigann YAML 定义内容"
            class="font-mono text-xs"
          />
        </Field>
      </Field>
    </FieldGroup>

    <Alert v-if="isBulkAdding">
      <Spinner />
      <AlertTitle>正在批量添加</AlertTitle>
      <AlertDescription>{{ bulkProgress.done }} / {{ bulkProgress.total }}</AlertDescription>
    </Alert>

    <Alert v-else-if="justAdded.length" class="text-success">
      <CheckCircle2Icon />
      <AlertTitle>本次已添加 {{ justAdded.length }} 个站点</AlertTitle>
      <AlertDescription>点「已添加站点」页的「刷新」可看到完整状态。</AlertDescription>
    </Alert>

    <div v-if="isLoading" class="border-border flex items-center justify-center rounded-lg border px-4 py-10">
      <Spinner class="text-muted-foreground" />
    </div>

    <Empty v-else-if="!definitions.length">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <RefreshCwIcon />
        </EmptyMedia>
        <EmptyTitle>模板库还是空的</EmptyTitle>
        <EmptyDescription>点「更新模板库」联网获取；如果失败，去「设置 → 搜索下载」检查代理。</EmptyDescription>
      </EmptyHeader>
    </Empty>

    <div v-else class="border-border divide-border divide-y overflow-hidden rounded-lg border">
      <div v-for="def in filtered" :key="def.id" class="hover:bg-muted/50 p-3 transition-colors">
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <a
                v-if="def.site_url"
                :href="def.site_url"
                target="_blank"
                rel="noopener noreferrer"
                class="truncate text-sm hover:text-primary hover:underline"
                :title="'打开站点主页：' + def.site_url"
                @click.stop
              >{{ def.name }}</a>
              <span v-else class="truncate text-sm">{{ def.name }}</span>
              <Badge :variant="compatOf(def.compat).variant" :class="compatOf(def.compat).tone">
                <component :is="compatOf(def.compat).icon" data-icon="inline-start" />
                {{ compatOf(def.compat).label }}
              </Badge>
              <Badge v-if="def.source === 'local'" variant="outline">自定义</Badge>
              <Badge v-if="def.needs_login" variant="outline">需要账号</Badge>
              <Badge v-if="def.needs_captcha" variant="outline">有验证码</Badge>
              <Badge v-if="def.used_count" variant="outline">{{ def.used_count }} 个在用</Badge>
            </div>
            <div class="text-muted-foreground mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px]">
              <span>{{ accessLabel(def.type) }}</span>
              <span aria-hidden="true">·</span>
              <span class="text-info">{{ def.language_label || '未知语言' }}</span>
              <Badge
                v-for="group in (def.category_groups ?? []).slice(0, 4)"
                :key="group"
                variant="outline"
                class="text-[11px]"
              >{{ group }}</Badge>
            </div>
            <p v-if="def.mode_summary" class="text-muted-foreground mt-0.5 truncate text-[11px]">{{ def.mode_summary }}</p>
            <div class="text-muted-foreground mt-1 flex flex-wrap gap-x-3 text-[11px]">
              <span>{{ def.id }}</span>
              <span>版本 {{ def.version }}</span>
              <span>更新 {{ formatTime(def.updated_at) }}</span>
            </div>
          </div>
          <div class="flex shrink-0 items-center gap-1">
            <Badge v-if="usedNames.has(def.name)" variant="secondary" class="text-success">
              <CheckCircle2Icon data-icon="inline-start" />
              已添加
            </Badge>
            <Button v-else-if="def.compat !== 'invalid'" size="xs" @click="openPicker(def)">
              <PlusIcon data-icon="inline-start" />
              添加
            </Button>
            <Button variant="ghost" size="xs" @click="expanded[def.id] = !expanded[def.id]">
              {{ expanded[def.id] ? '收起' : '详情' }}
              <component :is="expanded[def.id] ? ChevronUpIcon : ChevronDownIcon" data-icon="inline-end" />
            </Button>
            <Button variant="ghost" size="icon-xs" aria-label="删除定义" @click="remove(def)">
              <Trash2Icon />
            </Button>
          </div>
        </div>

        <div v-if="expanded[def.id]" class="border-border mt-2 border-t pt-2 text-xs">
          <div class="text-muted-foreground flex flex-wrap gap-x-4 gap-y-1">
            <span>类型 {{ def.type }}</span>
            <span>语言 {{ def.language }}</span>
            <span>来源 {{ def.source === 'local' ? '本地导入' : '上游同步' }}</span>
          </div>
          <ul v-if="def.detail?.length" class="text-warning mt-1 flex flex-col gap-0.5">
            <li v-for="(line, index) in def.detail" :key="index">· {{ line }}</li>
          </ul>
        </div>
      </div>
    </div>
  </div>
</template>
