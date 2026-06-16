package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/internal/storage/local"
	"github.com/bujic-movie/bujic-movie/pkg/tmdb"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type mockScrapeService struct{}

func (m *mockScrapeService) ScrapePath(ctx context.Context, path string, overwrite bool) error {
	return nil
}

func (m *mockScrapeService) ScrapePathWithType(ctx context.Context, path string, overwrite bool, mediaType string) error {
	return nil
}

func TestTransferService(t *testing.T) {
	// 1. Setup GORM Memory Database
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open SQLite memory DB: %v", err)
	}

	repo := repository.NewTransferHistoryRepository(db)

	// 2. Setup Config with transfer rules
	cfg := &config.Config{}
	cfg.Transfer.Mode = "copy"
	cfg.Transfer.OverwriteMode = "size"
	cfg.Transfer.AutoScrape = true
	cfg.Transfer.MinFileSizeMB = 0

	tempDir, err := os.MkdirTemp("", "bujic-movie-transfer-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	srcDir := filepath.Join(tempDir, "downloads")
	movieDir := filepath.Join(tempDir, "media/movies")
	tvDir := filepath.Join(tempDir, "media/tv")

	_ = os.MkdirAll(srcDir, 0755)
	_ = os.MkdirAll(movieDir, 0755)
	_ = os.MkdirAll(tvDir, 0755)

	cfg.Media.MoviePath = movieDir
	cfg.Media.TVPath = tvDir
	cfg.Media.DownloadPath = srcDir

	// Create a mock movie file in downloads folder
	srcMovieFile := filepath.Join(srcDir, "Inception.2010.1080p.mkv")
	if err := os.WriteFile(srcMovieFile, []byte("inception video content"), 0644); err != nil {
		t.Fatalf("Failed to write mock movie file: %v", err)
	}

	// Create a mock subtitle file matching the movie filename
	srcSubFile := filepath.Join(srcDir, "Inception.2010.1080p.zh-cn.srt")
	if err := os.WriteFile(srcSubFile, []byte("inception subtitle content"), 0644); err != nil {
		t.Fatalf("Failed to write mock subtitle: %v", err)
	}

	// 3. Setup mock services
	movieDetail := &tmdb.MovieDetail{
		ID:            550,
		Title:         "Inception",
		OriginalTitle: "Inception",
		ReleaseDate:   "2010-07-16",
	}

	mockRec := &mockRecognizeService{details: movieDetail}
	mockScrape := &mockScrapeService{}
	tmdbClient := tmdb.NewClient("key", "", "zh-CN")
	stg := local.NewLocalStorage()
	namingSvc := NewNamingService()

	cardRepo := repository.NewMediaCardRepository(db)
	testCard := &entity.MediaCard{
		Name:         "Test Card",
		DownloadPath: srcDir,
		ArchivePath:  movieDir,
		MediaType:    "movie",
		IsDefault:    true,
	}
	if err := cardRepo.Create(testCard); err != nil {
		t.Fatalf("Failed to create test card: %v", err)
	}

	svc := NewTransferService(repo, namingSvc, mockRec, mockScrape, tmdbClient, stg, cfg, cardRepo)

	ctx := context.Background()
	// Submit task
	err = svc.SubmitTask(ctx, srcMovieFile, TransferOptions{
		CardID: testCard.ID,
	})
	if err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}

	// Wait for queue processing (async background worker)
	time.Sleep(1 * time.Second)

	// Check if movie file is copied to destination directory with proper structure
	expectedDestMovie := filepath.Join(movieDir, "Inception (2010)", "Inception (2010) [1080p].mkv")
	if _, err := os.Stat(expectedDestMovie); os.IsNotExist(err) {
		q := svc.GetQueue()
		t.Logf("Queue state: %+v", q)
		for _, taskItem := range q {
			t.Logf("Task error: %s", taskItem.Message)
		}
		h, _ := repo.List(0, 10)
		t.Logf("History state: %+v", h)
		t.Fatalf("Expected dest movie file %s to be created, but it was not", expectedDestMovie)
	}

	// Check if subtitle is copied and renamed correctly based on parsed language!
	expectedDestSub := filepath.Join(movieDir, "Inception (2010)", "Inception (2010) [1080p].zh-CN.srt")
	if _, err := os.Stat(expectedDestSub); os.IsNotExist(err) {
		t.Errorf("Expected dest subtitle file %s to be created, but it was not", expectedDestSub)
	}

	// Check history database record
	histories, err := repo.List(0, 10)
	if err != nil || len(histories) == 0 {
		t.Fatalf("No transfer history found: %v", err)
	}

	if histories[0].Status != "success" || histories[0].Mode != "copy" {
		t.Errorf("Unexpected transfer history: %+v", histories[0])
	}
}
