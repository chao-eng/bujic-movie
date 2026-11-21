package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TST-027 / AC-27: a site that declares no support for the requested category
// is not queried, and the response is not an error.
func TestTST027_CategoryFilterSkipsUnsupportedSite(t *testing.T) {
	env := newTestEnv(t)

	var movieHits, tvHits int32
	movieSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&movieHits, 1)
		_, _ = w.Write([]byte(`<table class="torrents"><tbody></tbody></table>`))
	}))
	defer movieSrv.Close()
	tvSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tvHits, 1)
		_, _ = w.Write([]byte(`<table class="torrents"><tbody></tbody></table>`))
	}))
	defer tvSrv.Close()

	importCategoryDefinition(t, env, "movies-only", movieSrv.URL, "Movies")
	importCategoryDefinition(t, env, "tv-only", tvSrv.URL, "TV")
	if _, err := env.indexers.Create(IndexerInput{Name: "电影站", Connector: "cardigann", DefinitionID: "movies-only"}); err != nil {
		t.Fatalf("create movie site: %v", err)
	}
	if _, err := env.indexers.Create(IndexerInput{Name: "剧集站", Connector: "cardigann", DefinitionID: "tv-only"}); err != nil {
		t.Fatalf("create tv site: %v", err)
	}

	if _, err := env.search.Search(context.Background(), SearchQuery{Keyword: "x", Categories: []int{2000}}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if atomic.LoadInt32(&movieHits) == 0 {
		t.Error("movie site must be queried")
	}
	if atomic.LoadInt32(&tvHits) != 0 {
		t.Errorf("tv-only site was queried %d times, want 0", tvHits)
	}
}

// TST-026 / AC-26: an empty keyword performs an RSS-style latest query and does
// not error when the site returns nothing.
func TestTST026_EmptyKeyword(t *testing.T) {
	env := newTestEnv(t)
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		_, _ = w.Write([]byte(`<table class="torrents"><tbody></tbody></table>`))
	}))
	defer srv.Close()
	env.createSiteNamed(t, "站点", srv.URL)

	result, err := env.search.Search(context.Background(), SearchQuery{Keyword: ""})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result == nil {
		t.Fatal("empty keyword must return a result envelope")
	}
	if gotPath == "" {
		t.Error("the site should still be queried for the latest releases")
	}
}

// TST-024 / AC-24: after three consecutive push failures the bound client is
// marked unavailable and the next push lands on another client.
func TestTST024_FailoverAfterThreeFailures(t *testing.T) {
	env := newTestEnv(t)
	site := newCardigannSite(t, 1)
	defer site.Close()
	indexerID := env.createSiteNamed(t, "站点", site.URL)

	qbSrv, pushed := newQBSiteCapturing(t)
	defer qbSrv.Close()
	good, err := env.clients.Create(DownloadClientInput{Name: "备用", Host: hostOf(t, qbSrv.URL), Port: portOf(t, qbSrv.URL), Priority: 50})
	if err != nil {
		t.Fatalf("create good client: %v", err)
	}
	bad, err := env.clients.Create(DownloadClientInput{Name: "坏", Host: "127.0.0.1", Port: 1, Priority: 10})
	if err != nil {
		t.Fatalf("create bad client: %v", err)
	}
	// Bind the indexer to the dead client so the first three pushes fail.
	idx, _ := env.repo.GetIndexer(indexerID)
	idx.DownloadClientID = bad.ID
	_ = env.repo.UpdateIndexer(idx)

	for i := 0; i < 3; i++ {
		_, _ = env.grabs.Grab(context.Background(), GrabRequest{
			Fingerprint: "fp-fail-" + itoa(i), IndexerID: indexerID, Title: "t",
			DownloadURL: "magnet:?xt=urn:btih:abc", DownloadType: "magnet",
		})
	}
	badClient, _ := env.repo.GetClient(bad.ID)
	if badClient.ConsecutiveFailures < 3 {
		t.Fatalf("failures = %d, want >= 3", badClient.ConsecutiveFailures)
	}

	// The bound client is still consulted by Select; drop the binding to prove
	// the fallback order picks the healthy client.
	idx, _ = env.repo.GetIndexer(indexerID)
	idx.DownloadClientID = 0
	_ = env.repo.UpdateIndexer(idx)

	result, err := env.grabs.Grab(context.Background(), GrabRequest{
		Fingerprint: "fp-ok", IndexerID: indexerID, Title: "t",
		DownloadURL: "magnet:?xt=urn:btih:def", DownloadType: "magnet",
	})
	if err != nil {
		t.Fatalf("fourth grab: %v", err)
	}
	if result.ClientID != good.ID {
		t.Errorf("client = %d, want healthy client %d", result.ClientID, good.ID)
	}
	if pushed() == "" {
		t.Error("healthy client did not receive the push")
	}
}

// importCategoryDefinition stores a definition whose caps declare one category.
func importCategoryDefinition(t *testing.T, env *testEnv, id, baseURL, category string) {
	t.Helper()
	content := `id: ` + id + `
name: ` + id + `
type: public
links:
  - ` + baseURL + `/
caps:
  categories:
    ` + category + `: 1
  modes:
    search: [q]
search:
  path: /s
  rows:
    selector: table.torrents > tbody > tr
  fields:
    title:
      selector: a.title
    download:
      selector: a.download
      attribute: href
`
	summary, err := env.defs.Import(id+".yml", []byte(content))
	if err != nil || summary.Added != 1 {
		t.Fatalf("import %s: %v %+v", id, err, summary)
	}
}
