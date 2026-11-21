package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TST-048 / BR-29: two concurrent searches against the same site never overlap;
// the second waits or is skipped, and the site sees a single in-flight request.
func TestTST048_SameSiteSingleInFlight(t *testing.T) {
	env := newTestEnv(t)
	var inFlight int32
	var maxInFlight int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			prev := atomic.LoadInt32(&maxInFlight)
			if cur <= prev || atomic.CompareAndSwapInt32(&maxInFlight, prev, cur) {
				break
			}
		}
		time.Sleep(120 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		_, _ = w.Write([]byte(`<table class="torrents"><tbody><tr>
			<td><a class="title">T</a><a class="download" href="/d/1">d</a></td>
			<td class="size">1 GB</td><td class="seeders">5</td><td class="cat" data-cat="2000">m</td>
		</tr></tbody></table>`))
	}))
	defer srv.Close()
	env.createSiteNamed(t, "唯一站点", srv.URL)
	// Re-enable a real interval so requests would overlap without the semaphore.
	idx, _ := env.repo.ListIndexers()
	idx[0].MinIntervalMS = 0
	_ = env.repo.UpdateIndexer(&idx[0])

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = env.search.Search(context.Background(), SearchQuery{Keyword: fmt.Sprintf("k%d", i), RequestID: fmt.Sprintf("r%d", i)})
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&maxInFlight); got > 1 {
		t.Fatalf("max in-flight per site = %d, want 1", got)
	}
}

// TST-046 / BR-28: the global search semaphore caps concurrent searches at the
// configured limit.
func TestTST046_GlobalSearchLimit(t *testing.T) {
	env := newTestEnv(t)
	env.cfg.Search.MaxGlobalSearches = 2
	env.search = NewSearchService(env.repo, env.indexers, env.cfg)

	var inFlight int32
	var maxInFlight int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			prev := atomic.LoadInt32(&maxInFlight)
			if cur <= prev || atomic.CompareAndSwapInt32(&maxInFlight, prev, cur) {
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		_, _ = w.Write([]byte(`<table class="torrents"><tbody></tbody></table>`))
	}))
	defer srv.Close()
	env.createSiteNamed(t, "站点", srv.URL)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = env.search.Search(context.Background(), SearchQuery{Keyword: "k", RequestID: fmt.Sprintf("g%d", i)})
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&maxInFlight); got > 2 {
		t.Fatalf("max concurrent searches = %d, want <= 2", got)
	}
}

// TST-047 / BR-28: one search never runs more than 20 site workers at once.
func TestTST047_SiteWorkerLimit(t *testing.T) {
	env := newTestEnv(t)
	env.cfg.Search.MaxSiteWorkers = 20
	env.search = NewSearchService(env.repo, env.indexers, env.cfg)

	var inFlight int32
	var maxInFlight int32
	var mu sync.Mutex
	stats := map[string]bool{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			prev := atomic.LoadInt32(&maxInFlight)
			if cur <= prev || atomic.CompareAndSwapInt32(&maxInFlight, prev, cur) {
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		_, _ = w.Write([]byte(`<table class="torrents"><tbody></tbody></table>`))
	}))
	defer srv.Close()

	// 25 sites share one stub but use distinct paths so they are distinct rate keys.
	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("worker-site-%d", i)
		mu.Lock()
		if !stats[id] {
			stats[id] = true
			importWorkerDefinition(t, env, id, srv.URL, fmt.Sprintf("/s%d", i))
		}
		mu.Unlock()
		if _, err := env.indexers.Create(IndexerInput{
			Name: id, Connector: "cardigann", DefinitionID: id,
		}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	if _, err := env.search.Search(context.Background(), SearchQuery{Keyword: "k"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := atomic.LoadInt32(&maxInFlight); got > 20 {
		t.Fatalf("max site workers = %d, want <= 20", got)
	}
}

// importWorkerDefinition stores a definition pointing at one stub path.
func importWorkerDefinition(t *testing.T, env *testEnv, id, baseURL, path string) {
	t.Helper()
	content := fmt.Sprintf(`id: %s
name: %s
type: public
links:
  - %s%s/
caps:
  modes:
    search: [q]
search:
  path: /
  rows:
    selector: table.torrents > tbody > tr
  fields:
    title:
      selector: a.title
    download:
      selector: a.download
      attribute: href
`, id, id, baseURL, path)
	summary, err := env.defs.Import(id+".yml", []byte(content))
	if err != nil || summary.Added+summary.Updated != 1 {
		t.Fatalf("import worker definition %s: %v %+v", id, err, summary)
	}
}
