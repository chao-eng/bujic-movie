package repository

import (
	"time"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"gorm.io/gorm"
)

type MCPCallRecordFilter struct {
	APIKeyID *uint
	Tool     string
	Status   string
	From     *time.Time
	To       *time.Time
}

type MCPCallRecordRepository interface {
	Create(rec *entity.MCPCallRecord) error
	Query(f MCPCallRecordFilter, offset, limit int) ([]entity.MCPCallRecord, int64, error)
	PurgeBefore(cutoff time.Time, batch int) (int64, error)
	DeleteAll() error
}

type mcpCallRecordRepository struct {
	db *gorm.DB
}

func NewMCPCallRecordRepository(db *gorm.DB) MCPCallRecordRepository {
	_ = db.AutoMigrate(&entity.MCPCallRecord{})
	return &mcpCallRecordRepository{db: db}
}

func (r *mcpCallRecordRepository) Create(rec *entity.MCPCallRecord) error {
	return r.db.Create(rec).Error
}

func (r *mcpCallRecordRepository) Query(f MCPCallRecordFilter, offset, limit int) ([]entity.MCPCallRecord, int64, error) {
	var records []entity.MCPCallRecord
	var total int64

	q := r.db.Model(&entity.MCPCallRecord{})
	if f.APIKeyID != nil {
		q = q.Where("api_key_id = ?", *f.APIKeyID)
	}
	if f.Tool != "" {
		q = q.Where("tool = ?", f.Tool)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.From != nil {
		q = q.Where("created_at >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("created_at <= ?", *f.To)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := q.Order("created_at desc").Offset(offset).Limit(limit).Find(&records).Error
	return records, total, err
}

// PurgeBefore deletes records older than cutoff in small batches (transactional
// per batch) to avoid long-running locks (BR-34).
func (r *mcpCallRecordRepository) PurgeBefore(cutoff time.Time, batch int) (int64, error) {
	var deleted int64
	for {
		res := r.db.Where("created_at < ?", cutoff).Limit(batch).Delete(&entity.MCPCallRecord{})
		if res.Error != nil {
			return deleted, res.Error
		}
		if res.RowsAffected == 0 {
			break
		}
		deleted += res.RowsAffected
	}
	return deleted, nil
}

// DeleteAll truncates the call-record audit table (admin "清空调用记录").
func (r *mcpCallRecordRepository) DeleteAll() error {
	return r.db.Where("1 = 1").Delete(&entity.MCPCallRecord{}).Error
}
