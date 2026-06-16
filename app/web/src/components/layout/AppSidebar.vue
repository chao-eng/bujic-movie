<script setup lang="ts">
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import {
  LayoutDashboard,
  Film,
  Sparkles,
  ArrowLeftRight,
  Settings,
  Menu
} from 'lucide-vue-next'

const router = useRouter()
const route = useRoute()

const menuItems = [
  { name: '仪表盘', path: '/dashboard', icon: LayoutDashboard },
  { name: '媒体库', path: '/library', icon: Film },
  { name: '智能刮削', path: '/scrape', icon: Sparkles },
  { name: '文件整理', path: '/transfer', icon: ArrowLeftRight },
  { name: '系统设置', path: '/settings', icon: Settings },
]

const isCollapsed = ref(false)

const navigate = (path: string) => {
  router.push(path)
}

const toggleSidebar = () => {
  isCollapsed.value = !isCollapsed.value
}
</script>

<template>
  <aside
    :class="[
      'h-screen bg-slate-900 border-r border-slate-800 text-slate-100 flex flex-col transition-all duration-300',
      isCollapsed ? 'w-16' : 'w-64'
    ]"
  >
    <!-- Logo area -->
    <div class="h-16 flex items-center justify-between px-4 border-b border-slate-800">
      <div v-if="!isCollapsed" class="flex items-center gap-2">
        <Film class="h-6 w-6 text-amber-500" />
        <span class="font-bold text-lg tracking-wider text-amber-500">BUJIC MOVIE</span>
      </div>
      <div v-else class="mx-auto">
        <Film class="h-6 w-6 text-amber-500" />
      </div>
      <button
        @click="toggleSidebar"
        class="text-slate-400 hover:text-slate-200 transition-colors p-1 rounded hover:bg-slate-800"
      >
        <Menu class="h-5 w-5" />
      </button>
    </div>

    <!-- Navigation items -->
    <nav class="flex-1 py-4 space-y-1 px-2">
      <button
        v-for="item in menuItems"
        :key="item.path"
        @click="navigate(item.path)"
        :class="[
          'w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-all duration-200 group',
          route.path.startsWith(item.path)
            ? 'bg-amber-500/10 text-amber-500 border-l-2 border-amber-500'
            : 'text-slate-400 hover:bg-slate-800/50 hover:text-slate-100'
        ]"
      >
        <component :is="item.icon" class="h-5 w-5 shrink-0" />
        <span v-if="!isCollapsed" class="transition-opacity duration-200">{{ item.name }}</span>
      </button>
    </nav>

    <!-- Footer / Version -->
    <div class="p-4 border-t border-slate-800 text-xs text-slate-500 text-center">
      <span v-if="!isCollapsed">Bujic Movie v1.0.0</span>
      <span v-else>v1.0</span>
    </div>
  </aside>
</template>
