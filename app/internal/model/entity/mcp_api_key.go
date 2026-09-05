package entity

import (
	"time"

	"gorm.io/gorm"
)

// MCPAPIKey represents a machine credential used to authenticate MCP clients
// against the /api/v1/mcp endpoint. Only a salted hash is stored; the plaintext
// is returned exactly once at creation time.
type MCPAPIKey struct {
	ID         uint           `gorm:"primarykey" json:"id"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
	Name       string         `gorm:"column:name" json:"name"`
	KeyHash    string         `gorm:"column:key_hash;uniqueIndex" json:"-"`
	KeyPrefix  string         `gorm:"column:key_prefix" json:"key_prefix"`
	Status     string         `gorm:"column:status;default:active" json:"status"` // active / disabled
	LastUsedAt *time.Time     `gorm:"column:last_used_at" json:"last_used_at"`
}

func (MCPAPIKey) TableName() string {
	return "mcp_api_keys"
}
