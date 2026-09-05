package entity

import (
	"time"

	"gorm.io/gorm"
)

// MCPCallRecord is an audit entry for a single MCP tools/call invocation.
// input_meta stores a tool-specific, redacted summary (never subtitle content,
// file content, or raw paths) per BR-26 of the PRD.
type MCPCallRecord struct {
	ID          uint           `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time      `json:"created_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	APIKeyID    uint           `gorm:"column:api_key_id;index" json:"api_key_id"`
	Tool        string         `gorm:"column:tool;index" json:"tool"`
	Status      string         `gorm:"column:status" json:"status"` // ok / error / timeout
	ErrorCode   string         `gorm:"column:error_code" json:"error_code"`
	DurationMS  int64          `gorm:"column:duration_ms" json:"duration_ms"`
	InputMeta   string         `gorm:"column:input_meta;type:text" json:"input_meta"`
	ResultBytes int64          `gorm:"column:result_bytes" json:"result_bytes"`
	ClientIP    string         `gorm:"column:client_ip" json:"client_ip"`
}

func (MCPCallRecord) TableName() string {
	return "mcp_call_records"
}
