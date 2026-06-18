# 媒体库刷新通知机制 — 设计文档（与实现同步）

> 目标：**转移并刮削完成后**，按「目录」绑定的「媒体服务器」，通知其刷新媒体库（可选**整库**或**指定某个媒体库**），使新入库内容尽快出现在播放端；并提供**手动刷新**、**测试连接**、**在线心跳**。
> 参考：`reference/MoviePilot` 媒体服务器模块。
>
> **术语**（与前端展示一致）：
> - **媒体服务器**：一台 Emby / Jellyfin / Plex 实例（代码实体仍为 `MediaLibrary`，表 `media_libraries`）。
> - **媒体库**：媒体服务器内部的库/分区（Emby/Jellyfin 的 Media Folder、Plex 的 Section）。
> - **目录**：下载源→归档目录的整理配置（代码实体仍为 `MediaCard`，表 `media_cards`；前端原「媒体卡片」已更名为「目录管理」）。

---

## 1. 需求范围

**已实现：**
- 「媒体服务器」为 **SQLite 持久化实体**，前端用户录入并维护（增删改查），模式对齐现有 `media_cards`。
- 「目录」（`media_cards`）可绑定一个媒体服务器。
- 当某目录对应的内容「转移 + 自动刮削」完成后，**自动通知其绑定的媒体服务器刷新**。
- 刷新范围**可选**：编辑媒体服务器且**密钥正确时自动加载其媒体库列表**，可选「全部媒体库」或**某一个媒体库**；刷新时仅刷新所选范围。
- 每台媒体服务器提供 **手动刷新**、**测试连接**、**在线状态心跳（绿点）**。
- 支持 Emby / Jellyfin / Plex。
- 刷新**异步、不阻塞、失败只记日志**（不影响整理/刮削）。
- 刷新目标按服务器去抖合并（5s），避免短时间重复刷新。

**非目标（未来扩展）：**
- 聊天类消息通知（TG/企业微信等）。
- 手动刮削（无目录上下文）自动触发刷新——本期仅整理链路自动触发（但可用手动刷新按钮）。
- 按文件路径的增量刷新（需「应用路径 ↔ 服务器路径」映射，复杂度高，暂不做）。
- 一个目录绑定多台服务器 / 一台服务器选择多个库（当前单选）。

---

## 2. 数据模型

### 2.1 实体 `MediaLibrary`（表 `media_libraries`，前端称「媒体服务器」）

| 字段 | 类型 | 说明 |
|:---|:---|:---|
| `ID` | uint | 主键 |
| `Name` | string | 用户自定义名称，如「家庭 Emby」 |
| `Type` | string | `emby` / `jellyfin` / `plex` |
| `URL` | string | 如 `http://192.168.1.10:8096` |
| `APIKey` | string | Emby/Jellyfin 的 API Key，或 Plex 的 Token（统一存此字段） |
| `Enabled` | bool | 是否启用 |
| `LibraryID` | string | 选定要刷新的服务器内媒体库 ID；**为空表示「全部媒体库」** |
| `LibraryName` | string | 选定媒体库名称（用于前端展示） |
| 时间戳/软删 | — | 与 `MediaCard` 一致 |

仓库 `MediaLibraryRepository`：`Create/Update/Delete/GetByID/List`，构造函数内 `AutoMigrate(&MediaLibrary{})`。

### 2.2 `MediaCard` 绑定字段

```go
MediaLibraryID uint `gorm:"column:media_library_id;default:0" json:"media_library_id"`
```
`0` 表示未绑定（该目录整理后不触发刷新）。GORM `AutoMigrate` 自动补列。

---

## 3. 触发链

```
整理 executeTransfer ──> debounceScrape(destDir, mediaType, cardID)   // 5s 防抖
                              └─(5s 后)─> ScrapePathWithType(destDir)  // 刮削
                                              └─成功─> notifier.NotifyRefreshForCard(cardID)
                                                          │ 查 card.MediaLibraryID → 查 MediaLibrary
                                                          │ 按 Type 构建 mediaserver.Server
                                                          └─异步(按服务器 5s 去抖)─> server.Refresh(lib.LibraryID)
                                                                                       // LibraryID 为空=全部库，否则=该库
```

- **触发点**：`transfer_service.go` 的 `debounceScrape` 中，`ScrapePathWithType` 返回成功之后。此处恰为「转移完 + 刮削完」，且能拿到 `cardID`。
- 三个 `debounceScrape` 调用点（目录 / 单文件 / 蓝光）都已持有 `cardID`，统一透传。
- 手动刮削（`ScrapeController`）不在此链路，本期不自动触发刷新（可用手动刷新按钮）。

---

## 4. 组件设计

### 4.1 `pkg/mediaserver`（公共包，HTTP 风格对齐 `pkg/tmdb`）

```go
type ServerType string // "emby" | "jellyfin" | "plex"

type Library struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}

type Server interface {
    TestConnection(ctx context.Context) error
    ListLibraries(ctx context.Context) ([]Library, error)
    Refresh(ctx context.Context, libraryID string) error // libraryID 为空 → 刷新全部库
}

func New(t ServerType, baseURL, apiKey string) (Server, error)
```

各类型端点（经 MoviePilot 源码与官方实践确认）：

| 类型 | 列举媒体库 | 刷新单个库 | 刷新全部 | 鉴权 | 连接测试 |
|:---|:---|:---|:---|:---|:---|
| Emby | `GET {url}/emby/Library/VirtualFolders`（取 `ItemId`） | `POST {url}/emby/Items/{id}/Refresh?Recursive=true` | `POST {url}/emby/Library/Refresh` | Header `X-Emby-Token` | `GET /emby/System/Info` |
| Jellyfin | `GET {url}/Library/VirtualFolders`（取 `ItemId`） | `POST {url}/Items/{id}/Refresh?Recursive=true` | `POST {url}/Library/Refresh` | Header `X-Emby-Token` | `GET /System/Info` |
| Plex | `GET {url}/library/sections` | `GET {url}/library/sections/{key}/refresh` | 列出 sections 后逐个 refresh | Query `X-Plex-Token` | `GET /identity` |

- 内置 `http.Client{Timeout:15s}`、失败重试 1 次、请求日志写 `data/logs/mediaserver_api.log`（Token 掩码）。
- Emby 与 Jellyfin 共用一份实现（差异仅在 `/emby` 路径前缀）。
- 刷新不传文件路径，因此不依赖「应用路径 ↔ 服务器路径」一致；刷新粒度为「整库」或「某个库」。

### 4.2 `service.MediaLibraryService`（CRUD + 测试 + 探测 + 心跳 + 刷新）

```go
type MediaLibraryService interface {
    Create/Update/Delete/GetByID/List(...)
    TestConnection(ctx, id) error
    Refresh(ctx, id) error                              // 刷新该服务器的选定库（lib.LibraryID）
    ProbeLibraries(ctx, serverType, url, apiKey) ([]mediaserver.Library, error) // 编辑表单按凭据列举库
    Statuses(ctx) []LibraryStatus                       // 心跳：并发 TestConnection，返回各服务器在线状态
}
```
- `ProbeLibraries`：用「未保存的」类型/地址/密钥即时列举库，供编辑表单下拉。
- `Statuses`：并发对每台启用的服务器做 6s 超时的 `TestConnection`，返回 `[{id, online}]`。

### 4.3 `service.NotificationService`（刷新编排）

```go
type NotificationService interface {
    NotifyRefreshForCard(ctx context.Context, cardID uint)
}
```
- 依赖 `MediaLibraryRepository` + `MediaCardRepository`。
- `cardID==0` / 卡片未绑定 / 库未启用 → 直接返回；否则**异步 + 按服务器 ID 防抖（5s）**，构建 `mediaserver.Server` 并 `Refresh(lib.LibraryID)`。
- 失败 `logger.Warn`，成功 `logger.Info`（经现有 `LogBroadcaster` 自动推送前端日志面板）。

---

## 5. API（`/api/v1`，均在 `AuthRequired` 保护下）

| 方法 | 路径 | 说明 |
|:---|:---|:---|
| GET | `/libraries` | 列表 |
| GET | `/libraries/status` | 心跳：返回各服务器在线状态 `[{id, online}]` |
| GET | `/libraries/:id` | 详情 |
| POST | `/libraries` | 新增 |
| POST | `/libraries/probe` | 按 `{type,url,api_key}` 列举服务器内媒体库（编辑表单用） |
| PUT | `/libraries/:id` | 修改 |
| DELETE | `/libraries/:id` | 删除 |
| POST | `/libraries/:id/test` | 测试连接 |
| POST | `/libraries/:id/refresh` | 手动刷新（按选定库范围） |

> 注：`/libraries/status`、`/libraries/probe` 为静态段，与 `/libraries/:id` 并存（gin 支持，类似既有 `/cards/default` 与 `/cards/:id`）。
>
> 目录接口复用现有 `/cards*`，请求体新增 `media_library_id`。

---

## 6. 依赖注入（`router.go`）

```go
mediaLibraryRepo := repository.NewMediaLibraryRepository(gormDB)
notificationSvc  := service.NewNotificationService(mediaLibraryRepo, mediaCardRepo)
transferSvc      := service.NewTransferService(..., mediaCardRepo, notificationSvc) // 末尾 +1 参数
mediaLibrarySvc  := service.NewMediaLibraryService(mediaLibraryRepo)
mediaLibraryCtrl := controller.NewMediaLibraryController(mediaLibrarySvc)
```

---

## 7. 前端（`web/src/pages/SettingPage.vue`）

- **Tab 布局**：系统设置页拆分为四个 Tab —— **常规**（TMDB + 整理刮削规则 + 保存）｜**目录管理**｜**媒体服务器**｜**安全**（改密码）。保留 slate+amber 暗色主题，分段式 Tab 栏（amber 高亮）+ 切换淡入与内容错位上浮动效。
- **重命名**：原「媒体卡片」→「目录管理」；原「媒体库」→「媒体服务器」。
- **媒体服务器区块**：增删改查表单（名称/类型/地址/密钥/启用）；卡片标题前显示**在线绿点**（轮询 `/libraries/status`，每 30s）；操作按钮含**刷新 / 测试连接 / 编辑 / 删除**。
- **按库选择**：编辑表单中地址+密钥变化时（去抖 600ms）调 `/libraries/probe`；**仅当探测成功（密钥正确）时**展示「刷新的媒体库」下拉（「全部媒体库」或具体某个），保存 `library_id` + `library_name`。
- **目录绑定**：目录表单中「所属媒体服务器」下拉（值为 `media_library_id`，含「不绑定」）。
- **提示修复**：`main.ts` 引入 `vue-sonner/style.css`，修复 toast（含测试连接结果）此前不显示的问题。

---

## 8. 改动文件清单

**后端（新增）**：`entity/media_library.go`、`repository/media_library_repo.go`、`pkg/mediaserver/{mediaserver,emby,plex}.go`(+`mediaserver_test.go`)、`service/media_library_service.go`、`service/notification_service.go`、`controller/media_library_controller.go`
**后端（修改）**：`entity/media_card.go`（+`media_library_id`）、`service/transfer_service.go`（注入 notifier + `debounceScrape` 透传 cardID + 触发）、`router/router.go`（接线 + 路由）
**前端（修改）**：`web/src/main.ts`（toast 样式）、`web/src/pages/SettingPage.vue`（Tab 布局 + 媒体服务器 CRUD/心跳/按库选择 + 目录绑定 + 重命名）

---

## 9. 测试与验证

- `pkg/mediaserver`：`httptest` 模拟三类服务器，校验 列举库 / 刷新单库 / 刷新全部 / 鉴权 路径正确，失败不 panic（`TestEmbyRefreshAll`、`TestEmbyRefreshSingleLibrary`、`TestEmbyListLibraries`、`TestJellyfinRefreshAllUsesRootPrefix`、`TestPlexRefreshAllRefreshesEverySection`、`TestPlexRefreshSingleSection` 等）。
- `go build ./... && go vet ./... && go test ./...` 全绿。
- 前端 `vue-tsc -b` 类型检查 + `vite build` 通过。
- 手工：配置一台媒体服务器 → 选定刷新范围 → 绑定到目录 → 触发整理 → 日志出现刷新记录；卡片绿点随心跳更新；手动刷新按钮可即时触发。
