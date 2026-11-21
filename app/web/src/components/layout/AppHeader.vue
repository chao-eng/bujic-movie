<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import axios from 'axios'
import { CircleCheckIcon, CircleAlertIcon, LogOutIcon, ServerIcon } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'

const backendStatus = ref<'online' | 'offline' | 'checking'>('checking')
let timer: ReturnType<typeof setInterval> | null = null

const checkBackendHealth = async () => {
  try {
    const res = await axios.get('/api/v1/health')
    backendStatus.value = res.data?.code === 0 && res.data?.data?.status === 'ok' ? 'online' : 'offline'
  } catch {
    backendStatus.value = 'offline'
  }
}

const handleLogout = () => {
  localStorage.removeItem('token')
  window.location.href = '/login'
}

onMounted(() => {
  checkBackendHealth()
  timer = setInterval(checkBackendHealth, 30000)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <header
    class="bg-background/80 border-border sticky top-0 z-40 flex h-16 shrink-0 items-center justify-between border-b px-6 backdrop-blur-md"
  >
    <h2 class="text-lg font-semibold">Bujic Movie 媒体管理系统</h2>

    <div class="flex items-center gap-4">
      <div class="flex items-center gap-2 text-xs">
        <ServerIcon class="text-muted-foreground size-4" />
        <span class="text-muted-foreground">后台服务</span>
        <Badge v-if="backendStatus === 'online'" variant="secondary" class="text-success">
          <CircleCheckIcon data-icon="inline-start" />
          在线
        </Badge>
        <Badge v-else-if="backendStatus === 'offline'" variant="destructive">
          <CircleAlertIcon data-icon="inline-start" />
          离线
        </Badge>
        <Badge v-else variant="outline" class="text-warning">检测中</Badge>
      </div>

      <Button variant="outline" size="sm" title="退出登录" @click="handleLogout">
        <LogOutIcon data-icon="inline-start" />
        退出登录
      </Button>
    </div>
  </header>
</template>
