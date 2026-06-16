package entity

import (
	"time"

	"gorm.io/gorm"
)

type MediaCard struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Name         string `gorm:"column:name" json:"name"`
	DownloadPath string `gorm:"column:download_path" json:"download_path"`
	ArchivePath  string `gorm:"column:archive_path" json:"archive_path"`
	MediaType    string `gorm:"column:media_type" json:"media_type"` // "movie" or "tv"
	IsDefault    bool   `gorm:"column:is_default;default:false" json:"is_default"`
}

func (MediaCard) TableName() string {
	return "media_cards"
}
