import { defineConfig } from 'astro/config'
import vue from '@astrojs/vue'
import sitemap from '@astrojs/sitemap'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

// 官网（web/）与产品前端（app/web/）共享同一套语义 token 与 shadcn-vue
// 组件源码，视觉上应当是同一个产品：琥珀强调色 + 中性底色 + 亮暗双主题。
export default defineConfig({
  // 部署到自有域名时改这里：canonical 与 sitemap 都取自它。
  site: 'https://bujic.movie',
  integrations: [vue(), sitemap()],
  // 构建产物是目录式静态页（/docs/deploy/index.html），
  // 与文档里的链接写法（结尾带斜杠）保持一致。
  trailingSlash: 'ignore',
  vite: {
    plugins: [tailwindcss()],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
  },
})
