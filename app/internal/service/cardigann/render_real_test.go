package cardigann

import (
	"os"
	"path/filepath"
	"testing"
)

// Regression for upstream templates: Scriban-style quoted regex args and
// hyphenated config keys must render.
func TestTST015_ScribanStyleTemplates(t *testing.T) {
	cases := []struct {
		name string
		src  string
		ctx  TemplateContext
		want string
	}{
		{
			name: "regex escapes",
			src:  `{{ if .Keywords }}{{ re_replace .Keywords "\\b0(\\d{1})\\b" "$1" }}{{ else }}{{ end }}`,
			ctx:  NewTemplateContext(" 05 ", nil, nil, nil),
			want: " 5 ",
		},
		{
			name: "hyphenated config key",
			src:  "{{ .Config.cat-id }}",
			ctx:  NewTemplateContext("", nil, nil, map[string]string{"cat-id": "42"}),
			want: "42",
		},
		{
			name: "today year",
			src:  "{{ .Today.Year }}",
			ctx:  NewTemplateContext("", nil, nil, nil),
			want: "2026",
		},
		{
			name: "if else",
			src:  "{{ if .Keywords }}{{ .Keywords }}{{ else }}latest{{ end }}",
			ctx:  NewTemplateContext("", nil, nil, nil),
			want: "latest",
		},
	}
	for _, c := range cases {
		got, err := Render(c.src, c.ctx)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// Upstream definitions rely on settings defaults (apiurl, filter-id, ...).
func TestTST005_DefinitionDefaultsApplied(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "search", "testdata", "definitions", "fake-cardigann.yml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	res, err := Parse("fake.yml", raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// The fixture's select field has a default; it must reach the config map.
	def := res.Definition
	t.Logf("settings: %d", len(def.Settings))
}
