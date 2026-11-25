<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { toast } from 'vue-sonner'
import {
  BellIcon,
  BotIcon,
  CheckCircle2Icon,
  CircleAlertIcon,
  ClapperboardIcon,
  CopyIcon,
  FilmIcon,
  FolderTreeIcon,
  GlobeIcon,
  HardDriveIcon,
  HistoryIcon,
  KeyRoundIcon,
  PencilIcon,
  PlugIcon,
  PlusIcon,
  RefreshCwIcon,
  SaveIcon,
  SendIcon,
  ServerIcon,
  ShieldCheckIcon,
  SlidersHorizontalIcon,
  StarIcon,
  Trash2Icon,
  TvIcon,
} from 'lucide-vue-next'
import PageSection from '@/components/common/PageSection.vue'
import DownloadClientPanel from '@/components/search/DownloadClientPanel.vue'
import client from '@/api/client'
import { useConfirm } from '@/composables/useConfirm'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { encryptAESGCM } from '@/lib/crypto'
import { copyText, EMPTY_SELECT_VALUE, fromSelectValue, toSelectValue } from '@/lib/utils'

const { confirm } = useConfirm()

type TabKey = 'general' | 'directories' | 'servers' | 'clients' | 'notify' | 'security' | 'mcp' | 'search' | 'proxy'
const activeTab = ref<TabKey>('general')
const tabs: { key: TabKey; label: string; icon: any }[] = [
  { key: 'general', label: '刮削配置', icon: SlidersHorizontalIcon },
  { key: 'directories', label: '目录管理', icon: FolderTreeIcon },
  { key: 'servers', label: '媒体服务器', icon: ServerIcon },
  { key: 'clients', label: '下载器', icon: HardDriveIcon },
  { key: 'notify', label: '通知配置', icon: BellIcon },
  { key: 'security', label: '安全', icon: ShieldCheckIcon },
  { key: 'mcp', label: 'MCP / API Key', icon: BotIcon },
  { key: 'search', label: '搜索下载', icon: PlugIcon },
  { key: 'proxy', label: '代理配置', icon: GlobeIcon },
]

const settings = ref({
  tmdb_api_key: '',
  tmdb_language: 'zh-CN',
  transfer_mode: 'link',
  overwrite_mode: 'size',
  auto_scrape: true,
  scrape_person: false,
  lock_nfo: false,
  search_proxy: '',
  search_definition_source: '',
  search_definition_version: '',
  search_auto_sync: true,
  network_proxy: '',
  network_scrape_via_proxy: false,
  network_search_via_proxy: true,
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

// Notification channels state
const notifyChannels = ref<any[]>([])
const channelTypes = ref<any[]>([])
const editingChannel = ref<any>(null)
const showChannelForm = ref(false)
const isChannelSaving = ref(false)
const channelSuccessMsg = ref('')
const testingChannelId = ref<number | null>(null)

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

const searchProxyMsg = ref('')
const searchProxyMsgOk = ref(true)
const isTestingProxy = ref(false)

const testProxy = async () => {
  searchProxyMsg.value = ''
  isTestingProxy.value = true
  try {
    const res: any = await client.put('/api/v1/settings', settingsPayload())
    if (res.code !== 0) throw new Error(res.msg || '保存失败')
    const res2: any = await client.post('/api/v1/search/proxy-test', {}, { timeout: 20000 })
    const data = res2.data ?? {}
    if (!data.ok) throw new Error(data.message || '代理不可用')
    searchProxyMsgOk.value = true
    searchProxyMsg.value = `${data.message}，耗时 ${data.elapsed_ms}ms`
  } catch (err: any) {
    searchProxyMsgOk.value = false
    searchProxyMsg.value = err?.msg || String(err?.message || err)
  } finally {
    isTestingProxy.value = false
  }
}

const clearProxy = async () => {
  settings.value.network_proxy = ''
  settings.value.search_proxy = ''
  isSaving.value = true
  successMsg.value = ''
  try {
    const res: any = await client.put('/api/v1/settings', { ...settingsPayload(), clear_network_proxy: true })
    if (res.code === 0) {
      successMsg.value = '代理已清空'
      setTimeout(() => { successMsg.value = '' }, 3000)
    }
  } catch (err) {
    console.error('Failed to clear proxy', err)
  } finally {
    isSaving.value = false
  }
}

// settingsPayload 去掉升级前遗留的 search_proxy：它是服务端只读回显字段，回传会被
// 当成「清空旧代理」的指令。
const settingsPayload = (): Record<string, any> => {
  const { search_proxy: _legacySearchProxy, ...payload } = settings.value as Record<string, any>
  return payload
}

const saveSettings = async () => {
  isSaving.value = true
  successMsg.value = ''
  try {
    const res: any = await client.put('/api/v1/settings', settingsPayload())
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

// ===== Notification channels =====
const fetchNotifyChannels = async () => {
  try {
    const res: any = await client.get('/api/v1/notify-channels')
    if (res.code === 0) notifyChannels.value = res.data || []
  } catch (err) {
    console.error('Failed to load notify channels', err)
  }
}

const fetchChannelTypes = async () => {
  try {
    const res: any = await client.get('/api/v1/notify-channels/types')
    if (res.code === 0) channelTypes.value = res.data || []
  } catch (err) {
    console.error('Failed to load channel types', err)
  }
}

const currentChannelFields = computed(
  () => channelTypes.value.find((t) => t.type === editingChannel.value?.type)?.fields || []
)

const channelTypeName = (type: string) =>
  channelTypes.value.find((t) => t.type === type)?.name || type

const applyChannelTypeDefaults = () => {
  const t = channelTypes.value.find((t) => t.type === editingChannel.value.type)
  if (!t) return
  for (const f of t.fields) {
    if (f.default && !editingChannel.value.config[f.key]) {
      editingChannel.value.config[f.key] = f.default
    }
  }
}

const openNewChannel = () => {
  editingChannel.value = {
    name: '',
    type: channelTypes.value[0]?.type || 'wecombot',
    enabled: true,
    config: {},
  }
  applyChannelTypeDefaults()
  showChannelForm.value = true
}

const onChannelTypeChange = () => {
  editingChannel.value.config = {}
  applyChannelTypeDefaults()
}

const openEditChannel = (ch: any) => {
  editingChannel.value = { ...ch, config: { ...(ch.config || {}) } }
  showChannelForm.value = true
}

const closeChannelForm = () => {
  showChannelForm.value = false
  editingChannel.value = null
}

const saveChannel = async () => {
  if (!editingChannel.value.name) {
    toast.warning('请填写渠道名称')
    return
  }
  isChannelSaving.value = true
  try {
    const payload = {
      name: editingChannel.value.name,
      type: editingChannel.value.type,
      enabled: editingChannel.value.enabled,
      config: editingChannel.value.config,
    }
    let res: any
    if (editingChannel.value.id) {
      res = await client.put(`/api/v1/notify-channels/${editingChannel.value.id}`, payload)
    } else {
      res = await client.post('/api/v1/notify-channels', payload)
    }
    if (res.code === 0) {
      channelSuccessMsg.value = editingChannel.value.id ? '通知渠道更新成功！' : '通知渠道创建成功！'
      await fetchNotifyChannels()
      setTimeout(() => { channelSuccessMsg.value = '' }, 3000)
      closeChannelForm()
    }
  } catch (err) {
    console.error('Failed to save channel', err)
  } finally {
    isChannelSaving.value = false
  }
}

const deleteChannel = async (id: number) => {
  if (!await confirm('确定删除此通知渠道？')) return
  try {
    await client.delete(`/api/v1/notify-channels/${id}`)
    await fetchNotifyChannels()
  } catch (err) {
    console.error('Failed to delete channel', err)
  }
}

const testChannel = async (id: number) => {
  testingChannelId.value = id
  try {
    const res: any = await client.post(`/api/v1/notify-channels/${id}/test`)
    if (res.code === 0) {
      toast.success('测试消息已发送')
    } else {
      toast.error(res.msg || '发送失败')
    }
  } catch (err: any) {
    toast.error(err.response?.data?.msg || '发送失败')
  } finally {
    testingChannelId.value = null
  }
}

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

// ===== MCP / API Key =====
const locationOrigin = typeof window !== 'undefined' ? window.location.origin : ''
const mcpKeys = ref<any[]>([])
const newKeyName = ref('')
const creatingKey = ref(false)
const showSecret = ref<number | null>(null)   // key id whose plaintext modal is open
const secretText = ref('')
const secretName = ref('')
// Input 是组件，模板 ref 拿到的是组件实例，取 $el 才能拿到原生 input 做 select()
const secretInputRef = ref<any>(null)
const togglingKeyId = ref<number | null>(null)
// call records
const mcpRecords = ref<any[]>([])
const recordsTotal = ref(0)
const recordsPage = ref(1)
const recordsLimit = ref(20)
const recordsFilterKey = ref<number | ''>('')
const recordsFilterTool = ref('')
const TOOL_LABELS: Record<string, string> = {
  list_media_cards: '查询媒体卡片',
  query_media_list: '查询媒体列表',
  query_media_subtitles: '查询字幕明细',
  fetch_subtitle: '获取字幕内容',
  upload_subtitle: '上传字幕',
  mcp_ping: '连通性自测',
}
const toolLabel = (tool: string) => TOOL_LABELS[tool] || tool
const recordsFilterStatus = ref('')
const loadingRecords = ref(false)

const fetchMCPKeys = async () => {
  try {
    const res: any = await client.get('/api/v1/mcp/api-keys')
    if (res.code === 0) mcpKeys.value = res.data || []
  } catch (err) {
    console.error('Failed to load mcp keys', err)
  }
}

const createMCPKey = async () => {
  if (!newKeyName.value.trim()) {
    toast.warning('请填写用途名称')
    return
  }
  creatingKey.value = true
  try {
    const res: any = await client.post('/api/v1/mcp/api-keys', { name: newKeyName.value.trim() })
    if (res.code === 0) {
      secretText.value = res.data.key
      secretName.value = res.data.name
      showSecret.value = res.data.id
      newKeyName.value = ''
      await fetchMCPKeys()
    } else {
      toast.error(res.msg || '创建失败')
    }
  } catch (err: any) {
    toast.error(err.response?.data?.msg || '创建失败')
  } finally {
    creatingKey.value = false
  }
}

const closeSecret = () => {
  showSecret.value = null
  secretText.value = ''
}

const copySecret = async () => {
  const ok = await copyText(secretText.value)
  if (ok) {
    toast.success('已复制，请妥善保存（仅显示一次）')
  } else {
    // 复制通道被浏览器拒绝时，至少把明文选中，让用户能手动 ⌘C / Ctrl+C
    secretInputRef.value?.$el?.select?.()
    toast.error('复制失败，已选中明文，请按 ⌘C / Ctrl+C 手动复制')
  }
}

const copyMCPConfig = async (key: any) => {
  // 从创建弹窗或已有 key（仅前缀，不含明文）复制端点模板
  const token = key?.key || secretText.value
  const cfg = {
    mcpServers: {
      'bujic-movie': {
        type: 'http',
        url: `${location.origin}/api/v1/mcp`,
        headers: { Authorization: `Bearer ${token}` },
        toolNames: [
          'list_media_cards',
          'query_media_list',
          'query_media_subtitles',
          'fetch_subtitle',
          'upload_subtitle',
        ],
      },
    },
  }
  const ok = await copyText(JSON.stringify(cfg, null, 2))
  if (ok) {
    if (token) {
      toast.success('MCP 配置已复制，粘贴到 Agent 的 MCP 配置即可')
    } else {
      toast.warning('已复制配置模板（列表不含明文 Key），请手动填入完整 API Key')
    }
  } else {
    toast.error('复制失败')
  }
}

const toggleMCPKey = async (k: any) => {
  const target = k.status === 'active' ? 'disable' : 'enable'
  const actionLabel = target === 'disable' ? '禁用' : '启用'
  if (target === 'disable' && !(await confirm('确定禁用此 API Key？使用它的 Agent 将立即无法调用。'))) return
  togglingKeyId.value = k.id
  try {
    const res: any = await client.put(`/api/v1/mcp/api-keys/${k.id}/${target}`)
    if (res.code === 0) {
      toast.success(`已${actionLabel}`)
      await fetchMCPKeys()
    } else {
      toast.error(res.msg || '操作失败')
    }
  } catch (err: any) {
    toast.error(err.response?.data?.msg || '操作失败')
  } finally {
    togglingKeyId.value = null
  }
}

const deleteMCPKey = async (k: any) => {
  const ok = await confirm({
    title: '删除 API Key',
    message: `确定删除 Key「${k.name}」？删除后立即失效不可恢复（历史调用记录会保留）。`,
    confirmText: '删除',
  })
  if (!ok) return
  try {
    const res: any = await client.delete(`/api/v1/mcp/api-keys/${k.id}`)
    if (res.code === 0) {
      toast.success('已删除')
      if (recordsFilterKey.value === k.id) recordsFilterKey.value = ''
      await fetchMCPKeys()
    } else {
      toast.error(res.msg || '删除失败')
    }
  } catch (err: any) {
    toast.error(err.response?.data?.msg || '删除失败')
  }
}

const clearMCPRecords = async () => {
  const ok = await confirm({
    title: '清空调用记录',
    message: '确定清空全部 MCP 调用记录？此操作不可撤销。',
    confirmText: '清空',
  })
  if (!ok) return
  try {
    const res: any = await client.delete('/api/v1/mcp/call-records')
    if (res.code === 0) {
      toast.success('已清空')
      mcpRecords.value = []
      recordsTotal.value = 0
    } else {
      toast.error(res.msg || '清空失败')
    }
  } catch (err: any) {
    toast.error(err.response?.data?.msg || '清空失败')
  }
}

const fmtTime = (s?: string) => (s ? new Date(s).toLocaleString() : '—')

const fetchMCPRecords = async () => {
  loadingRecords.value = true
  try {
    const params: Record<string, any> = { page: recordsPage.value, limit: recordsLimit.value }
    if (recordsFilterTool.value) params.tool = recordsFilterTool.value
    if (recordsFilterStatus.value) params.status = recordsFilterStatus.value
    let url = '/api/v1/mcp/call-records'
    if (recordsFilterKey.value) url = `/api/v1/mcp/api-keys/${recordsFilterKey.value}/records`
    const res: any = await client.get(url, { params })
    if (res.code === 0) {
      mcpRecords.value = res.data.records || []
      recordsTotal.value = res.data.total || 0
    }
  } catch (err) {
    console.error('Failed to load mcp records', err)
  } finally {
    loadingRecords.value = false
  }
}

const openRecords = async () => {
  recordsPage.value = 1
  await fetchMCPRecords()
}

/** Key 状态用 Badge 表达。 */
const keyStatusTone = (s: string) => (s === 'active' ? { variant: 'secondary' as const, tone: 'text-success' } : { variant: 'destructive' as const, tone: '' })

onMounted(() => {
  fetchSettings()
  fetchCards()
  fetchLibraries()
  fetchStatuses()
  fetchNotifyChannels()
  fetchChannelTypes()
  fetchMCPKeys()
  statusTimer = setInterval(fetchStatuses, 30000)
})

watch(activeTab, (tab) => {
  if (tab === 'mcp') {
    fetchMCPKeys()
  }
})

onUnmounted(() => {
  if (statusTimer) clearInterval(statusTimer)
  if (probeTimer) clearTimeout(probeTimer)
})
</script>

<template>
  <div class="flex flex-col gap-6">
    <div>
      <h1 class="text-2xl font-bold tracking-tight">系统设置</h1>
      <p class="text-muted-foreground mt-1 text-sm">配置刮削与文件整理参数，并持久化存储在服务器。</p>
    </div>

    <div v-if="isLoading" class="flex items-center justify-center py-20">
      <Spinner class="text-primary" />
    </div>

    <Tabs v-else v-model="activeTab" class="flex flex-col gap-6">
      <TabsList variant="line" class="max-w-full flex-wrap">
        <TabsTrigger v-for="t in tabs" :key="t.key" :value="t.key" class="flex-none">
          <component :is="t.icon" data-icon="inline-start" />
          {{ t.label }}
        </TabsTrigger>
      </TabsList>

      <!-- ===================== 常规 ===================== -->
      <TabsContent value="general" class="flex flex-col gap-4">
        <PageSection title="刮削配置" description="TMDB 配置与整理、刮削规则">
          <Button :disabled="isSaving" @click="saveSettings">
            <Spinner v-if="isSaving" data-icon="inline-start" />
            <SaveIcon v-else data-icon="inline-start" />
            保存设置
          </Button>
        </PageSection>

        <Alert v-if="successMsg" class="text-success">
          <CheckCircle2Icon />
          <AlertTitle>配置保存成功</AlertTitle>
        </Alert>

        <Card>
          <CardHeader>
            <CardTitle class="flex items-center gap-2">
              <ClapperboardIcon class="text-primary size-5" />
              TMDB API 配置
            </CardTitle>
            <CardDescription>
              刮削媒体墙海报及 NFO 文件必须配置此项。
              <a
                href="https://www.themoviedb.org/settings/api"
                target="_blank"
                rel="noopener noreferrer"
                class="text-primary hover:underline"
              >点此获取 API 密钥</a>
            </CardDescription>
          </CardHeader>
          <CardContent>
            <FieldGroup class="gap-4 md:grid md:grid-cols-2">
              <Field>
                <FieldLabel for="tmdb-key">TMDB API Key</FieldLabel>
                <Input id="tmdb-key" v-model="settings.tmdb_api_key" type="password" placeholder="填入您的 TMDB API Key" />
              </Field>
              <Field>
                <FieldLabel for="tmdb-lang">刮削首选语言</FieldLabel>
                <Select v-model="settings.tmdb_language">
                  <SelectTrigger id="tmdb-lang" class="w-full"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value="zh-CN">简体中文 (zh-CN)</SelectItem>
                      <SelectItem value="zh-TW">繁体中文 (zh-TW)</SelectItem>
                      <SelectItem value="en-US">英文 (en-US)</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
            </FieldGroup>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle class="flex items-center gap-2">
              <SlidersHorizontalIcon class="text-primary size-5" />
              整理与刮削规则
            </CardTitle>
            <CardDescription>调整文件转移策略与覆盖机制</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldGroup class="gap-4 md:grid md:grid-cols-3">
              <Field>
                <FieldLabel for="transfer-mode-setting">转移模式</FieldLabel>
                <Select v-model="settings.transfer_mode">
                  <SelectTrigger id="transfer-mode-setting" class="w-full"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value="copy">复制 (Copy) — 占双倍硬盘空间</SelectItem>
                      <SelectItem value="move">移动 (Move) — 源文件将被清除</SelectItem>
                      <SelectItem value="link">硬链接 (Link) — 推荐，不占空间可双向删除</SelectItem>
                      <SelectItem value="softlink">软链接 (Softlink) — 快捷方式</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
              <Field>
                <FieldLabel for="overwrite-mode-setting">覆盖冲突模式</FieldLabel>
                <Select v-model="settings.overwrite_mode">
                  <SelectTrigger id="overwrite-mode-setting" class="w-full"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value="never">从不覆盖 (Never)</SelectItem>
                      <SelectItem value="always">总是覆盖 (Always)</SelectItem>
                      <SelectItem value="size">大文件覆盖小文件 (Size) — 推荐</SelectItem>
                      <SelectItem value="latest">仅覆盖同季同集视频 (Latest)</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
              <Field orientation="horizontal">
                <Switch id="auto-scrape" v-model="settings.auto_scrape" />
                <FieldLabel for="auto-scrape">整理后自动触发刮削</FieldLabel>
              </Field>

              <Field orientation="horizontal" class="md:col-span-3">
                <Switch id="scrape-person-toggle" v-model="settings.scrape_person" />
                <FieldContent>
                  <FieldLabel for="scrape-person-toggle">启用演职人员刮削</FieldLabel>
                  <FieldDescription>
                    开启后，写入 NFO 的 <code>&lt;actor&gt;</code> 字段会包含主要演职人员（与电影/电视剧均生效）。
                  </FieldDescription>
                </FieldContent>
              </Field>

              <Field orientation="horizontal" class="md:col-span-3">
                <Switch id="lock-nfo-toggle" v-model="settings.lock_nfo" />
                <FieldContent>
                  <FieldLabel for="lock-nfo-toggle">锁定 NFO 文件</FieldLabel>
                  <FieldDescription>
                    开启后，写入 NFO 的 <code>&lt;lockdata&gt;</code> 字段将被设为 true，防止被媒体服务器自动修改/覆盖。
                  </FieldDescription>
                </FieldContent>
              </Field>
            </FieldGroup>
          </CardContent>
        </Card>
      </TabsContent>

      <!-- ===================== 目录管理 ===================== -->
      <TabsContent value="directories" class="flex flex-col gap-4">
        <PageSection title="目录管理" description="配置下载源目录、归档目录和媒体类型，支持多目录管理">
          <Button @click="openNewCard">
            <PlusIcon data-icon="inline-start" />
            添加目录
          </Button>
        </PageSection>

        <Alert v-if="cardSuccessMsg" class="text-success">
          <CheckCircle2Icon />
          <AlertTitle>{{ cardSuccessMsg }}</AlertTitle>
        </Alert>

        <Card v-if="showCardForm" class="border-primary/30">
          <CardHeader>
            <CardTitle>{{ editingCard?.id ? '编辑目录' : '新建目录' }}</CardTitle>
          </CardHeader>
          <CardContent>
            <FieldGroup class="gap-4">
              <div class="grid gap-4 md:grid-cols-2">
                <Field>
                  <FieldLabel for="card-name">名称</FieldLabel>
                  <Input id="card-name" v-model="editingCard.name" placeholder="例如: 电影整理" />
                </Field>
                <Field>
                  <FieldLabel for="card-media-type">媒体类型</FieldLabel>
                  <Select v-model="editingCard.media_type">
                    <SelectTrigger id="card-media-type" class="w-full"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value="movie">电影 (Movie)</SelectItem>
                        <SelectItem value="tv">电视剧 (TV)</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
              </div>
              <Field>
                <FieldLabel for="card-download-path">下载源目录</FieldLabel>
                <Input id="card-download-path" v-model="editingCard.download_path" placeholder="例如: /downloads/movies" />
              </Field>
              <Field>
                <FieldLabel for="card-archive-path">归档目录</FieldLabel>
                <Input id="card-archive-path" v-model="editingCard.archive_path" placeholder="例如: /media/Movies" />
              </Field>
              <Field>
                <FieldLabel for="card-library">所属媒体服务器（整理刮削完成后自动刷新）</FieldLabel>
                <Select v-model.number="editingCard.media_library_id">
                  <SelectTrigger id="card-library" class="w-full"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem :value="0">不绑定（不触发刷新）</SelectItem>
                      <SelectItem v-for="lib in libraries" :key="lib.id" :value="lib.id">{{ lib.name }} ({{ lib.type }})</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
              <Field orientation="horizontal">
                <Checkbox id="card-default" :model-value="editingCard.is_default" @update:model-value="editingCard.is_default = Boolean($event)" />
                <FieldLabel for="card-default">设为默认卡片</FieldLabel>
              </Field>
              <Field orientation="horizontal">
                <Checkbox id="card-watch" :model-value="editingCard.watch_directory" @update:model-value="editingCard.watch_directory = Boolean($event)" />
                <FieldLabel for="card-watch">开启实时目录监听（自动整理）</FieldLabel>
              </Field>
              <div class="flex gap-3">
                <Button :disabled="isCardSaving" @click="saveCard">
                  <Spinner v-if="isCardSaving" data-icon="inline-start" />
                  <SaveIcon v-else data-icon="inline-start" />
                  {{ isCardSaving ? '保存中…' : '保存目录' }}
                </Button>
                <Button variant="outline" @click="closeCardForm">取消</Button>
              </div>
            </FieldGroup>
          </CardContent>
        </Card>

        <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          <Card
            v-for="card in cards"
            :key="card.id"
            :class="card.is_default ? 'ring-1 ring-ring/50' : ''"
            class="transition-all hover:border-input"
          >
            <CardHeader>
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-2">
                  <FilmIcon v-if="card.media_type === 'movie'" class="text-primary size-5" />
                  <TvIcon v-else class="text-info size-5" />
                  <CardTitle class="text-base">{{ card.name }}</CardTitle>
                </div>
                <div class="flex items-center gap-1">
                  <Button v-if="!card.is_default" variant="ghost" size="icon-sm" title="设为默认" @click="setDefaultCard(card.id)">
                    <StarIcon />
                  </Button>
                  <StarIcon v-else class="text-primary size-4 fill-current" />
                  <Button variant="ghost" size="icon-sm" title="编辑" @click="openEditCard(card)">
                    <PencilIcon />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    class="text-muted-foreground hover:text-destructive"
                    title="删除"
                    @click="deleteCard(card.id)"
                  >
                    <Trash2Icon />
                  </Button>
                </div>
              </div>
              <CardDescription class="text-xs">
                {{ card.media_type === 'movie' ? '电影' : '电视剧' }}
                <Badge v-if="card.is_default" variant="secondary" class="ml-1 text-primary">默认</Badge>
              </CardDescription>
            </CardHeader>
            <CardContent class="flex flex-col gap-2 pt-0">
              <p class="text-xs">
                <span class="text-muted-foreground">下载源：</span>
                <span class="ml-1 font-mono break-all">{{ card.download_path }}</span>
              </p>
              <p class="text-xs">
                <span class="text-muted-foreground">归档到：</span>
                <span class="ml-1 font-mono break-all">{{ card.archive_path }}</span>
              </p>
              <p class="text-xs">
                <span class="text-muted-foreground">自动整理：</span>
                <span :class="['ml-1 font-semibold', card.watch_directory ? 'text-success' : 'text-muted-foreground']">
                  {{ card.watch_directory ? '已开启目录监听' : '已关闭' }}
                </span>
              </p>
              <p v-if="card.media_library_id" class="text-xs">
                <span class="text-muted-foreground">媒体服务器：</span>
                <span class="text-info ml-1">{{ libraryName(card.media_library_id) }}</span>
              </p>
            </CardContent>
          </Card>

          <Empty v-if="!cards.length" class="md:col-span-2 lg:col-span-3">
            <EmptyHeader>
              <EmptyMedia variant="icon"><FolderTreeIcon /></EmptyMedia>
              <EmptyTitle>暂无目录</EmptyTitle>
              <EmptyDescription>点击上方「添加目录」开始配置。</EmptyDescription>
            </EmptyHeader>
          </Empty>
        </div>
      </TabsContent>

      <!-- ===================== 媒体服务器 ===================== -->
      <TabsContent value="servers" class="flex flex-col gap-4">
        <PageSection title="媒体服务器" description="配置 Emby / Jellyfin / Plex，整理刮削完成后自动刷新">
          <Button @click="openNewLibrary">
            <PlusIcon data-icon="inline-start" />
            添加媒体服务器
          </Button>
        </PageSection>

        <Alert v-if="librarySuccessMsg" class="text-success">
          <CheckCircle2Icon />
          <AlertTitle>{{ librarySuccessMsg }}</AlertTitle>
        </Alert>

        <Card v-if="showLibraryForm" class="border-primary/30">
          <CardHeader>
            <CardTitle>{{ editingLibrary?.id ? '编辑媒体服务器' : '新建媒体服务器' }}</CardTitle>
          </CardHeader>
          <CardContent>
            <FieldGroup class="gap-4">
              <div class="grid gap-4 md:grid-cols-2">
                <Field>
                  <FieldLabel for="lib-name">名称</FieldLabel>
                  <Input id="lib-name" v-model="editingLibrary.name" placeholder="例如: 家庭 Emby" />
                </Field>
                <Field>
                  <FieldLabel for="lib-type">类型</FieldLabel>
                  <Select v-model="editingLibrary.type">
                    <SelectTrigger id="lib-type" class="w-full"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value="emby">Emby</SelectItem>
                        <SelectItem value="jellyfin">Jellyfin</SelectItem>
                        <SelectItem value="plex">Plex</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
              </div>
              <Field>
                <FieldLabel for="lib-url">服务器地址</FieldLabel>
                <Input id="lib-url" v-model="editingLibrary.url" placeholder="例如: http://192.168.1.10:8096" />
              </Field>
              <Field>
                <FieldLabel for="lib-key">{{ editingLibrary.type === 'plex' ? 'X-Plex-Token' : 'API Key' }}</FieldLabel>
                <Input id="lib-key" v-model="editingLibrary.api_key" type="password" placeholder="服务器密钥 / Token" />
              </Field>

              <Field>
                <FieldLabel for="lib-scope" class="flex items-center gap-2">
                  刷新的媒体库
                  <Spinner v-if="probingLibraries" class="text-muted-foreground" />
                </FieldLabel>
                <Select
                  v-if="probeLibraries.length > 0"
                  :model-value="toSelectValue(editingLibrary.library_id)"
                  @update:model-value="editingLibrary.library_id = fromSelectValue($event); onLibrarySelect()"
                >
                  <SelectTrigger id="lib-scope" class="w-full"><SelectValue placeholder="全部媒体库" /></SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem :value="EMPTY_SELECT_VALUE">全部媒体库</SelectItem>
                      <SelectItem v-for="l in probeLibraries" :key="l.id" :value="l.id">{{ l.name }}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FieldDescription v-else>
                  填写正确的地址与密钥后将自动加载媒体库列表；当前默认刷新「全部媒体库」。
                </FieldDescription>
              </Field>

              <Field orientation="horizontal">
                <Checkbox id="lib-enabled" :model-value="editingLibrary.enabled" @update:model-value="editingLibrary.enabled = Boolean($event)" />
                <FieldLabel for="lib-enabled">启用此媒体服务器</FieldLabel>
              </Field>

              <div class="flex gap-3">
                <Button :disabled="isLibrarySaving" @click="saveLibrary">
                  <Spinner v-if="isLibrarySaving" data-icon="inline-start" />
                  <SaveIcon v-else data-icon="inline-start" />
                  {{ isLibrarySaving ? '保存中…' : '保存媒体服务器' }}
                </Button>
                <Button variant="outline" @click="closeLibraryForm">取消</Button>
              </div>
            </FieldGroup>
          </CardContent>
        </Card>

        <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          <Card v-for="lib in libraries" :key="lib.id" class="transition-all hover:border-input">
            <CardHeader>
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-2">
                  <span
                    :class="['size-2.5 shrink-0 rounded-full', statusMap[lib.id] ? 'bg-success shadow-[0_0_6px_1px] shadow-success/60' : 'bg-muted']"
                    :title="statusMap[lib.id] ? '在线' : '离线 / 未知'"
                  ></span>
                  <ServerIcon class="text-info size-5" />
                  <CardTitle class="text-base">{{ lib.name }}</CardTitle>
                </div>
                <div class="flex items-center gap-1">
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    title="立即刷新媒体库"
                    :disabled="refreshingLibraryId === lib.id"
                    @click="refreshLibrary(lib.id)"
                  >
                    <Spinner v-if="refreshingLibraryId === lib.id" />
                    <RefreshCwIcon v-else />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    title="测试连接"
                    :disabled="testingLibraryId === lib.id"
                    @click="testLibrary(lib.id)"
                  >
                    <Spinner v-if="testingLibraryId === lib.id" />
                    <PlugIcon v-else />
                  </Button>
                  <Button variant="ghost" size="icon-sm" title="编辑" @click="openEditLibrary(lib)">
                    <PencilIcon />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    class="text-muted-foreground hover:text-destructive"
                    title="删除"
                    @click="deleteLibrary(lib.id)"
                  >
                    <Trash2Icon />
                  </Button>
                </div>
              </div>
              <CardDescription class="text-xs">
                <span class="uppercase">{{ lib.type }}</span>
                <Badge v-if="!lib.enabled" variant="destructive" class="ml-1">已禁用</Badge>
              </CardDescription>
            </CardHeader>
            <CardContent class="flex flex-col gap-2 pt-0">
              <p class="text-xs"><span class="text-muted-foreground">地址：</span><span class="ml-1 font-mono break-all">{{ lib.url }}</span></p>
              <p class="text-xs"><span class="text-muted-foreground">刷新范围：</span><span class="text-info ml-1">{{ lib.library_name || '全部媒体库' }}</span></p>
            </CardContent>
          </Card>

          <Empty v-if="!libraries.length" class="md:col-span-2 lg:col-span-3">
            <EmptyHeader>
              <EmptyMedia variant="icon"><ServerIcon /></EmptyMedia>
              <EmptyTitle>暂无媒体服务器</EmptyTitle>
              <EmptyDescription>点击上方「添加媒体服务器」开始配置。</EmptyDescription>
            </EmptyHeader>
          </Empty>
        </div>
      </TabsContent>

      <!-- ===================== 下载器 ===================== -->
      <TabsContent value="clients" class="flex flex-col gap-4">
        <PageSection title="下载器" description="支持 qBittorrent / Transmission / Deluge / Aria2；能按分类自动落盘的就只配分类映射，不能的按分类指定下载目录" />
        <DownloadClientPanel />
      </TabsContent>

      <!-- ===================== 通知配置 ===================== -->
      <TabsContent value="notify" class="flex flex-col gap-4">
        <PageSection title="通知配置" description="配置第三方通知渠道，刮削入库后自动推送">
          <Button @click="openNewChannel">
            <PlusIcon data-icon="inline-start" />
            添加渠道
          </Button>
        </PageSection>

        <Alert v-if="channelSuccessMsg" class="text-success">
          <CheckCircle2Icon />
          <AlertTitle>{{ channelSuccessMsg }}</AlertTitle>
        </Alert>

        <Card v-if="showChannelForm" class="border-primary/30">
          <CardHeader>
            <CardTitle>{{ editingChannel?.id ? '编辑渠道' : '新建渠道' }}</CardTitle>
          </CardHeader>
          <CardContent>
            <FieldGroup class="gap-4">
              <div class="grid gap-4 md:grid-cols-2">
                <Field>
                  <FieldLabel for="ch-name">渠道名称</FieldLabel>
                  <Input id="ch-name" v-model="editingChannel.name" placeholder="例如: 我的企业微信" />
                </Field>
                <Field>
                  <FieldLabel for="ch-type">渠道类型</FieldLabel>
                  <Select v-model="editingChannel.type" @update:model-value="onChannelTypeChange">
                    <SelectTrigger id="ch-type" class="w-full"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem v-for="t in channelTypes" :key="t.type" :value="t.type">{{ t.name }}</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
              </div>

              <Field v-for="f in currentChannelFields" :key="f.key">
                <FieldLabel :for="`ch-field-${f.key}`">
                  {{ f.label }}<span v-if="f.required" class="text-destructive ml-0.5">*</span>
                </FieldLabel>
                <Textarea
                  v-if="f.type === 'textarea'"
                  :id="`ch-field-${f.key}`"
                  v-model="editingChannel.config[f.key]"
                  :placeholder="f.placeholder"
                  rows="3"
                  class="font-mono"
                />
                <Input
                  v-else
                  :id="`ch-field-${f.key}`"
                  v-model="editingChannel.config[f.key]"
                  :type="f.type === 'password' ? 'password' : 'text'"
                  :placeholder="f.placeholder"
                />
              </Field>

              <Field orientation="horizontal">
                <Checkbox id="ch-enabled" :model-value="editingChannel.enabled" @update:model-value="editingChannel.enabled = Boolean($event)" />
                <FieldLabel for="ch-enabled">启用此渠道</FieldLabel>
              </Field>

              <div class="flex gap-3">
                <Button :disabled="isChannelSaving" @click="saveChannel">
                  <Spinner v-if="isChannelSaving" data-icon="inline-start" />
                  <SaveIcon v-else data-icon="inline-start" />
                  {{ isChannelSaving ? '保存中…' : '保存渠道' }}
                </Button>
                <Button variant="outline" @click="closeChannelForm">取消</Button>
              </div>
            </FieldGroup>
          </CardContent>
        </Card>

        <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          <Card v-for="ch in notifyChannels" :key="ch.id" class="transition-all hover:border-input">
            <CardHeader>
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-2">
                  <BellIcon class="text-primary size-5" />
                  <CardTitle class="text-base">{{ ch.name }}</CardTitle>
                </div>
                <div class="flex items-center gap-1">
                  <Button variant="ghost" size="icon-sm" title="测试发送" :disabled="testingChannelId === ch.id" @click="testChannel(ch.id)">
                    <Spinner v-if="testingChannelId === ch.id" />
                    <SendIcon v-else />
                  </Button>
                  <Button variant="ghost" size="icon-sm" title="编辑" @click="openEditChannel(ch)">
                    <PencilIcon />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    class="text-muted-foreground hover:text-destructive"
                    title="删除"
                    @click="deleteChannel(ch.id)"
                  >
                    <Trash2Icon />
                  </Button>
                </div>
              </div>
              <CardDescription class="text-xs">
                {{ channelTypeName(ch.type) }}
                <Badge v-if="!ch.enabled" variant="destructive" class="ml-1">已禁用</Badge>
              </CardDescription>
            </CardHeader>
          </Card>

          <Empty v-if="!notifyChannels.length" class="md:col-span-2 lg:col-span-3">
            <EmptyHeader>
              <EmptyMedia variant="icon"><BellIcon /></EmptyMedia>
              <EmptyTitle>暂无通知渠道</EmptyTitle>
              <EmptyDescription>点击上方「添加渠道」开始配置。</EmptyDescription>
            </EmptyHeader>
          </Empty>
        </div>
      </TabsContent>

      <!-- ===================== 安全 ===================== -->
      <TabsContent value="security" class="flex flex-col gap-4">
        <PageSection title="安全" description="修改控制台登录密码" />

        <Card>
          <CardHeader>
            <CardTitle class="flex items-center gap-2">
              <KeyRoundIcon class="text-primary size-5" />
              修改登录密码
            </CardTitle>
            <CardDescription>为了系统安全，请定期修改控制台登录密码</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldGroup class="gap-4 md:grid md:grid-cols-3">
              <Field>
                <FieldLabel for="current-pw">当前密码</FieldLabel>
                <Input id="current-pw" v-model="currentPassword" type="password" placeholder="当前登录密码" />
              </Field>
              <Field>
                <FieldLabel for="new-pw">新密码</FieldLabel>
                <Input id="new-pw" v-model="newPassword" type="password" placeholder="新登录密码" />
              </Field>
              <Field>
                <FieldLabel for="confirm-pw">确认新密码</FieldLabel>
                <div class="flex items-center gap-3">
                  <Input id="confirm-pw" v-model="confirmNewPassword" type="password" placeholder="确认新登录密码" class="flex-1" />
                  <Button :disabled="isChangingPassword" @click="changePassword">
                    <Spinner v-if="isChangingPassword" data-icon="inline-start" />
                    修改密码
                  </Button>
                </div>
              </Field>
            </FieldGroup>
          </CardContent>
        </Card>
      </TabsContent>

      <!-- ===================== 搜索下载 ===================== -->
      <TabsContent value="search" class="flex flex-col gap-4">
        <PageSection title="搜索下载" description="定义库来源与搜索开关">
          <Button :disabled="isSaving" @click="saveSettings">
            <Spinner v-if="isSaving" data-icon="inline-start" />
            <SaveIcon v-else data-icon="inline-start" />
            保存设置
          </Button>
        </PageSection>

        <Card>
          <CardHeader>
            <CardTitle class="flex items-center gap-2">
              <PlugIcon class="text-primary size-5" />
              搜索行为
            </CardTitle>
            <CardDescription>
              网络出口在「代理配置」页统一设置；片名识别的开关在搜索页顶部，这里只管定义库来源。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <FieldGroup class="gap-4">
              <Field>
                <FieldLabel>当前代理</FieldLabel>
                <div class="text-muted-foreground flex flex-wrap items-center gap-2 text-xs">
                  <Badge variant="outline">{{ settings.network_proxy || '未配置（直连）' }}</Badge>
                  <span>刮削 {{ settings.network_scrape_via_proxy ? '走代理' : '直连' }}</span>
                  <span>·</span>
                  <span>搜索器 {{ settings.network_search_via_proxy ? '走代理' : '直连' }}</span>
                </div>
              </Field>
            </FieldGroup>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle class="flex items-center gap-2">
              <ServerIcon class="text-primary size-5" />
              定义库来源
            </CardTitle>
            <CardDescription>Cardigann 定义的上游地址与版本。同步失败时会保留本地定义。</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldGroup class="gap-4 md:grid md:grid-cols-2">
              <Field class="md:col-span-2">
                <FieldLabel for="definition-source">定义源地址</FieldLabel>
                <Input id="definition-source" v-model="settings.search_definition_source" placeholder="https://indexers.prowlarr.com" />
              </Field>
              <Field>
                <FieldLabel for="definition-version">定义版本</FieldLabel>
                <Input id="definition-version" v-model="settings.search_definition_version" placeholder="12" />
              </Field>
              <Field orientation="horizontal">
                <Checkbox
                  id="auto-sync"
                  :model-value="settings.search_auto_sync"
                  @update:model-value="settings.search_auto_sync = Boolean($event)"
                />
                <FieldLabel for="auto-sync">启动时自动同步定义库</FieldLabel>
              </Field>
            </FieldGroup>
          </CardContent>
        </Card>
      </TabsContent>

      <!-- ===================== 代理配置 ===================== -->
      <TabsContent value="proxy" class="flex flex-col gap-4">
        <PageSection title="代理配置" description="网络出口，刮削与搜索器各自独立开关">
          <Button :disabled="isSaving" @click="saveSettings">
            <Spinner v-if="isSaving" data-icon="inline-start" />
            <SaveIcon v-else data-icon="inline-start" />
            保存设置
          </Button>
        </PageSection>

        <Alert v-if="searchProxyMsg" :class="searchProxyMsgOk ? 'text-success' : 'text-destructive'">
          <CheckCircle2Icon v-if="searchProxyMsgOk" />
          <CircleAlertIcon v-else />
          <AlertTitle>{{ searchProxyMsgOk ? '代理可用' : '代理不可用' }}</AlertTitle>
          <AlertDescription>{{ searchProxyMsg }}</AlertDescription>
        </Alert>

        <Card>
          <CardHeader>
            <CardTitle class="flex items-center gap-2">
              <GlobeIcon class="text-primary size-5" />
              网络代理
            </CardTitle>
            <CardDescription>
              全局出口。支持 <code>http://</code>、<code>https://</code>、<code>socks5://</code>、<code>socks5h://</code>；留空表示直连。
              刮削（TMDB 与海报下载）和搜索器各自独立开关，因为两者常常需要不同的出口。
              下载器（qBittorrent / Transmission / Deluge / Aria2）始终直连。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <FieldGroup class="gap-4">
              <Field>
                <FieldLabel for="network-proxy">代理地址</FieldLabel>
                <Input id="network-proxy" v-model="settings.network_proxy" placeholder="socks5h://127.0.0.1:10808" />
              </Field>
              <Field orientation="horizontal">
                <Checkbox
                  id="scrape-proxy"
                  :model-value="settings.network_scrape_via_proxy"
                  @update:model-value="settings.network_scrape_via_proxy = Boolean($event)"
                />
                <div class="flex flex-col gap-1">
                  <FieldLabel for="scrape-proxy">刮削走代理</FieldLabel>
                  <FieldDescription>TMDB 元数据与海报/背景图下载。</FieldDescription>
                </div>
              </Field>
              <Field orientation="horizontal">
                <Checkbox
                  id="search-proxy"
                  :model-value="settings.network_search_via_proxy"
                  @update:model-value="settings.network_search_via_proxy = Boolean($event)"
                />
                <div class="flex flex-col gap-1">
                  <FieldLabel for="search-proxy">搜索器走代理</FieldLabel>
                  <FieldDescription>定义源同步与站点搜索。单个站点仍可在「已添加站点」页单独覆盖。</FieldDescription>
                </div>
              </Field>
              <div class="flex flex-wrap items-center gap-2">
                <Button variant="outline" size="sm" :disabled="isTestingProxy" @click="testProxy">
                  <Spinner v-if="isTestingProxy" data-icon="inline-start" />
                  <PlugIcon v-else data-icon="inline-start" />
                  保存并测试代理
                </Button>
                <Button variant="ghost" size="sm" @click="clearProxy">清空代理</Button>
                <span class="text-muted-foreground text-xs">测试会通过此代理访问定义源。</span>
              </div>
            </FieldGroup>
          </CardContent>
        </Card>
      </TabsContent>

      <!-- ===================== MCP / API Key ===================== -->
      <TabsContent value="mcp" class="flex flex-col gap-4">
        <PageSection title="MCP / API Key" description="为 Agent 提供媒体库查询与字幕工具的访问凭证">
          <Button variant="outline" @click="openRecords">
            <HistoryIcon data-icon="inline-start" />
            查看调用记录
          </Button>
        </PageSection>

        <Card>
          <CardContent class="pt-6">
            <p class="flex flex-wrap items-center gap-2 text-sm">
              <BotIcon class="text-primary size-5 shrink-0" />
              <span>MCP 端点（仅 API Key 鉴权，不接受网页登录态）：</span>
              <code class="bg-muted text-primary rounded px-2 py-1 font-mono text-xs">{{ locationOrigin }}/api/v1/mcp</code>
            </p>
            <p class="text-muted-foreground mt-3 text-xs">
              创建 Key 后把完整密钥填入 Agent 的 MCP 配置（<code>Authorization: Bearer &lt;key&gt;</code>）。明文仅在创建时展示一次。
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle class="flex items-center gap-2">
              <PlusIcon class="text-primary size-5" />
              创建 API Key
            </CardTitle>
            <CardDescription>每个 Agent / 用途一个独立 Key，便于按需禁用与审计</CardDescription>
          </CardHeader>
          <CardContent>
            <div class="flex gap-3">
              <Input
                v-model="newKeyName"
                placeholder="用途备注，例如: Claude Code 字幕助手"
                class="flex-1"
                @keyup.enter="createMCPKey"
              />
              <Button :disabled="creatingKey" class="shrink-0" @click="createMCPKey">
                <Spinner v-if="creatingKey" data-icon="inline-start" />
                <KeyRoundIcon v-else data-icon="inline-start" />
                创建
              </Button>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>已有 Key</CardTitle>
          </CardHeader>
          <CardContent class="flex flex-col gap-3">
            <div
              v-for="k in mcpKeys"
              :key="k.id"
              class="bg-muted/40 flex flex-wrap items-center gap-x-4 gap-y-2 rounded-lg border px-4 py-3"
            >
              <div class="min-w-0 flex-1">
                <p class="text-sm font-semibold">{{ k.name }}</p>
                <p class="text-muted-foreground mt-0.5 font-mono text-xs">{{ k.key_prefix }}</p>
              </div>
              <Badge :variant="keyStatusTone(k.status).variant" :class="keyStatusTone(k.status).tone">
                {{ k.status === 'active' ? '启用' : '已禁用' }}
              </Badge>
              <span class="text-muted-foreground text-xs">
                {{ k.last_used_at ? '最近使用: ' + fmtTime(k.last_used_at) : '从未使用' }}
              </span>
              <div class="flex items-center gap-1">
                <Button
                  variant="outline"
                  size="sm"
                  title="复制配置模板（不含明文 Key，需手动填入 API Key）"
                  @click="copyMCPConfig(k)"
                >
                  <CopyIcon data-icon="inline-start" />
                  配置模板
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  :class="k.status === 'active' ? 'text-destructive' : 'text-success'"
                  :disabled="togglingKeyId === k.id"
                  @click="toggleMCPKey(k)"
                >
                  <Spinner v-if="togglingKeyId === k.id" data-icon="inline-start" />
                  {{ k.status === 'active' ? '禁用' : '启用' }}
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  class="text-destructive"
                  title="删除此 Key（历史调用记录保留）"
                  @click="deleteMCPKey(k)"
                >
                  <Trash2Icon />
                </Button>
              </div>
            </div>

            <Empty v-if="!mcpKeys.length">
              <EmptyHeader>
                <EmptyMedia variant="icon"><KeyRoundIcon /></EmptyMedia>
                <EmptyTitle>暂无 API Key</EmptyTitle>
                <EmptyDescription>请先在上方创建。</EmptyDescription>
              </EmptyHeader>
            </Empty>
          </CardContent>
        </Card>

        <Dialog :open="showSecret !== null" @update:open="(value) => { if (!value) closeSecret() }">
          <DialogContent class="sm:max-w-lg">
            <DialogHeader>
              <DialogTitle class="flex items-center gap-2">
                <KeyRoundIcon class="text-primary size-5" />
                Key 创建成功（{{ secretName }}）
              </DialogTitle>
              <DialogDescription class="text-destructive font-semibold">
                明文仅显示这一次，关闭后无法再次查看。请立即复制并妥善保存。
              </DialogDescription>
            </DialogHeader>
            <div class="flex gap-2">
              <Input ref="secretInputRef" :model-value="secretText" readonly class="text-primary flex-1 font-mono" />
              <Button class="shrink-0" @click="copySecret">
                <CopyIcon data-icon="inline-start" />
                复制
              </Button>
            </div>
            <div class="flex gap-2">
              <Button variant="outline" class="flex-1" @click="copyMCPConfig({ key: secretText })">复制 MCP 配置片段</Button>
              <Button variant="outline" class="flex-1" @click="closeSecret">我已保存，关闭</Button>
            </div>
          </DialogContent>
        </Dialog>

        <Card v-if="mcpRecords.length > 0 || recordsTotal > 0">
          <CardHeader>
            <div class="flex flex-wrap items-center justify-between gap-2">
              <CardTitle class="flex items-center gap-2">
                <HistoryIcon class="text-primary size-5" />
                调用记录
              </CardTitle>
              <div class="flex flex-wrap items-center gap-2">
                <Select
                  :model-value="recordsFilterKey === '' ? EMPTY_SELECT_VALUE : recordsFilterKey"
                  @update:model-value="recordsFilterKey = $event === EMPTY_SELECT_VALUE ? '' : Number($event); openRecords()"
                >
                  <SelectTrigger size="sm" class="w-32"><SelectValue placeholder="全部 Key" /></SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem :value="EMPTY_SELECT_VALUE">全部 Key</SelectItem>
                      <SelectItem v-for="k in mcpKeys" :key="k.id" :value="k.id">{{ k.name }}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <Select
                  :model-value="toSelectValue(recordsFilterTool)"
                  @update:model-value="recordsFilterTool = fromSelectValue($event); openRecords()"
                >
                  <SelectTrigger size="sm" class="w-36"><SelectValue placeholder="全部工具" /></SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem :value="EMPTY_SELECT_VALUE">全部工具</SelectItem>
                      <SelectItem
                                          v-for="tool in ['list_media_cards','query_media_list','query_media_subtitles','fetch_subtitle','upload_subtitle']"
                                          :key="tool"
                                          :value="tool"
                                        >{{ TOOL_LABELS[tool] }}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <Button variant="outline" size="icon-sm" @click="openRecords">
                  <Spinner v-if="loadingRecords" />
                  <RefreshCwIcon v-else />
                </Button>
                <Button v-if="recordsTotal > 0" variant="outline" size="sm" class="text-destructive" title="清空全部调用记录" @click="clearMCPRecords">
                  <Trash2Icon data-icon="inline-start" />
                  清空
                </Button>
              </div>
            </div>
          </CardHeader>
          <CardContent>
            <div v-if="loadingRecords" class="flex justify-center py-8">
              <Spinner class="text-primary" />
            </div>
            <div v-else class="flex max-h-96 flex-col gap-2 overflow-y-auto pr-1">
              <div
                v-for="r in mcpRecords"
                :key="r.id"
                class="bg-muted/40 flex flex-wrap items-center gap-x-3 gap-y-1 rounded border px-3 py-2 text-xs"
              >
                <span class="text-muted-foreground font-mono">{{ fmtTime(r.created_at) }}</span>
                <span class="text-primary font-mono">{{ toolLabel(r.tool) }}</span>
                <Badge :variant="r.status === 'ok' ? 'secondary' : 'destructive'" :class="r.status === 'ok' ? 'text-success' : ''">
                  {{ r.status }}
                </Badge>
                <span class="text-muted-foreground">耗时 {{ r.duration_ms }}ms</span>
                <span class="text-muted-foreground">结果 {{ r.result_bytes }}B</span>
                <span class="text-muted-foreground max-w-[30ch] truncate" :title="r.input_meta">{{ r.input_meta }}</span>
              </div>

              <Empty v-if="!mcpRecords.length">
                <EmptyHeader>
                  <EmptyDescription>暂无记录</EmptyDescription>
                </EmptyHeader>
              </Empty>
            </div>

            <div v-if="recordsTotal > 0" class="text-muted-foreground mt-3 flex items-center justify-between text-xs">
              <span>共 {{ recordsTotal }} 条</span>
              <div class="flex items-center gap-1">
                <Button variant="ghost" size="icon-xs" :disabled="recordsPage <= 1" @click="recordsPage--; fetchMCPRecords()">‹</Button>
                <span class="px-1">{{ recordsPage }}</span>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  :disabled="recordsPage * recordsLimit >= recordsTotal"
                  @click="recordsPage++; fetchMCPRecords()"
                >›</Button>
              </div>
            </div>
          </CardContent>
        </Card>
      </TabsContent>
    </Tabs>
  </div>
</template>
