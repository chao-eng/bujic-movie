package entity

import (
	"time"

	"gorm.io/gorm"
)

type TransferHistory struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	SrcPath        string    `gorm:"column:src_path" json:"src_path"`
	DestPath       string    `gorm:"column:dest_path" json:"dest_path"`
	Status         string    `gorm:"index;column:status" json:"status"` // "success" or "failed"
	Size           int64     `gorm:"column:size" json:"size"`
	Mode           string    `gorm:"column:mode" json:"mode"`           // "copy", "move", "link", "softlink"
	Message        string    `gorm:"column:message" json:"message"`
	TransferredAt  time.Time `gorm:"column:transferred_at" json:"transferred_at"`
}

func (TransferHistory) TableName() string {
	return "transfer_histories"
}
