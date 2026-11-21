package router

import (
	"io/fs"
	"net/http"
	"strings"

	bujicmovie "github.com/bujic-movie/bujic-movie"
	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/controller"
	internalcrypto "github.com/bujic-movie/bujic-movie/internal/crypto"
	"github.com/bujic-movie/bujic-movie/internal/db"
	"github.com/bujic-movie/bujic-movie/internal/mcp"
	"github.com/bujic-movie/bujic-movie/internal/middleware"
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/internal/service"
	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
	"github.com/bujic-movie/bujic-movie/internal/storage/local"
	"github.com/bujic-movie/bujic-movie/pkg/logger"
	"github.com/bujic-movie/bujic-movie/pkg/tmdb"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupRouter initializes GORM repositories, services, controllers, routes and middlewares
func SetupRouter(gormDB *gorm.DB, cfg *config.Config) *gin.Engine {
	db.DB = gormDB
	config.GlobalConfig = cfg
	r := gin.New()

	// Global middlewares
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(middleware.CORS())

	// 1. Storage & Clients Instantiation
	stg := local.NewLocalStorage()
	tmdbClient := tmdb.NewClient(cfg.TMDB.APIKey, cfg.TMDB.BaseURL, cfg.TMDB.Language)

	// 2. Repositories Instantiation
	mediaRepo := repository.NewMediaRepository(gormDB)
	historyRepo := repository.NewTransferHistoryRepository(gormDB)
	mediaCardRepo := repository.NewMediaCardRepository(gormDB)
	mediaLibraryRepo := repository.NewMediaLibraryRepository(gormDB)
	notifyChannelRepo := repository.NewNotifyChannelRepository(gormDB)
	mcpAPIKeyRepo := repository.NewMCPAPIKeyRepository(gormDB)
	mcpCallRecordRepo := repository.NewMCPCallRecordRepository(gormDB)
	searchRepo := repository.NewSearchRepository(gormDB)

	// 3. Services Instantiation
	recognizeSvc := service.NewRecognizeService(tmdbClient)
	msgNotifySvc := service.NewMessageNotifyService(notifyChannelRepo)
	scrapeSvc := service.NewScrapeService(mediaRepo, recognizeSvc, tmdbClient, stg, msgNotifySvc)
	namingSvc := service.NewNamingService()
	notificationSvc := service.NewNotificationService(mediaLibraryRepo, mediaCardRepo)
	transferSvc := service.NewTransferService(historyRepo, namingSvc, recognizeSvc, scrapeSvc, tmdbClient, stg, cfg, mediaCardRepo, notificationSvc)
	watcherSvc := service.NewWatcherService(transferSvc, mediaCardRepo)
	if err := watcherSvc.Start(); err != nil {
		// Log error but don't fail startup
		logger.Error("Failed to start directory watcher service: %v", err)
	}
	mediaCardSvc := service.NewMediaCardService(mediaCardRepo, watcherSvc)
	mediaLibrarySvc := service.NewMediaLibraryService(mediaLibraryRepo)
	subtitleAgentSvc := service.NewSubtitleAgentService(mediaRepo, mediaCardRepo, stg)
	mcpAPIKeySvc := service.NewMCPAPIKeyService(mcpAPIKeyRepo, mcpCallRecordRepo)
	mcpAPIKeySvc.StartRetentionLoop(0, 0) // default 180d retention, hourly tick

	// Search/download module. It shares the HTTP executor so timeouts, retries
	// and proxies behave the same for indexers and download clients.
	searchHTTP, searchHTTPErr := httpclient.New(nil, nil, cfg.Search.Proxy)
	if searchHTTPErr != nil {
		logger.Warn("search: proxy config ignored: %v", searchHTTPErr)
		searchHTTP, _ = httpclient.New(nil, nil, "")
	}
	// Re-read the proxy from the live config so a settings change applies
	// without a restart.
	searchHTTP.SetGlobalProxyResolver(func() string { return config.GlobalConfig.Search.Proxy })
	searchBox, boxErr := internalcrypto.NewSecretBox(cfg.Server.SecretKey)
	if boxErr != nil {
		logger.Error("search: secret box unavailable, module disabled: %v", boxErr)
	}
	var (
		definitionSvc     *service.DefinitionService
		indexerSvc        *service.IndexerService
		searchDownloadSvc *service.SearchService
		downloadClientSvc *service.DownloadClientService
		grabSvc           *service.GrabService
		taskSvc           *service.TaskService
	)
	searchEnabled := cfg.Search.Enabled && searchBox != nil
	if searchBox != nil {
		definitionSvc = service.NewDefinitionService(searchRepo, cfg, searchHTTP)
		indexerSvc = service.NewIndexerService(searchRepo, searchBox, definitionSvc, cfg, searchHTTP)
		downloadClientSvc = service.NewDownloadClientService(searchRepo, searchBox, cfg, searchHTTP)
		searchDownloadSvc = service.NewSearchService(searchRepo, indexerSvc, cfg)
		grabSvc = service.NewGrabService(searchRepo, indexerSvc, downloadClientSvc, cfg, searchDownloadSvc.Metrics())
		taskSvc = service.NewTaskService(searchRepo, downloadClientSvc)
	}

	// MCP gateway: 5 tools wired to the agent service; API-key auth happens in
	// Gateway.ServeHTTP. Call records persist via the record repo (BR-26).
	mcpGateway := mcp.NewGateway(subtitleAgentSvc, mcpAPIKeySvc, func(r mcp.Record) error {
		return mcpCallRecordRepo.Create(&entity.MCPCallRecord{
			APIKeyID:    r.APIKeyID,
			Tool:        r.Tool,
			Status:      r.Status,
			ErrorCode:   r.ErrorCode,
			DurationMS:  r.DurationMS,
			InputMeta:   r.InputMeta,
			ResultBytes: r.ResultBytes,
			ClientIP:    r.ClientIP,
		})
	})

	// 4. Controllers Instantiation
	authCtrl := controller.NewAuthController()
	healthCtrl := controller.NewHealthController()
	scrapeCtrl := controller.NewScrapeController(scrapeSvc)
	transferCtrl := controller.NewTransferController(transferSvc)
	mediaCtrl := controller.NewMediaController(mediaRepo, mediaCardRepo, stg, notificationSvc)
	settingCtrl := controller.NewSettingController(tmdbClient)
	fileCtrl := controller.NewFileController(stg)
	logStreamCtrl := controller.NewLogStreamController()
	dashboardCtrl := controller.NewDashboardController(mediaRepo, historyRepo, mediaCardRepo)
	mediaCardCtrl := controller.NewMediaCardController(mediaCardSvc)
	mediaLibraryCtrl := controller.NewMediaLibraryController(mediaLibrarySvc)
	notifyChannelCtrl := controller.NewNotifyChannelController(msgNotifySvc)
	mcpAPIKeyCtrl := controller.NewMCPAPIKeyController(mcpAPIKeySvc)
	var searchCtrl *controller.SearchController
	if searchEnabled {
		searchCtrl = controller.NewSearchController(definitionSvc, indexerSvc, downloadClientSvc, searchDownloadSvc, grabSvc, taskSvc, true)
	} else {
		// Keep /status reachable so the UI can explain why the module is off.
		searchCtrl = controller.NewSearchController(nil, nil, nil, nil, nil, nil, false)
	}

	// Public Routes
	api := r.Group("/api/v1")
	{
		api.GET("/health", healthCtrl.Check)
		api.GET("/auth/login-key", authCtrl.GetLoginKey)
		api.POST("/auth/login", authCtrl.Login)
		// MCP Streamable HTTP endpoint — auth is API-Key only (BR-18a/BR-19),
		// enforced inside the Gateway handler, so it is intentionally NOT in the
		// JWT-protected group below.
		api.Any("/mcp", gin.WrapH(mcpGateway))
	}

	// Protected Routes (Require JWT Auth)
	protected := r.Group("/api/v1")
	protected.Use(middleware.AuthRequired())
	{
		// 日志流里会出现服务器绝对路径、影片名、媒体库名等运行细节，
		// 必须和其余管理接口一样要求登录态；前端用 fetch 自带 Authorization 头。
		protected.GET("/logs/stream", logStreamCtrl.Stream)
		protected.GET("/logs/recent", logStreamCtrl.Snapshot)

		// Scrape
		protected.POST("/scrape", scrapeCtrl.Scrape)

		// Transfer
		protected.POST("/transfer", transferCtrl.Transfer)
		protected.GET("/transfer/queue", transferCtrl.GetQueue)
		protected.GET("/transfer/history", transferCtrl.GetHistory)

		// Media
		protected.GET("/media", mediaCtrl.List)
		protected.GET("/media/search", mediaCtrl.Search)
		protected.DELETE("/media/:id", mediaCtrl.Delete)
		protected.GET("/media/episodes", mediaCtrl.GetEpisodes)
		protected.POST("/media/refresh", mediaCtrl.Refresh)
		protected.POST("/subtitles/upload", mediaCtrl.UploadSubtitle)
		protected.GET("/media/image", mediaCtrl.GetImage)
		protected.GET("/media/subtitles", mediaCtrl.ListSubtitles)
		protected.POST("/subtitles/delete", mediaCtrl.DeleteSubtitle)
		protected.POST("/subtitles/convert", mediaCtrl.ConvertSubtitle)
		protected.GET("/subtitles/download", mediaCtrl.DownloadSubtitle)

		// Settings
		protected.GET("/settings", settingCtrl.Get)
		protected.PUT("/settings", settingCtrl.Update)
		protected.PUT("/settings/password", settingCtrl.UpdatePassword)

		// File Browser
		protected.GET("/files", fileCtrl.List)

		// Dashboard
		protected.GET("/dashboard/stats", dashboardCtrl.GetStats)

		// Media Cards
		protected.GET("/cards", mediaCardCtrl.List)
		protected.GET("/cards/default", mediaCardCtrl.GetDefault)
		protected.GET("/cards/:id", mediaCardCtrl.GetByID)
		protected.POST("/cards", mediaCardCtrl.Create)
		protected.PUT("/cards/:id", mediaCardCtrl.Update)
		protected.DELETE("/cards/:id", mediaCardCtrl.Delete)
		protected.PUT("/cards/:id/default", mediaCardCtrl.SetDefault)

		// Media Libraries (media servers: Emby / Jellyfin / Plex)
		protected.GET("/libraries", mediaLibraryCtrl.List)
		protected.GET("/libraries/status", mediaLibraryCtrl.Status)
		protected.GET("/libraries/:id", mediaLibraryCtrl.GetByID)
		protected.POST("/libraries", mediaLibraryCtrl.Create)
		protected.POST("/libraries/probe", mediaLibraryCtrl.Probe)
		protected.PUT("/libraries/:id", mediaLibraryCtrl.Update)
		protected.DELETE("/libraries/:id", mediaLibraryCtrl.Delete)
		protected.POST("/libraries/:id/test", mediaLibraryCtrl.Test)
		protected.POST("/libraries/:id/refresh", mediaLibraryCtrl.Refresh)

		// Notification channels (third-party message push)
		protected.GET("/notify-channels", notifyChannelCtrl.List)
		protected.GET("/notify-channels/types", notifyChannelCtrl.Types)
		protected.GET("/notify-channels/:id", notifyChannelCtrl.GetByID)
		protected.POST("/notify-channels", notifyChannelCtrl.Create)
		protected.PUT("/notify-channels/:id", notifyChannelCtrl.Update)
		protected.DELETE("/notify-channels/:id", notifyChannelCtrl.Delete)
		protected.POST("/notify-channels/:id/test", notifyChannelCtrl.Test)

		// MCP API Key management (human channel, JWT protected, BR-18a/BR-19)
		protected.POST("/mcp/api-keys", mcpAPIKeyCtrl.Create)
		protected.GET("/mcp/api-keys", mcpAPIKeyCtrl.List)
		protected.GET("/mcp/api-keys/:id", mcpAPIKeyCtrl.GetByID)
		protected.PUT("/mcp/api-keys/:id/:action", mcpAPIKeyCtrl.SetStatus)
		protected.DELETE("/mcp/api-keys/:id", mcpAPIKeyCtrl.Delete)
		protected.GET("/mcp/api-keys/:id/records", mcpAPIKeyCtrl.Records)
		protected.GET("/mcp/call-records", mcpAPIKeyCtrl.AllRecords)
		protected.DELETE("/mcp/call-records", mcpAPIKeyCtrl.ClearRecords)

		// Search & download (搜索下载). Every route is registered so a disabled
		// module answers JSON 404 instead of falling through to the SPA handler
		// (BR-40). /status stays available to explain the state.
		search := protected.Group("/search")
		search.GET("/status", searchCtrl.Status)
		guarded := search.Group("", requireSearchEnabled(searchEnabled))
		{
			guarded.GET("/definitions", searchCtrl.ListDefinitions)
			guarded.POST("/definitions/sync", searchCtrl.SyncDefinitions)
			guarded.POST("/definitions/import", searchCtrl.ImportDefinition)
			guarded.GET("/definitions/:id", searchCtrl.GetDefinition)
			guarded.DELETE("/definitions/:id", searchCtrl.DeleteDefinition)

			guarded.GET("/indexers", searchCtrl.ListIndexers)
			guarded.POST("/indexers", searchCtrl.CreateIndexer)
			guarded.GET("/indexers/:id", searchCtrl.GetIndexer)
			guarded.PUT("/indexers/:id", searchCtrl.UpdateIndexer)
			guarded.DELETE("/indexers/:id", searchCtrl.DeleteIndexer)
			guarded.PUT("/indexers/:id/enabled", searchCtrl.SetIndexerEnabled)
			guarded.POST("/indexers/:id/test", searchCtrl.TestIndexer)
			guarded.POST("/indexers/test-all", searchCtrl.TestAllIndexers)
			guarded.POST("/indexers/:id/captcha", searchCtrl.SubmitCaptcha)
			guarded.POST("/indexers/:id/refresh-definition", searchCtrl.RefreshDefinition)
			guarded.POST("/indexers/:id/clear-cooldown", searchCtrl.ClearCooldown)

			guarded.GET("/download-clients", searchCtrl.ListClients)
			guarded.POST("/download-clients", searchCtrl.CreateClient)
			guarded.GET("/download-clients/:id", searchCtrl.GetClient)
			guarded.PUT("/download-clients/:id", searchCtrl.UpdateClient)
			guarded.DELETE("/download-clients/:id", searchCtrl.DeleteClient)
			guarded.PUT("/download-clients/:id/enabled", searchCtrl.SetClientEnabled)
			guarded.POST("/download-clients/:id/test", searchCtrl.TestClient)
			guarded.GET("/download-clients/:id/categories", searchCtrl.ClientCategories)

			guarded.POST("/query", searchCtrl.Query)
			guarded.POST("/grab/preview", searchCtrl.GrabPreview)
			guarded.POST("/grab", searchCtrl.Grab)
			guarded.GET("/tasks", searchCtrl.Tasks)
			guarded.POST("/tasks/:client_id/:hash/action", searchCtrl.TaskAction)
			guarded.GET("/records", searchCtrl.Records)
			guarded.GET("/history", searchCtrl.History)
			guarded.DELETE("/history", searchCtrl.DeleteHistory)
			guarded.GET("/health", searchCtrl.Health)
			guarded.POST("/proxy-test", searchCtrl.TestProxy)
			guarded.POST("/cookie-import", searchCtrl.ImportCookies)
			guarded.POST("/events", searchCtrl.RecordEvent)
			guarded.GET("/metrics", searchCtrl.Metrics)
		}
	}

	// Serve Static Frontend Files
	setupStaticFiles(r)

	return r
}

// requireSearchEnabled rejects module routes when the feature switch is off.
func requireSearchEnabled(enabled bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !enabled {
			c.JSON(http.StatusNotFound, gin.H{"code": 40401, "msg": "搜索下载功能未启用"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func setupStaticFiles(r *gin.Engine) {
	distFS, err := fs.Sub(bujicmovie.StaticFiles, "dist")
	if err != nil {
		return
	}

	// Read index.html once at startup
	indexHTML, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		r.NoRoute(func(c *gin.Context) {
			c.String(http.StatusNotFound, "index.html not found")
		})
		return
	}

	r.NoRoute(func(c *gin.Context) {
		// Unknown API paths must not fall through to the SPA: a typo or a
		// disabled module should answer JSON, not index.html with status 200.
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"code": 40401, "msg": "接口不存在"})
			return
		}

		path := c.Request.URL.Path
		filePath := path
		if len(filePath) > 0 && filePath[0] == '/' {
			filePath = filePath[1:]
		}

		// Try to serve file from dist FS if it exists
		if filePath != "" {
			file, err := distFS.Open(filePath)
			if err == nil {
				file.Close()
				c.FileFromFS(filePath, http.FS(distFS))
				return
			}
		}

		// Otherwise serve index.html for SPA client-side routing
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
	})
}
