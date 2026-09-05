# AGENTS.md

> 本仓库面向 **Codex / OpenCode 等编码 Agent** 的统一指南已迁移到中文文档：
>
> ➡️ **请阅读 [`Agent开发指南.md`](./Agent开发指南.md)**（仓库布局、常用命令、架构要点、字幕/MCP 能力、前端、约定、测试清单与文档索引）。

简要提醒：

- Go module 在 `app/`（非仓库根）；所有后端命令在 `app/` 下执行。
- 提交信息用中文。
- 改代码前先读 `app/internal/router/router.go`（DI 装配源）与 `Agent开发指南.md`。
