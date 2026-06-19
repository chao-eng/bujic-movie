package repository

import (
	"path/filepath"
	"strings"

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
	ListAll(pathPrefix string) ([]entity.Media, error)
	Search(query string, pathPrefix string) ([]entity.Media, error)
	GetEpisodes(tmdbID int, season int, parentPath string) ([]entity.Media, error)
	Delete(id uint) error
	DeleteSeason(tmdbID int, season int) error
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

func (r *mediaRepository) ListAll(pathPrefix string) ([]entity.Media, error) {
	var medias []entity.Media
	query := r.db.Order("updated_at desc")
	if pathPrefix != "" {
		pattern := pathPrefix
		if !strings.HasSuffix(pattern, string(filepath.Separator)) {
			pattern += string(filepath.Separator)
		}
		query = query.Where("path LIKE ?", pattern+"%")
	}
	err := query.Find(&medias).Error
	return medias, err
}

func (r *mediaRepository) Search(query string, pathPrefix string) ([]entity.Media, error) {
	var medias []entity.Media
	dbQuery := r.db.Where("title LIKE ?", "%"+query+"%").Limit(50)
	if pathPrefix != "" {
		pattern := pathPrefix
		if !strings.HasSuffix(pattern, string(filepath.Separator)) {
			pattern += string(filepath.Separator)
		}
		dbQuery = dbQuery.Where("path LIKE ?", pattern+"%")
	}
	err := dbQuery.Find(&medias).Error
	return medias, err
}

func (r *mediaRepository) GetEpisodes(tmdbID int, season int, parentPath string) ([]entity.Media, error) {
	var medias []entity.Media
	var err error
	if tmdbID > 0 {
		err = r.db.Where("tmdb_id = ? AND type = ? AND season = ?", tmdbID, "tv", season).Order("path asc").Find(&medias).Error
	} else if parentPath != "" {
		pattern := parentPath
		if !strings.HasSuffix(pattern, string(filepath.Separator)) {
			pattern += string(filepath.Separator)
		}
		err = r.db.Where("type = ? AND path LIKE ?", "tv", pattern+"%").Order("path asc").Find(&medias).Error
	}
	return medias, err
}

func (r *mediaRepository) Delete(id uint) error {
	return r.db.Delete(&entity.Media{}, id).Error
}

func (r *mediaRepository) DeleteSeason(tmdbID int, season int) error {
	return r.db.Where("type = ? AND tmdb_id = ? AND season = ?", "tv", tmdbID, season).Delete(&entity.Media{}).Error
}

func (r *mediaRepository) Count(mediaType string) (int64, error) {
	var count int64
	if mediaType == "tv" {
		err := r.db.Model(&entity.Media{}).Where("type = ?", "tv").Select("count(distinct(tmdb_id || '-' || season))").Scan(&count).Error
		return count, err
	}
	err := r.db.Model(&entity.Media{}).Where("type = ?", mediaType).Count(&count).Error
	return count, err
}
