<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { PauseIcon, PlayIcon, Trash2Icon, RefreshCwIcon, TriangleAlertIcon, ClockIcon } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { useConfirm } from '@/composables/useConfirm'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'
import { Progress } from '@/components/ui/progress'
import { Label } from '@/components/ui/label'
import { Checkbox } from '@/components/ui/checkbox'
import { createVisibilityPoller } from '@/lib/searchRequest'
import {
  searchApi,
  type TaskGroup,
  type TaskItem,
  formatBytes,
  formatSpeed,
  formatETA,
  formatTime,
} from '@/api/search'

const groups = ref<TaskGroup[]>([])
const isLoading = ref(false)
const acting = ref<Record<string, boolean>>({})
const lastRefresh = ref('')
const deleteFiles = ref(false)
const { confirm } = useConfirm()

const poller = createVisibilityPoller({ intervalMs: 5000, fetch: () => load() })

const totalTasks = computed(() => groups.value.reduce((sum, group) => sum + (group.tasks?.length ?? 0), 0))

const load = async () => {
  isLoading.value = true
  try {
    const res: any = await searchApi.tasks()
    if (res.code === 0) {
      groups.value = res.data.groups ?? []
      lastRefresh.value = formatTime(new Date().toISOString())
    }
  } catch {
    toast.error('读取任务失败，显示的是上次数据')
  } finally {
    isLoading.value = false
  }
}

const onVisibility = () => {
  if (document.visibilityState === 'visible') poller.refreshNow()
}

const act = async (group: TaskGroup, task: TaskItem, action: 'pause' | 'resume' | 'delete') => {
  const key = `${group.client_id}-${task.hash}-${action}`
  if (action === 'delete') {
    const ok = await confirm({
      title: '删除任务',
      message: deleteFiles.value
        ? `将删除「${task.name}」并同时删除已下载的文件，该操作不可撤销。`
        : `将删除「${task.name}」，已下载的文件会保留。`,
      confirmText: '删除',
    })
    if (!ok) return
  }
  acting.value[key] = true
  try {
    await searchApi.taskAction(group.client_id, task.hash, action, action === 'delete' ? deleteFiles.value : false)
    await load()
  } catch (err: any) {
    toast.error(err?.msg || '操作失败')
  } finally {
    acting.value[key] = false
  }
}

const stateLabel = (state: string): string => {
  const map: Record<string, string> = {
    downloading: '下载中',
    uploading: '做种中',
    stalledDL: '等待中',
    stalledUP: '做种中',
    pausedDL: '已暂停',
    pausedUP: '已暂停',
    queuedDL: '排队中',
    queuedUP: '排队中',
    checkingDL: '校验中',
    checkingUP: '校验中',
    error: '出错',
    missingFiles: '文件缺失',
  }
  return map[state] ?? state
}

const progressPercent = (task: TaskItem): number => Math.round((task.progress ?? 0) * 100)

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
      <span>{{ totalTasks }} 个任务</span>
      <span v-if="lastRefresh" class="text-xs">数据时间 {{ lastRefresh }}</span>
      <div class="flex items-center gap-2">
        <Checkbox id="delete-files" :model-value="deleteFiles" @update:model-value="deleteFiles = Boolean($event)" />
        <Label for="delete-files" class="text-xs">删除时同时删除文件</Label>
      </div>
      <Button variant="outline" class="ml-auto" :disabled="isLoading" @click="load">
        <Spinner v-if="isLoading" data-icon="inline-start" />
        <RefreshCwIcon v-else data-icon="inline-start" />
        刷新
      </Button>
    </div>

    <Empty v-if="!groups.length && !isLoading">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <RefreshCwIcon />
        </EmptyMedia>
        <EmptyTitle>还没有配置下载器</EmptyTitle>
        <EmptyDescription>到「设置 → 下载器」添加 qBittorrent 后即可查看任务。</EmptyDescription>
      </EmptyHeader>
    </Empty>

    <div v-for="group in groups" :key="group.client_id" class="border-border overflow-hidden rounded-lg border">
      <div class="bg-muted/40 border-border flex items-center justify-between gap-2 border-b px-3 py-2">
        <div class="flex items-center gap-2">
          <span class="text-sm">{{ group.client_name }}</span>
          <Badge :variant="group.state === 'ok' ? 'secondary' : 'outline'" :class="group.state === 'ok' ? 'text-success' : 'text-warning'">
            {{ group.state === 'ok' ? '在线' : '不可达' }}
          </Badge>
        </div>
        <span v-if="group.state !== 'ok'" class="text-warning flex items-center gap-1 text-[11px]">
          <TriangleAlertIcon class="size-3" />
          {{ group.message || '下载器不可达' }}
          <span v-if="group.data_updated_at">（数据时间 {{ formatTime(group.data_updated_at) }}）</span>
        </span>
      </div>

      <Empty v-if="!group.tasks?.length" class="py-6">
        <EmptyDescription>暂无任务</EmptyDescription>
      </Empty>

      <div v-else class="divide-border divide-y">
        <div v-for="task in group.tasks" :key="task.hash" class="p-3">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0 flex-1">
              <p class="truncate text-sm" :title="task.name">{{ task.name }}</p>
              <div class="text-muted-foreground mt-1 flex flex-wrap items-center gap-x-3 text-[11px]">
                <Badge variant="outline">{{ stateLabel(task.state) }}</Badge>
                <span>{{ formatBytes(task.size_bytes) }}</span>
                <span v-if="task.speed_bps > 0">{{ formatSpeed(task.speed_bps) }}</span>
                <span class="flex items-center gap-1">
                  <ClockIcon class="size-3" />
                  {{ formatETA(task.eta_seconds) }}
                </span>
                <Badge v-if="task.category" variant="secondary">{{ task.category }}</Badge>
              </div>
              <Progress :model-value="progressPercent(task)" class="mt-2 h-1.5" />
              <p class="text-muted-foreground mt-1 text-[11px]">{{ progressPercent(task) }}%</p>
            </div>
            <div class="flex shrink-0 items-center gap-1">
              <Button
                v-if="task.state.startsWith('paused')"
                variant="ghost"
                size="xs"
                :disabled="acting[`${group.client_id}-${task.hash}-resume`]"
                @click="act(group, task, 'resume')"
              >
                <PlayIcon data-icon="inline-start" />
                继续
              </Button>
              <Button
                v-else
                variant="ghost"
                size="xs"
                :disabled="acting[`${group.client_id}-${task.hash}-pause`]"
                @click="act(group, task, 'pause')"
              >
                <PauseIcon data-icon="inline-start" />
                暂停
              </Button>
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label="删除任务"
                :disabled="acting[`${group.client_id}-${task.hash}-delete`]"
                @click="act(group, task, 'delete')"
              >
                <Trash2Icon />
              </Button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
