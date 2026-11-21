<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { LogInIcon, RefreshCwIcon, SnowflakeIcon } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { createVisibilityPoller } from '@/lib/searchRequest'
import { searchApi, type IndexerItem, formatTime } from '@/api/search'

const indexers = ref<IndexerItem[]>([])
const isLoading = ref(false)
const lastRefresh = ref('')

const poller = createVisibilityPoller({ intervalMs: 30000, fetch: () => load() })

const load = async () => {
  isLoading.value = true
  try {
    const res: any = await searchApi.health()
    if (res.code === 0) {
      indexers.value = res.data.indexers ?? []
      lastRefresh.value = formatTime(new Date().toISOString())
    }
  } catch {
    toast.error('读取站点状态失败')
  } finally {
    isLoading.value = false
  }
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

const onVisibility = () => {
  if (document.visibilityState === 'visible') poller.refreshNow()
}

/** 状态用 Badge 表达，文案与颜色分开，不只靠颜色。 */
const stateOf = (indexer: IndexerItem) => {
  const remaining = indexer.status?.cooldown_remaining_ms ?? 0
  if (!indexer.enabled) return { label: '已停用', variant: 'outline' as const }
  if (indexer.definition_status === 'missing') return { label: '定义缺失', variant: 'destructive' as const }
  if (remaining > 0) return { label: `冷却中 ${Math.ceil(remaining / 60000)} 分钟`, variant: 'secondary' as const }
  if (indexer.session_state === 'failed') return { label: '需重新登录', variant: 'destructive' as const }
  if (indexer.session_state === 'logged-in') return { label: '已登录', variant: 'secondary' as const }
  if (!indexer.status?.last_success_at) return { label: '从未成功', variant: 'outline' as const }
  if ((indexer.status?.consecutive_failures ?? 0) >= 1) return { label: '降级', variant: 'secondary' as const }
  return { label: '正常', variant: 'secondary' as const }
}

onMounted(() => {
  load()
  poller.start()
  document.addEventListener('visibilitychange', onVisibility)
})

onUnmounted(() => {
  poller.stop()
  document.removeEventListener('visibilitychange', onVisibility)
})
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="text-muted-foreground flex flex-wrap items-center gap-3 text-sm">
      <span>共 {{ indexers.length }} 个站点</span>
      <span v-if="lastRefresh" class="text-xs">数据时间 {{ lastRefresh }}</span>
      <Button variant="outline" class="ml-auto" :disabled="isLoading" @click="load">
        <Spinner v-if="isLoading" data-icon="inline-start" />
        <RefreshCwIcon v-else data-icon="inline-start" />
        刷新
      </Button>
    </div>

    <Empty v-if="!indexers.length && !isLoading">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <RefreshCwIcon />
        </EmptyMedia>
        <EmptyTitle>还没有站点</EmptyTitle>
        <EmptyDescription>添加站点后，这里会显示每个站点的健康状态。</EmptyDescription>
      </EmptyHeader>
    </Empty>

    <div v-else class="border-border overflow-hidden rounded-lg border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>站点</TableHead>
            <TableHead>状态</TableHead>
            <TableHead>最近成功</TableHead>
            <TableHead class="text-right">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="indexer in indexers" :key="indexer.id">
            <TableCell>
              <p>{{ indexer.name }}</p>
              <p class="text-muted-foreground text-[11px]">
                {{ indexer.connector === 'cardigann' ? indexer.definition_id : indexer.connector }}
                · 连续失败 {{ indexer.status?.consecutive_failures ?? 0 }}
                · 平均 {{ indexer.status?.avg_elapsed_ms ? indexer.status.avg_elapsed_ms + 'ms' : '—' }}
              </p>
            </TableCell>
            <TableCell>
              <Badge :variant="stateOf(indexer).variant">{{ stateOf(indexer).label }}</Badge>
            </TableCell>
            <TableCell class="text-muted-foreground text-xs">
              {{ indexer.status?.last_success_at ? formatTime(indexer.status.last_success_at) : '从未成功' }}
            </TableCell>
            <TableCell class="text-right">
              <Button
                v-if="(indexer.status?.cooldown_remaining_ms ?? 0) > 0"
                variant="ghost"
                size="xs"
                @click="clearCooldown(indexer)"
              >
                <SnowflakeIcon data-icon="inline-start" />
                立即恢复
              </Button>
              <TooltipProvider v-else-if="indexer.session_state === 'failed'">
                <Tooltip>
                  <TooltipTrigger as-child>
                    <span class="text-destructive inline-flex items-center gap-1 text-[11px]">
                      <LogInIcon class="size-3" />
                      到站点页测试
                    </span>
                  </TooltipTrigger>
                  <TooltipContent>会话已失效，去「已添加站点」页点一次测试重新登录。</TooltipContent>
                </Tooltip>
              </TooltipProvider>
              <span v-else class="text-muted-foreground text-[11px]">—</span>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
  </div>
</template>
