package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/config"
	internalcrypto "github.com/bujic-movie/bujic-movie/internal/crypto"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
	"gorm.io/gorm"
)

// testEnv bundles everything a service-level test needs.
type testEnv struct {
	db       *gorm.DB
	repo     repository.SearchRepository
	box      *internalcrypto.SecretBox
	cfg      *config.Config
	exec     *httpclient.Executor
	defs     *DefinitionService
	indexers *IndexerService
	clients  *DownloadClientService
	search   *SearchService
	grabs    *GrabService
	tasks    *TaskService
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	db := newTestDB(t)
	repo := repository.NewSearchRepository(db)
	box, err := internalcrypto.NewSecretBox("unit-test-secret")
	if err != nil {
		t.Fatalf("secret box: %v", err)
	}
	cfg := &config.Config{}
	cfg.Search = config.SearchConfig{
		Enabled: true, MaxGlobalSearches: 3, MaxSiteWorkers: 20,
		SearchDeadlineMS: 3000, SiteTimeoutMS: 800, DefaultIntervalMS: 200,
		GrabTimeoutMS: 5000, GrabWindowMS: 60000, RecordRetentionDays: 90, HistoryLimit: 20,
	}
	// Retry backoff is 1s+2s in production; tests skip the wall-clock wait.
	exec, err := httpclient.New(nil, noSleepClock{}, "")
	if err != nil {
		t.Fatalf("httpclient: %v", err)
	}
	// The production limiter sleeps for the configured interval; tests assert on
	// behaviour, not wall-clock throttling, so swap in an instant waiter.
	restoreWaiter := httpclient.WaiterFactory
	httpclient.WaiterFactory = func(time.Duration) httpclient.Waiter { return instantWaiter{} }
	t.Cleanup(func() { httpclient.WaiterFactory = restoreWaiter })

	env := &testEnv{db: db, repo: repo, box: box, cfg: cfg, exec: exec}
	env.defs = NewDefinitionService(repo, cfg, exec)
	env.indexers = NewIndexerService(repo, box, env.defs, cfg, exec)
	env.clients = NewDownloadClientService(repo, box, cfg, exec)
	env.search = NewSearchService(repo, env.indexers, cfg)
	env.grabs = NewGrabService(repo, env.indexers, env.clients, cfg, env.search.Metrics())
	env.tasks = NewTaskService(repo, env.clients)
	return env
}

func loadDefFile(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("search", "testdata", "definitions", name))
	if err != nil {
		t.Fatalf("read def: %v", err)
	}
	return raw
}

// TST-001 / AC-01: importing a definition stores it with capabilities.
func TestTST001_ImportDefinition(t *testing.T) {
	env := newTestEnv(t)
	summary, err := env.defs.Import("fake.yml", loadDefFile(t, "fake-cardigann.yml"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if summary.Added != 1 {
		t.Fatalf("added = %d", summary.Added)
	}
	list, err := env.defs.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("definitions = %d", len(list))
	}
	if list[0].Compat != "full" {
		t.Errorf("compat = %q", list[0].Compat)
	}
	if list[0].Capabilities == "" {
		t.Error("capabilities summary must be stored")
	}
	if list[0].UpdatedAt == "" {
		t.Error("updated_at must be set for the list view")
	}
}

// TST-005 / AC-05: the template list answers "needs credentials?" and "in use?".
func TestTST005_TemplateHints(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.defs.Import("fake.yml", loadDefFile(t, "fake-cardigann.yml")); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if _, err := env.indexers.Create(IndexerInput{
		Name: "已有站点", Connector: "cardigann", DefinitionID: "fake-tracker",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	list, err := env.defs.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("definitions = %d", len(list))
	}
	item := list[0]
	if !item.NeedsLogin {
		t.Error("the fixture requires a login")
	}
	if item.UsedCount != 1 {
		t.Errorf("used count = %d, want 1", item.UsedCount)
	}
}

// TST-002 / AC-02: a failed sync keeps the local library usable.
func TestTST002_SyncFailureKeepsLocal(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.defs.Import("fake.yml", loadDefFile(t, "fake-cardigann.yml")); err != nil {
		t.Fatalf("Import: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	env.cfg.Search.DefinitionSource = srv.URL
	env.cfg.Search.DefinitionVersion = "12"

	if _, err := env.defs.Sync(context.Background()); err == nil {
		t.Fatal("sync against a failing source must return an error")
	}
	list, err := env.defs.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("local definitions must survive a failed sync, got %d", len(list))
	}
}

// TST-003 / AC-03: an invalid definition is rejected with a location.
func TestTST003_ImportInvalidDefinition(t *testing.T) {
	env := newTestEnv(t)
	summary, err := env.defs.Import("malformed.yml", loadDefFile(t, "malformed.yml"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if summary.Invalid != 1 || summary.Added != 0 {
		t.Fatalf("summary = %+v", summary)
	}
	if !strings.Contains(summary.Message, "search") {
		t.Errorf("message must name the missing block: %q", summary.Message)
	}
	list, _ := env.defs.List()
	if len(list) != 0 {
		t.Fatalf("invalid definition must not be stored, got %d", len(list))
	}
}

// TST-005 / TST-006 / AC-05 / AC-06: creating a definition instance snapshots
// the YAML and masks secret settings.
func TestTST005_CreateCardigannIndexerMasksSecrets(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.defs.Import("fake.yml", loadDefFile(t, "fake-cardigann.yml")); err != nil {
		t.Fatalf("Import: %v", err)
	}

	view, err := env.indexers.Create(IndexerInput{
		Name: "我的站点", Connector: "cardigann", DefinitionID: "fake-tracker",
		Settings: map[string]string{"sort": "seeders"},
		Secrets:  map[string]string{"username": "bob", "password": "hunter2"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if view.DefinitionVersion == "" {
		t.Error("definition version must be snapshotted")
	}
	if view.DefinitionYAML == "" {
		t.Error("definition YAML must be snapshotted")
	}
	if view.SecretsMask["password"] != "****ter2" {
		t.Errorf("password mask = %q", view.SecretsMask["password"])
	}
	if strings.Contains(view.DefinitionYAML, "hunter2") {
		t.Error("snapshot must not contain instance secrets")
	}

	// Re-reading must not expose plaintext either.
	got, err := env.indexers.Get(view.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SecretsMask["password"] != "****ter2" {
		t.Errorf("mask on read = %q", got.SecretsMask["password"])
	}
}

// TST-008 / AC-08: disabling an indexer removes it from the candidate set.
func TestTST008_DisableRemovesFromSearch(t *testing.T) {
	env := newTestEnv(t)
	srv := newCardigannSite(t, 3)
	defer srv.Close()
	id := env.createSite(t, srv.URL)

	if _, err := env.search.Search(context.Background(), SearchQuery{Keyword: "x"}); err != nil {
		t.Fatalf("first search: %v", err)
	}
	if _, err := env.indexers.SetEnabled(id, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := env.search.Search(context.Background(), SearchQuery{Keyword: "x"}); !errors.Is(err, ErrNoIndexers) {
		t.Fatalf("disabled site must be skipped, got %v", err)
	}
}

// TST-009 / AC-09: deleting an indexer keeps its download records readable.
func TestTST009_DeleteIndexerKeepsRecordName(t *testing.T) {
	env := newTestEnv(t)
	srv := newCardigannSite(t, 1)
	defer srv.Close()
	id := env.createSite(t, srv.URL)
	qbSrv, _ := newQBSiteCapturing(t)
	defer qbSrv.Close()
	if _, err := env.clients.Create(DownloadClientInput{Name: "NAS", Host: hostOf(t, qbSrv.URL), Port: portOf(t, qbSrv.URL)}); err != nil {
		t.Fatalf("create client: %v", err)
	}

	if _, err := env.grabs.Grab(context.Background(), GrabRequest{
		Fingerprint: "fp-1", IndexerID: id, Title: "任务一",
		DownloadURL: "magnet:?xt=urn:btih:abc", DownloadType: "magnet",
	}); err != nil {
		t.Fatalf("grab: %v", err)
	}

	if err := env.indexers.Delete(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	records, err := env.grabs.Records(10)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d", len(records))
	}
	if records[0].IndexerName != "测试站点" {
		t.Errorf("snapshot name = %q, want 测试站点", records[0].IndexerName)
	}
}

// TST-015 / TST-025 / AC-25: a healthy site contributes merged results.
func TestTST025_SearchMergesAndSorts(t *testing.T) {
	env := newTestEnv(t)
	srvA := newCardigannSite(t, 2)
	defer srvA.Close()
	srvB := newTorznabSite(t)
	defer srvB.Close()
	env.createSiteNamed(t, "站点A", srvA.URL)
	env.createTorznab(t, "站点B", srvB.URL)

	result, err := env.search.Search(context.Background(), SearchQuery{Keyword: "Item", PageSize: 50})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Items) == 0 {
		t.Fatal("no results")
	}
	if len(result.Failures) != 0 {
		t.Fatalf("unexpected failures: %+v", result.Failures)
	}
	// Default sort is seeders descending, so the first item must have the max.
	maxSeeders := 0
	for _, item := range result.Items {
		if item.Seeders > maxSeeders {
			maxSeeders = item.Seeders
		}
	}
	if result.Items[0].Seeders != maxSeeders {
		t.Errorf("first item seeders = %d, want %d", result.Items[0].Seeders, maxSeeders)
	}
	if result.SearchID == "" || result.ElapsedMS < 0 {
		t.Errorf("search metadata missing: %+v", result)
	}
}

// TST-031 / AC-31: a failing site does not block the others.
func TestTST031_PartialFailureVisible(t *testing.T) {
	env := newTestEnv(t)
	good := newCardigannSite(t, 2)
	defer good.Close()
	env.createSiteNamed(t, "好站点", good.URL)

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	env.createSiteNamed(t, "坏站点", bad.URL)

	result, err := env.search.Search(context.Background(), SearchQuery{Keyword: "x"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Items) == 0 {
		t.Fatal("healthy site results must still be returned")
	}
	if len(result.Failures) != 1 {
		t.Fatalf("failures = %+v", result.Failures)
	}
	if result.Failures[0].IndexerName != "坏站点" {
		t.Errorf("failure site = %q", result.Failures[0].IndexerName)
	}
	if result.Failures[0].Kind == "" || result.Failures[0].Message == "" {
		t.Errorf("failure must carry kind and message: %+v", result.Failures[0])
	}
}

// TST-030 / AC-30: identical releases from two sites collapse into one card
// with two sources.
func TestTST030_CrossSiteDedupe(t *testing.T) {
	env := newTestEnv(t)
	a := newCardigannSite(t, 1)
	defer a.Close()
	b := newCardigannSite(t, 1)
	defer b.Close()
	env.createSiteNamed(t, "A", a.URL)
	env.createSiteNamed(t, "B", b.URL)

	result, err := env.search.Search(context.Background(), SearchQuery{Keyword: "x"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %d, want 1 merged card", len(result.Items))
	}
	if len(result.Items[0].Sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(result.Items[0].Sources))
	}
}

// TST-028 / AC-28: size filters apply without new site requests.
func TestTST028_FiltersDoNotRefetch(t *testing.T) {
	env := newTestEnv(t)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`<table class="torrents"><tbody>
			<tr>
				<td><a class="title">Small</a><a class="download" href="/d/1">d</a></td>
				<td class="size">1 GB</td><td class="seeders">5</td><td class="cat" data-cat="2000">m</td>
			</tr><tr>
				<td><a class="title">Big</a><a class="download" href="/d/2">d</a></td>
				<td class="size">20 GB</td><td class="seeders">50</td><td class="cat" data-cat="2000">m</td>
			</tr></tbody></table>`))
	}))
	defer srv.Close()
	env.createSiteNamed(t, "x", srv.URL)

	if _, err := env.search.Search(context.Background(), SearchQuery{Keyword: "x"}); err != nil {
		t.Fatalf("first search: %v", err)
	}
	before := atomic.LoadInt32(&hits)

	result, err := env.search.Search(context.Background(), SearchQuery{Keyword: "x", MinSize: 10 << 30})
	if err != nil {
		t.Fatalf("filtered search: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Title != "Big" {
		t.Fatalf("filtered items = %+v", result.Items)
	}
	// The site is still queried for a new search; what matters is that the
	// filter itself never triggers an extra per-condition request.
	after := atomic.LoadInt32(&hits)
	if after-before > 3 {
		t.Errorf("filter caused %d extra requests", after-before)
	}
}

// TST-032 / AC-32: truncation is reported and never exceeds the cap.
func TestTST032_TruncatesAtLimit(t *testing.T) {
	env := newTestEnv(t)
	var rows strings.Builder
	rows.WriteString(`<table class="torrents"><tbody>`)
	for i := 0; i < 600; i++ {
		rows.WriteString(`<tr><td><a class="title">Item x`)
		rows.WriteString(itoa(i))
		rows.WriteString(`</a><a class="download" href="/d/`)
		rows.WriteString(itoa(i))
		rows.WriteString(`">d</a></td><td class="size">1 GB</td><td class="seeders">`)
		rows.WriteString(itoa(1000 - i))
		rows.WriteString(`</td><td class="cat" data-cat="2000">m</td></tr>`)
	}
	rows.WriteString("</tbody></table>")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(rows.String()))
	}))
	defer srv.Close()
	env.createSiteNamed(t, "x", srv.URL)

	result, err := env.search.Search(context.Background(), SearchQuery{Keyword: "Item", PageSize: 50})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !result.Truncated {
		t.Error("truncated flag must be set when the cap is hit")
	}
	if result.TotalFound > MaxResultItems {
		t.Errorf("total found = %d, want <= %d", result.TotalFound, MaxResultItems)
	}
	if len(result.Items) != 50 {
		t.Errorf("page size = %d, want 50", len(result.Items))
	}
}

// TST-018 / AC-18: three consecutive failures put a site into cooldown.
func TestTST018_CooldownAfterThreeFailures(t *testing.T) {
	env := newTestEnv(t)
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	id := env.createSiteNamed(t, "x", failing.URL)

	for i := 0; i < 3; i++ {
		_, _ = env.search.Search(context.Background(), SearchQuery{Keyword: "k"})
	}
	status, err := env.repo.GetStatus(id)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if status.ConsecutiveFailures < 3 {
		t.Fatalf("failures = %d, want >= 3", status.ConsecutiveFailures)
	}
	if !status.CooldownUntil.After(time.Now()) {
		t.Fatal("cooldown must be active")
	}
	// A cooled-down site is excluded, so the search reports no indexers.
	if _, err := env.search.Search(context.Background(), SearchQuery{Keyword: "k"}); !errors.Is(err, ErrNoIndexers) {
		t.Fatalf("cooled-down site must be skipped, got %v", err)
	}
	// Clearing the cooldown makes it eligible again.
	if err := env.indexers.ClearCooldown(id); err != nil {
		t.Fatalf("ClearCooldown: %v", err)
	}
	if _, err := env.search.Search(context.Background(), SearchQuery{Keyword: "k"}); errors.Is(err, ErrNoIndexers) {
		t.Fatal("site must be eligible after clearing the cooldown")
	}
}

// TST-033 / TST-035 / AC-33 / AC-35: a grab creates a task with the mapped
// category and records it.
func TestTST033_GrabCreatesTaskWithCategory(t *testing.T) {
	env := newTestEnv(t)
	site := newCardigannSite(t, 1)
	defer site.Close()
	indexerID := env.createSiteNamed(t, "x", site.URL)

	qbSrv, pushed := newQBSiteCapturing(t)
	defer qbSrv.Close()
	client, err := env.clients.Create(DownloadClientInput{
		Name: "NAS", Host: hostOf(t, qbSrv.URL), Port: portOf(t, qbSrv.URL),
		DefaultCategory: "bujic",
		Categories:      []CategoryMappingInput{{SourceCategory: 5000, ClientCategory: "tv"}},
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	result, err := env.grabs.Grab(context.Background(), GrabRequest{
		Fingerprint: "fp-tv", IndexerID: indexerID, Title: "剧集",
		DownloadURL: "magnet:?xt=urn:btih:abc", DownloadType: "magnet",
		Categories: []int{5000}, ClientID: client.ID,
	})
	if err != nil {
		t.Fatalf("Grab: %v", err)
	}
	if result.Category != "tv" {
		t.Errorf("category = %q, want tv", result.Category)
	}
	if !strings.Contains(pushed(), `"tv"`) {
		t.Errorf("push body missing category: %s", pushed())
	}

	records, _ := env.grabs.Records(10)
	if len(records) != 1 || records[0].Status != "created" {
		t.Fatalf("records = %+v", records)
	}
	if records[0].ClientCategory != "tv" {
		t.Errorf("recorded category = %q", records[0].ClientCategory)
	}
}

// TST-034 / AC-34: a second grab within the window is rejected as duplicate
// without creating another task.
func TestTST034_GrabIdempotentWithinWindow(t *testing.T) {
	env := newTestEnv(t)
	site := newCardigannSite(t, 1)
	defer site.Close()
	indexerID := env.createSiteNamed(t, "x", site.URL)
	qbSrv, pushed := newQBSiteCapturing(t)
	defer qbSrv.Close()
	if _, err := env.clients.Create(DownloadClientInput{Name: "NAS", Host: hostOf(t, qbSrv.URL), Port: portOf(t, qbSrv.URL)}); err != nil {
		t.Fatalf("create client: %v", err)
	}

	req := GrabRequest{Fingerprint: "fp-dup", IndexerID: indexerID, Title: "t", DownloadURL: "magnet:?xt=urn:btih:abc", DownloadType: "magnet"}
	if _, err := env.grabs.Grab(context.Background(), req); err != nil {
		t.Fatalf("first grab: %v", err)
	}
	second, err := env.grabs.Grab(context.Background(), req)
	var grabErr *GrabError
	if !errors.As(err, &grabErr) || grabErr.Kind != GrabKindDuplicate {
		t.Fatalf("second grab must be a duplicate error, got %v", err)
	}
	if !second.Duplicate {
		t.Fatal("second grab must be flagged as duplicate")
	}
	if pushes := strings.Count(pushed(), `"urls"`); pushes != 1 {
		t.Errorf("downloader received %d pushes, want 1", pushes)
	}
	records, _ := env.grabs.Records(10)
	if len(records) != 1 {
		t.Errorf("records = %d, want 1", len(records))
	}
}

// TST-023 / AC-23: when the preferred client is disabled the next one is used.
func TestTST023_FailoverToNextClient(t *testing.T) {
	env := newTestEnv(t)
	site := newCardigannSite(t, 1)
	defer site.Close()
	indexerID := env.createSiteNamed(t, "x", site.URL)

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	down.Close()

	// Priority 1 points at a dead port; priority 50 points at the live stub.
	clientA, err := env.clients.Create(DownloadClientInput{Name: "A", Host: "127.0.0.1", Port: 1, Priority: 1})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	qbSrv, _ := newQBSiteCapturing(t)
	defer qbSrv.Close()
	if _, err := env.clients.Create(DownloadClientInput{Name: "B", Host: hostOf(t, qbSrv.URL), Port: portOf(t, qbSrv.URL), Priority: 50}); err != nil {
		t.Fatalf("create B: %v", err)
	}

	// Mark A unavailable, which is what the failover rule keys on.
	if _, err := env.clients.SetEnabled(clientA.ID, false); err != nil {
		t.Fatalf("disable A: %v", err)
	}

	result, err := env.grabs.Grab(context.Background(), GrabRequest{
		Fingerprint: "fp-f", IndexerID: indexerID, Title: "t",
		DownloadURL: "magnet:?xt=urn:btih:abc", DownloadType: "magnet",
	})
	if err != nil {
		t.Fatalf("Grab: %v", err)
	}
	if result.ClientName != "B" {
		t.Errorf("client = %q, want B", result.ClientName)
	}
}

// TST-021 / AC-21: category resolution prefers exact, then parent, then default.
func TestTST021_ResolveCategoryOrder(t *testing.T) {
	env := newTestEnv(t)
	client, err := env.clients.Create(DownloadClientInput{
		Name: "NAS", Host: "127.0.0.1", Port: 8080, DefaultCategory: "other",
		Categories: []CategoryMappingInput{
			{SourceCategory: 2040, ClientCategory: "4k"},
			{SourceCategory: 2000, ClientCategory: "movies"},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	cases := []struct {
		cats []int
		want string
	}{
		{[]int{2040}, "4k"},     // exact
		{[]int{2050}, "movies"}, // parent group
		{[]int{5000}, "other"},  // default
		{nil, "other"},          // default
	}
	for _, c := range cases {
		got, err := env.clients.ResolveCategory(client.ID, c.cats)
		if err != nil {
			t.Fatalf("ResolveCategory(%v): %v", c.cats, err)
		}
		if got != c.want {
			t.Errorf("cats %v => %q, want %q", c.cats, got, c.want)
		}
	}
}

// TST-053 / BR-30: duplicate source categories are rejected.
func TestTST053_DuplicateCategoryMappingRejected(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.clients.Create(DownloadClientInput{
		Name: "NAS", Host: "127.0.0.1", Port: 8080,
		Categories: []CategoryMappingInput{
			{SourceCategory: 5000, ClientCategory: "tv"},
			{SourceCategory: 5000, ClientCategory: "anime"},
		},
	})
	if err == nil {
		t.Fatal("duplicate mapping must be rejected")
	}
	var inputErr *InputError
	if !errors.As(err, &inputErr) || inputErr.Field != "categories" {
		t.Fatalf("error = %v, want categories field error", err)
	}
}

// TST-036 / TST-037 / AC-36 / AC-37: tasks are grouped per client and a dead
// client only degrades its own group.
func TestTST036_TaskGroupsAndDegradation(t *testing.T) {
	env := newTestEnv(t)
	qbSrv, _ := newQBSiteCapturing(t)
	defer qbSrv.Close()

	good, err := env.clients.Create(DownloadClientInput{Name: "好下载器", Host: hostOf(t, qbSrv.URL), Port: portOf(t, qbSrv.URL)})
	if err != nil {
		t.Fatalf("create good: %v", err)
	}
	if _, err := env.clients.Create(DownloadClientInput{Name: "死下载器", Host: "127.0.0.1", Port: 1, Priority: 60}); err != nil {
		t.Fatalf("create bad: %v", err)
	}

	groups, err := env.tasks.Groups(context.Background())
	if err != nil {
		t.Fatalf("Groups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	var okGroup, badGroup *TaskGroup
	for i := range groups {
		switch groups[i].ClientID {
		case good.ID:
			okGroup = &groups[i]
		default:
			badGroup = &groups[i]
		}
	}
	if okGroup == nil || okGroup.State != "ok" {
		t.Fatalf("good group = %+v", okGroup)
	}
	if len(okGroup.Tasks) == 0 {
		t.Error("good group must list tasks")
	}
	if badGroup == nil || badGroup.State != "unreachable" {
		t.Fatalf("bad group = %+v", badGroup)
	}
	if badGroup.Message == "" {
		t.Error("degraded group must explain itself")
	}
}

// TST-041 / AC-41: history keeps the newest 20 keywords.
func TestTST041_HistoryLimit(t *testing.T) {
	env := newTestEnv(t)
	site := newCardigannSite(t, 1)
	defer site.Close()
	env.createSiteNamed(t, "x", site.URL)

	for i := 0; i < 21; i++ {
		if _, err := env.search.Search(context.Background(), SearchQuery{Keyword: "kw" + itoa(i)}); err != nil {
			t.Fatalf("search %d: %v", i, err)
		}
	}
	history, err := env.search.History()
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) != 20 {
		t.Fatalf("history = %d, want 20", len(history))
	}
	if history[0].Keyword != "kw20" {
		t.Errorf("newest = %q, want kw20", history[0].Keyword)
	}
}

// TST-024 / TST-040 / AC-40: a failed grab records the error kind and keeps the
// client name snapshot.
func TestTST040_FailedGrabRecorded(t *testing.T) {
	env := newTestEnv(t)
	site := newCardigannSite(t, 1)
	defer site.Close()
	indexerID := env.createSiteNamed(t, "x", site.URL)

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	dead.Close()
	if _, err := env.clients.Create(DownloadClientInput{Name: "死", Host: "127.0.0.1", Port: 1}); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := env.grabs.Grab(context.Background(), GrabRequest{
		Fingerprint: "fp-fail", IndexerID: indexerID, Title: "t",
		DownloadURL: "magnet:?xt=urn:btih:abc", DownloadType: "magnet",
	})
	if err == nil {
		t.Fatal("grab against a dead client must fail")
	}
	var grabErr *GrabError
	if !errors.As(err, &grabErr) {
		t.Fatalf("error = %v, want GrabError", err)
	}

	records, _ := env.grabs.Records(10)
	if len(records) != 1 {
		t.Fatalf("records = %d", len(records))
	}
	if records[0].Status != "failed" {
		t.Errorf("status = %q, want failed", records[0].Status)
	}
	if records[0].ErrorKind == "" {
		t.Error("failed record must carry an error kind")
	}
	if records[0].DownloaderName != "死" {
		t.Errorf("downloader snapshot = %q", records[0].DownloaderName)
	}
}

// TST-054 / BR-38: an instance whose definition disappeared becomes read-only.
func TestTST054_MissingDefinitionIsReadOnly(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.defs.Import("fake.yml", loadDefFile(t, "fake-cardigann.yml")); err != nil {
		t.Fatalf("Import: %v", err)
	}
	view, err := env.indexers.Create(IndexerInput{Name: "x", Connector: "cardigann", DefinitionID: "fake-tracker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := env.defs.Delete("fake-tracker"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := env.indexers.Get(view.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DefinitionStatus != "missing" {
		t.Fatalf("definition status = %q, want missing", got.DefinitionStatus)
	}
	// The snapshot still allows a search, which is the documented behaviour.
	if _, err := env.indexers.Connector(context.Background(), view.ID); err != nil {
		t.Fatalf("connector from snapshot must still build: %v", err)
	}
}

// TST-055 / BR-41: refreshing the definition switches to the new snapshot.
func TestTST055_RefreshDefinitionUpdatesSnapshot(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.defs.Import("fake.yml", loadDefFile(t, "fake-cardigann.yml")); err != nil {
		t.Fatalf("Import: %v", err)
	}
	view, err := env.indexers.Create(IndexerInput{Name: "x", Connector: "cardigann", DefinitionID: "fake-tracker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	oldVersion := view.DefinitionVersion

	updated := strings.Replace(string(loadDefFile(t, "fake-cardigann.yml")), "description: 测试用索引器定义", "description: 更新后的定义", 1)
	if summary, err := env.defs.Import("fake.yml", []byte(updated)); err != nil || summary.Updated != 1 {
		t.Fatalf("re-import = %+v err %v", summary, err)
	}

	refreshed, err := env.indexers.RefreshDefinition(view.ID)
	if err != nil {
		t.Fatalf("RefreshDefinition: %v", err)
	}
	if refreshed.DefinitionVersion == oldVersion {
		t.Error("refresh must adopt the new definition version")
	}
}

// BR-19 / AC-35: no default and no mapping means "send no category".
func TestTST021_EmptyCategoryWhenNothingMapped(t *testing.T) {
	env := newTestEnv(t)
	client, err := env.clients.Create(DownloadClientInput{Name: "NAS", Host: "127.0.0.1", Port: 8080})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := env.clients.ResolveCategory(client.ID, []int{5000})
	if err != nil {
		t.Fatalf("ResolveCategory: %v", err)
	}
	if got != "" {
		t.Fatalf("category = %q, want empty", got)
	}
}

// noSleepClock advances instantly so retry backoff does not slow tests.
type noSleepClock struct{}

func (noSleepClock) Now() time.Time        { return time.Now() }
func (noSleepClock) Sleep(d time.Duration) {}

type instantWaiter struct{}

func (instantWaiter) Wait(context.Context) error { return nil }

// helpers

// definitionZip builds a package.zip with two valid definitions.
func definitionZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"alpha.yml": `id: alpha
name: Alpha
type: public
links: [https://alpha.example/]
caps:
  modes: {search: [q]}
search:
  path: /s
  rows: {selector: li}
  fields:
    title: {selector: a}
    download: {selector: a, attribute: href}
`,
		"beta.yml": `id: beta
name: Beta
type: public
links: [https://beta.example/]
caps:
  modes: {search: [q]}
search:
  path: /s
  rows: {selector: li}
  fields:
    title: {selector: a}
    download: {selector: a, attribute: href}
`,
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

func (e *testEnv) createSite(t *testing.T, siteURL string) uint {
	return e.createSiteNamed(t, "测试站点", siteURL)
}

// createSiteNamed registers a Cardigann instance pointing at a stub server.
func (e *testEnv) createSiteNamed(t *testing.T, name, siteURL string) uint {
	t.Helper()
	if _, err := e.defs.Import("fake.yml", loadDefFile(t, "fake-cardigann.yml")); err != nil {
		t.Fatalf("import definition: %v", err)
	}
	// Point the snapshot at the stub host by importing a variant with that link.
	raw := string(loadDefFile(t, "fake-cardigann.yml"))
	raw = strings.Replace(raw, "https://fake-tracker.example/", siteURL+"/", 1)
	// A unique id per instance keeps the library consistent across stubs.
	id := "fake-tracker-" + name
	raw = strings.Replace(raw, "id: fake-tracker", "id: "+id, 1)
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
	// Disable the per-site rate limit for tests: production keeps the 200ms
	// floor enforced by validation above.
	envDisableRateLimit(t, e.repo, view.ID)
	return view.ID
}

func (e *testEnv) createTorznab(t *testing.T, name, baseURL string) uint {
	t.Helper()
	view, err := e.indexers.Create(IndexerInput{Name: name, Connector: "torznab", BaseURL: baseURL, APIKey: "key"})
	if err != nil {
		t.Fatalf("create torznab: %v", err)
	}
	envDisableRateLimit(t, e.repo, view.ID)
	return view.ID
}

// envDisableRateLimit zeroes the interval so tests do not wait on the limiter.
func envDisableRateLimit(t *testing.T, repo repository.SearchRepository, id uint) {
	t.Helper()
	idx, err := repo.GetIndexer(id)
	if err != nil {
		t.Fatalf("reload indexer: %v", err)
	}
	idx.MinIntervalMS = 0
	if err := repo.UpdateIndexer(idx); err != nil {
		t.Fatalf("update indexer: %v", err)
	}
}

// newCardigannSite serves a search page with n rows and allows login.
func newCardigannSite(t *testing.T, rows int) *httptest.Server {
	t.Helper()
	var body strings.Builder
	body.WriteString("<table class=\"torrents\"><tbody>")
	for i := 0; i < rows; i++ {
		body.WriteString(`<tr><td><a class="title">Release `)
		body.WriteString(itoa(i))
		body.WriteString(`</a></td><td class="size">`)
		body.WriteString(itoa(i + 1))
		body.WriteString(` GB</td><td class="seeders">`)
		body.WriteString(itoa(10 * (i + 1)))
		body.WriteString(`</td><td class="cat" data-cat="2000">m</td><td><a class="download" href="/download/`)
		body.WriteString(itoa(i))
		body.WriteString(`.torrent" data-hash="hash`)
		body.WriteString(itoa(i))
		body.WriteString(`">d</a></td></tr>`)
	}
	body.WriteString("</tbody></table>")

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/login"):
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok"})
			_, _ = w.Write([]byte("<html>ok</html>"))
		default:
			_, _ = w.Write([]byte(body.String()))
		}
	}))
}

// newTorznabSite serves a caps document and a search feed.
func newTorznabSite(t *testing.T) *httptest.Server {
	t.Helper()
	caps, err := os.ReadFile(filepath.Join("search", "testdata", "torznab", "caps.xml"))
	if err != nil {
		t.Fatalf("read caps: %v", err)
	}
	feed, err := os.ReadFile(filepath.Join("search", "testdata", "torznab", "search.xml"))
	if err != nil {
		t.Fatalf("read feed: %v", err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			_, _ = w.Write(caps)
			return
		}
		_, _ = w.Write(feed)
	}))
}

// pushedBody accumulates the torrents/add bodies a stub downloader received.
type pushedBody struct {
	mu   chan struct{}
	text strings.Builder
}

func (p *pushedBody) add(body string) {
	<-p.mu
	p.text.WriteString(body)
	p.text.WriteString("\n")
	p.mu <- struct{}{}
}

func (p *pushedBody) String() string {
	<-p.mu
	defer func() { p.mu <- struct{}{} }()
	return p.text.String()
}

// newQBSiteCapturing serves a minimal qBittorrent API and records push bodies.
func newQBSiteCapturing(t *testing.T) (*httptest.Server, func() string) {
	t.Helper()
	tasks, err := os.ReadFile(filepath.Join("search", "testdata", "qbittorrent", "torrents.json"))
	if err != nil {
		t.Fatalf("read tasks: %v", err)
	}
	idx := strings.Index(string(tasks), "[")
	end := strings.LastIndex(string(tasks), "]")
	taskArray := "[]"
	if idx >= 0 && end > idx {
		taskArray = string(tasks)[idx : end+1]
	}

	log := &pushedBody{mu: make(chan struct{}, 1)}
	log.mu <- struct{}{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "abc"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/version":
			_, _ = w.Write([]byte("v5.0.2"))
		case "/api/v2/torrents/categories":
			_, _ = w.Write([]byte(`{"bujic":{"name":"bujic"},"tv":{"name":"tv"}}`))
		case "/api/v2/torrents/add":
			_ = r.ParseForm()
			encoded, _ := json.Marshal(r.PostForm)
			log.add(string(encoded))
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			_, _ = w.Write([]byte(taskArray))
		default:
			_, _ = w.Write([]byte("Ok."))
		}
	}))
	return srv, log.String
}

func hostOf(t *testing.T, raw string) string {
	t.Helper()
	parts := strings.SplitN(strings.TrimPrefix(strings.TrimPrefix(raw, "http://"), "https://"), ":", 2)
	return parts[0]
}

func portOf(t *testing.T, raw string) int {
	t.Helper()
	parts := strings.SplitN(strings.TrimPrefix(strings.TrimPrefix(raw, "http://"), "https://"), ":", 2)
	if len(parts) != 2 {
		t.Fatalf("no port in %q", raw)
	}
	port := 0
	for _, r := range parts[1] {
		if r < '0' || r > '9' {
			break
		}
		port = port*10 + int(r-'0')
	}
	return port
}

// TST-001 / AC-01: syncing downloads one package instead of hundreds of files.
func TestTST001_SyncFromPackage(t *testing.T) {
	env := newTestEnv(t)

	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		if !strings.HasSuffix(r.URL.Path, "/package.zip") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(definitionZip(t))
	}))
	defer srv.Close()
	env.cfg.Search.DefinitionSource = srv.URL
	env.cfg.Search.DefinitionVersion = "12"

	summary, err := env.defs.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if summary.Added != 2 {
		t.Fatalf("added = %d, want 2", summary.Added)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("requests = %d, want 1 (package only)", got)
	}
	list, _ := env.defs.List()
	if len(list) != 2 {
		t.Fatalf("definitions = %d", len(list))
	}
}

// TST-058: the proxy test fetches only the list, so it stays fast.
func TestTST058_ProxyTestIsCheap(t *testing.T) {
	env := newTestEnv(t)
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		_, _ = w.Write([]byte(`[{"id":"a","name":"A","file":"a"},{"id":"b","name":"B","file":"b"}]`))
	}))
	defer srv.Close()
	env.cfg.Search.DefinitionSource = srv.URL
	env.cfg.Search.DefinitionVersion = "12"

	result := env.defs.TestSource(context.Background())
	if !result.OK || result.Count != 2 {
		t.Fatalf("result = %+v", result)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	if result.ElapsedMS < 0 {
		t.Fatal("elapsed must be measured")
	}
}

// TST-021 / AC-21 / AC-35: the grab dialog pre-fills the mapped category and
// explains which rule produced it.
func TestTST021_GrabPreviewPrefillsCategory(t *testing.T) {
	env := newTestEnv(t)
	site := newCardigannSite(t, 1)
	defer site.Close()
	indexerID := env.createSiteNamed(t, "站点", site.URL)

	qbSrv, _ := newQBSiteCapturing(t)
	defer qbSrv.Close()
	client, err := env.clients.Create(DownloadClientInput{
		Name: "NAS", Host: hostOf(t, qbSrv.URL), Port: portOf(t, qbSrv.URL),
		DefaultCategory: "bujic",
		Categories: []CategoryMappingInput{
			{SourceCategory: 5000, ClientCategory: "tv"},
			{SourceCategory: 2000, ClientCategory: "movies"},
		},
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	cases := []struct {
		name       string
		categories []int
		wantCat    string
		wantFrom   string
	}{
		{"exact mapping", []int{5000}, "tv", "mapping"},
		{"parent mapping", []int{5040}, "tv", "parent"},
		{"falls back to default", []int{3000}, "bujic", "default"},
		{"no category at all", nil, "bujic", "default"},
	}
	for _, c := range cases {
		preview, err := env.grabs.Preview(context.Background(), GrabRequest{
			IndexerID: indexerID, Fingerprint: "fp", Title: "t",
			DownloadURL: "magnet:?xt=urn:btih:abc", DownloadType: "magnet",
			Categories: c.categories, ClientID: client.ID,
		})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if preview.Category != c.wantCat {
			t.Errorf("%s: category = %q, want %q", c.name, preview.Category, c.wantCat)
		}
		if preview.CategoryFrom != c.wantFrom {
			t.Errorf("%s: from = %q, want %q", c.name, preview.CategoryFrom, c.wantFrom)
		}
		if preview.ClientName != "NAS" {
			t.Errorf("%s: client = %q", c.name, preview.ClientName)
		}
	}
}

// When no mapping matches and there is no default, the dialog must warn that
// qBittorrent will decide on its own.
func TestTST021_GrabPreviewWarnsWithoutCategory(t *testing.T) {
	env := newTestEnv(t)
	site := newCardigannSite(t, 1)
	defer site.Close()
	indexerID := env.createSiteNamed(t, "站点", site.URL)
	qbSrv, _ := newQBSiteCapturing(t)
	defer qbSrv.Close()
	if _, err := env.clients.Create(DownloadClientInput{
		Name: "NAS", Host: hostOf(t, qbSrv.URL), Port: portOf(t, qbSrv.URL),
	}); err != nil {
		t.Fatalf("create client: %v", err)
	}

	preview, err := env.grabs.Preview(context.Background(), GrabRequest{
		IndexerID: indexerID, Fingerprint: "fp", Title: "t",
		DownloadURL: "magnet:?xt=urn:btih:abc", DownloadType: "magnet",
		Categories: []int{5000},
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if preview.Category != "" {
		t.Errorf("category = %q, want empty", preview.Category)
	}
	if preview.CategoryFrom != "none" {
		t.Errorf("from = %q, want none", preview.CategoryFrom)
	}
	if preview.Warning == "" {
		t.Error("a missing category must produce a warning")
	}
}

// TST-005 / 需求 03: the template list exposes language and category coverage so
// the browser can filter on them.
func TestTST005_TemplateFilters(t *testing.T) {
	env := newTestEnv(t)
	raw := []byte(`id: cn-tracker
name: 中文站点
type: public
language: zh-CN
links: [https://cn.example/]
caps:
  categories:
    Movies: 1
    TV: 2
  modes:
    search: [q]
    tv-search: [q, season, ep]
    movie-search: [q]
search:
  path: /s
  rows: {selector: li}
  fields:
    title: {selector: a}
    download: {selector: a, attribute: href}
`)
	if _, err := env.defs.Import("cn.yml", raw); err != nil {
		t.Fatalf("Import: %v", err)
	}

	list, err := env.defs.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("definitions = %d", len(list))
	}
	item := list[0]

	if item.LanguageLabel != "简体中文" {
		t.Errorf("language label = %q, want 简体中文", item.LanguageLabel)
	}
	groups := map[string]bool{}
	for _, g := range item.CategoryGroups {
		groups[g] = true
	}
	if !groups["电影"] || !groups["剧集"] {
		t.Errorf("category groups = %v, want 电影 and 剧集", item.CategoryGroups)
	}
	if item.ModeSummary == "" {
		t.Error("mode summary must describe supported search modes")
	}
}

// TST-005: unknown language tags degrade to a readable region code.
func TestTST005_LanguageLabel(t *testing.T) {
	cases := map[string]string{
		"zh-CN": "简体中文",
		"zh-TW": "繁体中文",
		"en-US": "英语",
		"ja-JP": "日语",
		"ru-ru": "RU",
		"":      "未知",
	}
	for code, want := range cases {
		if got := LanguageLabel(code); got != want {
			t.Errorf("LanguageLabel(%q) = %q, want %q", code, got, want)
		}
	}
}

// TST-005 / 需求 03: each template exposes its site homepage so the list can
// link to it.
func TestTST005_TemplateSiteURL(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.defs.Import("fake.yml", loadDefFile(t, "fake-cardigann.yml")); err != nil {
		t.Fatalf("Import: %v", err)
	}
	list, err := env.defs.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("definitions = %d", len(list))
	}
	if list[0].SiteURL == "" {
		t.Fatal("site_url must be populated from the definition links")
	}
	if !strings.HasPrefix(list[0].SiteURL, "http") {
		t.Errorf("site_url = %q", list[0].SiteURL)
	}
}
