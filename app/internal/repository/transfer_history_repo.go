package repository

import (
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"gorm.io/gorm"
)

type TransferHistoryRepository interface {
	Create(history *entity.TransferHistory) error
	List(offset, limit int) ([]entity.TransferHistory, error)
	Count(status string) (int64, error)
	CountAll() (int64, error)
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

func (r *transferHistoryRepository) Count(status string) (int64, error) {
	var count int64
	err := r.db.Model(&entity.TransferHistory{}).Where("status = ?", status).Count(&count).Error
	return count, err
}

func (r *transferHistoryRepository) CountAll() (int64, error) {
	var count int64
	err := r.db.Model(&entity.TransferHistory{}).Count(&count).Error
	return count, err
}
