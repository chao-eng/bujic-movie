package connector

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

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "search", "testdata", "torznab", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

// TST-007 / AC-07: a generic endpoint reports its declared categories.
func TestTST007_TorznabTestReportsCapabilities(t *testing.T) {
	caps := fixture(t, "caps.xml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") != "caps" {
			t.Errorf("unexpected t=%q", r.URL.Query().Get("t"))
		}
		_, _ = w.Write(caps)
	}))
	defer srv.Close()

	exec, _ := httpclient.New(nil, nil, "")
	conn := NewTorznab("假 Torznab", srv.URL, "key", "", exec, 0)

	got, err := conn.Test(context.Background())
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if got.Categories != 5 {
		t.Errorf("categories = %d, want 5 (2 groups + 3 subcats)", got.Categories)
	}
	if got.Protocol != "torrent" {
		t.Errorf("protocol = %q", got.Protocol)
	}
	if len(got.SearchModes) != 3 {
		t.Errorf("modes = %v, want search/tv/movie", got.SearchModes)
	}
	if got.Elapsed <= 0 {
		t.Error("elapsed must be measured")
	}
}

// TST-010: a non-Torznab body is a parse error naming the block.
func TestTST010_TorznabTestRejectsNonCapsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>login required</body></html>"))
	}))
	defer srv.Close()

	exec, _ := httpclient.New(nil, nil, "")
	conn := NewTorznab("x", srv.URL, "", "", exec, 0)
	_, err := conn.Test(context.Background())
	var cErr *Error
	if !asConnectorError(err, &cErr) || cErr.Kind != KindParse {
		t.Fatalf("kind = %v, want parse", err)
	}
	if cErr.Field != "caps" {
		t.Errorf("field = %q, want caps", cErr.Field)
	}
}

// TST-010: a 401 is classified as auth, not as an unreachable site.
func TestTST010_TorznabAuthClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	exec, _ := httpclient.New(nil, nil, "")
	conn := NewTorznab("x", srv.URL, "bad", "", exec, 0)
	_, err := conn.Test(context.Background())
	var cErr *Error
	if !asConnectorError(err, &cErr) || cErr.Kind != KindAuth {
		t.Fatalf("kind = %v, want auth", err)
	}
}

// TST-030: RSS results are normalised, including attrs and infohash.
func TestTST030_TorznabSearchParsesRSS(t *testing.T) {
	rss := fixture(t, "search.xml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") != "search" {
			t.Errorf("t = %q", r.URL.Query().Get("t"))
		}
		if r.URL.Query().Get("q") != "怪奇物语" {
			t.Errorf("q = %q", r.URL.Query().Get("q"))
		}
		if r.URL.Query().Get("cat") != "5000" {
			t.Errorf("cat = %q", r.URL.Query().Get("cat"))
		}
		_, _ = w.Write(rss)
	}))
	defer srv.Close()

	exec, _ := httpclient.New(nil, nil, "")
	conn := NewTorznab("假 Torznab", srv.URL, "key", "", exec, 0)

	releases, err := conn.Search(context.Background(), Query{Keyword: "怪奇物语", Categories: []int{5000}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("releases = %d, want 2", len(releases))
	}
	first := releases[0]
	if first.Seeders != 42 || first.Peers != 3 {
		t.Errorf("seeders/peers = %d/%d", first.Seeders, first.Peers)
	}
	if first.SizeBytes != 12884901888 {
		t.Errorf("size = %d", first.SizeBytes)
	}
	if first.InfoHash == "" {
		t.Error("infohash must be parsed")
	}
	if len(first.Categories) == 0 || first.Categories[0] != 2045 {
		t.Errorf("categories = %v", first.Categories)
	}
	if first.IndexerName != "假 Torznab" {
		t.Errorf("indexer name = %q", first.IndexerName)
	}
}

// TST-012 / REQ-07: unsupported mode falls back to a plain keyword query.
func TestTST012_TorznabModeMapping(t *testing.T) {
	cases := map[string]string{
		"search":       "search",
		"tv-search":    "tvsearch",
		"movie-search": "movie",
		"":             "search",
		"unknown":      "search",
	}
	for mode, want := range cases {
		if got := mapMode(mode); got != want {
			t.Errorf("mapMode(%q) = %q, want %q", mode, got, want)
		}
	}
}

// TST-033: magnet links keep their type and pass through untouched.
func TestTST033_ResolveDownloadMagnet(t *testing.T) {
	exec, _ := httpclient.New(nil, nil, "")
	conn := NewTorznab("x", "https://x.example", "", "", exec, 0)
	ref, err := conn.ResolveDownload(context.Background(), Release{
		Title:       "t",
		DownloadURL: "magnet:?xt=urn:btih:abc",
	})
	if err != nil {
		t.Fatalf("ResolveDownload: %v", err)
	}
	if ref.Type != "magnet" || ref.URL != "magnet:?xt=urn:btih:abc" {
		t.Fatalf("ref = %+v", ref)
	}
}

func TestTST033_ResolveDownloadMissingLink(t *testing.T) {
	exec, _ := httpclient.New(nil, nil, "")
	conn := NewTorznab("x", "https://x.example", "", "", exec, 0)
	if _, err := conn.ResolveDownload(context.Background(), Release{Title: "t"}); err == nil {
		t.Fatal("missing link must fail")
	}
}

func TestTST031_TorznabMalformedRSS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not xml at all"))
	}))
	defer srv.Close()

	exec, _ := httpclient.New(nil, nil, "")
	conn := NewTorznab("x", srv.URL, "", "", exec, 0)
	_, err := conn.Search(context.Background(), Query{Keyword: "x"})
	var cErr *Error
	if !asConnectorError(err, &cErr) || cErr.Kind != KindParse {
		t.Fatalf("kind = %v, want parse", err)
	}
	if !strings.Contains(cErr.Message, "解析") {
		t.Errorf("message = %q", cErr.Message)
	}
}

func asConnectorError(err error, target **Error) bool {
	for err != nil {
		if c, ok := err.(*Error); ok {
			*target = c
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
