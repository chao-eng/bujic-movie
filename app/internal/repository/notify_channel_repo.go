package repository

import (
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"gorm.io/gorm"
)

type NotifyChannelRepository interface {
	Create(ch *entity.NotifyChannel) error
	Update(ch *entity.NotifyChannel) error
	Delete(id uint) error
	GetByID(id uint) (*entity.NotifyChannel, error)
	List() ([]entity.NotifyChannel, error)
}

type notifyChannelRepository struct {
	db *gorm.DB
}

func NewNotifyChannelRepository(db *gorm.DB) NotifyChannelRepository {
	_ = db.AutoMigrate(&entity.NotifyChannel{})
	return &notifyChannelRepository{db: db}
}

func (r *notifyChannelRepository) Create(ch *entity.NotifyChannel) error {
	return r.db.Create(ch).Error
}

func (r *notifyChannelRepository) Update(ch *entity.NotifyChannel) error {
	return r.db.Save(ch).Error
}

func (r *notifyChannelRepository) Delete(id uint) error {
	return r.db.Delete(&entity.NotifyChannel{}, id).Error
}

func (r *notifyChannelRepository) GetByID(id uint) (*entity.NotifyChannel, error) {
	var ch entity.NotifyChannel
	if err := r.db.First(&ch, id).Error; err != nil {
		return nil, err
	}
	return &ch, nil
}

func (r *notifyChannelRepository) List() ([]entity.NotifyChannel, error) {
	var chs []entity.NotifyChannel
	err := r.db.Order("created_at asc").Find(&chs).Error
	return chs, err
}
