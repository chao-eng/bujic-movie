package cardigann

import (
	"net/url"
	"strings"
	"testing"
)

// TST-015 / REQ-09: templates render only whitelisted context keys.
func TestTST005_RenderTemplate(t *testing.T) {
	ctx := NewTemplateContext("怪奇物语", []string{"5000", "5070"},
		map[string]string{"page": "0"}, map[string]string{"username": "bob"})

	out, err := Render("{{ .Keywords }}|{{ .Config.username }}|{{ .CategoriesString }}", ctx)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if out != "怪奇物语|bob|5000,5070" {
		t.Fatalf("got %q", out)
	}

	encoded, err := Render("{{ .Keywords | urlencode }}", ctx)
	if err != nil {
		t.Fatalf("Render urlencode: %v", err)
	}
	if !strings.Contains(encoded, "%") {
		t.Fatalf("urlencode did not apply: %q", encoded)
	}
}

// BR-10: template functions are limited and must not reach the filesystem.
func TestTST071_RenderRejectsDangerousFunctions(t *testing.T) {
	ctx := NewTemplateContext("x", nil, nil, nil)
	for _, src := range []string{
		`{{ readFile "/etc/passwd" }}`,
		`{{ exec "ls" }}`,
		`{{ .Env.HOME }}`,
	} {
		if _, err := Render(src, ctx); err == nil {
			t.Errorf("template %q must fail", src)
		}
	}
}

func TestTST015_BuildQueryURL(t *testing.T) {
	base := "https://site.example/"
	req := RequestBlock{
		Path: "/torrents.php",
		Inputs: OrderedInputs{
			Keys: []string{"search", "cat", "empty"},
			Values: map[string]string{
				"search": "{{ .Keywords }}",
				"cat":    "{{ .CategoriesString }}",
				"empty":  "{{ .Result.missing }}",
			},
		},
	}
	ctx := NewTemplateContext("怪奇物语", []string{"5000"}, nil, nil)

	got, err := BuildQueryURL(base, req, ctx)
	if err != nil {
		t.Fatalf("BuildQueryURL: %v", err)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("result is not a URL: %v", err)
	}
	if parsed.Host != "site.example" || parsed.Path != "/torrents.php" {
		t.Fatalf("unexpected url: %s", got)
	}
	if parsed.Query().Get("search") != "怪奇物语" {
		t.Errorf("search param = %q", parsed.Query().Get("search"))
	}
	if parsed.Query().Get("cat") != "5000" {
		t.Errorf("cat param = %q", parsed.Query().Get("cat"))
	}
	if _, ok := parsed.Query()["empty"]; ok {
		t.Error("empty rendered input must be omitted")
	}
}

func TestTST015_BuildQueryURLCustomSeparator(t *testing.T) {
	req := RequestBlock{
		Path:           "/api",
		QuerySeparator: "/",
		Inputs: OrderedInputs{
			Keys:   []string{"a", "b"},
			Values: map[string]string{"a": "1", "b": "2"},
		},
	}
	got, err := BuildQueryURL("https://x.example", req, NewTemplateContext("", nil, nil, nil))
	if err != nil {
		t.Fatalf("BuildQueryURL: %v", err)
	}
	if !strings.Contains(got, "a=1/b=2") && !strings.Contains(got, "b=2/a=1") {
		t.Fatalf("custom separator not used: %s", got)
	}
}

// REQ-07: unsupported query params are dropped instead of being sent.
func TestTST012_QueryParamsRespectsSupportedList(t *testing.T) {
	q := Query{Keyword: "kw", Season: 2, Episode: 5, Year: 2020, Categories: []string{"5000"}}

	movie := QueryParams("movie-search", []string{"q", "imdbid"}, q)
	if _, ok := movie["season"]; ok {
		t.Error("season must not be sent for movie-search")
	}
	if _, ok := movie["year"]; ok {
		t.Error("year must not be sent when unsupported")
	}
	if movie["q"] != "kw" || movie["cat"] != "5000" {
		t.Errorf("unexpected params: %v", movie)
	}

	tv := QueryParams("tv-search", []string{"q", "season", "ep"}, q)
	if tv["season"] != "2" || tv["ep"] != "5" {
		t.Errorf("tv params: %v", tv)
	}

	basic := QueryParams("search", nil, q)
	if basic["q"] != "kw" {
		t.Errorf("basic search must send q: %v", basic)
	}
}

// BR-08: the definition requestdelay wins when it is larger than the default.
func TestTST017_MinIntervalUsesDefinitionDelay(t *testing.T) {
	def := &Definition{RequestDelay: 3}
	if got := def.MinIntervalMS(2000); got != 3000 {
		t.Errorf("got %d, want 3000", got)
	}
	def.RequestDelay = 0.5
	if got := def.MinIntervalMS(2000); got != 2000 {
		t.Errorf("got %d, want default 2000", got)
	}
}

// Regression (Torrent Downloads): the keyword parameter must lead so the site
// does not answer 403, and the remaining parameters keep declaration order.
func TestTST007_KeywordInputOrder(t *testing.T) {
	req := RequestBlock{
		Path: "/search/",
		Inputs: OrderedInputs{
			Keys:   []string{"s_cat", "search"},
			Values: map[string]string{"s_cat": "0", "search": "one piece"},
		},
	}
	got, err := BuildQueryURL("https://www.torrentdownloads.pro", req, NewTemplateContext("one piece", nil, nil, nil))
	if err != nil {
		t.Fatalf("BuildQueryURL: %v", err)
	}
	if !strings.HasPrefix(got, "https://www.torrentdownloads.pro/search/?search=one+piece&s_cat=0") {
		t.Fatalf("keyword must lead: %s", got)
	}
}

// Non-keyword parameters keep the order the definition declared.
func TestTST007_DeclarationOrderPreserved(t *testing.T) {
	req := RequestBlock{
		Path: "/x",
		Inputs: OrderedInputs{
			Keys:   []string{"zeta", "alpha", "mike"},
			Values: map[string]string{"zeta": "1", "alpha": "2", "mike": "3"},
		},
	}
	got, _ := BuildQueryURL("https://x.example", req, NewTemplateContext("", nil, nil, nil))
	if got != "https://x.example/x?zeta=1&alpha=2&mike=3" {
		t.Fatalf("order = %s", got)
	}
}
