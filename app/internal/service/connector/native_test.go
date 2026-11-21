package connector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
)

// TST-007 / AC-07: the Knaben meta-search endpoint answers with capabilities.
func TestTST007_KnabenTest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["order_by"] != "date" {
			t.Errorf("order_by = %v", body["order_by"])
		}
		if body["hide_unsafe"] != true {
			t.Errorf("hide_unsafe = %v", body["hide_unsafe"])
		}
		_, _ = w.Write([]byte(`{"hits":[{"title":"One Piece 1080p","bytes":100,"seeders":5,"hash":"abc"}]}`))
	}))
	defer srv.Close()

	exec, _ := httpclient.New(nil, nil, "")
	conn := NewKnaben("Knaben", srv.URL, "", exec, 0)
	caps, err := conn.Test(context.Background())
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if caps.Name != "Knaben" || caps.Protocol != "torrent" {
		t.Fatalf("caps = %+v", caps)
	}
	if len(caps.SearchModes) != 3 {
		t.Errorf("modes = %v", caps.SearchModes)
	}
}

// TST-030 / AC-30: JSON hits map onto normalised releases with magnet links.
func TestTST030_KnabenSearch(t *testing.T) {
	payload := `{"hits":[
		{"title":"One Piece S23E05 1080p","categoryId":[6001000,6000000],"hash":"71C0AF05FC81270006B2B5978643990BC10EF80B",
		 "details":"https://nyaa.iss.ink/view/1","link":null,
		 "magnetUrl":"magnet:?xt=urn:btih:71c0af05fc81270006b2b5978643990bc10ef80b",
		 "bytes":1395864371,"seeders":204,"peers":7,"date":"2026-11-17T20:30:00+00:00"},
		{"title":"No Link Release","categoryId":[3000000],"hash":"","details":"x","link":"","magnetUrl":"",
		 "bytes":10,"seeders":1,"peers":0,"date":""}
	]}`
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotQuery, _ = body["query"].(string)
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	exec, _ := httpclient.New(nil, nil, "")
	conn := NewKnaben("Knaben", srv.URL, "", exec, 0)
	releases, err := conn.Search(context.Background(), Query{Keyword: "One Piece", Categories: []int{5000}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if gotQuery != "One Piece" {
		t.Errorf("query = %q", gotQuery)
	}
	if len(releases) != 1 {
		t.Fatalf("releases = %d, want 1 (rows without a link are skipped)", len(releases))
	}
	rel := releases[0]
	if rel.DownloadType != "magnet" {
		t.Errorf("type = %q", rel.DownloadType)
	}
	if rel.InfoHash != "71c0af05fc81270006b2b5978643990bc10ef80b" {
		t.Errorf("infohash = %q", rel.InfoHash)
	}
	if rel.Seeders != 204 || rel.SizeBytes != 1395864371 {
		t.Errorf("seeders/size = %d/%d", rel.Seeders, rel.SizeBytes)
	}
	if len(rel.Categories) == 0 || rel.Categories[0] != 5070 {
		t.Errorf("categories = %v, want anime 5070", rel.Categories)
	}
	if rel.PublishDate == "" {
		t.Error("publish date must be normalised")
	}
}

// TST-012 / REQ-07: category groups map onto Knaben category ids.
func TestTST012_KnabenCategoryMapping(t *testing.T) {
	cases := []struct {
		in   []int
		want int
	}{
		{[]int{2000}, 3000000},
		{[]int{2040}, 3000000},
		{[]int{5000}, 1000000},
		{[]int{5070}, 6000000},
	}
	for _, c := range cases {
		got, ok := knabenCategoriesFor(c.in)
		if !ok {
			t.Fatalf("%v: no mapping", c.in)
		}
		found := false
		for _, id := range got {
			if id == c.want {
				found = true
			}
		}
		if !found {
			t.Errorf("%v => %v, want to include %d", c.in, got, c.want)
		}
	}

	if !NewKnaben("k", "", "", nil, 0).SupportsCategories([]int{2000}) {
		t.Error("Knaben must support movies")
	}
	if !NewKnaben("k", "", "", nil, 0).SupportsCategories(nil) {
		t.Error("no category filter means supported")
	}
}

// A non-JSON body is a classified parse error.
func TestTST031_KnabenParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>nope</html>"))
	}))
	defer srv.Close()

	exec, _ := httpclient.New(nil, nil, "")
	conn := NewKnaben("k", srv.URL, "", exec, 0)
	_, err := conn.Search(context.Background(), Query{Keyword: "x"})
	var cErr *Error
	if !asError(err, &cErr) || cErr.Kind != KindParse {
		t.Fatalf("kind = %v, want parse", err)
	}
}

// TST-007: SubsPlease answers the latest feed.
func TestTST007_SubsPleaseTest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "f=latest") {
			t.Errorf("query = %q, want f=latest", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	exec, _ := httpclient.New(nil, nil, "")
	conn := NewSubsPlease("SubsPlease", srv.URL, "", exec, 0)
	caps, err := conn.Test(context.Background())
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if caps.Name != "SubsPlease" {
		t.Errorf("name = %q", caps.Name)
	}
}

// TST-030: the nested payload flattens into one release per resolution.
func TestTST030_SubsPleaseSearch(t *testing.T) {
	payload := `{"Sousou no Frieren S2":{"time":"05/29/26","release_date":"Fri, 29 May 2026 17:05:35 +0000",
		"show":"Sousou no Frieren S2","episode":"01-10","page":"sousou-no-frieren-s2",
		"downloads":[
			{"res":"1080","magnet":"magnet:?xt=urn:btih:ABC123&dn=x&xl=1395864371"},
			{"res":"720","magnet":"magnet:?xt=urn:btih:DEF456&dn=y&xl=734003200"}
		]}}`
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	exec, _ := httpclient.New(nil, nil, "")
	conn := NewSubsPlease("SubsPlease", srv.URL, "", exec, 0)
	releases, err := conn.Search(context.Background(), Query{Keyword: "[SubsPlease] Sousou no Frieren S2 1080p"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !strings.Contains(gotQuery, "f=search") {
		t.Errorf("query = %q, want a search", gotQuery)
	}
	if strings.Contains(gotQuery, "1080p") {
		t.Errorf("resolution must be stripped from the query: %q", gotQuery)
	}
	if len(releases) != 2 {
		t.Fatalf("releases = %d, want 2", len(releases))
	}
	first := releases[0]
	if first.DownloadType != "magnet" {
		t.Errorf("type = %q", first.DownloadType)
	}
	if first.SizeBytes != 1395864371 && first.SizeBytes != 734003200 {
		t.Errorf("size = %d", first.SizeBytes)
	}
	if len(first.Categories) == 0 || first.Categories[0] != 5070 {
		t.Errorf("categories = %v", first.Categories)
	}
	if !strings.HasPrefix(first.Title, "[SubsPlease] Sousou no Frieren S2 - 01-10 (") {
		t.Errorf("title = %q", first.Title)
	}
}

// The API returns `[]` when nothing matches; that is not an error.
func TestTST032_SubsPleaseEmptyArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	exec, _ := httpclient.New(nil, nil, "")
	conn := NewSubsPlease("sp", srv.URL, "", exec, 0)
	releases, err := conn.Search(context.Background(), Query{Keyword: "nothing"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(releases) != 0 {
		t.Fatalf("releases = %d, want 0", len(releases))
	}
}

func TestTST012_SubsPleaseCategories(t *testing.T) {
	conn := NewSubsPlease("sp", "", "", nil, 0)
	if !conn.SupportsCategories([]int{5070}) {
		t.Error("must support anime")
	}
	if !conn.SupportsCategories([]int{2000}) {
		t.Error("must support movies")
	}
	if conn.SupportsCategories([]int{4000}) {
		t.Error("must not claim PC games")
	}
}

// A magnet without an xl parameter falls back to the resolution estimate.
func TestTST030_SubsPleaseSizeFallback(t *testing.T) {
	if got := subPleaseSize("magnet:?xt=urn:btih:abc", "1080"); got != 1395864371 {
		t.Errorf("1080 estimate = %d", got)
	}
	if got := subPleaseSize("magnet:?xt=urn:btih:abc", "480"); got != 367001600 {
		t.Errorf("480 estimate = %d", got)
	}
	if got := subPleaseSize("magnet:?xt=urn:btih:abc&xl=999", "1080"); got != 999 {
		t.Errorf("explicit size = %d", got)
	}
}

// TST-058 / BUG-010: a manual session (cookie + UA) is attached to every
// request, including the search request.
func TestTST058_SessionAppliedToRequests(t *testing.T) {
	var gotCookie, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"hits":[]}`))
	}))
	defer srv.Close()

	exec, _ := httpclient.New(nil, nil, "")
	conn := NewKnaben("k", srv.URL, "", exec, 0)
	conn.SetSession(Session{Cookies: "cf_clearance=abc; PHPSESSID=xyz", UserAgent: "Browser/9.9"})

	if _, err := conn.Search(context.Background(), Query{Keyword: "x"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if gotCookie != "cf_clearance=abc; PHPSESSID=xyz" {
		t.Errorf("cookie = %q", gotCookie)
	}
	if gotUA != "Browser/9.9" {
		t.Errorf("ua = %q", gotUA)
	}
}

// The session is merged with a login cookie rather than replacing it.
func TestTST058_SessionMergesWithLoginCookie(t *testing.T) {
	headers := map[string]string{"Cookie": "session=login", "User-Agent": "old"}
	Session{Cookies: "cf_clearance=abc", UserAgent: "Browser/9.9"}.Apply(headers)

	if headers["Cookie"] != "session=login; cf_clearance=abc" {
		t.Errorf("cookie = %q", headers["Cookie"])
	}
	if headers["User-Agent"] != "Browser/9.9" {
		t.Errorf("ua = %q", headers["User-Agent"])
	}
}

// TST-021: category ids are labelled for the UI instead of being shown as numbers.
func TestTST021_CategoryNames(t *testing.T) {
	cases := map[int]string{
		2000: "电影",
		2045: "电影/4K",
		// 208 is The Pirate Bay's own numbering, not Newznab: until the definition
		// maps it, keep the raw value visible rather than mislabel it.
		208:   "其它（208）",
		5099:  "剧集", // unknown child of TV
		99999: "其它（99999）",
	}
	for id, want := range cases {
		if got := CategoryName(id); got != want {
			t.Errorf("CategoryName(%d) = %q, want %q", id, got, want)
		}
	}
	names := CategoryNames([]int{5040, 5040, 2000})
	if len(names) != 2 {
		t.Fatalf("names = %v, want deduped 2", names)
	}
}

// TST-033 / AC-35: details URLs survive merging so the UI can link out.
func TestTST033_DetailsURLKept(t *testing.T) {
	merged := GroupReleases([]Release{
		{Title: "Same", SizeBytes: 100, Seeders: 5, DetailsURL: "https://site/1"},
		{Title: "Same", SizeBytes: 101, Seeders: 9, DetailsURL: "https://site/2"},
	})
	if len(merged) != 1 {
		t.Fatalf("merged = %d", len(merged))
	}
	if merged[0].DetailsURL != "https://site/1" {
		t.Errorf("card details = %q", merged[0].DetailsURL)
	}
	if len(merged[0].Sources) != 2 || merged[0].Sources[1].DetailsURL != "https://site/2" {
		t.Errorf("sources = %+v", merged[0].Sources)
	}
	if len(merged[0].CategoryNames) == 0 && len(merged[0].Categories) == 0 {
		t.Log("no categories on this fixture (expected)")
	}
}
