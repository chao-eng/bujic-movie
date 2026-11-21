<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { toast } from 'vue-sonner'
import {
  CheckIcon,
  CircleAlertIcon,
  ListPlusIcon,
  PlusIcon,
  RefreshCwIcon,
  SearchIcon,
} from 'lucide-vue-next'
import { useConfirm } from '@/composables/useConfirm'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { searchApi, accessLabel, accessGroupOf, type AccessGroup, type DefinitionItem, type IndexerItem } from '@/api/search'

/**
 * TemplatePicker adds a site from the built-in template list in two clicks:
 * search for a site name, press 添加, optionally fill credentials.
 */
const props = defineProps<{ preset?: DefinitionItem | null }>()
const emit = defineEmits<{ (e: 'done', payload: { count: number; added: string[] }): void; (e: 'cancel'): void }>()

const templates = ref<DefinitionItem[]>([])
const existing = ref<IndexerItem[]>([])
const isLoading = ref(false)
const isSyncing = ref(false)
const query = ref('')
const categoryFilter = ref('all')
const languageFilter = ref('all')
const accessFilter = ref<AccessGroup>('all')
// 从某个模板的「添加」进来时，直接进入填写表单，不显示模板列表——
// 用户已经选好了站点，再让他看到搜索框只会困惑。
const selected = ref<DefinitionItem | null>(props.preset ?? null)
const isSaving = ref(false)
// 本次会话内已添加的数量。添加后不关闭弹层、也不刷新列表，
// 这样用户可以连续添加多个站点。
const addedCount = ref(0)
const justAdded = ref<string[]>([])
const isBulkAdding = ref(false)
const bulkProgress = ref({ done: 0, total: 0 })

const form = ref({
  name: props.preset ? props.preset.name : '',
  username: '',
  password: '',
  extra_cookies: '',
  user_agent: '',
})
const { confirm } = useConfirm()

const load = async () => {
  isLoading.value = true
  try {
    const [defRes, idxRes]: any[] = await Promise.all([
      searchApi.listDefinitions(),
      searchApi.listIndexers(),
    ])
    if (defRes.code === 0) templates.value = defRes.data.items ?? []
    if (idxRes.code === 0) existing.value = idxRes.data.items ?? []
  } catch {
    toast.error('读取站点模板失败')
  } finally {
    isLoading.value = false
  }
}

const sync = async () => {
  isSyncing.value = true
  try {
    const res: any = await searchApi.syncDefinitions()
    const d = res.data ?? {}
    toast.success(`已更新站点模板：新增 ${d.added ?? 0}，更新 ${d.updated ?? 0}`)
    await load()
  } catch (err: any) {
    toast.error(err?.msg || '更新失败，请到「设置 → 搜索下载」检查代理')
  } finally {
    isSyncing.value = false
  }
}

const usedNames = computed(() => new Set(existing.value.map((i) => i.name)))

/** 分类与语言选项从实际模板里汇总，避免出现没有结果的筛选项。 */
const categoryOptions = computed(() => {
  const counts = new Map<string, number>()
  for (const t of templates.value) {
    if (t.compat === 'invalid') continue
    for (const group of t.category_groups ?? []) {
      counts.set(group, (counts.get(group) ?? 0) + 1)
    }
  }
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([label, count]) => ({ label, count }))
})

const accessCounts = computed(() => {
  let pub = 0
  let nonPub = 0
  for (const t of templates.value) {
    if (t.compat === 'invalid') continue
    if (accessGroupOf(t.type) === 'public') pub++
    else nonPub++
  }
  return { public: pub, nonPublic: nonPub }
})

const languageOptions = computed(() => {
  const counts = new Map<string, number>()
  for (const t of templates.value) {
    if (t.compat === 'invalid') continue
    const label = t.language_label || '未知'
    counts.set(label, (counts.get(label) ?? 0) + 1)
  }
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([label, count]) => ({ label, count }))
})

const filtered = computed(() => {
  const term = query.value.trim().toLowerCase()
  let list = templates.value.filter((t) => t.compat !== 'invalid')

  if (categoryFilter.value !== 'all') {
    list = list.filter((t) => (t.category_groups ?? []).includes(categoryFilter.value))
  }
  if (languageFilter.value !== 'all') {
    list = list.filter((t) => (t.language_label || '未知') === languageFilter.value)
  }
  if (accessFilter.value !== 'all') {
    list = list.filter((t) => accessGroupOf(t.type) === accessFilter.value)
  }
  if (term) {
    list = list.filter((t) => t.name.toLowerCase().includes(term) || t.id.toLowerCase().includes(term))
  }
  return list.slice(0, 80)
})

const isAdded = (t: DefinitionItem) => usedNames.value.has(t.name)

const pick = (t: DefinitionItem) => {
  selected.value = t
  form.value = {
    name: isAdded(t) ? `${t.name} 2` : t.name,
    username: '',
    password: '',
    extra_cookies: '',
    user_agent: '',
  }
}

const submit = async () => {
  const t = selected.value
  if (!t) return
  if (!form.value.name.trim()) {
    toast.error('请填写名称')
    return
  }
  isSaving.value = true
  try {
    const payload: Record<string, unknown> = {
      name: form.value.name.trim(),
      connector: 'cardigann',
      definition_id: t.id,
      secrets:
        form.value.username || form.value.password
          ? { username: form.value.username, password: form.value.password }
          : undefined,
      extra_cookies: form.value.extra_cookies || undefined,
      user_agent: form.value.user_agent || undefined,
    }
    await searchApi.createIndexer(payload)
    const name = form.value.name.trim()
    // 记录到本地：列表立刻显示「已添加」，不需要重新拉取全部站点。
    existing.value = [
      ...existing.value,
      { id: -1, name } as IndexerItem,
    ]
    justAdded.value = [...justAdded.value, name]
    addedCount.value += 1
    toast.success(`已添加「${name}」`)
    // 从列表点「添加」进来的（带 preset）：加完即关闭；
    // 从「添加站点」进来的：回到列表，方便连续添加多个。
    if (props.preset) {
      close()
    } else {
      selected.value = null
    }
  } catch (err: any) {
    toast.error(err?.msg || '添加失败')
  } finally {
    isSaving.value = false
  }
}

/** 未添加过的模板，按当前筛选结果计算。 */
const pending = computed(() => filtered.value.filter((t) => !isAdded(t)))

/** 批量添加当前筛选结果：一次把选好的分类/语言/访问类型全部加上。 */
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
  // 串行添加：站点创建会写库，并发只会让失败更难定位。
  for (const t of targets) {
    try {
      await searchApi.createIndexer({ name: t.name, connector: 'cardigann', definition_id: t.id })
      existing.value = [...existing.value, { id: -1, name: t.name } as IndexerItem]
      justAdded.value = [...justAdded.value, t.name]
      success++
    } catch (err: any) {
      failed++
      // 名称重复是最常见的失败原因，直接跳过继续。
      if (failed === 1) toast.error(err?.msg || `「${t.name}」添加失败`)
    }
    bulkProgress.value = { done: bulkProgress.value.done + 1, total: targets.length }
  }
  addedCount.value += success
  isBulkAdding.value = false
  toast.success(`批量添加完成：成功 ${success} 个${failed ? `，失败 ${failed} 个` : ''}`)
}

/** 关闭弹层：把本次添加的数量交给父组件，父组件据此决定是否刷新。 */
const close = () => {
  // 只把本次添加的名称交给父组件，由它就地补进列表，
  // 避免整页重新拉取把用户刚看到的内容刷掉。
  emit('done', { count: addedCount.value, added: justAdded.value })
}

onMounted(async () => {
  // 表单不依赖模板与站点列表，先让用户能填；列表在后台加载，
  // 这样从「添加」进来时不会先闪一下搜索框。
  if (props.preset) {
    load()
    return
  }
  await load()
})
</script>

<template>
  <div class="flex flex-col gap-3">
    <div v-if="!selected" class="flex flex-wrap items-center gap-2">
      <div class="relative min-w-56 flex-1">
        <SearchIcon class="text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2" />
        <Input v-model="query" class="h-8 pl-9" placeholder="搜索站点名称，例如 The Pirate Bay、Nyaa" />
      </div>
      <Button variant="outline" size="sm" :disabled="isSyncing" @click="sync">
        <Spinner v-if="isSyncing" data-icon="inline-start" />
        <RefreshCwIcon v-else data-icon="inline-start" />
        更新模板
      </Button>
      <Button variant="ghost" size="sm" @click="close">
        {{ addedCount ? `完成（已添加 ${addedCount}）` : '取消' }}
      </Button>
    </div>

    <Alert v-if="isBulkAdding">
      <Spinner />
      <AlertTitle>正在批量添加</AlertTitle>
      <AlertDescription>{{ bulkProgress.done }} / {{ bulkProgress.total }}</AlertDescription>
    </Alert>

    <Alert v-if="!props.preset && justAdded.length && !isBulkAdding" class="text-success">
      <CheckIcon />
      <AlertTitle>本次已添加 {{ justAdded.length }} 个站点</AlertTitle>
      <AlertDescription>可继续选择下一个；关闭后列表会更新。</AlertDescription>
    </Alert>

    <div v-if="!selected" class="flex flex-wrap items-center gap-2">
      <Select v-model="categoryFilter">
        <SelectTrigger size="sm" class="w-40"><SelectValue placeholder="分类：全部" /></SelectTrigger>
        <SelectContent>
          <SelectGroup>
            <SelectItem value="all">分类：全部</SelectItem>
            <SelectItem v-for="opt in categoryOptions" :key="opt.label" :value="opt.label">{{ opt.label }}（{{ opt.count }}）</SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>

      <Select v-model="languageFilter">
        <SelectTrigger size="sm" class="w-36"><SelectValue placeholder="语言：全部" /></SelectTrigger>
        <SelectContent>
          <SelectGroup>
            <SelectItem value="all">语言：全部</SelectItem>
            <SelectItem v-for="opt in languageOptions" :key="opt.label" :value="opt.label">{{ opt.label }}（{{ opt.count }}）</SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>

      <Select v-model="accessFilter">
        <SelectTrigger size="sm" class="w-48"><SelectValue placeholder="访问：公开与非公开" /></SelectTrigger>
        <SelectContent>
          <SelectGroup>
            <SelectItem value="all">访问：公开与非公开</SelectItem>
            <SelectItem value="public">公开（{{ accessCounts.public }}）</SelectItem>
            <SelectItem value="nonPublic">非公开（{{ accessCounts.nonPublic }}）</SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>

      <span class="text-muted-foreground text-xs">匹配 {{ filtered.length }} 个</span>
      <Button
        class="ml-auto"
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
    </div>

    <p v-if="!selected" class="text-muted-foreground text-xs">
      这些是内置的站点适配模板（共 {{ templates.length }} 个），选中后填个名字就能用，不需要懂配置。
    </p>

    <div v-if="!selected && isLoading" class="border-border flex items-center justify-center rounded-lg border px-4 py-8">
      <Spinner class="text-muted-foreground" />
    </div>

    <Empty v-else-if="!selected && !templates.length">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <RefreshCwIcon />
        </EmptyMedia>
        <EmptyTitle>还没有站点模板</EmptyTitle>
        <EmptyDescription>点「更新模板」联网获取。</EmptyDescription>
      </EmptyHeader>
    </Empty>

    <Empty v-else-if="!selected && !filtered.length">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SearchIcon />
        </EmptyMedia>
        <EmptyTitle>没有匹配「{{ query }}」的站点</EmptyTitle>
        <EmptyDescription>换个关键词，或清空筛选条件。</EmptyDescription>
      </EmptyHeader>
    </Empty>

    <div
      v-else-if="!selected"
      class="border-border divide-border divide-y max-h-[26rem] overflow-y-auto rounded-lg border"
    >
      <div
        v-for="t in filtered"
        :key="t.id"
        role="button"
        tabindex="0"
        class="hover:bg-muted/60 focus:bg-muted/60 flex w-full cursor-pointer items-center gap-3 px-3 py-2 text-left transition-colors focus:outline-none"
        @click="pick(t)"
        @keydown.enter.prevent="pick(t)"
        @keydown.space.prevent="pick(t)"
      >
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <a
              v-if="t.site_url"
              :href="t.site_url"
              target="_blank"
              rel="noopener noreferrer"
              class="truncate text-sm hover:text-primary hover:underline"
              :title="'打开站点主页：' + t.site_url"
              @click.stop
            >{{ t.name }}</a>
            <span v-else class="truncate text-sm">{{ t.name }}</span>
            <Badge v-if="isAdded(t)" variant="secondary" class="text-success">
              <CheckIcon data-icon="inline-start" />
              已添加
            </Badge>
            <Badge v-if="t.compat === 'partial'" variant="secondary" class="text-warning">
              <CircleAlertIcon data-icon="inline-start" />
              部分功能不可用
            </Badge>
            <Badge v-if="t.needs_login" variant="outline">需要账号</Badge>
            <Badge v-if="t.needs_captcha" variant="outline">有验证码</Badge>
          </div>
          <div class="text-muted-foreground mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px]">
            <span>{{ accessLabel(t.type) }}</span>
            <span aria-hidden="true">·</span>
            <span class="text-info">{{ t.language_label || '未知语言' }}</span>
            <template v-if="t.category_groups?.length">
              <span aria-hidden="true">·</span>
              <Badge
                v-for="group in t.category_groups.slice(0, 4)"
                :key="group"
                variant="outline"
                class="text-[11px]"
              >{{ group }}</Badge>
              <span v-if="t.category_groups.length > 4">等 {{ t.category_groups.length }} 类</span>
            </template>
            <span v-if="t.used_count" class="text-success">· {{ t.used_count }} 个在用</span>
          </div>
          <p v-if="t.mode_summary" class="text-muted-foreground mt-0.5 truncate text-[11px]">{{ t.mode_summary }}</p>
        </div>
        <PlusIcon class="text-muted-foreground size-4 shrink-0" />
      </div>
    </div>

    <!-- Details form -->
    <FieldGroup v-if="selected" class="border-border bg-card/60 gap-4 rounded-lg border p-4">
      <div class="flex items-center justify-between gap-2">
        <p class="text-sm font-semibold">添加「{{ selected.name }}」</p>
        <Button v-if="!props.preset" variant="ghost" size="xs" @click="selected = null">换一个</Button>
      </div>

      <Field>
        <FieldLabel for="site-name">显示名称</FieldLabel>
        <Input id="site-name" v-model="form.name" placeholder="站点名字，可自定义" />
      </Field>

      <Alert v-if="selected.needs_login" class="text-warning">
        <CircleAlertIcon />
        <AlertTitle>这个站点需要登录才能搜索</AlertTitle>
        <AlertDescription>
          请填写你在该站注册的账号。
          <template v-if="selected.needs_captcha">该站点还有验证码，添加后点「测试」时按提示输入。</template>
        </AlertDescription>
      </Alert>

      <div v-if="selected.needs_login" class="grid gap-4 sm:grid-cols-2">
        <Field>
          <FieldLabel for="site-username">账号</FieldLabel>
          <Input id="site-username" v-model="form.username" placeholder="用户名 / 邮箱" />
        </Field>
        <Field>
          <FieldLabel for="site-password">密码</FieldLabel>
          <Input id="site-password" v-model="form.password" type="password" placeholder="密码" />
        </Field>
      </div>

      <Field>
        <FieldLabel for="site-cookies">可选：站点要求浏览器验证时（Cloudflare）</FieldLabel>
        <Textarea
          id="site-cookies"
          v-model="form.extra_cookies"
          rows="2"
          placeholder="在浏览器完成验证后，从开发者工具复制 Cookie"
          class="font-mono text-xs"
        />
      </Field>
      <Field>
        <FieldLabel for="site-agent">User-Agent</FieldLabel>
        <Input id="site-agent" v-model="form.user_agent" placeholder="与完成验证的浏览器一致" />
      </Field>

      <div class="flex justify-end gap-2">
        <Button v-if="!props.preset" variant="ghost" size="sm" @click="selected = null">返回列表</Button>
        <Button v-else variant="ghost" size="sm" @click="close">取消</Button>
        <Button size="sm" :disabled="isSaving" @click="submit">
          <Spinner v-if="isSaving" data-icon="inline-start" />
          <PlusIcon v-else data-icon="inline-start" />
          添加站点
        </Button>
      </div>
    </FieldGroup>
  </div>
</template>
