package cardigann

import (
	"os"
	"path/filepath"
	"testing"
)

func loadSearchFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "search", "testdata", "cardigann", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

// TST-005 / TST-015: HTML rows are extracted into normalised releases.
func TestTST005_ExtractHTMLRows(t *testing.T) {
	res, err := Parse("fake-cardigann.yml", loadFixture(t, "fake-cardigann.yml"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	releases, err := Extract(&res.Definition, loadSearchFixture(t, "search.html"), "html")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(releases) != 3 {
		t.Fatalf("releases = %d, want 3", len(releases))
	}
	first := releases[0]
	if first.Title != "示例电影 2026 2160p" {
		t.Errorf("title = %q", first.Title)
	}
	if first.DownloadURL != "/download/1.torrent" {
		t.Errorf("download url = %q", first.DownloadURL)
	}
	if first.Seeders != 42 {
		t.Errorf("seeders = %d, want 42", first.Seeders)
	}
	if first.SizeBytes != 12*(1<<30) {
		t.Errorf("size = %d, want 12GiB", first.SizeBytes)
	}
	if len(first.Categories) != 1 || first.Categories[0] != 2045 {
		t.Errorf("categories = %v, want [2045]", first.Categories)
	}
	if first.DownloadType != "torrent" {
		t.Errorf("download type = %q", first.DownloadType)
	}
}

// TST-032 / BR-15: every row yields a release; truncation is the caller's job.
func TestTST032_ExtractKeepsAllRows(t *testing.T) {
	res, err := Parse("fake-cardigann.yml", loadFixture(t, "fake-cardigann.yml"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	releases, err := Extract(&res.Definition, loadSearchFixture(t, "search.html"), "")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(releases) != 3 {
		t.Fatalf("releases = %d", len(releases))
	}
	if releases[2].Seeders != 0 {
		t.Errorf("zero seeders row must keep 0, got %d", releases[2].Seeders)
	}
}

// TST-031: a malformed body surfaces a field-level error instead of panicking.
func TestTST031_ExtractMalformedBody(t *testing.T) {
	res, err := Parse("fake-cardigann.yml", loadFixture(t, "fake-cardigann.yml"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, err := Extract(&res.Definition, nil, "html"); err == nil {
		t.Fatal("empty body must return an error")
	}
	// Broken HTML is tolerated by goquery and simply yields no rows.
	if rels, err := Extract(&res.Definition, []byte("<div>nothing here</div>"), "html"); err != nil || len(rels) != 0 {
		t.Fatalf("broken html: got %d releases err %v", len(rels), err)
	}
}

// TST-030: infohash-only rows become magnet links so dedupe can use the hash.
func TestTST030_ExtractBuildsMagnetFromInfoHash(t *testing.T) {
	def := &Definition{
		Search: &SearchBlock{
			Rows: &RowsBlock{SelectorBlock: SelectorBlock{Selector: "tr"}},
			Fields: FieldMap{Entries: []FieldEntry{
				{Name: "title", Selector: SelectorBlock{Selector: "td.title"}},
				{Name: "infohash", Selector: SelectorBlock{Selector: "td.hash"}},
				{Name: "size", Selector: SelectorBlock{Selector: "td.size"}},
			}},
		},
	}
	html := []byte(`<table><tr>
		<td class="title">Only Hash Release</td>
		<td class="hash">0123456789abcdef0123456789abcdef01234567</td>
		<td class="size">1 GB</td>
	</tr></table>`)
	releases, err := Extract(def, html, "html")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("releases = %d", len(releases))
	}
	if releases[0].DownloadType != "magnet" {
		t.Fatalf("type = %q, want magnet", releases[0].DownloadType)
	}
	if releases[0].DownloadURL != "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Only Hash Release" {
		t.Fatalf("magnet = %q", releases[0].DownloadURL)
	}
}

func TestTST005_ParseSizeVariants(t *testing.T) {
	cases := map[string]int64{
		"1073741824": 1073741824,
		"1 GB":       1 << 30,
		"1.5 GB":     int64(1.5 * float64(1<<30)),
		"700 MB":     700 << 20,
		"12,5 MiB":   0, // unknown suffix stays zero rather than guessing
		"":           0,
	}
	for in, want := range cases {
		if got := parseSize(in); got != want {
			t.Errorf("parseSize(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestTST005_DetectResponseType(t *testing.T) {
	cases := []struct {
		declared string
		body     string
		want     string
	}{
		{"", `{"a":1}`, "json"},
		{"", `<?xml version="1.0"?><rss></rss>`, "xml"},
		{"", `<html></html>`, "html"},
		{"json", `<html></html>`, "json"},
	}
	for _, c := range cases {
		if got := detectResponseType(c.declared, []byte(c.body)); got != c.want {
			t.Errorf("detect(%q,%q) = %q, want %q", c.declared, c.body, got, c.want)
		}
	}
}
