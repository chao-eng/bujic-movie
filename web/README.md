# 布吉影库官网（web/）

本项目的官方网站与使用文档，部署与使用说明的唯一权威线上入口。
技术栈：**Astro + Vue 3 + Vite + TypeScript + shadcn-vue + Tailwind CSS v4**，
静态输出，可直接丢到任意静态托管或反向代理后面。

## 与产品前端的关系

视觉不是另一套设计语言：`src/styles/global.css` 的语义 token（琥珀强调色、中性底色、
亮/暗两套对比度）与 `app/web/src/style.css` 保持一致，`src/components/ui/` 是同一批
shadcn-vue 组件源码。改动配色时应当两边同步。

## 命令

```bash
cd web
npm install
npm run dev       # http://localhost:4321
npm run build     # 输出到 web/dist
npm run preview   # 预览构建产物
npm run check     # Astro + TS 类型检查
```

> 需要 Node.js >= 22.23（Astro 7 的要求）。`web/.nvmrc` 已固定版本，
> 用 nvm/fnm 的机器会自动切换。

## 部署到 Cloudflare Pages

| 配置项 | 值 |
|--------|-----|
| 根目录（Root directory） | `web` |
| 构建命令（Build command） | `npm ci && npm run build` |
| 输出目录（Build output directory） | `dist` |
| 环境变量 | `NODE_VERSION=22.23.4`（或让平台读 `.nvmrc`） |

产物是纯静态文件，不需要 Functions 或 Workers 运行时。

两个容易踩的点：

- **`package-lock.json` 必须进版本库**，否则 `npm ci` 会直接失败。仓库根的 `.gitignore`
  里有 `data/` 这类规则时要留意别误伤 `web/src/data/`——导航数据就在那里，被忽略会导致
  构建报 `Could not load src/data/nav`。
- **锁文件里的 `resolved` 统一写 `registry.npmjs.org`**。从国内镜像生成的锁文件会带
  `registry.npmmirror.com`，在 Cloudflare 的构建机上可能超时；`integrity` 是内容摘要，
  换源不影响校验。


## 目录

| 路径 | 说明 |
|------|------|
| `src/pages/index.astro` | 首页：特性、界面预览、MCP、部署、文档入口 |
| `src/pages/docs/*.astro` | 文档页（概览/部署/配置/刮削/整理/媒体库/搜索/MCP/设置/架构/FAQ） |
| `src/layouts/BaseLayout.astro` | 站点外壳：首帧主题、SEO、页头页脚 |
| `src/layouts/DocsLayout.astro` | 文档外壳：侧栏、面包屑、右侧目录、上下篇 |
| `src/components/site/` | 站点组件：代码块、截图外框、预览画廊、主题切换 |
| `src/components/ui/` | shadcn-vue 组件（与 app/web 同源，不要手改） |
| `src/data/nav.ts` | 文档目录单一来源，侧栏/翻页/首页入口都读它 |
| `public/screenshots/` | 真实运行截图（WebP，来自部署实例） |

## 新增一个文档页

1. 在 `src/pages/docs/` 下新建 `.astro` 文件，套用 `DocsLayout`。
2. 在 `src/data/nav.ts` 里加一行（标题、路径、一句话摘要）。
3. 侧栏、上下篇、首页文档卡片会自动出现。

正文标题写成 `<h2 id="...">` / `<h3 id="...">`，右侧目录从正文标题自动生成，不需要维护第二份清单。

## 截图更新

`public/screenshots/*.webp` 是从真实实例按 1600×1000、2x 缩放截取后压到宽 2000px 的 WebP。
换截图时保持同一视口尺寸，能避免页面里外框比例跳变。
