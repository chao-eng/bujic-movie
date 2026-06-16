<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import client from '@/api/client'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Folder, File, ArrowLeft, Loader2, ArrowLeftRight, CheckCircle2 } from 'lucide-vue-next'

const selectedPath = ref('')
const transferMode = ref('link')
const overwriteMode = ref('size')

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
    console.error(err)
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
    console.error(err)
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
    console.error(err)
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
    alert('错误: ' + (err.message || '提交失败'))
  } finally {
    isSubmitting.value = false
  }
}

onMounted(async () => {
  let initialPath = '/'
  try {
    const res: any = await client.get('/api/v1/settings')
    if (res.code === 0 && res.data && res.data.download_path) {
      initialPath = res.data.download_path
    }
  } catch (err) {
    console.error('Failed to get download_path from settings', err)
  }
  fetchFiles(initialPath)
  fetchQueue()
  fetchHistory()

  // Poll queue and history status every 3 seconds for real-time progress feel
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
  <div class="space-y-6">
    <div>
      <h1 class="text-3xl font-extrabold tracking-tight text-slate-100">文件整理</h1>
      <p class="text-slate-400 mt-1">自动识别下载完成的影片并重命名，按电影或电视剧季/集归档整理。</p>
    </div>

    <!-- Success Notification -->
    <div v-if="successMsg" class="bg-green-500/10 border border-green-500/20 text-green-400 text-sm px-4 py-3 rounded-lg flex items-center gap-2">
      <CheckCircle2 class="h-5 w-5" />
      <span>{{ successMsg }}</span>
    </div>

    <div class="grid gap-6 lg:grid-cols-3">
      <!-- File browser card -->
      <Card class="bg-slate-900 border-slate-800 text-slate-100 lg:col-span-2">
        <CardHeader class="pb-3 flex flex-row items-center justify-between border-b border-slate-800">
          <div>
            <CardTitle class="text-amber-500">文件浏览器</CardTitle>
            <CardDescription class="text-slate-400">选择待整理的源文件或文件夹</CardDescription>
          </div>
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
          <div class="bg-slate-950/50 px-4 py-2 border-b border-slate-800 text-xs font-mono text-slate-400 truncate">
            当前目录: {{ currentDir }}
          </div>

          <div v-if="isLoadingFiles" class="flex items-center justify-center py-20">
            <Loader2 class="h-8 w-8 text-amber-500 animate-spin" />
          </div>

          <div v-else class="max-h-80 overflow-y-auto divide-y divide-slate-800">
            <div
              v-for="item in fileItems"
              :key="item.path"
              @click="handleItemClick(item)"
              @dblclick="handleItemDoubleClick(item)"
              :class="[
                'flex items-center justify-between px-4 py-2.5 cursor-pointer transition-colors text-sm',
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
            <div v-if="fileItems.length === 0" class="text-center py-20 text-slate-500 text-sm">
              当前文件夹下无内容
            </div>
          </div>
        </CardContent>
      </Card>

      <!-- Settings Panel -->
      <Card class="bg-slate-900 border-slate-800 text-slate-100">
        <CardHeader>
          <CardTitle class="text-amber-500">整理选项</CardTitle>
          <CardDescription class="text-slate-400">设置转移规则与冲突机制</CardDescription>
        </CardHeader>
        <CardContent class="space-y-4">
          <div class="space-y-1.5">
            <label class="text-xs font-semibold text-slate-400">目标源路径</label>
            <Input v-model="selectedPath" placeholder="左侧选择或输入绝对路径" class="bg-slate-950 border-slate-800 text-slate-100" />
          </div>
          <div class="space-y-1.5">
            <label class="text-xs font-semibold text-slate-400">转移方式</label>
            <select v-model="transferMode" class="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm focus:outline-none text-slate-100 focus:border-amber-500">
              <option value="copy">复制 (Copy)</option>
              <option value="move">移动 (Move)</option>
              <option value="link">硬链接 (Link)</option>
              <option value="softlink">软链接 (Softlink)</option>
            </select>
          </div>
          <div class="space-y-1.5">
            <label class="text-xs font-semibold text-slate-400">冲突解决</label>
            <select v-model="overwriteMode" class="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm focus:outline-none text-slate-100 focus:border-amber-500">
              <option value="never">从不覆盖</option>
              <option value="always">总是覆盖</option>
              <option value="size">大文件覆盖小文件</option>
              <option value="latest">仅覆盖同集视频</option>
            </select>
          </div>
          <Button
            @click="triggerTransfer"
            :disabled="isSubmitting || !selectedPath"
            class="w-full bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold py-2.5 flex items-center justify-center gap-2 mt-4"
          >
            <Loader2 v-if="isSubmitting" class="h-4 w-4 animate-spin" />
            <ArrowLeftRight v-else class="h-4 w-4" />
            <span>{{ isSubmitting ? '提交中...' : '提交整理任务' }}</span>
          </Button>
        </CardContent>
      </Card>

      <!-- Active Queue List -->
      <Card class="bg-slate-900 border-slate-800 text-slate-100 lg:col-span-3">
        <CardHeader>
          <CardTitle class="text-amber-500">正在整理的队列</CardTitle>
          <CardDescription class="text-slate-400">后台并发整理队列实时进度</CardDescription>
        </CardHeader>
        <CardContent class="p-0 border-t border-slate-800">
          <div v-if="queue.length > 0" class="divide-y divide-slate-800">
            <div v-for="q in queue" :key="q.id" class="p-4 flex flex-col md:flex-row md:items-center justify-between gap-4">
              <div class="space-y-1 min-w-0 flex-1">
                <div class="text-sm font-semibold text-slate-100 truncate" :title="q.src_path">{{ q.src_path }}</div>
                <div class="text-xs text-slate-500">任务 ID: {{ q.id }} | 创建时间: {{ new Date(q.created_at).toLocaleString() }}</div>
              </div>
              <div class="flex items-center gap-4">
                <span :class="[
                  'px-2.5 py-0.5 rounded text-xs font-semibold uppercase tracking-wider',
                  q.status === 'queued' && 'bg-slate-800 text-slate-400',
                  q.status === 'running' && 'bg-amber-500/10 text-amber-500 animate-pulse',
                  q.status === 'success' && 'bg-green-500/10 text-green-500',
                  q.status === 'failed' && 'bg-rose-500/10 text-rose-500'
                ]">{{ q.status }}</span>
                <span class="text-sm font-bold text-slate-300 w-12 text-right">{{ q.progress }}%</span>
              </div>
            </div>
          </div>
          <div v-else class="text-center py-10 text-slate-500 text-sm">
            队列中暂无待整理任务
          </div>
        </CardContent>
      </Card>

      <!-- History logs -->
      <Card class="bg-slate-900 border-slate-800 text-slate-100 lg:col-span-3">
        <CardHeader>
          <CardTitle class="text-amber-500">整理历史记录</CardTitle>
          <CardDescription class="text-slate-400">最近完成的文件转移日志</CardDescription>
        </CardHeader>
        <CardContent class="p-0 border-t border-slate-800">
          <div v-if="history.length > 0" class="divide-y divide-slate-800 max-h-96 overflow-y-auto">
            <div v-for="h in history" :key="h.id" class="p-4 space-y-2">
              <div class="flex items-center justify-between gap-4">
                <span class="text-xs text-slate-500">{{ new Date(h.transferred_at).toLocaleString() }}</span>
                <span :class="[
                  'px-2 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider',
                  h.status === 'success' ? 'bg-green-500/10 text-green-500' : 'bg-rose-500/10 text-rose-500'
                ]">{{ h.status === 'success' ? '成功' : '失败' }}</span>
              </div>
              <div class="text-xs text-slate-300"><span class="text-slate-500">源:</span> {{ h.src_path }}</div>
              <div class="text-xs text-slate-300"><span class="text-slate-500">宿:</span> {{ h.dest_path }}</div>
              <div class="flex justify-between items-center text-[10px] text-slate-500 pt-1">
                <span>方式: {{ h.mode }} | 大小: {{ (h.size / 1024 / 1024).toFixed(1) }} MB</span>
                <span class="truncate max-w-[200px]" :title="h.message">{{ h.message }}</span>
              </div>
            </div>
          </div>
          <div v-else class="text-center py-10 text-slate-500 text-sm">
            暂无历史整理记录
          </div>
        </CardContent>
      </Card>
    </div>
  </div>
</template>
