package service

import (
	"testing"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestMCPCallRecords covers BR-26/BR-27 basics at the repository/service layer:
// records are queryable with filters/pagination and the retention purge removes
// old rows in bounded batches.
func TestMCPCallRecords(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	recRepo := repository.NewMCPCallRecordRepository(db)

	now := time.Now()
	recs := []*entity.MCPCallRecord{
		{APIKeyID: 1, Tool: "query_media_list", Status: "ok", DurationMS: 5, InputMeta: `{"limit":10}`, ResultBytes: 42, CreatedAt: now},
		{APIKeyID: 1, Tool: "fetch_subtitle", Status: "error", ErrorCode: "TOOL_ERROR", DurationMS: 8, InputMeta: `{"is_internal":true}`, ResultBytes: 0, CreatedAt: now.Add(-2 * time.Hour)},
		{APIKeyID: 2, Tool: "query_media_list", Status: "ok", DurationMS: 3, InputMeta: `{}`, ResultBytes: 12, CreatedAt: now.Add(-10 * 24 * time.Hour)},
	}
	for _, r := range recs {
		if err := recRepo.Create(r); err != nil {
			t.Fatalf("create rec: %v", err)
		}
	}

	// filter by key + tool
	kid := uint(1)
	rows, total, err := recRepo.Query(repository.MCPCallRecordFilter{APIKeyID: &kid, Tool: "query_media_list"}, 0, 10)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("expected 1 row for key1+query_media_list, got total=%d len=%d", total, len(rows))
	}

	// pagination across all
	_, totalAll, err := recRepo.Query(repository.MCPCallRecordFilter{}, 0, 10)
	if err != nil {
		t.Fatalf("query all: %v", err)
	}
	if totalAll != 3 {
		t.Fatalf("expected 3 total, got %d", totalAll)
	}

	// retention purge: only key2's 10-day-old record should be removed (purge < 3 days)
	cutoff := now.Add(-3 * 24 * time.Hour)
	deleted, err := recRepo.PurgeBefore(cutoff, 10)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 purged, got %d", deleted)
	}
	_, totalAfter, _ := recRepo.Query(repository.MCPCallRecordFilter{}, 0, 10)
	if totalAfter != 2 {
		t.Fatalf("expected 2 remaining, got %d", totalAfter)
	}
}
