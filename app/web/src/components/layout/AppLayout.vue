<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'
import { useLogStore } from '@/stores/logStore'

const logStore = useLogStore()

onMounted(() => {
  void logStore.init()
})

onUnmounted(() => {
  logStore.disconnect()
})
</script>

<template>
  <div class="bg-background text-foreground flex h-screen w-screen overflow-hidden font-sans">
    <AppSidebar />

    <div class="bg-background flex min-w-0 flex-1 flex-col">
      <AppHeader />

      <main class="flex-1 overflow-y-auto p-6">
        <!--
          这里不再包 <transition mode="out-in">：它要求旧页面先播完离场动画才肯卸载，
          而路由组件根元素上的过渡类不在本文件的 scoped 作用域里，事件不来就会把整棵
          组件树卡死（后续 parentNode / flags 报错都是它的连带反应）。
          页面切换的观感由 style.css 里的 .route-fade 挂载淡入负责，不阻塞路由。
        -->
        <router-view v-slot="{ Component }">
          <component :is="Component" class="route-fade" />
        </router-view>
      </main>
    </div>
  </div>
</template>
