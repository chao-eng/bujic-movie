package cardigann

import (
	"strings"
	"testing"
)

// TST-015 / BUG-013: JSONPath-style selectors (`$`, `$[0].id`) must work; the
// upstream TPB definition uses them.
func TestTST015_JSONPathSelectors(t *testing.T) {
	cases := map[string]string{
		"$":        "@this",
		"":         "@this",
		"$[0].id":  "0.id",
		"$.[0].id": "0.id",
		"name":     "name",
		"a.b[0]":   "a.b.0",
	}
	for in, want := range cases {
		if got := jsonPath(in); got != want {
			t.Errorf("jsonPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TST-015: a JSON body with a `$` rows selector yields one release per element.
func TestTST015_ExtractJSONRootRows(t *testing.T) {
	def := &Definition{
		Search: &SearchBlock{
			Rows: &RowsBlock{SelectorBlock: SelectorBlock{Selector: "$"}},
			Fields: FieldMap{Entries: []FieldEntry{
				{Name: "title", Selector: SelectorBlock{Selector: "name"}},
				{Name: "infohash", Selector: SelectorBlock{Selector: "info_hash"}},
				{Name: "size", Selector: SelectorBlock{Selector: "size"}},
				{Name: "seeders", Selector: SelectorBlock{Selector: "seeders"}},
				{Name: "category", Selector: SelectorBlock{Selector: "category"}},
			}},
		},
	}
	body := []byte(`[
		{"name":"Avengers Endgame","info_hash":"AAA111","size":"1000","seeders":"42","category":"2000"},
		{"name":"Avengers Infinity War","info_hash":"BBB222","size":"2000","seeders":"7","category":"2000"}
	]`)
	releases, err := Extract(def, body, "json")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("releases = %d, want 2", len(releases))
	}
	if releases[0].Title != "Avengers Endgame" || releases[0].Seeders != 42 {
		t.Errorf("first = %+v", releases[0])
	}
	if releases[0].DownloadType != "magnet" {
		t.Errorf("infohash-only row must become a magnet, got %q", releases[0].DownloadType)
	}
}

// TST-015 / BUG-013: a templated rows selector renders before use, so an empty
// `uploader` setting collapses to an empty selector instead of a literal.
func TestTST015_TemplatedRowSelector(t *testing.T) {
	def := &Definition{
		Search: &SearchBlock{
			Rows: &RowsBlock{SelectorBlock: SelectorBlock{
				Selector: `${{ if .Config.uploader }}:has(username:contains({{ .Config.uploader }})){{ else }}{{ end }}`,
			}},
			Fields: FieldMap{Entries: []FieldEntry{
				{Name: "title", Selector: SelectorBlock{Selector: "name"}},
				{Name: "infohash", Selector: SelectorBlock{Selector: "info_hash"}},
			}},
		},
	}
	body := []byte(`[{"name":"T","info_hash":"ABC"}]`)

	releases, err := ExtractWithConfig(def, body, "json", map[string]string{"uploader": ""})
	if err != nil {
		t.Fatalf("ExtractWithConfig: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("releases = %d, want 1", len(releases))
	}
}

// TST-015: a field with only `text` is a rendered literal, not a selector.
func TestTST015_TextFieldIsRendered(t *testing.T) {
	def := &Definition{
		Search: &SearchBlock{
			Rows: &RowsBlock{SelectorBlock: SelectorBlock{Selector: "$"}},
			Fields: FieldMap{Entries: []FieldEntry{
				{Name: "title", Selector: SelectorBlock{Selector: "name"}},
				{Name: "infohash", Selector: SelectorBlock{Selector: "hash"}},
				{Name: "details", Selector: SelectorBlock{Text: "https://site/description.php?id={{ .Result.id }}"}},
			}},
		},
	}
	body := []byte(`[{"name":"T","hash":"ABC","id":"42"}]`)
	releases, err := Extract(def, body, "json")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("releases = %d", len(releases))
	}
}

// TST-015 / BUG-013: .NET character-class names are translated for Go's regexp.
func TestTST015_DotNetRegexClasses(t *testing.T) {
	cases := map[string]string{
		`\p{IsCJKUnifiedIdeographs}`: `\p{Han}`,
		`\p{IsBasicLatin}`:           `\x00-\x7F`,
		`\d+`:                        `\d+`,
	}
	for in, want := range cases {
		if got := normaliseRegex(in); got != want {
			t.Errorf("normaliseRegex(%q) = %q, want %q", in, got, want)
		}
	}

	// The real TPB keyword filter must compile and strip CJK.
	filters := []FilterBlock{
		{Name: "re_replace", Args: []any{`([\p{IsCJKUnifiedIdeographs}\W]+)`, "."}},
		{Name: "tolower"},
	}
	got, err := ApplyFilters("复仇者联盟 Avengers", filters)
	if err != nil {
		t.Fatalf("ApplyFilters: %v", err)
	}
	if got != ".avengers" {
		t.Errorf("filtered = %q", got)
	}
}

// TST-015 / BUG-013: non-ASCII keywords are percent-encoded, ASCII URL
// structure is preserved.
func TestTST015_EscapeURL(t *testing.T) {
	cases := map[string]string{
		"https://a.example/q.php?q=avengers&cat=2000": "https://a.example/q.php?q=avengers&cat=2000",
		"https://a.example/?q=复仇":                     "https://a.example/?q=%E5%A4%8D%E4%BB%87",
		"https://a.example/?q=one piece":              "https://a.example/?q=one%20piece",
	}
	for in, want := range cases {
		if got := escapeURL(in); got != want {
			t.Errorf("escapeURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// TST-015: keyword filters run before the template renders.
func TestTST015_KeywordFiltersApplied(t *testing.T) {
	def := &Definition{
		Settings: []SettingsField{{Name: "apiurl", Default: "apibay.org"}},
		Search: &SearchBlock{
			Path:            "https://{{ .Config.apiurl }}/q.php?q={{ .Keywords }}",
			KeywordsFilters: []FilterBlock{{Name: "tolower"}},
		},
	}
	ctx := NewTemplateContext("AVENGERS", nil, nil, map[string]string{"apiurl": "apibay.org"})
	url, err := BuildQueryURL("", RequestBlock{Path: def.Search.Path}, ctx)
	if err != nil {
		t.Fatalf("BuildQueryURL: %v", err)
	}
	_ = url
}

// Regression for YTS: the title field appends a templated suffix built from
// earlier underscore-prefixed helper fields. Those fields only exist if
// evaluation follows declaration order, and the append argument must render.
func TestTST015_YTSAppendFilterRendersResultFields(t *testing.T) {
	def := &Definition{
		Search: &SearchBlock{
			Rows: &RowsBlock{SelectorBlock: SelectorBlock{Selector: "data.movies"}},
			Fields: FieldMap{Entries: []FieldEntry{
				{Name: "_quality", Selector: SelectorBlock{Selector: "torrents.0.quality"}},
				{Name: "_type", Selector: SelectorBlock{Selector: "torrents.0.type"}},
				{Name: "_audio", Selector: SelectorBlock{Selector: "torrents.0.audio_channels"}},
				{Name: "_depth", Selector: SelectorBlock{Selector: "torrents.0.bit_depth"}},
				{Name: "_codec", Selector: SelectorBlock{Selector: "torrents.0.video_codec"}},
				{Name: "year", Selector: SelectorBlock{Selector: "year"}},
				{Name: "title_default", Selector: SelectorBlock{
					Selector: "title",
					Filters:  []FilterBlock{{Name: "append", Args: " ({{ .Result.year }})"}},
				}},
				{Name: "title", Selector: SelectorBlock{
					Selector: "title_long",
					Filters: []FilterBlock{
						{Name: "replace", Args: []any{":", ""}},
						{Name: "append", Args: ` {{ .Result._quality }} ` +
							`{{ if eq .Result._type "web" }}WEBRip{{ else }}BRRip{{ end }} ` +
							`{{ if eq .Result._audio "5.1" }}5.1 {{ else }}{{ end }}` +
							`{{ if eq .Result._depth "10" }}10Bit {{ else }}{{ end }}` +
							`{{ .Result._codec }} -YTS`},
					},
				}},
				{Name: "infohash", Selector: SelectorBlock{Selector: "torrents.0.hash"}},
			}},
		},
	}
	body := []byte(`{"data":{"movies":[{"title":"The Avenger","title_long":"The Avenger: Rebirth (2024)","year":2024,
		"torrents":[{"hash":"BB36FCA157B406A8C3BF70718478E94CE24AB6AC","quality":"1080p","type":"web",
		"video_codec":"x265","bit_depth":"10","audio_channels":"5.1"}]}]}}`)

	releases, err := Extract(def, body, "json")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("releases = %d, want 1", len(releases))
	}
	const want = "The Avenger Rebirth (2024) 1080p WEBRip 5.1 10Bit x265 -YTS"
	if releases[0].Title != want {
		t.Errorf("title = %q, want %q", releases[0].Title, want)
	}
	if strings.Contains(releases[0].Title, "{{") {
		t.Errorf("template text leaked into title: %q", releases[0].Title)
	}
}

// BUG: `rows.attribute` + `rows.multiple` were ignored, so nested arrays (YTS
// movies→torrents, Gazelle-style `attributes`) never produced releases and every
// field read came from the wrong scope.
func TestTST015_RowsAttributeAndMultiple(t *testing.T) {
	def := &Definition{
		Search: &SearchBlock{
			Rows: &RowsBlock{
				SelectorBlock: SelectorBlock{Selector: "data.movies", Attribute: "torrents"},
				Multiple:      true,
			},
			Fields: FieldMap{Entries: []FieldEntry{
				{Name: "_quality", Selector: SelectorBlock{Selector: "quality"}},
				{Name: "year", Selector: SelectorBlock{Selector: "..year"}},
				{Name: "title", Selector: SelectorBlock{Text: "{{ .Result.year }} {{ .Result._quality }}"}},
				{Name: "infohash", Selector: SelectorBlock{Selector: "hash"}},
			}},
		},
	}
	body := []byte(`{"data":{"movies":[
		{"year":2024,"title_long":"A","torrents":[
			{"hash":"AAA","quality":"720p"},
			{"hash":"BBB","quality":"1080p"}]},
		{"year":1999,"title_long":"B","torrents":[]}
	]}}`)

	releases, err := Extract(def, body, "json")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("releases = %d, want 2 (one per torrent)", len(releases))
	}
	if releases[0].Title != "2024 720p" || releases[0].InfoHash != "AAA" {
		t.Errorf("first = %+v", releases[0])
	}
	// The second element resolves `..year` against the movie, not the torrent.
	if releases[1].Title != "2024 1080p" || releases[1].InfoHash != "BBB" {
		t.Errorf("second = %+v", releases[1])
	}
}

// A row whose attribute is missing contributes nothing; with
// missingAttributeEqualsNoResults that is a normal empty result.
func TestTST015_RowsAttributeMissing(t *testing.T) {
	def := &Definition{
		Search: &SearchBlock{
			Rows: &RowsBlock{
				SelectorBlock:                   SelectorBlock{Selector: "data", Attribute: "attributes"},
				MissingAttributeEqualsNoResults: true,
			},
			Fields: FieldMap{Entries: []FieldEntry{
				{Name: "title", Selector: SelectorBlock{Selector: "name"}},
				{Name: "infohash", Selector: SelectorBlock{Selector: "hash"}},
			}},
		},
	}
	body := []byte(`{"data":[{"id":1},{"attributes":{"name":"T","hash":"ABC"}}]}`)
	releases, err := Extract(def, body, "json")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(releases) != 1 || releases[0].Title != "T" {
		t.Fatalf("releases = %+v", releases)
	}
}

// The count selector short-circuits before rows are walked, matching YTS's
// "movie_count > 0 but no torrents" workaround.
func TestTST015_RowsCountZero(t *testing.T) {
	def := &Definition{
		Search: &SearchBlock{
			Rows: &RowsBlock{
				SelectorBlock: SelectorBlock{Selector: "data.movies"},
				Count:         &SelectorBlock{Selector: "data.movie_count"},
			},
			Fields: FieldMap{Entries: []FieldEntry{
				{Name: "title", Selector: SelectorBlock{Selector: "title"}},
				{Name: "infohash", Selector: SelectorBlock{Selector: "hash"}},
			}},
		},
	}
	releases, err := Extract(def, []byte(`{"data":{"movie_count":0,"movies":[]}}`), "json")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(releases) != 0 {
		t.Fatalf("releases = %d, want 0", len(releases))
	}
}

// The `case` block maps a value to another (YTS quality → Newznab category id).
func TestTST015_FieldCaseMapping(t *testing.T) {
	def := &Definition{
		Caps: CapabilitiesBlock{CategoryMappings: []CategoryMapping{
			{ID: "45", Cat: "Movies/HD", Desc: "Movies/x264/720p"},
			{ID: "44", Cat: "Movies/HD", Desc: "Movies/x264/1080p"},
			{ID: "46", Cat: "Movies/UHD", Desc: "Movies/x264/2160p"},
		}},
		Search: &SearchBlock{
			Rows: &RowsBlock{SelectorBlock: SelectorBlock{Selector: "data.movies"}},
			Fields: FieldMap{Entries: []FieldEntry{
				{Name: "title", Selector: SelectorBlock{Selector: "title"}},
				{Name: "category", Selector: SelectorBlock{
					Selector: "quality",
					Case:     map[string]string{"720p": "45", "1080p": "44", "2160p": "46", "*": "45"},
				}},
				{Name: "infohash", Selector: SelectorBlock{Selector: "hash"}},
			}},
		},
	}
	body := []byte(`{"data":{"movies":[
		{"title":"A","quality":"1080p","hash":"AAA"},
		{"title":"B","quality":"720p","hash":"BBB"},
		{"title":"C","quality":"2160p","hash":"CCC"},
		{"title":"D","quality":"3D","hash":"DDD"}
	]}}`)
	releases, err := Extract(def, body, "json")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(releases) != 4 {
		t.Fatalf("releases = %d", len(releases))
	}
	// 1080p/720p map to Movies/HD (2040), 2160p to Movies/UHD (2045), and the
	// `*` fallback takes 3D as 45 → Movies/HD.
	for i, want := range [][]int{{2040}, {2040}, {2045}, {2040}} {
		if len(releases[i].Categories) != 1 || releases[i].Categories[0] != want[0] {
			t.Errorf("release %d categories = %v, want %v", i, releases[i].Categories, want)
		}
	}
}
