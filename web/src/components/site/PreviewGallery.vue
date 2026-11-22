<script setup lang="ts">
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'

type Shot = {
  id: string
  label: string
  title: string
  desc: string
  src: string
}

const shots: Shot[] = [
  {
    id: 'dashboard',
    label: '仪表盘',
    title: '仪表盘',
    desc: '电影 / 剧集总量、待整理文件、整理成功率与后台实时日志（SSE，断线自动续传）。',
    src: '/screenshots/dashboard.webp',
  },
  {
    id: 'library',
    label: '媒体库',
    title: '媒体库',
    desc: '按卡片聚合成海报墙，电影按 TMDB ID、剧集按季目录归并，可直接进入字幕管理。',
    src: '/screenshots/library.webp',
  },
  {
    id: 'scrape',
    label: '智能刮削',
    title: '智能刮削',
    desc: '内置文件浏览器，选中电影或剧集目录即可从 TMDB 抓取元数据、海报与 NFO。',
    src: '/screenshots/scrape.webp',
  },
  {
    id: 'transfer',
    label: '文件整理',
    title: '文件整理',
    desc: '选择下载目录，按媒体库规范重命名并转移到归档目录，支持硬链接与实时队列进度。',
    src: '/screenshots/transfer.webp',
  },
  {
    id: 'search',
    label: '搜索下载',
    title: '搜索下载 · 片名识别',
    desc: '中文片名先经 TMDB 解析成原名与各地别名，每个站点只发一次请求，结果按别名判定相关性。',
    src: '/screenshots/search-titles.webp',
  },
  {
    id: 'indexers',
    label: '站点管理',
    title: '已添加站点',
    desc: '站点健康一目了然：最近成功、平均耗时、连续失败与冷却，可单个或全部测试。',
    src: '/screenshots/search-indexers.webp',
  },
  {
    id: 'settings',
    label: '系统设置',
    title: '系统设置',
    desc: '九个设置分区：刮削、目录、媒体服务器、下载器、通知、安全、MCP、搜索、代理。',
    src: '/screenshots/settings-cards.webp',
  },
  {
    id: 'dark',
    label: '暗色主题',
    title: '暗色主题',
    desc: '全部页面共享语义 token，亮暗两套主题都有独立对比度校准，不做简单的反色。',
    src: '/screenshots/dashboard-dark.webp',
  },
]

const mobileShots = [
  { label: '登录', src: '/screenshots/login.webp' },
  { label: '字幕管理', src: '/screenshots/library-subtitle.webp' },
  { label: '站点模板库', src: '/screenshots/search-templates.webp' },
  { label: '下载任务', src: '/screenshots/search-tasks.webp' },
  { label: '站点状态', src: '/screenshots/search-status.webp' },
  { label: '通知配置', src: '/screenshots/settings-notify.webp' },
]
</script>

<template>
  <div class="flex flex-col gap-4">
  <Tabs default-value="dashboard" class="w-full min-w-0 gap-4">
    <div class="no-scrollbar w-full min-w-0 overflow-x-auto">
      <TabsList variant="line" class="h-9 w-max min-w-full justify-start">
        <TabsTrigger v-for="shot in shots" :key="shot.id" :value="shot.id" class="shrink-0 px-2.5 text-sm">
          {{ shot.label }}
        </TabsTrigger>
      </TabsList>
    </div>

    <TabsContent v-for="shot in shots" :key="shot.id" :value="shot.id" class="mt-0 outline-none">
      <figure class="browser-frame">
        <div class="border-border/70 flex items-center gap-2 border-b px-3 py-2">
          <span class="flex gap-1.5" aria-hidden="true">
            <span class="bg-muted-foreground/25 size-2.5 rounded-full"></span>
            <span class="bg-muted-foreground/25 size-2.5 rounded-full"></span>
            <span class="bg-muted-foreground/25 size-2.5 rounded-full"></span>
          </span>
          <span class="text-muted-foreground ml-1 truncate font-mono text-[0.7rem]">
            {{ shot.title }}
          </span>
          <Badge variant="secondary" class="ml-auto h-5 text-[0.68rem]">真实截图</Badge>
        </div>
        <img
          :src="shot.src"
          :alt="shot.title"
          width="2000"
          height="1250"
          loading="lazy"
          decoding="async"
          class="bg-muted/40 block h-auto w-full"
        />
      </figure>
      <figcaption class="text-muted-foreground mt-3 text-sm leading-6">{{ shot.desc }}</figcaption>
    </TabsContent>
  </Tabs>

  <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
    <a
      v-for="item in mobileShots"
      :key="item.src"
      :href="item.src"
      target="_blank"
      rel="noreferrer"
      class="group ring-foreground/10 bg-card relative overflow-hidden rounded-lg ring-1 transition-shadow hover:shadow-md"
    >
      <img
        :src="item.src"
        :alt="item.label"
        width="2000"
        height="1250"
        loading="lazy"
        decoding="async"
        class="block aspect-[16/10] w-full object-cover object-top opacity-90 transition-opacity group-hover:opacity-100"
      />
      <span class="bg-background/85 text-foreground absolute inset-x-0 bottom-0 px-2 py-1 text-[0.7rem] backdrop-blur">
        {{ item.label }}
      </span>
    </a>
  </div>
  </div>
</template>
