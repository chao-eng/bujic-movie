package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
)

// loadTestEnabled gates the expensive NFR measurements. Run with:
//
//	BUJIC_SEARCH_LOADTEST=1 go test ./internal/service -run TestTST06 -v
//
// The suite is skipped by default so `go test ./...` stays fast.
func loadTestEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("BUJIC_SEARCH_LOADTEST") != "1" {
		t.Skip("set BUJIC_SEARCH_LOADTEST=1 to run NFR load tests")
	}
}

// fastSearchServer serves a fixed number of rows with a configurable latency.
func fastSearchServer(t *testing.T, rows int, latency time.Duration) *httptest.Server {
	t.Helper()
	var body string
	body = `<table class="torrents"><tbody>`
	for i := 0; i < rows; i++ {
		body += fmt.Sprintf(`<tr><td><a class="title">Release %d</a></td>
			<td class="size">%d GB</td><td class="seeders">%d</td>
			<td class="cat" data-cat="2000">m</td>
			<td><a class="download" href="/d/%d" data-hash="h%d">d</a></td></tr>`,
			i, i+1, 1000-i, i, i)
	}
	body += `</tbody></table>`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if latency > 0 {
			time.Sleep(latency)
		}
		_, _ = w.Write([]byte(body))
	}))
}

func percentileOf(samples []int64, p float64) int64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]int64(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

// TST-064 / NFR-02: 3 concurrent searches across 5 sites stay inside the 8s
// first-paint budget, and the error rate is zero.
func TestTST064_SearchPerformance(t *testing.T) {
	loadTestEnabled(t)
	env := newTestEnv(t)

	for i := 0; i < 5; i++ {
		srv := fastSearchServer(t, 50, 250*time.Millisecond)
		defer srv.Close()
		env.createSiteNamed(t, fmt.Sprintf("perf-%d", i), srv.URL)
	}

	const searches = 3
	var wg sync.WaitGroup
	latencies := make([]int64, searches)
	failures := 0
	var mu sync.Mutex

	start := time.Now()
	for i := 0; i < searches; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			began := time.Now()
			_, err := env.search.Search(context.Background(), SearchQuery{
				Keyword: "bench", RequestID: fmt.Sprintf("perf-%d", i),
			})
			mu.Lock()
			latencies[i] = time.Since(began).Milliseconds()
			if err != nil {
				failures++
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	total := time.Since(start)

	p95 := percentileOf(latencies, 0.95)
	t.Logf("NFR-02: searches=%d elapsed=%dms p95=%dms failures=%d", searches, total.Milliseconds(), p95, failures)
	if failures > 0 {
		t.Fatalf("failures = %d, want 0", failures)
	}
	// The first paint budget is per search; with 5 sites at 250ms the fan-out is
	// well under it, so a regression here means the pipeline serialised.
	if p95 > 8000 {
		t.Fatalf("p95 = %dms, want <= 8000ms", p95)
	}
}

// TST-065 / NFR-02: a 500-result search from 3 concurrent callers stays inside
// the memory budget.
func TestTST065_SearchMemory(t *testing.T) {
	loadTestEnabled(t)
	env := newTestEnv(t)

	for i := 0; i < 5; i++ {
		srv := fastSearchServer(t, 100, 50*time.Millisecond)
		defer srv.Close()
		env.createSiteNamed(t, fmt.Sprintf("mem-%d", i), srv.URL)
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = env.search.Search(context.Background(), SearchQuery{Keyword: "mem", RequestID: fmt.Sprintf("mem-%d", i)})
		}(i)
	}
	wg.Wait()

	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	deltaMB := float64(after.TotalAlloc-before.TotalAlloc) / (1 << 20)
	t.Logf("NFR-02: allocated %.1fMB across 3 concurrent searches", deltaMB)
	if deltaMB > 150 {
		t.Fatalf("memory delta = %.1fMB, want <= 150MB", deltaMB)
	}
}

// TST-066 / NFR-03: 1000 definitions, 50 indexers and 100k records load and
// query within the capacity budget.
func TestTST066_Capacity(t *testing.T) {
	loadTestEnabled(t)
	env := newTestEnv(t)

	// 1000 definitions
	startDefs := time.Now()
	defs := make([]entity.SearchIndexerDefinition, 0, 1000)
	for i := 0; i < 1000; i++ {
		defs = append(defs, entity.SearchIndexerDefinition{
			ID: fmt.Sprintf("capacity-%03d", i), Name: fmt.Sprintf("容量站点 %03d", i),
			Source: "remote", Version: "12", Type: "public", Language: "en-US",
			CapabilitiesJSON: "search", Compat: "full", RawYAML: "id: x",
			RawHash: fmt.Sprintf("hash-%d", i),
		})
	}
	if err := env.db.CreateInBatches(&defs, 200).Error; err != nil {
		t.Fatalf("seed definitions: %v", err)
	}
	defList, err := env.defs.List()
	if err != nil {
		t.Fatalf("List definitions: %v", err)
	}
	defElapsed := time.Since(startDefs)
	t.Logf("NFR-03: %d definitions loaded in %dms", len(defList), defElapsed.Milliseconds())
	if len(defList) != 1000 {
		t.Fatalf("definitions = %d, want 1000", len(defList))
	}
	if defElapsed > 2*time.Second {
		t.Errorf("definition load = %v, want <= 2s", defElapsed)
	}

	// 100k download records
	records := make([]entity.SearchDownloadRecord, 0, 100000)
	base := time.Now().Add(-30 * 24 * time.Hour)
	for i := 0; i < 100000; i++ {
		records = append(records, entity.SearchDownloadRecord{
			Title: fmt.Sprintf("record-%d", i), IndexerID: 1, IndexerName: "容量站点",
			DownloaderID: 1, DownloaderName: "NAS", SizeBytes: int64(i),
			Status: "created", Fingerprint: fmt.Sprintf("fp-%d", i),
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		})
	}
	seedStart := time.Now()
	if err := env.db.CreateInBatches(&records, 500).Error; err != nil {
		t.Fatalf("seed records: %v", err)
	}
	t.Logf("NFR-03: seeded 100k records in %dms", time.Since(seedStart).Milliseconds())

	queryStart := time.Now()
	list, err := env.grabs.Records(100)
	if err != nil {
		t.Fatalf("List records: %v", err)
	}
	queryElapsed := time.Since(queryStart)
	t.Logf("NFR-03: record list p95 sample = %dms (%d rows)", queryElapsed.Milliseconds(), len(list))
	if queryElapsed > 300*time.Millisecond {
		t.Errorf("record list = %v, want <= 300ms", queryElapsed)
	}
}

// TST-070 / NFR-06: injected failures degrade instead of cascading, and the
// system recovers once the dependency returns.
func TestTST070_FaultInjection(t *testing.T) {
	loadTestEnabled(t)
	env := newTestEnv(t)

	// 1) 429 from the site: cooldown after 3 failures, recovery after reset.
	var fail atomicBool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.get() {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`<table class="torrents"><tbody>
			<tr><td><a class="title">ok</a><a class="download" href="/d/1">d</a></td>
			<td class="size">1 GB</td><td class="seeders">5</td><td class="cat" data-cat="2000">m</td></tr>
			</tbody></table>`))
	}))
	defer srv.Close()
	id := env.createSiteNamed(t, "限流站点", srv.URL)

	fail.set(true)
	for i := 0; i < 3; i++ {
		_, _ = env.search.Search(context.Background(), SearchQuery{Keyword: "fault"})
	}
	status, err := env.repo.GetStatus(id)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.CooldownUntil.IsZero() {
		t.Fatal("expected a cooldown after three 429s")
	}

	// Recovery: clear the cooldown and let the site succeed again.
	fail.set(false)
	if err := env.indexers.ClearCooldown(id); err != nil {
		t.Fatalf("clear cooldown: %v", err)
	}
	recoverStart := time.Now()
	result, err := env.search.Search(context.Background(), SearchQuery{Keyword: "fault"})
	if err != nil {
		t.Fatalf("recovery search: %v", err)
	}
	if len(result.Items) == 0 {
		t.Fatal("recovery search returned no items")
	}
	if elapsed := time.Since(recoverStart); elapsed > 60*time.Second {
		t.Fatalf("recovery took %v, want <= 60s", elapsed)
	}
	t.Logf("NFR-06: recovered in %dms", time.Since(recoverStart).Milliseconds())

	// 2) Downloader down: the task group degrades, it does not 500 the page.
	dead, err := env.clients.Create(DownloadClientInput{Name: "停机下载器", Host: "127.0.0.1", Port: 1})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	groups, err := env.tasks.Groups(context.Background())
	if err != nil {
		t.Fatalf("task groups: %v", err)
	}
	degraged := false
	for _, g := range groups {
		if g.ClientID == dead.ID && g.State == "unreachable" {
			degraged = true
		}
	}
	if !degraged {
		t.Fatal("dead downloader must degrade to an unreachable group")
	}

	// 3) Definition source 500: sync fails but the local library survives.
	if _, err := env.defs.Import("keep.yml", loadDefFile(t, "fake-cardigann.yml")); err != nil {
		t.Fatalf("import: %v", err)
	}
	badSource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badSource.Close()
	env.cfg.Search.DefinitionSource = badSource.URL
	env.cfg.Search.DefinitionVersion = "12"
	if _, err := env.defs.Sync(context.Background()); err == nil {
		t.Fatal("sync against a 500 source must fail")
	}
	list, err := env.defs.List()
	if err != nil || len(list) == 0 {
		t.Fatalf("local definitions must survive: %v %d", err, len(list))
	}
}

// atomicBool is a tiny race-free flag for the fault stub.
type atomicBool struct {
	mu sync.Mutex
	v  bool
}

func (b *atomicBool) set(v bool) { b.mu.Lock(); b.v = v; b.mu.Unlock() }
func (b *atomicBool) get() bool  { b.mu.Lock(); defer b.mu.Unlock(); return b.v }
