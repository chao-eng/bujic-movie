package db

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/pkg/logger"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var DB *gorm.DB

// InitDB initializes GORM SQLite connection and creates parent directories if needed
func InitDB(dbPath string) (*gorm.DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	database, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	DB = database
	logger.Info("Database successfully initialized at: %s", dbPath)
	return DB, nil
}

func LoadSettingsFromDB(db *gorm.DB, cfg *config.Config) error {
	// Auto migrate settings table
	if err := db.AutoMigrate(&entity.SystemSetting{}); err != nil {
		return err
	}

	// Fetch all settings
	var settings []entity.SystemSetting
	if err := db.Find(&settings).Error; err != nil {
		return err
	}

	// Map settings to Config struct
	for _, setting := range settings {
		switch setting.Key {
		case "tmdb.api_key":
			cfg.TMDB.APIKey = setting.Value
		case "tmdb.base_url":
			cfg.TMDB.BaseURL = setting.Value
		case "tmdb.language":
			cfg.TMDB.Language = setting.Value
		case "transfer.mode":
			cfg.Transfer.Mode = setting.Value
		case "transfer.overwrite_mode":
			cfg.Transfer.OverwriteMode = setting.Value
		case "transfer.auto_scrape":
			cfg.Transfer.AutoScrape = (setting.Value == "true")
		case "transfer.min_file_size_mb":
			var val int64
			fmt.Sscanf(setting.Value, "%d", &val)
			cfg.Transfer.MinFileSizeMB = val
		case "media.movie_path":
			cfg.Media.MoviePath = setting.Value
		case "media.tv_path":
			cfg.Media.TVPath = setting.Value
		case "media.download_path":
			cfg.Media.DownloadPath = setting.Value
		}
	}
	return nil
}

func SaveSetting(key string, value string) error {
	setting := entity.SystemSetting{Key: key, Value: value}
	return DB.Save(&setting).Error
}
