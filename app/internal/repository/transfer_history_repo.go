package repository

import (
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"gorm.io/gorm"
)

type TransferHistoryRepository interface {
	Create(history *entity.TransferHistory) error
	List(offset, limit int) ([]entity.TransferHistory, error)
}

type transferHistoryRepository struct {
	db *gorm.DB
}

func NewTransferHistoryRepository(db *gorm.DB) TransferHistoryRepository {
	_ = db.AutoMigrate(&entity.TransferHistory{})
	return &transferHistoryRepository{db: db}
}

func (r *transferHistoryRepository) Create(history *entity.TransferHistory) error {
	return r.db.Create(history).Error
}

func (r *transferHistoryRepository) List(offset, limit int) ([]entity.TransferHistory, error) {
	var list []entity.TransferHistory
	err := r.db.Offset(offset).Limit(limit).Order("transferred_at desc").Find(&list).Error
	return list, err
}
