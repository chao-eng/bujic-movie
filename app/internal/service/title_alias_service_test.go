package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/service/connector"
	"github.com/bujic-movie/bujic-movie/pkg/tmdb"
)

// newAliasEnv builds a resolver pointed at a stub TMDB server.
func newAliasEnv(t *testing.T, hits *int32) (*TitleAliasService, *testEnv) {
	t.Helper()
	env := newTestEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			atomic.AddInt32(hits, 1)
		}
		switch {
		case r.URL.Path == "/search/movie":
			_ = json.NewEncoder(w).Encode(tmdb.SearchMovieResponse{Results: []tmdb.SearchMovieResult{
				{ID: 1001, Title: "达顿牧场", OriginalTitle: "Dutton Ranch", ReleaseDate: "2026-01-01", VoteAverage: 7.2},
				{ID: 1002, Title: "达顿牧场（旧版）", OriginalTitle: "Old Ranch", ReleaseDate: "1999-01-01", VoteAverage: 5.1},
			}})
		case r.URL.Path == "/search/tv":
			_ = json.NewEncoder(w).Encode(tmdb.SearchTVResponse{Results: []tmdb.SearchTVResult{
				{ID: 2001, Name: "鱿鱼游戏", OriginalName: "오징어 게임", FirstAirDate: "2021-09-17", VoteAverage: 8.4},
			}})
		case r.URL.Path == "/movie/1001/alternative_titles":
			_ = json.NewEncoder(w).Encode(tmdb.AlternativeTitlesResponse{Titles: []tmdb.AlternativeTitle{
				{ISO3166_1: "US", Title: "Dutton Ranch"},
				{ISO3166_1: "KR", Title: "더튼 랜치"},
			}})
		case r.URL.Path == "/movie/1002/alternative_titles":
			_ = json.NewEncoder(w).Encode(tmdb.AlternativeTitlesResponse{})
		case r.URL.Path == "/tv/2001/alternative_titles":
			_ = json.NewEncoder(w).Encode(tmdb.TVAlternativeTitlesResponse{Results: []tmdb.AlternativeTitle{
				{ISO3166_1: "US", Title: "Squid Game"},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	cfg := env.cfg
	cfg.Search.AliasResolution = true
	cfg.Search.AliasTimeoutMS = 3000
	client := tmdb.NewClient("test", server.URL, "zh-CN")
	return NewTitleAliasService(env.repo, client, cfg), env
}

// Resolving a Chinese name must yield the original title plus alternatives, and
// pick the best-known match automatically.
func TestTitleAliasResolveMovie(t *testing.T) {
	svc, _ := newAliasEnv(t, nil)
	resolution, err := svc.Resolve(context.Background(), "达顿牧场", "movie", false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolution.Selected == nil || resolution.Selected.TMDBID != 1001 {
		t.Fatalf("selected = %+v", resolution.Selected)
	}
	if !contains(resolution.Aliases, "Dutton Ranch") {
		t.Errorf("aliases = %v, want the English title", resolution.Aliases)
	}
	if !contains(resolution.Aliases, "达顿牧场") {
		t.Errorf("aliases = %v, want the keyword kept", resolution.Aliases)
	}
	if resolution.Confirmed {
		t.Error("an automatic pick must not be marked confirmed")
	}
	if !resolution.NeedsConfirmation {
		t.Error("the candidate list must ask for confirmation")
	}
	if len(resolution.Candidates) != 2 {
		t.Errorf("candidates = %d, want 2", len(resolution.Candidates))
	}
}

// Korean shows resolve to the Korean original title, which is what Korean
// trackers index.
func TestTitleAliasResolveTVKorean(t *testing.T) {
	svc, _ := newAliasEnv(t, nil)
	resolution, err := svc.Resolve(context.Background(), "鱿鱼游戏", "tv", false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !contains(resolution.Aliases, "오징어 게임") {
		t.Errorf("aliases = %v, want the Korean title", resolution.Aliases)
	}
	if got := KeywordForLanguage(resolution, "鱿鱼游戏", "ko"); got != "오징어 게임" {
		t.Errorf("ko keyword = %q", got)
	}
	if got := KeywordForLanguage(resolution, "鱿鱼游戏", "en"); got != "Squid Game" {
		t.Errorf("en keyword = %q", got)
	}
	if got := KeywordForLanguage(resolution, "鱿鱼游戏", "zh-CN"); got != "鱿鱼游戏" {
		t.Errorf("zh keyword = %q", got)
	}
}

// A second identical lookup must come from the cache, not from TMDB.
func TestTitleAliasCachesLookup(t *testing.T) {
	var hits int32
	svc, _ := newAliasEnv(t, &hits)
	if _, err := svc.Resolve(context.Background(), "达顿牧场", "movie", false); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	first := atomic.LoadInt32(&hits)
	cached, err := svc.Resolve(context.Background(), "达顿牧场", "movie", false)
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if !cached.Cached {
		t.Error("second resolution must report a cache hit")
	}
	if atomic.LoadInt32(&hits) != first {
		t.Errorf("cache miss: %d -> %d TMDB calls", first, atomic.LoadInt32(&hits))
	}
}

// Confirming a candidate stores the choice, and later searches reuse it without
// asking TMDB again.
func TestTitleAliasConfirmSticks(t *testing.T) {
	var hits int32
	svc, env := newAliasEnv(t, &hits)
	if _, err := svc.Resolve(context.Background(), "达顿牧场", "movie", false); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	confirmed, err := svc.Confirm(context.Background(), "达顿牧场", "movie", 1002)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if !confirmed.Confirmed || confirmed.Selected == nil || confirmed.Selected.TMDBID != 1002 {
		t.Fatalf("confirm result = %+v", confirmed)
	}
	if !contains(confirmed.Aliases, "Old Ranch") {
		t.Errorf("aliases = %v, want the confirmed original title", confirmed.Aliases)
	}

	before := atomic.LoadInt32(&hits)
	cached := svc.Cached("达顿牧场", "movie")
	if cached == nil || !cached.Confirmed {
		t.Fatalf("cached = %+v", cached)
	}
	if atomic.LoadInt32(&hits) != before {
		t.Error("reading a confirmed choice must not call TMDB")
	}
	if env == nil {
		t.Fatal("env must be wired")
	}
}

// A TMDB outage degrades to the raw keyword instead of failing the search.
func TestTitleAliasDegradesOnFailure(t *testing.T) {
	env := newTestEnv(t)
	env.cfg.Search.AliasResolution = true
	env.cfg.Search.AliasTimeoutMS = 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	svc := NewTitleAliasService(env.repo, tmdb.NewClient("test", server.URL, "zh-CN"), env.cfg)
	resolution, err := svc.Resolve(context.Background(), "达顿牧场", "", false)
	if err != nil {
		t.Fatalf("Resolve must not fail the caller: %v", err)
	}
	if !resolution.Degraded {
		t.Error("resolution must be marked degraded")
	}
	if len(resolution.Aliases) != 1 || resolution.Aliases[0] != "达顿牧场" {
		t.Errorf("aliases = %v, want the raw keyword", resolution.Aliases)
	}
}

// A batch answering any resolved alias must survive relevance filtering.
func TestTitleAliasKeepsAliasBatch(t *testing.T) {
	keywords := []string{"达顿牧场", "Dutton Ranch"}
	if !connector.MatchesAnyKeyword("Dutton.Ranch.S01E01.1080p.WEB-DL", keywords) {
		t.Error("a latin alias must match the latin release")
	}
	if connector.MatchesAnyKeyword("SNOS-462-U 无码破解", keywords) {
		t.Error("unrelated releases must still be filtered")
	}
	kept, dropped := connector.DropIrrelevantBatchFor([]connector.Release{
		{Title: "Dutton.Ranch.S01E01.1080p"},
		{Title: "Dutton.Ranch.S01E02.1080p"},
		{Title: "Dutton.Ranch.S01E03.1080p"},
		{Title: "Dutton.Ranch.S01E04.1080p"},
		{Title: "Dutton.Ranch.S01E05.1080p"},
	}, keywords)
	if dropped || len(kept) != 5 {
		t.Fatalf("alias batch dropped=%v kept=%d", dropped, len(kept))
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if strings.TrimSpace(item) == want {
			return true
		}
	}
	return false
}

var _ = config.SearchConfig{}

// A forced re-resolution must discard every stored variant for the keyword, so a
// confirmation written under a pinned media type does not resurface.
func TestTitleAliasRefreshClearsStoredChoice(t *testing.T) {
	var hits int32
	svc, env := newAliasEnv(t, &hits)

	// The user confirmed a movie, which writes (keyword, "movie").
	if _, err := svc.Resolve(context.Background(), "达顿牧场", "movie", false); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := svc.Confirm(context.Background(), "达顿牧场", "movie", 1002); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if cached := svc.Cached("达顿牧场", "movie"); cached == nil || !cached.Confirmed {
		t.Fatalf("expected a confirmed cache entry, got %+v", cached)
	}

	// A plain re-search now asks for both media types and forces a refresh.
	resolution, err := svc.Resolve(context.Background(), "达顿牧场", "", true)
	if err != nil {
		t.Fatalf("forced Resolve: %v", err)
	}
	if resolution.Confirmed {
		t.Error("a forced refresh must not report the old confirmation")
	}
	if resolution.Selected == nil || resolution.Selected.TMDBID != 1001 {
		t.Errorf("selected = %+v, want the automatic pick", resolution.Selected)
	}

	// The stale (keyword, movie) row must be gone, otherwise later cached
	// lookups would resurrect the discarded choice.
	if _, err := env.repo.GetTitleAlias("达顿牧场", "movie"); err == nil {
		t.Error("the old media-typed cache row must be deleted")
	}
}

// Confirming a candidate whose media type has no cached row must not deadlock:
// the previous implementation called Resolve while holding the same per-keyword
// mutex, so the HTTP request hung until the client timed out.
func TestTitleAliasConfirmWithoutCacheDoesNotDeadlock(t *testing.T) {
	svc, _ := newAliasEnv(t, nil)

	// Resolve under the wildcard media type, so nothing exists for "movie".
	if _, err := svc.Resolve(context.Background(), "达顿牧场", "", false); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := svc.Confirm(context.Background(), "达顿牧场", "movie", 1002)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Confirm: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Confirm deadlocked: it must not re-acquire the per-keyword mutex")
	}
}

// Switching an already-confirmed selection to a different candidate must return
// promptly and store the new choice.
func TestTitleAliasSwitchingSelection(t *testing.T) {
	svc, _ := newAliasEnv(t, nil)
	if _, err := svc.Resolve(context.Background(), "达顿牧场", "movie", false); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	first, err := svc.Confirm(context.Background(), "达顿牧场", "movie", 1001)
	if err != nil {
		t.Fatalf("Confirm first: %v", err)
	}
	if first.Selected == nil || first.Selected.TMDBID != 1001 {
		t.Fatalf("first = %+v", first.Selected)
	}

	done := make(chan *TitleResolution, 1)
	go func() {
		second, _ := svc.Confirm(context.Background(), "达顿牧场", "movie", 1002)
		done <- second
	}()

	select {
	case second := <-done:
		if second == nil || second.Selected == nil || second.Selected.TMDBID != 1002 {
			t.Fatalf("second = %+v, want the switched candidate", second)
		}
		if !contains(second.Aliases, "Old Ranch") {
			t.Errorf("aliases = %v, want the new candidate's title", second.Aliases)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("switching a confirmed selection deadlocked")
	}
}
