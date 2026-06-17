# Bujic Movie

`Bujic Movie` 是一个轻量级、自托管的媒体文件管理工具，旨在实现电影和电视剧的**自动刮削**（元数据抓取）与**文件智能整理**（重命名、归档、转移）。项目提供了一个精美现代的 Web 可视化操作界面，帮助您轻松管理个人媒体库。

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

- **🗃️ 极简单二进制部署**
  - 利用 Go 的 `go:embed` 特性，将编译好的前端静态资源直接嵌入到 Go 二进制中，实现单文件分发。
  - 数据库采用无配置、零运维的 SQLite，随起随用。

- **🎨 Premium 视觉设计**
  - 基于 Vue 3 + Vite + TypeScript + shadcn-vue + Tailwind CSS 打造。
  - 拥有精心调配的深色模式、流畅的动画效果和响应式布局。
  - 包含仪表盘、媒体库浏览、刮削控制台、整理队列、后台任务及全局设置面板。

---

## 🛠️ 技术栈选型

| 层级 | 技术选型 | 作用描述 |
| :--- | :--- | :--- |
| **后端框架** | Go + Gin | 提供高性能的 RESTful API 服务，实现轻量、单二进制部署 |
| **数据库** | SQLite + GORM | 适合自托管的轻量级关系型数据库，通过 ORM 实现自动表迁移 |
| **文件操作** | 自研存储抽象层 (类似 rclone) | 屏蔽底层存储差异，方便后续拓展云盘或网络存储后端 |
| **前端框架** | Vue 3 + Vite + TS | 现代且快速的单页面应用 (SPA) 开发栈 |
| **UI 组件库** | shadcn-vue + Tailwind CSS | 高质量可定制组件与原子化样式，保障极佳的视觉表现力 |
| **CI/CD** | GitHub Actions | 自动化多架构 (AMD64/ARM64) Docker 构建并推送至阿里云 ACR |

---


## 🏃 如何使用 (Usage)

### 开发环境调试

1. **克隆仓库**
   ```bash
   git clone <your-repo-url>
   cd bujic-movie
   ```

2. **启动前端开发服务器**
   ```bash
   cd app/web
   npm install
   npm run dev
   ```
   前端服务将默认运行在 `http://localhost:5173`。

3. **启动后端服务**
   ```bash
   cd app
   # 确保已安装 Go 环境
   go run ./cmd/server/main.go
   ```
   后端服务将默认启动在 `http://localhost:8080`。前端的 Vite 配置文件已默认设置将 `/api` 的请求转发至后端。

---

### 本地编译生产版本

可以通过 `app/Makefile` 一键编译嵌入了前端的单二进制程序：

```bash
cd app
# 编译前端，并将产物 embed 到 Go 二进制中输出
make build
```
编译成功后，将在 `app/` 下生成可执行文件 `bujic-movie`。

---

### Docker-compose 一键部署

在生产环境部署时，建议使用 `docker-compose` 运行：

```bash
cd app/deployments
# 根据实际情况修改 [docker-compose.yml](./app/deployments/docker-compose.yml) 中的卷挂载路径 (media/downloads,/path/to/media)
docker-compose up -d
```
默认会拉取阿里云镜像：`crpi-a1liy20beodq2bdl.cn-beijing.personal.cr.aliyuncs.com/bujic/bujic-movie:latest` 并启动服务。

---

## ⚙️ 环境变量配置 (Environment Variables)

系统支持通过环境变量注入基础的运行环境参数。所有环境变量前缀为 `BUJIC_`，并使用下划线代替配置层级的点。

| 环境变量 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `BUJIC_SERVER_PORT` | `8080` | Web 服务监听端口 |
| `BUJIC_SERVER_MODE` | `debug` | 运行模式 (`debug` / `release`) |
| `BUJIC_SERVER_SECRET_KEY` | `change_me_to_something_secure` | 用于 JWT 鉴权的签名密钥 |
| `BUJIC_DATABASE_DB_PATH` | `data/bujic-movie.db` | SQLite 数据库在容器内或本地的存储路径 |

> 💡 **提示**：其他系统业务配置（例如 TMDB API 密钥、媒体库路径、整理转移模式及账号密码等）均可直接在 **Web 网页后台的「系统设置」** 页面进行可视化配置，它们会被自动保存至数据库并享有最高优先级。
