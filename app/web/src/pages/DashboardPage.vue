<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import client from '@/api/client'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { useRouter } from 'vue-router'
import {
  Film,
  Sparkles,
  ArrowLeftRight,
  Database,
  Play,
  Terminal,
  Loader2,
  CheckCircle2,
  XCircle
} from 'lucide-vue-next'

const router = useRouter()

// Stats
const movieCount = ref<string | number>('...')
const tvCount = ref<string | number>('...')
const pendingCount = ref<string | number>('...')
const successRate = ref<string>('...')

// Settings and status
const settings = ref<any>(null)
const isTransferring = ref(false)
const isScraping = ref(false)
const successMsg = ref('')
const errMsg = ref('')

// Logs console
const logs = ref<any[]>([])
const logContainer = ref<HTMLElement | null>(null)
let socket: WebSocket | null = null

const scrollToBottom = () => {
  if (logContainer.value) {
    logContainer.value.scrollTop = logContainer.value.scrollHeight
  }
}

// Fetch stats and configuration
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
    console.error('Failed to fetch stats', err)
  }
}

const fetchSettings = async () => {
  try {
    const res: any = await client.get('/api/v1/settings')
    if (res.code === 0 && res.data) {
      settings.value = res.data
    }
  } catch (err) {
    console.error('Failed to fetch settings', err)
  }
}

// Actions
const triggerTransfer = async () => {
  isTransferring.value = true
  try {
    const res: any = await client.get('/api/v1/cards')
    if (res.code !== 0 || !res.data || res.data.length === 0) {
      showError('请先配置媒体卡片！')
      return
    }
    showSuccess('文件整理任务已提交，正在后台运行！')
    for (const card of res.data) {
      if (card.download_path) {
        await client.post('/api/v1/transfer', {
          path: card.download_path,
          media_type: card.media_type,
          card_id: card.id,
        })
      }
    }
    setTimeout(fetchStats, 2000)
  } catch (err) {
    showError('文件整理任务提交失败')
  } finally {
    isTransferring.value = false
  }
}

const triggerScrape = async () => {
  isScraping.value = true
  try {
    const res: any = await client.get('/api/v1/cards')
    if (res.code !== 0 || !res.data || res.data.length === 0) {
      showError('请先配置媒体卡片！')
      return
    }
    showSuccess('全量刮削任务已提交，正在后台运行！')
    for (const card of res.data) {
      if (card.archive_path) {
        await client.post('/api/v1/scrape', {
          path: card.archive_path,
          overwrite: false,
          media_type: card.media_type,
        })
      }
    }
    setTimeout(fetchStats, 3000)
  } catch (err) {
    showError('全量刮削任务提交失败')
  } finally {
    isScraping.value = false
  }
}

const navigateToTransfer = () => {
  router.push('/transfer')
}

// Toast helpers
const showSuccess = (msg: string) => {
  successMsg.value = msg
  errMsg.value = ''
  setTimeout(() => {
    if (successMsg.value === msg) successMsg.value = ''
  }, 5000)
}

const showError = (msg: string) => {
  errMsg.value = msg
  successMsg.value = ''
  setTimeout(() => {
    if (errMsg.value === msg) errMsg.value = ''
  }, 5000)
}

// Setup WebSocket log stream
const initWebSocket = () => {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const wsUrl = `${protocol}//${window.location.host}/api/v1/ws`
  socket = new WebSocket(wsUrl)

  socket.onopen = () => {
    logs.value.push({
      timestamp: new Date().toLocaleTimeString(),
      level: 'INFO',
      message: 'WebSocket 实时日志通道连接成功。',
    })
    nextTick(scrollToBottom)
  }

  socket.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data)
      if (msg.type === 'log') {
        logs.value.push(msg.payload)
        if (logs.value.length > 250) {
          logs.value.shift()
        }
        nextTick(scrollToBottom)
      }
    } catch (e) {
      console.error('WebSocket parse error', e)
    }
  }

  socket.onerror = () => {
    logs.value.push({
      timestamp: new Date().toLocaleTimeString(),
      level: 'ERROR',
      message: 'WebSocket 连接出现错误。',
    })
    nextTick(scrollToBottom)
  }

  socket.onclose = () => {
    logs.value.push({
      timestamp: new Date().toLocaleTimeString(),
      level: 'WARN',
      message: 'WebSocket 日志通道连接断开，正在尝试重连...',
    })
    nextTick(scrollToBottom)
    setTimeout(initWebSocket, 5000)
  }
}

onMounted(() => {
  fetchStats()
  fetchSettings()
  initWebSocket()
})

onUnmounted(() => {
  if (socket) {
    socket.close()
  }
})
</script>

<template>
  <div class="space-y-6">
    <!-- Welcome Header -->
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-3xl font-extrabold tracking-tight text-slate-100">控制面板</h1>
        <p class="text-slate-400 mt-1">欢迎使用 Bujic Movie 媒体管家，轻松完成电影电视剧的自动归档与元数据刮削。</p>
      </div>
      <Button
        @click="navigateToTransfer"
        class="bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold flex items-center gap-2"
      >
        <Play class="h-4 w-4" /> 快速开始整理
      </Button>
    </div>

    <!-- Success Notification -->
    <div v-if="successMsg" class="bg-green-500/10 border border-green-500/20 text-green-400 text-sm px-4 py-3 rounded-lg flex items-center gap-2 animate-fade-in">
      <CheckCircle2 class="h-5 w-5 shrink-0" />
      <span>{{ successMsg }}</span>
    </div>

    <!-- Error Notification -->
    <div v-if="errMsg" class="bg-rose-500/10 border border-rose-500/20 text-rose-400 text-sm px-4 py-3 rounded-lg flex items-center gap-2 animate-fade-in">
      <XCircle class="h-5 w-5 shrink-0" />
      <span>{{ errMsg }}</span>
    </div>

    <!-- Stats Grid -->
    <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
      <Card class="bg-slate-900 border-slate-800 text-slate-100">
        <CardHeader class="flex flex-row items-center justify-between pb-2">
          <CardTitle class="text-sm font-medium text-slate-400">电影总数</CardTitle>
          <Film class="h-5 w-5 text-amber-500" />
        </CardHeader>
        <CardContent>
          <div class="text-2xl font-bold">{{ movieCount }}</div>
        </CardContent>
      </Card>
      <Card class="bg-slate-900 border-slate-800 text-slate-100">
        <CardHeader class="flex flex-row items-center justify-between pb-2">
          <CardTitle class="text-sm font-medium text-slate-400">电视剧集</CardTitle>
          <Database class="h-5 w-5 text-blue-500" />
        </CardHeader>
        <CardContent>
          <div class="text-2xl font-bold">{{ tvCount }}</div>
        </CardContent>
      </Card>
      <Card class="bg-slate-900 border-slate-800 text-slate-100">
        <CardHeader class="flex flex-row items-center justify-between pb-2">
          <CardTitle class="text-sm font-medium text-slate-400">待整理文件</CardTitle>
          <ArrowLeftRight class="h-5 w-5 text-rose-500" />
        </CardHeader>
        <CardContent>
          <div class="text-2xl font-bold">{{ pendingCount }}</div>
        </CardContent>
      </Card>
      <Card class="bg-slate-900 border-slate-800 text-slate-100">
        <CardHeader class="flex flex-row items-center justify-between pb-2">
          <CardTitle class="text-sm font-medium text-slate-400">整理成功率</CardTitle>
          <Sparkles class="h-5 w-5 text-green-500" />
        </CardHeader>
        <CardContent>
          <div class="text-2xl font-bold">{{ successRate }}</div>
        </CardContent>
      </Card>
    </div>

    <!-- Quick Actions / Log Panel -->
    <div class="grid gap-6 md:grid-cols-2">
      <!-- Actions Card -->
      <Card class="bg-slate-900 border-slate-800 text-slate-100">
        <CardHeader>
          <CardTitle class="text-amber-500">快捷操作</CardTitle>
          <CardDescription class="text-slate-400">一键触发后台整理与刮削任务</CardDescription>
        </CardHeader>
        <CardContent class="grid grid-cols-2 gap-4">
          <Button
            @click="triggerTransfer"
            :disabled="isTransferring"
            variant="outline"
            class="border-slate-800 text-slate-300 hover:bg-slate-800 h-24 flex flex-col items-center justify-center gap-2"
          >
            <Loader2 v-if="isTransferring" class="h-6 w-6 text-amber-500 animate-spin" />
            <ArrowLeftRight v-else class="h-6 w-6 text-amber-500" />
            <span>执行文件整理</span>
          </Button>
          <Button
            @click="triggerScrape"
            :disabled="isScraping"
            variant="outline"
            class="border-slate-800 text-slate-300 hover:bg-slate-800 h-24 flex flex-col items-center justify-center gap-2"
          >
            <Loader2 v-if="isScraping" class="h-6 w-6 text-amber-500 animate-spin" />
            <Sparkles v-else class="h-6 w-6 text-amber-500" />
            <span>执行全量刮削</span>
          </Button>
        </CardContent>
      </Card>

      <!-- Server Logs Card -->
      <Card class="bg-slate-900 border-slate-800 text-slate-100">
        <CardHeader class="flex flex-row items-center justify-between pb-3">
          <div>
            <CardTitle class="text-slate-100 flex items-center gap-2">
              <Terminal class="h-5 w-5 text-amber-500" />
              <span>后台运行日志</span>
            </CardTitle>
            <CardDescription class="text-slate-400">实时日志通道概览</CardDescription>
          </div>
        </CardHeader>
        <CardContent>
          <div
            ref="logContainer"
            class="bg-slate-950 p-4 rounded-lg font-mono text-xs text-slate-300 h-28 overflow-y-auto space-y-1.5 border border-slate-800"
          >
            <div v-for="(log, idx) in logs" :key="idx" class="flex items-start gap-2 select-text leading-relaxed">
              <span class="text-slate-600 shrink-0">{{ log.timestamp }}</span>
              <span :class="[
                'shrink-0 px-1.5 py-0.5 text-[9px] font-bold rounded scale-90 origin-left uppercase',
                log.level === 'INFO' && 'bg-blue-500/10 text-blue-400',
                log.level === 'WARN' && 'bg-amber-500/10 text-amber-400',
                log.level === 'ERROR' && 'bg-rose-500/10 text-rose-400'
              ]">{{ log.level }}</span>
              <span class="break-all whitespace-pre-wrap flex-1">{{ log.message }}</span>
            </div>
            <div v-if="logs.length === 0" class="text-slate-600 text-center py-6 text-sm">
              正在开启日志通道...
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  </div>
</template>
