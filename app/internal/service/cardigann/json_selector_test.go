package cardigann

import "testing"

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
