<script setup lang="ts">
import { ref, onMounted } from 'vue'
import client from '@/api/client'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Save, Loader2, CheckCircle2, Plus, Trash2, Star, Film, Tv } from 'lucide-vue-next'

const settings = ref({
  tmdb_api_key: '',
  tmdb_language: 'zh-CN',
  transfer_mode: 'link',
  overwrite_mode: 'size',
  auto_scrape: true,
})

const cards = ref<any[]>([])
const isLoading = ref(false)
const isSaving = ref(false)
const isCardSaving = ref(false)
const successMsg = ref('')
const cardSuccessMsg = ref('')

const editingCard = ref<any>(null)
const showCardForm = ref(false)

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

const saveSettings = async () => {
  isSaving.value = true
  successMsg.value = ''
  try {
    const res: any = await client.put('/api/v1/settings', settings.value)
    if (res.code === 0) {
      successMsg.value = '配置保存成功！'
      setTimeout(() => { successMsg.value = '' }, 3000)
    }
  } catch (err) {
    console.error('Failed to save settings', err)
  } finally {
    isSaving.value = false
  }
}

const openNewCard = () => {
  editingCard.value = {
    name: '',
    download_path: '',
    archive_path: '',
    media_type: 'movie',
    is_default: false,
  }
  showCardForm.value = true
}

const openEditCard = (card: any) => {
  editingCard.value = { ...card }
  showCardForm.value = true
}

const closeCardForm = () => {
  showCardForm.value = false
  editingCard.value = null
}

const saveCard = async () => {
  if (!editingCard.value.name || !editingCard.value.download_path || !editingCard.value.archive_path) {
    alert('请填写完整信息')
    return
  }
  isCardSaving.value = true
  try {
    let res: any
    if (editingCard.value.id) {
      res = await client.put(`/api/v1/cards/${editingCard.value.id}`, editingCard.value)
    } else {
      res = await client.post('/api/v1/cards', editingCard.value)
    }
    if (res.code === 0) {
      cardSuccessMsg.value = editingCard.value.id ? '卡片更新成功！' : '卡片创建成功！'
      await fetchCards()
      setTimeout(() => { cardSuccessMsg.value = '' }, 3000)
      closeCardForm()
    }
  } catch (err) {
    console.error('Failed to save card', err)
  } finally {
    isCardSaving.value = false
  }
}

const deleteCard = async (id: number) => {
  if (!confirm('确定删除此卡片？')) return
  try {
    await client.delete(`/api/v1/cards/${id}`)
    await fetchCards()
  } catch (err) {
    console.error('Failed to delete card', err)
  }
}

const setDefaultCard = async (id: number) => {
  try {
    await client.put(`/api/v1/cards/${id}/default`)
    await fetchCards()
  } catch (err) {
    console.error('Failed to set default card', err)
  }
}

onMounted(() => {
  fetchSettings()
  fetchCards()
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

    <div v-else class="space-y-6">
      <!-- TMDB settings -->
      <Card class="bg-slate-900 border-slate-800 text-slate-100">
        <CardHeader>
          <CardTitle class="text-amber-500">TMDB API 配置</CardTitle>
          <CardDescription class="text-slate-400">刮削媒体墙海报及 NFO 文件必须配置此项</CardDescription>
        </CardHeader>
        <CardContent class="grid gap-6 md:grid-cols-2">
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

      <!-- Media Cards Section -->
      <div>
        <div class="flex items-center justify-between mb-4">
          <div>
            <h2 class="text-xl font-bold text-slate-100">媒体卡片</h2>
            <p class="text-slate-400 text-sm">配置下载源目录、归档目录和媒体类型，支持多卡片管理</p>
          </div>
          <Button @click="openNewCard" class="bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold flex items-center gap-2">
            <Plus class="h-4 w-4" />
            添加卡片
          </Button>
        </div>

        <div v-if="cardSuccessMsg" class="bg-green-500/10 border border-green-500/20 text-green-400 text-sm px-4 py-3 rounded-lg flex items-center gap-2 mb-4">
          <CheckCircle2 class="h-5 w-5" />
          <span>{{ cardSuccessMsg }}</span>
        </div>

        <!-- Card Form Modal -->
        <Card v-if="showCardForm" class="bg-slate-900 border-amber-500/30 text-slate-100 mb-6">
          <CardHeader>
            <CardTitle class="text-amber-500">{{ editingCard?.id ? '编辑卡片' : '新建卡片' }}</CardTitle>
          </CardHeader>
          <CardContent class="space-y-4">
            <div class="grid gap-4 md:grid-cols-2">
              <div class="space-y-1.5">
                <label class="text-xs font-semibold text-slate-400">卡片名称</label>
                <Input v-model="editingCard.name" placeholder="例如: 电影整理" class="bg-slate-950 border-slate-800 text-slate-100" />
              </div>
              <div class="space-y-1.5">
                <label class="text-xs font-semibold text-slate-400">媒体类型</label>
                <select v-model="editingCard.media_type" class="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm focus:outline-none text-slate-100 focus:border-amber-500">
                  <option value="movie">电影 (Movie)</option>
                  <option value="tv">电视剧 (TV)</option>
                </select>
              </div>
            </div>
            <div class="space-y-1.5">
              <label class="text-xs font-semibold text-slate-400">下载源目录</label>
              <Input v-model="editingCard.download_path" placeholder="例如: /downloads/movies" class="bg-slate-950 border-slate-800 text-slate-100" />
            </div>
            <div class="space-y-1.5">
              <label class="text-xs font-semibold text-slate-400">归档目录</label>
              <Input v-model="editingCard.archive_path" placeholder="例如: /media/Movies" class="bg-slate-950 border-slate-800 text-slate-100" />
            </div>
            <div class="flex items-center gap-2">
              <input type="checkbox" v-model="editingCard.is_default" class="rounded border-slate-700 bg-slate-950 text-amber-500 focus:ring-amber-500" />
              <span class="text-sm text-slate-300">设为默认卡片</span>
            </div>
            <div class="flex gap-3 pt-2">
              <Button @click="saveCard" :disabled="isCardSaving" class="bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold flex items-center gap-2">
                <Loader2 v-if="isCardSaving" class="h-4 w-4 animate-spin" />
                <Save v-else class="h-4 w-4" />
                {{ isCardSaving ? '保存中...' : '保存卡片' }}
              </Button>
              <Button @click="closeCardForm" variant="outline" class="border-slate-700 text-slate-300 hover:bg-slate-800">
                取消
              </Button>
            </div>
          </CardContent>
        </Card>

        <!-- Cards Grid -->
        <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          <Card v-for="card in cards" :key="card.id" :class="[
            'bg-slate-900 border-slate-800 text-slate-100 transition-all hover:border-slate-700',
            card.is_default ? 'ring-1 ring-amber-500/50' : ''
          ]">
            <CardHeader class="pb-3">
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-2">
                  <Film v-if="card.media_type === 'movie'" class="h-5 w-5 text-amber-500" />
                  <Tv v-else class="h-5 w-5 text-emerald-500" />
                  <CardTitle class="text-slate-100 text-base">{{ card.name }}</CardTitle>
                </div>
                <div class="flex items-center gap-1">
                  <button
                    v-if="!card.is_default"
                    @click="setDefaultCard(card.id)"
                    class="p-1.5 rounded hover:bg-slate-800 text-slate-500 hover:text-amber-500 transition-colors"
                    title="设为默认"
                  >
                    <Star class="h-4 w-4" />
                  </button>
                  <Star v-else class="h-4 w-4 text-amber-500 fill-amber-500" />
                  <button
                    @click="openEditCard(card)"
                    class="p-1.5 rounded hover:bg-slate-800 text-slate-500 hover:text-slate-300 transition-colors"
                    title="编辑"
                  >
                    <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
                  </button>
                  <button
                    @click="deleteCard(card.id)"
                    class="p-1.5 rounded hover:bg-slate-800 text-slate-500 hover:text-rose-500 transition-colors"
                    title="删除"
                  >
                    <Trash2 class="h-4 w-4" />
                  </button>
                </div>
              </div>
              <CardDescription class="text-slate-400 text-xs">
                {{ card.media_type === 'movie' ? '电影' : '电视剧' }}
                <span v-if="card.is_default" class="text-amber-500 ml-1">[默认]</span>
              </CardDescription>
            </CardHeader>
            <CardContent class="space-y-2 pt-0">
              <div class="text-xs">
                <span class="text-slate-500">下载源:</span>
                <span class="text-slate-300 ml-1 font-mono">{{ card.download_path }}</span>
              </div>
              <div class="text-xs">
                <span class="text-slate-500">归档到:</span>
                <span class="text-slate-300 ml-1 font-mono">{{ card.archive_path }}</span>
              </div>
            </CardContent>
          </Card>

          <!-- Empty State -->
          <div v-if="cards.length === 0" class="md:col-span-2 lg:col-span-3 text-center py-12 bg-slate-900/50 rounded-lg border border-dashed border-slate-800">
            <p class="text-slate-500 text-sm">暂无媒体卡片，点击上方按钮添加</p>
          </div>
        </div>
      </div>

      <!-- Transfer & Scrape rules -->
      <Card class="bg-slate-900 border-slate-800 text-slate-100">
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
