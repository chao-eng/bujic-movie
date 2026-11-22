package cardigann

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"
)

// TemplateLiteral is Scriban's `.True` / `.False` constant. Upstream renders
// `.True` as "True" and `.False` as null, and `.False` compares equal to an
// absent value as well as to a setting that is off — that second part matters
// because Cardigann stores checkbox settings as the strings "true"/"false",
// while upstream stores them as a bool.
type TemplateLiteral bool

// String renders the literal the way Scriban's output does.
func (l TemplateLiteral) String() string {
	if l {
		return "True"
	}
	return ""
}

// TemplateContext is the data a definition template may reference. Only these
// keys exist; a definition cannot reach the filesystem, environment or other
// instances through a template (BR-10).
type TemplateContext struct {
	Keywords         string
	Categories       []string
	Query            map[string]string
	Config           map[string]string
	Result           map[string]string
	Today            time.Time
	CategoriesString string

	// True and False stand for Scriban's `.True` / `.False` constants. They are
	// fields because Go's template syntax has no such literals, and definitions
	// compare them against settings (`eq .Config.disablesort .False`).
	True  TemplateLiteral
	False TemplateLiteral
}

// NewTemplateContext builds the render context for one request or row.
func NewTemplateContext(keywords string, categories []string, query map[string]string, config map[string]string) TemplateContext {
	if query == nil {
		query = map[string]string{}
	}
	if config == nil {
		config = map[string]string{}
	}
	return TemplateContext{
		Keywords:         keywords,
		Categories:       categories,
		Query:            query,
		Config:           config,
		Result:           map[string]string{},
		Today:            nowFn(),
		CategoriesString: strings.Join(categories, ","),
		True:             true,
		False:            false,
	}
}

// ForResult returns a copy of the context with Result populated, used when
// rendering download selectors that reference row fields.
func (c TemplateContext) ForResult(result map[string]string) TemplateContext {
	c.Result = result
	return c
}

// Render expands a definition template. Supported syntax is Go text/template
// with a restricted function set.
func Render(src string, ctx TemplateContext) (string, error) {
	if !strings.Contains(src, "{{") {
		return src, nil
	}
	tpl, err := template.New("cardigann").Option("missingkey=zero").Funcs(templateFuncs()).Parse(normaliseTemplate(src))
	if err != nil {
		return "", fmt.Errorf("template parse: %w", err)
	}
	var b strings.Builder
	if err := tpl.Execute(&b, ctx); err != nil {
		return "", fmt.Errorf("template render: %w", err)
	}
	return b.String(), nil
}

// dotNetRegexClasses maps .NET/Java character class names that Go's regexp
// does not know onto their Unicode script names. Upstream definitions use these
// (The Pirate Bay strips CJK keywords this way).
var dotNetRegexClasses = strings.NewReplacer(
	`\p{IsCJKUnifiedIdeographs}`, `\p{Han}`,
	`\p{IsCJKSymbolsandPunctuation}`, `\p{Han}`,
	`\p{IsHiragana}`, `\p{Hiragana}`,
	`\p{IsKatakana}`, `\p{Katakana}`,
	`\p{IsBasicLatin}`, `\x00-\x7F`,
	`\p{IsCyrillic}`, `\p{Cyrillic}`,
	`\p{IsGreek}`, `\p{Greek}`,
	`\p{IsArabic}`, `\p{Arabic}`,
	`\p{IsHebrew}`, `\p{Hebrew}`,
)

// normaliseRegex rewrites .NET regex constructs into Go equivalents.
func normaliseRegex(pattern string) string {
	return dotNetRegexClasses.Replace(pattern)
}

// hyphenKey matches Scriban-style map access with keys that contain a hyphen,
// for example `.Config.cat-id`. Go templates cannot parse that as a field, so it
// is rewritten to `(index .Config "cat-id")`.
var hyphenKey = regexp.MustCompile(`\.Config\.([A-Za-z0-9_]+(?:-[A-Za-z0-9_]+)+)`)

func normaliseTemplate(src string) string {
	src = hyphenKey.ReplaceAllString(src, `(index .Config "$1")`)
	return escapeTemplateStrings(src)
}

// escapeTemplateStrings doubles backslashes inside quoted arguments of template
// actions. Scriban accepts `\d` directly, while Go's template lexer treats it as
// an invalid string escape; doubling keeps the regex intact.
func escapeTemplateStrings(src string) string {
	var b strings.Builder
	inAction := false
	inString := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if !inAction {
			if c == '{' && i+1 < len(src) && src[i+1] == '{' {
				inAction = true
				b.WriteString("{{")
				i++
				continue
			}
			b.WriteByte(c)
			continue
		}
		if c == '}' && i+1 < len(src) && src[i+1] == '}' && !inString {
			inAction = false
			b.WriteString("}}")
			i++
			continue
		}
		switch c {
		case '"':
			inString = !inString
			b.WriteByte(c)
		case '\\':
			if inString && i+1 < len(src) {
				next := src[i+1]
				if next == '"' || next == '\\' {
					b.WriteByte(c)
					b.WriteByte(next)
					i++
					continue
				}
				// Unknown escape: double it so the template lexer keeps it.
				b.WriteString("\\\\")
				continue
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// templateEq compares two template operands the way Cardigann does. A Scriban
// boolean literal is spelled "true"/"false" here, while a checkbox setting
// arrives as the string "true"/"false", so the two sides compare without regard
// to case. Every other pair keeps Cardigann's exact string comparison.
func templateEq(args ...any) bool {
	if len(args) < 2 {
		return false
	}
	for _, other := range args[1:] {
		if compareOperands(args[0], other) == 0 {
			return true
		}
	}
	return false
}

// templateNe is the inverse of templateEq.
func templateNe(a, b any) bool {
	return compareOperands(a, b) != 0
}

// compareOperands normalises literal types before comparing. Returns 0 when the
// operands are equal, negative/positive otherwise.
func compareOperands(a, b any) int {
	if lit, ok := a.(TemplateLiteral); ok {
		return compareConstant(bool(lit), b)
	}
	if lit, ok := b.(TemplateLiteral); ok {
		return -compareConstant(bool(lit), a)
	}
	// Every other pair keeps Cardigann's exact string comparison.
	left, right := asString(a), asString(b)
	if left == right {
		return 0
	}
	if left < right {
		return -1
	}
	return 1
}

// compareConstant compares a Scriban constant with another operand. Upstream
// represents `.False` as null, so an absent value equals it; `.True` is the
// string "True". Settings reach this build as "true"/"false" text, hence the
// case-insensitive comparison.
func compareConstant(want bool, other any) int {
	text := strings.TrimSpace(asString(other))
	if want {
		if strings.EqualFold(text, "true") {
			return 0
		}
		return 1
	}
	if text == "" || strings.EqualFold(text, "false") {
		return 0
	}
	return 1
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"eq":        templateEq,
		"ne":        templateNe,
		"urlencode": url.QueryEscape,
		"urldecode": func(s string) string {
			out, err := url.QueryUnescape(s)
			if err != nil {
				return s
			}
			return out
		},
		"join":  strings.Join,
		"lower": strings.ToLower,
		"upper": strings.ToUpper,
		"trim":  strings.TrimSpace,
		"replace": func(s, old, new string) string {
			return strings.ReplaceAll(s, old, new)
		},
		// Scriban's regex replace, used by several upstream definitions.
		"re_replace": func(s, pattern, replacement string) string {
			re, err := regexp.Compile(normaliseRegex(pattern))
			if err != nil {
				return s
			}
			return re.ReplaceAllString(s, replacement)
		},
		"default": func(fallback, value string) string {
			if strings.TrimSpace(value) == "" {
				return fallback
			}
			return value
		},
		"yesno": func(v string) string {
			if v == "" || v == "false" || v == "0" {
				return "no"
			}
			return "yes"
		},
	}
}

// BuildQueryURL renders the request path and inputs into a full URL relative to
// the site base URL.
func BuildQueryURL(baseURL string, req RequestBlock, ctx TemplateContext) (string, error) {
	pathSrc := req.Path
	if pathSrc == "" {
		pathSrc = "/"
	}
	path, err := Render(pathSrc, ctx)
	if err != nil {
		return "", err
	}

	sep, _ := Render(req.QuerySeparator, ctx)
	if sep == "" {
		sep = "&"
	}

	// Ordering matters twice over: keyword parameters go first (many endpoints
	// and WAFs expect the query term in front), and the rest keep the order the
	// definition declared, since some sites reject a specific leading parameter.
	// Torrent Downloads returns 403 when `s_cat` leads and 200 when `search` does.
	type pair struct{ key, value string }
	pairs := make([]pair, 0, req.Inputs.Len())
	for _, key := range orderedInputKeys(req.Inputs) {
		raw := req.Inputs.Values[key]
		rendered, err := Render(raw, ctx)
		if err != nil {
			return "", fmt.Errorf("input %s: %w", key, err)
		}
		if rendered == "" {
			continue
		}
		pairs = append(pairs, pair{key: key, value: rendered})
	}

	full := joinURL(baseURL, path)
	if len(pairs) > 0 {
		encoded := make([]string, 0, len(pairs))
		for _, p := range pairs {
			encoded = append(encoded, url.QueryEscape(p.key)+"="+url.QueryEscape(p.value))
		}
		query := strings.Join(encoded, sep)
		if strings.Contains(full, "?") {
			full += sep + query
		} else {
			full += "?" + query
		}
	}
	return escapeURL(full), nil
}

// escapeURL percent-encodes characters that a definition template cannot
// escape itself: non-ASCII (CJK keywords), spaces, and stray control bytes.
// ASCII URL structure (`?`, `&`, `=`, `/`, `%`) is left untouched so templates
// that already build a full URL keep working.
func escapeURL(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c < 0x80 {
			// A space inside a URL is invalid; encode it.
			if c == ' ' {
				b.WriteString("%20")
				continue
			}
			b.WriteByte(c)
			continue
		}
		// Encode the whole rune, not the individual byte.
		r, size := utf8.DecodeRuneInString(raw[i:])
		if r == utf8.RuneError && size == 1 {
			fmt.Fprintf(&b, "%%%02X", c)
			continue
		}
		var buf [4]byte
		n := utf8.EncodeRune(buf[:], r)
		for _, bb := range buf[:n] {
			fmt.Fprintf(&b, "%%%02X", bb)
		}
		i += size - 1
	}
	return b.String()
}

// keywordInputs are treated as the query term and emitted first.
var keywordInputs = map[string]bool{
	"q": true, "search": true, "keywords": true, "keyword": true, "query": true,
}

// orderedInputKeys puts keyword parameters first, then everything else in the
// order the definition declared.
func orderedInputKeys(inputs OrderedInputs) []string {
	out := make([]string, 0, len(inputs.Keys))
	for _, key := range inputs.Keys {
		if keywordInputs[strings.ToLower(key)] {
			out = append(out, key)
		}
	}
	for _, key := range inputs.Keys {
		if !keywordInputs[strings.ToLower(key)] {
			out = append(out, key)
		}
	}
	return out
}

func joinURL(base, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	base = strings.TrimRight(base, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Insertion-order independence matters for rate limits and tests.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// CategoryParams renders the category list the way a definition expects:
// comma separated ids.
func CategoryParams(categories []string) string {
	return strings.Join(categories, ",")
}

// QueryParams maps a normalised query onto the definition's declared inputs.
// Keys absent from the mode's supported parameter list are dropped so a site
// never receives a parameter it cannot understand (REQ-07).
func QueryParams(mode string, supported []string, q Query) map[string]string {
	allowed := map[string]bool{}
	for _, p := range supported {
		allowed[strings.ToLower(p)] = true
	}
	out := map[string]string{}
	if q.Keyword != "" && (allowed["q"] || len(supported) == 0) {
		out["q"] = q.Keyword
	}
	if q.Season > 0 && allowed["season"] {
		out["season"] = strconv.Itoa(q.Season)
	}
	if q.Episode > 0 && allowed["ep"] {
		out["ep"] = strconv.Itoa(q.Episode)
	}
	if q.IMDBID != "" && allowed["imdbid"] {
		out["imdbid"] = q.IMDBID
	}
	if q.TMDBID != "" && allowed["tmdbid"] {
		out["tmdbid"] = q.TMDBID
	}
	if q.Year > 0 && allowed["year"] {
		out["year"] = strconv.Itoa(q.Year)
	}
	if len(q.Categories) > 0 {
		out["cat"] = CategoryParams(q.Categories)
	}
	return out
}

// Query is the normalised search input shared by every connector.
type Query struct {
	Keyword    string
	Categories []string
	Season     int
	Episode    int
	Year       int
	IMDBID     string
	TMDBID     string
	Page       int
	Limit      int
	Mode       string // search / tv-search / movie-search
}
