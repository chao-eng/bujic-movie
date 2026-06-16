package main

import (
	"fmt"
	"log"

	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/db"
	"github.com/bujic-movie/bujic-movie/internal/router"
	"github.com/bujic-movie/bujic-movie/pkg/logger"
	"github.com/gin-gonic/gin"
)

func main() {
	// 1. Load configuration
	cfg, err := config.LoadConfig("")
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	logger.Info("Configuration loaded successfully from defaults and environment")

	// 2. Set Gin mode
	gin.SetMode(cfg.Server.Mode)

	// 3. Initialize Database
	database, err := db.InitDB(cfg.Database.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// 3b. Load all other application settings from SQLite DB
	if err := db.LoadSettingsFromDB(database, cfg); err != nil {
		log.Fatalf("Failed to load settings from database: %v", err)
	}

	// 4. Setup Router (injecting database and config dependencies)
	r := router.SetupRouter(database, cfg)

	// 5. Run Server
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	logger.Info("Server is starting on %s...", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
