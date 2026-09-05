# Bujic Movie 影视字幕 Agent 能力 — MCP Server + Skill 产品需求文档（PRD）

> 目标产品形态：在既有 Web UI 之外，面向 **Agent/LLM 工具调用** 场景开放"影视列表查询 / 字幕获取 / 字幕上传"能力；并新增 **MCP API Key 生命周期管理**（创建 / 启用·禁用 / 调用记录），形成 `Bujic Movie ↔ Agent` 的可审计、可管控闭环工作流。

---

## 文档信息

| 项目 | 内容 |
|------|------|
| **文档名称** | 影视字幕 Agent 能力 MCP Server + Skill PRD |
| **版本** | v0.3（prd-reviewer 四维审核回填版） |
| **日期** | 2026-09-05 |
| **状态** | Draft — v0.3（已回填四维审核结论；仍可在实施前再评审） |
| **适用范围** | `app/`（Go 后端）、`app/cmd/mcp/`（独立 MCP 进程）、`.agents/skills/bujic-subtitle/`（Agent Skill） |
| **关联文档** | `doc/项目架构设计.md`、`doc/开发任务执行计划.md`、`AGENTS.md` |
| **外部依赖 Skill** | `subtitle-translator-zh`（本 PRD 翻译规范的方法论来源，MIT 溯源见 §8.2） |

---

## 0. 原始需求基线（追溯表）

> 所有设计必须可向上回溯到原始需求。`<scope>` 标记表示该条为既有 REST 能力，本期仅做后端迁移/统一封装。

| # | 原始需求（原话/语义） | 产品含义拆解 | 本文档落地位置 |
|---|----------------------|--------------|----------------|
| R-01 | 提供影视列表查询（包括已存在字幕） | Agent 可按类型/媒体库/关键词查询媒体清单，且能感知每条媒体"已有哪些字幕（外挂 + 内嵌）"，避免重复下载 | UC-01、UC-02、BR-01、§3.1 |
| R-02 | 字幕文件获取 | Agent 可拉取指定媒体/指定视频文件的某条字幕原文（外挂文件内容 或 内嵌轨道）用于后续处理 | UC-03、BR-09 |
| R-03 | 字幕文件上传 | Agent 可将处理好的字幕文件（含中文语言标记）写入对应媒体目录，命名自动适配媒体库扫描规则 | UC-04、BR-10 |
| R-04 | 提供接口或 MCP Server 服务 | **决策：仅 MCP Server**。提供内嵌/独立 MCP Server，将上述能力暴露为 MCP tools；MCP 以 API Key 鉴权 | §1.3、§4.1、UC-01~UC-04 |
| R-05 | 配合 Skill 支持 Agent 工具调用闭环：查询影视列表 → 下载英文字幕 → 翻译为中文字幕并上传 | **决策：仅 Agent 自行翻译**。后端/Skill 不内置翻译通道；Agent 拉取英文字幕原文后自行中译并上传。翻译要求对齐 `subtitle-translator-zh` | UC-05、§4.2、§8.2、BR-28 |
| R-06 | **新增** 创建 API Key 功能，用于 MCP 接口调用 | 管理员可创建独立访问凭证（名称 + 一次性明文 + 服务端哈希存储），MCP Client 凭此鉴权 | UC-06、BR-23/BR-24、§4.1 |
| R-07 | **新增** API Key 具备 启动（启用）/ 禁用 功能 | 可对单个 Key 启用/禁用；禁用即即时失效，可再启用；本期不提供删除（禁用即作废） | UC-07、BR-25 |
| R-08 | **新增** 查看 API Key 调用记录 | 可按 Key/工具/状态/时间查询 MCP 工具调用历史（不含字幕正文/文件内容） | UC-08、BR-26/BR-27、§5.4 |
| R-09 | **翻译要求参考 subtitle-translator-zh** | Skill 中"翻译为中文字幕"一步的方法论、格式守则、错误处理与稳定性要求，整体对齐 `subtitle-translator-zh` | UC-05、§8.2（S/ERR 全量） |

> **一致性声明**：本期新增"Agent/MCP 访问面"与"API Key 管理体系"，既有 REST 接口（`/api/v1/media`、`/subtitles/*` 等）保持不变，作为 MCP Server 底层可复用实现。MCP 鉴权由「全局静态 token（v0.1 设想）」升级为「**按 Key 管理、可启停、可审计的 API Key**」——凡 v0.1 提及 `mcp_access_token` 之处一律作废，以本文档 v0.3 §4.1/§6.3 为准。冲突处见 §8.3。

---

## 1. 产品概述

### 1.1 产品背景

- Bujic Movie 已具备：媒体库（`medias` 表）、媒体卡（`media_cards`，`ArchivePath`/`DownloadPath`）、字幕**磁盘态**管理（外挂 `<videoBase>.<lang>.<ext>`；内嵌 ffprobe/ffmpeg）。
- 现状缺口：
  1. `/api/v1/media` 列表与搜索**不含**任何字幕信息，字幕须逐个调 `/media/subtitles` —— Agent 无法一次判断"这部电影缺中文字幕吗"。
  2. 能力仅以 **REST + JWT** 暴露，**无 MCP 协议**，Agent 编排（Claude Code/Codex/OpenCode 等 MCP 生态）接入成本高。
  3. 仓库内无配套 Agent Skill，工具调用流程与字幕格式/命名约定无人沉淀。
  4. **无机器对机器的可撤销凭证**：JWT 是"人"的会话（24h、可登录态），不宜直接下发 Agent；需要一个可创建/启停/审计的 **API Key** 体系。
  5. **无调用记录**：Agent 做了什么无法追溯审计。
  6. 既有字幕管理是"UI 手动操作"，翻译链路（尤其中译）缺乏一份以专业字幕翻译方法论（`subtitle-translator-zh`）为基准的规范。

### 1.2 产品目标（含可测指标）

| 目标 | 可测指标 | 达成验证 |
|------|----------|----------|
| G-01 Agent 一条工具调用拿到"媒体列表 + 每项字幕概览" | 列表项含 `has_subtitle`/`languages`/`missing_subtitles` | Go 单测 `TestAgentSubtitleFlow`（§6.5 V-1） |
| G-02 Agent 可拿到任意字幕的文本/字节用于翻译 | 外挂直读、内嵌抽取两类路径 | §6.5 V-2/V-3 |
| G-03 Agent 可把翻译结果写回且保持媒体库可识别 | 文件名 `<videoBase>.zh-CN.srt`，上传后媒体卡刷新可见 | §6.5 V-4 |
| G-04 无状态、幂等、低心智负担的工具面 | 每 tool 一次调用完成一件语义完整的事 | §4.1 |
| G-05 复用既有安全模型，不开口子 | 路径读写沿用 MediaCard 前缀白名单；**MCP 仅接受有效 API Key** | §5.3/§6.3、BR-09 |
| G-06 可创建 API Key 用于 MCP 调用 | 创建返回一次性明文；库中仅存哈希；列表/记录均不回显明文 | UC-06、§6.5 V-8 |
| G-07 API Key 可启用/禁用 | 禁用后 MCP 调用即时被拒（401 + `KEY_DISABLED`），再启用恢复 | UC-07、§6.5 V-8 |
| G-08 可查看 API Key 调用记录 | 每次业务 `tools/call`（4 tool）落库（Key/工具/状态/耗时/IP/时间）；`mcp_ping` 自测不计入；可按条件查询 | UC-08、§6.5 V-9/V-14 |
| G-09 翻译规范与 `subtitle-translator-zh` 对齐 | Skill 的翻译子任务在逐行映射、时间轴保留、术语一致性、格式守则、错误处理上符合 §8.2 | BR-28、§6.5 V-10 |

### 1.3 系统范围

| 组件 | 职责 | 形态/位置 |
|------|------|-----------|
| **Bujic Movie 核心后端** | 媒体库元数据、字幕盘态扫描/探测/抽取、目录刷新、既有权验逻辑 | 既有 `app/internal/*` |
| **Subtitle Agent 服务层（新增）** | 列表含字幕 / 字幕明细 / 字幕获取（外挂直读 + 内嵌抽取）/ 字幕上传 的进程内函数 | `app/internal/service/subtitle_agent_service.go`，`router.go` 装配 |
| **MCP Server（新增）** | MCP Streamable HTTP 暴露 4 个 tool；鉴权 + 调用记录落库 | `app/internal/mcp/`（内嵌 handler）；`app/cmd/mcp/main.go`（独立进程） |
| **MCP API Key 管理（新增）** | 创建/启用/禁用 API Key、调用记录查询 | `service/mcp_api_key_service.go`、`repository/mcp_api_key_repo.go` + `mcp_call_record_repo.go`、`controller/mcp_api_key_controller.go` |
| **数据实体（新增）** | `mcp_api_keys`、`mcp_call_records` 两表（AutoMigrate） | `app/internal/model/entity/` |
| **Agent Skill（新增）** | 固化 MCP 编排 + 字幕翻译规范（对齐 `subtitle-translator-zh`） | `.agents/skills/bujic-subtitle/SKILL.md` |
| **Web UI** | 设置页新增 **MCP/API Key 管理**区块（§5.4） | `app/web/` |

---

## 2. UML 用例模型

### 2.1 参与者定义

| 参与者 | 类型 | 说明 |
|--------|------|------|
| Agent（AI 代理） | 系统角色 | 经 MCP tools（API Key 鉴权）调用各项能力的 LLM 编排方 |
| 系统管理员（已登录用户） | 人 | Web UI 使用方；**创建/启停 API Key、查看调用记录**；也可直接使用 Web 面 |
| Bujic Movie 系统 | 子系统 | 媒体卡、存储抽象、TMDB 元数据、通知服务、MCP 网关 |

### 2.2 系统用例图（ASCII）

```
                        ┌────────────────────────────────────────────────┐
                        │             Bujic Movie 系统边界                 │
                        │                                                │
  ┌─────────┐   MCP      │   ┌────────────────────────────────────────┐  │
  │  Agent  │  (tools+   │   │         Subtitle Agent 服务层           │  │
  └────┬────┘  API Key)  │   │   query_media_list      (UC-01)        │  │
       │                │   │   query_media_subtitles  (UC-02)        │  │
       │                │   │   fetch_subtitle         (UC-03)        │  │
       │                │   │   upload_subtitle        (UC-04)        │  │
       │                │   │─────────────────────────────────────────│  │
       │                │   │   磁盘扫描 / ffprobe / ffmpeg            │  │
       │                │   └────────────────────────────────────────┘  │
       │                │         ▲ 鉴权(API Key) + 调用记录落库          │
  ┌────┴─────┐   REST    │   ┌────────────────────────────────────────┐  │
  │ 管理员    │  /api/v1  │   │         MCP API Key 管理面            │  │
  │(已登录)   │ ────────►│   │   创建 API Key      (UC-06)            │  │
  └────┬─────┘   JWT     │   │   启用/禁用 API Key (UC-07)            │  │
       │                │   │   查看调用记录      (UC-08)            │  │
  ┌────┴─────┐           │   └────────────────────────────────────────┘  │
  │ 用户(Web)│ 既有 UI    │       既有 MediaController / Repositories     │
  └──────────┘ ─────────►│                                               │
                        └────────────────────────────────────────────────┘
```

### 2.3 用例列表

| 用例编号 | 用例名称 | 参与者 | 优先级 |
|----------|----------|--------|--------|
| UC-01 | 查询媒体列表（含字幕状态） | Agent | 高 |
| UC-02 | 查询单个媒体/视频的字幕明细 | Agent | 高 |
| UC-03 | 获取字幕内容 | Agent | 高 |
| UC-04 | 上传字幕文件 | Agent | 高 |
| UC-05 | Agent 全链路：英文下载→中译→中文上传（Skill 编排，翻译对齐 subtitle-translator-zh） | Agent | 高 |
| UC-06 | 创建 API Key | 系统管理员 | 高 |
| UC-07 | 启用/禁用 API Key | 系统管理员 | 高 |
| UC-08 | 查看 API Key 调用记录 | 系统管理员 | 高 |

### 2.4 用例关系

```
UC-01 查询媒体列表（含字幕状态）
   ├──<include>── 读取 MediaCard（限定媒体库范围，缺省用默认卡）
   └──<extend>──> UC-02 查询媒体/视频字幕明细

UC-03 获取字幕内容
   ├──<extend>──>（内嵌）ffprobe 探测 + ffmpeg 抽取
   └── 输入来源可以是 UC-02 返回的 subtitle 记录

UC-04 上传字幕文件
   └──<include>── 校验目标视频路径位于 MediaCard 白名单

UC-05 Agent 全链路（Skill 编排）
   └── 依次调用 UC-01 → UC-02/UC-03 → UC-04
       （翻译子任务 = subtitle-translator-zh 方法论，见 §8.2）

UC-06 创建 API Key ──<include>── 校验身份(已登录 JWT)
UC-07 启用/禁用 API Key ──<include>── 读取/更新 Key 状态
UC-08 查看调用记录 ──<extend>──（记录来源）每次 MCP tools/call 自动落库
```

---

## 3. 详细用例规格说明

> 约定：Agent 工具运行在"媒体库=MediaCard"世界观里：范围 `media_card_id`（缺省→默认卡）；媒体标识 `media_id`（`medias.id`）；`path` 为服务器绝对路径。
>
> 说明（既有模型约束，BR-01）：`GET /api/v1/media` 响应是**聚合后的"卡片"形态**（电影按 TMDBID 分组；剧集聚合成 `剧名 (第 N 季)`，`path`=季目录，`tmdb_id`=剧集级）。本期不打破该聚合，新增 `has_subtitle` 聚合口径见 BR-01。

### 3.1 UC-01 查询媒体列表（含字幕状态）

| 项目 | 内容 |
|------|------|
| **用例编号** | UC-01 |
| **用例名称** | 查询媒体列表（含字幕状态） |
| **参与者** | Agent |
| **优先级** | 高 |
| **前置条件** | 服务已启动；存在至少一张 MediaCard（否则空列表 + `warn`）；API Key 有效 |
| **后置条件** | 无副作用（纯查询）；内嵌探测结果落入缓存（BR-03）则刷新缓存；本次调用落调用记录（BR-26） |
| **基本事件流** | 1. Agent 提交：`media_type`、可选 `media_card_id`、可选 `query`、可选 `page`/`limit`<br>2. 取库：`mediaRepo.ListAll/List`<br>3. 按 `groupMedias` 聚合；以视频文件粒度计算外挂 + 内嵌（三态口径 BR-03）<br>4. 组装：`media_id/title/type/year/season/path/has_subtitle/subtitle_status/languages/missing_subtitles/warn`<br>5. 返回 `{items,total,page,limit}` |
| **备选事件流** | 1a. 无 MediaCard：空列表 + `warn:"no media card"`<br>3a. 剧集聚合项按该季全部视频文件聚合，`has_subtitle=true` 当且仅当存在任一外挂/内嵌（BR-01/BR-03）；内嵌覆盖度见 `subtitle_status`<br>4a. 视频文件已被外部删除：`has_subtitle=false` + `warn`，跳过 ffprobe |
| **业务规则** | BR-01：**聚合口径**——外挂：同目录 `videoBase` 前缀 + `IsSubtitle` 即视为有；内嵌：ffprobe 探测到任一 `Subtitle` 流即视为有；`has_subtitle`=二者取并；`languages` 只列**外挂**语言（内嵌不枚举，见 BR-03）<br>BR-02：仅取 MediaCard `ArchivePath`（归档媒体库），`DownloadPath` 不进入媒体列表<br>BR-03：**列表层内嵌探测性能约束与三态口径**——禁止逐文件强 ffprobe；内嵌结果用进程级缓存（键=`videoPath\|mtime\|size`，TTL≥5min；缓存 map 需 `sync.Mutex`/分片锁，BR-30）。内嵌探测**覆盖度**须以三态 `subtitle_status` 明示，避免"未探测"被误报为"确无字幕"：<br> `full`=媒体范围内全部视频的内嵌均已探测（缓存命中或 ≤200 文件同步探测）；<br> `partial`=部分文件缓存未命中且范围 >200 文件，未同步探测——此时 `has_subtitle` 以内嵌缓存命中结果为准，未命中部分**不参与** true 判定，并附 `warn:"subtitle status partial (internal not scanned)"`；<br> `none`=全部文件已探测且确无外挂/内嵌。<br> 单次 tool 调用内 ffprobe 总数 ≤16（BR-31）。后台可对 `partial` 范围做低优先级预热以逐步收敛到 `full`（可选） |
| **数据说明表** | 见 §3.1.1/§3.1.2 |

#### 3.1.1 入参

| 字段名 | 字段中文名 | 数据类型 | 取值范围 | 是否必填 | 备注说明 |
|--------|------------|----------|----------|----------|----------|
| media_type | 媒体类型 | STRING | `movie`/`tv` | 否 | 缺省全部 |
| media_card_id | 媒体库范围 | INT | ≥1 | 否 | 缺省→默认卡；0→全部卡 |
| query | 关键词 | STRING | ≤100 字符 | 否 | 标题模糊 |
| page | 页码 | INT | ≥1 | 否 | 缺省 1 |
| limit | 每页条数 | INT | 1~200 | 否 | 缺省 50 |

#### 3.1.2 出参（列表项字段）

| 字段名 | 字段中文名 | 数据类型 | 取值范围 | 是否必填 | 备注说明 |
|--------|------------|----------|----------|----------|----------|
| media_id | 媒体记录 ID | INT | 正整数 | 是 | `medias.id` |
| title | 标题 | STRING | — | 是 | 剧集聚合项 `剧名 (第 N 季)` |
| type | 类型 | STRING | `movie`/`tv` | 是 | |
| year | 年份 | INT | — | 否 | |
| season | 季号 | INT | ≥0 | 否 | 电影 0 |
| path | 路径 | STRING | 绝对路径 | 是 | 电影=视频文件；剧集=季目录 |
| has_subtitle | 是否有字幕 | BOOLEAN | true/false | 是 | 口径 BR-01/BR-03；`partial` 时未命中部分不参与 true 判定 |
| subtitle_status | 内嵌探测覆盖度 | STRING | `full`/`partial`/`none` | 是 | 见 BR-03；`full` 才可断言"确无字幕" |
| languages | 外挂字幕语言 | STRING[] | `en`/`zh-CN`/`zh-TW`/… | 否 | 去重；外挂为准 |
| missing_subtitles | 缺失提示 | STRING[] | `zh-CN` 等 | 否 | **仅当 `subtitle_status=full` 且确无对应外挂时给出**；`partial` 时省略以免误导 |

| **接口说明** | MCP tool `query_media_list`（§4.1）；经 API Key 鉴权；落调用记录 |
|------------|----|

### 3.2 UC-02 查询单个媒体/视频的字幕明细

| 项目 | 内容 |
|------|------|
| **用例编号** | UC-02 |
| **用例名称** | 查询媒体/视频字幕明细 |
| **参与者** | Agent |
| **优先级** | 高 |
| **前置条件** | 目标媒体存在；API Key 有效 |
| **后置条件** | 无（纯查询） |
| **基本事件流** | 1. 二选一定位：a. `media_id`；b. `path`(+可选 `media_card_id`)<br>2. 按 `media_id` 解析：电影→单视频；剧集行→该集；若为季目录 `path`→返回该季全部集<br>3. 逐视频盘点：外挂扫同目录 `videoBase.*`；内嵌 `mediainfo.Probe`(≤2s) 记录 `index/language/format`<br>4. 返回字幕记录数组 |
| **备选事件流** | 2a. `media_id` 为剧集聚合：按 `GetEpisodes` 展开逐集盘点<br>3b. 内嵌探测失败/无 ffprobe：返回外挂结果 + `warn:"internal probe failed"` |
| **业务规则** | BR-04：字幕记录带足够定位信息使 UC-03 无需二次猜测（外挂含 `path`；内嵌含 `video_path`+`index`+`language`）。复用 `SubtitleInfo` 结构<br>BR-05：内嵌格式映射沿用现有（`subrip→srt`、`ass/ssa→ass`、`webvtt→vtt`、`pgs→sup(copy)`、`dvd_subtitle→sub(copy)`）；语言空/unknown 给 `track<index>` 兜底名 |
| **数据说明表** | 入参：`media_id` 或 `path`（必填其一）、`media_card_id?`、`include_internal?`（缺省 true）<br>出参：见 §5.3 示例 |
| **接口说明** | MCP tool `query_media_subtitles`；对齐既有 `GetEpisodes`/`ListSubtitles` |
|------------|----|

### 3.3 UC-03 获取字幕内容

| 项目 | 内容 |
|------|------|
| **用例编号** | UC-03 |
| **用例名称** | 获取字幕内容 |
| **参与者** | Agent |
| **优先级** | 高 |
| **前置条件** | 目标字幕存在；API Key 有效 |
| **后置条件** | 无持久副作用（内嵌抽取写临时文件后清理） |
| **基本事件流** | 1. 定位：a. `path`（外挂）；b. `video_path`+`internal_index`（内嵌）<br>2. 白名单校验（BR-09）<br>3. 外挂读文件（`storage.Read`）；内嵌 ffmpeg 抽取→读→清理（≤60s）<br>4. 返回 `content/content_base64/is_image/format/encoding/byte_size/language/name` |
| **备选事件流** | 3a. 图像字幕（pgs/dvd）：返回二进制 `content_base64` + `is_image=true`（BR-08）<br>3b. 原始编码非 UTF-8：转 UTF-8 返回，`encoding` 注明原编码（BR-06） |
| **业务规则** | BR-06：文本字幕统一输出 UTF-8；原始编码（GBK/Big5/UTF-16）chardet 检测+转码；失败原样 base64+`warn`<br>BR-07：返回 `format` 告知 `srt`/`ass`（ass 含 `{\\...}` 样式标签，翻译须保留语义，见 §8.2）<br>BR-08：图像字幕返回 `content_base64`+`is_image=true`；Skill 对这类跳过文本翻译（UC-05 备选流）<br>BR-09：**路径安全**——读写/抽取路径必须命中某 MediaCard `ArchivePath`/`DownloadPath` 前缀（`filepath.Clean`+分隔符前缀，对齐 `DownloadSubtitle` 严格版）；否则拒绝 |
| **数据说明表** | 入参：`path` 或（`video_path`+`internal_index`）二选一；`media_card_id?`；<br>出参：`content`、`content_base64`、`is_image`、`format`、`encoding`、`byte_size`、`language`、`name` |
| **接口说明** | MCP tool `fetch_subtitle`；复用既有抽取/读取 |
|------------|----|

### 3.4 UC-04 上传字幕文件

| 项目 | 内容 |
|------|------|
| **用例编号** | UC-04 |
| **用例名称** | 上传字幕文件 |
| **参与者** | Agent |
| **优先级** | 高 |
| **前置条件** | 目标视频存在且位于 MediaCard `ArchivePath` 内；API Key 有效 |
| **后置条件** | 字幕落盘至视频同目录；匹配到 MediaCard 则异步刷新通知 |
| **基本事件流** | 1. Agent 提交：`video_path`、`subtitle_content`(文本) 或 `subtitle_base64`(字节)、`format?`、`language`<br>2. 校验 `video_path` 存在 + 白名单（BR-09）<br>3. 计算目标名：`<videoBase>.<language>.<format>`；语言无法被 `ParseSubtitle` 映射时退回 `<videoBase>.<format>`（对齐既有）<br>4. 文本按 UTF-8、字节原样写入；扩展名白名单（BR-11）<br>5. `storage.Write` → `ChmodWithUmask`<br>6. 触发媒体卡异步刷新（不阻塞返回）<br>7. 返回 `{path,message}` |
| **备选事件流** | 2a. 目标已存在：默认覆盖（对齐既有上传语义）；服务端**不强制**"已存在 zh-CN 即拒"（BR-33），去重由 Skill 按 BR-14 先行查询决定<br>4a. 语言标签与内容不符：服务端不语义校验，返回 `warn:"language label may mismatch content"`（BR-13）<br>4b. `format` 缺省按内容结构推断失败→400 要求显式声明 |
| **业务规则** | BR-10：命名严格 `<videoBase>.<language>.<format>`（语言能被 parser 识别），保证扫描/播放器自动匹配；`zh-CN` 简体、`zh-TW` 繁体；parser 优先级 繁→简→日→英（既有）<br>BR-11：扩展名白名单 `.srt/.ass/.ssa/.sub/.vtt`（对齐 `fileutil.SubtitleExtensions`）；其余拒绝<br>BR-12：文本通道基础嗅探——含 NUL 或非 UTF-8 比例过高则拒绝文本通道要求 `subtitle_base64`；内容仅按数据对待<br>BR-13：语言标签与探测不符仅 `warn` 提示不阻断 |
| **数据说明表** | 入参见流程 1；出参：`path`、`message` |
| **接口说明** | MCP tool `upload_subtitle`；复用既有 `UploadSubtitle` 文件名/写盘/通知逻辑 |
|------------|----|

### 3.5 UC-05 Agent 全链路：下载英文→翻译中文→上传（翻译对齐 subtitle-translator-zh）

| 项目 | 内容 |
|------|------|
| **用例编号** | UC-05 |
| **用例名称** | Agent 全链路（英文→中文翻译→上传闭环） |
| **参与者** | Agent（编排方） |
| **优先级** | 高 |
| **前置条件** | 目标媒体已在媒体库；Agent 具备中译能力；具备可用 API Key |
| **后置条件** | 媒体库出现 `zh-CN` 外挂字幕；媒体卡刷新后 `languages` 含 `zh-CN` |
| **基本事件流** | 1.（UC-01）查列表，确认目标与 `missing_subtitles`（是否缺 `zh-CN`）<br>2.（UC-02）取字幕明细，筛英文字幕（外挂 `en` 优先，否则内嵌 `eng`）<br>3.（UC-03）拉取英文字幕内容 + `format`<br>4.（**翻译子任务**）按 §8.2 `subtitle-translator-zh` 方法论逐行中译（保留时间轴/ass 标签、术语一致性、分批/断点/重试），输出 UTF-8<br>5.（UC-04）`video_path`+`language=zh-CN`+翻译文本上传<br>6.（可选复核）再 UC-02/UC-01 确认落盘与识别 |
| **备选事件流** | 3a. 英文为图像字幕（pgs/sup）：文本翻译不可行——告知用户，或由 Agent 自行判断是否从在线源另取文本版（本期无内置源）<br>4a. 字幕超大：按 §8.2 分批 + ±5 上下文翻译，编号/时间轴连续校验后拼装单文件再上传（BR-16）<br>5a. `warn: language label mismatch`：复核内容语言后重译或改标签 |
| **业务规则** | BR-14：去重/幂等——步骤 1 优先选 `missing_subtitles` 含 `zh-CN` 的媒体；已存在 `zh-CN` 默认跳过（除非用户要求覆盖）<br>BR-15：中文统一简体标记 `zh-CN`；繁体链路不在本期<br>BR-16：超长字幕分段完整性——每段只允许在空行/序号边界切割；段间序号与时间轴连续；全部段完成后拼装为**单一完整文件**再上传一次（禁止逐段覆盖同名目标）<br>BR-28：**翻译规范强制对齐**——翻译子任务必须整体遵循 `subtitle-translator-zh` 方法论（逐行映射/时间轴锚定/±5 行上下文/术语表一致性 `<terminology>`/SRT·ASS·VTT 语法/分批 20–30 条且单次 ≤500/错误处理与 ≤3 次重试/断点续译/交付自检），细则见 §8.2；不满足视为任务未完成<br>BR-29：**调用记录写入可靠性**——`mcp_call_records` 用有界批量缓冲 + 单消费者 goroutine；进程优雅退出（`server.Shutdown`）前强制 flush；缓冲满或单条写失败退化为**同步直写**，保证审计不因崩溃丢批（审核竞态 #3）<br>BR-30：**内嵌探测缓存并发安全**——缓存 map 用 `sync.Mutex`/分片锁保护读写；TTL 清理 timer 与请求并发安全；运行 `go test -race` 无告警（审核竞态 #2）<br>BR-31：**单次调用 ffprobe 预算**——单次 MCP tool 调用内 ffprobe 调用总数 ≤16（超出以缓存/`partial` 兜底，BR-03）；ffprobe/ffmpeg 子进程与并发工具调用共享同一信号量（容量 8，BR-21）<br>BR-32：**`mcp_ping` 自测工具**——工具 5，仅返回 `{ok,server_time,version}`；不读业务数据、**不落调用记录**；用于鉴权连通自测（§5.5）与排除故障（审核易用性 #2）<br>BR-33：**上传去重边界**——服务端**不强制**"已存在 zh-CN 即拒"，默认覆盖对齐既有；去重由 Skill 按 BR-14 执行；上传结果附 `overwrite_existing: true/false`（审核模糊 #7）<br>BR-34：**调用记录保留期清理**——超 180 天记录由常驻定时任务（每小时级 ticker）**分批删除**（`id < cutoff` 分页，事务内），避免长事务锁表（审核竞态 #4）<br>BR-35：**在途调用 vs 禁用边界**——`disabled` 即时生效仅约束**尚未通过鉴权**的新调用；已在执行的在途调用（如长 ffmpeg 抽取）不强制中断，随任务自然结束（审核竞态 #1）<br>BR-36：**同 Key 并发上传同名**——v0 不做乐观锁/版本控制，语义为最后写者生效；调用记录保留 `result_bytes`/`duration_ms` 供事后排查（审核竞态 #6） |
| **数据说明表** | 无新入参；编排见 §4.2 时序图 |
| **接口说明** | 由 Skill 固化；翻译细则对接 §8.2 |
|------------|----|

### 3.6 UC-06 创建 API Key

| 项目 | 内容 |
|------|------|
| **用例编号** | UC-06 |
| **用例名称** | 创建 API Key |
| **参与者** | 系统管理员（已登录用户） |
| **优先级** | 高 |
| **前置条件** | 已通过 JWT 登录（Web UI/REST）；具备系统设置访问权 |
| **后置条件** | `mcp_api_keys` 新增一行；本次调用返回**一次性明文**（此后不可再取）；记录写入 |
| **基本事件流** | 1. 管理员提交：`name`（用途备注）<br>2. 服务端生成随机密钥（`bmk_` + ≥32 字节加密安全随机数，URL-safe 编码）<br>3. 库中仅存 `key_hash`（随机盐 + SHA-256 等）与显示 `key_prefix`（`bmk_<前8位>…`）；`status=active`<br>4. 返回 `{id,name,key(明文·仅此一次),key_prefix,status,created_at}` |
| **备选事件流** | 1a. `name` 为空：允许（系统默认命名），但提示建议填写便于审计<br>1b. 重名：允许创建，返回 `warn` 提示存在同名 Key（区分不同环境应独立命名） |
| **业务规则** | BR-23：**生成与存证**——密钥由 CSPRNG 生成且 ≥32 字节；库中只存 `盐:哈希`（不使用明文/可逆加密）；明文仅创建响应返回一次，任何查询接口不回显，丢失即需重建<br>BR-24：**状态与展示**——`status` 取值 `active`/`disabled`；列表只返回 `key_prefix`（脱敏）与元信息；`last_used_at` 在每次成功鉴权时更新 |
| **数据说明表** | 入参：`name`(STRING≤100, 否)<br>出参：见流程 4 |

**数据实体 `mcp_api_keys`**

| 字段名 | 字段中文名 | 数据类型 | 取值范围 | 是否必填 | 备注 |
|--------|------------|----------|----------|----------|------|
| id | 主键 | BIGINT | ≥1 | 是 | |
| name | 用途备注 | VARCHAR(100) | 任意 | 否 | |
| key_hash | 密钥哈希 | VARCHAR(255) | `盐:哈希` | 是 | 不可逆 |
| key_prefix | 显示前缀 | VARCHAR(32) | `bmk_…` | 是 | 列表展示 |
| status | 状态 | STRING | `active`/`disabled` | 是 | |
| last_used_at | 最后使用 | DATETIME | — | 否 | |
| created_at / updated_at | 时间戳 | DATETIME | — | 是 | |

| **接口说明** | 管理 REST `POST /api/v1/mcp/api-keys`、`GET /api/v1/mcp/api-keys`（列表，脱敏）、`GET /api/v1/mcp/api-keys/:id`（详情），JWT 保护（人通道）；Web UI §5.4 |
|------------|----|

### 3.7 UC-07 启用/禁用 API Key

| 项目 | 内容 |
|------|------|
| **用例编号** | UC-07 |
| **用例名称** | 启用/禁用 API Key |
| **参与者** | 系统管理员 |
| **优先级** | 高 |
| **前置条件** | 已登录（JWT）；目标 Key 存在 |
| **后置条件** | Key 状态翻转；被禁用 Key 的后续 MCP 请求被拒绝并记录 |
| **基本事件流** | 1. 管理员查询 Key 列表（UC-06 出参中 `status` 可见）<br>2. 提交启用或禁用某 `id`<br>3. 服务端更新 `status`<br>4. 返回新状态 |
| **备选事件流** | 2a. 目标不存在：404<br>2b. 已是目标状态（重复禁用/启用）：幂等返回当前状态（不报错）<br>3a. 禁用后仍有在途长任务：读操作即刻拒止新请求；已在执行的 ffmpeg 抽取不强制中断（随任务自然结束） |
| **业务规则** | BR-25：**启停语义**——`disabled` Key 在鉴权层即时失效（MCP 返回 401 + `error_code=KEY_DISABLED`），无需等待会话过期；`active` 恢复后立即可用；本期不提供删除，禁用即达"作废"目的（保留审计记录） |
| **数据说明表** | 入参：`id`(路径)、`action`=`enable`/`disable`<br>出参：`{id,name,status,updated_at}` |
| **接口说明** | REST `PUT /api/v1/mcp/api-keys/:id/enable`、`PUT /api/v1/mcp/api-keys/:id/disable`（JWT 保护） |
|------------|----|

### 3.8 UC-08 查看 API Key 调用记录

| 项目 | 内容 |
|------|------|
| **用例编号** | UC-08 |
| **用例名称** | 查看 API Key 调用记录 |
| **参与者** | 系统管理员 |
| **优先级** | 高 |
| **前置条件** | 已登录（JWT）；存在已发生的 MCP 调用（否则空列表） |
| **后置条件** | 无（只读） |
| **基本事件流** | 1. 管理员选择筛选：`api_key_id?`、`tool?`、`status?`、`time_range?`、`page/limit`<br>2. 服务端查 `mcp_call_records`<br>3. 返回记录数组（不含字幕正文/文件内容） |
| **备选事件流** | 1a. 无记录：返回空列表 + `total=0` |
| **业务规则** | BR-26：**记录范围与脱敏**——每次 MCP `tools/call`（业务 4 tool）落一条：`api_key_id/tool/status/error_code/duration_ms/input_meta/result_bytes/client_ip/created_at`。`input_meta` 为 JSON，字段精确化：<br> `query_media_list` → `{media_type?,media_card_id?,query?,page?,limit?}`（query 标题本身可记，非正文）；<br> `query_media_subtitles` → `{media_id?,media_card_id?,include_internal?}`（`path` 不记，或记为 `<红action>` 化路径前缀）；<br> `fetch_subtitle` → `{is_internal,internal_index?,byte_size}`（**不记 `path`/`video_path`/`content`**）；<br> `upload_subtitle` → `{language,format?,byte_size,overwrite_existing}`（**不记 `path`/`content`/`base64`**）。<br> `result_bytes`=返回 JSON 序列化字节数（可用于审计大结果，但**不构成内容泄露**）。鉴权失败不入库（BR-25）。记录写入策略：采用**有界批量缓冲 + 退出前 flush + 单条失败退化为同步直写**（BR-29），保证审计不因崩溃丢批<br>BR-27：**保留期**——默认保留 180 天，超期由后台定时清理（BR-34）；记录只读，不可由 Agent 经 MCP 查询（仅管理员 REST） |
| **数据说明表** | 入参：`api_key_id?`、`tool?`(`query_media_list` 等)、`status?`(`ok`/`error`/`timeout`)、`from`/`to`(DATETIME)、`page`/`limit`(缺省 20/1~200)<br>出参：记录数组 + `total/page/limit` |

**数据实体 `mcp_call_records`**

| 字段名 | 字段中文名 | 数据类型 | 取值范围 | 是否必填 | 备注 |
|--------|------------|----------|----------|----------|------|
| id | 主键 | BIGINT | ≥1 | 是 | |
| api_key_id | 关联 Key | BIGINT | FK | 是 | 索引 |
| tool | 工具名 | VARCHAR(50) | 4 个 tool | 是 | 索引 |
| status | 状态 | STRING | `ok`/`error`/`timeout` | 是 | |
| error_code | 错误码 | STRING | `KEY_DISABLED`/`NOT_FOUND`/`FORBIDDEN`… | 否 | |
| duration_ms | 耗时 | INT | ≥0 | 是 | |
| input_meta | 入参元信息 | JSON | 不含正文 | 是 | |
| result_bytes | 结果大小 | BIGINT | ≥0 | 否 | |
| client_ip | 来源 IP | STRING | IP | 否 | |
| created_at | 调用时间 | DATETIME | — | 是 | 索引 |

| **接口说明** | 管理 REST `GET /api/v1/mcp/api-keys/:id/records`（单 Key）与 `GET /api/v1/mcp/call-records`（跨 Key 汇总），JWT 保护（人通道）；不向 MCP（机器）开放 |
|------------|----|

---

## 4. 详细交互设计

### 4.1 MCP Server 架构、鉴权与工具契约

```
┌──────────────────────────────────────────────────────────────────────┐
│  Bujic Movie 主进程（Gin :8080）                                       │
│    SetupRouter                                                        │
│      ├─ 既有 HTTP /api/v1/*（不变）                                     │
│      ├─ 新增管理 REST（JWT 保护 = 人通道，与 MCP 机器通道隔离）           │
│      │    POST   /api/v1/mcp/api-keys           创建（返回一次性明文）   │
│      │    GET    /api/v1/mcp/api-keys           列表（脱敏）            │
│      │    GET    /api/v1/mcp/api-keys/:id       详情                    │
│      │    PUT    /api/v1/mcp/api-keys/:id/enable|disable  启停          │
│      │    GET    /api/v1/mcp/api-keys/:id/records    单 Key 调用记录     │
│      │    GET    /api/v1/mcp/call-records         跨 Key 汇总记录        │
│      └─ 新增 GET /api/v1/mcp（MCP Streamable HTTP；仅 API Key 鉴权）     │
│                                                                        │
│  Subtitle Agent 服务层（进程内单例，MCP 与 REST 复用）                    │
│    QueryMediaList / QueryMediaSubtitles / FetchSubtitle / UploadSubtitle│
│                                                                        │
│  MCP 网关层：API Key 鉴权(哈希+active) + 调用记录脱敏落库 + 并发上限8    │
└──────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────┐
│  独立进程（可选）：app/cmd/mcp/main.go  → 主进程 REST（API Key）        │
└──────────────────────────────────────────────────────────────────────┘

Agent 运行时 ← MCP 配置注册（携带 API Key header）→ 主进程 /api/v1/mcp
```

**两种运行形态（BR-17）**：
- **BR-17**：MCP Server 支持两种运行形态且工具契约完全一致（同一份工具定义生成 schema）：形态 A=内嵌 Streamable HTTP（`GET /api/v1/mcp`，推荐默认）；形态 B=独立进程（`app/cmd/mcp/main.go`，内部远端调用主服务 REST）。
- 形态 A 细节：主进程 `GET /api/v1/mcp`。**推荐默认**，复用同一份文件系统/DB 视图。
- 形态 B 细节：`app/cmd/mcp/main.go` 作为 MCP server 暴露，内部以远端模式调用主服务 REST。适用：Agent 运行时要求独立端口/主服务不直接暴露给 Agent 网络。

**传输与鉴权（BR-18/BR-18a/BR-19）**：
- 传输：MCP **Streamable HTTP**（JSON-RPC `initialize`/`tools/list`/`tools/call`）。
- **BR-18**：传输规范 —— MCP 端点遵循 Streamable HTTP；工具协议含 `initialize`/`tools/list`/`tools/call`；输入输出为纯 JSON。
- **BR-18a**：**双通道隔离原则** —— 存在且仅存在两条鉴权通道：机器通道（MCP=API Key）与人通道（管理 REST/Web UI=JWT）。两条通道互相独立，凭据不互通、不互用。
- **BR-19**：**通道归属强制** —— MCP 端点 `/api/v1/mcp` 只接受有效 API Key（`status=active` 且哈希匹配），**不回落、不豁免 JWT**；管理 REST 与 Web UI 只接受 JWT，**不接受 API Key**。不存在"带 JWT 的 MCP 可绕过 API Key"的第三条路。
- **双通道隔离（审核修订 BR-18a/BR-19）**：
  - **机器通道**：MCP Client 请求头携带 `Authorization: Bearer <api-key>`（或 `X-API-Key`）。网关逐请求校验：存在 + `status=active` + 哈希匹配（BR-23/BR-25）。校验失败返回 `-32001` 且**不进入工具执行**（BR-19）。
  - **人通道**：管理 REST（Key 管理/记录查询）与 Web UI **仅 JWT**（人），与机器通道互不通用（BR-19）。
  - "人在浏览器里调试工具"：由管理员在前端**为该用途创建一次性 Key** 注入 MCP 配置（§5.4），不依赖 JWT 直通 MCP 端点。
- **不存在全局静态 token**（废除 v0.1 的 `mcp_access_token` 设想）：每个 Agent/用途一个独立可启停的 Key。

**鉴权与记录时序（UC-06~08 域）**

```
管理员(Web)               API Key 管理 REST               MCP 网关               mcp_call_records
    │ 1. POST /mcp/api-keys {name}                     │                       │
    │ ────────────────────────►  生成+哈希存储           │                       │
    │  ◄── {id,key(明文一次),key_prefix,status} ────────┤                       │
    │                                                    │                       │
    │ 2. PUT /mcp/api-keys/5/disable                    │                       │
    │ ────────────────────────►  更新 status=disabled    │                       │
    │  ◄── {status:disabled} ────────────────────────────┤                       │
    │                                                    │                       │
Agent(持 Key)               MCP 网关                                            │
    │ 3. tools/call(旧 Key) ──► 校验:disabled → 401 KEY_DISABLED                │
    │  ◄── error ────────────┤                       （鉴权失败不入记录，BR-25）   │
    │ 4. tools/call(新 Key) ──► 校验通过 → 执行 → 落一条 ok/error + input_meta  │
    │  ◄── result ───────────┤──────────────────────────────────────────────►│
管理员
    │ 5. GET /mcp/api-keys/5/records?tool=&status=     │                       │
    │ ────────────────────────►  查 mcp_call_records（脱敏字段）                 │
    │  ◄── {records:[...],total} ─────────────────────────────────────────────│
```

**工具注册清单（tools/list 契约，BR-20）**
- **BR-20**：`tools/list` 返回且仅返回表内 5 个工具（4 业务 + `mcp_ping` 自测）；每个工具输入/输出 schema 与用例数据表一致；语言标记 IETF 风格（`en`/`zh-CN`/`zh-TW`/`ja`…），对齐 `pkg/parser`。

| MCP tool name | 对应用例 | 输入（必填加粗） | 输出 |
|---|---|---|---|
| `query_media_list` | UC-01 | `media_type?`,`media_card_id?`,`query?`,`page?`,`limit?` | `{items,total,page,limit}` |
| `query_media_subtitles` | UC-02 | **`media_id` 或 `path` 二选一**；`media_card_id?`,`include_internal?` | `{video_path,subtitles:[...]}`（季目录→多集） |
| `fetch_subtitle` | UC-03 | **`path` 或（`video_path`+`internal_index`）二选一**；`media_card_id?` | §3.3 出参 |
| `upload_subtitle` | UC-04 | **`video_path`**；`subtitle_content?`/`subtitle_base64?`；`format?`,`language` | `{path,message}` |
| `mcp_ping`（仅作连通性自测，管理员/配置校验用） | — | `{}` | `{ok:true,server_time,version}`（**不落调用记录**，BR-32） |

> 语言标记 IETF 风格（`en`/`zh-CN`/`zh-TW`/`ja`…），对齐 `pkg/parser`。输入输出纯 JSON，保证各运行时可用。
> **工具总数=5（含自测）；业务审计 tool=前 4 个**。`mcp_ping` 不读业务数据、不落记录，用于 §5.5 一键自测与 Agent 侧连通性排查。

### 4.2 时序图（UC-05 全链路数据流转）

```
Agent / Skill                    MCP Server / 服务层                 文件系统 / DB
     │                                  │                                 │
     │ 1. query_media_list(type, query) │                                 │
     │ ────────────────────────────────►│  mediaRepo.List + groupMedias    │
     │                                  │────────────────────────────────►│
     │                                  │  外挂扫盘(同目录 videoBase.*)     │
     │      ◄─── {items, has_subtitle, languages, missing_subtitles} ────│
     │                                  │                                 │
     │ 2. query_media_subtitles(path)   │                                 │
     │ ────────────────────────────────►│  getSubtitlesForVideo（外挂+Probe）│
     │      ◄─── {subtitles:[... external en / internal eng index:2 ...]} │
     │                                  │                                 │
     │ 3. fetch_subtitle(path=en.srt)   │                                 │
     │ ────────────────────────────────►│  storage.Read → UTF-8 normalize │
     │      ◄─── {content,format:"srt",encoding:"utf-8",byte_size} ──────│
     │                                  │                                 │
     │ 4. [翻译子任务] 遵循 subtitle-translator-zh（§8.2）：                │
     │    逐行·锚定时间轴·±5 上下文·术语一致·分批/断点/重试→简体中文 UTF-8   │
     │                                  │                                 │
     │ 5. upload_subtitle(video_path, content=<zh-CN>, language=zh-CN)   │
     │ ────────────────────────────────►│ 白名单 → 写 <videoBase>.zh-CN.srt│
     │                                  │  → NotifyRefreshForCard 异步    │
     │      ◄─── {path:"...zh-CN.srt", message:"字幕上传成功"} ────────────│
     │                                  │                                 │
     │ 6. (复核) query_media_subtitles  │      ◄── 确认 zh-CN 已可见 ─────│
     │                                  │                                 │
     （步骤 1~6 每次 tools/call 均经 API Key 鉴权 + 脱敏调用记录落库）
```

### 4.3 并发与任务约束

| 维度 | 约束 | 对应规则 |
|------|------|----------|
| 读取类（UC-01~03） | 无状态、可重复调用 | — |
| 写入类（UC-04） | 幂等（同名覆盖）；v0 无并发版本控制；同 Key 并发上传同名 → 最后写者生效 | BR-14/BR-36 |
| 内嵌探测并发 | 单文件 ffprobe ≤2s；列表层禁止全库强 ffprobe；**单次 tool 调用内 ffprobe ≤16** | BR-03/BR-31 |
| MCP 层并发上限 | 并发 tool 调用 = 8（超出排队）；ffprobe/ffmpeg 子进程数 ≤ 并发数（同一信号量） | BR-21/BR-31 |
| 在途调用 vs 禁用 | 禁用(UC-07)只影响"尚未开始鉴权"的调用；在途调用以鉴权通过时刻为界，不强制中断（不产生"已禁用仍新起任务"） | BR-35 |
| 调用记录写入 | 有界批量缓冲 + 退出前 flush + 失败退化为同步直写；落库失败不影响工具结果 | BR-29 |
| 内嵌缓存并发 | 缓存 map 加 `sync.Mutex`/分片锁，TTL 清理并发安全 | BR-30 |
| 计费/负载透明 | MCP 响应透出 `probe_ms`/`extract_ms`/`bytes` 元信息 | BR-22 |

- **BR-21**：**MCP 层并发上限** —— 并发 MCP tool 调用上限=8（超出排队等待）；ffprobe/ffmpeg 子进程数与并发调用共享同一信号量（容量 8），整机子进程数 ≤8，防宿主资源耗尽。
- **BR-22**：**计费/负载透明** —— 读类工具响应可透出 `probe_ms`/`extract_ms`/`bytes` 元信息（放入结果旁路字段），供 Agent 判断重负载调用；不改变业务结果 schema 主体。

### 4.4 线程模型与技术选型缺口（审核回填）

**线程模型（实施与并发审核用）**

```
MCP HTTP 请求(n)
  → API Key 鉴权层(读 mcp_api_keys 哈希, 只读, 并发安全; 判断 status=active)
  → 信号量 Semaphore(容量=8) 排队（同时约束 ffprobe/ffmpeg 子进程, BR-21/BR-31）
  → Subtitle Agent 服务层执行
       │ 读共享数据：内嵌字幕探测缓存 map（BR-03）
       │              缓存 TTL 清理 timer（sync.Mutex/分片锁, BR-30）
       └→ 调用记录有界批量写入器（buffer → 退出前 flush → 失败退化同步直写, BR-29）
管理 REST / Web UI（独立线程，人通道）
  → 读写 mcp_api_keys（含 status 翻转, BR-35）
  → 读 mcp_call_records
定时任务线程（独立）
  → 调用记录保留期清理（180d, 分批删除事务, BR-34）
```

**技术选型缺口（PRD 必须明确的）**
| # | 缺口 | 建议选型 |
|---|------|----------|
| 1 | MCP Streamable HTTP 的 Go 实现 | `mark3labs/mcp-go`（主流，支持 Streamable HTTP/stdio）做协议层；内嵌端点由它挂在 Gin handler 上（`/api/v1/mcp`），避免自研 JSON-RPC/SSE |
| 2 | 编码检测 | `github.com/saintfish/chardet`（Go 纯实现，检测 UTF-8/GBK/Big5/UTF-16）；与 `pkg/sat`（OpenCC 简繁转换）职责分离 |
| 3 | 定时清理（BR-34） | 自建 `time.Ticker` 常驻 goroutine（低频，如每小时）即可；无需引入 cron 库 |
| 4 | 调用记录批量写入器 | 有界 channel + 单消费者 goroutine + 优雅退出（`server.Shutdown` 前 flush）；单条失败即退化为同步直写 |
| 5 | 并发信号量 | `golang.org/x/sync/semaphore`（weighted）或 buffered channel |
| 6 | API Key 哈希 | 随机盐 + `sha256`/`scrypt`（每次鉴权一次哈希，量小可承受；`sha256` 足够，避免额外依赖） |

---

## 5. UI 设计规范

### 5.1 边界说明

> 本产品是 **Agent 工具面 + 管理员管理面**。UI 面 = 工具协议契约 + Skill 自然语言 + Web 设置页的 API Key 管理区块。

### 5.2 用户可感知面

| 面 | 形式 | 说明 |
|----|------|------|
| Agent 编排 | MCP tools + Skill 提示词 | 面向 LLM 的结构化输入/输出 |
| 系统管理员 | Web UI 设置页「MCP / API Key」 | 创建 / 启停 Key / 查看调用记录 |

### 5.3 Skill 输出/输入示范（面向 Agent 的"UI"）

```
工具 1：query_media_list
输入：{ "media_type": "movie", "query": "Inception", "limit": 10 }
输出：{ "items": [
    { "media_id": 3, "title": "Inception (2010)", "type": "movie", "year": 2010,
      "path": "/media/Inception (2010)/Inception (2010) [1080p].mkv",
      "has_subtitle": true, "languages": ["en"],
      "missing_subtitles": ["zh-CN"] }
  ], "total": 1, "page": 1, "limit": 10 }

工具 2：query_media_subtitles
输入：{ "media_id": 3 }
输出：{ "video_path": "/media/...mkv",
  "subtitles": [
    { "type": "external", "name": "Inception (2010) [1080p].en.srt",
      "language": "en", "format": "srt",
      "path": "/media/.../Inception (2010) [1080p].en.srt" },
    { "type": "internal", "name": "subrip", "language": "eng",
      "format": "srt", "index": 2 } ] }

工具 3：fetch_subtitle
输入：{ "path": "/media/.../Inception (2010) [1080p].en.srt" }
输出：{ "content": "1\n00:00:01,000 --> 00:00:04,000\n...",
       "format": "srt", "encoding": "utf-8", "byte_size": 123456 }

工具 4：upload_subtitle
输入：{ "video_path": "/media/Inception (2010)/Inception (2010) [1080p].mkv",
       "content": "1\n00:00:01,000 --> ...", "language": "zh-CN", "format": "srt" }
输出：{ "path": "/media/Inception (2010)/Inception (2010) [1080p].zh-CN.srt",
       "message": "字幕上传成功" }
```

### 5.4 Web UI：设置页「MCP / API Key」区块（新增）

- **API Key 列表**：表头 `名称 / 前缀(bmk_xxxx…) / 状态(启用·禁用 徽标) / 最后使用 / 创建时间 / 操作`。
- **创建**：输入名称 → 点击创建 → 弹窗**一次性**展示明文密钥 + "复制"按钮 + 明确提示"关闭后不再显示，请妥善保存"，并提供"创建后立即复制到 MCP 配置示例"提示。
- **启用/禁用**：每行操作列提供「禁用」/「启用」按钮；禁用需二次确认（提示影响面：使用该 Key 的 Agent 将立即无法调用）。
- **调用记录**：区块内嵌记录面板，支持筛选 `API Key / 工具 / 状态 / 时间范围` 与分页；每行显示 `时间 / 工具 / 状态 / 耗时 / IP / 结果大小`。
- 人（Web）调试用既有登录态；页面顶部给出 MCP 配置接入示例（含 `Authorization: Bearer <api-key>` header）。

### 5.5 一键自测（审核回填，易用性）

- 每行 Key 提供「测试连接」按钮：内部以该 Key 调一次 `mcp_ping`（BR-32）——**不落调用记录、不读业务数据**，返回 `{ok,server_time,version}`。
- 反馈分级：成功 → 绿勾"连接正常，耗时 xx ms"；失败 → 明示原因（Key 被禁用 / 哈希不匹配 / MCP 端点未启用）。

### 5.6 MCP 配置片段一键复制（审核回填，消除配置劝退点）

- 「创建成功」弹窗与 Key 列表操作列均提供 **「复制 MCP 配置」**：一键产出主流 Agent 的配置片段（`claude_desktop_config.json` / OpenCode `.mcp.json` 风格），含 endpoint、header、type，管理员粘贴即用：

```jsonc
// claude_desktop_config.json 片段
{
  "mcpServers": {
    "bujic-movie": {
      "type": "http",
      "url": "${SERVER_URL}/api/v1/mcp",   // SERVER_URL = 服务器完整地址（scheme://host:port，整体为变量），如 http://192.168.1.10:8080
      "headers": { "Authorization": "Bearer bmk_..." },
      "toolNames": ["query_media_list","query_media_subtitles","fetch_subtitle","upload_subtitle"]
    }
  }
}
```

- 「复制 Skill」按钮：给出本仓库 `.agents/skills/bujic-subtitle/` 路径与 opencode/Claude Code 引用说明（Skill 落盘与 `.gitignore` 处理见 §6.6 交付物）。

---

## 6. 非功能需求

### 6.1 性能

| 场景 | 指标 | 约束说明 |
|------|------|----------|
| 列表查询（UC-01，不含内嵌探测） | 默认媒体库 ≤1000 条分组媒体，外挂扫盘 ≤2s | 只目录 `List`+前缀匹配，不读文件内容 |
| 明细内嵌探测（UC-02） | 每文件 ffprobe ≤2s | 对齐既有 |
| 内嵌抽取（UC-03） | ffmpeg ≤60s；图像字幕大文件可超时 | 返回明确错误 |
| 上传写盘（UC-04） | ≤1s；大 ass <5s | 同步写盘后返回 |
| API Key 创建/启停（UC-06/07） | ≤200ms | 哈希计算为常数级 |
| 调用记录查询（UC-08） | ≤500ms（100 万行内，命中索引） | `api_key_id/tool/created_at` 索引 |
| 调用记录写入 | 单条 ≤50ms，异步批量 | 不阻塞工具返回 |
| MCP tools/call 平均时延 | 读 ≤3s；写 ≤2s（不含 ffmpeg 抽取） | 回环/内网 |
| 并发 | MCP 层并发上限 8；ffprobe/ffmpeg 进程数 ≤ 并发数 | BR-21 |

### 6.2 兼容性

| 面 | 要求 |
|----|------|
| 字幕格式 | 外挂 `srt/ass/ssa/sub/vtt`；内嵌 `subrip/ass/ssa/webvtt/pgs/dvd_subtitle`；图像字幕 `sup/sub` copy 语义 |
| 编码 | 读取兼容 UTF-8/UTF-16/GBK/Big5（chardet+转码，BR-06）；输出一律 UTF-8 |
| 平台 | Win/Linux/macOS；ffmpeg/ffprobe 在 PATH（既有前提） |
| Agent 运行时 | MCP Streamable HTTP；纯 JSON 工具契约（BR-20）；5 tool（4 业务 + `mcp_ping` 自测） |

### 6.3 安全与隐私

| 控制点 | 要求 |
|--------|------|
| 机器鉴权 | MCP 端点**仅**接受 `status=active` 且哈希匹配的 API Key（BR-18a/BR-19/BR-23/BR-25）；不回落 JWT |
| 人鉴权 | 管理 REST / Web UI 沿用 JWT；**API Key 不适用于管理 REST**（双通道隔离，BR-19） |
| 密钥存储 | 只存 `盐:哈希`；不回显明文；密钥生成用 CSPRNG |
| 路径防穿越 | 全部读写经 MediaCard 前缀白名单 + `filepath.Clean`（BR-09） |
| 上传内容 | 二进制嗅探（BR-12）；扩展名白名单（BR-11） |
| 调用记录隐私 | 记录不含字幕正文/文件内容/`path`（BR-26 逐 tool 精确化）；日志不记录字幕明文 |
| 无代码执行 | 字幕内容仅按数据对待；无新增外向请求（除本地 ffmpeg） |
| 审计合规 | 禁用即拒（`KEY_DISABLED`，BR-35 界定在途）；鉴权失败不入记录（防记录投毒），可选用独立安全日志记录暴力尝试（可选） |

### 6.4 可靠性

- ffprobe/ffmpeg 失败：结构化错误 `{error_code,message}`，不 panic。
- 上传半写失败：删除残留目标文件，不产生悬空半文件。
- 媒体文件被外部移动/删除：媒体仍列出但 `warn`（UC-01 4a）。
- 调用记录写入：有界缓冲 + 优雅退出前 flush + 单条失败退化同步直写（BR-29），崩溃不丢审计批。
- 禁用 Key 的在途调用不强制中断（BR-35）。
- 内嵌探测缓存并发安全（BR-30）；`go test -race` 通过。
- MCP 会话异常断开：已完成写操作幂等保留；读操作无副作用。

### 6.5 技术验证清单（上线前 Spike/验证项）

| # | 验证项 | 验收口径 | 验证方式 |
|---|--------|----------|----------|
| V-1 | 服务层列表聚合正确性 | 电影单文件/剧集季聚合得到正确 `has_subtitle`/`languages` | Go 单测 `TestAgentSubtitleFlow` |
| V-2 | 外挂读取 + UTF-8/GBK 转码 | 非 UTF-8 外挂正确转 UTF-8 返回 | 单测 + GBK fixture |
| V-3 | 内嵌 ass 抽取路径 | 内嵌 ass 抽取返回正确文本与 format | 单测（需 ffmpeg，缺则 skip） |
| V-4 | 上传命名与扫描识别闭环 | 上传 `zh-CN` 后 parser/刷新识别出 `languages=[zh-CN]` | 单测命名断言 + 刷新后 UC-02 断言 |
| V-5 | 路径安全回归 | 穿越/卡外路径全拒 | 单测 + 对照 `f27a2c2` 回归集 |
| V-6 | MCP tools/list + call 契约 | 5 tool（4 业务 + `mcp_ping`）schema 与 §4.1 一致；JSON-RPC 往返 | `app/internal/mcp` 单测 + MCP Inspector |
| V-7 | 并发与超时 | 8 并发 ffprobe 不崩；抽取超时不悬挂 | 压测（可选） |
| V-8 | API Key 生命周期 | 创建一次性明文；库中仅哈希；禁用→401 `KEY_DISABLED`；再启用→恢复 | Go 单测 `TestMCPAPIKeyLifecycle`（创建/禁用/启用/过期模拟） |
| V-9 | 调用记录 | 每次 tools/call 落库（含 input_meta 脱敏断言：不含 content/path）；查询过滤正确 | Go 单测 `TestMCPCallRecords` |
| V-10 | 翻译规范对齐 | Skill 产物抽查：行数=原、时间轴未动、无合并、`<terminology>` 存在（如有术语表） | 用 `subtitle-translator-zh` 参照 fixture 人工抽查 + 断言脚本 |
| V-11 | 人/机通道隔离 | ①无 Key 的 MCP 请求被拒；②带有效 Key 但不带 JWT 的业务 tool 正常；③管理 REST 用 Key 被拒（仅 JWT） | Go 集成测试 `TestMCPAuthChannels` |
| V-12 | 三态字幕状态 | 冷缓存小库 → `full`；冷缓存 >200 文件 → `partial` + 无 `missing_subtitles`；热缓存 → `full` | `TestAgentSubtitleFlow` 扩展断言 `subtitle_status` |
| V-13 | 记录批量 flush | 进程退出前 flush；单条写失败退化同步直写不丢审计 | 单测模拟 buffer 满 + 崩溃前 flush |
| V-14 | `mcp_ping` 自测 | 用有效/无效 Key 调 `mcp_ping`：有效→ok 且不落记录；无效→拒绝 | 单测 |
| V-15 | 字幕状态对 Skill 正确 | 同一媒体已存在 `zh-CN` 时 `missing_subtitles` 不含 `zh-CN`（`full` 态） | 单测 |

### 6.6 交付物与仓库约束（审核回填）

- **Skill 落盘冲突**：仓库 `.gitignore` 含 `.agents/`（既有约定，`AGENTS.md` 亦如此）；本 PRD 声称"Skill 仓库内提交"与 git 忽略冲突。处置二选一（实施时锁定其一）：
  1. **保留 `.agents/` 忽略**，Skill 作为文档化交付物提交到 `doc/` 引用 + 由部署脚本/README 安装到用户 `~/.agents/skills/bujic-subtitle`；
  2. 若确需进仓库，新增 `.gitignore` 例外 `!.agents/skills/bujic-subtitle/`（副作用：其他 `.agents/` 内容仍忽略）。
- 交付物清单：MCP 内嵌端点 + 管理 REST + `app/cmd/mcp/main.go` + Skill（`.agents/skills/bujic-subtitle/SKILL.md`）+ 迁移（`mcp_api_keys`、`mcp_call_records` AutoMigrate）。

---

## 7. 术语表 / 数据对象定义 / 参考资料

### 7.1 术语表

| 术语 | 含义 |
|------|------|
| MediaCard（媒体卡） | `media_cards` 表：`ArchivePath` 归档 / `DownloadPath` 下载、类型、默认/监视 |
| media item（媒体记录） | `medias` 表行；电影一行 / 剧集每集一行 |
| 外挂字幕 | 视频同目录、`<videoBase>.<lang>.<ext>` 字幕文件 |
| 内嵌字幕 | mux 于容器的字幕轨道，需 ffprobe/ffmpeg |
| 图像字幕 | pgs/dvd_subtitle 位图轨道，无法直接"翻译" |
| API Key | MCP 机器鉴权凭证；`bmk_` 前缀；库中仅存哈希；可启停 |
| 调用记录 | `mcp_call_records`：每次 MCP 工具调用的脱敏审计日志 |
| Streamable HTTP | MCP 传输规范（MCP Specification 2025-06-18 起的推荐传输） |

### 7.2 参考资料

- `AGENTS.md`：架构、测试名、构建与约定（提交信息用中文）
- `doc/项目架构设计.md`：原设计文档（已漂移，以代码为准）
- `app/internal/router/router.go`：DI 装配与路由（装配点）
- `app/internal/controller/media_controller.go`：`List/ListSubtitles/GetEpisodes/UploadSubtitle/ConvertSubtitle/DownloadSubtitle`
- `app/pkg/parser/subtitle_parser.go`、`app/pkg/fileutil/walk.go`、`app/pkg/mediainfo/mediainfo.go`、`app/pkg/sat/sat.go`
- `subtitle-translator-zh` Skill（`@user_00c9b356/chenlin-subtitle-translator-zh`）：翻译规范方法论来源（§8.2）
- MCP Specification（Streamable HTTP、tools/list、tools/call）

### 7.3 关键数据对象形态（对齐现有字段）

**字幕记录（SubtitleInfo，复用现有结构）**

| 字段 | 类型 | 说明 |
|------|------|------|
| type | enum | `external` / `internal` |
| name | string | 外挂=文件名；内嵌=codec 名 |
| language | string | `en`/`zh-CN`/`zh-TW`/`eng`… |
| title | string | 内嵌轨道标题（若有） |
| format | string | `srt/ass/ssa/vtt/sub/sup` |
| path | string | 外挂绝对路径；内嵌为空 |
| index | int | 内嵌轨道序号；外挂为 0 |

> 字幕状态聚合字段：`subtitle_status`（`full`/`partial`/`none`）见 §3.1.2 与 BR-03；`missing_subtitles` 仅在 `full` 态输出。

**API Key / 调用记录**：见 UC-06/UC-08 数据说明表（`mcp_api_keys`、`mcp_call_records`）。

---

## 8. 易用性设计原则 + 字幕翻译规范 + 一致性检查表

> 本产品里的"小白用户" = **不够细心的 Agent**；翻译环节的用户 = **字幕观众**。两条线都在本节给出规范。

### 8.1 工具面易用性规范

| # | 原则 | 落地 |
|---|------|------|
| E-01 | 一次调用一件事 | `query_media_subtitles` 返回全量字幕（含内嵌） |
| E-02 | 输入有明确缺省 | `media_card_id` 可省；`include_internal` 缺省 true |
| E-03 | 输出自解释 | `missing_subtitles:["zh-CN"]` 直接告诉 Agent 该做什么 |
| E-04 | 错峰提示 | 超长/图像字幕/语言不符都显式 `warn` |
| E-05 | 大文件分段规则 | 字幕 >2000 条按段翻译、编号连续性校验后拼装上传（BR-16） |
| E-06 | 危险操作默认不做 | 默认跳过已有 `zh-CN`（除非用户要求覆盖）BR-14 |
| E-07 | Key 泄漏即止损 | 管理员一键禁用；明文只展示一次，复制按钮 + 安全提示（§5.4） |

### 8.2 字幕翻译规范（翻译子任务必须遵循；对齐 `subtitle-translator-zh`）

> **方法论来源与许可**：本节是 `subtitle-translator-zh` Skill（其内核提炼自 machinewrapped/llm-subtrans 与 gnehs/subtitle-translator-electron，均为 MIT 许可，保留原作者署名）的落地要求。实际交付的 `.agents/skills/bujic-subtitle/SKILL.md` 中的翻译子任务须内嵌本规范（可整体唤起 `subtitle-translator-zh` 执行）。服务端不校验语义，是否符合本规范由交付自检（§8.2.6）保证。

#### 8.2.1 身份与硬规则（S 规则）

| 编号 | 规则 |
|------|------|
| S-01 | **身份锁定**：翻译子任务是"专业字幕翻译师"，只做字幕翻译；不写代码、不编排额外流程、不解释翻译之外的内容。输出 = 同格式字幕文件内容 |
| S-02 | **逐行翻译，绝不合并/拆分**：每条字幕独立成一条译文，行数必须与原文严格一致（合并句子是字幕翻译头号错误，破坏时间轴与阅读节奏） |
| S-03 | **序号与时间轴一字不改**：SRT `1` / `00:00:01,000 --> 00:00:04,000`；ASS `Dialogue:` 行时间码；VTT `00:00:01.000 --> …`——只替换文本部分 |
| S-04 | **只译文本，不动结构**：ASS 只译最后一个 `Text` 字段，`Style:`/`[V4+ Styles]`/其它字段原样保留；VTT 保留 `WEBVTT` 头 |
| S-05 | **上下文连贯但只译当前批**：以 ±5 行为滑动上下文窗口，但只输出当前批次的译文，绝不输出上下文行 |
| S-06 | **术语表一致性**：有术语表严格套用；完成后附 `<terminology>` 块（`原文::译文`，一行一对）供核对 |
| S-07 | **润色为自然口语**：字幕是"说出来的话"，流畅顺口、无翻译腔，但不得改变原意 |
| S-08 | **纠错不臆造 + 粗口对等**：明显 OCR/拼写错误结合上下文修正，不自行发挥添加信息；粗口用对等地道粗口 |
| S-09 | **听障元素**：`[背景音乐]`/`(轻声)` 等默认保留并翻译其描述，除非用户要求清理 |
| S-10 | **输出锁定**：默认简体中文；除双语模式（原文在上、译文在下）外，只输出译文，不加说明 |
| S-11 | **人名/专名**：无术语表时按目标语习惯音译/转写（如英文名音译中文），全片一致 |

#### 8.2.2 分批、上下文与稳定性

| 编号 | 规则 |
|------|------|
| S-12 | **分批**：按时间顺序分批，建议每批 20–30 条；每批带入前 5 条 + 后 5 条作上下文，只译当前批 |
| S-13 | **超大文件切片上限**：单次处理 ≤500 条；超出强制分块（20–30 条/批 + ±5 上下文），不一次性吞整片 |
| S-14 | **断点续译**：分批时记录"已译批次区间"（如 `已译 1–120 条`）；中断可从断点续译，不重跑全片 |
| S-15 | **单条隔离 + 重试上限**：单条异常不影响整批，异常条标注待核；单条/单批最多重试 3 次，3 次仍失败则标注 `⚠ 待核：第 N 条` 并继续，绝不无限重试 |
| S-16 | **超时上限**：单条 60s、单批 300s 级；超时即按 S-15 隔离处理 |
| S-17 | **一致性回滚**：发现专名前后不一，按"首现译法全局统一"修正 |
| S-18 | **多文件**：默认单次处理一个字幕文件；多文件时逐个独立走完整流程（不并行、不混上下文） |

#### 8.2.3 格式速查（对齐 subtitle-format-cheatsheet）

| 格式 | 语法要点 | 双语样例 |
|------|----------|----------|
| SRT | 序号 + `HH:MM:SS,mmm --> HH:MM:SS,mmm` + 文本 | `1` / 时间轴 / 原文 / 译文 |
| ASS/SSA | `[Script Info]`/`[V4+ Styles]`/`Dialogue: ... Text`（只译 Text） | 保留字段，双语在 Text 内以 `\N` 分行 |
| WebVTT | `WEBVTT` 头；`HH:MM:SS.mmm --> …`（点号） | 头保留，双语文本用空行/换行 |

#### 8.2.4 术语表与一致性清单

- 用户提供术语表（JSON / CSV / 内联 `原→译`）时：严格套用，冲突以术语表为准并标注"已按术语表套用"。
- 交付附 `<terminology>` 清单，例：

```
<terminology>
Sherlock Holmes::夏洛克·福尔摩斯
Dr. Watson::华生医生
</terminology>
```

#### 8.2.5 错误处理（沿用 subtitle-translator-zh ERR 码）

| 错误码 | 触发 | 处理 |
|---|---|---|
| ERR-01 行数不匹配 | 译文条数 ≠ 原文条数 | 停下逐条比对，补齐/拆开后再输出 |
| ERR-02 时间轴破坏 | 时间码缺失/错乱 | 回退"只替换文本、锚定原时间轴"保守模式 |
| ERR-03 非字幕输入 | 输入非 SRT/ASS/VTT | 提示确认格式或给示例 |
| ERR-04 术语表冲突 | 术语表与你判断冲突 | 以术语表为准并标注 |
| ERR-05 编码异常 | 非 UTF-8 或乱码 | 提示确认编码（字幕多用 UTF-8），不强译 |
| ERR-06 上下文缺失 | 只有孤立行 | 标注"上下文不足，按字面译"，不脑补 |

#### 8.2.6 交付前自检清单（技能内嵌，未过视为未完成）

- [ ] 译文条数 = 原文条数（无合并/拆分）
- [ ] 每条序号、时间轴/时间码原样保留
- [ ] ASS 只动 `Dialogue:` Text；VTT 保留头；SRT 结构完整
- [ ] 术语表/专名全程一致；有表则附 `<terminology>`
- [ ] 听障元素按偏好处理（保留并翻译 / 清理）
- [ ] 口语自然、无翻译腔、原意未变
- [ ] 输出为同格式（SRT/ASS/VTT）、UTF-8
- [ ] 若分批：批次区间已记录、断点可续、拼装完整后再上传（BR-16）

### 8.3 一致性检查表（以本 PRD v0.3 为准声明）

| 冲突点 | 既有表述（v0.1/v0.2） | 本文档 v0.3 声明 | 处置 |
|--------|----------|------------|------|
| MCP 鉴权 | v0.1：`mcp_access_token` 全局静态 token | **废除**；改为可创建/启停/审计的 API Key（UC-06~08）；MCP 端点**仅 API Key**，不回落 JWT（BR-19） | 以 §4.1/§6.3 为准 |
| 人/机通道 | v0.2：§4.1"JWT 可豁免 API Key"表述含糊 | 明确**双通道隔离**：MCP=仅 API Key（机器）；管理 REST/Web=仅 JWT（人）；互不通用（BR-18a/BR-19）；人调试用一次性 Key（§5.4/§5.6） | 以 §4.1 为准 |
| 字幕状态口径 | v0.2 BR-01/BR-03：`has_subtitle` 布尔 + "冷缓存兜底"冲突 | 引入三态 `subtitle_status: full/partial/none`；`partial` 不参与 `has_subtitle=true` 判定、不产出 `missing_subtitles` | 以 BR-03 为准 |
| 工具契约 | v0.2：4 tool | 新增工具 5 `mcp_ping`（自测，不落记录 BR-32）；业务审计 tool 仍为前 4 | 以 §4.1 为准 |
| 记录脱敏粒度 | v0.2 BR-26：仅"不含正文" | 逐 tool 精确化 `input_meta` 字段（去 path/content/base64），`result_bytes`=JSON 字节数 | 以 BR-26 为准 |
| 记录写入 | v0.2：异步批量，无崩溃语义 | 有界缓冲 + 退出前 flush + 失败退化同步直写（BR-29） | 以 BR-29 为准 |
| 上传去重 | v0.2：默认覆盖，去重由 Skill | 明确服务端不强制，附 `overwrite_existing` 标记（BR-33） | 以 BR-33 为准 |
| 媒体范围 | `/api/v1/media` 现行为 | MCP 列表默认仅取 MediaCard `ArchivePath`（BR-02） | 以本 PRD 为准 |
| 剧集聚合 | `GET /media` 季目录聚合卡片 | MCP 沿用聚合 + 季级字幕口径（BR-01/BR-03） | 以本 PRD 为准 |
| 语言命名 | 既有 parser 顺序 | 上传 tool 显式 `language`，命名由服务端计算（BR-10） | 命名交给服务端 |
| 翻译规范 | v0.1/v0.2：S 规则 | 全量对齐 `subtitle-translator-zh`（§8.2，BR-28）不变 | §8.2 覆盖 |
| 交付物落盘 | v0.2：Skill"仓库内提交" | 与 `.gitignore` `.agents/` 冲突 → 二选一处置（§6.6） | 以 §6.6 为准 |

---

## 附：Skill 文件（交付物）概要结构

> 交付 `.agents/skills/bujic-subtitle/SKILL.md`。本 PRD 定义其结构与本 PRD 对接点。

```
.agents/skills/bujic-subtitle/SKILL.md
├── name / description（触发词：给某影视补中文字幕 / 下载字幕翻译上传）
├── 前置：MCP 服务地址、API Key 获取与配置（Authorization: Bearer）、媒体库状态检查
├── MCP 工具清单（4 业务 tool + mcp_ping 自测：语义一句话、输入必填/可选、输出要点）
├── 全链路流程（UC-05 步骤 1~6，含"已存在 zh-CN 则跳过" BR-14；服务端不强制去重 BR-33）
├── 翻译子任务（整体遵循 subtitle-translator-zh，§8.2）：
│     ├── 硬规则（S-01~S-11）/ 分批与稳定性（S-12~S-18）
│     ├── 格式守则 srt/ass/vtt / 术语表与 <terminology>
│     ├── 错误处理（ERR-01~06）/ 交付自检清单
│     └── 可配置：双语模式、术语表输入、听障元素偏好
├── 字幕状态读取提示（三态 subtitle_status：只在 full 时依据 missing_subtitles 决策）
├── 边界处理（图像字幕跳过文本翻译、超长字幕分段 BR-16、语言标签 warn）
├── 复核自查清单（完成后 query_media_subtitles 复核 zh-CN 可见）
└── 示例对话（英文→中文补全一轮完整案例）
```

---

## 变更记录

| 版本 | 日期 | 变更 | 作者 |
|------|------|------|------|
| v0.1 | 2026-09-05 | 初稿：仅 MCP Server / 仅 Agent 自行翻译 / 项目级 Skill / 外挂+内嵌全口径；UC-01~05 | — |
| v0.2 | 2026-09-05 | 新增 R-06~09：①MCP API Key 生命周期管理（UC-06~08，创建/启停/调用记录，新增 2 实体与 REST）；②MCP 鉴权由静态 token 改为 API Key（§4.1/§6.3）；③翻译规范全量对齐 subtitle-translator-zh（§8.2 扩展为 S-01~S-18 + ERR + 自检，BR-28）；BR 扩至 01~28 | — |
| v0.3 | 2026-09-05 | prd-reviewer 四维审核回填：①修复 BR-19 鉴权双通道逻辑洞（MCP=仅 API Key，管理=仅 JWT）；②BR-03 引入 `subtitle_status` 三态口径，消除"未探测被误报为无字幕"；③新增 BR-29~36（记录写入 flush、缓存加锁、单次 ffprobe≤16、mcp_ping 自测、上传去重边界、180d 清理分批事务、在途禁用边界、同 Key 并发语义）；④BR-26 `input_meta` 逐 tool 脱敏精确化；⑤新增工具 5 `mcp_ping` 与 §5.5/§5.6 一键自测/配置复制；⑥新增 §4.4 线程模型 + 技术选型（mcp-go/chardet/ticker/semaphore）；⑦新增 §6.6 交付物与 `.gitignore` 处置；⑧更新 §6.5 V-11~15、§8.3 一致性表 | — |
