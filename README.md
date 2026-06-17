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

## 📁 项目目录结构

```text
bujic-movie/
├── .github/workflows/           # GitHub Actions 工作流（包含 ACR 自动打包推送）
├── app/                          # 应用代码目录
│   ├── cmd/server/              # 后端服务入口 (main.go)
│   ├── configs/                 # 配置模版
│   ├── deployments/             # 部署配置 (Dockerfile, docker-compose)
│   ├── internal/                # 后端核心业务实现 (Controller/Service/Repo/Model/Storage)
│   ├── pkg/                     # 独立公共工具包 (nfo, tmdb, parser, fileutil)
│   └── web/                     # 前端 Vue 3 源码项目
├── doc/                          # 系统设计与架构说明文档
└── README.md                     # 项目说明文件
```

---

## ⚙️ 系统配置项 (Environment Variables)

系统支持通过环境变量覆盖默认参数。所有环境变量前缀为 `BUJIC_`，并使用下划线代替配置层级的点。

| 环境变量 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `BUJIC_SERVER_PORT` | `8080` | Web 服务监听端口 |
| `BUJIC_SERVER_MODE` | `debug` | 运行模式 (`debug` / `release`) |
| `BUJIC_SERVER_USERNAME` | `admin` | Web 界面默认登录账号 |
| `BUJIC_SERVER_PASSWORD` | `admin123` | Web 界面默认登录密码 |
| `BUJIC_SERVER_SECRET_KEY` | `change_me_...`| 用于 JWT 鉴权的密钥 |
| `BUJIC_DATABASE_DB_PATH` | `data/bujic-movie.db`| SQLite 数据库存储路径 |
| `BUJIC_TMDB_BASE_URL` | `https://api.themoviedb.org/3` | TMDB API 请求基地址 |
| `BUJIC_TMDB_LANGUAGE` | `zh-CN` | 元数据默认语言 |
| `BUJIC_TRANSFER_MODE` | `link` | 默认整理模式 (`copy`/`move`/`link`/`softlink`) |
| `BUJIC_TRANSFER_MIN_FILE_SIZE_MB`| `50` | 整理时允许的最小文件大小限制 |

*注：TMDB API Key 等进阶配置可在系统启动后在 Web 设置页面中配置，它将持久化在 SQLite 中且具备最高优先级。*

---

## 🏃 快速开始

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

### 本地编译生产版本

可以通过 `app/Makefile` 一键编译嵌入了前端的单二进制程序：

```bash
cd app
# 编译前端，并将产物 embed 到 Go 二进制中输出
make build
```
编译成功后，将在 `app/` 下生成可执行文件 `bujic-movie`。

### Docker-compose 一键部署

在生产环境部署时，建议使用 `docker-compose` 运行：

```bash
cd app/deployments
# 根据实际情况修改 docker-compose.yml 中的卷挂载路径 (media/downloads)
docker-compose up -d
```

---

## 🛠️ CI/CD 与自动打包配置

项目已内置 GitHub Actions 自动构建多架构镜像流水线：

- **配置文件**: [.github/workflows/build-push.yml](file:///.github/workflows/build-push.yml)
- **触发条件**: 当代码被推送到 `master` 分支时触发构建。
- **构建平台**: 同时构建 `linux/amd64` (x86_64) 与 `linux/arm64` (适用于 M 系列 Mac / 树莓派 / 阿里云 ARM 服务器) 架构镜像。
- **发布标签**: 构建完成后自动推送至阿里云 ACR，并被打上三种标签：
  - `YYYYMMDD-HHMMSS` (具体时间戳)
  - `YYYYMMDD` (日期戳)
  - `latest` (最新版本)

> [!IMPORTANT]
> 在启用 GitHub Actions 之前，请确保在您 GitHub 仓库的 **Settings -> Secrets and variables -> Actions** 中添加以下 Secrets 配置：
> - `ALIYUN_REGISTRY`
> - `ALIYUN_NAMESPACE`
> - `ALIYUN_REPOSITORY`
> - `ALIYUN_USERNAME`
> - `ALIYUN_PASSWORD`
