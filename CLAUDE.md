# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`Bujic Movie` — a self-hosted media manager that auto-scrapes movie/TV metadata (via TMDB) and
organizes downloaded files into a media library (rename + copy/move/hardlink/symlink). Go + Gin
backend with an embedded Vue 3 SPA, shipped as a single binary.

## Repository layout

The Go module lives in **`app/`** (module `github.com/bujic-movie/bujic-movie`), not the repo root.
Run all backend commands from `app/`. The Vue project is in `app/web/`.

- `app/cmd/server/main.go` — entrypoint: load config → init DB → load DB settings → wire router → serve.
- `app/internal/` — private app code (config, db, router, controller, service, repository, model/entity, storage, middleware).
- `app/pkg/` — reusable packages: `tmdb` (API client), `nfo` (NFO XML generator), `parser` (filename + subtitle parsing), `fileutil` (walk/hash/permission), `logger`, `response`.
- `app/embed.go` — `//go:embed dist` embeds the built frontend.
- `doc/project_architecture.md` — the **original design doc**; it has drifted from the code (no `dto/`/`enum/` dirs; extra controllers like auth/dashboard/health/media_card; a watcher service). Trust the code over this doc.

## Commands (run from `app/`)

```bash
# Dev: backend on :8080
make dev-backend          # = go run ./cmd/server/main.go

# Dev: frontend on :5173 (proxies /api → :8080) — run in app/web first time: npm install
make dev-frontend         # = cd web && npm run dev

# Build single binary (frontend → app/dist, then go build → app/bujic-movie)
make build

# Tests
go test ./...
go test ./internal/service/ -run TestTransferService   # single test

# Docker (multi-arch image; CI also does this on push to master)
make docker
```

Test names: `TestAPIRoutes`, `TestEncryptedLoginAndPasswordUpdate`, `TestScrapeService`,
`TestTransferService`, `TestTransferExtraFiles`, `TestLocalStorage`, `TestNFOXMLGeneration`,
`TestParseFilename`, `TestParseSubtitle`, `TestTMDBClient`.

**Build gotcha:** `embed.go` requires `app/dist/` to exist or `go build`/`go run`/`go test` fails to
compile. `app/dist/` is committed, but if you wipe it, rebuild the frontend (`cd web && npm run build`)
before any Go command. The frontend `build` script runs `vue-tsc -b && vite build`, so it also type-checks.

CI (`.github/workflows/build-push.yml`) builds a multi-arch (amd64/arm64) image from the `./app`
context using `app/deployments/Dockerfile` and pushes to Aliyun ACR on every push to `master`.

## Architecture

**Manual DI in one place.** `internal/router/router.go` `SetupRouter` instantiates storage, the TMDB
client, all repositories, services, and controllers, then registers routes. This is the source of
truth for how everything is wired and what depends on what — read it first. Layering is
Controller → Service → Repository (GORM/SQLite), with a `storage` abstraction and the `pkg/*` clients
as infrastructure.

**Config precedence (three layers, last wins):** defaults in `internal/config/config.go` →
`BUJIC_`-prefixed env vars (e.g. `BUJIC_SERVER_PORT`) → values in the SQLite `system_settings` table,
applied in-memory at boot by `db.LoadSettingsFromDB`. There is **no `config.yaml`**. Most business
config (TMDB key, media/download paths, transfer mode, credentials) is edited in the Web UI **Settings**
page, persisted to the DB, and therefore overrides env/defaults.

**Migrations are decentralized.** Each repository constructor (`NewMediaRepository`,
`NewTransferHistoryRepository`, `NewMediaCardRepository`) calls `AutoMigrate` for its own entity;
`SystemSetting` migrates inside `LoadSettingsFromDB`. Entities live in `internal/model/entity/`:
`media`, `media_card`, `transfer_history`, `system_setting`.

**Auth is encrypted login + JWT.** Client calls `GET /api/v1/auth/login-key` to get a one-time session
key, AES-GCM-encrypts the password client-side (`@noble/ciphers`), and posts it to `/auth/login`;
`auth_controller.go` decrypts and issues a JWT. All routes except `health`, `auth/*`, and `ws` sit
behind `middleware.AuthRequired()`.

**Real-time progress over WebSocket.** `GET /api/v1/ws` (`ws_controller.go`) is public and pushes
task/transfer progress to the browser. Backend services broadcast through it; the README notes prior
WS concurrency/blocking fixes, so be careful with concurrent writes to a connection.

**Transfer engine** (`service/transfer_service.go`): a goroutine worker pool processes a queue;
`naming_service.go` computes Emby/Plex/Jellyfin-style destination paths; modes are
`copy`/`move`/`link`/`symlink`; `overwrite_mode` (default `size`) decides conflicts; small files are
filtered by `min_file_size_mb`; Blu-ray directories (containing `BDMV`) get special path handling to
avoid breaking the disc structure. The `storage` interface (`internal/storage/`, local impl in
`storage/local/`) abstracts all file ops so other backends could be added later.

**Watcher** (`service/watcher_service.go`): an fsnotify watcher started in `SetupRouter` auto-triggers
transfers on new files in watched download directories; it's driven by "media cards" (watched-directory
configurations managed via `/api/v1/cards`).

**Scrape flow:** `recognize_service.go` parses the filename (`pkg/parser`) and identifies the title via
TMDB → `scrape_service.go` generates the `.nfo` (`pkg/nfo`) and downloads poster/backdrop images.

## Frontend (`app/web/`)

Vue 3 + Vite + TypeScript + shadcn-vue + Tailwind CSS v4 + Pinia + vue-router. `@` aliases to
`app/web/src`. `vite build` outputs to `../dist` (i.e. `app/dist`), which is what `embed.go` ships.
A `frontend-ui-ux` design skill at `.agents/skills/frontend-ui-ux/SKILL.md` documents the intended
visual aesthetic for new UI.

## Conventions

Commit messages in this repo are written in Chinese; match that convention.
