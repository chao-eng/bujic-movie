package connector

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/service/cardigann"
	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
)

// memSession is an in-memory SessionStore for tests.
type memSession struct {
	mu      sync.Mutex
	cookies map[string]string
	state   string
	failed  string
}

func (m *memSession) Load(ctx context.Context) (map[string]string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cookies == nil {
		return nil, "none", nil
	}
	return m.cookies, m.state, nil
}

func (m *memSession) Save(ctx context.Context, cookies map[string]string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cookies = cookies
	m.state = "logged-in"
	return nil
}

func (m *memSession) MarkFailed(ctx context.Context, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = "failed"
	m.failed = reason
	return nil
}

func (m *memSession) MarkPending(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = "pending"
	return nil
}

// memClock records sleeps so tests do not wait for rate limits.
type memClock struct{}

func (memClock) Now() time.Time        { return time.Now() }
func (memClock) Sleep(d time.Duration) {}

func loadCardigann(t *testing.T, name string) *cardigann.Definition {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "search", "testdata", "definitions", name))
	if err != nil {
		t.Fatalf("read definition: %v", err)
	}
	res, err := cardigann.Parse(name, raw)
	if err != nil {
		t.Fatalf("parse definition: %v", err)
	}
	return &res.Definition
}

func newTestCardigann(t *testing.T, def *cardigann.Definition, srv *httptest.Server, session SessionStore) *CardigannConnector {
	t.Helper()
	return newTestCardigannWithConfig(t, def, srv, session,
		map[string]string{"freeleech": "false", "sort": "seeders"},
		map[string]string{"username": "bob", "password": "hunter2"})
}

func newTestCardigannWithConfig(t *testing.T, def *cardigann.Definition, srv *httptest.Server, session SessionStore, settings, secrets map[string]string) *CardigannConnector {
	t.Helper()
	exec, err := httpclient.New(nil, memClock{}, "")
	if err != nil {
		t.Fatalf("httpclient.New: %v", err)
	}
	// Point the definition at the stub server.
	cloned := *def
	cloned.Links = []string{srv.URL}
	conn, err := NewCardigann(&cloned, "假站点", settings, secrets, session, exec, "", 200)
	if err != nil {
		t.Fatalf("NewCardigann: %v", err)
	}
	return conn
}

// TST-015 / TST-005: a Cardigann definition runs end to end against a stub.
func TestTST015_CardigannSearch(t *testing.T) {
	htmlPath := filepath.Join("..", "search", "testdata", "cardigann", "search.html")
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var gotUser string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/login"):
			// form login
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			gotUser = r.Form.Get("username")
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok"})
			_, _ = w.Write([]byte("<html>logged in</html>"))
		case strings.HasPrefix(r.URL.Path, "/torrents"):
			if c, err := r.Cookie("session"); err == nil && c.Value == "ok" {
				_, _ = w.Write(html)
				return
			}
			w.WriteHeader(http.StatusForbidden)
		default:
			_, _ = w.Write(html)
		}
	}))
	defer srv.Close()

	def := loadCardigann(t, "fake-cardigann.yml")
	session := &memSession{}
	conn := newTestCardigann(t, def, srv, session)

	ctx := context.Background()
	if err := conn.LoginWithCaptcha(ctx, "1234"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, _, _ = session.Load(ctx); session.state != "logged-in" {
		t.Fatalf("session state = %q", session.state)
	}

	releases, err := conn.Search(ctx, Query{Keyword: "怪奇物语", Categories: []int{5000}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(releases) != 3 {
		t.Fatalf("releases = %d, want 3", len(releases))
	}
	if gotUser == "" {
		t.Errorf("login form did not render the configured username")
	}
	if !strings.HasPrefix(releases[0].DownloadURL, srv.URL) {
		t.Errorf("download url must be absolute, got %q", releases[0].DownloadURL)
	}
	if releases[0].IndexerName != "假站点" {
		t.Errorf("indexer name = %q", releases[0].IndexerName)
	}
}

// TST-013 / AC-13: a site that needs a captcha refuses to test until one is
// supplied.
func TestTST013_CardigannCaptchaRequired(t *testing.T) {
	def := loadCardigann(t, "fake-cardigann.yml")
	def.Login.Captcha = &cardigann.CaptchaBlock{Type: "image", Selector: "img.captcha"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer srv.Close()

	conn := newTestCardigann(t, def, srv, &memSession{})
	_, err := conn.Test(context.Background())
	var cErr *Error
	if !asError(err, &cErr) || cErr.Kind != KindCaptcha {
		t.Fatalf("kind = %v, want captcha", err)
	}
	if cErr.Field != "login.captcha" {
		t.Errorf("field = %q", cErr.Field)
	}
}

// TST-014 / AC-14: an expired session is renewed once and the search proceeds.
func TestTST014_CardigannReloginOnAuthFailure(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("..", "search", "testdata", "cardigann", "search.html"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var searchCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/login") {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "fresh"})
			_, _ = w.Write([]byte("<html>ok</html>"))
			return
		}
		searchCalls++
		c, err := r.Cookie("session")
		if err != nil || c.Value != "fresh" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write(html)
	}))
	defer srv.Close()

	def := loadCardigann(t, "fake-cardigann.yml")
	// Pre-seed an expired session cookie.
	session := &memSession{cookies: map[string]string{"session": "expired"}, state: "logged-in"}
	conn := newTestCardigann(t, def, srv, session)

	releases, err := conn.Search(context.Background(), Query{Keyword: "x"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(releases) != 3 {
		t.Fatalf("releases = %d", len(releases))
	}
	if searchCalls < 2 {
		t.Fatalf("expected a retry after re-login, calls = %d", searchCalls)
	}
}

// TST-010: wrong credentials are classified as auth with the login field.
func TestTST010_CardigannLoginFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><div class="error">用户名或密码错误</div></html>`))
	}))
	defer srv.Close()

	def := loadCardigann(t, "fake-cardigann.yml")
	conn := newTestCardigann(t, def, srv, &memSession{})
	err := conn.LoginWithCaptcha(context.Background(), "")
	var cErr *Error
	if !asError(err, &cErr) || cErr.Kind != KindAuth {
		t.Fatalf("kind = %v, want auth", err)
	}
}

// TST-016 / REQ-09: a definition whose filter is unknown still yields results
// but the connector reports the parse problem instead of panicking.
func TestTST016_CardigannUnknownFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<table><tr><td><a class="title">T</a><a class="dl" href="/d/1">d</a></td></tr></table>`))
	}))
	defer srv.Close()

	def := &cardigann.Definition{
		ID: "x", Name: "x", Type: "public", Links: []string{srv.URL},
		Caps: cardigann.CapabilitiesBlock{Modes: map[string][]string{"search": {"q"}}},
		Search: &cardigann.SearchBlock{
			Path: "/s",
			Rows: &cardigann.RowsBlock{SelectorBlock: cardigann.SelectorBlock{Selector: "tr"}},
			Fields: cardigann.FieldMap{Entries: []cardigann.FieldEntry{
				{Name: "title", Selector: cardigann.SelectorBlock{Selector: "a.title"}},
				{Name: "download", Selector: cardigann.SelectorBlock{
					Selector: "a.dl", Attribute: "href",
					Filters: []cardigann.FilterBlock{{Name: "no_such_filter"}},
				}},
			}},
		},
	}
	conn := newTestCardigann(t, def, srv, &memSession{})
	releases, err := conn.Search(context.Background(), Query{Keyword: "x"})
	if err != nil {
		t.Fatalf("Search must tolerate unknown filters: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("releases = %d, want 1", len(releases))
	}
}

// TST-015 / AC-15: multiple search paths run and merge.
func TestTST015_CardigannMultiplePaths(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		title := r.URL.Path
		fmt.Fprintf(w, `<table><tr><td><a class="t">%s</a><a class="d" href="/x">d</a></td></tr></table>`, title)
	}))
	defer srv.Close()

	def := &cardigann.Definition{
		ID: "x", Name: "x", Type: "public", Links: []string{srv.URL},
		Caps: cardigann.CapabilitiesBlock{Modes: map[string][]string{"search": {"q"}}},
		Search: &cardigann.SearchBlock{
			Paths: []cardigann.SearchPathBlock{
				{RequestBlock: cardigann.RequestBlock{Path: "/p1"}, Categories: []string{"2000"}},
				{RequestBlock: cardigann.RequestBlock{Path: "/p2"}, Categories: []string{"5000"}},
			},
			Rows: &cardigann.RowsBlock{SelectorBlock: cardigann.SelectorBlock{Selector: "tr"}},
			Fields: cardigann.FieldMap{Entries: []cardigann.FieldEntry{
				{Name: "title", Selector: cardigann.SelectorBlock{Selector: "a.t"}},
				{Name: "download", Selector: cardigann.SelectorBlock{Selector: "a.d", Attribute: "href"}},
			}},
		},
	}
	conn := newTestCardigann(t, def, srv, &memSession{})
	releases, err := conn.Search(context.Background(), Query{Keyword: "x"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("paths executed = %v, want 2", paths)
	}
	if len(releases) != 2 {
		t.Fatalf("releases = %d, want 2", len(releases))
	}
}

// TST-025 / BR-04: each path gets a budget share with a 2s floor.
func TestTST025_PathBudget(t *testing.T) {
	if got := pathBudget(1); got != 8*time.Second {
		t.Errorf("single path budget = %v, want 8s", got)
	}
	if got := pathBudget(4); got != 2*time.Second {
		t.Errorf("4 paths budget = %v, want 2s", got)
	}
	if got := pathBudget(20); got != 2*time.Second {
		t.Errorf("20 paths budget = %v, want floor 2s", got)
	}
}

// BR-36: a definition may only reach hosts it declares.
func TestTST050_CardigannHostAllowlist(t *testing.T) {
	def := loadCardigann(t, "fake-cardigann.yml")
	exec, _ := httpclient.New(nil, memClock{}, "")
	conn, err := NewCardigann(def, "x", nil, nil, &memSession{}, exec, "", 200)
	if err != nil {
		t.Fatalf("NewCardigann: %v", err)
	}
	if _, err := conn.Search(context.Background(), Query{Keyword: "x"}); err == nil {
		t.Fatal("search against a host outside the definition must fail")
	}
}

func TestTST033_CardigannResolveDownloadKeepsMagnet(t *testing.T) {
	def := loadCardigann(t, "fake-cardigann.yml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	conn := newTestCardigann(t, def, srv, &memSession{})

	ref, err := conn.ResolveDownload(context.Background(), Release{
		Title: "t", DownloadURL: "magnet:?xt=urn:btih:abc", InfoHash: "abc", DownloadType: "magnet",
	})
	if err != nil {
		t.Fatalf("ResolveDownload: %v", err)
	}
	if ref.Type != "magnet" {
		t.Fatalf("type = %q", ref.Type)
	}
}
