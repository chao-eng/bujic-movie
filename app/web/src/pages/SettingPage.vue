<script setup lang="ts">
import { ref, onMounted } from 'vue'
import client from '@/api/client'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Save, Loader2, CheckCircle2 } from 'lucide-vue-next'

const settings = ref({
  tmdb_api_key: '',
  tmdb_language: 'zh-CN',
  movie_path: '',
  tv_path: '',
  download_path: '',
  transfer_mode: 'link',
  overwrite_mode: 'size',
  auto_scrape: true,
})

const isLoading = ref(false)
const isSaving = ref(false)
const successMsg = ref('')

const fetchSettings = async () => {
  isLoading.value = true
  try {
    const res: any = await client.get('/api/v1/settings')
    if (res.code === 0) {
      settings.value = { ...settings.value, ...res.data }
    }
  } catch (err) {
    console.error('Failed to load settings', err)
  } finally {
    isLoading.value = false
  }
}

const saveSettings = async () => {
  isSaving.value = true
  successMsg.value = ''
  try {
    const res: any = await client.put('/api/v1/settings', settings.value)
    if (res.code === 0) {
      successMsg.value = '配置保存成功！'
      setTimeout(() => {
        successMsg.value = ''
      }, 3000)
    }
  } catch (err) {
    console.error('Failed to save settings', err)
  } finally {
    isSaving.value = false
  }
}

onMounted(() => {
  fetchSettings()
})
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-3xl font-extrabold tracking-tight text-slate-100">系统设置</h1>
        <p class="text-slate-400 mt-1">配置刮削与文件整理参数，并持久化存储在服务器。</p>
      </div>
      <Button @click="saveSettings" :disabled="isSaving" class="bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold flex items-center gap-2">
        <Loader2 v-if="isSaving" class="h-4 w-4 animate-spin" />
        <Save v-else class="h-4 w-4" />
        保存设置
      </Button>
    </div>

    <!-- Success Message Alert -->
    <div v-if="successMsg" class="bg-green-500/10 border border-green-500/20 text-green-400 text-sm px-4 py-3 rounded-lg flex items-center gap-2">
      <CheckCircle2 class="h-5 w-5" />
      <span>{{ successMsg }}</span>
    </div>

    <div v-if="isLoading" class="flex items-center justify-center py-20">
      <Loader2 class="h-8 w-8 text-amber-500 animate-spin" />
    </div>

    <div v-else class="grid gap-6 md:grid-cols-2">
      <!-- TMDB settings -->
      <Card class="bg-slate-900 border-slate-800 text-slate-100">
        <CardHeader>
          <CardTitle class="text-amber-500">TMDB API 配置</CardTitle>
          <CardDescription class="text-slate-400">刮削媒体墙海报及 NFO 文件必须配置此项</CardDescription>
        </CardHeader>
        <CardContent class="space-y-4">
          <div class="space-y-1.5">
            <label class="text-xs font-semibold text-slate-400">TMDB API Key</label>
            <Input v-model="settings.tmdb_api_key" type="password" placeholder="填入您的 TMDB API Key" class="bg-slate-950 border-slate-800 text-slate-100" />
          </div>
          <div class="space-y-1.5">
            <label class="text-xs font-semibold text-slate-400">刮削首选语言</label>
            <select v-model="settings.tmdb_language" class="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm focus:outline-none text-slate-100 focus:border-amber-500">
              <option value="zh-CN">简体中文 (zh-CN)</option>
              <option value="zh-TW">繁体中文 (zh-TW)</option>
              <option value="en-US">英文 (en-US)</option>
            </select>
          </div>
        </CardContent>
      </Card>

      <!-- Media Folders settings -->
      <Card class="bg-slate-900 border-slate-800 text-slate-100">
        <CardHeader>
          <CardTitle class="text-amber-500">文件目录配置</CardTitle>
          <CardDescription class="text-slate-400">定义服务器下载与媒体存储的路径</CardDescription>
        </CardHeader>
        <CardContent class="space-y-4">
          <div class="space-y-1.5">
            <label class="text-xs font-semibold text-slate-400">下载源目录 (Downloads)</label>
            <Input v-model="settings.download_path" placeholder="例如: /downloads" class="bg-slate-950 border-slate-800 text-slate-100" />
          </div>
          <div class="space-y-1.5">
            <label class="text-xs font-semibold text-slate-400">电影归档目录</label>
            <Input v-model="settings.movie_path" placeholder="例如: /media/Movies" class="bg-slate-950 border-slate-800 text-slate-100" />
          </div>
          <div class="space-y-1.5">
            <label class="text-xs font-semibold text-slate-400">电视剧归档目录</label>
            <Input v-model="settings.tv_path" placeholder="例如: /media/TV" class="bg-slate-950 border-slate-800 text-slate-100" />
          </div>
        </CardContent>
      </Card>

      <!-- Transfer & Scrape rules -->
      <Card class="bg-slate-900 border-slate-800 text-slate-100 md:col-span-2">
        <CardHeader>
          <CardTitle class="text-amber-500">整理与刮削规则</CardTitle>
          <CardDescription class="text-slate-400">调整文件转移策略与覆盖机制</CardDescription>
        </CardHeader>
        <CardContent class="grid gap-6 md:grid-cols-3">
          <div class="space-y-1.5">
            <label class="text-xs font-semibold text-slate-400">转移模式</label>
            <select v-model="settings.transfer_mode" class="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm focus:outline-none text-slate-100 focus:border-amber-500">
              <option value="copy">复制 (Copy) — 占双倍硬盘空间</option>
              <option value="move">移动 (Move) — 源文件将被清除</option>
              <option value="link">硬链接 (Link) — 推荐，不占空间可双向删除</option>
              <option value="softlink">软链接 (Softlink) — 快捷方式</option>
            </select>
          </div>
          <div class="space-y-1.5">
            <label class="text-xs font-semibold text-slate-400">覆盖冲突模式</label>
            <select v-model="settings.overwrite_mode" class="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm focus:outline-none text-slate-100 focus:border-amber-500">
              <option value="never">从不覆盖 (Never)</option>
              <option value="always">总是覆盖 (Always)</option>
              <option value="size">大文件覆盖小文件 (Size) — 推荐</option>
              <option value="latest">仅覆盖同季同集视频 (Latest)</option>
            </select>
          </div>
          <div class="space-y-1.5">
            <label class="text-xs font-semibold text-slate-400">自动刮削</label>
            <div class="flex items-center h-10">
              <label class="relative inline-flex items-center cursor-pointer">
                <input type="checkbox" v-model="settings.auto_scrape" class="sr-only peer" />
                <div class="w-11 h-6 bg-slate-800 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-slate-400 after:border-slate-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-amber-500 peer-checked:after:bg-slate-950"></div>
                <span class="ml-3 text-sm font-medium text-slate-300">整理后自动触发刮削</span>
              </label>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  </div>
</template>
