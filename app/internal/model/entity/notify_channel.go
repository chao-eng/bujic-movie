package entity

import (
	"time"

	"gorm.io/gorm"
)

// NotifyChannel 是用户配置的一个第三方消息通知渠道（企业微信/钉钉/飞书/Server酱/Bark 等）。
// 各渠道的差异化参数以 JSON 存放于 Config（键值对），由前端按渠道 schema 动态渲染。
type NotifyChannel struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Name    string `gorm:"column:name" json:"name"`
	Type    string `gorm:"column:type" json:"type"`       // wecombot/dingtalk/feishu/serverchan/bark/pushplus/pushdeer/gotify/ntfy/webhook
	Enabled bool   `gorm:"column:enabled;default:true" json:"enabled"`
	Config  string `gorm:"column:config" json:"config"` // JSON 对象字符串：{"key":"value",...}
}

func (NotifyChannel) TableName() string {
	return "notify_channels"
}
