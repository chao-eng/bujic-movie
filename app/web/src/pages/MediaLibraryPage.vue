<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import client from '@/api/client'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Search, Film, Database, Loader2, RefreshCw, Folder, Upload, X, ArrowLeft, Check, AlertCircle, Trash2, ChevronDown, ChevronUp } from 'lucide-vue-next'
import { toast } from 'vue-sonner'

const cards = ref<any[]>([])
const selectedCardId = ref<number | 'all'>('all')

const medias = ref<any[]>([])
const searchQuery = ref('')
const isLoading = ref(false)
const isRefreshing = ref(false)

// Subtitle Dialog state variables
const isDialogOpen = ref(false)
const activeMedia = ref<any>(null)
const episodes = ref<any[]>([])
const isLoadingEpisodes = ref(false)

// Upload state variables (Single)
const singleUploadFile = ref<File | null>(null)
const isUploading = ref(false)

// Batch upload state variables
const isBatchMode = ref(false)
const batchFiles = ref<{ file: File; episodeId: number | null }[]>([])
const isBatchUploading = ref(false)
const batchProgress = ref({ current: 0, total: 0, currentName: '' })

const getPosterURL = (path: string) => {
  if (!path) return 'https://placehold.co/342x513/1e293b/e2e8f0?text=No+Poster'
  if (path.startsWith('http://') || path.startsWith('https://')) {
    return path
  }
  const isTmdPath = path.startsWith('/') && !path.includes('/', 1)
  if (isTmdPath) {
    return `https://image.tmdb.org/t/p/w342${path}`
  }
  const token = localStorage.getItem('token') || ''
  return `/api/v1/media/image?path=${encodeURIComponent(path)}&token=${encodeURIComponent(token)}`
}

const getShortPath = (path: string) => {
  if (!path) return ''
  const parts = path.split(/[/\\]/)
  const cleanParts = parts.filter(p => p !== '')
  if (cleanParts.length === 0) return path
  if (cleanParts.length === 1) return cleanParts[0]
  return '.../' + cleanParts.slice(-2).join('/')
}

// Fetch all media cards
const fetchCards = async () => {
  try {
    const res: any = await client.get('/api/v1/cards')
    if (res.code === 0) {
      cards.value = res.data || []
    }
  } catch (err) {
    console.error('Failed to fetch media cards', err)
  }
}

// Fetch media list filtered by selected card
const fetchMedias = async () => {
  isLoading.value = true
  try {
    let url = '/api/v1/media'
    const params: any = {}
    if (selectedCardId.value !== 'all') {
      params.card_id = selectedCardId.value
    }
    
    const res: any = await client.get(url, { params })
    if (res.code === 0) {
      medias.value = res.data || []
    }
  } catch (err) {
    console.error('Failed to fetch media list', err)
  } finally {
    isLoading.value = false
  }
}

const handleSearch = async () => {
  if (!searchQuery.value) {
    fetchMedias()
    return
  }

  isLoading.value = true
  try {
    let url = `/api/v1/media/search?q=${encodeURIComponent(searchQuery.value)}`
    if (selectedCardId.value !== 'all') {
      url += `&card_id=${selectedCardId.value}`
    }
    const res: any = await client.get(url)
    if (res.code === 0) {
      medias.value = res.data || []
    }
  } catch (err) {
    console.error('Search failed', err)
  } finally {
    isLoading.value = false
  }
}

// Refresh folder scanner for the currently selected card
const handleRefresh = async () => {
  if (selectedCardId.value === 'all') {
    toast.warning('请先选择一个具体的目录卡片进行刷新。')
    return
  }

  const activeCard = cards.value.find((c) => c.id === selectedCardId.value)
  if (!activeCard) return

  isRefreshing.value = true
  const toastId = toast.loading(`正在扫描并同步卡片【${activeCard.name}】的归档目录...`)
  try {
    const res: any = await client.post('/api/v1/media/refresh', {
      card_id: activeCard.id
    })
    
    if (res.code === 0) {
      toast.dismiss(toastId)
      toast.success(res.data.message || '刷新同步成功！')
      fetchMedias()
    } else {
      toast.dismiss(toastId)
      toast.error(res.msg || '刷新同步失败')
    }
  } catch (err: any) {
    toast.dismiss(toastId)
    toast.error(err.response?.data?.msg || err.message || '刷新同步出错，请检查服务器日志')
  } finally {
    isRefreshing.value = false
  }
}

const selectTab = (id: number | 'all') => {
  selectedCardId.value = id
  searchQuery.value = ''
  fetchMedias()
}

const movieSubtitles = ref<any[]>([])
const isLoadingMovieSubtitles = ref(false)

const fetchMovieSubtitles = async (path: string) => {
  isLoadingMovieSubtitles.value = true
  try {
    const res: any = await client.get('/api/v1/media/subtitles', {
      params: { path }
    })
    if (res.code === 0) {
      movieSubtitles.value = res.data || []
    }
  } catch (err) {
    console.error('Failed to fetch movie subtitles', err)
    toast.error('获取电影字幕失败')
  } finally {
    isLoadingMovieSubtitles.value = false
  }
}

const isConfirmOpen = ref(false)
const confirmTitle = ref('')
const confirmMessage = ref('')
let confirmResolver: ((value: boolean) => void) | null = null

const showConfirmDialog = (title: string, message: string): Promise<boolean> => {
  confirmTitle.value = title
  confirmMessage.value = message
  isConfirmOpen.value = true
  return new Promise<boolean>((resolve) => {
    confirmResolver = resolve
  })
}

const handleConfirmYes = () => {
  isConfirmOpen.value = false
  if (confirmResolver) confirmResolver(true)
}

const handleConfirmNo = () => {
  isConfirmOpen.value = false
  if (confirmResolver) confirmResolver(false)
}

const handleDeleteSubtitle = async (subPath: string, isMovie: boolean) => {
  const confirmDel = await showConfirmDialog('确认删除', '确定要删除这个外挂字幕文件吗？')
  if (!confirmDel) return

  try {
    const res: any = await client.post('/api/v1/subtitles/delete', { path: subPath })
    if (res.code === 0) {
      toast.success('字幕删除成功')
      if (isMovie) {
        if (activeMedia.value) {
          await fetchMovieSubtitles(activeMedia.value.path)
        }
      } else {
        await fetchEpisodes()
      }
    } else {
      toast.error(res.msg || '删除字幕失败')
    }
  } catch (err: any) {
    toast.error(err.response?.data?.msg || err.message || '删除字幕出错')
  }
}

const fetchEpisodes = async () => {
  if (!activeMedia.value || activeMedia.value.type !== 'tv') return
  isLoadingEpisodes.value = true
  try {
    const res: any = await client.get('/api/v1/media/episodes', {
      params: {
        tmdb_id: activeMedia.value.tmdb_id,
        season: activeMedia.value.season,
        path: activeMedia.value.path
      }
    })
    if (res.code === 0) {
      episodes.value = res.data || []
    }
  } catch (err) {
    console.error('Failed to fetch episodes', err)
    toast.error('获取剧集列表失败')
  } finally {
    isLoadingEpisodes.value = false
  }
}

const formatLang = (sub: any) => {
  if (!sub) return '未知'
  
  let lang = ''
  let title = ''
  let name = ''
  
  if (typeof sub === 'string') {
    lang = sub.toLowerCase()
  } else {
    lang = (sub.language || '').toLowerCase()
    title = (sub.title || '').toLowerCase()
    name = (sub.name || '').toLowerCase()
  }

  // Helper to check if any string indicates Traditional Chinese
  const isTraditional = (s: string) => {
    return s.includes('tw') || s.includes('hk') || s.includes('hant') || s.includes('zht') || s.includes('繁') || s.includes('traditional') || s.includes('cht')
  }

  // Helper to check if any string indicates Simplified Chinese
  const isSimplified = (s: string) => {
    return s.includes('cn') || s.includes('hans') || s.includes('chs') || s.includes('简') || s.includes('simplified')
  }

  // If language tag or title or filename indicates Traditional
  if (isTraditional(lang) || isTraditional(title) || isTraditional(name)) {
    return '繁体中文'
  }

  // If language tag or title or filename indicates Simplified
  if (isSimplified(lang) || isSimplified(title) || isSimplified(name)) {
    return '简体中文'
  }

  // Fallback for general Chinese codes
  if (lang === 'chi' || lang === 'zho' || lang.includes('zh')) {
    return '中文'
  }

  // General mappings
  if (lang.includes('en') || lang === 'eng') return '英语'
  if (lang.includes('ja') || lang === 'jpn') return '日语'
  if (lang === 'kor') return '韩语'
  if (lang === 'cze') return '捷克语'
  if (lang === 'dan') return '丹麦语'
  if (lang === 'ger') return '德语'
  if (lang === 'gre') return '希腊语'
  if (lang === 'spa') return '西班牙语'
  if (lang === 'fin') return '芬兰语'
  if (lang === 'fre') return '法语'
  if (lang === 'hun') return '匈牙利语'
  if (lang === 'ita') return '意大利语'
  if (lang === 'dut') return '荷兰语'
  if (lang === 'nor') return '挪威语'
  if (lang === 'pol') return '波兰语'
  if (lang === 'por') return '葡萄牙语'
  if (lang === 'rum') return '罗马尼亚语'
  if (lang === 'slo') return '斯洛伐克语'
  if (lang === 'swe') return '瑞典语'
  if (lang === 'tur') return '土耳其语'

  return typeof sub === 'string' ? sub : (sub.language || '未知')
}
const isConverting = ref(false)
const isConvertingBatch = ref(false)
const hasTraditionalSubs = computed(() => {
  if (!episodes.value || episodes.value.length === 0) return false
  return episodes.value.some((ep: any) => 
    ep.subtitles && ep.subtitles.some((sub: any) => formatLang(sub) === '繁体中文')
  )
})

const handleBatchConvertSubtitles = async () => {
  const itemsToConvert: { sub: any; videoPath: string; epTitle: string }[] = []
  for (const ep of episodes.value) {
    if (ep.subtitles && ep.subtitles.length > 0) {
      for (const sub of ep.subtitles) {
        if (formatLang(sub) === '繁体中文') {
          const filename = ep.path.split(/[/\\]/).pop() || ep.path
          itemsToConvert.push({
            sub,
            videoPath: ep.path,
            epTitle: ep.title || filename
          })
        }
      }
    }
  }

  if (itemsToConvert.length === 0) {
    toast.info('当前季所有剧集中未发现可转换的繁体字幕')
    return
  }

  const confirmConv = await showConfirmDialog(
    '确认整季转换',
    `确定要将本季所有剧集的繁体字幕自动转换为简体字幕吗？共发现 ${itemsToConvert.length} 个繁体字幕轨道。这将在各视频同级目录下新建对应的简体外挂字幕文件。`
  )
  if (!confirmConv) return

  isConvertingBatch.value = true
  const toastId = toast.loading(`正在启动整季字幕转换... (0/${itemsToConvert.length})`)
  
  let successCount = 0
  let failCount = 0
  
  try {
    for (let i = 0; i < itemsToConvert.length; i++) {
      const item = itemsToConvert[i]
      toast.loading(`正在转换第 ${i + 1}/${itemsToConvert.length} 个字幕: ${item.epTitle}...`, { id: toastId })
      
      try {
        const res: any = await client.post('/api/v1/subtitles/convert', {
          video_path: item.videoPath,
          subtitle_path: item.sub.type === 'external' ? item.sub.path : '',
          is_internal: item.sub.type === 'internal',
          internal_index: item.sub.type === 'internal' ? item.sub.index : 0
        })
        
        if (res.code === 0) {
          successCount++
        } else {
          failCount++
          console.error(`Failed to convert subtitle for ${item.epTitle}:`, res.msg)
        }
      } catch (err) {
        failCount++
        console.error(`Error converting subtitle for ${item.epTitle}:`, err)
      }
    }
    
    toast.dismiss(toastId)
    if (failCount === 0) {
      toast.success(`整季繁体字幕转换完成！成功转换了 ${successCount} 个字幕。`)
    } else {
      toast.success(`整季字幕转换结束：成功 ${successCount} 个，失败 ${failCount} 个。`)
    }
    
    await fetchEpisodes()
  } catch (err: any) {
    toast.dismiss(toastId)
    toast.error('整季转换过程中出现严重错误: ' + err.message)
  } finally {
    isConvertingBatch.value = false
  }
}

const handleConvertSubtitle = async (sub: any, isMovie: boolean, videoPath: string) => {
  const confirmConv = await showConfirmDialog('确认转换', '确定要将该繁体字幕转换为简体字幕吗？这将新建一个对应的简体外挂字幕文件。')
  if (!confirmConv) return

  isConverting.value = true
  const toastId = toast.loading('正在提取并转换繁体字幕为简体...')
  try {
    const res: any = await client.post('/api/v1/subtitles/convert', {
      video_path: videoPath,
      subtitle_path: sub.type === 'external' ? sub.path : '',
      is_internal: sub.type === 'internal',
      internal_index: sub.type === 'internal' ? sub.index : 0
    })

    if (res.code === 0) {
      toast.dismiss(toastId)
      toast.success('繁体字幕转换成功并保存为简体外挂字幕！')
      if (isMovie) {
        if (activeMedia.value) {
          await fetchMovieSubtitles(activeMedia.value.path)
        }
      } else {
        await fetchEpisodes()
      }
    } else {
      toast.dismiss(toastId)
      toast.error(res.msg || '转换字幕失败')
    }
  } catch (err: any) {
    toast.dismiss(toastId)
    toast.error(err.response?.data?.msg || err.message || '转换字幕出错')
  } finally {
    isConverting.value = false
  }
}
const expandedEpisodeId = ref<number | null>(null)
const toggleExpandEpisode = (id: number) => {
  if (expandedEpisodeId.value === id) {
    expandedEpisodeId.value = null
  } else {
    expandedEpisodeId.value = id
  }
}

// Manage subtitle dialog triggers
const openManageDialog = async (media: any) => {
  activeMedia.value = media
  isDialogOpen.value = true
  isBatchMode.value = false
  batchFiles.value = []
  singleUploadFile.value = null
  episodes.value = []
  movieSubtitles.value = []
  expandedEpisodeId.value = null
  
  if (media.type === 'tv') {
    await fetchEpisodes()
  } else if (media.type === 'movie') {
    await fetchMovieSubtitles(media.path)
  }
}

const closeDialog = () => {
  if (isUploading.value || isBatchUploading.value) return
  isDialogOpen.value = false
  activeMedia.value = null
}

// Single Upload Handler
const triggerSingleFileSelect = (inputId: string) => {
  const el = document.getElementById(inputId) as HTMLInputElement
  if (el) el.click()
}

const handleSingleFileChange = (e: Event, videoPath: string, isMovie = true) => {
  const target = e.target as HTMLInputElement
  if (target.files && target.files.length > 0) {
    uploadSingleSubtitle(target.files[0], videoPath, isMovie)
  }
}

const uploadSingleSubtitle = async (file: File, videoPath: string, isMovie = true) => {
  isUploading.value = true
  const toastId = toast.loading(`正在上传字幕【${file.name}】...`)
  try {
    const formData = new FormData()
    formData.append('video_path', videoPath)
    formData.append('file', file)

    const res: any = await client.post('/api/v1/subtitles/upload', formData, {
      headers: {
        'Content-Type': 'multipart/form-data'
      }
    })

    if (res.code === 0) {
      toast.dismiss(toastId)
      toast.success(`字幕上传成功并保存：${file.name}`)
      if (isMovie) {
        await fetchMovieSubtitles(videoPath)
      } else {
        await fetchEpisodes()
      }
    } else {
      toast.dismiss(toastId)
      toast.error(res.msg || '字幕上传失败')
    }
  } catch (err: any) {
    toast.dismiss(toastId)
    toast.error(err.response?.data?.msg || err.message || '上传字幕出错')
  } finally {
    isUploading.value = false
    singleUploadFile.value = null
  }
}

// Batch Subtitles Logic
const triggerBatchFilesSelect = () => {
  const el = document.getElementById('batchFileInput') as HTMLInputElement
  if (el) el.click()
}

const handleBatchFilesChange = (e: Event) => {
  const target = e.target as HTMLInputElement
  if (target.files) {
    addFilesToBatch(Array.from(target.files))
  }
}

// Extract episode number from filename (e.g. S01E03 -> 3, ep02 -> 2, 05 -> 5)
const guessEpisodeNumber = (fileName: string): number | null => {
  const lower = fileName.toLowerCase()
  
  // 1. Matches E03 or EP03 or e03
  const epMatch = lower.match(/(?:ep?|e|集)\s*(\d+)/)
  if (epMatch) {
    return parseInt(epMatch[1], 10)
  }

  // 2. Matches S01E03
  const sMatch = lower.match(/s\d+e(\d+)/)
  if (sMatch) {
    return parseInt(sMatch[1], 10)
  }

  // 3. Match pure numbers inside file base (e.g. "赌金 02.srt")
  const pureNumMatch = lower.match(/(?:\D|^)(\d{2})(?:\D|$)/)
  if (pureNumMatch) {
    return parseInt(pureNumMatch[1], 10)
  }

  return null
}

const addFilesToBatch = (files: File[]) => {
  files.forEach((file) => {
    if (!batchFiles.value.some((bf) => bf.file.name === file.name)) {
      const guessedEpNum = guessEpisodeNumber(file.name)
      let matchedEpisodeId: number | null = null
      
      if (guessedEpNum !== null) {
        const matchedEp = episodes.value.find((ep) => {
          const match = ep.path.toLowerCase().match(/(?:ep?|e|集)\s*(\d+)/)
          if (match && parseInt(match[1], 10) === guessedEpNum) {
            return true
          }
          const sMatch = ep.path.toLowerCase().match(/s\d+e(\d+)/)
          if (sMatch && parseInt(sMatch[1], 10) === guessedEpNum) {
            return true
          }
          return false
        })
        if (matchedEp) {
          matchedEpisodeId = matchedEp.id
        }
      }

      batchFiles.value.push({
        file,
        episodeId: matchedEpisodeId
      })
    }
  })
}

// Drag & Drop Handlers
const handleDragOver = (e: DragEvent) => {
  e.preventDefault()
}

const handleDrop = (e: DragEvent, type: 'single' | 'batch', videoPath?: string) => {
  e.preventDefault()
  if (e.dataTransfer && e.dataTransfer.files) {
    const files = Array.from(e.dataTransfer.files)
    if (files.length === 0) return

    if (type === 'single' && videoPath) {
      uploadSingleSubtitle(files[0], videoPath)
    } else if (type === 'batch') {
      addFilesToBatch(files)
    }
  }
}

const removeFileFromBatch = (index: number) => {
  batchFiles.value.splice(index, 1)
}

// Upload mapped batch subtitles sequentially
const startBatchUpload = async () => {
  const validUploads = batchFiles.value.filter((bf) => bf.episodeId !== null)
  if (validUploads.length === 0) {
    toast.error('未绑定有效的剧集，请先选择对应的集数。')
    return
  }

  isBatchUploading.value = true
  batchProgress.value = { current: 0, total: validUploads.length, currentName: '' }

  let successCount = 0
  let failCount = 0

  for (let i = 0; i < validUploads.length; i++ ) {
    const item = validUploads[i]
    const matchedEp = episodes.value.find((ep) => ep.id === item.episodeId)
    if (!matchedEp) continue

    batchProgress.value.current = i + 1
    batchProgress.value.currentName = item.file.name

    try {
      const formData = new FormData()
      formData.append('video_path', matchedEp.path)
      formData.append('file', item.file)

      const res: any = await client.post('/api/v1/subtitles/upload', formData, {
        headers: {
          'Content-Type': 'multipart/form-data'
        }
      })

      if (res.code === 0) {
        successCount++
      } else {
        failCount++
      }
    } catch (err) {
      failCount++
    }
    
    await new Promise((resolve) => setTimeout(resolve, 500))
  }

  isBatchUploading.value = false
  isBatchMode.value = false
  batchFiles.value = []
  await fetchEpisodes()
  
  if (failCount === 0) {
    toast.success(`批量上传成功！共上传 ${successCount} 个字幕。`)
  } else {
    toast.info(`批量上传完成：成功 ${successCount} 个，失败 ${failCount} 个。`)
  }
}

onMounted(async () => {
  await fetchCards()
  await fetchMedias()
})
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col lg:flex-row lg:items-center justify-between gap-6">
      <div>
        <h1 class="text-3xl font-extrabold tracking-tight text-slate-100">媒体库</h1>
        <p class="text-slate-400 mt-1">浏览及管理归档目录中的所有影视资源。</p>
      </div>

      <!-- Search Bar -->
      <div class="flex items-center gap-2 max-w-md w-full">
        <div class="relative flex-1">
          <Search class="absolute left-3 h-4 w-4 text-slate-500 top-1/2 -translate-y-1/2" />
          <Input
            v-model="searchQuery"
            @keyup.enter="handleSearch"
            placeholder="搜索影片名称..."
            class="bg-slate-900 border-slate-800 text-slate-100 pl-10"
          />
        </div>
        <Button @click="handleSearch" class="bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold">
          搜索
        </Button>
      </div>
    </div>

    <!-- Media Card Filter Tabs & Refresh Button -->
    <div class="flex flex-col sm:flex-row sm:items-center justify-between border-b border-slate-800/80 pb-3 gap-4">
      <!-- Tabs Selector -->
      <div class="flex flex-wrap gap-2">
        <button
          @click="selectTab('all')"
          :class="[
            'px-4 py-2 rounded-lg text-sm font-medium transition-all duration-200 border',
            selectedCardId === 'all'
              ? 'bg-amber-500/10 text-amber-500 border-amber-500/30'
              : 'border-slate-800 bg-slate-900/55 text-slate-400 hover:text-slate-200 hover:border-slate-700'
          ]"
        >
          全部卡片
        </button>

        <button
          v-for="card in cards"
          :key="card.id"
          @click="selectTab(card.id)"
          :class="[
            'px-4 py-2 rounded-lg text-sm font-medium transition-all duration-200 border flex items-center gap-2',
            selectedCardId === card.id
              ? 'bg-amber-500/10 text-amber-500 border-amber-500/30 font-bold'
              : 'border-slate-800 bg-slate-900/55 text-slate-400 hover:text-slate-200 hover:border-slate-700'
          ]"
        >
          <Folder class="h-4 w-4 shrink-0 text-slate-400" />
          <span>{{ card.name }}</span>
          <span class="text-[10px] bg-slate-950/50 border border-slate-800 px-1.5 py-0.5 rounded text-slate-500 font-normal">
            {{ card.media_type === 'movie' ? '电影' : '电视剧' }}
          </span>
        </button>
      </div>

      <!-- Sync Refresh Button -->
      <Button
        v-if="selectedCardId !== 'all'"
        @click="handleRefresh"
        :disabled="isRefreshing"
        class="bg-slate-900 hover:bg-slate-800 text-slate-200 border border-slate-800 font-medium text-xs py-2 px-4 rounded-lg flex items-center gap-2 cursor-pointer transition-colors duration-200"
      >
        <RefreshCw :class="['h-3.5 w-3.5 text-amber-500', isRefreshing ? 'animate-spin' : '']" />
        <span>{{ isRefreshing ? '正在同步...' : '扫描刷新目录' }}</span>
      </Button>
    </div>

    <!-- Media Grid Loading state -->
    <div v-if="isLoading" class="flex items-center justify-center py-24">
      <Loader2 class="h-8 w-8 text-amber-500 animate-spin" />
    </div>

    <!-- Media Poster Grid -->
    <div v-else-if="medias.length > 0" class="grid grid-cols-2 sm:grid-cols-[repeat(auto-fill,minmax(160px,1fr))] gap-6">
      <Card
        v-for="item in medias"
        :key="item.id"
        class="bg-slate-900 border-slate-800 text-slate-100 overflow-hidden group hover:border-amber-500/50 transition-all duration-300 flex flex-col justify-between"
      >
        <div class="relative aspect-[2/3] overflow-hidden bg-slate-950">
          <img
            :src="getPosterURL(item.poster_path)"
            :alt="item.title"
            class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-300"
            loading="lazy"
          />
          <!-- Media Type Badge -->
          <div class="absolute top-2 right-2 bg-slate-950/80 border border-slate-800 px-2 py-0.5 rounded text-[10px] font-bold tracking-wide flex items-center gap-1">
            <Film v-if="item.type === 'movie'" class="h-3 w-3 text-amber-500" />
            <Database v-else class="h-3 w-3 text-blue-500" />
            <span>{{ item.type === 'movie' ? '电影' : '剧集' }}</span>
          </div>
        </div>

        <CardContent class="p-3 space-y-1 flex-1 flex flex-col justify-between">
          <div>
            <h3 class="font-bold text-sm text-slate-100 truncate" :title="item.title">{{ item.title }}</h3>
            <span class="text-xs text-slate-400">{{ item.year || '未知年份' }}</span>
          </div>
          <div class="pt-3 border-t border-slate-800/50 mt-2 space-y-2">
            <div class="flex items-center gap-1 text-slate-500" :title="item.path">
              <Folder class="h-3.5 w-3.5 shrink-0" />
              <span class="text-[10px] truncate">{{ getShortPath(item.path) }}</span>
            </div>
            <Button
              @click="openManageDialog(item)"
              class="w-full bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold text-xs py-1.5 px-2 rounded flex items-center justify-center gap-1 cursor-pointer"
            >
              <Upload class="h-3 w-3" />
              <span>字幕管理</span>
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>

    <!-- Empty State -->
    <div v-else class="border border-dashed border-slate-800 rounded-lg h-96 flex flex-col items-center justify-center text-slate-500">
      <Film class="h-12 w-12 text-slate-600 mb-3" />
      <span class="text-lg font-semibold mb-2">暂无媒体记录</span>
      <span v-if="selectedCardId === 'all'">数据库中没有媒体记录。</span>
      <div v-else class="text-center flex flex-col items-center gap-2">
        <span>当前目录卡片中没有任何导入的媒体。</span>
        <Button @click="handleRefresh" :disabled="isRefreshing" class="mt-2 bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold flex items-center gap-2 py-1 px-3 text-xs">
          <RefreshCw :class="['h-3.5 w-3.5', isRefreshing ? 'animate-spin' : '']" />
          立即扫描文件夹目录
        </Button>
      </div>
    </div>

    <!-- Subtitle Upload & Management Dialog -->
    <div
      v-if="isDialogOpen"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/75 backdrop-blur-sm p-4 overflow-y-auto animate-in fade-in duration-200"
    >
      <div
        class="bg-slate-900 border border-slate-800 rounded-2xl w-full max-w-4xl shadow-2xl overflow-hidden flex flex-col max-h-[85vh]"
      >
        <!-- Dialog Header -->
        <div class="p-6 border-b border-slate-800 flex items-center justify-between">
          <div class="flex items-center gap-3">
            <Film v-if="activeMedia.type === 'movie'" class="h-6 w-6 text-amber-500" />
            <Database v-else class="h-6 w-6 text-blue-500" />
            <div>
              <h2 class="text-xl font-bold text-slate-100">{{ activeMedia.title }}</h2>
              <p class="text-xs text-slate-400 truncate max-w-[500px] mt-0.5">{{ activeMedia.path }}</p>
            </div>
          </div>
          <button
            @click="closeDialog"
            class="text-slate-400 hover:text-slate-100 hover:bg-slate-800 p-1.5 rounded-lg transition-colors cursor-pointer"
            :disabled="isUploading || isBatchUploading"
          >
            <X class="h-5 w-5" />
          </button>
        </div>

        <!-- Dialog Content -->
        <div class="flex-1 overflow-y-auto p-6">
          <!-- 1. MOVIE MODE -->
          <div v-if="activeMedia.type === 'movie'" class="space-y-6">
            <div class="bg-slate-950/40 border border-slate-800 rounded-xl p-4 flex items-start gap-3">
              <AlertCircle class="h-5 w-5 text-amber-500 shrink-0 mt-0.5" />
              <div class="text-xs text-slate-400 space-y-1">
                <p class="font-semibold text-slate-300">手动上传指南：</p>
                <p>1. 支持的字幕类型：.srt, .ass, .ssa, .sbv, .webvtt</p>
                <p>2. 系统会自动提取字幕文件名中的语言标记（如中英、简、繁、Eng等），并重命名为 `电影名.语言.后缀` 进行同级放置。</p>
              </div>
            </div>

            <!-- Existing Subtitles Section -->
            <div class="space-y-3">
              <h3 class="text-sm font-semibold text-slate-200 flex items-center gap-2">
                <Database class="h-4 w-4 text-amber-500" />
                <span>已有字幕列表</span>
              </h3>
              
              <div v-if="isLoadingMovieSubtitles" class="flex justify-center py-4">
                <Loader2 class="h-5 w-5 text-amber-500 animate-spin" />
              </div>
              <div v-else-if="movieSubtitles.length > 0" class="grid grid-cols-1 sm:grid-cols-2 gap-3 max-h-48 overflow-y-auto pr-1">
                <div
                  v-for="sub in movieSubtitles"
                  :key="sub.path || sub.name"
                  class="bg-slate-950/40 border border-slate-800 rounded-xl p-3 flex items-center justify-between gap-3 hover:border-slate-700/60 transition-colors"
                >
                  <div class="flex items-center gap-2 min-w-0">
                    <span
                      :class="[
                        'text-[10px] font-bold px-1.5 py-0.5 rounded shrink-0 border',
                        sub.type === 'external' ? 'bg-amber-500/10 text-amber-500 border-amber-500/20' : 'bg-blue-500/10 text-blue-500 border-blue-500/20'
                      ]"
                    >
                      {{ sub.type === 'external' ? '外挂' : '内嵌' }}
                    </span>
                    <div class="truncate">
                      <p class="text-xs font-semibold text-slate-200 truncate" :title="sub.name">{{ sub.name }}</p>
                      <p class="text-[10px] text-slate-400 mt-0.5">语言: <span class="text-slate-300">{{ formatLang(sub) }}</span> | 格式: {{ sub.format }}</p>
                    </div>
                  </div>
                  <div class="flex items-center gap-1.5 shrink-0">
                    <button
                      v-if="formatLang(sub) === '繁体中文'"
                      @click="handleConvertSubtitle(sub, true, activeMedia.path)"
                      :disabled="isConverting"
                      class="text-xs bg-amber-500/10 text-amber-500 hover:bg-amber-500/20 border border-amber-500/20 px-2 py-1 rounded transition-colors cursor-pointer"
                      title="转换为简体字幕"
                    >
                      繁转简
                    </button>
                    <button
                      v-if="sub.type === 'external'"
                      @click="handleDeleteSubtitle(sub.path, true)"
                      class="text-slate-500 hover:text-rose-500 p-1.5 rounded-lg hover:bg-rose-500/10 transition-all cursor-pointer shrink-0"
                      title="删除外挂字幕"
                    >
                      <Trash2 class="h-4 w-4" />
                    </button>
                  </div>
                </div>
              </div>
              <div v-else class="text-xs text-slate-500 bg-slate-950/20 border border-slate-800/60 rounded-xl p-4 text-center">
                暂无已有字幕。请在下方上传。
              </div>
            </div>

            <!-- Drag & Drop Zone -->
            <div
              @dragover="handleDragOver"
              @drop="(e) => handleDrop(e, 'single', activeMedia.path)"
              class="border-2 border-dashed border-slate-800 rounded-xl h-48 flex flex-col items-center justify-center p-6 bg-slate-950/20 hover:border-amber-500/50 transition-colors"
            >
              <Upload class="h-10 w-10 text-slate-500 mb-3" />
              <p class="text-slate-300 font-semibold mb-1 text-sm">拖拽字幕文件到此处上传</p>
              <p class="text-slate-500 text-xs mb-2">或</p>
              <input
                id="singleSubInput"
                type="file"
                class="hidden"
                accept=".srt,.ass,.ssa,.sbv,.webvtt"
                @change="(e) => handleSingleFileChange(e, activeMedia.path)"
              />
              <Button
                @click="triggerSingleFileSelect('singleSubInput')"
                :disabled="isUploading"
                class="bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold text-xs py-1 px-3"
              >
                选择本地字幕
              </Button>
            </div>

            <div v-if="isUploading" class="flex items-center justify-center gap-3 py-4 text-amber-500">
              <Loader2 class="h-5 w-5 animate-spin" />
              <span class="text-sm font-semibold">正在上传处理中，请稍候...</span>
            </div>
          </div>

          <!-- 2. TV SHOW SEASON MODE -->
          <div v-else-if="activeMedia.type === 'tv'">
            <!-- Loading state -->
            <div v-if="isLoadingEpisodes" class="flex flex-col items-center justify-center py-20 gap-3">
              <Loader2 class="h-8 w-8 text-amber-500 animate-spin" />
              <span class="text-sm text-slate-500">正在加载剧集列表...</span>
            </div>

            <!-- Standard List Mode -->
            <div v-else-if="!isBatchMode" class="space-y-4">
              <!-- Header action bar -->
              <div class="flex items-center justify-between bg-slate-950/40 p-4 border border-slate-800 rounded-xl">
                <div>
                  <h3 class="text-sm font-semibold text-slate-200">电视剧整季字幕整理</h3>
                  <p class="text-xs text-slate-500 mt-0.5">共有 {{ episodes.length }} 集</p>
                </div>
                <div class="flex items-center gap-2">
                  <Button
                    v-if="hasTraditionalSubs"
                    @click="handleBatchConvertSubtitles"
                    :disabled="isConvertingBatch"
                    class="bg-yellow-600 hover:bg-yellow-700 text-slate-950 font-bold flex items-center gap-2"
                  >
                    <RefreshCw :class="['h-4 w-4', isConvertingBatch ? 'animate-spin' : '']" />
                    <span>一键整季繁转简</span>
                  </Button>
                  <Button
                    @click="isBatchMode = true"
                    class="bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold flex items-center gap-2"
                  >
                    <Upload class="h-4 w-4" />
                    <span>批量上传整季字幕</span>
                  </Button>
                </div>
              </div>

              <!-- Episode rows -->
              <div class="border border-slate-800 rounded-xl overflow-hidden divide-y divide-slate-800">
                <div
                  v-for="(ep, index) in episodes"
                  :key="ep.id"
                  class="flex flex-col hover:bg-slate-950/5 transition-colors"
                >
                  <!-- Episode Header Row -->
                  <div class="p-4 flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                    <div class="min-w-0 flex-1">
                      <div class="flex items-center flex-wrap gap-2">
                        <span class="text-xs bg-slate-800 text-slate-300 font-bold px-2 py-0.5 rounded">第 {{ index + 1 }} 集</span>
                        <span class="text-sm font-semibold text-slate-200 truncate">{{ ep.title || '单集' }}</span>
                      </div>
                      
                      <!-- Subtitle Badges -->
                      <div class="flex flex-wrap gap-1 mt-2">
                        <span
                          v-for="sub in ep.subtitles"
                          :key="sub.path || sub.name"
                          :class="[
                            'text-[10px] px-1.5 py-0.5 rounded font-medium cursor-pointer transition-colors border',
                            sub.type === 'external' ? 'bg-amber-500/10 text-amber-500 border-amber-500/20 hover:bg-amber-500/20' : 'bg-blue-500/10 text-blue-500 border-blue-500/20 hover:bg-blue-500/20'
                          ]"
                          @click="toggleExpandEpisode(ep.id)"
                        >
                          {{ sub.type === 'external' ? '外挂' : '内嵌' }}·{{ formatLang(sub) }}
                        </span>
                        <span v-if="!ep.subtitles || ep.subtitles.length === 0" class="text-[10px] text-slate-500 italic">
                          暂无已有字幕
                        </span>
                      </div>
                      <p class="text-[10px] text-slate-500 mt-1 truncate max-w-[450px]" :title="ep.path">{{ ep.path }}</p>
                    </div>
                    
                    <div class="flex items-center gap-2 shrink-0">
                      <input
                        :id="'subInput_' + ep.id"
                        type="file"
                        class="hidden"
                        accept=".srt,.ass,.ssa,.sbv,.webvtt"
                        @change="(e) => handleSingleFileChange(e, ep.path, false)"
                      />
                      <Button
                        @click="triggerSingleFileSelect('subInput_' + ep.id)"
                        :disabled="isUploading"
                        class="bg-slate-800 hover:bg-slate-700 text-slate-200 border border-slate-700 text-xs py-1.5 px-3 rounded flex items-center gap-1.5 cursor-pointer"
                      >
                        <Upload class="h-3.5 w-3.5 text-amber-500" />
                        <span>上传字幕</span>
                      </Button>
                      
                      <Button
                        @click="toggleExpandEpisode(ep.id)"
                        class="bg-slate-900 hover:bg-slate-800 text-slate-400 border border-slate-800 text-xs py-1.5 px-2 rounded flex items-center gap-1 cursor-pointer"
                        title="查看/管理字幕"
                      >
                        <ChevronDown v-if="expandedEpisodeId !== ep.id" class="h-3.5 w-3.5" />
                        <ChevronUp v-else class="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </div>

                  <!-- Expanded Subtitle Management for Episode -->
                  <div
                    v-if="expandedEpisodeId === ep.id"
                    class="px-4 pb-4 pt-2 bg-slate-950/20 border-t border-slate-800/50 space-y-2.5"
                  >
                    <div class="text-xs font-semibold text-slate-400">本集字幕详情</div>
                    
                    <div v-if="ep.subtitles && ep.subtitles.length > 0" class="grid grid-cols-1 sm:grid-cols-2 gap-2.5">
                      <div
                        v-for="sub in ep.subtitles"
                        :key="sub.path || sub.name"
                        class="bg-slate-950/60 border border-slate-800/80 rounded-lg p-2.5 flex items-center justify-between gap-3 hover:border-slate-700/60 transition-colors"
                      >
                        <div class="flex items-center gap-2 min-w-0">
                          <span
                            :class="[
                              'text-[9px] font-bold px-1.5 py-0.5 rounded shrink-0 border',
                              sub.type === 'external' ? 'bg-amber-500/10 text-amber-500 border-amber-500/20' : 'bg-blue-500/10 text-blue-500 border-blue-500/20'
                            ]"
                          >
                            {{ sub.type === 'external' ? '外挂' : '内嵌' }}
                          </span>
                          <div class="truncate">
                            <p class="text-xs font-semibold text-slate-300 truncate" :title="sub.name">{{ sub.name }}</p>
                            <p class="text-[9px] text-slate-500 mt-0.5">语言: {{ formatLang(sub) }} | 格式: {{ sub.format }}</p>
                          </div>
                        </div>
                        <div class="flex items-center gap-1.5 shrink-0">
                          <button
                            v-if="formatLang(sub) === '繁体中文'"
                            @click="handleConvertSubtitle(sub, false, ep.path)"
                            :disabled="isConverting"
                            class="text-[10px] bg-amber-500/10 text-amber-500 hover:bg-amber-500/20 border border-amber-500/20 px-1.5 py-0.5 rounded transition-colors cursor-pointer"
                            title="转换为简体字幕"
                          >
                            繁转简
                          </button>
                          <button
                            v-if="sub.type === 'external'"
                            @click="handleDeleteSubtitle(sub.path, false)"
                            class="text-slate-500 hover:text-rose-500 p-1.5 rounded-lg hover:bg-rose-500/10 transition-all cursor-pointer shrink-0"
                            title="删除外挂字幕"
                          >
                            <Trash2 class="h-3.5 w-3.5" />
                          </button>
                        </div>
                      </div>
                    </div>
                    <div v-else class="text-xs text-slate-500 italic p-2 text-center bg-slate-950/10 rounded">
                      暂无字幕，请在右侧点击“上传字幕”添加。
                    </div>
                  </div>
                </div>
              </div>
            </div>

            <!-- Batch Mode Interface -->
            <div v-else class="space-y-6">
              <div class="flex items-center gap-3 pb-3 border-b border-slate-800">
                <button
                  @click="isBatchMode = false"
                  class="text-slate-400 hover:text-slate-100 flex items-center gap-1.5 text-sm cursor-pointer"
                  :disabled="isBatchUploading"
                >
                  <ArrowLeft class="h-4 w-4" />
                  <span>返回剧集列表</span>
                </button>
                <span class="text-slate-500">|</span>
                <span class="text-sm font-bold text-slate-200">批量上传整季字幕</span>
              </div>

              <!-- Batch Drag & Drop Zone -->
              <div
                @dragover="handleDragOver"
                @drop="(e) => handleDrop(e, 'batch')"
                class="border-2 border-dashed border-slate-800 rounded-xl p-6 flex flex-col items-center justify-center bg-slate-950/20 hover:border-amber-500/50 transition-colors"
              >
                <Upload class="h-10 w-10 text-slate-500 mb-3" />
                <p class="text-sm text-slate-300 font-semibold mb-1">拖拽本季多个字幕文件到此处</p>
                <p class="text-xs text-slate-500 mb-3">支持文件名含类似 E01、S01E02、03 等集数编号进行自动识别匹配</p>
                <input
                  id="batchFileInput"
                  type="file"
                  multiple
                  class="hidden"
                  accept=".srt,.ass,.ssa,.sbv,.webvtt"
                  @change="handleBatchFilesChange"
                />
                <Button
                  @click="triggerBatchFilesSelect"
                  :disabled="isBatchUploading"
                  class="bg-amber-500 hover:bg-amber-600 text-slate-950 font-semibold text-xs py-1.5 px-4"
                >
                  多选本地字幕
                </Button>
              </div>

              <!-- Batch Files List & Matching -->
              <div v-if="batchFiles.length > 0" class="space-y-4">
                <div class="flex items-center justify-between">
                  <h4 class="text-xs font-bold text-slate-400">已选字幕与剧集匹配表</h4>
                  <span class="text-[10px] text-slate-500">未指定匹配集的字幕文件不会上传</span>
                </div>

                <div class="border border-slate-800 rounded-xl overflow-hidden divide-y divide-slate-800 max-h-80 overflow-y-auto">
                  <div
                    v-for="(item, index) in batchFiles"
                    :key="index"
                    class="p-3 flex items-center justify-between gap-4 hover:bg-slate-950/10 transition-colors"
                  >
                    <span class="text-xs text-slate-300 truncate max-w-[350px] font-mono" :title="item.file.name">
                      {{ item.file.name }}
                    </span>
                    <div class="flex items-center gap-3">
                      <!-- Episode Dropdown Selector -->
                      <select
                        v-model="item.episodeId"
                        class="bg-slate-950 border border-slate-800 text-slate-300 rounded px-2.5 py-1 text-xs focus:outline-none focus:border-amber-500"
                        :disabled="isBatchUploading"
                      >
                        <option :value="null">-- 选择对应集 --</option>
                        <option v-for="(ep, epIdx) in episodes" :key="ep.id" :value="ep.id">
                          第 {{ epIdx + 1 }} 集 - {{ ep.title || '单集' }}
                        </option>
                      </select>
                      <button
                        @click="removeFileFromBatch(index)"
                        class="text-slate-500 hover:text-rose-500 p-1 cursor-pointer"
                        :disabled="isBatchUploading"
                      >
                        <X class="h-4 w-4" />
                      </button>
                    </div>
                  </div>
                </div>

                <!-- Progress bar -->
                <div v-if="isBatchUploading" class="bg-slate-950 border border-slate-800 rounded-xl p-4 space-y-2.5">
                  <div class="flex justify-between text-xs font-bold text-amber-500">
                    <span>正在批量上传 ({{ batchProgress.current }} / {{ batchProgress.total }})...</span>
                    <span>{{ Math.round((batchProgress.current / batchProgress.total) * 100) }}%</span>
                  </div>
                  <div class="w-full bg-slate-800 h-2 rounded-full overflow-hidden">
                    <div
                      class="bg-amber-500 h-full transition-all duration-300"
                      :style="{ width: (batchProgress.current / batchProgress.total) * 100 + '%' }"
                    ></div>
                  </div>
                  <p class="text-[10px] text-slate-500 truncate">当前文件: {{ batchProgress.currentName }}</p>
                </div>

                <!-- Actions -->
                <div class="flex justify-end gap-2.5 pt-3">
                  <Button
                    @click="isBatchMode = false"
                    :disabled="isBatchUploading"
                    class="bg-slate-800 hover:bg-slate-700 text-slate-200 border border-slate-700 font-semibold"
                  >
                    取消
                  </Button>
                  <Button
                    @click="startBatchUpload"
                    :disabled="isBatchUploading"
                    class="bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold flex items-center gap-1.5"
                  >
                    <Check class="h-4 w-4" />
                    <span>确认开始上传</span>
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- Dialog Footer -->
        <div class="p-6 border-t border-slate-800 flex justify-end gap-3 bg-slate-950/20">
          <Button
            @click="closeDialog"
            :disabled="isUploading || isBatchUploading"
            class="bg-slate-800 hover:bg-slate-700 text-slate-200 border border-slate-700 font-semibold"
          >
            关闭面板
          </Button>
        </div>
      </div>
    </div>
    <!-- Custom Confirmation Dialog -->
    <div
      v-if="isConfirmOpen"
      class="fixed inset-0 z-[100] flex items-center justify-center bg-slate-950/80 backdrop-blur-sm p-4 animate-in fade-in duration-200"
    >
      <div class="bg-slate-900 border border-slate-800 rounded-2xl w-full max-w-md shadow-2xl p-6 space-y-4">
        <h3 class="text-lg font-bold text-slate-100">{{ confirmTitle }}</h3>
        <p class="text-sm text-slate-400 leading-relaxed">{{ confirmMessage }}</p>
        <div class="flex justify-end gap-3 pt-2">
          <Button
            @click="handleConfirmNo"
            class="bg-slate-800 hover:bg-slate-700 text-slate-200 border border-slate-700 text-xs py-1.5 px-4 rounded font-semibold cursor-pointer"
          >
            取消
          </Button>
          <Button
            @click="handleConfirmYes"
            class="bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold text-xs py-1.5 px-4 rounded cursor-pointer"
          >
            确认
          </Button>
        </div>
      </div>
    </div>
  </div>
</template>
