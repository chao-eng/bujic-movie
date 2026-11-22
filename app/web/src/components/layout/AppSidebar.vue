<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ArrowLeftRight,
  Film,
  LayoutDashboard,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  Settings,
  Sparkles,
} from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

const router = useRouter()
const route = useRoute()

const menuItems = [
  { name: '仪表盘', path: '/dashboard', icon: LayoutDashboard },
  { name: '搜索下载', path: '/search', icon: Search },
  { name: '媒体库', path: '/library', icon: Film },
  { name: '智能刮削', path: '/scrape', icon: Sparkles },
  { name: '文件整理', path: '/transfer', icon: ArrowLeftRight },
  { name: '系统设置', path: '/settings', icon: Settings },
]

const isCollapsed = ref(false)

const isActive = (path: string) => route.path.startsWith(path)
</script>

<template>
  <aside
    :class="
      cn(
        'bg-sidebar text-sidebar-foreground border-sidebar-border flex h-screen flex-col border-r transition-all duration-300',
        isCollapsed ? 'w-16' : 'w-64',
      )
    "
  >
    <div
      class="border-sidebar-border flex h-16 items-center border-b"
      :class="isCollapsed ? 'justify-center px-2' : 'justify-between px-4'"
    >
      <div v-if="!isCollapsed" class="flex items-center gap-2">
        <img src="/favicon.png" alt="布吉影库" class="bg-muted ring-border size-7 shrink-0 rounded ring-1" />
        <span class="text-primary text-lg font-bold tracking-wide">布吉影库</span>
      </div>
      <!-- 收起时只留切换按钮：窄栏里同时放 logo 与按钮会因空间不足互相挤压。 -->
      <Button
        variant="ghost"
        size="icon-sm"
        class="shrink-0"
        :aria-label="isCollapsed ? '展开侧栏' : '收起侧栏'"
        @click="isCollapsed = !isCollapsed"
      >
        <PanelLeftOpen v-if="isCollapsed" data-icon="inline-start" />
        <PanelLeftClose v-else data-icon="inline-start" />
      </Button>
    </div>

    <nav class="flex flex-1 flex-col gap-1 px-2 py-4">
      <Button
        v-for="item in menuItems"
        :key="item.path"
        :variant="isActive(item.path) ? 'secondary' : 'ghost'"
        :class="
          cn(
            'h-auto w-full gap-3 py-2.5 font-medium',
            isCollapsed ? 'justify-center px-0' : 'justify-start px-3',
            isActive(item.path) && 'text-primary',
          )
        "
        :title="isCollapsed ? item.name : undefined"
        :aria-label="item.name"
        :aria-current="isActive(item.path) ? 'page' : undefined"
        @click="router.push(item.path)"
      >
        <component :is="item.icon" data-icon="inline-start" class="size-5 shrink-0" />
        <span v-if="!isCollapsed">{{ item.name }}</span>
      </Button>
    </nav>

    <div class="border-sidebar-border text-muted-foreground border-t p-2 text-center text-xs">
      <span v-if="!isCollapsed">布吉影库 v1.0.0</span>
      <span v-else class="text-[10px]">v1.0</span>
    </div>
  </aside>
</template>
