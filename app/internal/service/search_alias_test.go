package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/bujic-movie/bujic-movie/pkg/tmdb"
)

// aliasSite is a Cardigann stub that records the keyword it was asked for, so a
// test can assert which alias reached the site.
type aliasSite struct {
	mu      sync.Mutex
	queries []string
	server  *httptest.Server
}

func newAliasSite(t *testing.T) *aliasSite {
	t.Helper()
	site := &aliasSite{}
	site.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		keyword := r.Form.Get("search")
		site.mu.Lock()
		site.queries = append(site.queries, keyword)
		site.mu.Unlock()

		// Answer with the title the site was asked for so relevance filtering
		// keeps the batch.
		_, _ = w.Write([]byte(`<table class="torrents"><tbody>
<tr><td><a class="title">` + keyword + ` S01E01 1080p</a></td><td class="size">8 GB</td><td class="seeders">12</td><td><a class="download" href="/d/1.torrent" data-hash="h1">d</a></td></tr>
<tr><td><a class="title">` + keyword + ` S01E02 1080p</a></td><td class="size">9 GB</td><td class="seeders">8</td><td><a class="download" href="/d/2.torrent" data-hash="h2">d</a></td></tr>
</tbody></table>`))
	}))
	t.Cleanup(site.server.Close)
	return site
}

func (s *aliasSite) lastQuery() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queries) == 0 {
		return ""
	}
	return s.queries[len(s.queries)-1]
}

// createSiteWithLanguage registers the fake Cardigann definition with an explicit
// language so the search pipeline can route the alias.
func (e *testEnv) createSiteWithLanguage(t *testing.T, name, siteURL, language string) uint {
	t.Helper()
	raw := string(loadDefFile(t, "fake-cardigann.yml"))
	raw = strings.Replace(raw, "https://fake-tracker.example/", siteURL+"/", 1)
	id := "fake-tracker-" + name
	raw = strings.Replace(raw, "id: fake-tracker", "id: "+id, 1)
	raw = strings.Replace(raw, "language: zh-CN", "language: "+language, 1)
	if _, err := e.defs.Import(id+".yml", []byte(raw)); err != nil {
		t.Fatalf("import variant: %v", err)
	}
	view, err := e.indexers.Create(IndexerInput{
		Name: name, Connector: "cardigann", DefinitionID: id,
		Settings: map[string]string{"sort": "seeders"},
		Secrets:  map[string]string{"username": "bob", "password": "pw"},
	})
	if err != nil {
		t.Fatalf("create indexer %s: %v", name, err)
	}
	envDisableRateLimit(t, e.repo, view.ID)
	return view.ID
}

// A Chinese site keeps the localized title while an English site gets the latin
// alias, in one search pass.
func TestSearchRoutesKeywordBySiteLanguage(t *testing.T) {
	env := newTestEnv(t)
	site := newAliasSite(t)
	env.createSiteWithLanguage(t, "英文站", site.server.URL, "en-US")

	var hits int
	tmdbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/search/tv":
			_, _ = w.Write([]byte(`{"results":[{"id":77,"name":"达顿牧场","original_name":"Dutton Ranch","first_air_date":"2026-01-01","vote_average":7.5}]}`))
		case "/tv/77/alternative_titles":
			_, _ = w.Write([]byte(`{"results":[{"iso_3166_1":"US","title":"Dutton Ranch"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer tmdbServer.Close()

	env.cfg.Search.AliasResolution = true
	env.cfg.Search.AliasTimeoutMS = 3000
	titles := NewTitleAliasService(env.repo, tmdb.NewClient("k", tmdbServer.URL, "zh-CN"), env.cfg)
	env.search = NewSearchServiceWithTitles(env.repo, env.indexers, env.cfg, titles)

	result, err := env.search.Search(context.Background(), SearchQuery{Keyword: "达顿牧场", MediaType: "tv"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := site.lastQuery(); got != "Dutton Ranch" {
		t.Errorf("english site keyword = %q, want the latin alias", got)
	}
	if result.Title == nil || result.Title.Selected == nil {
		t.Fatalf("result must carry the resolution, got %+v", result.Title)
	}
	if !result.Title.NeedsConfirmation {
		t.Error("an automatic match must still ask for confirmation")
	}
	// The English release must survive relevance filtering even though the user
	// typed a Chinese title.
	if len(result.Items) == 0 {
		t.Error("latin alias results must not be dropped as irrelevant")
	}
	if hits == 0 {
		t.Error("TMDB must be consulted")
	}
}

// A confirmed choice is reused for later searches without another TMDB call.
func TestSearchReusesConfirmedTitle(t *testing.T) {
	env := newTestEnv(t)
	site := newAliasSite(t)
	env.createSiteWithLanguage(t, "英文站", site.server.URL, "en-US")

	var mu sync.Mutex
	calls := 0
	tmdbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		switch r.URL.Path {
		case "/search/tv":
			_, _ = w.Write([]byte(`{"results":[{"id":10,"name":"鱿鱼游戏","original_name":"오징어 게임","first_air_date":"2021-09-17","vote_average":8.1},{"id":11,"name":"鱿鱼游戏(美版)","original_name":"Squid Game: America","first_air_date":"2040-01-01","vote_average":2.0}]}`))
		case "/tv/10/alternative_titles":
			_, _ = w.Write([]byte(`{"results":[{"iso_3166_1":"US","title":"Squid Game"}]}`))
		case "/tv/11/alternative_titles":
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer tmdbServer.Close()

	env.cfg.Search.AliasResolution = true
	env.cfg.Search.AliasTimeoutMS = 3000
	titles := NewTitleAliasService(env.repo, tmdb.NewClient("k", tmdbServer.URL, "zh-CN"), env.cfg)
	env.search = NewSearchServiceWithTitles(env.repo, env.indexers, env.cfg, titles)

	if _, err := env.search.Search(context.Background(), SearchQuery{Keyword: "鱿鱼游戏", MediaType: "tv"}); err != nil {
		t.Fatalf("first search: %v", err)
	}
	if _, err := titles.Confirm(context.Background(), "鱿鱼游戏", "tv", 10); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	mu.Lock()
	before := calls
	mu.Unlock()

	result, err := env.search.Search(context.Background(), SearchQuery{Keyword: "鱿鱼游戏", MediaType: "tv"})
	if err != nil {
		t.Fatalf("second search: %v", err)
	}
	mu.Lock()
	after := calls
	mu.Unlock()
	if after != before {
		t.Errorf("a confirmed title must skip TMDB, calls %d -> %d", before, after)
	}
	if result.Title == nil || !result.Title.Confirmed {
		t.Errorf("resolution = %+v, want confirmed", result.Title)
	}
	if got := site.lastQuery(); got != "Squid Game" {
		t.Errorf("english site keyword = %q, want the confirmed latin alias", got)
	}
}

// Every site must report the keyword it was actually asked for, so the UI can
// show it as a subtitle instead of guessing.
func TestSearchReportsSiteQueries(t *testing.T) {
	env := newTestEnv(t)
	english := newAliasSite(t)
	env.createSiteWithLanguage(t, "英文站", english.server.URL, "en-US")

	env.cfg.Search.AliasResolution = true
	env.cfg.Search.AliasTimeoutMS = 3000
	resolution := &TitleResolution{
		Keyword: "鱿鱼游戏", MediaType: "tv", Confirmed: true,
		Selected: &TitleCandidate{
			TMDBID: 93405, MediaType: "tv", Title: "鱿鱼游戏",
			Aliases: []TitleAliasEntry{
				{Title: "鱿鱼游戏", Lang: "localized"},
				{Title: "오징어 게임", Lang: "original"},
				{Title: "Squid Game", Lang: "us"},
			},
		},
		Aliases: []string{"鱿鱼游戏", "오징어 게임", "Squid Game"},
	}

	result, err := env.search.Search(context.Background(), SearchQuery{
		Keyword: "鱿鱼游戏", MediaType: "tv",
		Aliases: resolution.Aliases, Confirmed: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.SiteQueries) == 0 {
		t.Fatal("site_queries must be reported")
	}
	for _, sq := range result.SiteQueries {
		if sq.IndexerID == 0 || sq.IndexerName == "" {
			t.Errorf("incomplete site query: %+v", sq)
		}
		if sq.Keyword == "" {
			t.Errorf("every site must report its keyword: %+v", sq)
		}
	}
	if got := keywordFor(resolution, "鱿鱼游戏", "en-US"); got != "Squid Game" {
		t.Errorf("english site keyword = %q", got)
	}
	if got := keywordFor(resolution, "鱿鱼游戏", "zh-CN"); got != "鱿鱼游戏" {
		t.Errorf("chinese site keyword = %q", got)
	}
}

// A confirmed choice stored in the cache must survive an incoming alias list,
// because the cache carries the language-tagged candidate that per-site routing
// needs.
func TestSearchUsesCachedConfirmedResolution(t *testing.T) {
	env := newTestEnv(t)
	site := newAliasSite(t)
	env.createSiteWithLanguage(t, "韩文站", site.server.URL, "ko-KR")

	env.cfg.Search.AliasResolution = true
	env.cfg.Search.AliasTimeoutMS = 3000
	tmdbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/tv":
			_, _ = w.Write([]byte(`{"results":[{"id":93405,"name":"鱿鱼游戏","original_name":"오징어 게임","first_air_date":"2021-09-17","vote_average":8.5,"vote_count":100}]}`))
		case "/search/movie":
			_, _ = w.Write([]byte(`{"results":[]}`))
		case "/tv/93405/alternative_titles":
			_, _ = w.Write([]byte(`{"results":[{"iso_3166_1":"US","title":"Squid Game"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer tmdbServer.Close()

	titles := NewTitleAliasService(env.repo, tmdb.NewClient("k", tmdbServer.URL, "zh-CN"), env.cfg)
	env.search = NewSearchServiceWithTitles(env.repo, env.indexers, env.cfg, titles)

	if _, err := titles.Resolve(context.Background(), "鱿鱼游戏", "tv", false); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := titles.Confirm(context.Background(), "鱿鱼游戏", "tv", 93405); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	// The UI sends its own alias list after confirming; the cache must still win
	// so the Korean site receives the Korean title.
	if _, err := env.search.Search(context.Background(), SearchQuery{
		Keyword: "鱿鱼游戏", MediaType: "tv",
		Aliases: []string{"鱿鱼游戏"}, Confirmed: true,
	}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := site.lastQuery(); got != "오징어 게임" {
		t.Errorf("korean site keyword = %q, want the Korean title", got)
	}
}

// "直接搜索" must not consult TMDB and must send the typed keyword verbatim to
// every site, regardless of each site's language.
func TestSearchSkipTitleUsesRawKeyword(t *testing.T) {
	env := newTestEnv(t)
	chinese := newAliasSite(t)
	english := newAliasSite(t)
	env.createSiteWithLanguage(t, "中文站", chinese.server.URL, "zh-CN")
	env.createSiteWithLanguage(t, "英文站", english.server.URL, "en-US")

	var hits int32
	tmdbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer tmdbServer.Close()

	env.cfg.Search.AliasResolution = true
	env.cfg.Search.AliasTimeoutMS = 3000
	titles := NewTitleAliasService(env.repo, tmdb.NewClient("k", tmdbServer.URL, "zh-CN"), env.cfg)
	env.search = NewSearchServiceWithTitles(env.repo, env.indexers, env.cfg, titles)

	result, err := env.search.Search(context.Background(), SearchQuery{
		Keyword: "鱿鱼游戏", SkipTitle: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Errorf("TMDB was queried %d times, want 0 for a direct search", got)
	}
	if result.Title != nil {
		t.Errorf("result must carry no resolution, got %+v", result.Title)
	}
	// Both sites get the raw Chinese keyword, even the English one.
	if got := chinese.lastQuery(); got != "鱿鱼游戏" {
		t.Errorf("chinese site keyword = %q", got)
	}
	if got := english.lastQuery(); got != "鱿鱼游戏" {
		t.Errorf("english site keyword = %q, want the raw keyword", got)
	}
}

// The default (no flag) keeps resolving, so existing callers are unaffected.
func TestSearchWithoutSkipTitleStillResolves(t *testing.T) {
	env := newTestEnv(t)
	site := newAliasSite(t)
	env.createSiteWithLanguage(t, "英文站", site.server.URL, "en-US")

	tmdbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/tv":
			_, _ = w.Write([]byte(`{"results":[{"id":93405,"name":"鱿鱼游戏","original_name":"오징어 게임","first_air_date":"2021-09-17","vote_count":100}]}`))
		case "/search/movie":
			_, _ = w.Write([]byte(`{"results":[]}`))
		case "/tv/93405/alternative_titles":
			_, _ = w.Write([]byte(`{"results":[{"iso_3166_1":"US","title":"Squid Game"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer tmdbServer.Close()

	env.cfg.Search.AliasResolution = true
	env.cfg.Search.AliasTimeoutMS = 3000
	titles := NewTitleAliasService(env.repo, tmdb.NewClient("k", tmdbServer.URL, "zh-CN"), env.cfg)
	env.search = NewSearchServiceWithTitles(env.repo, env.indexers, env.cfg, titles)

	if _, err := env.search.Search(context.Background(), SearchQuery{Keyword: "鱿鱼游戏", MediaType: "tv"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := site.lastQuery(); got != "Squid Game" {
		t.Errorf("english site keyword = %q, want the resolved alias", got)
	}
}

// Restricting the search to certain site languages must skip every other site
// before the fan-out, and support multiple languages at once.
func TestSearchFiltersBySiteLanguage(t *testing.T) {
	env := newTestEnv(t)
	en := newAliasSite(t)
	ko := newAliasSite(t)
	zh := newAliasSite(t)
	env.createSiteWithLanguage(t, "英文站", en.server.URL, "en-US")
	env.createSiteWithLanguage(t, "韩文站", ko.server.URL, "ko-KR")
	env.createSiteWithLanguage(t, "中文站", zh.server.URL, "zh-CN")

	// Single language: only the English site is queried.
	if _, err := env.search.Search(context.Background(), SearchQuery{
		Keyword: "yellowstone", SiteLanguages: []string{"en"},
	}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := en.lastQuery(); got == "" {
		t.Error("english site must be queried")
	}
	if got := ko.lastQuery(); got != "" {
		t.Errorf("korean site must be skipped, got %q", got)
	}
	if got := zh.lastQuery(); got != "" {
		t.Errorf("chinese site must be skipped, got %q", got)
	}
}

// Multi-select: several languages can be active at once.
func TestSearchFiltersByMultipleSiteLanguages(t *testing.T) {
	env := newTestEnv(t)
	en := newAliasSite(t)
	ko := newAliasSite(t)
	zh := newAliasSite(t)
	env.createSiteWithLanguage(t, "英文站", en.server.URL, "en-US")
	env.createSiteWithLanguage(t, "韩文站", ko.server.URL, "ko-KR")
	env.createSiteWithLanguage(t, "中文站", zh.server.URL, "zh-CN")

	if _, err := env.search.Search(context.Background(), SearchQuery{
		Keyword: "test", SiteLanguages: []string{"en", "ko"},
	}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if en.lastQuery() == "" {
		t.Error("english site must be queried")
	}
	if ko.lastQuery() == "" {
		t.Error("korean site must be queried")
	}
	if zh.lastQuery() != "" {
		t.Errorf("chinese site must be skipped, got %q", zh.lastQuery())
	}
}

// An empty language list keeps the original behaviour: every enabled site.
func TestSearchWithoutLanguageFilterQueriesAllSites(t *testing.T) {
	env := newTestEnv(t)
	en := newAliasSite(t)
	ko := newAliasSite(t)
	env.createSiteWithLanguage(t, "英文站", en.server.URL, "en-US")
	env.createSiteWithLanguage(t, "韩文站", ko.server.URL, "ko-KR")

	if _, err := env.search.Search(context.Background(), SearchQuery{Keyword: "test"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if en.lastQuery() == "" || ko.lastQuery() == "" {
		t.Errorf("both sites must be queried: en=%q ko=%q", en.lastQuery(), ko.lastQuery())
	}
}

// Region and script variants of one language group together, so picking "zh"
// covers both zh-CN and zh-TW sites.
func TestSearchLanguageGrouping(t *testing.T) {
	env := newTestEnv(t)
	cn := newAliasSite(t)
	tw := newAliasSite(t)
	env.createSiteWithLanguage(t, "简体站", cn.server.URL, "zh-CN")
	env.createSiteWithLanguage(t, "繁体站", tw.server.URL, "zh-TW")

	if _, err := env.search.Search(context.Background(), SearchQuery{
		Keyword: "test", SiteLanguages: []string{"zh"},
	}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if cn.lastQuery() == "" || tw.lastQuery() == "" {
		t.Errorf("both Chinese variants must be queried: cn=%q tw=%q", cn.lastQuery(), tw.lastQuery())
	}
}

// A site configured with several search languages must be queried once per
// language, in parallel, and its results merged. FileMood indexes both Chinese
// and English releases, so picking one language loses half the catalogue.
func TestSearchSiteWithMultipleLanguages(t *testing.T) {
	env := newTestEnv(t)
	site := newAliasSite(t)
	id := env.createSiteWithLanguage(t, "多语言站", site.server.URL, "en-US")

	// Give the site an explicit zh+en override.
	idx, err := env.repo.GetIndexer(id)
	if err != nil {
		t.Fatalf("reload indexer: %v", err)
	}
	idx.SearchLanguages = "zh,en"
	if err := env.repo.UpdateIndexer(idx); err != nil {
		t.Fatalf("update indexer: %v", err)
	}

	// Resolution with both a Chinese title and an English one.
	resolution := &TitleResolution{
		Keyword: "鱿鱼游戏", MediaType: "tv", Confirmed: true,
		Selected: &TitleCandidate{
			TMDBID: 93405, MediaType: "tv", Title: "鱿鱼游戏",
			Aliases: []TitleAliasEntry{
				{Title: "鱿鱼游戏", Lang: "localized"},
				{Title: "오징어 게임", Lang: "original"},
				{Title: "Squid Game", Lang: "us"},
			},
		},
		Aliases: []string{"鱿鱼游戏", "오징어 게임", "Squid Game"},
	}

	result, err := env.search.Search(context.Background(), SearchQuery{
		Keyword: "鱿鱼游戏", MediaType: "tv",
		Aliases: resolution.Aliases, Confirmed: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// The site must have been queried with two distinct keywords.
	site.mu.Lock()
	queries := append([]string(nil), site.queries...)
	site.mu.Unlock()
	distinct := map[string]bool{}
	for _, q := range queries {
		distinct[q] = true
	}
	if len(distinct) < 2 {
		t.Fatalf("site was queried with %v, want both languages", queries)
	}
	if !distinct["鱿鱼游戏"] {
		t.Errorf("missing the Chinese keyword: %v", queries)
	}
	if !distinct["Squid Game"] {
		t.Errorf("missing the English keyword: %v", queries)
	}

	// Both keywords must be reported so the UI can explain the fan-out.
	reported := map[string]bool{}
	for _, sq := range result.SiteQueries {
		reported[sq.Keyword] = true
	}
	if len(reported) < 2 {
		t.Errorf("site_queries = %v, want one entry per keyword", result.SiteQueries)
	}
}

// A single-language site keeps the previous behaviour: exactly one request.
func TestSearchSingleLanguageSiteSendsOneQuery(t *testing.T) {
	env := newTestEnv(t)
	site := newAliasSite(t)
	env.createSiteWithLanguage(t, "英文站", site.server.URL, "en-US")

	resolution := &TitleResolution{
		Keyword: "鱿鱼游戏", Confirmed: true,
		Selected: &TitleCandidate{
			TMDBID: 1, Title: "鱿鱼游戏",
			Aliases: []TitleAliasEntry{
				{Title: "鱿鱼游戏", Lang: "localized"},
				{Title: "Squid Game", Lang: "us"},
			},
		},
		Aliases: []string{"鱿鱼游戏", "Squid Game"},
	}
	if _, err := env.search.Search(context.Background(), SearchQuery{
		Keyword: "鱿鱼游戏", Aliases: resolution.Aliases, Confirmed: true,
	}); err != nil {
		t.Fatalf("Search: %v", err)
	}

	site.mu.Lock()
	count := len(site.queries)
	site.mu.Unlock()
	if count != 1 {
		t.Errorf("requests = %d, want 1 for a single-language site", count)
	}
}

// Language list parsing and normalisation.
func TestJoinAndSplitLanguages(t *testing.T) {
	if got := joinLanguages([]string{"ZH", "en", "zh", " ", "KO"}); got != "zh,en,ko" {
		t.Errorf("joinLanguages = %q", got)
	}
	if got := splitLanguages("zh,en,ko"); len(got) != 3 || got[0] != "zh" {
		t.Errorf("splitLanguages = %v", got)
	}
	if got := splitLanguages(""); got != nil {
		t.Errorf("empty = %v, want nil", got)
	}
	if got := splitLanguages(" en , ,zh "); len(got) != 2 || got[0] != "en" || got[1] != "zh" {
		t.Errorf("splitLanguages with blanks = %v", got)
	}
}
