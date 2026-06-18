package entity

import (
	"time"

	"gorm.io/gorm"
)

// MediaLibrary 表示一台用户配置的媒体服务器（Emby/Jellyfin/Plex），
// 由用户在前端录入并持久化到 SQLite。媒体卡片可绑定一个媒体库，
// 当该卡片对应内容整理并刮削完成后，会对其绑定的媒体库发起增量刷新。
type MediaLibrary struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Name    string `gorm:"column:name" json:"name"`
	Type    string `gorm:"column:type" json:"type"`       // "emby" | "jellyfin" | "plex"
	URL     string `gorm:"column:url" json:"url"`         // 如 http://192.168.1.10:8096
	APIKey  string `gorm:"column:api_key" json:"api_key"` // Emby/Jellyfin API Key 或 Plex Token
	Enabled bool   `gorm:"column:enabled;default:true" json:"enabled"`
	// LibraryID 选定要刷新的服务器内媒体库 ID；为空表示「全部媒体库」
	LibraryID   string `gorm:"column:library_id" json:"library_id"`
	LibraryName string `gorm:"column:library_name" json:"library_name"`
}

func (MediaLibrary) TableName() string {
	return "media_libraries"
}
