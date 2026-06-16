package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/internal/storage/local"
	"github.com/bujic-movie/bujic-movie/pkg/parser"
	"github.com/bujic-movie/bujic-movie/pkg/tmdb"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type mockRecognizeService struct {
	details interface{}
}

func (m *mockRecognizeService) Recognize(ctx context.Context, path string) (*parser.Metadata, interface{}, error) {
	return m.RecognizeWithType(ctx, path, "")
}

func (m *mockRecognizeService) RecognizeWithType(ctx context.Context, path string, mediaType string) (*parser.Metadata, interface{}, error) {
	meta := parser.ParseFilename(path)
	if mediaType == "movie" {
		meta.IsMovie = true
	} else if mediaType == "tv" {
		meta.IsMovie = false
	}
	return meta, m.details, nil
}

func TestScrapeService(t *testing.T) {
	// Set up in-memory sqlite DB for testing GORM persistence layer
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open sqlite memory DB: %v", err)
	}

	repo := repository.NewMediaRepository(db)
	stg := local.NewLocalStorage()

	tempDir, err := os.MkdirTemp("", "bujic-movie-scrape-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a mock movie file
	movieFile := filepath.Join(tempDir, "Mock Movie (2024).mkv")
	if err := os.WriteFile(movieFile, []byte("video data"), 0644); err != nil {
		t.Fatalf("Failed to write mock movie file: %v", err)
	}

	movieDetail := &tmdb.MovieDetail{
		ID:            123,
		Title:         "Mock Movie",
		OriginalTitle: "Mock Movie Original",
		ReleaseDate:   "2024-01-01",
		VoteAverage:   8.5,
		Runtime:       120,
	}

	mockRec := &mockRecognizeService{details: movieDetail}
	tmdbClient := tmdb.NewClient("key", "", "zh-CN")

	svc := NewScrapeService(repo, mockRec, tmdbClient, stg)

	ctx := context.Background()
	if err := svc.ScrapePath(ctx, movieFile, true); err != nil {
		t.Fatalf("ScrapePath failed: %v", err)
	}

	// Verify NFO file is created
	nfoFile := filepath.Join(tempDir, "Mock Movie (2024).nfo")
	if _, err := os.Stat(nfoFile); os.IsNotExist(err) {
		t.Errorf("Expected NFO file %s to be created, but it was not", nfoFile)
	}

	// Verify record exists in DB
	record, err := repo.GetByPath(movieFile)
	if err != nil {
		t.Fatalf("Failed to get media from DB: %v", err)
	}
	if record.Title != "Mock Movie" || record.TMDBID != 123 {
		t.Errorf("DB record mismatch: %+v", record)
	}
}
