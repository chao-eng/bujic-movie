<script setup lang="ts">
import { computed } from 'vue'
import { Check, ExternalLink, RefreshCw, TriangleAlert, Wand2 } from 'lucide-vue-next'
import type { TitleCandidate, TitleResolution } from '@/api/search'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { Alert, AlertDescription } from '@/components/ui/alert'

const props = defineProps<{
  resolution: TitleResolution | null
  isRefreshing?: boolean
  isConfirming?: boolean
}>()

const emit = defineEmits<{
  (e: 'confirm', candidate: TitleCandidate): void
  (e: 'refresh'): void
  (e: 'clear'): void
}>()

const candidates = computed(() => props.resolution?.candidates ?? [])
const selectedId = computed(() => props.resolution?.selected?.tmdb_id ?? 0)
// 只要有候选就一直显示，便于已确认后继续切换；确认不再隐藏列表。
const showList = computed(() => candidates.value.length > 0)

const mediaLabel = (type: string) => (type === 'tv' ? '剧集' : '电影')

const posterURL = (path: string) =>
  path ? `https://image.tmdb.org/t/p/w92${path}` : ''

// tmdbURL links to the TMDB page for the candidate, opened in a new tab so the
// user can verify a match without losing their search.
const tmdbURL = (candidate: TitleCandidate) =>
  `https://www.themoviedb.org/${candidate.media_type === 'tv' ? 'tv' : 'movie'}/${candidate.tmdb_id}`

const openDetails = (candidate: TitleCandidate) => {
  window.open(tmdbURL(candidate), '_blank', 'noopener,noreferrer')
}

const chosenAliases = computed(() => props.resolution?.aliases ?? [])
</script>

<template>
  <div v-if="resolution" class="border-border flex flex-col gap-3 rounded-lg border p-3">
    <div class="flex flex-wrap items-center gap-2">
      <Wand2 class="text-primary size-4" />
      <span class="text-sm">片名识别</span>
      <Badge v-if="resolution.confirmed" variant="secondary" class="text-success">已确认</Badge>
      <Badge v-else-if="showList" variant="secondary" class="text-warning">待确认</Badge>
      <Badge v-else-if="resolution.degraded" variant="outline">按原词搜索</Badge>
      <Badge v-else variant="outline">自动匹配</Badge>

      <span class="text-muted-foreground text-xs">
        {{ resolution.selected ? resolution.selected.title : '未匹配到 TMDB 条目' }}
        <template v-if="resolution.selected?.year"> · {{ resolution.selected.year }}</template>
      </span>

      <div class="ml-auto flex items-center gap-1">
        <Button variant="ghost" size="sm" :disabled="isRefreshing" @click="emit('refresh')">
          <Spinner v-if="isRefreshing" data-icon="inline-start" />
          <RefreshCw v-else data-icon="inline-start" />
          重新识别
        </Button>
        <Button v-if="resolution.confirmed" variant="ghost" size="sm" @click="emit('clear')">
          清除选择
        </Button>
      </div>
    </div>

    <Alert v-if="resolution.degraded || resolution.message" class="text-warning">
      <TriangleAlert />
      <AlertDescription>{{ resolution.message || '片名识别不可用，已按原关键词搜索。' }}</AlertDescription>
    </Alert>

    <div v-if="chosenAliases.length" class="flex flex-wrap items-center gap-1">
      <span class="text-muted-foreground text-xs">实际检索词</span>
      <Badge v-for="alias in chosenAliases" :key="alias" variant="outline" class="text-[11px]">{{ alias }}</Badge>
    </div>

    <div v-if="showList" class="flex flex-col gap-2">
      <p class="text-muted-foreground text-xs">这是你要找的那部影视剧吗？选中的条目会用于后续搜索。</p>
      <div
        v-for="candidate in candidates"
        :key="`${candidate.media_type}-${candidate.tmdb_id}`"
        class="hover:bg-muted/60 flex items-start gap-3 rounded-md p-2 text-left transition-colors"
        :class="[
          selectedId === candidate.tmdb_id ? 'bg-muted' : '',
          isConfirming ? 'pointer-events-none opacity-60' : 'cursor-pointer',
        ]"
        role="button"
        :tabindex="isConfirming ? -1 : 0"
        :aria-pressed="selectedId === candidate.tmdb_id"
        @click="emit('confirm', candidate)"
        @keydown.enter.prevent="emit('confirm', candidate)"
        @keydown.space.prevent="emit('confirm', candidate)"
      >
        <img
          v-if="candidate.poster_path"
          :src="posterURL(candidate.poster_path)"
          :alt="candidate.title"
          class="h-16 w-11 shrink-0 rounded object-cover"
          loading="lazy"
        />
        <div v-else class="bg-muted h-16 w-11 shrink-0 rounded" />
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <span class="truncate text-sm">{{ candidate.title }}</span>
            <Badge variant="outline" class="text-[11px]">{{ mediaLabel(candidate.media_type) }}</Badge>
            <span v-if="candidate.year" class="text-muted-foreground text-xs">{{ candidate.year }}</span>
            <Badge v-if="selectedId === candidate.tmdb_id" variant="secondary" class="text-success text-[11px]">当前</Badge>
          </div>
          <p v-if="candidate.original_title && candidate.original_title !== candidate.title" class="text-muted-foreground truncate text-xs">
            原名：{{ candidate.original_title }}
          </p>
          <p v-if="candidate.overview" class="text-muted-foreground mt-1 line-clamp-2 text-xs">{{ candidate.overview }}</p>
          <div v-if="candidate.aliases?.length" class="mt-1 flex flex-wrap gap-1">
            <Badge v-for="alias in candidate.aliases.slice(0, 4)" :key="alias.title" variant="outline" class="text-[10px]">
              {{ alias.title }}
            </Badge>
          </div>
        </div>
        <div class="flex shrink-0 items-center gap-1">
          <Check v-if="selectedId === candidate.tmdb_id" class="text-primary size-4" />
          <Button
            variant="ghost"
            size="sm"
            class="pointer-events-auto"
            :aria-label="`在 TMDB 查看《${candidate.title}》`"
            @click.stop="openDetails(candidate)"
          >
            <ExternalLink data-icon="inline-start" />
            详情
          </Button>
        </div>
      </div>
      <Spinner v-if="isConfirming" class="self-start" />
    </div>
  </div>
</template>
