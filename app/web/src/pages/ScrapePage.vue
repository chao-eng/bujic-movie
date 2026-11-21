<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { CheckCircle2Icon, FilmIcon, SparklesIcon, TvIcon } from 'lucide-vue-next'
import client from '@/api/client'
import { EMPTY_SELECT_VALUE, fromSelectValue, toSelectValue } from '@/lib/utils'
import FileBrowserCard from '@/components/common/FileBrowserCard.vue'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'

const selectedPath = ref('')
const overwrite = ref(false)
const mediaType = ref('')

const cards = ref<any[]>([])
const selectedCardId = ref<number | null>(null)

const currentDir = ref('/')
const fileItems = ref<any[]>([])
const isLoadingFiles = ref(false)
const isScraping = ref(false)
const successMsg = ref('')

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

const triggerScrape = async () => {
  if (!selectedPath.value) return

  isScraping.value = true
  successMsg.value = ''

  try {
    const res: any = await client.post('/api/v1/scrape', {
      path: selectedPath.value,
      overwrite: overwrite.value,
      media_type: mediaType.value,
    })

    if (res.code === 0) {
      successMsg.value = '刮削任务提交成功！可在控制台或 WebSocket 中查看详细进度。'
      setTimeout(() => {
        successMsg.value = ''
      }, 5000)
    }
  } catch (err) {
    console.error('提交刮削失败', err)
  } finally {
    isScraping.value = false
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
  fetchFiles(card.archive_path)
}

onMounted(async () => {
  await fetchCards()

  let initialPath = '/'
  if (cards.value.length > 0) {
    const defaultCard = cards.value.find((c: any) => c.is_default) || cards.value[0]
    selectedCardId.value = defaultCard.id
    mediaType.value = defaultCard.media_type || ''
    initialPath = defaultCard.archive_path || '/'
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
})
</script>

<template>
  <div class="flex flex-col gap-6">
    <div>
      <h1 class="text-2xl font-bold tracking-tight">智能刮削</h1>
      <p class="text-muted-foreground mt-1 text-sm">手动选择服务器上的视频文件或目录，从 TMDB 刮削元数据及海报墙。</p>
    </div>

    <Alert v-if="successMsg" class="text-success">
      <CheckCircle2Icon />
      <AlertTitle>刮削任务提交成功</AlertTitle>
      <AlertDescription>可在控制台或 WebSocket 中查看详细进度。</AlertDescription>
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

    <div class="grid gap-6 md:grid-cols-5">
      <FileBrowserCard
        class="md:col-span-3"
        title="文件浏览器"
        description="选择待刮削的目标路径"
        empty-title="当前目录下无子文件夹或文件"
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
          <CardTitle>刮削设置</CardTitle>
          <CardDescription>配置刮削行为参数</CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup class="gap-5">
            <Field>
              <FieldLabel for="scrape-path">选中的目标路径</FieldLabel>
              <Input id="scrape-path" v-model="selectedPath" placeholder="在左侧浏览器选择或在此处填入路径" />
            </Field>

            <Field>
              <FieldLabel for="scrape-media-type">媒体类型</FieldLabel>
              <Select :model-value="toSelectValue(mediaType)" @update:model-value="mediaType = fromSelectValue($event)">
                <SelectTrigger id="scrape-media-type" class="w-full"><SelectValue placeholder="自动识别" /></SelectTrigger>
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

            <Field orientation="horizontal">
              <Switch id="scrape-overwrite" v-model="overwrite" />
              <FieldLabel for="scrape-overwrite">覆盖已存在的元数据</FieldLabel>
            </Field>

            <Button class="w-full" :disabled="isScraping || !selectedPath" @click="triggerScrape">
              <Spinner v-if="isScraping" data-icon="inline-start" />
              <SparklesIcon v-else data-icon="inline-start" />
              {{ isScraping ? '提交中…' : '提交刮削任务' }}
            </Button>
          </FieldGroup>
        </CardContent>
      </Card>
    </div>
  </div>
</template>
