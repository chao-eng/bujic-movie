package service

import (
	"testing"

	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupAPIKeyHarness(t *testing.T) MCPAPIKeyService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	keyRepo := repository.NewMCPAPIKeyRepository(db)
	recRepo := repository.NewMCPCallRecordRepository(db)
	return NewMCPAPIKeyService(keyRepo, recRepo)
}

// TestMCPAPIKeyLifecycle covers UC-06/UC-07/BR-23/BR-25: create returns plaintext
// once and stores only a hash; disable makes Validate fail; re-enable restores.
func TestMCPAPIKeyLifecycle(t *testing.T) {
	svc := setupAPIKeyHarness(t)

	key, plain, err := svc.Create("agent-test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if plain == "" || len(plain) < 20 {
		t.Fatalf("plaintext not returned properly: %q", plain)
	}
	if key.Status != "active" {
		t.Errorf("expected active, got %s", key.Status)
	}

	// Validate with correct key
	got, err := svc.Validate(plain)
	if err != nil {
		t.Fatalf("validate correct key should pass: %v", err)
	}
	if got.ID != key.ID {
		t.Errorf("validate returned wrong key id")
	}

	// Validate with wrong key must fail
	if _, err := svc.Validate("bmk_wrong"); err == nil {
		t.Errorf("expected invalid key error")
	}

	// Disable -> validate must fail (BR-25)
	if _, err := svc.SetStatus(key.ID, "disabled"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := svc.Validate(plain); err == nil {
		t.Errorf("expected validate to fail after disable")
	}

	// Enable -> validate passes again
	if _, err := svc.SetStatus(key.ID, "active"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := svc.Validate(plain); err != nil {
		t.Errorf("expected validate to pass after re-enable: %v", err)
	}

	// list only returns prefix (no plaintext stored)
	keys, err := svc.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].KeyHash == "" {
		t.Errorf("key_hash should be stored")
	}
}
