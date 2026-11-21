# AGENTS.md

> 本仓库面向 **Codex / OpenCode 等编码 Agent** 的统一指南已迁移到中文文档：
>
> ➡️ **请阅读 [`Agent开发指南.md`](./Agent开发指南.md)**（仓库布局、常用命令、架构要点、搜索下载与字幕/MCP 能力、前端约定、测试清单与文档索引）。

简要提醒：

- Go module 在 `app/`（非仓库根）；所有后端命令在 `app/` 下执行。
- 提交信息用中文。
- 改代码前先读 `app/internal/router/router.go`（DI 装配源）与 `Agent开发指南.md`。

---

## 发版经验（v2026.11.21 实操总结）

首次通过 Actions 发版时构建在 Docker 里失败、本地却一切正常。下面这些坑和顺序值得记住。

### 发版流程

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

### 构建相关的三个坑

这三点叠加导致「本地过、CI 挂」，排查时按顺序看：

1. **`package-lock.json` 必须进版本库**。本机全局 gitignore 会忽略它；一旦不进库，Dockerfile 里 `npm install` 装的是浮动版本，类型检查工具版本一变就报一堆类型错误。仓库 `.gitignore` 已显式放行 `app/web/package-lock.json`，Dockerfile 用 `npm ci`。

2. **锁文件要包含所有平台的原生绑定**。在 macOS 上生成的锁文件只有 `@rolldown/binding-darwin-arm64`，Linux 下会缺 `@rolldown/binding-linux-*`、`lightningcss-*`、`@tailwindcss/oxide-*` 等包，`npm ci` 严格按锁文件安装就会报 `Cannot find native binding`。补齐方式：从 registry 元数据取每个平台包的 `integrity`/`tarball`/`os`/`cpu` 写进 `packages`。

3. **锁文件里的 `resolved` 统一用 `registry.npmjs.org`**。混入镜像源（如 npmmirror）在 CI 环境可能 400 或超时；`integrity` 是内容摘要，换源不影响校验。

### 本地复现 CI 的正确方式

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

### 其它注意

- 改过 `manifest.json`（浏览器扩展权限）后，必须在 `chrome://extensions` 手动刷新扩展，否则新权限不生效且错误是静默的。
- `gh release edit --notes-file` 会**整体替换**说明正文。工作流自动附加的镜像地址会被冲掉，编辑后要补回，或直接用脚本重跑一次发布。
- 发布是手工触发的，不会因为 push 自动跑；tag 已存在时脚本会中止，不会覆盖已发布的版本。
