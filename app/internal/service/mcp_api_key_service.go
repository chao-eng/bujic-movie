package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/pkg/logger"
)

const keyPrefix = "bmk_"

// MCPAPIKeyService implements API-key lifecycle management for MCP clients
// (UC-06/07) plus record query + retention glue used by the MCP gateway.
type MCPAPIKeyService interface {
	Create(name string) (*entity.MCPAPIKey, string, error) // returns key + plaintext (once)
	SetStatus(id uint, status string) (*entity.MCPAPIKey, error)
	Delete(id uint) error
	ClearRecords() error
	Validate(rawKey string) (*entity.MCPAPIKey, error)
	List() ([]entity.MCPAPIKey, error)
	GetByID(id uint) (*entity.MCPAPIKey, error)
	QueryRecords(f repository.MCPCallRecordFilter, page, limit int) ([]entity.MCPCallRecord, int64, error)
	StartRetentionLoop(retention time.Duration, interval time.Duration)
}

type mcpAPIKeyService struct {
	keyRepo repository.MCPAPIKeyRepository
	recRepo repository.MCPCallRecordRepository
	stopCh  chan struct{}
}

func NewMCPAPIKeyService(keyRepo repository.MCPAPIKeyRepository, recRepo repository.MCPCallRecordRepository) MCPAPIKeyService {
	return &mcpAPIKeyService{
		keyRepo: keyRepo,
		recRepo: recRepo,
		stopCh:  make(chan struct{}),
	}
}

func hashKey(raw string, salt string) string {
	h := sha256.Sum256([]byte(salt + ":" + raw))
	return hex.EncodeToString(h[:])
}

func generateSalt() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Create generates a new API key, stores only the salted hash, and returns the
// plaintext exactly once (BR-23).
func (s *mcpAPIKeyService) Create(name string) (*entity.MCPAPIKey, string, error) {
	if name == "" {
		name = "mcp-default"
	}
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return nil, "", errors.New("failed to generate secret")
	}
	plain := keyPrefix + strings.TrimRight(base64Encode(secretBytes), "=")
	salt := generateSalt()

	key := &entity.MCPAPIKey{
		Name:      name,
		KeyHash:   salt + ":" + hashKey(plain, salt),
		KeyPrefix: displayPrefix(plain),
		Status:    "active",
	}
	if err := s.keyRepo.Create(key); err != nil {
		return nil, "", err
	}
	return key, plain, nil
}

func displayPrefix(plain string) string {
	if len(plain) > 12 {
		return plain[:12] + "…"
	}
	return plain
}

// SetStatus flips a key active/disabled (UC-07, BR-25).
func (s *mcpAPIKeyService) SetStatus(id uint, status string) (*entity.MCPAPIKey, error) {
	if status != "active" && status != "disabled" {
		return nil, errors.New("status must be active or disabled")
	}
	return s.keyRepo.SetStatus(id, status)
}

// Delete removes an API key (soft delete). The key stops validating
// immediately; its historical call records are preserved for audit.
func (s *mcpAPIKeyService) Delete(id uint) error {
	if _, err := s.keyRepo.GetByID(id); err != nil {
		return errors.New("api key not found")
	}
	return s.keyRepo.Delete(id)
}

// ClearRecords deletes ALL MCP call records (admin "清空调用记录").
func (s *mcpAPIKeyService) ClearRecords() error {
	return s.recRepo.DeleteAll()
}

// Validate checks a raw key against stored active keys. Returns the matched key
// when valid, otherwise an error (BR-18a/BR-19/BR-25).
func (s *mcpAPIKeyService) Validate(rawKey string) (*entity.MCPAPIKey, error) {
	if rawKey == "" {
		return nil, errors.New("missing api key")
	}
	keys, err := s.keyRepo.List()
	if err != nil {
		return nil, err
	}
	for i := range keys {
		k := &keys[i]
		if k.Status != "active" {
			continue
		}
		if k.KeyHash == "" {
			continue
		}
		parts := strings.SplitN(k.KeyHash, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if hashKey(rawKey, parts[0]) == parts[1] {
			_ = s.keyRepo.TouchLastUsed(k.ID)
			return k, nil
		}
	}
	return nil, errors.New("invalid api key")
}

func (s *mcpAPIKeyService) List() ([]entity.MCPAPIKey, error) {
	return s.keyRepo.List()
}

func (s *mcpAPIKeyService) GetByID(id uint) (*entity.MCPAPIKey, error) {
	return s.keyRepo.GetByID(id)
}

func (s *mcpAPIKeyService) QueryRecords(f repository.MCPCallRecordFilter, page, limit int) ([]entity.MCPCallRecord, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 20
	}
	offset := (page - 1) * limit
	return s.recRepo.Query(f, offset, limit)
}

// StartRetentionLoop periodically purges call records older than retention
// (BR-34). Run in a goroutine.
func (s *mcpAPIKeyService) StartRetentionLoop(retention, interval time.Duration) {
	if retention <= 0 {
		retention = 180 * 24 * time.Hour
	}
	if interval <= 0 {
		interval = time.Hour
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				cutoff := time.Now().Add(-retention)
				n, err := s.recRepo.PurgeBefore(cutoff, 500)
				if err != nil {
					logger.Warn("[MCP] 调用记录清理失败: %v", err)
				} else if n > 0 {
					logger.Info("[MCP] 已清理过期调用记录 %d 条", n)
				}
			}
		}
	}()
}
