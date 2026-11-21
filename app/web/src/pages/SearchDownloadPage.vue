<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Activity, Download, Layers, Search as SearchIcon } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import SearchPanel from '@/components/search/SearchPanel.vue'
import DefinitionPanel from '@/components/search/DefinitionPanel.vue'
import IndexerPanel from '@/components/search/IndexerPanel.vue'
import TaskPanel from '@/components/search/TaskPanel.vue'
import StatusPanel from '@/components/search/StatusPanel.vue'
import { searchApi } from '@/api/search'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'

type TabKey = 'search' | 'definitions' | 'indexers' | 'tasks' | 'status'

const tabs: { key: TabKey; label: string; icon: any }[] = [
  { key: 'search', label: '搜索', icon: SearchIcon },
  { key: 'definitions', label: '站点列表', icon: Layers },
  { key: 'indexers', label: '已添加站点', icon: Activity },
  { key: 'tasks', label: '下载任务', icon: Download },
  { key: 'status', label: '状态', icon: Activity },
]

const active = ref<TabKey>('search')
const isLoading = ref(true)
const enabled = ref(true)

onMounted(async () => {
  try {
    const res: any = await searchApi.status()
    enabled.value = Boolean(res?.data?.enabled)
  } catch {
    enabled.value = false
  } finally {
    isLoading.value = false
  }
  if (!enabled.value) toast.error('搜索下载功能未启用')
})
</script>

<template>
  <div class="flex flex-col gap-4">
    <div>
      <h1 class="text-lg font-semibold">搜索下载</h1>
      <p class="text-muted-foreground text-xs">一次搜索多个资源站，点「下载」就交给下载器</p>
    </div>

    <div v-if="isLoading" class="flex flex-col gap-2">
      <Skeleton class="h-8 w-72" />
      <Skeleton class="h-40 w-full" />
    </div>

    <Alert v-else-if="!enabled" class="text-warning">
      <AlertTitle>搜索下载功能未启用</AlertTitle>
      <AlertDescription>在设置里启用 <code>search.enabled</code> 并重启后即可使用。</AlertDescription>
    </Alert>

    <Tabs v-else v-model="active" class="flex flex-col gap-4">
      <TabsList variant="line">
        <TabsTrigger v-for="tab in tabs" :key="tab.key" :value="tab.key" class="gap-1.5">
          <component :is="tab.icon" />
          {{ tab.label }}
        </TabsTrigger>
      </TabsList>

      <TabsContent value="search"><SearchPanel /></TabsContent>
      <TabsContent value="definitions"><DefinitionPanel /></TabsContent>
      <TabsContent value="indexers"><IndexerPanel /></TabsContent>
      <TabsContent value="tasks"><TaskPanel /></TabsContent>
      <TabsContent value="status"><StatusPanel /></TabsContent>
    </Tabs>
  </div>
</template>
