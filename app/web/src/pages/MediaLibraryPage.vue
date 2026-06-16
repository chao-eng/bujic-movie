<script setup lang="ts">
import { ref, onMounted } from 'vue'
import client from '@/api/client'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Search, Film, Database, Trash2, Loader2 } from 'lucide-vue-next'

const medias = ref<any[]>([])
const searchQuery = ref('')
const isLoading = ref(false)

const getPosterURL = (path: string) => {
  if (!path) return 'https://placehold.co/342x513/1e293b/e2e8f0?text=No+Poster'
  return `https://image.tmdb.org/t/p/w342${path}`
}

const fetchMedias = async () => {
  isLoading.value = true
  try {
    const res: any = await client.get('/api/v1/media')
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
    const res: any = await client.get(`/api/v1/media/search?q=${encodeURIComponent(searchQuery.value)}`)
    if (res.code === 0) {
      medias.value = res.data || []
    }
  } catch (err) {
    console.error('Search failed', err)
  } finally {
    isLoading.value = false
  }
}

const deleteMedia = async (id: number) => {
  if (!confirm('确定要从数据库中删除该媒体记录吗？文件将保留在硬盘上。')) {
    return
  }

  try {
    const res: any = await client.delete(`/api/v1/media/${id}`)
    if (res.code === 0) {
      medias.value = medias.value.filter((m) => m.id !== id)
    }
  } catch (err) {
    console.error('Failed to delete media', err)
  }
}

onMounted(() => {
  fetchMedias()
})
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col md:flex-row md:items-center justify-between gap-4">
      <div>
        <h1 class="text-3xl font-extrabold tracking-tight text-slate-100">媒体库</h1>
        <p class="text-slate-400 mt-1">浏览及管理已刮削入库的所有电影与电视剧集。</p>
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

    <div v-if="isLoading" class="flex items-center justify-center py-20">
      <Loader2 class="h-8 w-8 text-amber-500 animate-spin" />
    </div>

    <!-- Media Poster Grid -->
    <div v-else-if="medias.length > 0" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-6">
      <Card
        v-for="item in medias"
        :key="item.id"
        class="bg-slate-900 border-slate-800 text-slate-100 overflow-hidden group hover:border-amber-500/50 transition-all duration-300 flex flex-col justify-between"
      >
        <div class="relative aspect-[2/3] overflow-hidden">
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
          <div class="flex items-center justify-between pt-3 border-t border-slate-800/50 mt-2">
            <span class="text-[10px] text-slate-500 truncate max-w-[100px]" :title="item.path">{{ item.path }}</span>
            <button
              @click="deleteMedia(item.id)"
              class="text-slate-500 hover:text-rose-500 transition-colors p-1 rounded hover:bg-slate-800"
              title="删除记录"
            >
              <Trash2 class="h-4 w-4" />
            </button>
          </div>
        </CardContent>
      </Card>
    </div>

    <!-- Empty State -->
    <div v-else class="border border-dashed border-slate-800 rounded-lg h-96 flex flex-col items-center justify-center text-slate-500">
      <Film class="h-12 w-12 text-slate-600 mb-3" />
      <span class="text-lg font-semibold mb-2">暂无入库的媒体</span>
      <span>您整理并完成刮削的影片会在该列表展示。</span>
    </div>
  </div>
</template>
