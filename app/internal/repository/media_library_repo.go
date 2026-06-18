package repository

import (
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"gorm.io/gorm"
)

type MediaLibraryRepository interface {
	Create(lib *entity.MediaLibrary) error
	Update(lib *entity.MediaLibrary) error
	Delete(id uint) error
	GetByID(id uint) (*entity.MediaLibrary, error)
	List() ([]entity.MediaLibrary, error)
}

type mediaLibraryRepository struct {
	db *gorm.DB
}

func NewMediaLibraryRepository(db *gorm.DB) MediaLibraryRepository {
	_ = db.AutoMigrate(&entity.MediaLibrary{})
	return &mediaLibraryRepository{db: db}
}

func (r *mediaLibraryRepository) Create(lib *entity.MediaLibrary) error {
	return r.db.Create(lib).Error
}

func (r *mediaLibraryRepository) Update(lib *entity.MediaLibrary) error {
	return r.db.Save(lib).Error
}

func (r *mediaLibraryRepository) Delete(id uint) error {
	return r.db.Delete(&entity.MediaLibrary{}, id).Error
}

func (r *mediaLibraryRepository) GetByID(id uint) (*entity.MediaLibrary, error) {
	var lib entity.MediaLibrary
	if err := r.db.First(&lib, id).Error; err != nil {
		return nil, err
	}
	return &lib, nil
}

func (r *mediaLibraryRepository) List() ([]entity.MediaLibrary, error) {
	var libs []entity.MediaLibrary
	err := r.db.Order("created_at asc").Find(&libs).Error
	return libs, err
}
