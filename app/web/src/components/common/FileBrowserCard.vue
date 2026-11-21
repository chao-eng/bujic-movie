<script setup lang="ts">
import {
  ArrowLeftIcon,
  FileIcon,
  FolderIcon,
  RefreshCwIcon,
} from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'
import { cn } from '@/lib/utils'

/**
 * 服务器文件浏览器：两处页面（刮削 / 整理）用同一套交互——
 * 单击选中、双击进入目录、返回上级、刷新当前目录。
 */
const props = defineProps<{
  title: string
  description: string
  currentDir: string
  items: any[]
  loading: boolean
  selectedPath: string
  emptyTitle?: string
  class?: string
}>()

const emit = defineEmits<{
  (e: 'select', item: any): void
  (e: 'open', item: any): void
  (e: 'refresh'): void
  (e: 'up'): void
}>()

const canGoUp = () => props.currentDir !== '/' && props.currentDir !== ''
</script>

<template>
  <Card :class="props.class">
    <CardHeader class="border-border flex flex-row items-center justify-between gap-3 border-b">
      <div>
        <CardTitle>{{ props.title }}</CardTitle>
        <CardDescription>{{ props.description }}</CardDescription>
      </div>
      <div class="flex items-center gap-2">
        <Button variant="outline" size="icon-sm" title="刷新当前目录" @click="emit('refresh')">
          <RefreshCwIcon />
        </Button>
        <Button variant="outline" size="sm" :disabled="!canGoUp()" @click="emit('up')">
          <ArrowLeftIcon data-icon="inline-start" />
          返回上级
        </Button>
      </div>
    </CardHeader>

    <CardContent class="p-0">
      <div class="bg-muted/40 border-border truncate border-b px-4 py-2 font-mono text-xs">
        当前目录：{{ props.currentDir }}
      </div>

      <div v-if="props.loading" class="flex items-center justify-center py-20">
        <Spinner class="text-primary" />
      </div>

      <div v-else class="divide-border max-h-96 overflow-y-auto divide-y">
        <div
          v-for="item in props.items"
          :key="item.path"
          :class="
            cn(
              'flex cursor-pointer items-center justify-between px-4 py-2.5 text-sm transition-colors',
              props.selectedPath === item.path ? 'bg-primary/10 text-primary' : 'hover:bg-muted/30',
            )
          "
          @click="emit('select', item)"
          @dblclick="emit('open', item)"
        >
          <div class="flex items-center gap-3 truncate">
            <FolderIcon v-if="item.is_dir" class="text-primary size-5 shrink-0" />
            <FileIcon v-else class="text-muted-foreground size-5 shrink-0" />
            <span class="truncate">{{ item.name }}</span>
          </div>
          <span class="text-muted-foreground text-xs">
            {{ item.is_dir ? '文件夹' : (item.size / 1024 / 1024).toFixed(1) + ' MB' }}
          </span>
        </div>

        <Empty v-if="!props.items.length" class="border-0 py-20">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <FolderIcon />
            </EmptyMedia>
            <EmptyTitle>{{ props.emptyTitle ?? '当前目录下无内容' }}</EmptyTitle>
            <EmptyDescription>可以返回上级目录继续找。</EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    </CardContent>
  </Card>
</template>
