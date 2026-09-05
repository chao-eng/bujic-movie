package repository

import (
	"time"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"gorm.io/gorm"
)

type MCPAPIKeyRepository interface {
	Create(key *entity.MCPAPIKey) error
	Update(key *entity.MCPAPIKey) error
	GetByID(id uint) (*entity.MCPAPIKey, error)
	List() ([]entity.MCPAPIKey, error)
	SetStatus(id uint, status string) (*entity.MCPAPIKey, error)
	TouchLastUsed(id uint) error
}

type mcpAPIKeyRepository struct {
	db *gorm.DB
}

func NewMCPAPIKeyRepository(db *gorm.DB) MCPAPIKeyRepository {
	_ = db.AutoMigrate(&entity.MCPAPIKey{})
	return &mcpAPIKeyRepository{db: db}
}

func (r *mcpAPIKeyRepository) Create(key *entity.MCPAPIKey) error {
	return r.db.Create(key).Error
}

func (r *mcpAPIKeyRepository) Update(key *entity.MCPAPIKey) error {
	return r.db.Save(key).Error
}

func (r *mcpAPIKeyRepository) GetByID(id uint) (*entity.MCPAPIKey, error) {
	var key entity.MCPAPIKey
	if err := r.db.First(&key, id).Error; err != nil {
		return nil, err
	}
	return &key, nil
}

func (r *mcpAPIKeyRepository) List() ([]entity.MCPAPIKey, error) {
	var keys []entity.MCPAPIKey
	if err := r.db.Order("created_at desc").Find(&keys).Error; err != nil {
		return nil, err
	}
	return keys, nil
}

func (r *mcpAPIKeyRepository) SetStatus(id uint, status string) (*entity.MCPAPIKey, error) {
	var key entity.MCPAPIKey
	if err := r.db.First(&key, id).Error; err != nil {
		return nil, err
	}
	key.Status = status
	if err := r.db.Save(&key).Error; err != nil {
		return nil, err
	}
	return &key, nil
}

func (r *mcpAPIKeyRepository) TouchLastUsed(id uint) error {
	return r.db.Model(&entity.MCPAPIKey{}).Where("id = ?", id).Update("last_used_at", time.Now()).Error
}
