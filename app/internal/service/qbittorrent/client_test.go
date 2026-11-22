package qbittorrent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
)

func loadTasks(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "search", "testdata", "qbittorrent", "torrents.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// The fixture is an object with a tasks array; extract the array.
	idx := strings.Index(string(raw), "[")
	end := strings.LastIndex(string(raw), "]")
	if idx < 0 || end < idx {
		t.Fatal("fixture has no array")
	}
	return []byte(string(raw)[idx : end+1])
}

func newClient(t *testing.T, srv *httptest.Server) *HTTPClient {
	t.Helper()
	exec, err := httpclient.New(nil, nil, "")
	if err != nil {
		t.Fatalf("httpclient.New: %v", err)
	}
	host, port := splitHostPort(t, srv.URL)
	return NewHTTPClient(Config{Host: host, Port: port, Username: "admin", Password: "pw"}, exec)
}

// TST-022 / AC-22: a successful test reports the version and category state.
func TestTST022_TestReportsVersionAndCategories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "abc"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/version":
			_, _ = w.Write([]byte("v5.0.2"))
		case "/api/v2/torrents/categories":
			_, _ = w.Write([]byte(`{"tv":{"name":"tv","savePath":"/tv"}}`))
		case "/api/v2/torrents/createCategory":
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := newClient(t, srv)
	status, err := client.Test(context.Background(), []string{"tv", "movies"})
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if status.Version != "v5.0.2" {
		t.Errorf("version = %q", status.Version)
	}
	if len(status.CategoriesReady) != 2 {
		t.Errorf("ready = %v", status.CategoriesReady)
	}
	if len(status.CategoriesFailed) != 0 {
		t.Errorf("failed = %v", status.CategoriesFailed)
	}
}

// TST-022: a category that cannot be created is reported, not fatal.
func TestTST022_CategoryCreationFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "abc"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/version":
			_, _ = w.Write([]byte("v4.6.0"))
		case "/api/v2/torrents/categories":
			_, _ = w.Write([]byte(`{}`))
		case "/api/v2/torrents/createCategory":
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	client := newClient(t, srv)
	status, err := client.Test(context.Background(), []string{"4k"})
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if len(status.CategoriesFailed) != 1 || status.CategoriesFailed[0] != "4k" {
		t.Fatalf("failed = %v", status.CategoriesFailed)
	}
}

// TST-022: bad credentials are classified as auth.
func TestTST022_AuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Fails."))
	}))
	defer srv.Close()

	client := newClient(t, srv)
	_, err := client.Test(context.Background(), nil)
	var qErr *Error
	if !asClientError(err, &qErr) || qErr.Kind != KindAuth {
		t.Fatalf("kind = %v, want auth", err)
	}
}

// TST-033 / AC-33: pushing sends the magnet and category.
func TestTST033_PushSendsCategory(t *testing.T) {
	var gotCategory, gotURLs string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "abc"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/add":
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			gotCategory = r.Form.Get("category")
			gotURLs = r.Form.Get("urls")
			_, _ = w.Write([]byte("Ok."))
		}
	}))
	defer srv.Close()

	client := newClient(t, srv)
	err := client.Push(context.Background(), "magnet", "magnet:?xt=urn:btih:abc", "tv", "n", false)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if gotCategory != "tv" {
		t.Errorf("category = %q", gotCategory)
	}
	if gotURLs != "magnet:?xt=urn:btih:abc" {
		t.Errorf("urls = %q", gotURLs)
	}
}

// TST-034: a duplicate torrent is rejected with a stable kind.
func TestTST034_PushDuplicateRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "abc"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/add":
			_, _ = w.Write([]byte("Fails."))
		}
	}))
	defer srv.Close()

	client := newClient(t, srv)
	err := client.Push(context.Background(), "magnet", "magnet:?xt=urn:btih:abc", "", "n", false)
	var qErr *Error
	if !asClientError(err, &qErr) || qErr.Kind != KindRejected {
		t.Fatalf("kind = %v, want rejected", err)
	}
}

// TST-036 / TST-038: tasks are parsed from the real API shape.
func TestTST036_TasksParsed(t *testing.T) {
	body := loadTasks(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "abc"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			_, _ = w.Write(body)
		}
	}))
	defer srv.Close()

	client := newClient(t, srv)
	tasks, err := client.Tasks(context.Background())
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(tasks))
	}
	first := tasks[0]
	if first.Hash != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("hash = %q", first.Hash)
	}
	if first.Progress != 0.42 {
		t.Errorf("progress = %v", first.Progress)
	}
	if first.SizeBytes != 12884901888 {
		t.Errorf("size = %d", first.SizeBytes)
	}
	if first.Category != "4k" {
		t.Errorf("category = %q", first.Category)
	}
	if first.State != "downloading" {
		t.Errorf("state = %q", first.State)
	}
	// The connection count is what explains a stuck download, so it must be read
	// from num_seeds / num_complete rather than left at zero.
	if first.ConnectedSeeds != 12 || first.AvailableSeeds != 84 {
		t.Errorf("seeds = %d/%d, want 12/84", first.ConnectedSeeds, first.AvailableSeeds)
	}
}

// TST-038 / TST-039: pause, resume and delete map to the right endpoints and
// delete defaults to keeping files.
func TestTST038_ActionsMapToEndpoints(t *testing.T) {
	var calls []string
	var deleteFiles string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "abc"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/pause", "/api/v2/torrents/resume", "/api/v2/torrents/delete":
			calls = append(calls, r.URL.Path)
			_ = r.ParseForm()
			if r.URL.Path == "/api/v2/torrents/delete" {
				deleteFiles = r.Form.Get("deleteFiles")
			}
		}
	}))
	defer srv.Close()

	client := newClient(t, srv)
	ctx := context.Background()
	if err := client.Action(ctx, "hash1", "pause", false); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := client.Action(ctx, "hash1", "resume", false); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if err := client.Action(ctx, "hash1", "delete", false); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("calls = %v", calls)
	}
	if deleteFiles != "false" {
		t.Errorf("deleteFiles = %q, want false by default", deleteFiles)
	}
}

func TestTST038_UnknownActionRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/auth/login" {
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "abc"})
			_, _ = w.Write([]byte("Ok."))
		}
	}))
	defer srv.Close()

	client := newClient(t, srv)
	if err := client.Action(context.Background(), "h", "explode", false); err == nil {
		t.Fatal("unknown action must be rejected")
	}
}

// TST-037: an unreachable client is classified so the task list can degrade.
func TestTST037_Unreachable(t *testing.T) {
	exec, _ := httpclient.New(nil, nil, "")
	client := NewHTTPClient(Config{Host: "127.0.0.1", Port: 1, Username: "a", Password: "b"}, exec)
	_, err := client.Tasks(context.Background())
	var qErr *Error
	if !asClientError(err, &qErr) || qErr.Kind != KindUnreachable {
		t.Fatalf("kind = %v, want unreachable", err)
	}
}

// NFR-07: errors never echo the password.
func TestTST022_ErrorDoesNotLeakPassword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Fails."))
	}))
	defer srv.Close()

	exec, _ := httpclient.New(nil, nil, "")
	host, port := splitHostPort(t, srv.URL)
	client := NewHTTPClient(Config{Host: host, Port: port, Username: "admin", Password: "s3cr3t-pw"}, exec)
	_, err := client.Test(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "s3cr3t") {
		t.Fatalf("error leaked password: %v", err)
	}
}

func asClientError(err error, target **Error) bool {
	for err != nil {
		if q, ok := err.(*Error); ok {
			*target = q
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// TST-021 / AC-21: existing categories are listed with their save paths so the
// mapping UI can offer them and show where each one writes.
func TestTST021_CategoriesListed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "abc"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/categories":
			_, _ = w.Write([]byte(`{"电影":{"name":"电影","savePath":"/downloads/movies"},"电视剧":{"name":"电视剧","savePath":"/downloads/tv"}}`))
		}
	}))
	defer srv.Close()

	client := newClient(t, srv)
	categories, err := client.Categories(context.Background())
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if len(categories) != 2 {
		t.Fatalf("categories = %d, want 2", len(categories))
	}
	byName := map[string]string{}
	for _, c := range categories {
		byName[c.Name] = c.SavePath
	}
	if byName["电影"] != "/downloads/movies" {
		t.Errorf("电影 save path = %q", byName["电影"])
	}
	if byName["电视剧"] != "/downloads/tv" {
		t.Errorf("电视剧 save path = %q", byName["电视剧"])
	}
}
