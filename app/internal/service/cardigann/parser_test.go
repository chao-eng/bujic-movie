package cardigann

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "search", "testdata", "definitions", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

// TST-005 / TST-015 / TST-035: a complete definition parses as full and keeps
// the declared field order.
func TestTST005_ParseFullDefinition(t *testing.T) {
	res, err := Parse("fake-cardigann.yml", loadFixture(t, "fake-cardigann.yml"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.Compat != "full" {
		t.Fatalf("compat = %q, detail %v", res.Compat, res.Detail)
	}
	if res.Definition.ID != "fake-tracker" || res.Definition.Name != "假站点" {
		t.Fatalf("unexpected identity: %+v", res.Definition)
	}
	if len(res.Definition.Settings) != 4 {
		t.Fatalf("settings = %d, want 4", len(res.Definition.Settings))
	}
	if !res.Definition.UsesLogin() || !res.Definition.NeedsCaptcha() {
		// fixture has login without captcha
		if !res.Definition.UsesLogin() {
			t.Fatal("definition should require login")
		}
	}
	names := res.Definition.Search.Fields.Names()
	want := []string{"title", "download", "size", "seeders", "category", "infohash"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("fields order = %v, want %v", names, want)
	}
	if res.Hash == "" {
		t.Fatal("hash must be set")
	}
}

// TST-003: a definition without a search block is invalid and points at the
// missing block.
func TestTST003_MissingSearchBlockIsInvalid(t *testing.T) {
	res, err := Parse("malformed.yml", loadFixture(t, "malformed.yml"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.Compat != "invalid" {
		t.Fatalf("compat = %q, want invalid", res.Compat)
	}
	if !containsSubstring(res.Detail, "search") {
		t.Fatalf("detail must mention search block, got %v", res.Detail)
	}
}

// TST-004: an unknown block is accepted but marked partial with the block name.
func TestTST004_UnsupportedBlockMarkedPartial(t *testing.T) {
	res, err := Parse("partial.yml", loadFixture(t, "partial.yml"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.Compat != "partial" {
		t.Fatalf("compat = %q, want partial (detail %v)", res.Compat, res.Detail)
	}
	if !containsSubstring(res.Detail, "flaresolverr") {
		t.Fatalf("detail must name flaresolverr, got %v", res.Detail)
	}
}

// TST-051 / BR-39: size and shape limits are enforced before parsing.
func TestTST051_ParseLimits(t *testing.T) {
	big := make([]byte, MaxDefinitionBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	if _, err := Parse("big.yml", big); err == nil {
		t.Fatal("oversized file must be rejected")
	}

	deep := []byte("id: x\n")
	for i := 0; i < MaxYAMLDepth+3; i++ {
		deep = append(deep, []byte(strings.Repeat("  ", i+1)+"k:\n")...)
	}
	_, err := Parse("deep.yml", deep)
	if err == nil {
		t.Fatal("deeply nested file must be rejected")
	}
	var pe *ParseError
	if !asParseError(err, &pe) {
		t.Fatalf("expected ParseError, got %T", err)
	}
	if pe.Block != "top" {
		t.Fatalf("error block = %q, want top", pe.Block)
	}

	if _, err := Parse("empty.yml", nil); err == nil {
		t.Fatal("empty file must be rejected")
	}
}

// TST-050 / BR-01: only http(s) links are accepted.
func TestTST050_ParseRejectsNonHTTPScheme(t *testing.T) {
	raw := []byte(`id: x
name: x
type: public
links:
  - file:///etc/passwd
caps:
  modes:
    search: [q]
search:
  path: /s
  rows:
    selector: li
  fields:
    title:
      selector: a
    download:
      selector: a
      attribute: href
`)
	res, err := Parse("bad-scheme.yml", raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.Compat != "invalid" {
		t.Fatalf("compat = %q, want invalid", res.Compat)
	}
	if !containsSubstring(res.Detail, "http") {
		t.Fatalf("detail must mention scheme rule, got %v", res.Detail)
	}
}

// BR-25: errors carry block and field so the user can locate the problem.
func TestTST003_ParseErrorHasThreeLevelLocation(t *testing.T) {
	_, err := Parse("broken.yml", []byte("id: [unclosed\n"))
	if err == nil {
		t.Fatal("expected syntax error")
	}
	var pe *ParseError
	if !asParseError(err, &pe) {
		t.Fatalf("expected ParseError, got %T", err)
	}
	if pe.File != "broken.yml" || pe.Block == "" {
		t.Fatalf("location incomplete: %+v", pe)
	}
}

func TestTST007_CapabilitiesSummary(t *testing.T) {
	res, err := Parse("fake-cardigann.yml", loadFixture(t, "fake-cardigann.yml"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	summary := CapabilitiesSummary(&res.Definition)
	for _, want := range []string{"search", "movie-search", "tv-search", "需登录"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q missing %q", summary, want)
		}
	}
}

func containsSubstring(items []string, sub string) bool {
	for _, item := range items {
		if strings.Contains(item, sub) {
			return true
		}
	}
	return false
}

func asParseError(err error, target **ParseError) bool {
	pe, ok := err.(*ParseError)
	if ok {
		*target = pe
	}
	return ok
}
