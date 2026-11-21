# AGENTS.md

> 本文件是仓库 **Codex / Claude Code / OpenCode 等编码 Agent 的统一中文指南**。
> 修改仓库代码前请先通读本文件；需要改动某块功能时，先看第 4 节对应的架构说明。

---

## 1. 项目是什么

`Bujic Movie` —— 自托管的媒体文件管理器。核心能力：

- **自动刮削**：解析影片文件名 → TMDB 抓取海报/背景图 → 生成 Emby/Plex/Jellyfin 规范的 NFO 元数据。
- **智能整理**：把下载目录的文件重命名并按媒体库规范转移（rename + copy/move/hardlink/symlink），随行处理字幕，保护蓝光原盘结构。
- **资源搜索下载**：多站点聚合搜索 → 一键推送到 qBittorrent。站点接入支持通用协议（Torznab）与 Cardigann 定义（兼容 Prowlarr）。
- **Web UI**：Vue 3 + Vite 单页应用，前端构建产物被 `go:embed` 进单个 Go 二进制。
- **MCP / Agent 工具面**：内置 MCP（Streamable HTTP）端点，向 LLM Agent 开放媒体库查询与字幕工具，配套 API Key 管理与调用审计。详见第 8 节。

技术栈：后端 **Go + Gin + GORM/SQLite**；前端 **Vue 3 + TypeScript + shadcn-vue + Tailwind CSS v4 + Pinia**；单二进制分发。

---

## 2. 仓库布局

> Go module 位于 **`app/`**（module `github.com/bujic-movie/bujic-movie`），不在仓库根。**所有后端命令在 `app/` 下执行**。Vue 工程在 `app/web/`。

| 路径 | 说明 |
|------|------|
| `app/cmd/server/main.go` | 入口：加载配置 → 初始化 DB → 加载 DB 设置 → 装配路由 → 启动 |
| `app/cmd/defcheck/` | 定义覆盖自检工具：对真实 Prowlarr 定义做解析率统计 |
| `app/cmd/tplcheck/` | 模板渲染自检工具 |
| `app/internal/` | 私有应用代码 |
| `app/internal/router/router.go` | **DI 装配中心 + 路由表**（源码真相，先读它） |
| `app/internal/controller/` | HTTP 控制器（REST、日志 SSE、MCP API Key 管理） |
| `app/internal/service/` | 业务服务（transfer/scrape/naming/watcher/subtitle_agent/mcp_api_key/搜索下载各服务） |
| `app/internal/service/cardigann/` | Cardigann YAML 定义解析引擎（解析、模板渲染、过滤器、字段抽取） |
| `app/internal/service/connector/` | 索引器接出：统一 `Connector` 接口 + Torznab / Cardigann / Knaben / SubsPlease |
| `app/internal/service/httpclient/` | 出站请求执行器：超时、重试、限速、主机白名单、响应体积上限 |
| `app/internal/service/qbittorrent/` | qBittorrent 客户端 |
| `app/internal/crypto/` | 站点凭据加密（AES-256-GCM） |
| `app/internal/mcp/` | MCP Streamable HTTP 网关（工具定义、鉴权、调用记录） |
| `app/internal/mediautil/` | REST 与 MCP 共享的字幕扫描/媒体聚合/路径白名单工具 |
| `app/internal/repository/` | GORM 仓储（各自构造时 `AutoMigrate` 自己的实体） |
| `app/internal/model/entity/` | 实体：`media`、`media_card`、`media_library`、`transfer_history`、`system_setting`、`notify_channel`、`mcp_api_key`、`mcp_call_record`、`search_*` |
| `app/internal/storage/` | 存储抽象（`storage.go` + 本地实现 `storage/local/`），可加其它后端 |
| `app/pkg/` | 可复用包：`tmdb`、`nfo`、`parser`、`fileutil`、`mediainfo`、`sat`、`logger`、`response`、`notify`、`mediaserver` |
| `app/embed.go` | `//go:embed dist` 嵌入已构建的前端 |
| `app/web/` | Vue 前端工程 |
| `app/web/src/components/ui/` | shadcn-vue 组件源码（随仓库递交，**不要手改**，见 6.2） |
| `skills/` | Agent 技能目录（唯一副本 `bujic-subtitle`，见第 8 节；本地私有技能不提交） |
| `tools/bujic-cookie-import/` | Chrome/Edge 扩展：把浏览器已通过的 Cloudflare 会话导入服务端 |
| `doc/` | 设计/PRD/任务文档，按能力分目录、中文命名（见第 10 节） |

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

# Docker（多架构镜像；CI 为手动触发，见第 9 节）
make docker

# 定义覆盖自检（对真实定义库统计解析率）
go run ./cmd/defcheck <definitions-dir>

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
- **MCP/Agent**：`TestAgentSubtitleFlow`、`TestAgentSubtitleFlowConcurrent`、`TestMCPAPIKeyLifecycle`、`TestMCPCallRecords`
- **搜索下载**：错误码契约 `internal/router/search_contract_test.go`、指标 `search_metrics_test.go`、日志流 `internal/controller/log_stream_controller_test.go`
- **前端**：`cd web && npx vitest run`（57 项，含组件导入自检、筛选逻辑、日志通道生命周期）

### 构建注意事项（坑）

- `embed.go` 要求 `app/dist/` 存在，否则 `go build`/`go run`/`go test` 编译失败。`app/dist/` 已提交；若被清掉，先 `cd web && npm run build` 再执行任何 Go 命令。
- 前端 `build` 脚本 = `vue-tsc -b && vite build`，自带类型检查。**不要**用 `npx vue-tsc -b <路径>` 指定单目录，它会把整个仓库当项目跑出一堆既有文件的历史类型问题；直接用 `cd app/web && npm run build`。
- `go test -race` 需要 CGO + gcc；本仓库 sqlite 为纯 Go（`glebarez/sqlite`），默认无 CGO 环境，`-race` 通常不可用——不要依赖它做 CI。

---

## 4. 架构要点

### 4.1 手工 DI（唯一装配点）

`internal/router/router.go` 的 `SetupRouter` 实例化存储、TMDB 客户端、全部仓储 → 服务 → 控制器 → 注册路由。**阅读顺序起点**。分层：`Controller → Service → Repository(GORM/SQLite)`；`storage` 抽象与 `pkg/*` 客户端为基础设施。

### 4.2 配置优先级（三层，后者覆盖前者）

1. 默认值：`internal/config/config.go`（`v.SetDefault`）
2. 环境变量（`BUJIC_` 前缀，层级用下划线，如 `BUJIC_SEARCH_PROXY`）
3. SQLite `system_settings` 表，启动时由 `db.LoadSettingsFromDB` 加载进内存

**没有 `config.yaml`**。绝大多数业务配置（TMDB key、媒体/下载路径、转移模式、搜索代理、账号密码）在 Web「设置」页编辑 → 存 DB → 覆盖 env/默认值。

少数配置支持**保存即生效**而不需重启，例如全局搜索代理：`httpclient.Executor.SetGlobalProxyResolver` 每次出站读取实时值。

### 4.3 迁移是分散式的

每个仓储构造器对自己的实体 `AutoMigrate`；`SystemSetting` 在 `LoadSettingsFromDB` 内迁移。实体集中在 `internal/model/entity/`。新增能力时**不改既有表**（搜索模块的 8 张 `search_*` 表就是全新增）。

### 4.4 鉴权（人）：加密登录 + JWT

- `GET /api/v1/auth/login-key` 取一次性会话 key → 前端 AES-GCM 加密密码（`@noble/ciphers`）→ `POST /auth/login` 解密后签发 JWT（24h）。
- 公开路由只有：`health`、`auth/*`、`mcp`（API Key 鉴权在 handler 内）。**其余全部**在 `middleware.AuthRequired()` 之后，包括 `/logs/stream`。
- JWT 也接受 `?token=` 查询参数（便于非浏览器/脚本调用）。但**不要**把 JWT 拼进 `<img src>` 之类的 URL——它会漏进访问日志与 Referer。需要鉴权的图片资源要用带 `Authorization` 头的请求取回（见 `MediaLibraryPage.vue` 的 `loadLocalPosters`）。

### 4.5 实时日志：SSE（不是 WebSocket）

`GET /api/v1/logs/stream` + `GET /api/v1/logs/recent`（`controller/log_stream_controller.go`）。旧的 WebSocket 控制器已删除。

- 每条消息带自增 `id`，重连时按 `Last-Event-ID` 只补断线期间缺失的行；重放窗口 200 行。
- 每 15s 发一次 `heartbeat` 事件，既保活也让前端识别「假活」（TCP 未断但对端已死）。
- 服务端重启后序号从 1 重来，`ready` 事件带 `last_seq` 用于识别这种倒退。
- 客户端在 `web/src/stores/logStore.ts`，重试完全由 store 掌控（tail 退避 1s→30s），并用「世代号」保证手动重连不会留下并行连接。

### 4.6 整理引擎（transfer）

`service/transfer_service.go`：goroutine 工作池处理队列；`naming_service.go` 计算 Emby/Plex/Jellyfin 风格目标路径；模式 `copy/move/link/symlink`；`overwrite_mode`（默认 `size`）决定冲突；`min_file_size_mb` 过滤小文件；含 `BDMV` 的蓝光目录做特殊路径处理；随行字幕会重命名并转移。`storage` 接口抽象所有文件操作。

### 4.7 目录监听（watcher）

`service/watcher_service.go`：fsnotify 监听，`SetupRouter` 里启动，新文件自动触发整理；由「媒体卡」（`/api/v1/cards` 管理的目录配置）驱动。

### 4.8 刮削链路

`recognize_service.go` 解析文件名（`pkg/parser`）→ TMDB 识别 → `scrape_service.go` 生成 `.nfo`（`pkg/nfo`）+ 下载海报/背景。`pkg/sat` 负责繁体→简体转换。

### 4.9 搜索下载（索引器 + qBittorrent）

Prowlarr 核心能力的产品级移植：定义库 → 站点 → 聚合搜索 → 推送到 qBittorrent。**与整理/刮削/字幕链路完全解耦**：不读媒体卡、不写下载目录、不触发 watcher。

| 层 | 位置 | 说明 |
|----|------|------|
| 实体 | `internal/model/entity/search.go` | 8 张 `search_*` 表，全部新增，不改既有表 |
| 仓储 | `internal/repository/search_repo.go` | 唯一 owner；构造器内 `AutoMigrate` 新表 |
| 定义解析 | `internal/service/cardigann/` | YAML → 定义模型、三级错误定位、过滤器白名单、HTML/JSON/XML 提取 |
| 连接器 | `internal/service/connector/` | `Connector` 接口 + Torznab / Cardigann / Knaben / SubsPlease；`GroupReleases` 跨站去重 |
| HTTP | `internal/service/httpclient/` | 超时、GET 重试（1s/2s）、代理、主机白名单、20MB 响应上限、每站限流；**每一跳重定向都重新校验主机** |
| 下载器 | `internal/service/qbittorrent/` | qB 4.6/5.x 登录、建分类、推送、任务、暂停/继续/删除 |
| 服务 | `internal/service/{definition,indexer,search,download_client,grab}_service.go`、`cookie_import_service.go` | 定义库、站点健康、聚合搜索、分类映射、幂等推送、任务聚合、会话导入 |
| 加密 | `internal/crypto/secretbox.go` | AES-256-GCM，密钥由 `server.secret_key` 域分离派生；接口只回掩码 |
| 控制器/路由 | `internal/controller/search_controller.go`、`internal/router/router.go` | `/api/v1/search/*`，JWT；`search.enabled=false` 时除 `/status` 外 404 |
| 前端 | `web/src/pages/SearchDownloadPage.vue`、`components/search/*`、`lib/indexerFilter.ts` | 搜索 / 站点列表 / 已添加站点 / 下载任务 / 状态五个标签 |

#### 人机验证（Cloudflare）

不自动破解。站点返回 `cf-mitigated: challenge` 时分类为 `challenge`（错误码 42204）。用户在浏览器完成验证后，通过站点表单或 `tools/bujic-cookie-import/` 扩展导入 Cookie + User-Agent（`POST /api/v1/search/cookie-import`，按域名匹配）。

`cf_clearance` 常是 **HttpOnly + 分区（CHIPS）Cookie**：它能随请求发出，但 `chrome.cookies` 与 `document.cookie` 都读不到。扩展因此还有两条途径——分区查询（Chrome 120+ 带 `partitionKey`）与从真实请求头读 `Cookie`（需 `webRequest` + `extraHeaders`）。扩展的探测面板会列出每一步结果，排查时先看它。

`cf_clearance` 绑定**出口 IP + UA**，两边必须一致。

#### 关键约束（改代码前必读）

- **不传保存目录**：推送只带分类，落盘位置由 qBittorrent 的分类规则决定（PRD D-04）。
- **凭据只写不读**：`api_key`/`password`/代理凭据/登录 Cookie 加密落库；读接口只回掩码。更新时空值表示「保持原值」，清除需显式 `__clear__`。
- **定义是快照**：实例保存时的 YAML 不随定义库更新；用户点「刷新定义」才切换（BR-41）。
- **并发预算**：全局搜索 ≤3、单次站点 ≤20、同站单在途、单站 8s、全量 15s；不要绕开 `SearchService` 直接扇出。
- **搜索结果不落库**；只有推送动作写 `search_download_records`，记录里固化站点/下载器名称。
- **测试结果就地更新**：单个/批量测试只补丁对应行，不要为此重新拉取整个列表（会丢滚动位置与展开状态）。
- **错误码别复用 401**：目标站点认证失败返回 422 + `40102`，避免前端把它当成会话失效而登出。

配置项在 `config.Search`（环境变量 `BUJIC_SEARCH_*`）：`enabled`、`definition_source/version`、`auto_sync`、`proxy`、并发与超时、`grab_window_ms`、记录保留天数。全局代理也可在「系统设置 → 搜索下载」修改，保存即生效；站点级 `proxy_url` 优先于全局；qBittorrent 请求带 `BypassProxy`，始终直连。

测试：`go test ./internal/service/ ./internal/service/cardigann ./internal/service/connector ./internal/service/httpclient ./internal/service/qbittorrent ./internal/router ./internal/controller`。需求/设计/测试文档在 `doc/搜索下载能力/01-需求文档.md`、`doc/搜索下载能力/03-设计说明书.md`、`doc/搜索下载能力/04-测试计划.md`。

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

### 6.1 样式约定

- **只用语义 token**：`bg-background` / `bg-card` / `bg-muted` / `border-border` / `text-foreground` / `text-muted-foreground` / `text-primary` / `text-success` / `text-warning` / `text-destructive` / `text-info`。
- **不要写裸色号**（`slate-*`、`amber-*`、`emerald-*`、`rose-*` 等），也不要手写 `dark:` 覆盖——主题已经在 `style.css` 的 `:root` / `.dark` 里定义好。
- 间距用 `flex flex-col gap-*`，不要 `space-y-*`；宽高相等用 `size-*`。

### 6.2 shadcn-vue 组件

`src/components/ui/` 是 CLI 从官方 registry 拉下来的源码，随仓库递交。**优先复用既有组件，不要手写样式 div**：

| 场景 | 用 |
|------|-----|
| 表单布局 | `FieldGroup` + `Field` + `FieldLabel`（不要裸 div + label） |
| 下拉 | `Select` + `SelectGroup` + `SelectItem`（**`SelectItem` 的 value 不能是空字符串**，reka-ui 会直接抛错；用 `lib/utils.ts` 的 `EMPTY_SELECT_VALUE` 哨兵） |
| 弹层 | `Dialog`（自带 ESC / 遮罩关闭 / 滚动锁定）、`AlertDialog`（确认） |
| 反馈 | `Spinner`（加载）、`Alert`（提示）、`Badge`（状态）、`Skeleton`（占位）、`Empty`（空态） |
| 表格 / 标签页 | `Table` / `Tabs` |

加载按钮不要手拼 `animate-spin`，用 `<Spinner data-icon="inline-start" />`。状态徽章用 `Badge` 的 `variant` 表达语气，文案本身要能说明状态（不只靠颜色）。

**路由过渡坑**：不要在 `AppLayout.vue` 用 `<transition mode="out-in">`。过渡类加在路由组件根元素上，不在该文件的 scoped 作用域内，`transitionend` 不来就会让组件树卡死（曾表现为 `parentNode` of null 报错）。现在用的是 `style.css` 里的 `.route-fade` 纯 CSS 挂载动画。

### 6.3 数据层

- `src/api/client.ts`：axios，自动附 JWT。**响应拦截器只在错误码 `40101` 时登出**；业务上的 401 可能是外部服务认证失败（`401xx`），不能据此清 token。
- `src/api/search.ts`：搜索下载相关接口与标签映射。
- 纯逻辑抽到 `src/lib/` 并配 vitest（`indexerFilter.ts`、`logStream.ts`、`searchRequest.ts` 是范例）。`lib/__tests__/componentImports.test.ts` 会检查模板里用到的组件是否都 import 了。

---

## 7. 约定

- **提交信息用中文**（见 git log，如 `feat(mcp): ...`、`fix(build): ...`）。
- 提交粒度按功能域拆，不要把多个不相干改动塞进一个提交。
- 代码风格：Go 走 `gofmt`；Vue 走项目 lint。不添加无关注释。
- 新后端能力按 实体→仓储→服务→控制器→router 装配 的顺序添加；实体 AutoMigrate 放仓储构造器。
- 改文档时遵循 `doc/` 的目录与命名约定（见第 10 节）。

---

## 8. MCP / Agent 能力（改动频繁区）

> 详规见 `doc/字幕Agent能力/需求文档.md`（BR 编号权威来源）。**改 MCP 相关代码先读它**。

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
- Agent 交互 Skill：`skills/bujic-subtitle/SKILL.md`（工具编排 + 字幕翻译规范，对齐 `subtitle-translator-zh`）。

---

## 9. CI / 部署与发版

`.github/workflows/build-push.yml`：**手动触发**（Actions 页面 → Run workflow），不随 push 自动构建。用 `./app` 上下文 + `app/deployments/Dockerfile` 构建多架构（默认 amd64/arm64）镜像推阿里云 ACR。`make docker` 本地等价。

手动触发时可填的参数：

| 参数 | 必填 | 说明 |
|------|------|------|
| `release_notes` | 否 | 版本更新说明，单行；UI 不支持多行输入 |
| `release_notes_file` | 否 | 仓库内说明文件路径（如 `doc/发布说明-YYYY.MM.DD.md`），**优先于上一项**，适合多行 Markdown |
| `version_tag` | 否 | 版本标签，留空用当天日期 `vYYYY.MM.DD`；不带 `v` 会自动补 |
| `create_release` | 否 | 构建成功后是否创建 tag 与 release，默认是 |
| `platforms` | 否 | 构建架构，默认 `linux/amd64,linux/arm64` |
| `push_latest` | 否 | 是否同时打 `latest`，默认是 |

`release_notes` 与 `release_notes_file` 必须至少填一个，否则脚本报错中止——`workflow_dispatch` 的 `string` 输入在 UI 上是单行框，多行说明请走文件。

发布顺序是「先推镜像，成功后再建 tag/release」——镜像推送失败就不该对外发版本。`gh release create` 会顺带创建指向默认分支的 tag，不需要自己打 tag 再 push。

两条容易踩的规则：

- **标签列表在脚本里拼好后传给构建步骤**。直接写在 `tags:` 里时，`latest` 的可选逻辑会留下空行，`build-push-action` 遇到空标签会直接报错。
- **同名 tag 已存在则中止**。覆盖发布版本等于悄悄改变用户拿到的内容，脚本会报错并提示换 `version_tag` 或先删旧标签。

### 9.1 发版流程

```bash
# 1. 把版本说明写进仓库（不要用 UI 的单行输入框）
vim doc/发布说明-YYYY.MM.DD.md
git add doc/发布说明-YYYY.MM.DD.md && git commit -m "docs: 添加 YYYY.MM.DD 版本发布说明"
git push origin master

# 2. 触发：说明从文件读，标签留空则用当天日期
gh workflow run build-push.yml \
  -f version_tag="vYYYY.MM.DD" \
  -f release_notes_file="doc/发布说明-YYYY.MM.DD.md" \
  -f create_release=true

# 3. 跟踪
gh run list --workflow=build-push.yml --limit 1
gh run view <run-id> --log-failed        # 失败时看这一步
gh release view vYYYY.MM.DD --json body -q .body
```

**说明文件必须先 push**：工作流用 `actions/checkout` 拉取仓库，文件不在远端就读不到，脚本会报「更新说明文件不存在」。

**提交范围**：版本说明里统计的提交数只算功能提交，排除发布流程自身产生的（说明文件、脚本改动、构建修复）。

### 9.2 构建相关的三个坑

这三点曾叠加导致「本地过、CI 挂」，排查时按顺序看：

1. **`package-lock.json` 必须进版本库**。本机全局 gitignore 会忽略它；一旦不进库，Dockerfile 里 `npm install` 装的是浮动版本，类型检查工具版本一变就报一堆类型错误。仓库 `.gitignore` 已显式放行 `app/web/package-lock.json`，Dockerfile 用 `npm ci`。

2. **锁文件要包含所有平台的原生绑定**。在 macOS 上生成的锁文件只有 `@rolldown/binding-darwin-arm64`，Linux 下会缺 `@rolldown/binding-linux-*`、`lightningcss-*`、`@tailwindcss/oxide-*` 等包，`npm ci` 严格按锁文件安装就会报 `Cannot find native binding`。补齐方式：从 registry 元数据取每个平台包的 `integrity`/`tarball`/`os`/`cpu` 写进 `packages`。

3. **锁文件里的 `resolved` 统一用 `registry.npmjs.org`**。混入镜像源（如 npmmirror）在 CI 环境可能 400 或超时；`integrity` 是内容摘要，换源不影响校验。

### 9.3 本地复现 CI 的正确方式

```bash
# 只验前端阶段（最快）
cd app
docker build -f deployments/Dockerfile --target frontend-builder \
  --platform linux/amd64 --progress=plain -t test-front .

# 完整镜像；两个架构都要试，arm64 才会暴露 musl 相关的绑定问题
docker build -f deployments/Dockerfile --platform linux/amd64 -t test-amd64 .
docker build -f deployments/Dockerfile --platform linux/arm64 -t test-arm64 .
```

`--progress=plain` 必须加，否则错误信息被截断看不到真实原因。

**`npx` 会骗你**：本机全局可能装了另一个版本的 `vue-tsc`，`npx vue-tsc --version` 显示的未必是 `node_modules` 里的那个。要用 `./node_modules/.bin/vue-tsc`，或直接看 `node_modules/vue-tsc/package.json` 的 version。

**npm 缓存也会骗你**：`npm ci` 复用旧缓存时会装出与锁文件不符的版本。验证锁文件时用干净缓存：`npm ci --cache /tmp/fresh --prefer-online`。

**arm64 的 `apk add` 偶发 SSL EOF** 是网络抖动，重试即可，不是代码问题。

### 9.4 其它注意

- 改过 `manifest.json`（浏览器扩展权限）后，必须在 `chrome://extensions` 手动刷新扩展，否则新权限不生效且错误是静默的。
- `gh release edit --notes-file` 会**整体替换**说明正文。工作流自动附加的镜像地址会被冲掉，编辑后要补回，或直接用脚本重跑一次发布。
- 发布是手工触发的，不会因为 push 自动跑；tag 已存在时脚本会中止，不会覆盖已发布的版本。

---

## 10. 文档索引（doc/，按能力分目录、中文命名）

| 文档 | 用途 | 信任度 |
|------|------|--------|
| `doc/文档索引.md` | 全部文档的分类索引，含新增约定 | 入口 |
| `doc/搜索下载能力/01-需求文档.md` | 搜索下载 PRD（BR 权威） | 实现参考 |
| `doc/搜索下载能力/03-设计说明书.md` | 搜索下载设计 | 参考 |
| `doc/搜索下载能力/04-测试计划.md`、`04-测试状态.md` | 用例与执行结果（TST 编号） | 参考 |
| `doc/搜索下载能力/架构决策/决策*.md` | 10 份 ADR（选型与取舍理由） | 参考 |
| `doc/字幕Agent能力/需求文档.md` | MCP 能力 PRD（BR 权威） | 实现参考（代码已落地） |
| `doc/项目文档/项目架构设计.md` | 原始设计文档 | **已漂移**：无 `dto/`/`enum/` 目录，实际有 auth/dashboard/health/media_card 控制器与 watcher 服务等。**以代码为准** |
| `doc/项目文档/开发任务执行计划.md` | 阶段任务与里程碑 | 历史 |
| `doc/项目文档/媒体库刷新通知设计.md` | 媒体库刷新/通知设计 | 参考 |
| `doc/参考资料/MoviePilot刮削与整理功能解析.md` | 从 MoviePilot 移植逻辑分析 | 参考 |

新增文档约定：能力同名子目录 + `01-需求` / `02-审核` / `03-设计` / `04-测试` / `05-追溯` 编号；决策放该目录下 `架构决策/`；文档间引用写**从仓库根起的完整相对路径**。

---

## 11. 快速上手（改代码前 checklist）

1. 从 `internal/router/router.go` 读装配，确认新能力如何接线。
2. 复用优先：字幕/媒体逻辑用 `internal/mediautil`；文件操作用 `internal/storage` 接口；出站请求用 `internal/service/httpclient`（不要裸用 `http.Client`）；筛选类纯逻辑放 `web/src/lib/` 并配单测。
3. 需要持久化 → 新实体放 `internal/model/entity/`，仓储构造器 `AutoMigrate`。
4. 配置项走 默认值/`BUJIC_` env/DB 三层，别新增 config.yaml；需要热生效的用 resolver 模式（参考全局代理）。
5. 涉及 MCP → 守第 8.3 节安全红线，并向 `doc/字幕Agent能力/需求文档.md` 的 BR 回填对齐。
6. 涉及搜索下载 → 守 4.9 的关键约束；改前端时注意 6.2 的组件与路由过渡坑。
7. 跑 `go build ./...`、`go test ./...`（需 `app/dist` 存在）；前端改完 `cd app/web && npm run build` + `npx vitest run`。
8. 提交信息用中文，按功能域拆分。
9. 发版前读第 9 节，尤其 9.2 与 9.3。
