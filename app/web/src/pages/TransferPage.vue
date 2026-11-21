<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { toast } from 'vue-sonner'
import {
  ArrowLeftRightIcon,
  CheckCircle2Icon,
  FilmIcon,
  TvIcon,
} from 'lucide-vue-next'
import client from '@/api/client'
import FileBrowserCard from '@/components/common/FileBrowserCard.vue'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { cn, EMPTY_SELECT_VALUE, fromSelectValue, toSelectValue } from '@/lib/utils'

const selectedPath = ref('')
const transferMode = ref('link')
const overwriteMode = ref('size')
const mediaType = ref('')

const cards = ref<any[]>([])
const selectedCardId = ref<number | null>(null)

const currentDir = ref('/')
const fileItems = ref<any[]>([])
const isLoadingFiles = ref(false)
const isSubmitting = ref(false)
const successMsg = ref('')

const queue = ref<any[]>([])
const history = ref<any[]>([])
const isLoadingHistory = ref(false)

let pollInterval: any = null

const fetchFiles = async (path: string) => {
  isLoadingFiles.value = true
  try {
    const res: any = await client.get(`/api/v1/files?path=${encodeURIComponent(path)}`)
    if (res.code === 0) {
      fileItems.value = res.data || []
      currentDir.value = path
    }
  } catch (err) {
    console.error('读取目录失败', err)
  } finally {
    isLoadingFiles.value = false
  }
}

const navigateUp = () => {
  if (currentDir.value === '/' || currentDir.value === '') return
  const parts = currentDir.value.split('/')
  parts.pop()
  let parent = parts.join('/')
  if (parent === '') parent = '/'
  fetchFiles(parent)
}

const handleItemClick = (item: any) => {
  selectedPath.value = item.path
}

const handleItemDoubleClick = (item: any) => {
  if (item.is_dir) {
    fetchFiles(item.path)
  } else {
    selectedPath.value = item.path
  }
}

const fetchQueue = async () => {
  try {
    const res: any = await client.get('/api/v1/transfer/queue')
    if (res.code === 0) {
      queue.value = res.data || []
    }
  } catch (err) {
    console.error('读取队列失败', err)
  }
}

const fetchHistory = async () => {
  isLoadingHistory.value = true
  try {
    const res: any = await client.get('/api/v1/transfer/history?page=1&limit=20')
    if (res.code === 0) {
      history.value = res.data || []
    }
  } catch (err) {
    console.error('读取历史失败', err)
  } finally {
    isLoadingHistory.value = false
  }
}

const triggerTransfer = async () => {
  if (!selectedPath.value) return

  isSubmitting.value = true
  successMsg.value = ''

  try {
    const res: any = await client.post('/api/v1/transfer', {
      path: selectedPath.value,
      mode: transferMode.value,
      overwrite_mode: overwriteMode.value,
      media_type: mediaType.value,
      card_id: selectedCardId.value || undefined,
    })

    if (res.code === 0) {
      successMsg.value = '整理任务成功提交至后台队列！'
      selectedPath.value = ''
      fetchQueue()
      setTimeout(() => {
        successMsg.value = ''
      }, 5000)
    }
  } catch (err: any) {
    toast.error('错误: ' + (err.message || '提交失败'))
  } finally {
    isSubmitting.value = false
  }
}

const fetchCards = async () => {
  try {
    const res: any = await client.get('/api/v1/cards')
    if (res.code === 0) {
      cards.value = res.data || []
    }
  } catch (err) {
    console.error('读取卡片失败', err)
  }
}

const selectCard = (card: any) => {
  selectedCardId.value = card.id
  mediaType.value = card.media_type || ''
  fetchFiles(card.download_path)
}

/** 队列与历史状态用 Badge 表达。 */
const queueTone = (status: string) => {
  if (status === 'running') return { variant: 'secondary' as const, tone: 'text-warning' }
  if (status === 'success') return { variant: 'secondary' as const, tone: 'text-success' }
  if (status === 'failed') return { variant: 'destructive' as const, tone: '' }
  return { variant: 'outline' as const, tone: '' }
}

onMounted(async () => {
  await fetchCards()

  let initialPath = '/'
  if (cards.value.length > 0) {
    const defaultCard = cards.value.find((c: any) => c.is_default) || cards.value[0]
    selectedCardId.value = defaultCard.id
    mediaType.value = defaultCard.media_type || ''
    initialPath = defaultCard.download_path || '/'
  } else {
    // 兼容旧设置
    try {
      const res: any = await client.get('/api/v1/settings')
      if (res.code === 0 && res.data && res.data.download_path) {
        initialPath = res.data.download_path
      }
    } catch (err) {
      console.error('读取下载目录失败', err)
    }
  }

  if (initialPath && initialPath !== '/') {
    fetchFiles(initialPath)
  }
  fetchQueue()
  fetchHistory()

  // 每 3 秒刷一次队列与历史，进度才有实时感
  pollInterval = setInterval(() => {
    fetchQueue()
    fetchHistory()
  }, 3000)
})

onUnmounted(() => {
  if (pollInterval) clearInterval(pollInterval)
})
</script>

<template>
  <div class="flex flex-col gap-6">
    <div>
      <h1 class="text-2xl font-bold tracking-tight">文件整理</h1>
      <p class="text-muted-foreground mt-1 text-sm">
        自动识别下载完成的影片并重命名，按电影或电视剧季/集归档整理。
      </p>
    </div>

    <Alert v-if="successMsg" class="text-success">
      <CheckCircle2Icon />
      <AlertTitle>整理任务已提交</AlertTitle>
      <AlertDescription>任务已进入后台队列，可在下方查看进度。</AlertDescription>
    </Alert>

    <div v-if="cards.length > 0" class="flex gap-2 overflow-x-auto pb-2">
      <Button
        v-for="card in cards"
        :key="card.id"
        :variant="selectedCardId === card.id ? 'default' : 'outline'"
        class="shrink-0"
        @click="selectCard(card)"
      >
        <FilmIcon v-if="card.media_type === 'movie'" data-icon="inline-start" />
        <TvIcon v-else data-icon="inline-start" />
        {{ card.name }}
        <Badge v-if="card.is_default" variant="secondary" class="ml-1">默认</Badge>
      </Button>
    </div>

    <div class="grid gap-6 lg:grid-cols-5">
      <FileBrowserCard
        class="lg:col-span-3"
        title="文件浏览器"
        description="选择待整理的源文件或文件夹"
        :current-dir="currentDir"
        :items="fileItems"
        :loading="isLoadingFiles"
        :selected-path="selectedPath"
        @select="handleItemClick"
        @open="handleItemDoubleClick"
        @refresh="fetchFiles(currentDir)"
        @up="navigateUp"
      />

      <Card>
        <CardHeader>
          <CardTitle>整理选项</CardTitle>
          <CardDescription>设置转移规则与冲突机制</CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup class="gap-5">
            <Field>
              <FieldLabel for="transfer-path">目标源路径</FieldLabel>
              <Input id="transfer-path" v-model="selectedPath" placeholder="左侧选择或输入绝对路径" />
            </Field>

            <Field>
              <FieldLabel for="transfer-mode">转移方式</FieldLabel>
              <Select v-model="transferMode">
                <SelectTrigger id="transfer-mode" class="w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="copy">复制 (Copy)</SelectItem>
                    <SelectItem value="move">移动 (Move)</SelectItem>
                    <SelectItem value="link">硬链接 (Link)</SelectItem>
                    <SelectItem value="softlink">软链接 (Softlink)</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>

            <Field>
              <FieldLabel for="transfer-overwrite">冲突解决</FieldLabel>
              <Select v-model="overwriteMode">
                <SelectTrigger id="transfer-overwrite" class="w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="never">从不覆盖</SelectItem>
                    <SelectItem value="always">总是覆盖</SelectItem>
                    <SelectItem value="size">大文件覆盖小文件</SelectItem>
                    <SelectItem value="latest">仅覆盖同集视频</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>

            <Field>
              <FieldLabel for="transfer-media-type">媒体类型</FieldLabel>
              <Select :model-value="toSelectValue(mediaType)" @update:model-value="mediaType = fromSelectValue($event)">
                <SelectTrigger id="transfer-media-type" class="w-full"><SelectValue placeholder="自动识别" /></SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem :value="EMPTY_SELECT_VALUE">自动识别</SelectItem>
                    <SelectItem value="movie">电影 (Movie)</SelectItem>
                    <SelectItem value="tv">电视剧 (TV)</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FieldDescription>手动指定可避免自动识别错误。</FieldDescription>
            </Field>

            <Button class="w-full" :disabled="isSubmitting || !selectedPath" @click="triggerTransfer">
              <Spinner v-if="isSubmitting" data-icon="inline-start" />
              <ArrowLeftRightIcon v-else data-icon="inline-start" />
              {{ isSubmitting ? '提交中…' : '提交整理任务' }}
            </Button>
          </FieldGroup>
        </CardContent>
      </Card>

      <Card class="lg:col-span-5">
        <CardHeader>
          <CardTitle>正在整理的队列</CardTitle>
          <CardDescription>后台并发整理队列实时进度</CardDescription>
        </CardHeader>
        <CardContent class="divide-border divide-y p-0">
          <div
            v-for="q in queue"
            :key="q.id"
            class="flex flex-col justify-between gap-4 p-4 md:flex-row md:items-center"
          >
            <div class="min-w-0 flex-1">
              <p class="truncate text-sm font-semibold" :title="q.src_path">{{ q.src_path }}</p>
              <p class="text-muted-foreground text-xs">任务 ID: {{ q.id }} | 创建时间: {{ new Date(q.created_at).toLocaleString() }}</p>
            </div>
            <div class="flex items-center gap-4">
              <Badge :variant="queueTone(q.status).variant" :class="cn(queueTone(q.status).tone, q.status === 'running' && 'animate-pulse')">
                {{ q.status }}
              </Badge>
              <span class="w-16 text-right text-sm font-bold">{{ Number(q.progress).toFixed(2) }}%</span>
            </div>
          </div>

          <Empty v-if="!queue.length" class="border-0 py-10">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <ArrowLeftRightIcon />
              </EmptyMedia>
              <EmptyTitle>队列中暂无待整理任务</EmptyTitle>
              <EmptyDescription>提交任务后会在这里实时显示进度。</EmptyDescription>
            </EmptyHeader>
          </Empty>
        </CardContent>
      </Card>

      <Card class="lg:col-span-5">
        <CardHeader>
          <CardTitle>整理历史记录</CardTitle>
          <CardDescription>最近完成的文件转移日志</CardDescription>
        </CardHeader>
        <CardContent class="p-0">
          <div v-if="history.length" class="divide-border max-h-96 overflow-y-auto divide-y">
            <div v-for="h in history" :key="h.id" class="flex flex-col gap-2 p-4">
              <div class="flex items-center justify-between gap-4">
                <span class="text-muted-foreground text-xs">{{ new Date(h.transferred_at).toLocaleString() }}</span>
                <Badge :variant="h.status === 'success' ? 'secondary' : 'destructive'" :class="h.status === 'success' ? 'text-success' : ''">
                  {{ h.status === 'success' ? '成功' : '失败' }}
                </Badge>
              </div>
              <div class="text-xs"><span class="text-muted-foreground">源：</span> {{ h.src_path }}</div>
              <div class="text-xs"><span class="text-muted-foreground">宿：</span> {{ h.dest_path }}</div>
              <div class="text-muted-foreground flex items-center justify-between pt-1 text-[10px]">
                <span>方式: {{ h.mode }} | 大小: {{ (h.size / 1024 / 1024).toFixed(1) }} MB</span>
                <span class="max-w-[200px] truncate" :title="h.message">{{ h.message }}</span>
              </div>
            </div>
          </div>

          <Empty v-if="!history.length" class="border-0 py-10">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <ArrowLeftRightIcon />
              </EmptyMedia>
              <EmptyTitle>暂无历史整理记录</EmptyTitle>
              <EmptyDescription>整理任务完成后会记录在这里。</EmptyDescription>
            </EmptyHeader>
          </Empty>
        </CardContent>
      </Card>
    </div>
  </div>
</template>
