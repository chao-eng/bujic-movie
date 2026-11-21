<script setup lang="ts">
import { computed, ref } from 'vue'
import { Search as SearchIcon, Download, ChevronDown, ChevronUp, AlertTriangle, ExternalLink } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { trackSearch } from '@/api/track'
import { RequestGate } from '@/lib/searchRequest'
import { EMPTY_SELECT_VALUE, fromSelectValue, toSelectValue } from '@/lib/utils'
import {
  searchApi,
  newznabLabel,
  type GrabPreview,
  type SearchItem,
  type SearchResponse,
  type HistoryItem,
  type GrabResult,
  formatBytes,
} from '@/api/search'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Separator } from '@/components/ui/separator'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectGroup,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import SearchModal from '@/components/search/SearchModal.vue'

const keyword = ref('')
const results = ref<SearchItem[]>([])
const failures = ref<{ indexer_name: string; kind: string; message: string }[]>([])
const isSearching = ref(false)
const elapsed = ref(0)
const totalFound = ref(0)
const truncated = ref(false)
const hasSearched = ref(false)
const history = ref<HistoryItem[]>([])
const showFailures = ref(false)
const expandedSources = ref<Record<string, boolean>>({})
const noIndexers = ref(false)

const filters = ref({ minSeeders: 0, minSizeGB: 0, maxSizeGB: 0, category: 'all' })
const sortBy = ref<'seeders' | 'size' | 'date' | 'indexer'>('seeders')

const grabTarget = ref<SearchItem | null>(null)
const grabCategory = ref('')
const grabPreview = ref<GrabPreview | null>(null)
const isLoadingPreview = ref(false)
const isGrabbing = ref(false)

const gate = new RequestGate()

const filtered = computed(() => {
  let list = results.value.slice()

  if (filters.value.category !== 'all') {
    const want = Number(filters.value.category)
    list = list.filter((item) =>
      (item.categories ?? []).some(
        (cat) => cat === want || (String(cat).length === 4 && String(cat).slice(0, 2) === String(want).slice(0, 2)),
      ),
    )
  }
  if (filters.value.minSeeders > 0) list = list.filter((item) => item.seeders >= filters.value.minSeeders)
  if (filters.value.minSizeGB > 0) list = list.filter((item) => item.size_bytes === 0 || item.size_bytes >= filters.value.minSizeGB * 1024 ** 3)
  if (filters.value.maxSizeGB > 0) list = list.filter((item) => item.size_bytes === 0 || item.size_bytes <= filters.value.maxSizeGB * 1024 ** 3)

  switch (sortBy.value) {
    case 'size':
      list.sort((a, b) => b.size_bytes - a.size_bytes)
      break
    case 'date':
      list.sort((a, b) => (b.publish_date ?? '').localeCompare(a.publish_date ?? ''))
      break
    case 'indexer':
      list.sort((a, b) => a.indexer_name.localeCompare(b.indexer_name))
      break
    default:
      list.sort((a, b) => {
        if ((a.seeders === 0) !== (b.seeders === 0)) return a.seeders === 0 ? 1 : -1
        return b.seeders - a.seeders
      })
  }
  return list
})

const itemLabels = (item: SearchItem): string[] => {
  const fromServer = item.category_labels ?? []
  if (fromServer.length) return fromServer
  return (item.categories ?? []).map(newznabLabel)
}

const runSearch = async (term?: string) => {
  const query = (term ?? keyword.value).trim()
  if (term !== undefined) keyword.value = term

  const requestId = gate.next()
  const startedAt = performance.now()
  trackSearch('search_submit')

  isSearching.value = true
  hasSearched.value = true
  noIndexers.value = false
  results.value = []
  failures.value = []
  elapsed.value = 0
  totalFound.value = 0
  truncated.value = false

  try {
    const res: any = await searchApi.query({
      request_id: requestId,
      keyword: query,
      page: 1,
      page_size: 50,
      sort: sortBy.value,
    })
    if (!gate.isActive(requestId)) return
    if (res.code !== 0) {
      if (res.code === 40901) noIndexers.value = true
      toast.error(res.msg || '搜索失败')
      return
    }
    const data = res.data as SearchResponse
    if ((data.items?.length ?? 0) > 0) {
      trackSearch('search_first_render', { durationMS: performance.now() - startedAt, success: true })
    }
    results.value = data.items ?? []
    failures.value = data.failures ?? []
    elapsed.value = data.elapsed_ms
    totalFound.value = data.total_found ?? data.total ?? 0
    truncated.value = data.truncated
    await loadHistory()
  } catch (err: any) {
    if (!gate.isActive(requestId)) return
    if (err?.code === 40901) noIndexers.value = true
    toast.error(err?.msg || '搜索失败，请稍后重试')
  } finally {
    if (gate.isActive(requestId)) isSearching.value = false
  }
}

const loadHistory = async () => {
  try {
    const res: any = await searchApi.history()
    if (res.code === 0) history.value = res.data.items ?? []
  } catch {
    // 历史是辅助功能，失败不影响搜索。
  }
}

const removeHistory = async (term: string) => {
  try {
    await searchApi.deleteHistory(term)
    await loadHistory()
  } catch {
    toast.error('删除历史失败')
  }
}

const openGrab = async (item: SearchItem) => {
  grabTarget.value = item
  grabCategory.value = ''
  grabPreview.value = null
  isLoadingPreview.value = true
  try {
    const res: any = await searchApi.grabPreview({
      fingerprint: item.fingerprint,
      indexer_id: item.indexer_id,
      download_url: item.download_url,
      title: item.title,
      size_bytes: item.size_bytes,
      info_hash: item.info_hash,
      download_type: item.download_type,
      categories: item.categories ?? [],
    })
    if (res.code === 0 && res.data) {
      grabPreview.value = res.data as GrabPreview
      grabCategory.value = res.data.category || ''
    }
  } catch (err: any) {
    toast.error(err?.msg || '无法确定分类，请到「设置 → 下载器」检查配置')
  } finally {
    isLoadingPreview.value = false
  }
}

const openGrabFromSource = (item: SearchItem, source: { indexer_id: number; indexer_name: string; download_url: string; info_hash: string }) => {
  openGrab({
    ...item,
    indexer_id: source.indexer_id,
    indexer_name: source.indexer_name,
    download_url: source.download_url,
    info_hash: source.info_hash,
  })
}

const closeGrab = () => {
  grabTarget.value = null
  grabCategory.value = ''
  grabPreview.value = null
}

const confirmGrab = async () => {
  const item = grabTarget.value
  if (!item) return
  trackSearch('grab_confirm')
  isGrabbing.value = true
  try {
    const res: any = await searchApi.grab({
      fingerprint: item.fingerprint,
      indexer_id: item.indexer_id,
      download_url: item.download_url,
      title: item.title,
      size_bytes: item.size_bytes,
      info_hash: item.info_hash,
      download_type: item.download_type,
      categories: item.categories ?? [],
      category_override: grabCategory.value,
    })
    const data = res.data as GrabResult
    trackSearch('grab_result', { success: true })
    toast.info(data?.duplicate ? '该资源已在下载器中' : `任务已创建${data?.client_name ? '：' + data.client_name : ''}`)
    closeGrab()
  } catch (err: any) {
    trackSearch('grab_result', { success: false })
    const data = err?.data as GrabResult | undefined
    if (err?.code === 40902) {
      toast.info('该资源已在下载器中')
      closeGrab()
      return
    }
    toast.error(err?.msg || '推送失败')
    if (data) closeGrab()
  } finally {
    isGrabbing.value = false
  }
}

const openDetails = (item: SearchItem) => {
  const url = item.details_url || (item.sources ?? []).find((s) => s.details_url)?.details_url
  if (!url) {
    toast.info('该结果没有提供详情页')
    return
  }
  window.open(url, '_blank', 'noopener,noreferrer')
}

const toggleSources = (fingerprint: string) => {
  expandedSources.value[fingerprint] = !expandedSources.value[fingerprint]
}

const clearFilters = () => {
  filters.value = { minSeeders: 0, minSizeGB: 0, maxSizeGB: 0, category: 'all' }
}

loadHistory()
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center">
      <div class="relative flex-1">
        <SearchIcon class="text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2" />
        <Input
          v-model="keyword"
          placeholder="输入片名、剧名或关键词，回车搜索"
          class="h-10 pl-9"
          @keyup.enter="runSearch()"
        />
      </div>
      <Button class="h-10" :disabled="isSearching" @click="runSearch()">
        <Spinner v-if="isSearching" data-icon="inline-start" />
        <SearchIcon v-else data-icon="inline-start" />
        {{ isSearching ? '搜索中' : '搜索' }}
      </Button>
    </div>

    <div v-if="history.length" class="flex flex-wrap items-center gap-2">
      <span class="text-muted-foreground text-xs">最近搜索</span>
      <Badge
        v-for="entry in history"
        :key="entry.keyword"
        variant="outline"
        class="cursor-pointer gap-1 hover:bg-muted"
        @click="runSearch(entry.keyword)"
      >
        {{ entry.keyword }}
        <button
          class="text-muted-foreground hover:text-destructive"
          aria-label="删除该历史"
          @click.stop="removeHistory(entry.keyword)"
        >
          ×
        </button>
      </Badge>
    </div>

    <Alert v-if="noIndexers">
      <AlertTriangle />
      <AlertTitle>还没有可用的站点</AlertTitle>
      <AlertDescription>先去「已添加站点」页添加一个站点，再回来搜索。</AlertDescription>
    </Alert>

    <Alert v-if="failures.length" class="text-warning">
      <AlertTriangle />
      <AlertTitle class="flex items-center justify-between">
        <span>{{ failures.length }} 个站点失败，其余结果已展示</span>
        <Button variant="ghost" size="icon-xs" aria-label="展开失败详情" @click="showFailures = !showFailures">
          <component :is="showFailures ? ChevronUp : ChevronDown" />
        </Button>
      </AlertTitle>
      <AlertDescription v-if="showFailures">
        <ul class="mt-1 flex flex-col gap-1 text-xs">
          <li v-for="failure in failures" :key="failure.indexer_name">
            {{ failure.indexer_name }}：{{ failure.message }}
          </li>
        </ul>
      </AlertDescription>
    </Alert>

    <div v-if="hasSearched && !isSearching" class="border-border flex flex-wrap items-center gap-3 rounded-lg border px-3 py-2 text-xs">
      <span class="text-muted-foreground">
        共 {{ totalFound }} 条，筛后 {{ filtered.length }} 条
        <template v-if="elapsed"> · 耗时 {{ elapsed }}ms</template>
      </span>
      <Badge v-if="truncated" variant="secondary" class="text-warning">结果已截断，请缩小关键词</Badge>

      <div class="ml-auto flex flex-wrap items-end gap-2">
        <div class="flex flex-col gap-1">
          <Label class="text-xs">分类</Label>
          <Select v-model="filters.category">
            <SelectTrigger size="sm" class="w-28">
              <SelectValue placeholder="全部" />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value="all">全部</SelectItem>
                <SelectItem value="2000">电影</SelectItem>
                <SelectItem value="5000">剧集</SelectItem>
                <SelectItem value="5070">动画</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>
        <div class="flex flex-col gap-1">
          <Label class="text-xs">最小做种</Label>
          <Input v-model.number="filters.minSeeders" type="number" min="0" class="h-7 w-16" />
        </div>
        <div class="flex flex-col gap-1">
          <Label class="text-xs">大小 (GB)</Label>
          <div class="flex items-center gap-1">
            <Input v-model.number="filters.minSizeGB" type="number" min="0" class="h-7 w-16" />
            <span class="text-muted-foreground">–</span>
            <Input v-model.number="filters.maxSizeGB" type="number" min="0" class="h-7 w-16" />
          </div>
        </div>
        <div class="flex flex-col gap-1">
          <Label class="text-xs">排序</Label>
          <Select v-model="sortBy">
            <SelectTrigger size="sm" class="w-28">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value="seeders">做种数</SelectItem>
                <SelectItem value="size">大小</SelectItem>
                <SelectItem value="date">发布时间</SelectItem>
                <SelectItem value="indexer">站点</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>
      </div>
    </div>

    <div v-if="isSearching && !results.length" class="flex flex-col gap-2">
      <Skeleton v-for="n in 5" :key="n" class="h-16 w-full rounded-lg" />
    </div>

    <Empty v-else-if="hasSearched && !filtered.length && !isSearching">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SearchIcon />
        </EmptyMedia>
        <EmptyTitle>{{ results.length ? '没有符合当前筛选的结果' : '没有找到结果' }}</EmptyTitle>
        <EmptyDescription>
          {{ results.length ? '试着放宽筛选条件。' : '换个关键词，或到「已添加站点」页确认站点可用。' }}
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent v-if="results.length">
        <Button variant="outline" size="sm" @click="clearFilters">清除筛选</Button>
      </EmptyContent>
    </Empty>

    <div v-else-if="filtered.length" class="border-border divide-border divide-y overflow-hidden rounded-lg border">
      <div v-for="item in filtered" :key="item.fingerprint" class="hover:bg-muted/50 p-3 transition-colors">
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0 flex-1">
            <p class="truncate text-sm" :title="item.title">{{ item.title }}</p>
            <div class="text-muted-foreground mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
              <span class="text-primary/90">{{ item.indexer_name }}</span>
              <span>{{ formatBytes(item.size_bytes) }}</span>
              <Badge v-if="item.seeders > 0" variant="secondary" class="text-success">做种 {{ item.seeders }}</Badge>
              <Badge v-else variant="outline">做种 0</Badge>
              <Badge v-if="(item.sources?.length ?? 0) > 1" variant="secondary"> {{ item.sources?.length }} 个来源 </Badge>
              <Badge v-for="label in itemLabels(item)" :key="label" variant="outline" class="text-[11px]">
                {{ label }}
              </Badge>
            </div>
          </div>
          <div class="flex shrink-0 items-center gap-1">
            <Button v-if="item.details_url" variant="ghost" size="xs" @click="openDetails(item)">
              <ExternalLink data-icon="inline-start" />
              详情
            </Button>
            <Button v-if="(item.sources?.length ?? 0) > 1" variant="ghost" size="xs" @click="toggleSources(item.fingerprint)">
              来源
            </Button>
            <Button size="sm" @click="openGrab(item)">
              <Download data-icon="inline-start" />
              下载
            </Button>
          </div>
        </div>

        <div v-if="expandedSources[item.fingerprint] && item.sources" class="border-border mt-2 flex flex-col gap-1 border-t pt-2 text-xs">
          <div
            v-for="source in item.sources"
            :key="source.indexer_id + source.download_url"
            class="bg-muted/40 flex items-center justify-between gap-2 rounded px-2 py-1"
          >
            <span>{{ source.indexer_name }}</span>
            <span class="text-muted-foreground flex items-center gap-3">
              <span>{{ formatBytes(source.size_bytes) }}</span>
              <span>做种 {{ source.seeders }}</span>
              <a
                v-if="source.details_url"
                :href="source.details_url"
                target="_blank"
                rel="noopener noreferrer"
                class="text-info hover:underline"
              >详情</a>
              <Button variant="link" size="xs" @click="openGrabFromSource(item, source)">用此来源下载</Button>
            </span>
          </div>
        </div>
      </div>
    </div>

    <Empty v-else>
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SearchIcon />
        </EmptyMedia>
        <EmptyTitle>检索多个站点，找到后一键交给下载器</EmptyTitle>
        <EmptyDescription>先到「已添加站点」添加站点，再到「设置 → 下载器」添加一个下载器。</EmptyDescription>
      </EmptyHeader>
    </Empty>

    <SearchModal v-if="grabTarget" title="下载资源" :subtitle="grabTarget.title" @close="closeGrab">
      <div class="flex flex-col gap-3 text-sm">
        <div class="flex items-start justify-between gap-3">
          <span class="text-muted-foreground">来源站点</span>
          <span>{{ grabTarget.indexer_name }}</span>
        </div>
        <div class="flex items-start justify-between gap-3">
          <span class="text-muted-foreground">大小</span>
          <span>{{ formatBytes(grabTarget.size_bytes) }}</span>
        </div>
        <Separator />
        <div class="flex items-start justify-between gap-3">
          <span class="text-muted-foreground pt-1.5">下载器</span>
          <span class="flex-1 text-right">
            <Spinner v-if="isLoadingPreview" class="ml-auto" />
            <template v-else>{{ grabPreview?.client_name || '—' }}</template>
          </span>
        </div>
        <div class="flex items-start justify-between gap-3">
          <span class="text-muted-foreground pt-1.5">分类</span>
          <div class="flex flex-1 flex-col gap-1">
            <Select
              v-if="grabPreview?.categories?.length"
              :model-value="toSelectValue(grabCategory)"
              @update:model-value="grabCategory = fromSelectValue($event)"
            >
              <SelectTrigger class="w-full">
                <SelectValue placeholder="不指定（交给下载器决定）" />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem :value="EMPTY_SELECT_VALUE">不指定（交给下载器决定）</SelectItem>
                  <SelectItem v-for="cat in grabPreview.categories" :key="cat" :value="cat">{{ cat }}</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
            <Input v-else v-model="grabCategory" placeholder="下载器分类名，可留空" />
            <p
              v-if="grabPreview"
              class="text-[11px]"
              :class="grabPreview.category_from === 'none' ? 'text-warning' : 'text-muted-foreground'"
            >
              <template v-if="grabPreview.category_from === 'mapping'">按分类映射自动选中</template>
              <template v-else-if="grabPreview.category_from === 'parent'">按上级分类映射自动选中</template>
              <template v-else-if="grabPreview.category_from === 'default'">使用下载器的默认分类</template>
              <template v-else>该资源没有匹配的分类映射</template>
              <span v-if="grabPreview.warning"> · {{ grabPreview.warning }}</span>
            </p>
          </div>
        </div>
        <p class="text-muted-foreground text-xs">
          {{ grabPreview?.save_path_hint || '文件落盘位置由 qBittorrent 的分类规则决定，本页不指定保存目录。' }}
        </p>
        <div class="mt-2 flex justify-end gap-2">
          <Button variant="ghost" :disabled="isGrabbing" @click="closeGrab">取消</Button>
          <Button :disabled="isGrabbing || isLoadingPreview" @click="confirmGrab">
            <Spinner v-if="isGrabbing" data-icon="inline-start" />
            确认下载
          </Button>
        </div>
      </div>
    </SearchModal>
  </div>
</template>
