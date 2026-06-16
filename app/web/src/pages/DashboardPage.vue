<script setup lang="ts">
import { ref } from 'vue'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import {
  Film,
  Sparkles,
  ArrowLeftRight,
  Database,
  Play
} from 'lucide-vue-next'

const stats = ref([
  { title: '电影总数', value: '1,245', icon: Film, color: 'text-amber-500' },
  { title: '电视剧集', value: '3,812', icon: Database, color: 'text-blue-500' },
  { title: '待整理文件', value: '18', icon: ArrowLeftRight, color: 'text-rose-500' },
  { title: '刮削成功率', value: '98.4%', icon: Sparkles, color: 'text-green-500' },
])
</script>

<template>
  <div class="space-y-6">
    <!-- Welcome Header -->
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-3xl font-extrabold tracking-tight text-slate-100">控制面板</h1>
        <p class="text-slate-400 mt-1">欢迎使用 Bujic Movie 媒体管家，轻松完成电影电视剧的自动归档与元数据刮削。</p>
      </div>
      <Button class="bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold flex items-center gap-2">
        <Play class="h-4 w-4" /> 快速开始整理
      </Button>
    </div>

    <!-- Stats Grid -->
    <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
      <Card v-for="stat in stats" :key="stat.title" class="bg-slate-900 border-slate-800 text-slate-100">
        <CardHeader class="flex flex-row items-center justify-between pb-2">
          <CardTitle class="text-sm font-medium text-slate-400">{{ stat.title }}</CardTitle>
          <component :is="stat.icon" :class="['h-5 w-5', stat.color]" />
        </CardHeader>
        <CardContent>
          <div class="text-2xl font-bold">{{ stat.value }}</div>
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
          <Button variant="outline" class="border-slate-800 text-slate-300 hover:bg-slate-800 h-24 flex flex-col items-center justify-center gap-2">
            <ArrowLeftRight class="h-6 w-6 text-amber-500" />
            <span>执行文件整理</span>
          </Button>
          <Button variant="outline" class="border-slate-800 text-slate-300 hover:bg-slate-800 h-24 flex flex-col items-center justify-center gap-2">
            <Sparkles class="h-6 w-6 text-amber-500" />
            <span>执行全量刮削</span>
          </Button>
        </CardContent>
      </Card>

      <!-- Server Logs Card -->
      <Card class="bg-slate-900 border-slate-800 text-slate-100">
        <CardHeader>
          <CardTitle class="text-slate-100">后台运行日志</CardTitle>
          <CardDescription class="text-slate-400">实时流式日志概览</CardDescription>
        </CardHeader>
        <CardContent>
          <div class="bg-slate-950 p-4 rounded-lg font-mono text-xs text-slate-300 h-28 overflow-y-auto space-y-1.5 border border-slate-800">
            <div>[INFO] 2026-06-16 15:24:01 - 成功加载配置文件 config.yaml</div>
            <div>[INFO] 2026-06-16 15:24:02 - 成功初始化 SQLite 数据库</div>
            <div>[INFO] 2026-06-16 15:24:02 - 服务在端口 :8080 启动运行...</div>
            <div class="text-amber-500">[WARN] 2026-06-16 15:24:10 - TMDB API Key 未配置，刮削任务已挂起</div>
          </div>
        </CardContent>
      </Card>
    </div>
  </div>
</template>
