package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bujic-movie/bujic-movie/pkg/tmdb"
)

// stubTMDB mirrors the real shape that broke English input: TMDB ranks a
// spin-off first and the original show only appears further down, without a
// latin title until its alternatives are read.
func stubTMDB(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/tv":
			_ = json.NewEncoder(w).Encode(tmdb.SearchTVResponse{Results: []tmdb.SearchTVResult{
				{ID: 204082, Name: "鱿鱼游戏：真人挑战赛", OriginalName: "Squid Game: The Challenge", FirstAirDate: "2023-11-22", VoteCount: 655, Popularity: 20},
				{ID: 321480, Name: "Squid Game Podcast", OriginalName: "Squid Game Podcast", FirstAirDate: "2024-01-01", VoteCount: 0, Popularity: 3},
				{ID: 93405, Name: "鱿鱼游戏", OriginalName: "오징어 게임", FirstAirDate: "2021-09-17", VoteCount: 17963, Popularity: 300},
			}})
		case "/search/movie":
			_ = json.NewEncoder(w).Encode(tmdb.SearchMovieResponse{Results: []tmdb.SearchMovieResult{}})
		case "/tv/204082/alternative_titles":
			_ = json.NewEncoder(w).Encode(tmdb.TVAlternativeTitlesResponse{})
		case "/tv/321480/alternative_titles":
			_ = json.NewEncoder(w).Encode(tmdb.TVAlternativeTitlesResponse{})
		case "/tv/93405/alternative_titles":
			_ = json.NewEncoder(w).Encode(tmdb.TVAlternativeTitlesResponse{Results: []tmdb.AlternativeTitle{
				{ISO3166_1: "US", Title: "Squid Game"},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// Typing the English name must resolve to the show whose latin title matches,
// not to a spin-off that merely starts with the same words.
func TestTitleAliasResolvesEnglishInputToOriginal(t *testing.T) {
	env := newTestEnv(t)
	env.cfg.Search.AliasResolution = true
	env.cfg.Search.AliasTimeoutMS = 3000
	server := stubTMDB(t)
	svc := NewTitleAliasService(env.repo, tmdb.NewClient("k", server.URL, "zh-CN"), env.cfg)

	resolution, err := svc.Resolve(context.Background(), "Squid Game", "", false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolution.Selected == nil || resolution.Selected.TMDBID != 93405 {
		t.Fatalf("selected = %+v, want the original show", resolution.Selected)
	}
	// A latin keyword must be sent as typed, not replaced by another market title.
	if got := KeywordForLanguage(resolution, "Squid Game", "en-US"); got != "Squid Game" {
		t.Errorf("english keyword = %q, want the typed keyword", got)
	}
}

// A low-ranked candidate must still get its regional titles read, otherwise the
// true match never gains the alias that makes it win.
func TestTitleAliasEnrichesLowRankedCandidate(t *testing.T) {
	env := newTestEnv(t)
	env.cfg.Search.AliasResolution = true
	env.cfg.Search.AliasTimeoutMS = 3000
	server := stubTMDB(t)
	svc := NewTitleAliasService(env.repo, tmdb.NewClient("k", server.URL, "zh-CN"), env.cfg)

	resolution, err := svc.Resolve(context.Background(), "鱿鱼游戏", "tv", false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var original *TitleCandidate
	for i := range resolution.Candidates {
		if resolution.Candidates[i].TMDBID == 93405 {
			original = &resolution.Candidates[i]
			break
		}
	}
	if original == nil {
		t.Fatalf("the original must be in the list: %+v", resolution.Candidates)
	}
	if !hasLatinAlias(original.Aliases) {
		t.Errorf("aliases = %+v, want the latin title from alternatives", original.Aliases)
	}
	if got := KeywordForLanguage(resolution, "鱿鱼游戏", "en"); got != "Squid Game" {
		t.Errorf("english keyword = %q", got)
	}
}
