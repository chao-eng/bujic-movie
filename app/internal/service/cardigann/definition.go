package cardigann

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

// Definition is the subset of the Cardigann YAML schema this build understands.
// Unknown top-level blocks are recorded in Unsupported so a definition can be
// stored as `partial` instead of being silently dropped.
type Definition struct {
	ID              string            `yaml:"id"`
	Name            string            `yaml:"name"`
	Description     string            `yaml:"description"`
	Type            string            `yaml:"type"`
	Language        string            `yaml:"language"`
	Encoding        string            `yaml:"encoding"`
	RequestDelay    float64           `yaml:"requestdelay"`
	Links           []string          `yaml:"links"`
	LegacyLinks     []string          `yaml:"legacylinks"`
	FollowRedirect  bool              `yaml:"followredirect"`
	TestLinkTorrent *bool             `yaml:"testlinktorrent"`
	Settings        []SettingsField   `yaml:"settings"`
	Caps            CapabilitiesBlock `yaml:"caps"`
	Login           *LoginBlock       `yaml:"login"`
	Search          *SearchBlock      `yaml:"search"`
	Download        *DownloadBlock    `yaml:"download"`
	Ratio           map[string]any    `yaml:"ratio"`

	// Unsupported lists top-level keys present in the YAML but not handled.
	Unsupported []string `yaml:"-"`
}

// SettingsField is one dynamically rendered instance setting.
type SettingsField struct {
	Name    string            `yaml:"name"`
	Type    string            `yaml:"type"`
	Label   string            `yaml:"label"`
	Default RawValue          `yaml:"default"`
	Options map[string]string `yaml:"options"`
}

// RawValue keeps a scalar exactly as written. YAML 1.1 parses `0_0` as the
// number 0, which would silently corrupt select defaults such as Nyaa's
// category id. This type preserves the original token.
type RawValue string

// UnmarshalYAML captures the raw scalar token and removes YAML quoting.
func (r *RawValue) UnmarshalYAML(raw []byte) error {
	s := strings.TrimSpace(string(raw))
	switch {
	case s == "" || s == "null" || s == "~":
		s = ""
	case len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"':
		if unquoted, err := strconv.Unquote(s); err == nil {
			s = unquoted
		} else {
			s = s[1 : len(s)-1]
		}
	case len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'':
		s = strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	*r = RawValue(s)
	return nil
}

// String lets RawValue be used where a plain string is expected.
func (r RawValue) String() string { return string(r) }

// CapabilitiesBlock declares search modes and category mapping.
type CapabilitiesBlock struct {
	Categories       map[string]string   `yaml:"categories"`
	CategoryMappings []CategoryMapping   `yaml:"categorymappings"`
	Modes            map[string][]string `yaml:"modes"`
	AllowRawSearch   bool                `yaml:"allowrawsearch"`
}

// CategoryMapping maps a site category id to a Newznab category name.
type CategoryMapping struct {
	ID      string `yaml:"id"`
	Cat     string `yaml:"cat"`
	Desc    string `yaml:"desc"`
	Default bool   `yaml:"default"`
}

// LoginBlock describes how to authenticate against the site.
type LoginBlock struct {
	Path              string                   `yaml:"path"`
	SubmitPath        string                   `yaml:"submitpath"`
	Method            string                   `yaml:"method"`
	Form              string                   `yaml:"form"`
	Cookies           []string                 `yaml:"cookies"`
	Inputs            OrderedInputs            `yaml:"inputs"`
	SelectorInputs    map[string]SelectorBlock `yaml:"selectorinputs"`
	GetSelectorInputs map[string]SelectorBlock `yaml:"getselectorinputs"`
	Error             []ErrorBlock             `yaml:"error"`
	Test              *PageTestBlock           `yaml:"test"`
	Captcha           *CaptchaBlock            `yaml:"captcha"`
	Headers           map[string][]string      `yaml:"headers"`
}

// CaptchaBlock marks a login that needs manual user input.
type CaptchaBlock struct {
	Type     string `yaml:"type"`
	Selector string `yaml:"selector"`
	Input    string `yaml:"input"`
}

// PageTestBlock is a post-login check.
type PageTestBlock struct {
	Path     string `yaml:"path"`
	Selector string `yaml:"selector"`
}

// ErrorBlock matches a login failure.
type ErrorBlock struct {
	Path     string        `yaml:"path"`
	Selector string        `yaml:"selector"`
	Message  SelectorBlock `yaml:"message"`
}

// SelectorBlock extracts one value from a document and runs filters over it.
type SelectorBlock struct {
	Selector  string            `yaml:"selector"`
	Attribute string            `yaml:"attribute"`
	Text      string            `yaml:"text"`
	Remove    string            `yaml:"remove"`
	Optional  bool              `yaml:"optional"`
	Default   string            `yaml:"default"`
	Filters   []FilterBlock     `yaml:"filters"`
	Case      map[string]string `yaml:"case"`
}

// FilterBlock is one entry of a filter chain.
type FilterBlock struct {
	Name string `yaml:"name"`
	Args any    `yaml:"args"`
}

// SearchBlock is the query template.
type SearchBlock struct {
	Path                 string              `yaml:"path"`
	Paths                []SearchPathBlock   `yaml:"paths"`
	Headers              map[string][]string `yaml:"headers"`
	KeywordsFilters      []FilterBlock       `yaml:"keywordsfilters"`
	AllowEmptyInputs     bool                `yaml:"allowemptyinputs"`
	Inputs               OrderedInputs       `yaml:"inputs"`
	Error                []ErrorBlock        `yaml:"error"`
	PreprocessingFilters []FilterBlock       `yaml:"preprocessingfilters"`
	Rows                 *RowsBlock          `yaml:"rows"`
	Fields               FieldMap            `yaml:"fields"`
}

// SearchPathBlock is one query variant, optionally scoped to categories.
type SearchPathBlock struct {
	RequestBlock   `yaml:",inline"`
	Categories     []string       `yaml:"categories"`
	InheritInputs  *bool          `yaml:"inheritinputs"`
	FollowRedirect bool           `yaml:"followredirect"`
	Response       *ResponseBlock `yaml:"response"`
}

// RequestBlock is a raw HTTP request template.
type RequestBlock struct {
	Path           string        `yaml:"path"`
	Method         string        `yaml:"method"`
	Inputs         OrderedInputs `yaml:"inputs"`
	QuerySeparator string        `yaml:"queryseparator"`
}

// ResponseBlock declares the response format.
type ResponseBlock struct {
	Type             string `yaml:"type"`
	NoResultsMessage string `yaml:"noresultmessage"`
}

// RowsBlock scopes result rows inside a document.
type RowsBlock struct {
	SelectorBlock                   `yaml:",inline"`
	After                           int            `yaml:"after"`
	DateHeaders                     *SelectorBlock `yaml:"dateheaders"`
	Count                           *SelectorBlock `yaml:"count"`
	Multiple                        bool           `yaml:"multiple"`
	MissingAttributeEqualsNoResults bool           `yaml:"missingattributeequalsnoresults"`
}

// DownloadBlock describes how to turn a row into a download link or infohash.
type DownloadBlock struct {
	Selectors []SelectorField     `yaml:"selectors"`
	Method    string              `yaml:"method"`
	Before    *BeforeBlock        `yaml:"before"`
	InfoHash  *InfoHashBlock      `yaml:"infohash"`
	Headers   map[string][]string `yaml:"headers"`
}

// BeforeBlock requests a detail page before extracting the download link.
type BeforeBlock struct {
	RequestBlock `yaml:",inline"`
	PathSelector SelectorField `yaml:"pathselector"`
}

// InfoHashBlock extracts a torrent infohash, optionally from the before page.
type InfoHashBlock struct {
	Hash              SelectorField `yaml:"hash"`
	Title             SelectorField `yaml:"title"`
	UseBeforeResponse bool          `yaml:"usebeforeresponse"`
}

// SelectorField is a selector used by the download block.
type SelectorField struct {
	Selector          string        `yaml:"selector"`
	Attribute         string        `yaml:"attribute"`
	UseBeforeResponse bool          `yaml:"usebeforeresponse"`
	Filters           []FilterBlock `yaml:"filters"`
}

// FieldMap preserves the YAML field order, since `fields` is an ordered map
// and some definitions depend on evaluation order.
type FieldMap struct {
	Entries []FieldEntry
}

// FieldEntry is one named field with its selector.
type FieldEntry struct {
	Name     string
	Selector SelectorBlock
}

// UnmarshalYAML keeps the declared order of search.fields. The order comes from
// a MapSlice decode; the values are decoded separately so filters with literal
// blocks (`args: ["\n", ""]`) are not round-tripped through Marshal again.
func (m *FieldMap) UnmarshalYAML(unmarshal func(any) error) error {
	var ordered yaml.MapSlice
	if err := unmarshal(&ordered); err != nil {
		return err
	}
	var values map[string]SelectorBlock
	if err := unmarshal(&values); err != nil {
		return err
	}
	entries := make([]FieldEntry, 0, len(ordered))
	for _, item := range ordered {
		name, ok := item.Key.(string)
		if !ok {
			continue
		}
		sel, ok := values[name]
		if !ok {
			return fmt.Errorf("fields.%s: 无法解析", name)
		}
		entries = append(entries, FieldEntry{Name: name, Selector: sel})
	}
	m.Entries = entries
	return nil
}

// MarshalYAML renders the map back in declaration order.
func (m FieldMap) MarshalYAML() (any, error) {
	out := make(yaml.MapSlice, 0, len(m.Entries))
	for _, e := range m.Entries {
		out = append(out, yaml.MapItem{Key: e.Name, Value: e.Selector})
	}
	return out, nil
}

// Get returns the selector for a field name.
func (m FieldMap) Get(name string) (SelectorBlock, bool) {
	for _, e := range m.Entries {
		if e.Name == name {
			return e.Selector, true
		}
	}
	return SelectorBlock{}, false
}

// Names lists the declared field names in order.
func (m FieldMap) Names() []string {
	names := make([]string, 0, len(m.Entries))
	for _, e := range m.Entries {
		names = append(names, e.Name)
	}
	return names
}

// UnsupportedBlocks are top-level Cardigann blocks this build does not
// interpret. Presence makes a definition partial, never invalid.
var UnsupportedBlocks = []string{
	"flaresolverr",
	"captcha",
	"ratio",
	"grab",
	"oauth",
}

// knownBlocks are the keys Regularise accepts without marking unsupported.
var knownBlocks = map[string]bool{
	"id": true, "name": true, "description": true, "type": true,
	"language": true, "encoding": true, "requestdelay": true,
	"links": true, "legacylinks": true, "followredirect": true,
	"testlinktorrent": true, "settings": true, "caps": true, "login": true,
	"search": true, "download": true,
}

// regularise records unknown top-level keys so they surface as "partial".
// Matching is case-insensitive because upstream mixes camelCase and lowercase.
func regularise(def *Definition, presentKeys []string) {
	sort.Strings(presentKeys)
	for _, key := range presentKeys {
		if knownBlocks[strings.ToLower(key)] {
			continue
		}
		def.Unsupported = append(def.Unsupported, key)
	}
}

// Validate reports every missing or malformed required element with a
// three-level location: block, then field, then reason.
func Validate(def *Definition) []string {
	var problems []string
	add := func(block, field, reason string) {
		problems = append(problems, fmt.Sprintf("%s.%s: %s", block, field, reason))
	}

	if strings.TrimSpace(def.ID) == "" {
		add("top", "id", "缺失")
	}
	if strings.TrimSpace(def.Name) == "" {
		add("top", "name", "缺失")
	}
	if strings.TrimSpace(def.Type) == "" {
		add("top", "type", "缺失")
	}
	if len(def.Links) == 0 {
		add("top", "links", "至少需要一个站点地址")
	}
	for i, link := range def.Links {
		if !strings.HasPrefix(link, "http://") && !strings.HasPrefix(link, "https://") {
			add("top", fmt.Sprintf("links[%d]", i), "只接受 http/https")
		}
	}
	if _, ok := def.Caps.Modes["search"]; !ok {
		add("caps", "modes.search", "缺失")
	}
	if def.Search == nil {
		add("search", "-", "块缺失")
	} else {
		if def.Search.Path == "" && len(def.Search.Paths) == 0 {
			add("search", "path", "path 与 paths 至少需要一个")
		}
		if def.Search.Rows == nil || def.Search.Rows.Selector == "" {
			add("search", "rows.selector", "缺失")
		}
		if len(def.Search.Fields.Entries) == 0 {
			add("search", "fields", "至少需要一个字段")
		}
		if _, ok := def.Search.Fields.Get("title"); !ok {
			add("search", "fields.title", "缺失")
		}
		// A result needs a download URL or an infohash; the latter is turned into
		// a magnet link by the extractor. A download block may also supply it.
		_, hasDownload := def.Search.Fields.Get("download")
		_, hasMagnet := def.Search.Fields.Get("magnet")
		_, hasInfoHash := def.Search.Fields.Get("infohash")
		hasDownloadBlock := def.Download != nil && len(def.Download.Selectors) > 0
		if !hasDownload && !hasMagnet && !hasInfoHash && !hasDownloadBlock {
			add("search", "fields.download", "需要一个下载链接或 infohash 字段")
		}
	}
	if def.Login != nil {
		method := strings.ToLower(strings.TrimSpace(def.Login.Method))
		switch method {
		case "", "form", "post", "get":
			if def.Login.Path == "" {
				add("login", "path", "缺失")
			}
		case "cookie":
			if def.Login.Inputs.Len() == 0 {
				add("login", "inputs", "cookie 方式需要 inputs.cookie")
			}
		default:
			add("login", "method", "只支持 form/post/get/cookie")
		}
	}
	return problems
}

// CategoryGroupLabel maps a Cardigann category name to a top-level Newznab
// group label used by the template browser's category filter.
func CategoryGroupLabel(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasPrefix(lower, "movies"):
		return "电影"
	case strings.HasPrefix(lower, "tv"), strings.HasPrefix(lower, "anime"):
		return "剧集"
	case strings.HasPrefix(lower, "audio"):
		return "音乐"
	case strings.HasPrefix(lower, "books"):
		return "书籍"
	case strings.HasPrefix(lower, "pc"):
		return "软件"
	case strings.HasPrefix(lower, "console"):
		return "游戏"
	case strings.HasPrefix(lower, "xxx"):
		return "成人"
	case strings.HasPrefix(lower, "other"):
		return "其它"
	}
	return ""
}

// SearchModes reports which structured search modes the definition supports.
func (def *Definition) SearchModes() []string {
	modes := make([]string, 0, len(def.Caps.Modes))
	for mode := range def.Caps.Modes {
		modes = append(modes, mode)
	}
	sort.Strings(modes)
	return modes
}

// SupportsMovieSearch reports whether the definition accepts a movie search.
func (def *Definition) SupportsMovieSearch() bool {
	return len(def.Caps.Modes["movie-search"]) > 0
}

// SupportsTVSearch reports whether the definition accepts a TV search.
func (def *Definition) SupportsTVSearch() bool {
	return len(def.Caps.Modes["tv-search"]) > 0
}

// NeedsCaptcha reports whether login requires manual captcha input.
func (def *Definition) NeedsCaptcha() bool {
	return def.Login != nil && def.Login.Captcha != nil
}

// UsesLogin reports whether the definition authenticates first.
func (def *Definition) UsesLogin() bool {
	return def.Login != nil && (def.Login.Path != "" || def.Login.Inputs.Len() > 0)
}

// MinIntervalMS is the request interval in milliseconds, taking the definition
// requestdelay when it is larger than the default.
func (def *Definition) MinIntervalMS(defaultMS int) int {
	ms := int(def.RequestDelay * 1000)
	if ms > defaultMS {
		return ms
	}
	return defaultMS
}
