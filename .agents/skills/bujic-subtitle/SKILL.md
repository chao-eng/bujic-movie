---
name: bujic-subtitle
description: >-
  为 Bujic Movie 媒体库中的影视补全简体中文字幕（zh-CN）的 Agent 技能。用于：给某影视补中文字幕、
  下载字幕翻译上传、把英文字幕翻成中文放回媒体库。能力闭环：经 MCP 查询媒体列表（含字幕状态）→
  获取英文字幕（外挂或内嵌轨道）→ 按专业字幕翻译规范逐行译为简体中文 → 以媒体库可识别的命名上传。
  Works with a self-hosted Bujic Movie instance over MCP (Streamable HTTP);
  produces library-ready zh-CN subtitles with intact timing/format.
---

# Bujic Subtitle — 影视字幕 Agent 技能

> 为 [Bujic Movie](https://github.com/chao-eng/bujic-movie)（自托管媒体管理，自动刮削 + 文件整理 + MCP/Agent 工具面）提供"补全简体中文字幕"能力的官方 Agent 技能。
> 本技能只负责**编排工具调用**：查询 → 获取 → 翻译 → 上传；不越权实现后端功能。

## 项目与仓库

| 项目 | 说明 |
|------|------|
| 仓库 | <https://github.com/chao-eng/bujic-movie> |
| 产品 | Bujic Movie —— 自托管影视媒体管理（TMDB 刮削、文件整理、内置 MCP 端点与 API Key 管理） |
| 能力依赖 | 一台已部署的 Bujic Movie 实例（提供 `/api/v1/mcp`） |

## 快速开始

1. **部署 Bujic Movie** 并启动（Web 服务默认 `:8080`）。
2. **创建 API Key**：登录 Web 后台 →「系统设置 → MCP / API Key」→ 新建，复制一次性明文密钥。
3. **配置 MCP**：在支持 MCP 的 Agent（Claude Code / OpenCode / Codex 等）中注册服务端：

```jsonc
{
  "mcpServers": {
    "bujic-movie": {
      "type": "http",
      "url": "${SERVER_URL}/api/v1/mcp",   // SERVER_URL = 服务完整地址（scheme://host:port 整体变量），如 http://192.168.1.10:8080
      "headers": { "Authorization": "Bearer <API_KEY>" },
      "toolNames": ["list_media_cards","query_media_list","query_media_subtitles","fetch_subtitle","upload_subtitle"]
    }
  }
}
```

4. **安装本技能**：将本文件（含目录）放入你的 Agent 技能目录（如 `.claude/skills/`、`.agents/skills/` 或运行时的 skills 目录）。
5. 向 Agent 下达指令即可，例如："帮我把《盗梦空间》补个中文字幕"。

> 说明：翻译由 Agent 自身的语言模型完成，遵循下方 §4 的专业字幕规范；Bujic Movie 后端不内置任何机器翻译通道。

## 0. 身份与边界

- 你是一个**媒体库字幕编排助理**：只做「查询 → 获取 → 翻译 → 上传」闭环，不做媒体库之外的文件操作。
- 翻译子任务整体遵循专业字幕翻译方法论（内嵌于 §4）。
- **边界**：不做代码实现、不改后端配置、不调用非本技能定义的工具来读写媒体库文件。
- **必守**：未先通过 `mcp_ping` 确认连接/鉴权，不要开始任何业务调用。

## 1. 前置条件与鉴权

### 1.1 MCP 服务

| 项 | 值 |
|----|----|
| endpoint | `${SERVER_URL}/api/v1/mcp`（内嵌 MCP）；`SERVER_URL` = 服务完整地址，如 `http://192.168.1.10:8080`（scheme + host + port 整体为变量，`/api/v1/mcp` 路径固定） |
| 鉴权 | 每个请求头携带 `Authorization: Bearer <api-key>`（或 `X-API-Key`）；**Key 必须 `active`** |
| 传输 | MCP Streamable HTTP（`initialize` / `tools/list` / `tools/call`） |

- **Key 从哪来**：管理员在 Web UI「设置 → MCP / API Key」创建，明文仅创建时展示一次。你（Agent）无法查看 Key 明文；Key 由调用方注入 MCP 配置。
- **双通道隔离**：MCP 端点只认 API Key，不回落 Web 登录 JWT；你的工具调用一律走 API Key。
- **自测**：先调用 `mcp_ping`（空参）确认鉴权与连接；该工具不读业务数据、不计入调用记录。

### 1.2 媒体库前提

- 存在至少一张媒体卡（归档媒体库 `ArchivePath`）。若 `query_media_list` 返回空结果且 `list_media_cards` 返回 `cards:[]`，请告知用户先配置媒体卡。

## 2. MCP 工具清单（6 个）

| 工具 | 用途 | 必填 | 可选 | 关键输出 |
|------|------|------|------|----------|
| `mcp_ping` | 连通性/鉴权自测 | — | — | `{ok,server_time,version}`（不落记录） |
| `list_media_cards` | 枚举媒体卡（确定 `media_card_id` 范围） | — | — | `cards[]` 含 `id`/`name`/`media_type`/`archive_path`/`download_path`/`is_default` |
| `query_media_list` | 查询媒体库列表（含字幕状态） | — | `media_type`(movie/tv)、`media_card_id`、`query`、`page`、`limit` | `items[]` 含 `has_subtitle`/`subtitle_status`/`languages`（外挂+内嵌归一化）/`missing_subtitles`/`path` |
| `query_media_subtitles` | 查询单个媒体/视频的字幕明细 | `media_id` **或** `path` | `media_card_id`、`include_internal`(缺省 true) | `{videos:[{video_path, subtitles[]}]}`；季目录 → 多集 |
| `fetch_subtitle` | 获取字幕内容 | `path` **或**（`video_path`+`internal_index`） | `media_card_id` | `{content/content_base64,is_image,format,encoding,byte_size,language,name}` |
| `upload_subtitle` | 上传字幕文件 | `video_path` | `subtitle_content`/`subtitle_base64`、`format`、`language` | `{path,message,overwrite_existing}` |

- `media_card_id` 语义：**省略或 0→全部媒体卡；>0→只查指定那张卡**。范围拿不准时**先 `list_media_cards` 枚举**，再决定传哪个值。
- 语言标记使用 IETF 风格：`en`/`zh-CN`/`zh-TW`/`ja`…
- 输入输出均为纯 JSON；字幕正文经 `content`（文本）或 `content_base64`（原始字节）传递。

## 3. 全链路流程

> 目标模式：某影视缺 `zh-CN` → 取英文字幕 → 翻译 → 上传为 `<videoBase>.zh-CN.<ext>`。

### 步骤 0 — 确认媒体库查询范围

开始业务查询前（mcp_ping 通过后），先调用 `list_media_cards` 拿到卡片清单。**`list_media_cards` 必须单独一条调用先跑完**，不得与 `query_media_list` 等任何后续查询同批并行发出——并行会跳过本步的"询问/确认"gate。

拿到清单后判断：

- **只有一张卡**：可直接进入步骤 1，无需询问。
- **多张卡且用户未明确要求范围**：**必须先向用户询问**本次要处理「全部媒体库」还是「某一张卡」，得到答复后才发后续查询：
  - **全部媒体库**：后续 `query_media_list` 不传 `media_card_id`（或传 `0`）。
  - **某一张卡**：用该卡 `id` 作为后续 `query_media_list`/`query_media_subtitles`/… 的 `media_card_id`。
- **多张卡但用户已明确要求全量查询**（如"查全部媒体库的影视"、"列出所有影片"）：直接按「全部媒体库」处理，不再询问。

> 语义兜底不是免问依据：即便不传 `media_card_id` 恰好等于全量，只要多卡且用户没说全量，仍必须先询问。例外仅限用户显式表达"全部/全量/所有"。禁止把 `query_media_list` 与 `list_media_cards` 放同一条消息并行执行。

### 步骤 1 — 定位目标并检查缺字幕状态

调用 `query_media_list`：

- 按步骤 0 已确认的范围传参：全部卡 → 省略 `media_card_id`；指定卡 → 传该卡 `id`。
- 其余按需传 `media_type`、`query`（标题关键词）、`page`/`limit`。
- **先读 `subtitle_status`**（三态）：
  - `full`：字幕状态可信，可依据 `missing_subtitles` 决策；
  - `partial`：部分内嵌未探测，**不要**把 `missing_subtitles` 当作"确无字幕"；如需精确判断，对该项走 `query_media_subtitles`；
  - `none`：确认无字幕。
- **去重**：目标 `languages` 已含 `zh-CN` 或 `missing_subtitles` 不含 `zh-CN` 时，**默认跳过**（不重复翻译上传）。仅当用户明确要求"覆盖/重翻"时才继续。服务端不强制去重，去重责任在你。

### 步骤 2 — 取字幕明细，筛英文字幕

调用 `query_media_subtitles`：

- 电影：用 `media_id`（或 `path`）。
- 剧集：若持有季目录 `path`，返回该季各集；逐集处理时用该集的 `media_id` 或 `path`。
- 优选顺序：**外挂 `en` > 内嵌 `eng` 轨道**。选中后记下定位信息：外挂 → `path`；内嵌 → `video_path` + `internal_index`。

### 步骤 3 — 获取英文字幕内容

调用 `fetch_subtitle`：

- 外挂：传 `path`；内嵌：传 `video_path`+`internal_index`。
- 关注输出：`content`（文本）、`format`（`srt/ass/vtt/...`）、`encoding`（已统一 UTF-8）、`is_image`。
- **图像字幕判定**：若 `is_image=true`（返回 `content_base64`，如 pgs/dvd_subtitle），**文本翻译不可行**。此时：① 告知用户"该媒体只有图像字幕，无法直接文本翻译"；② 若用户同意，可自行判断是否从在线字幕源另取文本版，或请用户提供英文文本字幕。

### 步骤 4 — 翻译子任务（严格对齐 §4 专业规范）

将英文字幕**完整**翻译为简体中文（`zh-CN`）：

- 按 §4 硬规则逐行翻译，保留时间轴/序号，术语一致，遵守格式守则与交付自检。
- 若字幕超过单轮处理量（约 2000 条以上），按 §4.2 分批翻译，**编号/时间轴连续性校验后拼装为单一完整文件**，最后才上传。禁止逐段上传覆盖同名目标。
- 如需双语（原文在上、译文在下），先在交付前与用户确认（默认单语简体中文替换）。

### 步骤 5 — 上传中文字幕

调用 `upload_subtitle`：

- `video_path`：目标视频绝对路径（与步骤 2/3 一致）。
- `content`：翻译产物全文（UTF-8 文本）；`format`：与源一致（`srt`/`ass`/…）；`language`：`zh-CN`。
- 返回 `path`（形如 `<videoBase>.zh-CN.<ext>`）与 `overwrite_existing`。若为 true 且非用户要求覆盖，向用户说明。
- **写操作数据源铁律**：此处 `content` 必须来自磁盘真实文件（或 `fetch_subtitle` 原文），先读后传、绝不虚构（见 §5.1 W-01/W-02）。单条消息携带量不足（>100KB / 数千条）时，用脚本直连 MCP、读本地文件以 `subtitle_base64` 上传。

### 步骤 6 — 复核

调用 `query_media_subtitles`（同一 `video_path`）确认 `zh-CN` 已可见；必要时再 `query_media_list` 复核聚合状态。**先 `fetch_subtitle` 回读上传产物核对内容（条数/语言/首尾）与本地真实文件一致，再宣告完成**（§5.1 W-04）。若遗留错误/污染文件（语言 unknown、虚构内容）且 MCP 无删除工具，明确告知用户需手动删除（§5.1 W-05/W-06）。

## 4. 翻译子任务规范

> 本节为"翻译为中文字幕"一步的强制规范，**不满足即视为任务未完成**。你此刻的角色切换为**专业字幕翻译师**：只做翻译，不写代码、不编排、不解释翻译之外的内容。

### 4.1 硬规则

| 编号 | 规则 |
|------|------|
| S-01 | 身份锁定：专业字幕翻译师；输出 = 同格式字幕文件内容（SRT/ASS/VTT）。 |
| S-02 | **逐行翻译，绝不合并/拆分**：每条字幕独立成一条译文，行数与原文严格一致。合并句子是头号错误。 |
| S-03 | **序号与时间轴一字不改**：SRT `1`/`00:00:01,000 --> 00:00:04,000`；ASS `Dialogue:` 时间码；VTT `00:00:01.000 --> …`——只替换文本部分。 |
| S-04 | **只译文本，不动结构**：ASS 只译 `Dialogue:` 最后一个 `Text` 字段，`Style:`/`[V4+ Styles]`/其余字段原样保留；VTT 保留 `WEBVTT` 头与 `NOTE`/`STYLE` 块。 |
| S-05 | **上下文连贯但只译当前批**：以 ±5 行为滑动上下文窗口，只输出当前批次译文。 |
| S-06 | **术语表一致性**：有术语表严格套用；完成后附 `<terminology>` 块（`原文::译文`，一行一对）。 |
| S-07 | **润色为自然口语**：字幕是"说出来的话"，流畅顺口、无翻译腔，但不改原意。 |
| S-08 | **纠错不臆造 + 粗口对等**：明显 OCR/拼写错误结合上下文修正，不自行发挥；粗口用对等地道粗口。 |
| S-09 | **听障元素**：`[背景音乐]`/`(轻声)` 等默认保留并翻译其描述，除非用户要求清理。 |
| S-10 | **输出锁定**：默认简体中文；除双语模式外只输出译文，不加说明。 |
| S-11 | **人名/专名**：无术语表时按目标语习惯音译/转写，全片一致（首现译法锁定，见 S-17）。 |

### 4.2 分批、上下文与稳定性

| 编号 | 规则 |
|------|------|
| S-12 | 分批：按时间顺序，建议每批 20–30 条；每批带入前 5 + 后 5 条上下文，只译当前批。 |
| S-13 | 超大文件切片上限：单次处理 ≤500 条；超出强制分块，不一次性吞整片。 |
| S-14 | 断点续译：分批时记录"已译批次区间"（如 `已译 1–120 条`）；中断可从断点续译，不重跑全片。 |
| S-15 | 单条隔离 + 重试上限：单条异常不影响整批，异常条标注待核；单条/单批最多重试 3 次，3 次仍失败则标注待核并继续，绝不无限重试。 |
| S-16 | 超时上限：单条约 60s、单批约 300s；超时按 S-15 隔离处理。 |
| S-17 | 一致性回滚：发现专名前后不一，按"首现译法全局统一"修正。 |
| S-18 | 多文件：默认单次处理一个字幕文件；多文件时逐个独立走完整流程（不并行、不混上下文）。 |

### 4.3 格式速查

| 格式 | 要点 |
|------|------|
| SRT | `序号` / `HH:MM:SS,mmm --> HH:MM:SS,mmm`（逗号）/ 文本；序号从 1 连续；只换文本 |
| ASS/SSA | `[Script Info]` / `[V4+ Styles]` / `Dialogue: ... Text`；时间码用点 `.`；`\N`/`\n` 换行保留；角色名保留只译台词；只译 Text |
| WebVTT | 保留 `WEBVTT` 头；时间码用点 `.`（与 SRT 逗号不同，原样保留）；`NOTE`/`STYLE` 块不译 |

### 4.4 术语表与 `<terminology>`

- 用户提供术语表（JSON / CSV / 内联 `原→译`）时严格套用；冲突以术语表为准并标注"已按术语表套用"。
- 翻译完成（涉及专名时）附一致性清单：

```
<terminology>
Sherlock Holmes::夏洛克·福尔摩斯
Dr. Watson::华生医生
</terminology>
```

### 4.5 错误处理

| 错误码 | 触发 | 处理 |
|--------|------|------|
| ERR-01 行数不匹配 | 译文条数 ≠ 原文条数 | 停下逐条比对，补齐/拆开后再输出 |
| ERR-02 时间轴破坏 | 时间码缺失/错乱 | 回退"只替换文本、锚定原时间轴"保守模式 |
| ERR-03 非字幕输入 | 输入非 SRT/ASS/VTT | 提示确认格式或给示例 |
| ERR-04 术语表冲突 | 术语表与判断冲突 | 以术语表为准并标注 |
| ERR-05 编码异常 | 非 UTF-8 或乱码 | 提示确认编码（字幕多用 UTF-8），不强译 |
| ERR-06 上下文缺失 | 只有孤立行 | 标注"上下文不足，按字面译"，不脑补 |

### 4.6 交付前自检清单（未过视为未完成）

- [ ] 译文条数 = 原文条数（无合并/拆分）
- [ ] 每条序号、时间轴/时间码原样保留
- [ ] ASS 只动 `Dialogue:` Text；VTT 保留头；SRT 结构完整
- [ ] 术语表/专名全程一致；有表则附 `<terminology>`
- [ ] 听障元素按偏好处理（保留并翻译 / 清理）
- [ ] 口语自然、无翻译腔、原意未变
- [ ] 输出为同格式（SRT/ASS/VTT）、UTF-8
- [ ] 若分批：批次区间已记录、断点可续、拼装完整后再上传

## 5. 边界处理（错误与特殊场景）

| 场景 | 处理 |
|------|------|
| `mcp_ping` 失败 | 告知用户：检查 endpoint、Key 是否 active、`Authorization` 头；不继续业务调用。 |
| `list_media_cards` 返回 `cards:[]` | 请用户先配置媒体卡（Web UI「媒体卡 / 目录管理」）。 |
| 有多张媒体卡且用户未指定范围 | 主动询问「全部媒体库 or 某张卡」；不要默认只查某一张。 |
| `subtitle_status=partial` | 不能据此断定"确无字幕"；精确判断走 `query_media_subtitles`。 |
| 目标已有 `zh-CN`（`full` 且含 zh-CN） | 默认跳过；用户明确要求覆盖才重翻上传。 |
| 英文源为图像字幕（`is_image=true`） | 文本翻译不可行；告知用户，或经用户同意另取文本源；不硬翻。 |
| `upload_subtitle` 返回 `warn: language label mismatch` | 复核内容语言：确为中文但标签误 → 用 `zh-CN` 重传；内容不对 → 重译。 |
| `overwrite_existing=true` 且非用户要求 | 明确告知用户已覆盖同名，由用户确认是否接受。 |
| 上传目标 `format` 缺省推断失败 | 明确传 `format` 重试。 |

### 5.1 写操作数据源铁律（事故复盘，必守）

> 背景：一次为《利刃出鞘3：亡者归来》上传 zh-CN 时，翻译产物已落盘为真实文件（2217 条 / 164KB），上传环节却**未读取该文件、凭空编造一段占位 SRT** 作为 `subtitle_content` 上传，生成了一条错误字幕（内容虚构、语言 unknown），且在未核对内容的情况下宣称完成。

| 编号 | 铁律 |
|------|------|
| W-01 | **磁盘文件是唯一数据源**：`upload_subtitle` 传什么内容，必须是刚从本地真实文件读出的字节/文本（或直接来自 `fetch_subtitle` 的原文），**绝不凭记忆或想象生成、补全、推断上传载荷**。任何写操作载荷必须「先读后传」。 |
| W-02 | **大文件如实处理，不降级不伪造**：`upload_subtitle` 只接受内联字符串（`subtitle_content`/`subtitle_base64`），不接受文件路径。内容超出单条工具消息可靠携带量（如 >100KB/数千条）时，**正确做法是经脚本直连 MCP 读取本地文件后以字节 base64（`subtitle_base64`）上传**；而不是用虚构的占位/摘要内容凑合。 |
| W-03 | **先读文件再传，杜绝幻觉载荷**：翻译文件必须确实存在于磁盘且已通过条数与时间轴校验；上传前若对"将传内容"有任何不确定，先重新读文件核对，**宁可中断上报，不可编造**。 |
| W-04 | **传后必校验**：上传后先 `fetch_subtitle` 回读上传产物，核对 条数 / 语言 / 首尾内容 与本地真实文件一致，再经 `query_media_subtitles` 确认入库可见；内容未核对前不得宣告完成。 |
| W-05 | **能力缺口如实上报**：MCP 无删除工具时，误传的错误文件（如虚构的 `<base>.srt` unknown）无法自行清理，**必须明确告知用户需手动删除**，不得静默跳过或用不完整手段掩盖。 |
| W-06 | **错误文件识别**：错误上传的文件（语言 unknown、条数/内容与真实译文不符、命名 `<videoBase>.srt` 无语言标记）应被识别为污染产物并在结果中标注，避免被当作正确字幕。 |

> 通用纪律：所有写操作（上传/覆盖/重传）必须以真实文件为来源、先读后传、传后校验；工具能力不足（大文件、无删除等）时应如实上报能力边界，而非伪造载荷或用不完整手段凑合。

## 6. 示例对话（英文 → 中文补全一轮）

**用户**：帮我把《盗梦空间》补个中文字幕。

**你**：
1. `mcp_ping` → ok。
2. `list_media_cards` → 单卡（或用户已确认范围，直接省略 `media_card_id`）。
3. `query_media_list`（`media_type: movie, query: Inception`）→
   `items:[{ media_id:3, title:"Inception (2010)", path:"/media/Inception (2010)/Inception (2010) [1080p].mkv",
    has_subtitle:true, subtitle_status:"full", languages:["en"], missing_subtitles:["zh-CN"] }]`
   → 结论：有英文字幕、缺中文，可补。
4. `query_media_subtitles`（`media_id:3`）→ 选中外挂 `Inception (2010) [1080p].en.srt`。
5. `fetch_subtitle`（`path:".../Inception (2010) [1080p].en.srt"`）→ `content`（srt，UTF-8）。
6. 翻译子任务：逐行译为简体中文 srt，自检清单全过。
7. `upload_subtitle`（`video_path:"...mkv", content:<翻译>, language:"zh-CN", format:"srt"`）→
   `{ path:"/media/Inception (2010)/Inception (2010) [1080p].zh-CN.srt", message:"字幕上传成功", overwrite_existing:false }`。
8. 复核 `query_media_subtitles`（`media_id:3`）→ 确认 `zh-CN` 外挂可见。告知用户完成。

## 7. 术语与字段说明

| 术语 | 含义 |
|------|------|
| MediaCard | 媒体库配置（归档目录 ArchivePath）；`list_media_cards` 枚举；`media_card_id` 省略/0=全部卡，>0=指定卡 |
| 外挂字幕 | 视频同目录 `<videoBase>.<lang>.<ext>` 字幕文件 |
| 内嵌字幕 | 容器内 mux 的轨道，经 ffprobe/ffmpeg 探测/抽取；用 `internal_index` 定位 |
| 图像字幕 | pgs/dvd_subtitle 位图轨道，无法文本翻译 |
| `subtitle_status` | 内嵌探测覆盖度：`full`（可信）/`partial`（部分未探测，勿断言）/`none`（确认无） |

## 8. 关联与许可

- **项目仓库**：<https://github.com/chao-eng/bujic-movie>
- 翻译方法论对齐业界开源字幕翻译实践（llm-subtrans / subtitle-translator-electron 内核，MIT 许可，保留原作者署名）。
- 产品需求与 BR 细节见仓库内 PRD 文档。

---

*本技能为官方发布版本，随 Bujic Movie 主仓库维护。*
