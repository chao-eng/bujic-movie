package db

import (
	"os"
	"path/filepath"

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
