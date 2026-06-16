package main

import (
	"fmt"
	"log"

	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/db"
	"github.com/bujic-movie/bujic-movie/internal/router"
	"github.com/gin-gonic/gin"
)

func main() {
	// 1. Load configuration
	cfg, err := config.LoadConfig("")
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// 2. Set Gin mode
	gin.SetMode(cfg.Server.Mode)

	// 3. Initialize Database
	database, err := db.InitDB(cfg.Database.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// 4. Setup Router (injecting database and config dependencies)
	r := router.SetupRouter(database, cfg)

	// 5. Run Server
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("Server is starting on %s...", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
