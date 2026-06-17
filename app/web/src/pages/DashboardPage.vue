<script setup lang="ts">
import { ref, onMounted, watch, nextTick } from 'vue'
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
  CheckCircle2,
  XCircle
} from 'lucide-vue-next'
import { useLogStore } from '@/stores/logStore'

const router = useRouter()
const logStore = useLogStore()

// Stats
const movieCount = ref<string | number>('...')
const tvCount = ref<string | number>('...')
const pendingCount = ref<string | number>('...')
const successRate = ref<string>('...')

// Settings and status
const successMsg = ref('')
const errMsg = ref('')

// Logs console
const logContainer = ref<HTMLElement | null>(null)

const scrollToBottom = () => {
  if (logContainer.value) {
    logContainer.value.scrollTop = logContainer.value.scrollHeight
  }
}

// Watch logs array length and scroll to bottom
watch(() => logStore.logs.length, () => {
  nextTick(scrollToBottom)
})

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

// Actions
const navigateToTransfer = () => {
  router.push('/transfer')
}

onMounted(() => {
  fetchStats()
  // Ensure we scroll to the bottom when entering the page if logs already exist
  nextTick(scrollToBottom)
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

    <!-- Log Panel -->
    <Card class="bg-slate-900 border-slate-800 text-slate-100 w-full">
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
          class="bg-slate-950 p-4 rounded-lg font-mono text-xs text-slate-300 h-96 overflow-y-auto space-y-1.5 border border-slate-800"
        >
          <div v-for="(log, idx) in logStore.logs" :key="idx" class="flex items-start gap-2 select-text leading-relaxed">
            <span class="text-slate-600 shrink-0">{{ log.timestamp }}</span>
            <span :class="[
              'shrink-0 px-1.5 py-0.5 text-[9px] font-bold rounded scale-90 origin-left uppercase',
              log.level === 'INFO' && 'bg-blue-500/10 text-blue-400',
              log.level === 'WARN' && 'bg-amber-500/10 text-amber-400',
              log.level === 'ERROR' && 'bg-rose-500/10 text-rose-400'
            ]">{{ log.level }}</span>
            <span class="break-all whitespace-pre-wrap flex-1">{{ log.message }}</span>
          </div>
          <div v-if="logStore.logs.length === 0" class="text-slate-600 text-center py-20 text-sm">
            正在开启日志通道...
          </div>
        </div>
      </CardContent>
    </Card>
  </div>
</template>
