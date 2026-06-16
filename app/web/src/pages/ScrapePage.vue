<script setup lang="ts">
import { ref, onMounted } from 'vue'
import client from '@/api/client'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Folder, File, ArrowLeft, Loader2, Sparkles, CheckCircle2, Film, Tv } from 'lucide-vue-next'

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
    console.error('Failed to load directories', err)
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
    console.error('Failed to trigger scrape', err)
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
    console.error('Failed to load cards', err)
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
    // Fallback to old settings
    try {
      const res: any = await client.get('/api/v1/settings')
      if (res.code === 0 && res.data && res.data.download_path) {
        initialPath = res.data.download_path
      }
    } catch (err) {
      console.error('Failed to get download_path from settings', err)
    }
  }
  if (initialPath && initialPath !== '/') {
    fetchFiles(initialPath)
  }
})
</script>

<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-3xl font-extrabold tracking-tight text-slate-100">智能刮削</h1>
      <p class="text-slate-400 mt-1">手动选择服务器上的视频文件或目录，从 TMDB 刮削元数据及海报墙。</p>
    </div>

    <!-- Success Notification -->
    <div v-if="successMsg" class="bg-green-500/10 border border-green-500/20 text-green-400 text-sm px-4 py-3 rounded-lg flex items-center gap-2">
      <CheckCircle2 class="h-5 w-5" />
      <span>{{ successMsg }}</span>
    </div>

    <!-- Card Selector -->
    <div v-if="cards.length > 0" class="flex gap-3 overflow-x-auto pb-2">
      <button
        v-for="card in cards"
        :key="card.id"
        @click="selectCard(card)"
        :class="[
          'flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-all whitespace-nowrap',
          selectedCardId === card.id
            ? 'bg-amber-500 text-slate-950'
            : 'bg-slate-800 text-slate-300 hover:bg-slate-700'
        ]"
      >
        <Film v-if="card.media_type === 'movie'" class="h-4 w-4" />
        <Tv v-else class="h-4 w-4" />
        {{ card.name }}
        <span v-if="card.is_default" class="text-[10px] opacity-70">[默认]</span>
      </button>
    </div>

    <div class="grid gap-6 md:grid-cols-3">
      <!-- File Selector Tree -->
      <Card class="bg-slate-900 border-slate-800 text-slate-100 md:col-span-2">
        <CardHeader class="pb-3 flex flex-row items-center justify-between border-b border-slate-800">
          <div>
            <CardTitle class="text-amber-500">文件浏览器</CardTitle>
            <CardDescription class="text-slate-400">选择待刮削的目标路径</CardDescription>
          </div>
          <!-- Go Back button -->
          <Button
            @click="navigateUp"
            :disabled="currentDir === '/' || currentDir === ''"
            variant="outline"
            class="border-slate-800 text-slate-300 hover:bg-slate-800 flex items-center gap-2 h-9"
          >
            <ArrowLeft class="h-4 w-4" /> 返回上级
          </Button>
        </CardHeader>
        <CardContent class="p-0">
          <!-- Current Directory Path -->
          <div class="bg-slate-950/50 px-4 py-2 border-b border-slate-800 text-xs font-mono text-slate-400 truncate">
            当前路径: {{ currentDir }}
          </div>

          <div v-if="isLoadingFiles" class="flex items-center justify-center py-20">
            <Loader2 class="h-8 w-8 text-amber-500 animate-spin" />
          </div>

          <!-- Files List -->
          <div v-else class="max-h-96 overflow-y-auto divide-y divide-slate-800">
            <div
              v-for="item in fileItems"
              :key="item.path"
              @click="handleItemClick(item)"
              @dblclick="handleItemDoubleClick(item)"
              :class="[
                'flex items-center justify-between px-4 py-3 cursor-pointer transition-colors text-sm',
                selectedPath === item.path ? 'bg-amber-500/10 text-amber-500' : 'hover:bg-slate-800/30'
              ]"
            >
              <div class="flex items-center gap-3 truncate">
                <Folder v-if="item.is_dir" class="h-5 w-5 text-amber-500 shrink-0" />
                <File v-else class="h-5 w-5 text-slate-400 shrink-0" />
                <span class="truncate">{{ item.name }}</span>
              </div>
              <span class="text-xs text-slate-500">{{ item.is_dir ? '文件夹' : (item.size / 1024 / 1024).toFixed(1) + ' MB' }}</span>
            </div>

            <!-- Empty Directory -->
            <div v-if="fileItems.length === 0" class="text-center py-20 text-slate-500 text-sm">
              当前目录下无子文件夹或文件
            </div>
          </div>
        </CardContent>
      </Card>

      <!-- Scraping Control Panel -->
      <Card class="bg-slate-900 border-slate-800 text-slate-100">
        <CardHeader>
          <CardTitle class="text-amber-500">刮削设置</CardTitle>
          <CardDescription class="text-slate-400">配置刮削行为参数</CardDescription>
        </CardHeader>
        <CardContent class="space-y-6">
          <!-- Path Input -->
          <div class="space-y-1.5">
            <label class="text-xs font-semibold text-slate-400">选中的目标路径</label>
            <Input
              v-model="selectedPath"
              placeholder="在左侧浏览器选择或在此处填入路径"
              class="bg-slate-950 border-slate-800 text-slate-100"
            />
          </div>

          <!-- Options -->
          <div class="space-y-4">
            <div class="space-y-1.5">
              <label class="text-xs font-semibold text-slate-400">媒体类型</label>
              <select v-model="mediaType" class="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm focus:outline-none text-slate-100 focus:border-amber-500">
                <option value="">自动识别</option>
                <option value="movie">电影 (Movie)</option>
                <option value="tv">电视剧 (TV)</option>
              </select>
              <p class="text-[10px] text-slate-500">手动指定可避免自动识别错误</p>
            </div>
            <label class="relative inline-flex items-center cursor-pointer">
              <input type="checkbox" v-model="overwrite" class="sr-only peer" />
              <div class="w-11 h-6 bg-slate-800 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-slate-400 after:border-slate-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-amber-500 peer-checked:after:bg-slate-950"></div>
              <span class="ml-3 text-sm font-medium text-slate-300">覆盖已存在的元数据</span>
            </label>
          </div>

          <!-- Submit Action -->
          <Button
            @click="triggerScrape"
            :disabled="isScraping || !selectedPath"
            class="w-full bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold py-2.5 flex items-center justify-center gap-2 transition-all"
          >
            <Loader2 v-if="isScraping" class="h-4 w-4 animate-spin" />
            <Sparkles v-else class="h-4 w-4" />
            <span>{{ isScraping ? '提交中...' : '提交刮削任务' }}</span>
          </Button>
        </CardContent>
      </Card>
    </div>
  </div>
</template>
