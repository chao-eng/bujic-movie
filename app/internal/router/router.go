package router

import (
	"io/fs"
	"net/http"

	bujicmovie "github.com/bujic-movie/bujic-movie"
	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/controller"
	"github.com/bujic-movie/bujic-movie/internal/middleware"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/internal/service"
	"github.com/bujic-movie/bujic-movie/internal/storage/local"
	"github.com/bujic-movie/bujic-movie/pkg/tmdb"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupRouter initializes GORM repositories, services, controllers, routes and middlewares
func SetupRouter(db *gorm.DB, cfg *config.Config) *gin.Engine {
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
	mediaRepo := repository.NewMediaRepository(db)
	historyRepo := repository.NewTransferHistoryRepository(db)
	mediaCardRepo := repository.NewMediaCardRepository(db)

	// 3. Services Instantiation
	recognizeSvc := service.NewRecognizeService(tmdbClient)
	scrapeSvc := service.NewScrapeService(mediaRepo, recognizeSvc, tmdbClient, stg)
	namingSvc := service.NewNamingService()
	transferSvc := service.NewTransferService(historyRepo, namingSvc, recognizeSvc, scrapeSvc, tmdbClient, stg, cfg, mediaCardRepo)
	mediaCardSvc := service.NewMediaCardService(mediaCardRepo)

	// 4. Controllers Instantiation
	authCtrl := controller.NewAuthController()
	healthCtrl := controller.NewHealthController()
	scrapeCtrl := controller.NewScrapeController(scrapeSvc)
	transferCtrl := controller.NewTransferController(transferSvc)
	mediaCtrl := controller.NewMediaController(mediaRepo)
	settingCtrl := controller.NewSettingController(tmdbClient)
	fileCtrl := controller.NewFileController(stg)
	wsCtrl := controller.NewWSController()
	dashboardCtrl := controller.NewDashboardController(mediaRepo, historyRepo, mediaCardRepo)
	mediaCardCtrl := controller.NewMediaCardController(mediaCardSvc)

	// Public Routes
	api := r.Group("/api/v1")
	{
		api.GET("/health", healthCtrl.Check)
		api.POST("/auth/login", authCtrl.Login)
		api.GET("/ws", wsCtrl.Handle) // WebSocket can be public for easy browser handshakes
	}

	// Protected Routes (Require JWT Auth)
	protected := r.Group("/api/v1")
	protected.Use(middleware.AuthRequired())
	{
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

		// Settings
		protected.GET("/settings", settingCtrl.Get)
		protected.PUT("/settings", settingCtrl.Update)

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
	}

	// Serve Static Frontend Files
	setupStaticFiles(r)

	return r
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
