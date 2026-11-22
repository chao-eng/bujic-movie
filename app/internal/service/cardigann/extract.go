package cardigann

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/tidwall/gjson"
)

// Release is one normalised search result shared by all connectors.
type Release struct {
	Title        string
	DownloadURL  string
	InfoHash     string
	SizeBytes    int64
	Seeders      int
	Peers        int
	PublishDate  string
	Categories   []int
	IndexerID    uint
	IndexerName  string
	DownloadType string // magnet / torrent
	// DetailsURL points at the site's own page for this release, when the
	// definition declares a `details` field.
	DetailsURL string
}

// ExtractionError carries the field that failed so the UI can name it (BR-25).
type ExtractionError struct {
	Field  string
	Reason string
}

func (e *ExtractionError) Error() string {
	return fmt.Sprintf("fields.%s: %s", e.Field, e.Reason)
}

// Extract parses a response body into releases using the definition's search
// block. It supports HTML (goquery), JSON (gjson) and XML/RSS.
func Extract(def *Definition, raw []byte, responseType string) ([]Release, error) {
	return ExtractWithConfig(def, raw, responseType, nil)
}

// ExtractWithConfig is Extract with instance settings available to template
// selectors: upstream definitions write things like
// `${{ if .Config.uploader }}:has(...){{ end }}` in the rows selector.
func ExtractWithConfig(def *Definition, raw []byte, responseType string, config map[string]string) ([]Release, error) {
	if def.Search == nil || def.Search.Rows == nil {
		return nil, errors.New("cardigann: definition has no rows selector")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("cardigann: empty response body")
	}

	switch detectResponseType(responseType, raw) {
	case "json":
		return extractJSON(def, raw, config)
	case "xml":
		return extractXML(def, raw)
	default:
		return extractHTML(def, raw, config)
	}
}

// renderSelector resolves templates inside a selector or text block. A selector
// with no template is returned unchanged.
func renderSelector(src string, config, result map[string]string) string {
	if !strings.Contains(src, "{{") {
		return src
	}
	ctx := NewTemplateContext("", nil, nil, config)
	ctx.Result = result
	if ctx.Result == nil {
		ctx.Result = map[string]string{}
	}
	rendered, err := Render(src, ctx)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(rendered)
}

// filterTemplates runs a filter chain with the instance config and the row's
// already extracted values available to templated arguments.
func filterTemplates(value string, filters []FilterBlock, config, result map[string]string) string {
	if len(filters) == 0 {
		return value
	}
	ctx := NewTemplateContext("", nil, nil, config)
	ctx.Result = result
	if ctx.Result == nil {
		ctx.Result = map[string]string{}
	}
	filtered, err := ApplyFiltersWithContext(value, filters, ctx)
	if err != nil {
		return value
	}
	return filtered
}

func detectResponseType(declared string, raw []byte) string {
	switch strings.ToLower(strings.TrimSpace(declared)) {
	case "json":
		return "json"
	case "xml", "rss":
		return "xml"
	case "html":
		return "html"
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 {
		switch trimmed[0] {
		case '{', '[':
			return "json"
		case '<':
			if bytes.Contains(trimmed[:min(len(trimmed), 200)], []byte("<rss")) ||
				bytes.Contains(trimmed[:min(len(trimmed), 200)], []byte("<?xml")) {
				return "xml"
			}
			return "html"
		}
	}
	return "html"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func extractHTML(def *Definition, raw []byte, config map[string]string) ([]Release, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
	if err != nil {
		return nil, &ExtractionError{Field: "rows.selector", Reason: "HTML 解析失败: " + err.Error()}
	}

	selector := renderSelector(def.Search.Rows.Selector, config, nil)
	rows := doc.Find(selector)
	if rows.Length() == 0 && def.Search.Rows.MissingAttributeEqualsNoResults {
		return nil, nil
	}

	releases := make([]Release, 0, rows.Length())
	rows.Each(func(i int, row *goquery.Selection) {
		values := map[string]string{}
		for _, entry := range def.Search.Fields.Entries {
			values[entry.Name] = extractHTMLField(row, entry.Selector, config, values)
		}
		rel, err := buildRelease(def, values)
		if err != nil || rel.Title == "" {
			return
		}
		releases = append(releases, rel)
	})
	return releases, nil
}

func extractHTMLField(row *goquery.Selection, sel SelectorBlock, config, result map[string]string) string {
	// A field declared with only `text` is a literal, possibly templated, value.
	if sel.Selector == "" && sel.Text != "" {
		return renderSelector(sel.Text, config, result)
	}
	scope := row
	if sel.Selector != "" {
		scope = row.Find(sel.Selector)
	}
	if scope.Length() == 0 {
		if sel.Default != "" {
			return sel.Default
		}
		return ""
	}
	first := scope.First()
	var value string
	if sel.Attribute != "" {
		value, _ = first.Attr(sel.Attribute)
	} else {
		value = first.Text()
	}
	if sel.Remove != "" {
		value = strings.ReplaceAll(value, sel.Remove, "")
	}
	value = strings.TrimSpace(value)
	if sel.Text != "" && !strings.Contains(sel.Text, "{{") && !strings.Contains(value, sel.Text) {
		value = ""
	}
	value = filterTemplates(value, sel.Filters, config, result)
	if value == "" && sel.Default != "" {
		return sel.Default
	}
	return value
}

func extractJSON(def *Definition, raw []byte, config map[string]string) ([]Release, error) {
	rowsBlock := def.Search.Rows
	rowsPath := jsonPath(renderSelector(rowsBlock.Selector, config, nil))
	if rowsPath == "" {
		return nil, &ExtractionError{Field: "rows.selector", Reason: "JSON 定义缺少路径"}
	}
	// A count selector can report "no results" before anything is walked.
	if rowsBlock.Count != nil && rowsBlock.Count.Selector != "" {
		countPath := jsonPath(renderSelector(rowsBlock.Count.Selector, config, nil))
		if count := gjson.GetBytes(raw, countPath); count.Exists() && count.Int() < 1 {
			return nil, nil
		}
	}
	rows := gjson.GetBytes(raw, rowsPath)
	if !rows.Exists() {
		return nil, nil
	}
	// A single-object body is treated as a one-element array so `rows` selectors
	// written for arrays (e.g. SubsPlease-style envelopes) still work.
	if !rows.IsArray() && rows.IsObject() && rowsPath == "@this" {
		return collectJSONRows(def, []gjson.Result{rows}, config), nil
	}
	return collectJSONRows(def, rows.Array(), config), nil
}

// collectJSONRows walks the row objects of a JSON response. A row may point at
// a nested array (`rows.attribute`) and emit one release per element
// (`rows.multiple`); YTS lists movies and their torrents that way. Field
// selectors starting with `..` always resolve against the enclosing row rather
// than the expanded element.
func collectJSONRows(def *Definition, rows []gjson.Result, config map[string]string) []Release {
	releases := make([]Release, 0, len(rows))
	rowsBlock := def.Search.Rows
	for _, row := range rows {
		element := row
		if rowsBlock.Attribute != "" {
			path := jsonPath(renderSelector(rowsBlock.Attribute, config, nil))
			element = row.Get(path)
			if !element.Exists() {
				// A row without the attribute contributes nothing.
				continue
			}
		}
		elements := []gjson.Result{element}
		if rowsBlock.Multiple {
			if !element.IsArray() {
				continue
			}
			elements = element.Array()
		}
		for _, item := range elements {
			values := map[string]string{}
			for _, entry := range def.Search.Fields.Entries {
				values[entry.Name] = extractJSONFieldScoped(row, item, entry.Selector, config, values)
			}
			rel, err := buildRelease(def, values)
			if err == nil && rel.Title != "" {
				releases = append(releases, rel)
			}
		}
	}
	return releases
}

// jsonPath converts a Cardigann/JSONPath selector into a gjson path.
//
// Upstream definitions write selectors in JSONPath style: `$` is the document
// root, `$[0].id` addresses the first element, and plain `name` is relative to
// the current row. gjson spells the root `@this` and array indexes without
// brackets, so the two forms are translated here.
func jsonPath(selector string) string {
	s := strings.TrimSpace(selector)
	if s == "" || s == "$" || s == "." || s == "@this" {
		return "@this"
	}
	// `$[0].id` / `$.[0].id` -> `0.id`
	s = strings.TrimPrefix(s, "$")
	s = strings.TrimPrefix(s, ".")
	s = strings.ReplaceAll(s, "[", ".")
	s = strings.ReplaceAll(s, "]", "")
	s = strings.ReplaceAll(s, "..", ".")
	s = strings.Trim(s, ".")
	if s == "" {
		return "@this"
	}
	return s
}

// xmlEnvelope is the minimal RSS shape used by Torznab-like XML definitions.
type xmlEnvelope struct {
	Channel struct {
		Items []struct {
			Title     string `xml:"title"`
			Link      string `xml:"link"`
			PubDate   string `xml:"pubDate"`
			Size      string `xml:"size"`
			Enclosure struct {
				URL    string `xml:"url,attr"`
				Length string `xml:"length,attr"`
			} `xml:"enclosure"`
		} `xml:"item"`
	} `xml:"channel"`
}

func extractXML(def *Definition, raw []byte) ([]Release, error) {
	var env xmlEnvelope
	if err := xml.Unmarshal(raw, &env); err != nil {
		return nil, &ExtractionError{Field: "rows.selector", Reason: "XML 解析失败: " + err.Error()}
	}
	releases := make([]Release, 0, len(env.Channel.Items))
	for _, item := range env.Channel.Items {
		rel := Release{
			Title:       strings.TrimSpace(item.Title),
			DownloadURL: item.Enclosure.URL,
			PublishDate: item.PubDate,
		}
		if rel.DownloadURL == "" {
			rel.DownloadURL = item.Link
		}
		rel.SizeBytes = parseSize(item.Size)
		if rel.SizeBytes == 0 {
			rel.SizeBytes = parseSize(item.Enclosure.Length)
		}
		rel.DownloadType = downloadTypeFor(rel.DownloadURL)
		if rel.Title != "" {
			releases = append(releases, rel)
		}
	}
	return releases, nil
}

// extractJSONField reads one field from a JSON row.
func extractJSONField(row gjson.Result, sel SelectorBlock, config, result map[string]string) string {
	return extractJSONFieldScoped(row, row, sel, config, result)
}

// extractJSONFieldScoped reads one field, resolving a selector that starts with
// `..` against the enclosing row instead of the expanded element.
func extractJSONFieldScoped(row, element gjson.Result, sel SelectorBlock, config, result map[string]string) string {
	if sel.Selector == "" && sel.Text != "" {
		return renderSelector(sel.Text, config, result)
	}
	selector := strings.TrimSpace(renderSelector(sel.Selector, config, result))
	// Cardigann lets a definition reach the parent row with a leading `..`.
	scope := element
	if strings.HasPrefix(selector, "..") {
		scope = row
		selector = strings.TrimPrefix(selector, "..")
		if selector != "" && !strings.HasPrefix(selector, ".") {
			selector = "." + selector
		}
	}
	if selector == "" {
		return ""
	}
	value := scope.Get(jsonPath(selector)).String()
	value = applyJSONCase(value, sel.Case, config, result)
	value = filterTemplates(value, sel.Filters, config, result)
	return strings.TrimSpace(value)
}

// applyJSONCase maps a field value through its declared `case` table. Entries are
// matched exactly first, then "*"; a template value is rendered. YTS turns its
// quality string into a Newznab category id this way.
func applyJSONCase(value string, cases map[string]string, config, result map[string]string) string {
	if len(cases) == 0 {
		return value
	}
	for key, mapped := range cases {
		if key == "*" || !strings.EqualFold(strings.TrimSpace(value), key) {
			continue
		}
		return renderSelector(mapped, config, result)
	}
	if fallback, ok := cases["*"]; ok {
		return renderSelector(fallback, config, result)
	}
	return value
}

func buildRelease(def *Definition, values map[string]string) (Release, error) {
	rel := Release{Title: strings.TrimSpace(values["title"])}
	rel.DetailsURL = strings.TrimSpace(values["details"])
	rel.DownloadURL = strings.TrimSpace(values["download"])
	if rel.DownloadURL == "" {
		// Some definitions expose a magnet field instead of a download link.
		rel.DownloadURL = strings.TrimSpace(values["magnet"])
	}
	rel.InfoHash = strings.TrimSpace(values["infohash"])
	rel.PublishDate = strings.TrimSpace(values["date"])
	if rel.PublishDate == "" {
		rel.PublishDate = strings.TrimSpace(values["publishdate"])
	}
	rel.SizeBytes = parseSize(values["size"])
	rel.Seeders = parseInt(values["seeders"])
	rel.Peers = parseInt(values["peers"])
	// Site categories are mapped to Newznab ids through the definition's caps.
	// Without this the UI would show the tracker's own numbering (TPB's 208).
	rel.Categories = mapCategories(def, values["category"])

	if rel.DownloadURL == "" && rel.InfoHash != "" {
		rel.DownloadURL = MagnetFromInfoHash(rel.InfoHash, rel.Title)
	}
	if rel.DownloadURL == "" {
		return rel, &ExtractionError{Field: "download", Reason: "该行没有下载链接或 infohash"}
	}
	rel.DownloadType = downloadTypeFor(rel.DownloadURL)
	return rel, nil
}

func downloadTypeFor(link string) string {
	if strings.HasPrefix(strings.ToLower(link), "magnet:") {
		return "magnet"
	}
	return "torrent"
}

// MagnetFromInfoHash builds a magnet link when the site only exposes a hash.
func MagnetFromInfoHash(hash, title string) string {
	h := strings.ToLower(strings.TrimSpace(hash))
	if h == "" {
		return ""
	}
	magnet := "magnet:?xt=urn:btih:" + h
	if title != "" {
		magnet += "&dn=" + title
	}
	return magnet
}

var sizeRe = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*(B|KB|KIB|MB|MIB|GB|GIB|TB|TIB)$`)

func parseSize(raw string) int64 {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" {
		return 0
	}
	// Plain byte count.
	if f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64); err == nil {
		return int64(f)
	}
	m := sizeRe.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil {
		return 0
	}
	multipliers := map[string]float64{
		"B": 1, "KB": 1 << 10, "KIB": 1 << 10,
		"MB": 1 << 20, "MIB": 1 << 20,
		"GB": 1 << 30, "GIB": 1 << 30,
		"TB": 1 << 40, "TIB": 1 << 40,
	}
	return int64(value * multipliers[m[2]])
}

func parseInt(raw string) int {
	s := strings.TrimSpace(strings.ReplaceAll(raw, ",", ""))
	if s == "" {
		return 0
	}
	var v int
	if _, err := fmt.Sscanf(s, "%d", &v); err != nil {
		return 0
	}
	return v
}

// mapCategories converts a site category value into Newznab ids using the
// definition's categorymappings. Unknown values are kept as-is so nothing is
// silently lost, and the caller can still show them.
func mapCategories(def *Definition, raw string) []int {
	values := parseCategories(raw)
	if len(values) == 0 || raw == "" {
		// Some definitions emit a name rather than an id.
		if name := strings.TrimSpace(raw); name != "" {
			if id, ok := newznabMapping(def).ByName(name); ok {
				return []int{id}
			}
		}
		return nil
	}
	mapping := newznabMapping(def)
	out := make([]int, 0, len(values))
	for _, site := range values {
		if id, ok := mapping.ByID(site); ok {
			out = append(out, id)
			continue
		}
		out = append(out, site)
	}
	return out
}

// categoryMapping resolves a definition's categorymappings both ways.
type categoryMapping struct {
	byID   map[int]int
	byName map[string]int
}

// ByID looks up a site category id.
func (c categoryMapping) ByID(site int) (int, bool) {
	id, ok := c.byID[site]
	return id, ok
}

// ByName looks up a site category name (case-insensitive prefix match).
func (c categoryMapping) ByName(name string) (int, bool) {
	if id, ok := c.byName[strings.ToLower(strings.TrimSpace(name))]; ok {
		return id, true
	}
	return 0, false
}

// newznabCapsNames maps a Cardigann category name to a Newznab id.
// It mirrors the table used by the connector so mapping happens in one place.
var newznabCapsNames = map[string]int{
	"movies": 2000, "movies/hd": 2040, "movies/uhd": 2045, "movies/bluray": 2050,
	"movies/sd": 2030, "movies/dvd": 2010, "movies/foreign": 2010, "movies/other": 2010,
	"movies/3d": 2060,
	"tv":        5000, "tv/hd": 5040, "tv/uhd": 5045, "tv/sd": 5030, "tv/anime": 5070,
	"tv/documentary": 5080, "tv/sport": 5060, "tv/foreign": 5020, "tv/other": 5080,
	"anime": 5070, "audio": 3000, "audio/mp3": 3010, "audio/lossless": 3040,
	"audio/audiobook": 3030, "audio/video": 3000, "audio/other": 3000,
	"pc": 4000, "pc/games": 4050, "pc/mac": 4030, "pc/mobile-ios": 4020,
	"pc/mobile-android": 4010, "pc/mobile-other": 4000,
	"console": 1000, "console/ps4": 1010, "console/xbox": 1030, "console/wii": 1030,
	"books": 7000, "books/ebook": 7020, "books/comics": 7030, "books/mags": 7040,
	"books/technical": 7000, "books/other": 7000,
	"xxx": 6000, "other": 8000,
}

// newznabMapping builds the lookup table for one definition.
func newznabMapping(def *Definition) categoryMapping {
	m := categoryMapping{byID: map[int]int{}, byName: map[string]int{}}
	if def == nil {
		return m
	}
	for _, cm := range def.Caps.CategoryMappings {
		target, ok := newznabCapsNames[strings.ToLower(strings.TrimSpace(cm.Cat))]
		if !ok {
			continue
		}
		if site, err := parseCategoryID(cm.ID); err == nil {
			m.byID[site] = target
		}
		if strings.TrimSpace(cm.Desc) != "" {
			m.byName[strings.ToLower(strings.TrimSpace(cm.Desc))] = target
		}
	}
	for name, site := range def.Caps.Categories {
		target, ok := newznabCapsNames[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			continue
		}
		if id, err := parseCategoryID(site); err == nil {
			m.byID[id] = target
		}
	}
	return m
}

func parseCategoryID(raw string) (int, error) {
	var id int
	if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &id); err != nil {
		return 0, err
	}
	return id, nil
}

func parseCategories(raw string) []int {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		if v := parseInt(p); v > 0 {
			out = append(out, v)
		}
	}
	return out
}
