package cardigann

import (
	"errors"
	"strings"
	"testing"
)

// TST-016 / REQ-09: every whitelisted filter is implemented and unknown names
// are reported instead of silently ignored.
func TestTST016_FilterWhitelistIsComplete(t *testing.T) {
	samples := map[string]FilterBlock{
		"trim":          {Name: "trim"},
		"tolower":       {Name: "tolower"},
		"toupper":       {Name: "toupper"},
		"prepend":       {Name: "prepend", Args: "p-"},
		"append":        {Name: "append", Args: "-s"},
		"replace":       {Name: "replace", Args: []any{"a", "b"}},
		"re_replace":    {Name: "re_replace", Args: []any{`\d+`, "N"}},
		"regexp":        {Name: "regexp", Args: []any{`(\d+)`, "1"}},
		"split":         {Name: "split", Args: []any{"|", "1"}},
		"base64encode":  {Name: "base64encode"},
		"base64decode":  {Name: "base64decode"},
		"urlencode":     {Name: "urlencode"},
		"urldecode":     {Name: "urldecode"},
		"htmlencode":    {Name: "htmlencode"},
		"htmldecode":    {Name: "htmldecode"},
		"querystring":   {Name: "querystring", Args: "id"},
		"dateparse":     {Name: "dateparse", Args: []any{"2006-01-02"}},
		"timeparse":     {Name: "timeparse", Args: []any{"2006-01-02 15:04:05", "UTC"}},
		"timeago":       {Name: "timeago"},
		"reltime":       {Name: "reltime"},
		"fuzzytime":     {Name: "fuzzytime"},
		"validfilename": {Name: "validfilename"},
		"diacritics":    {Name: "diacritics"},
		"jsonjoinarray": {Name: "jsonjoinarray"},
		"hexdump":       {Name: "hexdump"},
		"strdump":       {Name: "strdump"},
		"validate":      {Name: "validate", Args: []any{`^\d+$`, "in"}},
		"and":           {Name: "and", Args: []any{"true"}},
		"or":            {Name: "or", Args: []any{"true"}},
		"eq":            {Name: "eq", Args: []any{"abc"}},
		"ne":            {Name: "ne", Args: []any{"zzz"}},
		"has":           {Name: "has", Args: []any{"1"}},
		"not":           {Name: "not"},
		"contains":      {Name: "contains", Args: []any{"a"}},
	}
	for name := range KnownFilters {
		if _, ok := samples[name]; !ok {
			t.Errorf("filter %q is whitelisted but has no test sample", name)
		}
	}
	for name, f := range samples {
		if _, err := ApplyFilters("abc123", []FilterBlock{f}); err != nil {
			t.Errorf("filter %q failed: %v", name, err)
		}
	}
}

func TestTST016_UnknownFilterReportsName(t *testing.T) {
	_, err := ApplyFilters("x", []FilterBlock{{Name: "definitely_not_real"}})
	if err == nil {
		t.Fatal("unknown filter must return an error")
	}
	if !errors.Is(err, ErrUnknownFilter) {
		t.Fatalf("error must wrap ErrUnknownFilter, got %v", err)
	}
}

func TestTST016_FilterChainOrder(t *testing.T) {
	got, err := ApplyFilters("  Hello World  ", []FilterBlock{
		{Name: "trim"},
		{Name: "tolower"},
		{Name: "replace", Args: []any{" ", "-"}},
	})
	if err != nil {
		t.Fatalf("ApplyFilters: %v", err)
	}
	if got != "hello-world" {
		t.Fatalf("got %q", got)
	}
}

func TestTST016_FilterDetails(t *testing.T) {
	cases := []struct {
		name string
		in   string
		f    FilterBlock
		want string
	}{
		{"regexp group", "abc123", FilterBlock{Name: "regexp", Args: []any{`(\d+)`, "1"}}, "123"},
		{"split index", "a|b|c", FilterBlock{Name: "split", Args: []any{"|", "2"}}, "c"},
		{"base64 roundtrip", "MTIz", FilterBlock{Name: "base64decode"}, "123"},
		{"urlencode", "a b", FilterBlock{Name: "urlencode"}, "a+b"},
		{"htmldecode", "a&amp;b", FilterBlock{Name: "htmldecode"}, "a&b"},
		{"querystring", "?id=42&x=1", FilterBlock{Name: "querystring", Args: "id"}, "42"},
		{"dateparse iso", "2026-11-10", FilterBlock{Name: "dateparse", Args: []any{"2006-01-02"}}, "2026-11-10T00:00:00Z"},
		{"validfilename", `a/b:c*d`, FilterBlock{Name: "validfilename"}, "a b c d"},
		{"diacritics", "café", FilterBlock{Name: "diacritics"}, "cafe"},
		{"validate drop", "abc", FilterBlock{Name: "validate", Args: []any{`^\d+$`, "in"}}, ""},
		{"validate keep", "123", FilterBlock{Name: "validate", Args: []any{`^\d+$`, "in"}}, "123"},
		{"contains", "1080p", FilterBlock{Name: "contains", Args: []any{"1080"}}, "true"},
		{"not", "true", FilterBlock{Name: "not"}, "false"},
		{"eq", "AbC", FilterBlock{Name: "eq", Args: []any{"abc"}}, "true"},
		{"ne", "abc", FilterBlock{Name: "ne", Args: []any{"abc"}}, "false"},
	}
	for _, c := range cases {
		got, err := ApplyFilters(c.in, []FilterBlock{c.f})
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// BR-10: filters never execute external commands or touch the filesystem.
func TestTST071_FiltersArePure(t *testing.T) {
	inputs := []string{"$(rm -rf /)", "; cat /etc/passwd", "../../etc/passwd"}
	for _, in := range inputs {
		out, err := ApplyFilters(in, []FilterBlock{{Name: "validfilename"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// The value may still contain shell-looking text; what matters is that
		// path separators are gone so it cannot address a real file.
		if strings.ContainsAny(out, `/\`) {
			t.Errorf("path separators survived sanitisation: %q", out)
		}
	}
}

// YTS builds its title suffix in a filter argument. Template placeholders there
// must render against the instance config and the row's earlier fields,
// including nested if/else.
func TestTST016_FilterArgsRenderTemplates(t *testing.T) {
	ctx := NewTemplateContext("", nil, nil, map[string]string{"sitelink": "https://yts.example/"})
	ctx = ctx.ForResult(map[string]string{
		"_quality": "720p",
		"_type":    "web",
		"_audio":   "5.1",
		"_depth":   "10",
		"_codec":   "x264",
		"year":     "2023",
	})

	filters := []FilterBlock{
		{Name: "replace", Args: []any{":", ""}},
		{Name: "append", Args: ` {{ .Result._quality }} ` +
			`{{ if eq .Result._type "web" }}WEBRip{{ else }}BRRip{{ end }} ` +
			`{{ if eq .Result._audio "5.1" }}5.1 {{ else }}{{ end }}` +
			`{{ if eq .Result._depth "10" }}10Bit {{ else }}{{ end }}` +
			`{{ .Result._codec }} -YTS`},
	}

	got, err := ApplyFiltersWithContext("Avatar: The Way of Water (2022)", filters, ctx)
	if err != nil {
		t.Fatalf("ApplyFiltersWithContext: %v", err)
	}
	const want = "Avatar The Way of Water (2022) 720p WEBRip 5.1 10Bit x264 -YTS"
	if got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("template text leaked into title: %q", got)
	}

	// The non-web / 2.0 / 8-bit branch takes the else sides.
	ctx.Result["_type"] = "bluray"
	ctx.Result["_audio"] = "2.0"
	ctx.Result["_depth"] = "8"
	got, err = ApplyFiltersWithContext("T", filters[1:], ctx)
	if err != nil {
		t.Fatalf("ApplyFiltersWithContext: %v", err)
	}
	if want := "T 720p BRRip x264 -YTS"; got != want {
		t.Errorf("else branch = %q, want %q", got, want)
	}
}

// The replacement half of replace/re_replace carries templates too (YTS fixes
// old mirror domains with `.Config.sitelink`).
func TestTST016_ReplaceArgsRenderTemplates(t *testing.T) {
	ctx := NewTemplateContext("", nil, nil, map[string]string{"sitelink": "https://yts.gg/"})

	got, err := ApplyFiltersWithContext(
		"https://yts.mx/movies/the-avenger-2024",
		[]FilterBlock{{Name: "re_replace", Args: []any{`^https?:\/\/yts\.(mx|lt|bz|gg)\/`, "{{ .Config.sitelink }}"}}},
		ctx,
	)
	if err != nil {
		t.Fatalf("ApplyFiltersWithContext: %v", err)
	}
	if want := "https://yts.gg/movies/the-avenger-2024"; got != want {
		t.Errorf("re_replace = %q, want %q", got, want)
	}

	// The pattern half is never rendered: a regex must stay literal.
	got, err = ApplyFiltersWithContext("a{{b}}c", []FilterBlock{
		{Name: "replace", Args: []any{"{{b}}", "{{ .Config.sitelink }}"}},
	}, ctx)
	if err != nil {
		t.Fatalf("ApplyFiltersWithContext: %v", err)
	}
	if want := "ahttps://yts.gg/c"; got != want {
		t.Errorf("replace = %q, want %q", got, want)
	}
}

// Without a context the arguments stay literal, so existing callers keep their
// behaviour.
func TestTST016_ApplyFiltersWithoutContextKeepsLiteralArgs(t *testing.T) {
	got, err := ApplyFilters("x", []FilterBlock{{Name: "append", Args: " {{ .Result._quality }}"}})
	if err != nil {
		t.Fatalf("ApplyFilters: %v", err)
	}
	if want := "x {{ .Result._quality }}"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Bug: the list form of `validate` used a single argument and read args[1]
// unconditionally, so 13 definitions panicked. It keeps only the tokens that
// also appear in the argument list, joined by ", " (upstream behaviour).
func TestTST016_ValidateListForm(t *testing.T) {
	args := "Action, Adventure, Animation, Comedy, Crime, Documentary, Drama, Family, Fantasy"
	cases := []struct{ in, want string }{
		{"Action", "action"},
		{"Action, Drama, Nonsense", "action, drama"},
		{"drama | action", "drama, action"},
		{"SciFi, Crime", "crime"},
		{"Nonsense", ""},
		{"", ""},
	}
	for _, c := range cases {
		got, err := ApplyFilters(c.in, []FilterBlock{{Name: "validate", Args: args}})
		if err != nil {
			t.Errorf("validate(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("validate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The two-argument regexp form keeps working: "in" keeps matches, anything else
// keeps non-matches.
func TestTST016_ValidateRegexpForm(t *testing.T) {
	if got, _ := ApplyFilters("123", []FilterBlock{{Name: "validate", Args: []any{`^\d+$`, "in"}}}); got != "123" {
		t.Errorf("keep matched = %q", got)
	}
	if got, _ := ApplyFilters("abc", []FilterBlock{{Name: "validate", Args: []any{`^\d+$`, "in"}}}); got != "" {
		t.Errorf("drop unmatched = %q", got)
	}
}
