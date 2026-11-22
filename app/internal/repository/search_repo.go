package repository

import (
	"errors"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"gorm.io/gorm"
)

// SearchRepository owns every table used by the search/download module.
type SearchRepository interface {
	// Definitions
	UpsertDefinition(def *entity.SearchIndexerDefinition) error
	GetDefinition(id string) (*entity.SearchIndexerDefinition, error)
	ListDefinitions() ([]entity.SearchIndexerDefinition, error)
	DeleteDefinition(id string) error

	// Indexers
	CreateIndexer(idx *entity.SearchIndexer) error
	UpdateIndexer(idx *entity.SearchIndexer) error
	DeleteIndexer(id uint) error
	GetIndexer(id uint) (*entity.SearchIndexer, error)
	GetIndexerByName(name string) (*entity.SearchIndexer, error)
	ListIndexers() ([]entity.SearchIndexer, error)
	EnabledIndexers() ([]entity.SearchIndexer, error)

	// Sessions
	GetSession(indexerID uint) (*entity.SearchIndexerSession, error)
	SaveSession(session *entity.SearchIndexerSession) error
	DeleteSession(indexerID uint) error
	DeleteExpiredSessions(before time.Time) error

	// Status
	GetStatus(indexerID uint) (*entity.SearchIndexerStatus, error)
	ListStatuses() ([]entity.SearchIndexerStatus, error)
	SaveStatus(status *entity.SearchIndexerStatus) error
	DeleteStatus(indexerID uint) error

	// Download clients
	CreateClient(client *entity.SearchDownloadClient) error
	UpdateClient(client *entity.SearchDownloadClient) error
	DeleteClient(id uint) error
	GetClient(id uint) (*entity.SearchDownloadClient, error)
	GetClientByName(name string) (*entity.SearchDownloadClient, error)
	ListClients() ([]entity.SearchDownloadClient, error)
	EnabledClients() ([]entity.SearchDownloadClient, error)
	ListClientCategories(clientID uint) ([]entity.SearchDownloadClientCategory, error)
	ReplaceClientCategories(clientID uint, categories []entity.SearchDownloadClientCategory) error
	UnbindIndexersFromClient(clientID uint) error

	// Records
	CreateRecord(record *entity.SearchDownloadRecord) error
	FindRecentRecord(fingerprint string, since time.Time) (*entity.SearchDownloadRecord, error)
	ListRecords(limit int) ([]entity.SearchDownloadRecord, error)
	DeleteRecordsBefore(before time.Time) error

	// History
	TouchHistory(keyword string) error
	ListHistory(limit int) ([]entity.SearchHistory, error)
	DeleteHistory(keyword string) error
	ClearHistory() error

	// Title aliases
	GetTitleAlias(keyword, mediaType string) (*entity.SearchTitleAlias, error)
	SaveTitleAlias(alias *entity.SearchTitleAlias) error
	DeleteTitleAlias(keyword string) error
	DeleteTitleAliasesBefore(before time.Time) error
}

type searchRepository struct {
	db *gorm.DB
}

// NewSearchRepository migrates the module tables. Only new tables are created;
// existing tables are never altered (BR-40).
func NewSearchRepository(db *gorm.DB) SearchRepository {
	_ = db.AutoMigrate(
		&entity.SearchIndexerDefinition{},
		&entity.SearchIndexer{},
		&entity.SearchIndexerSession{},
		&entity.SearchIndexerStatus{},
		&entity.SearchDownloadClient{},
		&entity.SearchDownloadClientCategory{},
		&entity.SearchDownloadRecord{},
		&entity.SearchHistory{},
		&entity.SearchTitleAlias{},
	)
	return &searchRepository{db: db}
}

func (r *searchRepository) UpsertDefinition(def *entity.SearchIndexerDefinition) error {
	def.UpdatedAt = time.Now()
	return r.db.Save(def).Error
}

func (r *searchRepository) GetDefinition(id string) (*entity.SearchIndexerDefinition, error) {
	var def entity.SearchIndexerDefinition
	if err := r.db.First(&def, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &def, nil
}

func (r *searchRepository) ListDefinitions() ([]entity.SearchIndexerDefinition, error) {
	var defs []entity.SearchIndexerDefinition
	err := r.db.Order("name asc").Find(&defs).Error
	return defs, err
}

func (r *searchRepository) DeleteDefinition(id string) error {
	return r.db.Delete(&entity.SearchIndexerDefinition{}, "id = ?", id).Error
}

func (r *searchRepository) CreateIndexer(idx *entity.SearchIndexer) error {
	return r.db.Create(idx).Error
}

func (r *searchRepository) UpdateIndexer(idx *entity.SearchIndexer) error {
	return r.db.Save(idx).Error
}

func (r *searchRepository) DeleteIndexer(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&entity.SearchIndexerSession{}, "indexer_id = ?", id).Error; err != nil {
			return err
		}
		if err := tx.Delete(&entity.SearchIndexerStatus{}, "indexer_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&entity.SearchIndexer{}, id).Error
	})
}

func (r *searchRepository) GetIndexer(id uint) (*entity.SearchIndexer, error) {
	var idx entity.SearchIndexer
	if err := r.db.First(&idx, id).Error; err != nil {
		return nil, err
	}
	return &idx, nil
}

func (r *searchRepository) GetIndexerByName(name string) (*entity.SearchIndexer, error) {
	var idx entity.SearchIndexer
	if err := r.db.First(&idx, "name = ?", name).Error; err != nil {
		return nil, err
	}
	return &idx, nil
}

func (r *searchRepository) ListIndexers() ([]entity.SearchIndexer, error) {
	var list []entity.SearchIndexer
	err := r.db.Order("priority asc, id asc").Find(&list).Error
	return list, err
}

func (r *searchRepository) EnabledIndexers() ([]entity.SearchIndexer, error) {
	var list []entity.SearchIndexer
	err := r.db.Where("enabled = ?", true).Order("priority asc, id asc").Find(&list).Error
	return list, err
}

func (r *searchRepository) GetSession(indexerID uint) (*entity.SearchIndexerSession, error) {
	var s entity.SearchIndexerSession
	if err := r.db.First(&s, "indexer_id = ?", indexerID).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *searchRepository) SaveSession(session *entity.SearchIndexerSession) error {
	session.UpdatedAt = time.Now()
	var existing entity.SearchIndexerSession
	err := r.db.First(&existing, "indexer_id = ?", session.IndexerID).Error
	if err == gorm.ErrRecordNotFound {
		return r.db.Create(session).Error
	}
	if err != nil {
		return err
	}
	return r.db.Save(session).Error
}

func (r *searchRepository) DeleteSession(indexerID uint) error {
	return r.db.Delete(&entity.SearchIndexerSession{}, "indexer_id = ?", indexerID).Error
}

func (r *searchRepository) DeleteExpiredSessions(before time.Time) error {
	return r.db.Where("expires_at < ?", before).Delete(&entity.SearchIndexerSession{}).Error
}

func (r *searchRepository) GetStatus(indexerID uint) (*entity.SearchIndexerStatus, error) {
	var s entity.SearchIndexerStatus
	if err := r.db.First(&s, "indexer_id = ?", indexerID).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *searchRepository) ListStatuses() ([]entity.SearchIndexerStatus, error) {
	var list []entity.SearchIndexerStatus
	err := r.db.Find(&list).Error
	return list, err
}

func (r *searchRepository) SaveStatus(status *entity.SearchIndexerStatus) error {
	return r.db.Save(status).Error
}

func (r *searchRepository) DeleteStatus(indexerID uint) error {
	return r.db.Delete(&entity.SearchIndexerStatus{}, "indexer_id = ?", indexerID).Error
}

func (r *searchRepository) CreateClient(client *entity.SearchDownloadClient) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if client.IsDefault {
			if err := tx.Model(&entity.SearchDownloadClient{}).Where("1 = 1").Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Create(client).Error
	})
}

func (r *searchRepository) UpdateClient(client *entity.SearchDownloadClient) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if client.IsDefault {
			if err := tx.Model(&entity.SearchDownloadClient{}).Where("id != ?", client.ID).Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Save(client).Error
	})
}

func (r *searchRepository) DeleteClient(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&entity.SearchDownloadClientCategory{}, "downloader_id = ?", id).Error; err != nil {
			return err
		}
		if err := tx.Model(&entity.SearchIndexer{}).Where("download_client_id = ?", id).Update("download_client_id", 0).Error; err != nil {
			return err
		}
		return tx.Delete(&entity.SearchDownloadClient{}, id).Error
	})
}

func (r *searchRepository) GetClient(id uint) (*entity.SearchDownloadClient, error) {
	var c entity.SearchDownloadClient
	if err := r.db.First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *searchRepository) GetClientByName(name string) (*entity.SearchDownloadClient, error) {
	var c entity.SearchDownloadClient
	if err := r.db.First(&c, "name = ?", name).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *searchRepository) ListClients() ([]entity.SearchDownloadClient, error) {
	var list []entity.SearchDownloadClient
	err := r.db.Order("priority asc, id asc").Find(&list).Error
	return list, err
}

func (r *searchRepository) EnabledClients() ([]entity.SearchDownloadClient, error) {
	var list []entity.SearchDownloadClient
	err := r.db.Where("enabled = ?", true).Order("priority asc, id asc").Find(&list).Error
	return list, err
}

func (r *searchRepository) ListClientCategories(clientID uint) ([]entity.SearchDownloadClientCategory, error) {
	var list []entity.SearchDownloadClientCategory
	err := r.db.Where("downloader_id = ?", clientID).Order("source_category asc").Find(&list).Error
	return list, err
}

func (r *searchRepository) ReplaceClientCategories(clientID uint, categories []entity.SearchDownloadClientCategory) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&entity.SearchDownloadClientCategory{}, "downloader_id = ?", clientID).Error; err != nil {
			return err
		}
		for i := range categories {
			categories[i].ID = 0
			categories[i].DownloaderID = clientID
			if err := tx.Create(&categories[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *searchRepository) UnbindIndexersFromClient(clientID uint) error {
	return r.db.Model(&entity.SearchIndexer{}).Where("download_client_id = ?", clientID).Update("download_client_id", 0).Error
}

func (r *searchRepository) CreateRecord(record *entity.SearchDownloadRecord) error {
	return r.db.Create(record).Error
}

func (r *searchRepository) FindRecentRecord(fingerprint string, since time.Time) (*entity.SearchDownloadRecord, error) {
	var rec entity.SearchDownloadRecord
	err := r.db.Where("fingerprint = ? AND created_at >= ?", fingerprint, since).
		Order("created_at desc").First(&rec).Error
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *searchRepository) ListRecords(limit int) ([]entity.SearchDownloadRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var list []entity.SearchDownloadRecord
	err := r.db.Order("created_at desc").Limit(limit).Find(&list).Error
	return list, err
}

func (r *searchRepository) DeleteRecordsBefore(before time.Time) error {
	return r.db.Where("created_at < ?", before).Delete(&entity.SearchDownloadRecord{}).Error
}

func (r *searchRepository) TouchHistory(keyword string) error {
	keyword = trimKeyword(keyword)
	if keyword == "" {
		return nil
	}
	now := time.Now()
	var existing entity.SearchHistory
	err := r.db.First(&existing, "keyword = ?", keyword).Error
	switch {
	case err == gorm.ErrRecordNotFound:
		return r.db.Create(&entity.SearchHistory{Keyword: keyword, UseCount: 1, LastUsedAt: now}).Error
	case err != nil:
		return err
	}
	existing.UseCount++
	existing.LastUsedAt = now
	return r.db.Save(&existing).Error
}

func trimKeyword(keyword string) string {
	out := make([]rune, 0, len(keyword))
	space := false
	for _, r := range keyword {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !space && len(out) > 0 {
				out = append(out, ' ')
				space = true
			}
			continue
		}
		out = append(out, r)
		space = false
	}
	for len(out) > 0 && out[len(out)-1] == ' ' {
		out = out[:len(out)-1]
	}
	return string(out)
}

func (r *searchRepository) ListHistory(limit int) ([]entity.SearchHistory, error) {
	if limit <= 0 {
		limit = 20
	}
	var list []entity.SearchHistory
	err := r.db.Order("last_used_at desc").Limit(limit).Find(&list).Error
	return list, err
}

func (r *searchRepository) DeleteHistory(keyword string) error {
	return r.db.Where("keyword = ?", trimKeyword(keyword)).Delete(&entity.SearchHistory{}).Error
}

func (r *searchRepository) ClearHistory() error {
	return r.db.Where("1 = 1").Delete(&entity.SearchHistory{}).Error
}

func (r *searchRepository) GetTitleAlias(keyword, mediaType string) (*entity.SearchTitleAlias, error) {
	var alias entity.SearchTitleAlias
	err := r.db.Where("keyword = ? AND media_type = ?", keyword, mediaType).First(&alias).Error
	if err != nil {
		return nil, err
	}
	return &alias, nil
}

func (r *searchRepository) SaveTitleAlias(alias *entity.SearchTitleAlias) error {
	now := time.Now()
	alias.UpdatedAt = now
	if alias.CreatedAt.IsZero() {
		alias.CreatedAt = now
	}
	// The table has a unique index on (keyword, media_type), so an existing row
	// is updated in place instead of failing on a duplicate key.
	if alias.ID == 0 {
		var existing entity.SearchTitleAlias
		err := r.db.Where("keyword = ? AND media_type = ?", alias.Keyword, alias.MediaType).First(&existing).Error
		if err == nil {
			alias.ID = existing.ID
			alias.CreatedAt = existing.CreatedAt
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	return r.db.Save(alias).Error
}

func (r *searchRepository) DeleteTitleAlias(keyword string) error {
	return r.db.Where("keyword = ?", keyword).Delete(&entity.SearchTitleAlias{}).Error
}

func (r *searchRepository) DeleteTitleAliasesBefore(before time.Time) error {
	return r.db.Where("updated_at < ?", before).Delete(&entity.SearchTitleAlias{}).Error
}
