<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  ArrowLeftRightIcon,
  DatabaseIcon,
  FilmIcon,
  PlayIcon,
  RefreshCwIcon,
  SparklesIcon,
  TerminalIcon,
} from 'lucide-vue-next'
import client from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { useLogStore } from '@/stores/logStore'

const router = useRouter()
const logStore = useLogStore()

const movieCount = ref<string | number>('…')
const tvCount = ref<string | number>('…')
const pendingCount = ref<string | number>('…')
const successRate = ref<string>('…')

const logContainer = ref<HTMLElement | null>(null)

const scrollToBottom = () => {
  if (logContainer.value) {
    logContainer.value.scrollTop = logContainer.value.scrollHeight
  }
}

watch(() => logStore.logs.length, () => {
  nextTick(scrollToBottom)
})

const fetchStats = async () => {
  try {
    const res: any = await client.get('/api/v1/dashboard/stats')
    if (res.code === 0 && res.data) {
      movieCount.value = res.data.movie_count
      tvCount.value = res.data.tv_count
      pendingCount.value = res.data.pending_count
      successRate.value = res.data.success_rate.toFixed(1) + '%'
    }
  } catch (err) {
    console.error('读取统计失败', err)
  }
}

const stats = [
  { label: '电影总数', icon: FilmIcon, tone: 'text-primary', value: movieCount },
  { label: '电视剧集', icon: DatabaseIcon, tone: 'text-info', value: tvCount },
  { label: '待整理文件', icon: ArrowLeftRightIcon, tone: 'text-warning', value: pendingCount },
  { label: '整理成功率', icon: SparklesIcon, tone: 'text-success', value: successRate },
]

/** 日志级别用 Badge 语气表达，级别文字始终可见。 */
const levelTone = (level: string) => {
  if (level === 'ERROR') return 'destructive' as const
  if (level === 'WARN') return 'secondary' as const
  return 'outline' as const
}

/** 通道状态也走 Badge，文案自己说明，不只靠颜色。 */
const channelState = computed(() => {
  switch (logStore.state) {
    case 'open':
      return { label: '已连接', variant: 'secondary' as const, tone: 'text-success' }
    case 'connecting':
      return { label: '连接中', variant: 'outline' as const, tone: 'text-warning' }
    case 'reconnecting':
      return { label: `重连中${logStore.reconnectAttempt > 1 ? `（第 ${logStore.reconnectAttempt} 次）` : ''}`, variant: 'outline' as const, tone: 'text-warning' }
    case 'closed':
      return { label: '已断开', variant: 'destructive' as const, tone: '' }
    default:
      return { label: '未连接', variant: 'outline' as const, tone: '' }
  }
})

onMounted(() => {
  fetchStats()
  nextTick(scrollToBottom)
})
</script>

<template>
  <div class="flex flex-col gap-6">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">控制面板</h1>
        <p class="text-muted-foreground mt-1 text-sm">
          欢迎使用布吉影库媒体管家，轻松完成电影电视剧的自动归档与元数据刮削。
        </p>
      </div>
      <Button class="gap-2" @click="router.push('/transfer')">
        <PlayIcon data-icon="inline-start" />
        快速开始整理
      </Button>
    </div>

    <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
      <Card v-for="stat in stats" :key="stat.label">
        <CardHeader class="flex flex-row items-center justify-between">
          <CardTitle class="text-muted-foreground text-sm font-medium">{{ stat.label }}</CardTitle>
          <component :is="stat.icon" :class="['size-5', stat.tone]" />
        </CardHeader>
        <CardContent>
          <p class="text-2xl font-bold">{{ stat.value }}</p>
        </CardContent>
      </Card>
    </div>

    <Card>
      <CardHeader>
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div>
            <CardTitle class="flex items-center gap-2">
              <TerminalIcon class="text-primary size-5" />
              后台运行日志
            </CardTitle>
            <CardDescription>实时日志通道概览</CardDescription>
          </div>
          <div class="flex items-center gap-2">
            <Badge :variant="channelState.variant" :class="channelState.tone">{{ channelState.label }}</Badge>
            <Button
              variant="outline"
              size="sm"
              :disabled="logStore.state === 'open' || logStore.state === 'connecting'"
              @click="logStore.reconnect()"
            >
              <RefreshCwIcon data-icon="inline-start" />
              重连
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <div
          ref="logContainer"
          class="bg-background border-border flex h-96 flex-col gap-1.5 overflow-y-auto rounded-lg border p-4 font-mono text-xs"
        >
          <div v-for="(log, idx) in logStore.logs" :key="idx" class="flex items-start gap-2 leading-relaxed select-text">
            <span class="text-muted-foreground shrink-0">{{ log.timestamp }}</span>
            <Badge :variant="levelTone(log.level)" class="shrink-0 text-[9px]">{{ log.level }}</Badge>
            <span class="flex-1 whitespace-pre-wrap break-all">{{ log.message }}</span>
          </div>

          <Empty v-if="!logStore.logs.length" class="border-0 py-20">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <TerminalIcon />
              </EmptyMedia>
              <EmptyTitle>正在开启日志通道…</EmptyTitle>
              <EmptyDescription>后台有新日志时会自动出现在这里。</EmptyDescription>
            </EmptyHeader>
          </Empty>
        </div>
      </CardContent>
    </Card>
  </div>
</template>
