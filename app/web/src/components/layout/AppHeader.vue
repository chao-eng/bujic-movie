<script setup lang="ts">
import { ref, onMounted } from 'vue'
import axios from 'axios'
import { Server, CheckCircle2, AlertCircle, LogOut } from 'lucide-vue-next'

const backendStatus = ref<'online' | 'offline' | 'checking'>('checking')

const checkBackendHealth = async () => {
  try {
    const res = await axios.get('/api/v1/health')
    if (res.data && res.data.code === 0 && res.data.data.status === 'ok') {
      backendStatus.value = 'online'
    } else {
      backendStatus.value = 'offline'
    }
  } catch (err) {
    backendStatus.value = 'offline'
  }
}

const handleLogout = () => {
  localStorage.removeItem('token')
  window.location.href = '/login'
}

onMounted(() => {
  checkBackendHealth()
  // Check health every 30 seconds
  setInterval(checkBackendHealth, 30000)
})
</script>

<template>
  <header class="h-16 shrink-0 border-b border-slate-800/80 bg-slate-950/80 backdrop-blur-md flex items-center justify-between px-6 sticky top-0 z-40">
    <div class="flex items-center gap-4">
      <h2 class="text-lg font-semibold text-slate-100">Bujic Movie 媒体管理系统</h2>
    </div>

    <div class="flex items-center gap-4">
      <!-- Health status indicator -->
      <div class="flex items-center gap-2 bg-slate-900 border border-slate-800 px-3 py-1.5 rounded-full text-xs">
        <Server class="h-4 w-4 text-slate-400" />
        <span class="text-slate-300">后台服务:</span>
        <div class="flex items-center gap-1.5">
          <template v-if="backendStatus === 'online'">
            <CheckCircle2 class="h-4 w-4 text-green-500" />
            <span class="text-green-500 font-semibold">在线</span>
          </template>
          <template v-else-if="backendStatus === 'offline'">
            <AlertCircle class="h-4 w-4 text-red-500" />
            <span class="text-red-500 font-semibold">离线</span>
          </template>
          <template v-else>
            <span class="text-amber-500 animate-pulse">检测中...</span>
          </template>
        </div>
      </div>

      <!-- Logout button -->
      <button
        @click="handleLogout"
        class="bg-slate-900 hover:bg-slate-800 border border-slate-800 text-slate-300 hover:text-rose-400 px-3.5 py-1.5 rounded-full text-xs font-semibold flex items-center gap-2 transition-colors duration-200"
        title="退出登录"
      >
        <LogOut class="h-3.5 w-3.5" />
        <span>退出登录</span>
      </button>
    </div>
  </header>
</template>
