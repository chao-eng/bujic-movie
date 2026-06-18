<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch } from 'vue'
import client from '@/api/client'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Save, Loader2, CheckCircle2, Plus, Trash2, Star, Film, Tv, KeyRound, Server, Plug, RefreshCw, SlidersHorizontal, FolderTree, ShieldCheck, Clapperboard } from 'lucide-vue-next'
import { useConfirm } from '@/composables/useConfirm'
import { toast } from 'vue-sonner'
import { encryptAESGCM } from '@/lib/crypto'

const { confirm } = useConfirm()

type TabKey = 'general' | 'directories' | 'servers' | 'security'
const activeTab = ref<TabKey>('general')
const tabs: { key: TabKey; label: string; icon: any }[] = [
  { key: 'general', label: '常规', icon: SlidersHorizontal },
  { key: 'directories', label: '目录管理', icon: FolderTree },
  { key: 'servers', label: '媒体服务器', icon: Server },
  { key: 'security', label: '安全', icon: ShieldCheck },
]

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

// Media libraries (Emby / Jellyfin / Plex) state
const libraries = ref<any[]>([])
const editingLibrary = ref<any>(null)
const showLibraryForm = ref(false)
const isLibrarySaving = ref(false)
const librarySuccessMsg = ref('')
const testingLibraryId = ref<number | null>(null)
const refreshingLibraryId = ref<number | null>(null)
// Heartbeat online status keyed by library id
const statusMap = ref<Record<number, boolean>>({})
let statusTimer: ReturnType<typeof setInterval> | null = null
// Libraries probed from the server while editing (for the library dropdown)
const probeLibraries = ref<any[]>([])
const probingLibraries = ref(false)
let probeTimer: ReturnType<typeof setTimeout> | null = null

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
    watch_directory: false,
    media_library_id: 0,
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
    toast.warning('请填写完整信息')
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
      cardSuccessMsg.value = editingCard.value.id ? '目录更新成功！' : '目录创建成功！'
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
  if (!await confirm('确定删除此目录？')) return
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

// ===== Media Libraries =====
const fetchLibraries = async () => {
  try {
    const res: any = await client.get('/api/v1/libraries')
    if (res.code === 0) {
      libraries.value = res.data || []
    }
  } catch (err) {
    console.error('Failed to load libraries', err)
  }
}

const openNewLibrary = () => {
  editingLibrary.value = { name: '', type: 'emby', url: '', api_key: '', enabled: true, library_id: '', library_name: '' }
  probeLibraries.value = []
  showLibraryForm.value = true
}

const openEditLibrary = (lib: any) => {
  editingLibrary.value = { library_id: '', library_name: '', ...lib }
  probeLibraries.value = []
  showLibraryForm.value = true
  // 编辑时若已有地址与密钥，尝试自动加载媒体库列表
  triggerProbe()
}

const closeLibraryForm = () => {
  showLibraryForm.value = false
  editingLibrary.value = null
  probeLibraries.value = []
}

const saveLibrary = async () => {
  if (!editingLibrary.value.name || !editingLibrary.value.url) {
    toast.warning('请填写媒体服务器名称和地址')
    return
  }
  isLibrarySaving.value = true
  try {
    let res: any
    if (editingLibrary.value.id) {
      res = await client.put(`/api/v1/libraries/${editingLibrary.value.id}`, editingLibrary.value)
    } else {
      res = await client.post('/api/v1/libraries', editingLibrary.value)
    }
    if (res.code === 0) {
      librarySuccessMsg.value = editingLibrary.value.id ? '媒体服务器更新成功！' : '媒体服务器创建成功！'
      await fetchLibraries()
      fetchStatuses()
      setTimeout(() => { librarySuccessMsg.value = '' }, 3000)
      closeLibraryForm()
    }
  } catch (err) {
    console.error('Failed to save library', err)
  } finally {
    isLibrarySaving.value = false
  }
}

const deleteLibrary = async (id: number) => {
  if (!await confirm('确定删除此媒体服务器？绑定到它的目录将不再触发刷新。')) return
  try {
    await client.delete(`/api/v1/libraries/${id}`)
    await fetchLibraries()
  } catch (err) {
    console.error('Failed to delete library', err)
  }
}

const testLibrary = async (id: number) => {
  testingLibraryId.value = id
  try {
    const res: any = await client.post(`/api/v1/libraries/${id}/test`)
    if (res.code === 0) {
      toast.success('连接成功')
    } else {
      toast.error(res.msg || '连接失败')
    }
  } catch (err: any) {
    toast.error(err.response?.data?.msg || '连接失败')
  } finally {
    testingLibraryId.value = null
  }
}

const refreshLibrary = async (id: number) => {
  refreshingLibraryId.value = id
  try {
    const res: any = await client.post(`/api/v1/libraries/${id}/refresh`)
    if (res.code === 0) {
      toast.success('已通知媒体服务器刷新')
    } else {
      toast.error(res.msg || '刷新失败')
    }
  } catch (err: any) {
    toast.error(err.response?.data?.msg || '刷新失败')
  } finally {
    refreshingLibraryId.value = null
  }
}

const libraryName = (id: number) => libraries.value.find((l) => l.id === id)?.name || '—'

// Heartbeat: poll online status of every media server
const fetchStatuses = async () => {
  try {
    const res: any = await client.get('/api/v1/libraries/status')
    if (res.code === 0) {
      const map: Record<number, boolean> = {}
      for (const s of (res.data || [])) map[s.id] = s.online
      statusMap.value = map
    }
  } catch {
    // ignore heartbeat errors
  }
}

// Probe the server (in the edit form) for its libraries; only succeeds when the
// key is correct, in which case the library dropdown is shown.
const probeServerLibraries = async () => {
  const lib = editingLibrary.value
  if (!lib || !lib.url || !lib.api_key) {
    probeLibraries.value = []
    return
  }
  probingLibraries.value = true
  try {
    const res: any = await client.post('/api/v1/libraries/probe', {
      type: lib.type,
      url: lib.url,
      api_key: lib.api_key,
    })
    probeLibraries.value = res.code === 0 ? (res.data || []) : []
  } catch {
    probeLibraries.value = []
  } finally {
    probingLibraries.value = false
  }
}

const triggerProbe = () => {
  if (probeTimer) clearTimeout(probeTimer)
  probeTimer = setTimeout(probeServerLibraries, 600)
}

const onLibrarySelect = () => {
  const found = probeLibraries.value.find((l) => l.id === editingLibrary.value.library_id)
  editingLibrary.value.library_name = found ? found.name : ''
}

// Re-probe whenever the connection fields change while editing
watch(
  () => [editingLibrary.value?.type, editingLibrary.value?.url, editingLibrary.value?.api_key],
  () => {
    if (showLibraryForm.value && editingLibrary.value) triggerProbe()
  }
)

// Password change state
const currentPassword = ref('')
const newPassword = ref('')
const confirmNewPassword = ref('')
const isChangingPassword = ref(false)

const changePassword = async () => {
  if (!currentPassword.value || !newPassword.value || !confirmNewPassword.value) {
    toast.warning('请填写完整密码信息')
    return
  }
  if (newPassword.value !== confirmNewPassword.value) {
    toast.error('新密码与确认密码不一致')
    return
  }

  isChangingPassword.value = true
  try {
    // 1. Fetch encryption key challenge
    const keyRes: any = await client.get('/api/v1/auth/login-key')
    if (keyRes.code !== 0) {
      throw new Error(keyRes.msg || '无法获取加密密钥')
    }
    const { key_id, key } = keyRes.data

    // 2. Encrypt both passwords with the same challenge key
    const encryptedOld = await encryptAESGCM(currentPassword.value, key)
    const encryptedNew = await encryptAESGCM(newPassword.value, key)

    // 3. Post to password update endpoint
    const res: any = await client.put('/api/v1/settings/password', {
      key_id,
      encrypted_old_password: encryptedOld.ciphertext,
      old_iv: encryptedOld.iv,
      encrypted_new_password: encryptedNew.ciphertext,
      new_iv: encryptedNew.iv,
    })

    if (res.code === 0) {
      toast.success('密码修改成功！请重新登录')
      // Reset inputs
      currentPassword.value = ''
      newPassword.value = ''
      confirmNewPassword.value = ''

      // Auto logout after 2 seconds
      setTimeout(() => {
        localStorage.removeItem('token')
        window.location.href = '/login'
      }, 2000)
    } else {
      toast.error(res.msg || '密码修改失败')
    }
  } catch (err: any) {
    console.error('Failed to change password', err)
    toast.error(err.response?.data?.msg || err.message || '修改密码失败，请检查当前密码是否正确')
  } finally {
    isChangingPassword.value = false
  }
}

onMounted(() => {
  fetchSettings()
  fetchCards()
  fetchLibraries()
  fetchStatuses()
  statusTimer = setInterval(fetchStatuses, 30000)
})

onUnmounted(() => {
  if (statusTimer) clearInterval(statusTimer)
  if (probeTimer) clearTimeout(probeTimer)
})
</script>

<template>
  <div class="space-y-6">
    <!-- Page header -->
    <div>
      <h1 class="text-3xl font-extrabold tracking-tight text-slate-100">系统设置</h1>
      <p class="text-slate-400 mt-1">配置刮削与文件整理参数，并持久化存储在服务器。</p>
    </div>

    <div v-if="isLoading" class="flex items-center justify-center py-20">
      <Loader2 class="h-8 w-8 text-amber-500 animate-spin" />
    </div>

    <template v-else>
      <!-- Tab navigation -->
      <nav class="inline-flex gap-1 p-1 rounded-xl bg-slate-900/70 border border-slate-800 overflow-x-auto max-w-full">
        <button
          v-for="t in tabs"
          :key="t.key"
          type="button"
          @click="activeTab = t.key"
          :class="[
            'flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-semibold whitespace-nowrap transition-all duration-200',
            activeTab === t.key
              ? 'bg-amber-500 text-slate-950 shadow-lg shadow-amber-500/20'
              : 'text-slate-400 hover:text-slate-100 hover:bg-slate-800/60',
          ]"
        >
          <component :is="t.icon" class="h-4 w-4" />
          {{ t.label }}
        </button>
      </nav>

      <!-- Tab panels -->
      <Transition name="panel" mode="out-in">
        <div :key="activeTab" class="space-y-6 stagger">
          <!-- ===================== 常规 ===================== -->
          <template v-if="activeTab === 'general'">
            <div class="flex items-center justify-between gap-4 mb-2">
              <div class="min-w-0">
                <h2 class="text-xl font-bold text-slate-100">常规</h2>
                <p class="text-slate-400 text-sm truncate">TMDB 配置与整理、刮削规则</p>
              </div>
              <Button @click="saveSettings" :disabled="isSaving" class="shrink-0 bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold flex items-center gap-2">
                <Loader2 v-if="isSaving" class="h-4 w-4 animate-spin" />
                <Save v-else class="h-4 w-4" />
                保存设置
              </Button>
            </div>

            <div v-if="successMsg" class="bg-green-500/10 border border-green-500/20 text-green-400 text-sm px-4 py-3 rounded-lg flex items-center gap-2">
              <CheckCircle2 class="h-5 w-5" />
              <span>{{ successMsg }}</span>
            </div>

            <!-- TMDB settings -->
            <Card class="bg-slate-900 border-slate-800 text-slate-100">
              <CardHeader>
                <CardTitle class="text-amber-500 flex items-center gap-2">
                  <Clapperboard class="h-5 w-5" />
                  TMDB API 配置
                </CardTitle>
                <CardDescription class="text-slate-400 flex items-center gap-1.5 flex-wrap">
                  <span>刮削媒体墙海报及 NFO 文件必须配置此项。</span>
                  <a href="https://www.themoviedb.org/settings/api" target="_blank" rel="noopener noreferrer" class="text-amber-500 hover:text-amber-400 hover:underline inline-flex items-center gap-0.5">
                    点此获取 API 密钥
                    <svg class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" /></svg>
                  </a>
                </CardDescription>
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

            <!-- Transfer & Scrape rules -->
            <Card class="bg-slate-900 border-slate-800 text-slate-100">
              <CardHeader>
                <CardTitle class="text-amber-500 flex items-center gap-2">
                  <SlidersHorizontal class="h-5 w-5" />
                  整理与刮削规则
                </CardTitle>
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
          </template>

          <!-- ===================== 目录管理 ===================== -->
          <div v-else-if="activeTab === 'directories'">
            <div class="flex items-center justify-between gap-4 mb-4">
              <div class="min-w-0">
                <h2 class="text-xl font-bold text-slate-100">目录管理</h2>
                <p class="text-slate-400 text-sm truncate">配置下载源目录、归档目录和媒体类型，支持多目录管理</p>
              </div>
              <Button @click="openNewCard" class="shrink-0 bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold flex items-center gap-2">
                <Plus class="h-4 w-4" />
                添加目录
              </Button>
            </div>

            <div v-if="cardSuccessMsg" class="bg-green-500/10 border border-green-500/20 text-green-400 text-sm px-4 py-3 rounded-lg flex items-center gap-2 mb-4">
              <CheckCircle2 class="h-5 w-5" />
              <span>{{ cardSuccessMsg }}</span>
            </div>

            <!-- Card Form -->
            <Card v-if="showCardForm" class="bg-slate-900 border-amber-500/30 text-slate-100 mb-6">
              <CardHeader>
                <CardTitle class="text-amber-500">{{ editingCard?.id ? '编辑目录' : '新建目录' }}</CardTitle>
              </CardHeader>
              <CardContent class="space-y-4">
                <div class="grid gap-4 md:grid-cols-2">
                  <div class="space-y-1.5">
                    <label class="text-xs font-semibold text-slate-400">名称</label>
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
                <div class="space-y-1.5">
                  <label class="text-xs font-semibold text-slate-400">所属媒体服务器 (整理刮削完成后自动刷新)</label>
                  <select v-model.number="editingCard.media_library_id" class="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm focus:outline-none text-slate-100 focus:border-amber-500">
                    <option :value="0">不绑定（不触发刷新）</option>
                    <option v-for="lib in libraries" :key="lib.id" :value="lib.id">{{ lib.name }} ({{ lib.type }})</option>
                  </select>
                </div>
                <div class="flex items-center gap-2">
                  <input type="checkbox" v-model="editingCard.is_default" class="rounded border-slate-700 bg-slate-950 text-amber-500 focus:ring-amber-500" />
                  <span class="text-sm text-slate-300">设为默认卡片</span>
                </div>
                <div class="flex items-center gap-2">
                  <input type="checkbox" v-model="editingCard.watch_directory" class="rounded border-slate-700 bg-slate-950 text-amber-500 focus:ring-amber-500" />
                  <span class="text-sm text-slate-300">开启实时目录监听 (自动整理)</span>
                </div>
                <div class="flex gap-3 pt-2">
                  <Button @click="saveCard" :disabled="isCardSaving" class="bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold flex items-center gap-2">
                    <Loader2 v-if="isCardSaving" class="h-4 w-4 animate-spin" />
                    <Save v-else class="h-4 w-4" />
                    {{ isCardSaving ? '保存中...' : '保存目录' }}
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
                  <div class="text-xs">
                    <span class="text-slate-500">自动整理:</span>
                    <span :class="['ml-1 font-semibold', card.watch_directory ? 'text-amber-500' : 'text-slate-400']">
                      {{ card.watch_directory ? '已开启目录监听' : '已关闭' }}
                    </span>
                  </div>
                  <div class="text-xs" v-if="card.media_library_id">
                    <span class="text-slate-500">媒体服务器:</span>
                    <span class="text-sky-300 ml-1">{{ libraryName(card.media_library_id) }}</span>
                  </div>
                </CardContent>
              </Card>

              <!-- Empty State -->
              <div v-if="cards.length === 0" class="md:col-span-2 lg:col-span-3 text-center py-12 bg-slate-900/50 rounded-lg border border-dashed border-slate-800">
                <p class="text-slate-500 text-sm">暂无目录，点击上方按钮添加</p>
              </div>
            </div>
          </div>

          <!-- ===================== 媒体服务器 ===================== -->
          <div v-else-if="activeTab === 'servers'">
            <div class="flex items-center justify-between gap-4 mb-4">
              <div class="min-w-0">
                <h2 class="text-xl font-bold text-slate-100">媒体服务器</h2>
                <p class="text-slate-400 text-sm truncate">配置 Emby / Jellyfin / Plex，整理刮削完成后自动刷新</p>
              </div>
              <Button @click="openNewLibrary" class="shrink-0 bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold flex items-center gap-2">
                <Plus class="h-4 w-4" />
                添加媒体服务器
              </Button>
            </div>

            <div v-if="librarySuccessMsg" class="bg-green-500/10 border border-green-500/20 text-green-400 text-sm px-4 py-3 rounded-lg flex items-center gap-2 mb-4">
              <CheckCircle2 class="h-5 w-5" />
              <span>{{ librarySuccessMsg }}</span>
            </div>

            <!-- Library Form -->
            <Card v-if="showLibraryForm" class="bg-slate-900 border-amber-500/30 text-slate-100 mb-6">
              <CardHeader>
                <CardTitle class="text-amber-500">{{ editingLibrary?.id ? '编辑媒体服务器' : '新建媒体服务器' }}</CardTitle>
              </CardHeader>
              <CardContent class="space-y-4">
                <div class="grid gap-4 md:grid-cols-2">
                  <div class="space-y-1.5">
                    <label class="text-xs font-semibold text-slate-400">名称</label>
                    <Input v-model="editingLibrary.name" placeholder="例如: 家庭 Emby" class="bg-slate-950 border-slate-800 text-slate-100" />
                  </div>
                  <div class="space-y-1.5">
                    <label class="text-xs font-semibold text-slate-400">类型</label>
                    <select v-model="editingLibrary.type" class="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm focus:outline-none text-slate-100 focus:border-amber-500">
                      <option value="emby">Emby</option>
                      <option value="jellyfin">Jellyfin</option>
                      <option value="plex">Plex</option>
                    </select>
                  </div>
                </div>
                <div class="space-y-1.5">
                  <label class="text-xs font-semibold text-slate-400">服务器地址</label>
                  <Input v-model="editingLibrary.url" placeholder="例如: http://192.168.1.10:8096" class="bg-slate-950 border-slate-800 text-slate-100" />
                </div>
                <div class="space-y-1.5">
                  <label class="text-xs font-semibold text-slate-400">{{ editingLibrary.type === 'plex' ? 'X-Plex-Token' : 'API Key' }}</label>
                  <Input v-model="editingLibrary.api_key" type="password" placeholder="服务器密钥 / Token" class="bg-slate-950 border-slate-800 text-slate-100" />
                </div>
                <!-- 密钥正确时自动展示媒体库下拉 -->
                <div class="space-y-1.5">
                  <label class="text-xs font-semibold text-slate-400 flex items-center gap-2">
                    刷新的媒体库
                    <Loader2 v-if="probingLibraries" class="h-3 w-3 animate-spin text-slate-500" />
                  </label>
                  <select
                    v-if="probeLibraries.length > 0"
                    v-model="editingLibrary.library_id"
                    @change="onLibrarySelect"
                    class="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm focus:outline-none text-slate-100 focus:border-amber-500"
                  >
                    <option value="">全部媒体库</option>
                    <option v-for="l in probeLibraries" :key="l.id" :value="l.id">{{ l.name }}</option>
                  </select>
                  <p v-else class="text-xs text-slate-500">
                    填写正确的地址与密钥后将自动加载媒体库列表；当前默认刷新「全部媒体库」。
                  </p>
                </div>
                <div class="flex items-center gap-2">
                  <input type="checkbox" v-model="editingLibrary.enabled" class="rounded border-slate-700 bg-slate-950 text-amber-500 focus:ring-amber-500" />
                  <span class="text-sm text-slate-300">启用此媒体服务器</span>
                </div>
                <div class="flex gap-3 pt-2">
                  <Button @click="saveLibrary" :disabled="isLibrarySaving" class="bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold flex items-center gap-2">
                    <Loader2 v-if="isLibrarySaving" class="h-4 w-4 animate-spin" />
                    <Save v-else class="h-4 w-4" />
                    {{ isLibrarySaving ? '保存中...' : '保存媒体服务器' }}
                  </Button>
                  <Button @click="closeLibraryForm" variant="outline" class="border-slate-700 text-slate-300 hover:bg-slate-800">
                    取消
                  </Button>
                </div>
              </CardContent>
            </Card>

            <!-- Servers Grid -->
            <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
              <Card v-for="lib in libraries" :key="lib.id" class="bg-slate-900 border-slate-800 text-slate-100 transition-all hover:border-slate-700">
                <CardHeader class="pb-3">
                  <div class="flex items-center justify-between">
                    <div class="flex items-center gap-2">
                      <span
                        :class="['inline-block h-2.5 w-2.5 rounded-full shrink-0', statusMap[lib.id] ? 'bg-emerald-500 shadow-[0_0_6px_1px] shadow-emerald-500/60' : 'bg-slate-600']"
                        :title="statusMap[lib.id] ? '在线' : '离线 / 未知'"
                      ></span>
                      <Server class="h-5 w-5 text-sky-400" />
                      <CardTitle class="text-slate-100 text-base">{{ lib.name }}</CardTitle>
                    </div>
                    <div class="flex items-center gap-1">
                      <button
                        @click="refreshLibrary(lib.id)"
                        :disabled="refreshingLibraryId === lib.id"
                        class="p-1.5 rounded hover:bg-slate-800 text-slate-500 hover:text-amber-400 transition-colors disabled:opacity-50"
                        title="立即刷新媒体库"
                      >
                        <RefreshCw :class="['h-4 w-4', refreshingLibraryId === lib.id ? 'animate-spin' : '']" />
                      </button>
                      <button
                        @click="testLibrary(lib.id)"
                        :disabled="testingLibraryId === lib.id"
                        class="p-1.5 rounded hover:bg-slate-800 text-slate-500 hover:text-emerald-400 transition-colors disabled:opacity-50"
                        title="测试连接"
                      >
                        <Loader2 v-if="testingLibraryId === lib.id" class="h-4 w-4 animate-spin" />
                        <Plug v-else class="h-4 w-4" />
                      </button>
                      <button
                        @click="openEditLibrary(lib)"
                        class="p-1.5 rounded hover:bg-slate-800 text-slate-500 hover:text-slate-300 transition-colors"
                        title="编辑"
                      >
                        <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
                      </button>
                      <button
                        @click="deleteLibrary(lib.id)"
                        class="p-1.5 rounded hover:bg-slate-800 text-slate-500 hover:text-rose-500 transition-colors"
                        title="删除"
                      >
                        <Trash2 class="h-4 w-4" />
                      </button>
                    </div>
                  </div>
                  <CardDescription class="text-slate-400 text-xs">
                    <span class="uppercase">{{ lib.type }}</span>
                    <span v-if="!lib.enabled" class="text-rose-400 ml-1">[已禁用]</span>
                  </CardDescription>
                </CardHeader>
                <CardContent class="space-y-2 pt-0">
                  <div class="text-xs">
                    <span class="text-slate-500">地址:</span>
                    <span class="text-slate-300 ml-1 font-mono break-all">{{ lib.url }}</span>
                  </div>
                  <div class="text-xs">
                    <span class="text-slate-500">刷新范围:</span>
                    <span class="text-sky-300 ml-1">{{ lib.library_name || '全部媒体库' }}</span>
                  </div>
                </CardContent>
              </Card>

              <!-- Empty State -->
              <div v-if="libraries.length === 0" class="md:col-span-2 lg:col-span-3 text-center py-12 bg-slate-900/50 rounded-lg border border-dashed border-slate-800">
                <p class="text-slate-500 text-sm">暂无媒体服务器，点击上方按钮添加</p>
              </div>
            </div>
          </div>

          <!-- ===================== 安全 ===================== -->
          <template v-else-if="activeTab === 'security'">
            <div class="flex items-center justify-between gap-4 mb-2">
              <div class="min-w-0">
                <h2 class="text-xl font-bold text-slate-100">安全</h2>
                <p class="text-slate-400 text-sm truncate">修改控制台登录密码</p>
              </div>
            </div>
            <Card class="bg-slate-900 border-slate-800 text-slate-100">
              <CardHeader>
                <CardTitle class="text-amber-500 flex items-center gap-2">
                  <KeyRound class="h-5 w-5" />
                  修改登录密码
                </CardTitle>
                <CardDescription class="text-slate-400">为了系统安全，请定期修改控制台登录密码</CardDescription>
              </CardHeader>
              <CardContent class="grid gap-6 md:grid-cols-3">
                <div class="space-y-1.5">
                  <label class="text-xs font-semibold text-slate-400">当前密码</label>
                  <Input v-model="currentPassword" type="password" placeholder="当前登录密码" class="bg-slate-950 border-slate-800 text-slate-100" />
                </div>
                <div class="space-y-1.5">
                  <label class="text-xs font-semibold text-slate-400">新密码</label>
                  <Input v-model="newPassword" type="password" placeholder="新登录密码" class="bg-slate-950 border-slate-800 text-slate-100" />
                </div>
                <div class="space-y-1.5">
                  <label class="text-xs font-semibold text-slate-400">确认新密码</label>
                  <div class="flex gap-3">
                    <Input v-model="confirmNewPassword" type="password" placeholder="确认新登录密码" class="bg-slate-950 border-slate-800 text-slate-100 flex-1" />
                    <Button @click="changePassword" :disabled="isChangingPassword" class="bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold flex items-center gap-2 shrink-0 h-10 px-4">
                      <Loader2 v-if="isChangingPassword" class="h-4 w-4 animate-spin" />
                      <span>修改密码</span>
                    </Button>
                  </div>
                </div>
              </CardContent>
            </Card>
          </template>
        </div>
      </Transition>
    </template>
  </div>
</template>

<style scoped>
/* Tab panel cross-fade */
.panel-enter-active {
  transition: opacity 0.2s ease;
}
.panel-leave-active {
  transition: opacity 0.12s ease;
}
.panel-enter-from,
.panel-leave-to {
  opacity: 0;
}

/* Staggered reveal of the active panel's content */
.stagger > * {
  animation: rise 0.42s cubic-bezier(0.22, 0.61, 0.36, 1) both;
}
.stagger > *:nth-child(1) { animation-delay: 0.02s; }
.stagger > *:nth-child(2) { animation-delay: 0.09s; }
.stagger > *:nth-child(3) { animation-delay: 0.16s; }
.stagger > *:nth-child(4) { animation-delay: 0.23s; }

@keyframes rise {
  from {
    opacity: 0;
    transform: translateY(12px);
  }
  to {
    opacity: 1;
    transform: none;
  }
}
</style>
