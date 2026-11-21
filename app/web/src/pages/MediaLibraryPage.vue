<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { toast } from 'vue-sonner'
import {
  ArrowLeftIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronUpIcon,
  DatabaseIcon,
  DownloadIcon,
  FilmIcon,
  FolderIcon,
  InfoIcon,
  RefreshCwIcon,
  SearchIcon,
  Trash2Icon,
  UploadIcon,
  XIcon,
} from 'lucide-vue-next'
import client from '@/api/client'
import { useConfirm } from '@/composables/useConfirm'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import { Progress } from '@/components/ui/progress'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'

const { confirm } = useConfirm()

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

/**
 * 海报地址。
 *
 * 能直接给 <img src> 的只有两种：占位图和外链（TMDB 的 /xxx.jpg）。
 * 服务器本地图片不能这样给——<img> 无法携带 Authorization 头，过去只能把 JWT
 * 拼进 URL query，于是令牌会漏进访问日志、Referer 和浏览器历史。
 * 本地图片改为带鉴权头取回后转成 data URL，见 loadLocalPosters()。
 */
const NO_POSTER = 'https://placehold.co/342x513/1e293b/e2e8f0?text=No+Poster'

const isLocalPoster = (path: string) =>
  Boolean(path) && !/^https?:/i.test(path) && !(path.startsWith('/') && !path.includes('/', 1))

const getPosterURL = (path: string) => {
  if (!path) return NO_POSTER
  if (/^https?:/i.test(path)) return path
  // TMDB 的相对路径（形如 /abc123.jpg）直接走图床。
  if (path.startsWith('/') && !path.includes('/', 1)) {
    return `https://image.tmdb.org/t/p/w342${path}`
  }
  // 本地文件由 loadLocalPosters 填进 posterCache，这里先给占位图。
  return posterCache.value[path] ?? NO_POSTER
}

const posterCache = ref<Record<string, string>>({})
/** 正在取海报，避免重复请求同一张图。 */
const posterPending = new Set<string>()

/**
 * 带鉴权头取回本地海报并缓存为 data URL。
 *
 * 用并发上限避免一次打开几十张图把连接打满；失败就保持占位图，不让一张坏图
 * 影响整个页面。
 */
async function loadLocalPosters(items: any[]) {
  const targets = items.map((item) => item.poster_path).filter(isLocalPoster)
  const queue = [...new Set(targets)].filter((p) => !posterCache.value[p] && !posterPending.has(p))
  if (!queue.length) return

  const LIMIT = 6
  let cursor = 0
  const worker = async () => {
    while (cursor < queue.length) {
      const path = queue[cursor++]
      posterPending.add(path)
      try {
        const resp = await client.get('/api/v1/media/image', { params: { path }, responseType: 'blob' })
        const blob = new Blob([resp as any])
        posterCache.value[path] = await blobToDataURL(blob)
      } catch {
        // 单张失败保持占位图即可。
      } finally {
        posterPending.delete(path)
      }
    }
  }
  await Promise.all(Array.from({ length: Math.min(LIMIT, queue.length) }, worker))
}

function blobToDataURL(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result))
    reader.onerror = () => reject(reader.error)
    reader.readAsDataURL(blob)
  })
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
      // 列表上屏后再补海报，不阻塞首屏。
      void loadLocalPosters(medias.value)
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
      void loadLocalPosters(medias.value)
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

/** 页面内一次性确认：直接用全局 AlertDialog，不必自己画弹层。 */
const showConfirmDialog = async (title: string, message: string): Promise<boolean> => {
  return confirm({ title, message, confirmText: '删除' })
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

const downloadingSubCounts = ref<Record<string, number>>({})

const getSubtitleKey = (sub: any, videoPath: string) => {
  if (!sub) return ''
  if (sub.type === 'external') {
    return sub.path || sub.name || ''
  }
  return `${videoPath}_internal_${sub.index}`
}

const isSubDownloading = (key: string) => (downloadingSubCounts.value[key] || 0) > 0

const beginSubDownload = (key: string) => {
  downloadingSubCounts.value[key] = (downloadingSubCounts.value[key] || 0) + 1
}

const endSubDownload = (key: string) => {
  if (downloadingSubCounts.value[key]) {
    downloadingSubCounts.value[key] -= 1
    if (downloadingSubCounts.value[key] <= 0) {
      delete downloadingSubCounts.value[key]
    }
  }
}

const extractDownloadError = async (err: any): Promise<string> => {
  if (err instanceof Blob) {
    try {
      const text = await err.text()
      const parsed = JSON.parse(text)
      return parsed?.msg || text || '下载字幕失败'
    } catch {
      return '下载字幕失败'
    }
  }
  return err?.response?.data?.msg || err?.message || '下载字幕失败'
}

const handleDownloadSubtitle = async (sub: any, videoPath: string) => {
  const key = getSubtitleKey(sub, videoPath)
  if (isSubDownloading(key)) return
  beginSubDownload(key)
  const isInternal = sub.type === 'internal'
  const toastId = toast.loading(isInternal ? '正在从视频中提取内置字幕并下载...' : '正在准备下载字幕...')

  try {
    const params: any = {
      is_internal: isInternal,
      video_path: videoPath,
    }
    if (isInternal) {
      params.internal_index = sub.index
    } else {
      params.path = sub.path
    }

    const response = await client.get('/api/v1/subtitles/download', {
      params,
      responseType: 'blob',
      timeout: 120000,
    })

    const blob = new Blob([response as any])
    let filename = sub.name || 'subtitle.srt'
    if (isInternal) {
      const ext = (sub.format === 'ass' || sub.format === 'ssa') ? '.ass' : (sub.format === 'vtt' || sub.format === 'webvtt') ? '.vtt' : (sub.format === 'pgs' || sub.format === 'sup') ? '.sup' : '.srt'
      const base = videoPath.split(/[/\\]/).pop()?.replace(/\.[^/.]+$/, '') || 'video'
      const lang = sub.language && sub.language !== 'unknown' ? sub.language : (sub.title || `track${sub.index}`)
      filename = `${base}.${lang}${ext}`
    } else if (sub.path) {
      filename = sub.path.split(/[/\\]/).pop() || filename
    }

    const downloadUrl = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = downloadUrl
    link.download = filename
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    window.URL.revokeObjectURL(downloadUrl)

    toast.dismiss(toastId)
    toast.success(`字幕下载成功：${filename}`)
  } catch (err: any) {
    toast.dismiss(toastId)
    console.error('Failed to download subtitle', err)
    const msg = await extractDownloadError(err)
    toast.error(msg)
  } finally {
    endSubDownload(key)
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
  <div class="flex flex-col gap-6">
    <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">媒体库</h1>
        <p class="text-muted-foreground mt-1 text-sm">浏览及管理归档目录中的所有影视资源。</p>
      </div>

      <div class="flex w-full max-w-md items-center gap-2">
        <div class="relative flex-1">
          <SearchIcon class="text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2" />
          <Input v-model="searchQuery" placeholder="搜索影片名称…" class="pl-9" @keyup.enter="handleSearch" />
        </div>
        <Button @click="handleSearch">搜索</Button>
      </div>
    </div>

    <div class="border-border flex flex-col justify-between gap-4 border-b pb-3 sm:flex-row sm:items-center">
      <div class="flex flex-wrap gap-2">
        <Button
          :variant="selectedCardId === 'all' ? 'secondary' : 'outline'"
          size="sm"
          :class="selectedCardId === 'all' ? 'text-primary' : ''"
          @click="selectTab('all')"
        >
          全部卡片
        </Button>

        <Button
          v-for="card in cards"
          :key="card.id"
          :variant="selectedCardId === card.id ? 'secondary' : 'outline'"
          size="sm"
          :class="selectedCardId === card.id ? 'text-primary' : ''"
          @click="selectTab(card.id)"
        >
          <FolderIcon data-icon="inline-start" />
          {{ card.name }}
          <Badge variant="outline" class="ml-1">{{ card.media_type === 'movie' ? '电影' : '电视剧' }}</Badge>
        </Button>
      </div>

      <Button v-if="selectedCardId !== 'all'" variant="outline" size="sm" :disabled="isRefreshing" @click="handleRefresh">
        <Spinner v-if="isRefreshing" data-icon="inline-start" />
        <RefreshCwIcon v-else data-icon="inline-start" />
        {{ isRefreshing ? '正在同步…' : '扫描刷新目录' }}
      </Button>
    </div>

    <div v-if="isLoading" class="flex items-center justify-center py-24">
      <Spinner class="text-primary" />
    </div>

    <div
      v-else-if="medias.length > 0"
      class="grid gap-6 sm:grid-cols-[repeat(auto-fill,minmax(200px,1fr))]"
    >
      <Card
        v-for="item in medias"
        :key="item.id"
        class="group flex flex-col justify-between overflow-hidden transition-all duration-300 hover:border-primary/50"
      >
        <div class="bg-muted relative aspect-[2/3] overflow-hidden">
          <img
            :src="getPosterURL(item.poster_path)"
            :alt="item.title"
            class="size-full object-cover transition-transform duration-300 group-hover:scale-105"
            loading="lazy"
          />
          <Badge variant="secondary" class="absolute top-2 right-2 gap-1">
            <FilmIcon v-if="item.type === 'movie'" data-icon="inline-start" />
            <DatabaseIcon v-else data-icon="inline-start" />
            {{ item.type === 'movie' ? '电影' : '剧集' }}
          </Badge>
        </div>

        <CardContent class="flex flex-1 flex-col justify-between gap-2 p-3">
          <div>
            <h3 class="truncate text-sm font-bold" :title="item.title">{{ item.title }}</h3>
            <span class="text-muted-foreground text-xs">{{ item.year || '未知年份' }}</span>
          </div>
          <div class="border-border/50 mt-1 flex flex-col gap-2 border-t pt-3">
            <div class="text-muted-foreground flex items-center gap-1" :title="item.path">
              <FolderIcon class="size-3.5 shrink-0" />
              <span class="truncate text-[10px]">{{ getShortPath(item.path) }}</span>
            </div>
            <Button size="sm" class="w-full" @click="openManageDialog(item)">
              <UploadIcon data-icon="inline-start" />
              字幕管理
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>

    <Empty v-else class="h-96">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <FilmIcon />
        </EmptyMedia>
        <EmptyTitle>暂无媒体记录</EmptyTitle>
        <EmptyDescription v-if="selectedCardId === 'all'">数据库中没有媒体记录。</EmptyDescription>
        <EmptyDescription v-else>当前目录卡片中没有任何导入的媒体。</EmptyDescription>
      </EmptyHeader>
      <Button v-if="selectedCardId !== 'all'" size="sm" :disabled="isRefreshing" @click="handleRefresh">
        <RefreshCwIcon data-icon="inline-start" />
        立即扫描文件夹目录
      </Button>
    </Empty>

    <!-- 字幕上传与管理 -->
    <Dialog :open="isDialogOpen" @update:open="(value) => { if (!value) closeDialog() }">
      <DialogContent class="flex max-h-[85vh] flex-col gap-0 overflow-hidden p-0 sm:max-w-4xl">
        <DialogHeader v-if="activeMedia" class="border-border flex-row items-start justify-between gap-3 border-b p-6">
          <div class="flex items-center gap-3">
            <FilmIcon v-if="activeMedia.type === 'movie'" class="text-primary size-6" />
            <DatabaseIcon v-else class="text-info size-6" />
            <div>
              <DialogTitle class="text-xl font-bold">{{ activeMedia.title }}</DialogTitle>
              <DialogDescription class="mt-0.5 max-w-[500px] truncate">{{ activeMedia.path }}</DialogDescription>
            </div>
          </div>
        </DialogHeader>

        <div v-if="activeMedia" class="flex-1 overflow-y-auto p-6">
          <!-- 1. 电影 -->
          <div v-if="activeMedia.type === 'movie'" class="flex flex-col gap-6">
            <Alert>
              <InfoIcon />
              <AlertTitle>手动上传指南</AlertTitle>
              <AlertDescription>
                <p>1. 支持的字幕类型：.srt, .ass, .ssa, .sbv, .webvtt</p>
                <p>
                  2. 系统会自动提取字幕文件名中的语言标记（如中英、简、繁、Eng等），并重命名为
                  <code>电影名.语言.后缀</code> 进行同级放置。
                </p>
              </AlertDescription>
            </Alert>

            <div class="flex flex-col gap-3">
              <h3 class="flex items-center gap-2 text-sm font-semibold">
                <DatabaseIcon class="text-primary size-4" />
                已有字幕列表
              </h3>

              <div v-if="isLoadingMovieSubtitles" class="flex justify-center py-4">
                <Spinner class="text-primary" />
              </div>

              <div
                v-else-if="movieSubtitles.length > 0"
                class="grid max-h-48 grid-cols-1 gap-3 overflow-y-auto pr-1 sm:grid-cols-2"
              >
                <div
                  v-for="sub in movieSubtitles"
                  :key="sub.path || sub.name"
                  class="bg-muted/40 flex items-center justify-between gap-3 rounded-lg border p-3 transition-colors hover:border-input"
                >
                  <div class="flex min-w-0 items-center gap-2">
                    <Badge
                      :variant="sub.type === 'external' ? 'secondary' : 'outline'"
                      :class="sub.type === 'external' ? 'text-primary' : 'text-info'"
                      class="shrink-0"
                    >
                      {{ sub.type === 'external' ? '外挂' : '内嵌' }}
                    </Badge>
                    <div class="truncate">
                      <p class="truncate text-xs font-semibold" :title="sub.name">{{ sub.name }}</p>
                      <p class="text-muted-foreground mt-0.5 text-[10px]">
                        语言: <span class="text-foreground">{{ formatLang(sub) }}</span> | 格式: {{ sub.format }}
                      </p>
                    </div>
                  </div>
                  <div class="flex shrink-0 items-center gap-1.5">
                    <Button
                      variant="outline"
                      size="xs"
                      :disabled="isSubDownloading(getSubtitleKey(sub, activeMedia.path))"
                      title="下载字幕文件"
                      @click="handleDownloadSubtitle(sub, activeMedia.path)"
                    >
                      <Spinner v-if="isSubDownloading(getSubtitleKey(sub, activeMedia.path))" data-icon="inline-start" />
                      <DownloadIcon v-else data-icon="inline-start" />
                      下载
                    </Button>
                    <Button
                      v-if="formatLang(sub) === '繁体中文'"
                      variant="secondary"
                      size="xs"
                      class="text-warning"
                      :disabled="isConverting"
                      title="转换为简体字幕"
                      @click="handleConvertSubtitle(sub, true, activeMedia.path)"
                    >
                      繁转简
                    </Button>
                    <Button
                      v-if="sub.type === 'external'"
                      variant="ghost"
                      size="icon-xs"
                      class="text-muted-foreground hover:text-destructive"
                      title="删除外挂字幕"
                      @click="handleDeleteSubtitle(sub.path, true)"
                    >
                      <Trash2Icon />
                    </Button>
                  </div>
                </div>
              </div>

              <p v-else class="bg-muted/30 text-muted-foreground rounded-lg border p-4 text-center text-xs">
                暂无已有字幕。请在下方上传。
              </p>
            </div>

            <div
              class="hover:border-primary/50 bg-muted/20 flex h-48 flex-col items-center justify-center rounded-lg border-2 border-dashed p-6 transition-colors"
              @dragover="handleDragOver"
              @drop="(e) => handleDrop(e, 'single', activeMedia.path)"
            >
              <UploadIcon class="text-muted-foreground mb-3 size-10" />
              <p class="mb-1 text-sm font-semibold">拖拽字幕文件到此处上传</p>
              <p class="text-muted-foreground mb-2 text-xs">或</p>
              <input
                id="singleSubInput"
                type="file"
                class="hidden"
                accept=".srt,.ass,.ssa,.sbv,.webvtt"
                @change="(e) => handleSingleFileChange(e, activeMedia.path)"
              />
              <Button size="sm" :disabled="isUploading" @click="triggerSingleFileSelect('singleSubInput')">
                选择本地字幕
              </Button>
            </div>

            <div v-if="isUploading" class="text-warning flex items-center justify-center gap-3 py-4">
              <Spinner />
              <span class="text-sm font-semibold">正在上传处理中，请稍候…</span>
            </div>
          </div>

          <!-- 2. 电视剧整季 -->
          <div v-else-if="activeMedia.type === 'tv'">
            <div v-if="isLoadingEpisodes" class="flex flex-col items-center justify-center gap-3 py-20">
              <Spinner class="text-primary" />
              <span class="text-muted-foreground text-sm">正在加载剧集列表…</span>
            </div>

            <div v-else-if="!isBatchMode" class="flex flex-col gap-4">
              <div class="bg-muted/40 flex items-center justify-between rounded-lg border p-4">
                <div>
                  <h3 class="text-sm font-semibold">电视剧整季字幕整理</h3>
                  <p class="text-muted-foreground mt-0.5 text-xs">共有 {{ episodes.length }} 集</p>
                </div>
                <div class="flex items-center gap-2">
                  <Button
                    v-if="hasTraditionalSubs"
                    variant="secondary"
                    size="sm"
                    class="text-warning"
                    :disabled="isConvertingBatch"
                    @click="handleBatchConvertSubtitles"
                  >
                    <Spinner v-if="isConvertingBatch" data-icon="inline-start" />
                    <RefreshCwIcon v-else data-icon="inline-start" />
                    一键整季繁转简
                  </Button>
                  <Button size="sm" @click="isBatchMode = true">
                    <UploadIcon data-icon="inline-start" />
                    批量上传整季字幕
                  </Button>
                </div>
              </div>

              <div class="divide-border overflow-hidden rounded-lg border">
                <div v-for="(ep, index) in episodes" :key="ep.id" class="hover:bg-muted/30 flex flex-col transition-colors">
                  <div class="flex flex-col justify-between gap-4 p-4 sm:flex-row sm:items-center">
                    <div class="min-w-0 flex-1">
                      <div class="flex flex-wrap items-center gap-2">
                        <Badge variant="secondary">第 {{ index + 1 }} 集</Badge>
                        <span class="truncate text-sm font-semibold">{{ ep.title || '单集' }}</span>
                      </div>

                      <div class="mt-2 flex flex-wrap gap-1">
                        <Badge
                          v-for="sub in ep.subtitles"
                          :key="sub.path || sub.name"
                          :variant="sub.type === 'external' ? 'secondary' : 'outline'"
                          :class="['cursor-pointer', sub.type === 'external' ? 'text-primary' : 'text-info']"
                          @click="toggleExpandEpisode(ep.id)"
                        >
                          {{ sub.type === 'external' ? '外挂' : '内嵌' }}·{{ formatLang(sub) }}
                        </Badge>
                        <span v-if="!ep.subtitles || ep.subtitles.length === 0" class="text-muted-foreground text-[10px] italic">
                          暂无已有字幕
                        </span>
                      </div>
                      <p class="text-muted-foreground mt-1 max-w-[450px] truncate text-[10px]" :title="ep.path">{{ ep.path }}</p>
                    </div>

                    <div class="flex shrink-0 items-center gap-2">
                      <input
                        :id="'subInput_' + ep.id"
                        type="file"
                        class="hidden"
                        accept=".srt,.ass,.ssa,.sbv,.webvtt"
                        @change="(e) => handleSingleFileChange(e, ep.path, false)"
                      />
                      <Button
                        variant="outline"
                        size="sm"
                        :disabled="isUploading"
                        @click="triggerSingleFileSelect('subInput_' + ep.id)"
                      >
                        <UploadIcon data-icon="inline-start" class="text-primary" />
                        上传字幕
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        title="查看/管理字幕"
                        @click="toggleExpandEpisode(ep.id)"
                      >
                        <ChevronUpIcon v-if="expandedEpisodeId === ep.id" />
                        <ChevronDownIcon v-else />
                      </Button>
                    </div>
                  </div>

                  <div
                    v-if="expandedEpisodeId === ep.id"
                    class="bg-muted/20 border-border/50 flex flex-col gap-2.5 border-t px-4 pt-2 pb-4"
                  >
                    <div class="text-muted-foreground text-xs font-semibold">本集字幕详情</div>

                    <div v-if="ep.subtitles && ep.subtitles.length > 0" class="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
                      <div
                        v-for="sub in ep.subtitles"
                        :key="sub.path || sub.name"
                        class="bg-muted/40 flex items-center justify-between gap-3 rounded-lg border p-2.5 transition-colors hover:border-input"
                      >
                        <div class="flex min-w-0 items-center gap-2">
                          <Badge
                            :variant="sub.type === 'external' ? 'secondary' : 'outline'"
                            :class="sub.type === 'external' ? 'text-primary' : 'text-info'"
                            class="shrink-0"
                          >
                            {{ sub.type === 'external' ? '外挂' : '内嵌' }}
                          </Badge>
                          <div class="truncate">
                            <p class="truncate text-xs font-semibold" :title="sub.name">{{ sub.name }}</p>
                            <p class="text-muted-foreground mt-0.5 text-[9px]">
                              语言: {{ formatLang(sub) }} | 格式: {{ sub.format }}
                            </p>
                          </div>
                        </div>
                        <div class="flex shrink-0 items-center gap-1.5">
                          <Button
                            variant="outline"
                            size="xs"
                            :disabled="isSubDownloading(getSubtitleKey(sub, ep.path))"
                            title="下载字幕文件"
                            @click="handleDownloadSubtitle(sub, ep.path)"
                          >
                            <Spinner v-if="isSubDownloading(getSubtitleKey(sub, ep.path))" data-icon="inline-start" />
                            <DownloadIcon v-else data-icon="inline-start" />
                            下载
                          </Button>
                          <Button
                            v-if="formatLang(sub) === '繁体中文'"
                            variant="secondary"
                            size="xs"
                            class="text-warning"
                            :disabled="isConverting"
                            title="转换为简体字幕"
                            @click="handleConvertSubtitle(sub, false, ep.path)"
                          >
                            繁转简
                          </Button>
                          <Button
                            v-if="sub.type === 'external'"
                            variant="ghost"
                            size="icon-xs"
                            class="text-muted-foreground hover:text-destructive"
                            title="删除外挂字幕"
                            @click="handleDeleteSubtitle(sub.path, false)"
                          >
                            <Trash2Icon />
                          </Button>
                        </div>
                      </div>
                    </div>
                    <p v-else class="bg-muted/20 text-muted-foreground rounded-lg p-2 text-center text-xs italic">
                      暂无字幕，请在右侧点击「上传字幕」添加。
                    </p>
                  </div>
                </div>
              </div>
            </div>

            <!-- 批量模式 -->
            <div v-else class="flex flex-col gap-6">
              <div class="border-border flex items-center gap-3 border-b pb-3">
                <Button variant="ghost" size="sm" :disabled="isBatchUploading" @click="isBatchMode = false">
                  <ArrowLeftIcon data-icon="inline-start" />
                  返回剧集列表
                </Button>
                <span class="text-sm font-bold">批量上传整季字幕</span>
              </div>

              <div
                class="hover:border-primary/50 bg-muted/20 flex flex-col items-center justify-center rounded-lg border-2 border-dashed p-6 transition-colors"
                @dragover="handleDragOver"
                @drop="(e) => handleDrop(e, 'batch')"
              >
                <UploadIcon class="text-muted-foreground mb-3 size-10" />
                <p class="mb-1 text-sm font-semibold">拖拽本季多个字幕文件到此处</p>
                <p class="text-muted-foreground mb-3 text-xs">支持文件名含类似 E01、S01E02、03 等集数编号进行自动识别匹配</p>
                <input
                  id="batchFileInput"
                  type="file"
                  multiple
                  class="hidden"
                  accept=".srt,.ass,.ssa,.sbv,.webvtt"
                  @change="handleBatchFilesChange"
                />
                <Button size="sm" :disabled="isBatchUploading" @click="triggerBatchFilesSelect">
                  多选本地字幕
                </Button>
              </div>

              <div v-if="batchFiles.length > 0" class="flex flex-col gap-4">
                <div class="flex items-center justify-between">
                  <h4 class="text-muted-foreground text-xs font-bold">已选字幕与剧集匹配表</h4>
                  <span class="text-muted-foreground text-[10px]">未指定匹配集的字幕文件不会上传</span>
                </div>

                <div class="divide-border max-h-80 overflow-y-auto rounded-lg border">
                  <div
                    v-for="(item, index) in batchFiles"
                    :key="index"
                    class="hover:bg-muted/30 flex items-center justify-between gap-4 p-3 transition-colors"
                  >
                    <span class="max-w-[350px] truncate font-mono text-xs" :title="item.file.name">
                      {{ item.file.name }}
                    </span>
                    <div class="flex items-center gap-3">
                      <Select v-model="item.episodeId" :disabled="isBatchUploading">
                        <SelectTrigger size="sm" class="w-56">
                          <SelectValue placeholder="-- 选择对应集 --" />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectGroup>
                            <SelectItem v-for="(ep, epIdx) in episodes" :key="ep.id" :value="ep.id">第 {{ epIdx + 1 }} 集 - {{ ep.title || '单集' }}</SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        class="text-muted-foreground hover:text-destructive"
                        :disabled="isBatchUploading"
                        aria-label="移除该文件"
                        @click="removeFileFromBatch(index)"
                      >
                        <XIcon />
                      </Button>
                    </div>
                  </div>
                </div>

                <div v-if="isBatchUploading" class="flex flex-col gap-2.5 rounded-lg border p-4">
                  <div class="text-warning flex justify-between text-xs font-bold">
                    <span>正在批量上传 ({{ batchProgress.current }} / {{ batchProgress.total }})…</span>
                    <span>{{ Math.round((batchProgress.current / batchProgress.total) * 100) }}%</span>
                  </div>
                  <Progress :model-value="(batchProgress.current / batchProgress.total) * 100" />
                  <p class="text-muted-foreground truncate text-[10px]">当前文件: {{ batchProgress.currentName }}</p>
                </div>

                <div class="flex justify-end gap-2.5 pt-3">
                  <Button variant="outline" :disabled="isBatchUploading" @click="isBatchMode = false">取消</Button>
                  <Button :disabled="isBatchUploading" @click="startBatchUpload">
                    <CheckIcon data-icon="inline-start" />
                    确认开始上传
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div class="border-border bg-muted/20 flex justify-end gap-3 border-t p-6">
          <Button variant="outline" :disabled="isUploading || isBatchUploading" @click="closeDialog">关闭面板</Button>
        </div>
      </DialogContent>
    </Dialog>
  </div>
</template>
