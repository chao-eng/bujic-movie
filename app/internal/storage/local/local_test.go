package local

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStorage(t *testing.T) {
	s := NewLocalStorage()
	tempDir, err := os.MkdirTemp("", "bujic-movie-storage-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test Mkdir
	testDir := filepath.Join(tempDir, "test_dir")
	if err := s.Mkdir(testDir); err != nil {
		t.Fatalf("Failed to Mkdir: %v", err)
	}

	// Test Write and Read
	testFile := filepath.Join(testDir, "test_file.txt")
	content := "hello world"
	if err := s.Write(testFile, strings.NewReader(content)); err != nil {
		t.Fatalf("Failed to Write: %v", err)
	}

	r, err := s.Read(testFile)
	if err != nil {
		t.Fatalf("Failed to Read: %v", err)
	}
	defer r.Close()

	data, err := os.ReadFile(testFile)
	if err != nil || string(data) != content {
		t.Fatalf("Content mismatch: expected %q, got %q (err: %v)", content, string(data), err)
	}

	// Test Hash
	hash, err := s.Hash(testFile)
	if err != nil {
		t.Fatalf("Failed to Hash: %v", err)
	}
	// sha256 of "hello world" is b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9
	expectedHash := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if hash != expectedHash {
		t.Errorf("Expected hash %s, got %s", expectedHash, hash)
	}

	// Test Copy
	copyFile := filepath.Join(testDir, "copy_file.txt")
	if err := s.Copy(testFile, copyFile); err != nil {
		t.Fatalf("Failed to Copy: %v", err)
	}
	if !s.IsDir(testDir) {
		t.Errorf("Expected testDir to be a directory")
	}

	// Test Move
	moveFile := filepath.Join(testDir, "move_file.txt")
	if err := s.Move(copyFile, moveFile); err != nil {
		t.Fatalf("Failed to Move: %v", err)
	}
	if _, err := os.Stat(copyFile); !os.IsNotExist(err) {
		t.Errorf("Source file should have been deleted after move")
	}

	// Test List
	items, err := s.List(testDir)
	if err != nil {
		t.Fatalf("Failed to List: %v", err)
	}
	if len(items) != 2 { // test_file.txt and move_file.txt
		t.Errorf("Expected 2 items, got %d", len(items))
	}
}
