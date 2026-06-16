package repository

import (
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"gorm.io/gorm"
)

type MediaRepository interface {
	Create(media *entity.Media) error
	Update(media *entity.Media) error
	GetByID(id uint) (*entity.Media, error)
	GetByTMDBID(tmdbID int, mediaType string) (*entity.Media, error)
	GetByPath(path string) (*entity.Media, error)
	List(offset, limit int) ([]entity.Media, error)
	Search(query string) ([]entity.Media, error)
	Delete(id uint) error
	Count(mediaType string) (int64, error)
}

type mediaRepository struct {
	db *gorm.DB
}

func NewMediaRepository(db *gorm.DB) MediaRepository {
	// Auto migrate entity.Media inside constructor or main setup
	_ = db.AutoMigrate(&entity.Media{})
	return &mediaRepository{db: db}
}

func (r *mediaRepository) Create(media *entity.Media) error {
	return r.db.Create(media).Error
}

func (r *mediaRepository) Update(media *entity.Media) error {
	return r.db.Save(media).Error
}

func (r *mediaRepository) GetByID(id uint) (*entity.Media, error) {
	var media entity.Media
	err := r.db.First(&media, id).Error
	if err != nil {
		return nil, err
	}
	return &media, nil
}

func (r *mediaRepository) GetByTMDBID(tmdbID int, mediaType string) (*entity.Media, error) {
	var media entity.Media
	err := r.db.Where("tmdb_id = ? AND type = ?", tmdbID, mediaType).First(&media).Error
	if err != nil {
		return nil, err
	}
	return &media, nil
}

func (r *mediaRepository) GetByPath(path string) (*entity.Media, error) {
	var media entity.Media
	err := r.db.Where("path = ?", path).First(&media).Error
	if err != nil {
		return nil, err
	}
	return &media, nil
}

func (r *mediaRepository) List(offset, limit int) ([]entity.Media, error) {
	var medias []entity.Media
	err := r.db.Offset(offset).Limit(limit).Order("updated_at desc").Find(&medias).Error
	return medias, err
}

func (r *mediaRepository) Search(query string) ([]entity.Media, error) {
	var medias []entity.Media
	err := r.db.Where("title LIKE ?", "%"+query+"%").Limit(50).Find(&medias).Error
	return medias, err
}

func (r *mediaRepository) Delete(id uint) error {
	return r.db.Delete(&entity.Media{}, id).Error
}

func (r *mediaRepository) Count(mediaType string) (int64, error) {
	var count int64
	err := r.db.Model(&entity.Media{}).Where("type = ?", mediaType).Count(&count).Error
	return count, err
}
