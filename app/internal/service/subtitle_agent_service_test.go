package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/internal/storage/local"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type agentHarness struct {
	svc       SubtitleAgentService
	mediaRepo repository.MediaRepository
	cardRepo  repository.MediaCardRepository
	card      *entity.MediaCard
	archive   string
}

func setupSubtitleAgentHarness(t *testing.T) *agentHarness {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(
		fmt.Sprintf("file:agent-%d?mode=memory&cache=shared", time.Now().UnixNano()),
	), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	mediaRepo := repository.NewMediaRepository(db)
	cardRepo := repository.NewMediaCardRepository(db)
	stg := local.NewLocalStorage()

	tmp, err := os.MkdirTemp("", "bujic-agent-test")
	if err != nil {
		t.Fatalf("mk temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })

	archive := filepath.Join(tmp, "media", "movies")
	if err := os.MkdirAll(archive, 0755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}

	card := &entity.MediaCard{
		Name:         "movies",
		ArchivePath:  archive,
		DownloadPath: filepath.Join(tmp, "downloads"),
		MediaType:    "movie",
		IsDefault:    true,
	}
	if err := cardRepo.Create(card); err != nil {
		t.Fatalf("create card: %v", err)
	}

	svc := NewSubtitleAgentService(mediaRepo, cardRepo, stg)
	return &agentHarness{svc: svc, mediaRepo: mediaRepo, cardRepo: cardRepo, card: card, archive: archive}
}

// addCard persists an additional media card whose archive dir already exists.
func addCard(t *testing.T, h *agentHarness, name string, archivePath string) *entity.MediaCard {
	t.Helper()
	if err := os.MkdirAll(archivePath, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", archivePath, err)
	}
	card := &entity.MediaCard{
		Name:         name,
		ArchivePath:  archivePath,
		DownloadPath: filepath.Join(filepath.Dir(archivePath), "downloads-"+name),
		MediaType:    "tv",
	}
	if err := h.cardRepo.Create(card); err != nil {
		t.Fatalf("create card %s: %v", name, err)
	}
	return card
}

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func addMovieRow(h *agentHarness, title string, year int, videoPath string) *entity.Media {
	m := &entity.Media{
		Title: title,
		Year:  year,
		Type:  "movie",
		Path:  videoPath,
	}
	if err := h.mediaRepo.Create(m); err != nil {
		panic(err)
	}
	return m
}

// TestAgentSubtitleFlow: movie with only an English external sub.
// list -> has_subtitle=true, languages=[en], missing_subtitles=[zh-CN];
// upload zh-CN -> file named <base>.zh-CN.srt; list now shows zh-CN.
func TestAgentSubtitleFlow(t *testing.T) {
	h := setupSubtitleAgentHarness(t)
	ctx := context.Background()

	base := "Inception (2010) [1080p]"
	videoPath := writeFile(t, filepath.Join(h.archive, base+".mkv"), "video")
	enSub := writeFile(t, filepath.Join(h.archive, base+".en.srt"), "1\n00:00:01,000 --> 00:00:04,000\nHello\n")
	_ = enSub
	addMovieRow(h, "Inception (2010)", 2010, videoPath)

	// 1. list -> en present, zh-CN missing
	list, err := h.svc.QueryMediaList(ctx, MediaListRequest{MediaType: "movie"})
	if err != nil {
		t.Fatalf("QueryMediaList: %v", err)
	}
	if list.Total != 1 {
		t.Fatalf("expected 1 media, got %d", list.Total)
	}
	item := list.Items[0]
	if !item.HasSubtitle {
		t.Errorf("expected has_subtitle=true, got false")
	}
	if item.SubtitleStatus != SubtitleStatusFull {
		t.Errorf("expected subtitle_status=full, got %s", item.SubtitleStatus)
	}
	if len(item.Languages) != 1 || item.Languages[0] != "en" {
		t.Errorf("expected languages=[en], got %v", item.Languages)
	}
	if len(item.MissingSubtitles) != 1 || item.MissingSubtitles[0] != "zh-CN" {
		t.Errorf("expected missing_subtitles=[zh-CN], got %v", item.MissingSubtitles)
	}

	// 2. query subtitles detail
	details, err := h.svc.QuerySubtitles(ctx, QuerySubtitlesRequest{MediaID: item.MediaID})
	if err != nil {
		t.Fatalf("QuerySubtitles: %v", err)
	}
	if len(details) != 1 || len(details[0].Subtitles) == 0 {
		t.Fatalf("expected >=1 subtitle record, got %+v", details)
	}

	// 3. fetch external subtitle content
	fetched, err := h.svc.FetchSubtitle(ctx, FetchSubtitleRequest{Path: details[0].Subtitles[0].Path})
	if err != nil {
		t.Fatalf("FetchSubtitle: %v", err)
	}
	if fetched.Format != "srt" || !strings.Contains(fetched.Content, "Hello") {
		t.Errorf("unexpected fetch result: %+v", fetched)
	}

	// 4. upload zh-CN
	zhText := "1\n00:00:01,000 --> 00:00:04,000\n你好\n"
	up, err := h.svc.UploadSubtitle(ctx, UploadSubtitleRequest{
		VideoPath:       videoPath,
		SubtitleContent: zhText,
		Language:        "zh-CN",
		Format:          "srt",
	})
	if err != nil {
		t.Fatalf("UploadSubtitle: %v", err)
	}
	if up.OverwriteExisting {
		t.Errorf("expected overwrite_existing=false on first upload")
	}
	if filepath.Base(up.Path) != base+".zh-CN.srt" {
		t.Errorf("expected filename %s.zh-CN.srt, got %s", base, filepath.Base(up.Path))
	}

	// 5. re-list -> zh-CN no longer missing
	list2, err := h.svc.QueryMediaList(ctx, MediaListRequest{MediaType: "movie"})
	if err != nil {
		t.Fatalf("re-QueryMediaList: %v", err)
	}
	item2 := list2.Items[0]
	if !containsStr(item2.Languages, "zh-CN") {
		t.Errorf("expected languages to contain zh-CN, got %v", item2.Languages)
	}
	if containsStr(item2.MissingSubtitles, "zh-CN") {
		t.Errorf("expected missing_subtitles to NOT contain zh-CN, got %v", item2.MissingSubtitles)
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestAgentSubtitleFlowConcurrent(t *testing.T) {
	h := setupSubtitleAgentHarness(t)
	ctx := context.Background()

	base := "Concurrent (2020) [1080p]"
	videoPath := writeFile(t, filepath.Join(h.archive, base+".mkv"), "video")
	writeFile(t, filepath.Join(h.archive, base+".en.srt"), "1\n00:00:01,000 --> 00:00:04,000\nHi\n")
	addMovieRow(h, "Concurrent (2020)", 2020, videoPath)

	// Exercise list + detail concurrently to catch data races on the probe cache
	// and media aggregation (BR-30). Run without -race this is a smoke test; with
	// a race-enabled build it validates the mutex usage.
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			if _, err := h.svc.QueryMediaList(ctx, MediaListRequest{MediaType: "movie"}); err != nil {
				t.Errorf("concurrent QueryMediaList: %v", err)
				return
			}
			if _, err := h.svc.QuerySubtitles(ctx, QuerySubtitlesRequest{Path: videoPath}); err != nil {
				t.Errorf("concurrent QuerySubtitles: %v", err)
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}

// TestListMediaCards enumerates every card regardless of default flag.
func TestListMediaCards(t *testing.T) {
	h := setupSubtitleAgentHarness(t)
	ctx := context.Background()

	tvRoot := filepath.Join(filepath.Dir(h.archive), "tv")
	addCard(t, h, "tv-archive", tvRoot)

	cards, err := h.svc.ListMediaCards(ctx)
	if err != nil {
		t.Fatalf("ListMediaCards: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("expected 2 cards, got %d: %+v", len(cards), cards)
	}
	byName := map[string]MediaCardItem{}
	for _, c := range cards {
		byName[c.Name] = c
	}
	movies, ok := byName["movies"]
	if !ok {
		t.Fatalf("expected card 'movies', got %+v", cards)
	}
	if !movies.IsDefault {
		t.Errorf("expected 'movies' is_default=true, got %+v", movies)
	}
	if movies.ArchivePath != h.archive {
		t.Errorf("archive_path mismatch: %s != %s", movies.ArchivePath, h.archive)
	}
	if tv, ok := byName["tv-archive"]; !ok || tv.IsDefault {
		t.Errorf("expected non-default 'tv-archive' card, got %+v", cards)
	}
}

// TestAgentMultiCardScope: with media rows under two cards, no media_card_id
// (or explicit 0) scans all cards; a specific id scopes to that card only.
func TestAgentMultiCardScope(t *testing.T) {
	h := setupSubtitleAgentHarness(t)
	ctx := context.Background()

	tvRoot := filepath.Join(filepath.Dir(h.archive), "tv")
	cardB := addCard(t, h, "tv-archive", tvRoot)

	movieA := writeFile(t, filepath.Join(h.archive, "Alpha (2020) [1080p].mkv"), "video")
	addMovieRow(h, "Alpha (2020)", 2020, movieA)
	movieB := writeFile(t, filepath.Join(tvRoot, "Bravo (2021) [1080p].mkv"), "video")
	addMovieRow(h, "Bravo (2021)", 2021, movieB)

	listAll, err := h.svc.QueryMediaList(ctx, MediaListRequest{})
	if err != nil {
		t.Fatalf("QueryMediaList(all): %v", err)
	}
	if listAll.Total != 2 {
		t.Fatalf("expected 2 medias across all cards, got %d: %+v", listAll.Total, listAll.Items)
	}

	listZero, err := h.svc.QueryMediaList(ctx, MediaListRequest{MediaCardID: 0})
	if err != nil {
		t.Fatalf("QueryMediaList(0): %v", err)
	}
	if listZero.Total != 2 {
		t.Fatalf("expected media_card_id=0 to scan all cards, got %d", listZero.Total)
	}

	listCardB, err := h.svc.QueryMediaList(ctx, MediaListRequest{MediaCardID: cardB.ID})
	if err != nil {
		t.Fatalf("QueryMediaList(cardB): %v", err)
	}
	if listCardB.Total != 1 || len(listCardB.Items) != 1 || listCardB.Items[0].Path != movieB {
		t.Fatalf("expected only Bravo on card B, got %+v", listCardB.Items)
	}
}
