# Bujic Movie

`Bujic Movie` 是一个轻量级、自托管的媒体文件管理工具，旨在实现电影和电视剧的**自动刮削**（元数据抓取）、**文件智能整理**（重命名、归档、转移）与**资源搜索下载**（多站点聚合搜索、一键推送到下载器）。项目提供了一个精美现代的 Web 可视化操作界面，帮助您轻松管理个人媒体库。

---

## 🚀 核心特性

- **🎬 自动刮削与元数据管理**
  - 解析影片文件名，智能识别影片名称、年份、版本、分辨率等。
  - 通过 [The Movie Database (TMDB) API](https://www.themoviedb.org/) 自动抓取海报、背景图并生成符合 Emby / Plex / Jellyfin 标准的 NFO 元数据文件。
  - 支持图片别名自动复制，保证多媒体播放器兼容性。

- **📂 智能文件整理 (Transfer Engine)**
  - 支持从下载目录转移到媒体库目录。
  - 提供多种整理模式：`Copy` (复制)、`Move` (移动)、`Hardlink` (硬链接，推荐) 以及 `Symlink` (软链接)。
  - 内置防蓝光原盘 (Blu-ray) 破坏逻辑，保护重要媒体源文件完整性。
  - 智能文件名过滤与重命名引擎，支持设置最小文件体积过滤（排除小广告、片段）。

- **🔍 资源搜索与下载**
  - 一次搜索多个资源站，结果按种子指纹跨站点合并，同一条资源不会重复出现。
  - 站点接入支持两类：**通用协议**（Torznab / Newznab）与 **Cardigann 定义**（兼容 Prowlarr 的定义库，可运行时同步或本地导入 YAML）。
  - 搜索结果可按分类、做种数、大小筛选与排序；点「下载」即推送到 qBittorrent，映射成对应分类。
  - 站点健康状态可见：最近成功时间、平均耗时、连续失败与冷却，支持单个或全部测试。
  - **保存目录由 qBittorrent 的分类规则决定**，本系统只传分类名、不传路径 —— 避免与下载器的路径约定打架。

- **🗃️ 极简单二进制部署**
  - 利用 Go 的 `go:embed` 特性，将编译好的前端静态资源直接嵌入到 Go 二进制中，实现单文件分发。
  - 数据库采用无配置、零运维的 SQLite，随起随用。

- **🎨 Premium 视觉设计**
  - 基于 Vue 3 + Vite + TypeScript + shadcn-vue + Tailwind CSS 打造，组件与配色统一使用语义 token，支持亮/暗主题。
  - 包含仪表盘、媒体库浏览、刮削控制台、整理队列、搜索下载、后台任务及全局设置面板。

- **🤖 MCP / Agent 工具面**
  - 内置 MCP (Streamable HTTP) 端点，向 Agent 开放媒体库查询与字幕工具（列表 / 字幕明细 / 获取 / 上传）。
  - 配套 API Key 生命周期管理（创建 / 启停 / 调用记录审计）与字幕翻译 Skill。
  - 详见下文「MCP / Agent 接入」章节。

---

## 🛠️ 技术栈选型

| 层级 | 技术选型 | 作用描述 |
| :--- | :--- | :--- |
| **后端框架** | Go + Gin | 提供高性能的 RESTful API 服务，实现轻量、单二进制部署 |
| **数据库** | SQLite + GORM | 适合自托管的轻量级关系型数据库，通过 ORM 实现自动表迁移 |
| **文件操作** | 自研存储抽象层 (类似 rclone) | 屏蔽底层存储差异，方便后续拓展云盘或网络存储后端 |
| **前端框架** | Vue 3 + Vite + TS | 现代且快速的单页面应用 (SPA) 开发栈 |
| **UI 组件库** | shadcn-vue + Tailwind CSS | 组件源码内置于仓库，样式走语义 token（`bg-card` / `text-muted-foreground` 等），便于统一换肤 |
| **实时日志** | SSE (Server-Sent Events) | 后端日志单向下发，自带自增 id 与断线续传，比 WebSocket 更契合只读流 |
| **出站请求** | 自研执行器 | 统一超时、重试、限速、响应体积上限与主机白名单，代理可按站点覆盖 |
| **CI/CD** | GitHub Actions | 手动触发的多架构 (AMD64/ARM64) Docker 构建并推送至阿里云 ACR |

---

## 🏃 如何使用 (Usage)

### 开发环境调试

1. **克隆仓库**
   ```bash
   git clone <your-repo-url>
   cd bujic-movie
   ```

2. **启动后端服务**
   ```bash
   cd app
   go run ./cmd/server/main.go
   ```
   后端默认监听 `http://localhost:8080`，默认账号 `admin` / `admin123`（首次启动后请及时在「系统设置 → 安全」修改）。

3. **启动前端开发服务器**
   ```bash
   cd app/web
   npm install
   npm run dev
   ```
   前端运行在 `http://localhost:5173`，`/api` 请求已由 Vite 代理到后端。

> 也可以在 `app/` 下用 `make dev-backend` / `make dev-frontend` 分别启动。

---

### 本地编译生产版本

```bash
cd app
# 编译前端，并将产物 embed 到 Go 二进制中输出
make build
```
编译成功后，将在 `app/` 下生成可执行文件 `bujic-movie`。

> ⚠️ `embed.go` 要求 `app/dist/` 存在。若被清掉，`go build` / `go run` / `go test` 都会编译失败，先 `cd app/web && npm run build` 再执行任何 Go 命令。

---

### 测试

```bash
cd app
go test ./...                      # 后端
cd web && npx vitest run           # 前端（57 项）
cd web && npm run build            # 含 vue-tsc 类型检查
```

---

### Docker-compose 一键部署

在生产环境部署时，建议使用 `docker-compose` 运行：

```yaml
version: "3.8"
services:
  bujic-movie:
    image: crpi-a1liy20beodq2bdl.cn-beijing.personal.cr.aliyuncs.com/bujic/bujic-movie:latest
    container_name: bujic-movie
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./data:/app/data                # 配置 + SQLite 数据库
      - /path/to/media:/media           # 媒体库挂载
      - /path/to/downloads:/downloads   # 下载目录挂载
    environment:
      - TZ=Asia/Shanghai
      - BUJIC_SERVER_SECRET_KEY=change_me_to_something_secure
```

> ⚠️ **关于 Docker 媒体挂载路径一致性的最佳实践建议**：
>
> 本系统生成的 NFO 文件中会包含绝对路径的海报标签（例如指向 `/media/TV/.../episode.jpg`）。
> 为了确保 Emby / Jellyfin / Plex 等媒体服务器能够百分之百读取并正确显示刮削出的海报，**强烈建议保持其他媒体服务器容器的媒体目录挂载路径与本系统一致**（即：大家都将宿主机上同一个媒体文件夹挂载到各自容器内的同一个路径下，例如统一挂载为 `/media`）。

---

## 🔍 搜索下载怎么用

界面在 **「搜索下载」** 页，分五个标签：

| 标签 | 作用 |
| :--- | :--- |
| **搜索** | 输入片名检索全部启用站点，结果可按分类/做种/大小筛选排序，点「下载」推送到下载器 |
| **站点列表** | 内置模板库，挑一个站点填名字即可添加；支持批量添加与按兼容性/分类/语言筛选 |
| **已添加站点** | 管理已配置站点，查看健康状态、单个或全部测试、测试登录、绑定下载器 |
| **下载任务** | 按下载器分组查看任务进度，可暂停/继续/删除 |
| **状态** | 各站点的最近成功、平均耗时、连续失败与冷却情况 |

首次使用建议顺序：**站点列表**挑站点 → **设置 → 下载器** 配置 qBittorrent → 回 **搜索** 页检索。

### 需要登录 / 有 Cloudflare 拦截的站点

本系统**不自动破解人机验证**。遇到 `cf_clearance` 这类需要浏览器交互的站点，两种方式导入会话：

1. 站点编辑表单里直接粘贴 Cookie（至少包含 `cf_clearance`）与同一浏览器的 User-Agent；
2. 使用仓库内的浏览器扩展 `tools/bujic-cookie-import/`（Chrome / Edge）：在你自己的浏览器里完成验证后一键导入。

> `cf_clearance` 绑定**出口 IP + User-Agent**。如果本系统走代理而浏览器直连，Cookie 不会生效——请让两边使用同一出口 IP，并在站点配置里填同一个代理。

### 站点定义的来源

定义文件（Cardigann YAML）**不随仓库分发**（上游为 GPL 授权）。运行时从 `https://indexers.prowlarr.com` 同步，或在「站点列表」页导入本地 YAML。同步失败时会保留本地已有定义。

---

## 🤖 MCP / Agent 接入

Bujic Movie 通过内置的 MCP (Model Context Protocol) **Streamable HTTP** 端点向 Agent（如 Claude Code、OpenCode 等支持 MCP 的运行时）开放媒体库查询与字幕工具，可用于「查询媒体列表 → 下载英文字幕 → 翻译 → 上传中文字幕」等自动化闭环。

### 1. 获取 API Key

在 Web 后台 **「系统设置 → MCP / API Key」** 中创建一个 API Key。密钥明文仅在创建时展示一次，请妥善保存；每个 Key 可独立 **禁用 / 启用**，并可在同一页面查看该 Key 的调用记录（含脱敏入参、耗时、结果大小）。

### 2. MCP Server 配置示例

将下列 JSON 加入你的 MCP 客户端配置（Claude Code 的 `claude_desktop_config.json`、OpenCode 的 `.mcp.json` 等，字段以客户端实际格式为准）：

```jsonc
// MCP Server 配置示例
{
  "mcpServers": {
    "bujic-movie": {
      "type": "http",
      "url": "${SERVER_URL}/api/v1/mcp",
      // SERVER_URL 为服务器完整地址（scheme://host:port 整体为变量），
      // 例如 http://192.168.1.10:8080 或 http://localhost:8080；/api/v1/mcp 为固定路径
      "headers": { "Authorization": "Bearer <API_KEY>" },
      "toolNames": [
        "list_media_cards",       // 枚举媒体卡（确定查询范围）
        "query_media_list",       // 查询媒体库列表（含字幕状态）
        "query_media_subtitles",  // 查询单个媒体/视频的字幕明细
        "fetch_subtitle",         // 获取字幕内容（外挂 / 内嵌）
        "upload_subtitle"         // 上传字幕文件
      ]
    }
  }
}
```

- MCP 端点**仅接受 API Key 鉴权**（不接受网页登录 JWT）；管理 REST 与 Web UI 仅接受 JWT，两者互不通用。
- 端点内置 `mcp_ping` 工具，可用于连通性与鉴权自测（不读业务数据、不计入调用记录）。
- `query_media_list` 的 `media_card_id`：省略或 0 查**全部媒体卡**；>0 查指定卡（先用 `list_media_cards` 枚举）。
- 对接 Agent 使用的配套 Skill 见仓库 `skills/bujic-subtitle/SKILL.md`（含完整工具编排流程与字幕翻译规范）。
- 详细设计见 `doc/字幕Agent能力/需求文档.md`（v0.4，BR 权威来源）。
- 全部需求/设计/测试文档的分类索引见 [`doc/文档索引.md`](doc/文档索引.md)。

---

## ⚙️ 环境变量配置 (Environment Variables)

系统支持通过环境变量注入基础的运行环境参数。所有环境变量前缀为 `BUJIC_`，并使用下划线代替配置层级的点（例如配置项 `search.enabled` 对应 `BUJIC_SEARCH_ENABLED`）。

| 环境变量 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `BUJIC_SERVER_PORT` | `8080` | Web 服务监听端口 |
| `BUJIC_SERVER_MODE` | `debug` | 运行模式 (`debug` / `release`) |
| `BUJIC_SERVER_SECRET_KEY` | `change_me_to_something_secure` | 用于 JWT 签名，同时派生站点凭据的加密密钥，**务必修改** |
| `BUJIC_DATABASE_DB_PATH` | `data/bujic-movie.db` | SQLite 数据库的存储路径 |
| `BUJIC_SEARCH_ENABLED` | `true` | 是否启用搜索下载模块；关闭后除 `/status` 外相关接口返回 404 |
| `BUJIC_SEARCH_PROXY` | 空 | 站点默认出口代理，支持 `http://` / `socks5://` / `socks5h://`，留空直连 |

> 💡 **提示**：其他系统业务配置（例如 TMDB API 密钥、媒体库路径、整理转移模式、搜索代理及账号密码等）均可直接在 **Web 网页后台的「系统设置」** 页面进行可视化配置，它们会被自动保存至数据库并覆盖环境变量与默认值，保存即生效（多数项目无需重启）。

---

## 📚 文档

| 文档 | 说明 |
| :--- | :--- |
| [`doc/文档索引.md`](doc/文档索引.md) | 全部需求 / 设计 / 测试文档的分类索引 |
| [`Agent开发指南.md`](Agent开发指南.md) | 面向编码 Agent 的仓库导览、架构要点与改动约定 |
| [`doc/搜索下载能力/01-需求文档.md`](doc/搜索下载能力/01-需求文档.md) | 搜索下载能力的 PRD（业务规则权威来源） |
| [`doc/字幕Agent能力/需求文档.md`](doc/字幕Agent能力/需求文档.md) | MCP / 字幕能力 PRD |

---

## 📄 许可与致谢

站点定义格式与部分解析思路参考 [Prowlarr](https://github.com/Prowlarr/Prowlarr)（GPL-3.0）。本仓库不包含其定义文件，仅实现兼容的解释器。
