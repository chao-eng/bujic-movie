package repository

import (
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"gorm.io/gorm"
)

type MediaCardRepository interface {
	Create(card *entity.MediaCard) error
	Update(card *entity.MediaCard) error
	Delete(id uint) error
	GetByID(id uint) (*entity.MediaCard, error)
	List() ([]entity.MediaCard, error)
	GetDefault() (*entity.MediaCard, error)
	SetDefault(id uint) error
}

type mediaCardRepository struct {
	db *gorm.DB
}

func NewMediaCardRepository(db *gorm.DB) MediaCardRepository {
	_ = db.AutoMigrate(&entity.MediaCard{})
	return &mediaCardRepository{db: db}
}

func (r *mediaCardRepository) Create(card *entity.MediaCard) error {
	if card.IsDefault {
		_ = r.db.Model(&entity.MediaCard{}).Where("1 = 1").Update("is_default", false)
	}
	return r.db.Create(card).Error
}

func (r *mediaCardRepository) Update(card *entity.MediaCard) error {
	if card.IsDefault {
		_ = r.db.Model(&entity.MediaCard{}).Where("id != ?", card.ID).Update("is_default", false)
	}
	return r.db.Save(card).Error
}

func (r *mediaCardRepository) Delete(id uint) error {
	return r.db.Delete(&entity.MediaCard{}, id).Error
}

func (r *mediaCardRepository) GetByID(id uint) (*entity.MediaCard, error) {
	var card entity.MediaCard
	err := r.db.First(&card, id).Error
	if err != nil {
		return nil, err
	}
	return &card, nil
}

func (r *mediaCardRepository) List() ([]entity.MediaCard, error) {
	var cards []entity.MediaCard
	err := r.db.Order("created_at asc").Find(&cards).Error
	return cards, err
}

func (r *mediaCardRepository) GetDefault() (*entity.MediaCard, error) {
	var card entity.MediaCard
	err := r.db.Where("is_default = ?", true).First(&card).Error
	if err != nil {
		return nil, err
	}
	return &card, nil
}

func (r *mediaCardRepository) SetDefault(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&entity.MediaCard{}).Where("1 = 1").Update("is_default", false).Error; err != nil {
			return err
		}
		return tx.Model(&entity.MediaCard{}).Where("id = ?", id).Update("is_default", true).Error
	})
}
