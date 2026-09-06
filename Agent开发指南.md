# Bujic Movie — Agent 开发指南

> 本文件是仓库 **Codex / Claude Code / OpenCode 等编码 Agent 的统一中文指南**。
> `AGENTS.md` 与 `CLAUDE.md` 均指向本文件。修改仓库代码前请先通读本指南。

---

## 1. 项目是什么

`Bujic Movie` —— 自托管的媒体文件管理器。核心能力：

- **自动刮削**：解析影片文件名 → TMDB 抓取海报/背景图 → 生成 Emby/Plex/Jellyfin 规范的 NFO 元数据。
- **智能整理**：把下载目录的文件重命名并按媒体库规范转移（rename + copy/move/hardlink/symlink），随行处理字幕，保护蓝光原盘结构。
- **Web UI**：Vue 3 + Vite 单页应用，前端构建产物被 `go:embed` 进单个 Go 二进制。
- **MCP / Agent 工具面**：内置 MCP（Streamable HTTP）端点，向 LLM Agent 开放媒体库查询与字幕工具，配套 API Key 管理与调用审计。详见第 8 节。

技术栈：后端 **Go + Gin + GORM/SQLite**；前端 **Vue 3 + TypeScript + shadcn-vue + Tailwind CSS v4 + Pinia**；单二进制分发。

---

## 2. 仓库布局

> Go module 位于 **`app/`**（module `github.com/bujic-movie/bujic-movie`），不在仓库根。**所有后端命令在 `app/` 下执行**。Vue 工程在 `app/web/`。

| 路径 | 说明 |
|------|------|
| `app/cmd/server/main.go` | 入口：加载配置 → 初始化 DB → 加载 DB 设置 → 装配路由 → 启动 |
| `app/internal/` | 私有应用代码 |
| `app/internal/router/router.go` | **DI 装配中心 + 路由表**（源码真相，先读它） |
| `app/internal/controller/` | HTTP 控制器（REST + MCP API Key 管理） |
| `app/internal/service/` | 业务服务（transfer/scrape/naming/watcher/subtitle_agent/mcp_api_key…） |
| `app/internal/mcp/` | MCP Streamable HTTP 网关（工具定义、鉴权、调用记录） |
| `app/internal/mediautil/` | REST 与 MCP 共享的字幕扫描/媒体聚合/路径白名单工具 |
| `app/internal/repository/` | GORM 仓储（各自构造时 `AutoMigrate` 自己的实体） |
| `app/internal/model/entity/` | 实体：`media`、`media_card`、`media_library`、`transfer_history`、`system_setting`、`notify_channel`、`mcp_api_key`、`mcp_call_record` |
| `app/internal/storage/` | 存储抽象（`storage.go` + 本地实现 `storage/local/`），可加其它后端 |
| `app/pkg/` | 可复用包：`tmdb`、`nfo`、`parser`、`fileutil`、`mediainfo`、`sat`、`logger`、`response`、`notify`、`mediaserver` |
| `app/embed.go` | `//go:embed dist` 嵌入已构建的前端 |
| `app/web/` | Vue 前端工程 |
| `.agents/skills/` | Agent 技能（如 `bujic-subtitle`，见第 8 节） |
| `doc/` | 设计/PRD/任务文档（中文命名，见第 10 节） |

---

## 3. 常用命令（在 `app/` 下）

```bash
# 开发：后端跑在 :8080
make dev-backend          # = go run ./cmd/server/main.go

# 开发：前端跑在 :5173（/api 代理到 :8080）——首次先在 app/web 里 npm install
make dev-frontend         # = cd web && npm run dev

# 构建单二进制（前端 → app/dist，再 go build → app/bujic-movie）
make build

# 测试
go test ./...
go test ./internal/service/ -run TestTransferService   # 单测

# Docker（多架构镜像；推 master 时 CI 也会构建）
make docker

# 其它 Makefile 目标：clean
```

### 测试清单

关键测试（可用 `-run` 单个执行）：

- 路由/鉴权：`TestAPIRoutes`、`TestEncryptedLoginAndPasswordUpdate`
- 整理：`TestTransferService`、`TestTransferExtraFiles`
- 存储：`TestLocalStorage`
- NFO：`TestMovieNFOXMLGeneration`、`TestEpisodeNFOGeneration`、`TestEpisodeNFOSpecialsKeepsZero`
- 解析：`TestParseFilename`、`TestParseSubtitle`、`TestParseFramerate`、`TestParseStreamDetails`
- 刮削/媒体信息：`TestScrapeService`、`TestMicroCodec`、`TestProbeFfprobeMissing`
- 简繁转换：`TestToSimplified`
- 通知渠道：`TestBarkSend`、`TestServerChan3MissingFields`、`TestServerChan3Send`、`TestNtfySendHeaders`、`TestGotifySendSuccessNoError`、`TestWebhookTemplate`
- 媒体服务器：`TestEmbyListLibraries`、`TestEmbyRefreshAll`、`TestEmbyRefreshSingleLibrary`、`TestJellyfinRefreshAllUsesRootPrefix`、`TestPlexRefreshAllRefreshesEverySection`、`TestPlexRefreshSingleSection`
- 渲染模板：`TestRenderTemplate`、`TestExtractDirectors`
- **MCP/Agent（新增）**：`TestAgentSubtitleFlow`、`TestAgentSubtitleFlowConcurrent`、`TestMCPAPIKeyLifecycle`、`TestMCPCallRecords`

### 构建注意事项（坑）

- `embed.go` 要求 `app/dist/` 存在，否则 `go build`/`go run`/`go test` 编译失败。`app/dist/` 已提交；若被清掉，先 `cd web && npm run build` 再执行任何 Go 命令。
- 前端 `build` 脚本 = `vue-tsc -b && vite build`，自带类型检查。
- `go test -race` 需要 CGO + gcc；本仓库 sqlite 为纯 Go（`glebarez/sqlite`），默认无 CGO 环境，`-race` 通常不可用——不要依赖它做 CI。

---

## 4. 架构要点

### 4.1 手工 DI（唯一装配点）

`internal/router/router.go` 的 `SetupRouter` 实例化存储、TMDB 客户端、全部仓储 → 服务 → 控制器 → 注册路由。**阅读顺序起点**。分层：`Controller → Service → Repository(GORM/SQLite)`；`storage` 抽象与 `pkg/*` 客户端为基础设施。

### 4.2 配置优先级（三层，后者覆盖前者）

1. 默认值：`internal/config/config.go`
2. 环境变量（`BUJIC_` 前缀，如 `BUJIC_SERVER_PORT`）
3. SQLite `system_settings` 表，启动时由 `db.LoadSettingsFromDB` 加载进内存

**没有 `config.yaml`**。绝大多数业务配置（TMDB key、媒体/下载路径、转移模式、账号密码）在 Web「设置」页编辑 → 存 DB → 覆盖 env/默认值。

### 4.3 迁移是分散式的

每个仓储构造器对自己的实体 `AutoMigrate`；`SystemSetting` 在 `LoadSettingsFromDB` 内迁移。实体集中在 `internal/model/entity/`。

### 4.4 鉴权（人）：加密登录 + JWT

- `GET /api/v1/auth/login-key` 取一次性会话 key → 前端 AES-GCM 加密密码（`@noble/ciphers`）→ `POST /auth/login` 解密后签发 JWT（24h）。
- 除 `health`、`auth/*`、`ws`、`mcp` 外，全部路由在 `middleware.AuthRequired()` 之后。
- JWT 也接受 `?token=` 查询参数（便于非浏览器/脚本调用）。

### 4.5 实时进度：WebSocket

`GET /api/v1/ws`（`ws_controller.go`）公开，推送任务/整理进度。仓库曾修过 WS 并发/阻塞问题——对同一连接并发写要小心；`Broadcast` 用带缓冲 channel 非阻塞发送。

### 4.6 整理引擎（transfer）

`service/transfer_service.go`：goroutine 工作池处理队列；`naming_service.go` 计算 Emby/Plex/Jellyfin 风格目标路径；模式 `copy/move/link/symlink`；`overwrite_mode`（默认 `size`）决定冲突；`min_file_size_mb` 过滤小文件；含 `BDMV` 的蓝光目录做特殊路径处理；随行字幕会重命名并转移。`storage` 接口抽象所有文件操作。

### 4.7 目录监听（watcher）

`service/watcher_service.go`：fsnotify 监听，`SetupRouter` 里启动，新文件自动触发整理；由「媒体卡」（`/api/v1/cards` 管理的目录配置）驱动。

### 4.8 刮削链路

`recognize_service.go` 解析文件名（`pkg/parser`）→ TMDB 识别 → `scrape_service.go` 生成 `.nfo`（`pkg/nfo`）+ 下载海报/背景。`pkg/sat` 负责繁体→简体转换。

---

## 5. 字幕与媒体工具（REST + MCP 共享）

字幕能力同时以 **REST**（供 Web UI）与 **MCP**（供 Agent）暴露。**公共逻辑放在 `internal/mediautil/`，不要在两处重复实现**：

| 能力 | REST 路由 | MCP 工具 |
|------|-----------|----------|
| 媒体列表（含字幕状态） | `GET /api/v1/media` | `query_media_list` |
| 字幕明细（外挂+内嵌） | `GET /api/v1/media/subtitles`、`/media/episodes` | `query_media_subtitles` |
| 获取字幕内容 | `GET /api/v1/subtitles/download` | `fetch_subtitle` |
| 上传字幕 | `POST /api/v1/subtitles/upload` | `upload_subtitle` |
| 删除 / 简繁转换 | `/subtitles/delete`、`/subtitles/convert` | — |

关键实现点（改动前必读）：

- `mediautil.GetSubtitlesForVideo` / `ExternalSubtitlesForVideo`：外挂扫盘 + 内嵌 ffprobe。
- `mediautil.GroupMedias`：把 media 行聚合成「卡片」形态（电影按 TMDBID、剧集按季目录）；`path` 对 TV 会变成季目录。
- 外挂字幕命名约定：`<videoBase>.<lang>.<ext>`（如 `xxx.zh-CN.srt`）。
- 路径安全：所有按路径读写都要过 `mediautil.CardAllow`（MediaCard 的 `ArchivePath`/`DownloadPath` 前缀白名单 + `filepath.Clean`），禁止穿越。参照 `DownloadSubtitle` 的安全加固（历史 commit `f27a2c2` 修过目录穿越）。
- 字幕状态三态口径 `subtitle_status`（full/partial/none）定义见 `subtitle_agent_service.go` 与 PRD BR-01/BR-03。

---

## 6. 前端（`app/web/`）

Vue 3 + Vite + TypeScript + shadcn-vue + Tailwind CSS v4 + Pinia + vue-router。`@` 别名 → `app/web/src`。`vite build` 输出到 `../dist`（即 `app/dist`，供 `embed.go`）。

- 页面：`src/pages/`（Dashboard/MediaLibrary/Scrape/Transfer/Setting/Login）。
- 设置页 `SettingPage.vue` 含「MCP / API Key」区块（tab 键 `mcp`）。
- API 封装：`src/api/client.ts`（axios，JWT 拦截）。
- 新增 UI 遵循 `.agents/skills/frontend-ui-ux/SKILL.md` 的暗色 cinematic 美学（琥珀强调色、深蓝灰底）——该 skill 被 gitignore，本地开发时存在。

---

## 7. 约定

- **提交信息用中文**（见 git log，如 `feat(mcp): ...`、`fix(build): ...`）。
- 代码风格：Go 走 `gofmt`；Vue 走项目 lint。不添加无关注释。
- 新后端能力按 实体→仓储→服务→控制器→router 装配 的顺序添加；实体 AutoMigrate 放仓储构造器。
- 修改文档时注意 `doc/` 下文件已用中文命名（见第 10 节）。

---

## 8. MCP / Agent 能力（新增，改动频繁区）

> 详规见 `doc/影视字幕Agent能力PRD.md`（v0.3，BR 编号权威来源）。**改 MCP 相关代码先读它**。

### 8.1 组件

| 文件 | 职责 |
|------|------|
| `internal/mcp/server.go` | Gateway：API Key 鉴权（仅 API Key，不回落 JWT，BR-18a/19）+ 转发给 mcp-go Streamable HTTP |
| `internal/mcp/tools.go` | 6 个工具注册与 handler、input_meta 脱敏、信号量并发上限(8) |
| `internal/mcp/records.go` / `input_meta.go` | 调用记录有界缓冲（BR-29）与脱敏 input_meta（BR-26） |
| `internal/service/subtitle_agent_service.go` | 5 个业务工具实现（枚举卡/列表/明细/获取/上传） |
| `internal/service/mcp_api_key_service.go` | API Key 生命周期（创建哈希存证/启停/校验/记录查询/180d 清理） |
| `internal/controller/mcp_api_key_controller.go` | 管理 REST（人通道，JWT） |
| `internal/repository/mcp_api_key_repo.go`、`mcp_call_record_repo.go` | 持久化 |
| `app/web/src/pages/SettingPage.vue` | 「MCP / API Key」UI |

### 8.2 端点与工具

- MCP 端点：`{SERVER_URL}/api/v1/mcp`（仅 API Key；`SERVER_URL`=scheme://host:port 整体变量）。
- 工具：`mcp_ping`（自测，不落记录）+ `list_media_cards` / `query_media_list` / `query_media_subtitles` / `fetch_subtitle` / `upload_subtitle`。`query_media_list` 的 `media_card_id` 省略或 0=全部卡，>0=指定卡。
- 管理 REST（JWT，人通道）：`POST/GET /api/v1/mcp/api-keys`、`GET /:id`、`PUT /:id/enable|disable`、`GET /:id/records`、`GET /call-records`。

### 8.3 安全红线

- MCP 端点**绝不接受 JWT**；管理 REST/Web **绝不接受 API Key**（双通道隔离）。
- Key 只存 `盐:哈希`，明文仅创建响应返回一次。
- 调用记录 `input_meta` 必须脱敏（不含 content/base64/原始 path）。
- Agent 交互 Skill：`.agents/skills/bujic-subtitle/SKILL.md`（工具编排 + 字幕翻译规范，对齐 `subtitle-translator-zh`）。

---

## 9. CI / 部署

`.github/workflows/build-push.yml`：master 推送时用 `./app` 上下文 + `app/deployments/Dockerfile` 构建多架构（amd64/arm64）镜像，推阿里云 ACR。`make docker` 本地等价。

---

## 10. 文档索引（doc/，中文命名）

| 文档 | 用途 | 信任度 |
|------|------|--------|
| `doc/项目架构设计.md` | 原始设计文档 | **已漂移**：无 `dto/`/`enum/` 目录，实际有 auth/dashboard/health/media_card 控制器与 watcher 服务等。**以代码为准** |
| `doc/影视字幕Agent能力PRD.md` | MCP 能力 PRD（v0.3，BR 权威） | 实现参考（代码已落地） |
| `doc/开发任务执行计划.md` | 阶段任务与里程碑 | 历史 |
| `doc/媒体库刷新通知设计.md` | 媒体库刷新/通知设计 | 参考 |
| `doc/MoviePilot刮削与整理分析.md` | 从 MoviePilot 移植逻辑分析 | 参考 |

---

## 11. 快速上手（改代码前 checklist）

1. 从 `internal/router/router.go` 读装配，确认新能力如何接线。
2. 复用优先：字幕/媒体逻辑用 `internal/mediautil`；文件操作用 `internal/storage` 接口；不要复制 controller/service 私有实现。
3. 需要持久化 → 新实体放 `internal/model/entity/`，仓储构造器 `AutoMigrate`。
4. 配置项走 默认值/`BUJIC_` env/DB 三层，别新增 config.yaml。
5. 涉及 MCP → 守第 8.3 节安全红线，并向 `doc/影视字幕Agent能力PRD.md` 的 BR 回填对齐。
6. 跑 `go build ./...`、`go test ./...`（需 `app/dist` 存在）；前端改完跑 `vue-tsc -b`。
7. 提交信息用中文。
