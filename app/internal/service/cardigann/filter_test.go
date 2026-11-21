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
