package cardigann

import (
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrUnknownFilter is returned when a definition uses a filter this build does
// not implement. The connector turns it into a `partial` marker instead of
// failing the whole search (TST-016).
var ErrUnknownFilter = errors.New("cardigann: unknown filter")

// KnownFilters is the whitelist from REQ-09. Any other name is reported as
// unsupported rather than executed.
var KnownFilters = map[string]bool{
	"querystring": true, "timeparse": true, "dateparse": true, "regexp": true,
	"re_replace": true, "split": true, "replace": true, "trim": true,
	"prepend": true, "append": true, "tolower": true, "toupper": true,
	"base64decode": true, "base64encode": true, "urldecode": true,
	"urlencode": true, "htmldecode": true, "htmlencode": true,
	"timeago": true, "reltime": true, "fuzzytime": true, "validfilename": true,
	"diacritics": true, "jsonjoinarray": true, "hexdump": true, "strdump": true,
	"validate": true, "and": true, "or": true, "eq": true, "ne": true,
	"has": true, "not": true, "contains": true,
}

// nowFn is replaceable so date filters are deterministic in tests.
var nowFn = time.Now

// ApplyFilters runs a filter chain over an input value without a template
// context, so placeholder arguments are used literally.
func ApplyFilters(input string, filters []FilterBlock) (string, error) {
	return applyFilterChain(input, filters, nil)
}

// ApplyFiltersWithContext runs a filter chain and renders template placeholders
// inside the arguments that definitions write as templates. Upstream Cardigann
// expands `append`/`prepend` values and the replacement of
// `replace`/`re_replace`; YTS builds its whole title suffix this way.
func ApplyFiltersWithContext(input string, filters []FilterBlock, ctx TemplateContext) (string, error) {
	return applyFilterChain(input, filters, &ctx)
}

func applyFilterChain(input string, filters []FilterBlock, ctx *TemplateContext) (string, error) {
	value := input
	for _, f := range filters {
		next, err := applyFilter(value, f, ctx)
		if err != nil {
			return value, fmt.Errorf("filter %s: %w", f.Name, err)
		}
		value = next
	}
	return value, nil
}

// renderFilterArgs expands templates in a filter argument value. An argument
// without a placeholder is returned unchanged, and a broken template leaves the
// literal text in place rather than dropping the value silently.
func renderFilterArgs(v any, ctx *TemplateContext) any {
	if ctx == nil {
		return v
	}
	switch t := v.(type) {
	case string:
		if !strings.Contains(t, "{{") {
			return t
		}
		rendered, err := Render(t, *ctx)
		if err != nil {
			return t
		}
		return rendered
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = renderFilterArgs(item, ctx)
		}
		return out
	case []string:
		out := make([]string, len(t))
		for i, item := range t {
			out[i] = asString(renderFilterArgs(item, ctx))
		}
		return out
	}
	return v
}

func applyFilter(in string, f FilterBlock, ctx *TemplateContext) (string, error) {
	name := strings.ToLower(strings.TrimSpace(f.Name))
	if !KnownFilters[name] {
		return in, fmt.Errorf("%w: %s", ErrUnknownFilter, f.Name)
	}

	switch name {
	case "trim":
		return strings.TrimSpace(in), nil
	case "tolower":
		return strings.ToLower(in), nil
	case "toupper":
		return strings.ToUpper(in), nil
	case "prepend":
		return asString(renderFilterArgs(f.Args, ctx)) + in, nil
	case "append":
		return in + asString(renderFilterArgs(f.Args, ctx)), nil
	case "replace":
		args := argsList(f.Args)
		if len(args) != 2 {
			return in, fmt.Errorf("replace needs 2 args")
		}
		return strings.ReplaceAll(in, args[0], asString(renderFilterArgs(args[1], ctx))), nil
	case "re_replace":
		args := argsList(f.Args)
		if len(args) != 2 {
			return in, fmt.Errorf("re_replace needs 2 args")
		}
		re, err := regexp.Compile(normaliseRegex(args[0]))
		if err != nil {
			return in, err
		}
		return re.ReplaceAllString(in, asString(renderFilterArgs(args[1], ctx))), nil
	case "regexp":
		args := argsList(f.Args)
		if len(args) == 0 {
			return in, fmt.Errorf("regexp needs a pattern")
		}
		re, err := regexp.Compile(normaliseRegex(args[0]))
		if err != nil {
			return in, err
		}
		m := re.FindStringSubmatch(in)
		if m == nil {
			return "", nil
		}
		if len(args) > 1 {
			idx, err := strconv.Atoi(args[1])
			if err != nil || idx < 0 || idx >= len(m) {
				return "", fmt.Errorf("regexp group %q out of range", args[1])
			}
			return m[idx], nil
		}
		return m[0], nil
	case "split":
		args := argsList(f.Args)
		if len(args) == 0 {
			return in, fmt.Errorf("split needs a separator")
		}
		parts := strings.Split(in, args[0])
		if len(args) > 1 {
			idx, err := strconv.Atoi(args[1])
			if err != nil || idx < 0 || idx >= len(parts) {
				return "", nil
			}
			return parts[idx], nil
		}
		return strings.Join(parts, " "), nil
	case "base64encode":
		return base64.StdEncoding.EncodeToString([]byte(in)), nil
	case "base64decode":
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in))
		if err != nil {
			return in, nil
		}
		return string(raw), nil
	case "urlencode":
		return url.QueryEscape(in), nil
	case "urldecode":
		out, err := url.QueryUnescape(in)
		if err != nil {
			return in, nil
		}
		return out, nil
	case "htmlencode":
		return html.EscapeString(in), nil
	case "htmldecode":
		return html.UnescapeString(in), nil
	case "querystring":
		args := argsList(f.Args)
		if len(args) == 0 {
			return in, fmt.Errorf("querystring needs a key")
		}
		values, err := url.ParseQuery(strings.TrimPrefix(in, "?"))
		if err != nil {
			return "", nil
		}
		return values.Get(args[0]), nil
	case "dateparse":
		return applyDateParse(in, f.Args)
	case "timeparse":
		return applyTimeParse(in, f.Args)
	case "timeago", "reltime", "fuzzytime":
		return applyFuzzyTime(in, name), nil
	case "validfilename":
		return sanitizeFilename(in), nil
	case "diacritics":
		return stripDiacritics(in), nil
	case "hexdump":
		return fmt.Sprintf("%x", []byte(in)), nil
	case "strdump":
		return in, nil
	case "jsonjoinarray":
		return strings.Join(strings.Split(in, ","), ""), nil
	case "validate":
		return applyValidate(in, f.Args)
	case "has":
		if len(argsList(f.Args)) == 0 {
			return in, nil
		}
		if !strings.Contains(in, argsList(f.Args)[0]) {
			return "", nil
		}
		return in, nil
	case "contains":
		if len(argsList(f.Args)) == 0 {
			return in, nil
		}
		for _, a := range argsList(f.Args) {
			if strings.Contains(in, a) {
				return "true", nil
			}
		}
		return "false", nil
	case "not":
		if strings.EqualFold(strings.TrimSpace(in), "true") {
			return "false", nil
		}
		return "true", nil
	case "eq", "ne":
		args := argsList(f.Args)
		if len(args) == 0 {
			return "", fmt.Errorf("%s needs an argument", name)
		}
		equal := strings.EqualFold(strings.TrimSpace(in), strings.TrimSpace(args[0]))
		if name == "ne" {
			equal = !equal
		}
		return strconv.FormatBool(equal), nil
	case "and", "or":
		return applyBoolCombine(in, name, f.Args)
	}
	return in, nil
}

func applyDateParse(in string, raw any) (string, error) {
	args := argsList(raw)
	if len(args) == 0 {
		return in, fmt.Errorf("dateparse needs a layout")
	}
	layouts := make([]string, 0, len(args))
	for _, layout := range args {
		layouts = append(layouts, javaLayoutToGo(layout))
	}
	value := strings.TrimSpace(in)
	// Already ISO formatted.
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z07:00"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC().Format(time.RFC3339), nil
		}
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC().Format(time.RFC3339), nil
		}
	}
	// A declared layout is often slightly off for real rows: the zone token is
	// missing, or the site leaves a language particle behind ("5 October 2024
	// г. 10:23:04 +03:00"). Accept those rather than shipping raw text, which
	// costs the UI its relative time and date sorting.
	if t, ok := parseLooseDate(value); ok {
		return t.UTC().Format(time.RFC3339), nil
	}
	return value, nil
}

// looseDateRe captures "5 October 2024 г. 10:23:04 +03:00"-style values: a day,
// a month name, a year, then any filler before an optional time and offset.
var looseDateRe = regexp.MustCompile(`(?i)^(\d{1,2})\s+([a-z]{3,})\s+(\d{4})\b[^0-9]*(?:(\d{1,2}):(\d{2})(?::(\d{2}))?)?[^+-]*(?:([+-]\d{2}:?\d{2}))?`)

func parseLooseDate(value string) (time.Time, bool) {
	m := looseDateRe.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return time.Time{}, false
	}
	month, err := time.Parse("January", capitalise(m[2]))
	if err != nil {
		// Three-letter abbreviations are common too.
		month, err = time.Parse("Jan", capitalise(m[2]))
		if err != nil {
			return time.Time{}, false
		}
	}
	day, err := strconv.Atoi(m[1])
	if err != nil {
		return time.Time{}, false
	}
	year, err := strconv.Atoi(m[3])
	if err != nil {
		return time.Time{}, false
	}
	hour, minute, second := 0, 0, 0
	if m[4] != "" {
		hour, _ = strconv.Atoi(m[4])
		minute, _ = strconv.Atoi(m[5])
		if m[6] != "" {
			second, _ = strconv.Atoi(m[6])
		}
	}
	loc := time.UTC
	if m[7] != "" {
		name := m[7]
		if len(name) == 5 {
			// "+0300" → "+03:00" so one layout parses both spellings.
			name = name[:3] + ":" + name[3:]
		}
		if t, err := time.Parse("-07:00", name); err == nil {
			_, secs := t.Zone()
			loc = time.FixedZone("", secs)
		}
	}
	return time.Date(year, month.Month(), day, hour, minute, second, 0, loc), true
}

// capitalise upper-cases the first letter so a month name matches Go's layout
// names ("october" → "October"). strings.Title is deprecated and would
// upper-case every word.
func capitalise(s string) string {
	lower := strings.ToLower(strings.TrimSpace(s))
	if lower == "" {
		return lower
	}
	return strings.ToUpper(lower[:1]) + lower[1:]
}

func applyTimeParse(in string, raw any) (string, error) {
	args := argsList(raw)
	if len(args) == 0 {
		return in, fmt.Errorf("timeparse needs a layout")
	}
	args = []string{javaLayoutToGo(args[0])}
	value := strings.TrimSpace(in)
	loc := time.UTC
	if len(args) > 1 {
		if l, err := time.LoadLocation(args[1]); err == nil {
			loc = l
		}
	}
	if t, err := time.ParseInLocation(args[0], value, loc); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	return value, nil
}

// applyFuzzyTime turns a relative timestamp from a tracker listing into an
// absolute one. Definitions emit things like "3 hours ago", "yesterday" or
// "12:25am" (today); storing the raw text makes the UI unable to tell how old
// a release is, and makes sorting by 发布时间 compare strings.
func applyFuzzyTime(in, filterName string) string {
	value := strings.TrimSpace(in)
	if value == "" {
		return value
	}
	now := nowFn().UTC()
	lower := strings.ToLower(value)
	switch lower {
	case "just now":
		return now.Format(time.RFC3339)
	case "now", "today":
		// 定义里 date 找不到值时常用 now 兜底（1337x 就是这样），把它当空值，
		// 免得整页结果都被说成「刚刚」。
		return ""
	case "yesterday":
		return now.Add(-24 * time.Hour).Format(time.RFC3339)
	}

	// "<number> <unit> ago" / "<number><unit>" — upstream's timeago layout.
	if m := timeAgoRe.FindStringSubmatch(lower); m != nil {
		n, err := strconv.Atoi(m[1])
		if err == nil {
			unit := m[2]
			var step time.Duration
			switch {
			case strings.HasPrefix(unit, "s"), strings.HasPrefix(unit, "sec"):
				step = time.Second
			case strings.HasPrefix(unit, "m") && !strings.HasPrefix(unit, "mo"):
				step = time.Minute
			case strings.HasPrefix(unit, "h"):
				step = time.Hour
			case strings.HasPrefix(unit, "d"):
				step = 24 * time.Hour
			case strings.HasPrefix(unit, "w"):
				step = 7 * 24 * time.Hour
			case strings.HasPrefix(unit, "mo"):
				step = 30 * 24 * time.Hour
			case strings.HasPrefix(unit, "y"):
				step = 365 * 24 * time.Hour
			}
			if step > 0 {
				return now.Add(-time.Duration(n) * step).Format(time.RFC3339)
			}
		}
	}

	// A bare clock time means today (1337x shows "12:25am" for today's rows).
	if m := clockRe.FindStringSubmatch(lower); m != nil {
		hour, _ := strconv.Atoi(m[1])
		minute, _ := strconv.Atoi(m[2])
		ampm := m[3]
		switch ampm {
		case "am":
			if hour == 12 {
				hour = 0
			}
		case "pm":
			if hour != 12 {
				hour += 12
			}
		}
		day := now
		if hour > now.Hour() || (hour == now.Hour() && minute > now.Minute()) {
			// A time later than now must belong to yesterday.
			day = now.Add(-24 * time.Hour)
		}
		stamp := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, time.UTC)
		return stamp.Format(time.RFC3339)
	}

	if filterName == "fuzzytime" {
		// Cardigann's fuzzytime only ever sees "today"-style values; anything
		// else is already absolute and parsed below by dateparse.
		if t, err := time.Parse(time.RFC3339, value); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return value
}

var (
	timeAgoRe = regexp.MustCompile(`^(?:about\s+|over\s+)?(\d+)\s*(seconds?|secs?|s|minutes?|mins?|m|hours?|hrs?|h|days?|d|weeks?|w|months?|mo|years?|y)\s*(?:ago)?$`)
	clockRe   = regexp.MustCompile(`^(\d{1,2}):(\d{2})\s*(am|pm)?$`)
)

func applyValidate(in string, raw any) (string, error) {
	args := argsList(raw)
	if len(args) == 0 {
		return in, nil
	}
	if len(args) >= 2 {
		re, err := regexp.Compile(normaliseRegex(args[0]))
		if err != nil {
			return in, err
		}
		matched := re.MatchString(in)
		if (args[1] == "in") == matched {
			return in, nil
		}
		return "", nil
	}
	// One argument is Cardigann's list form: keep the tokens of the input that
	// also appear in the argument list, joined by ", ". Genre fields rely on it
	// (Cathode Ray Tube, TorrentLeech and friends pass a comma separated list).
	allowed := map[string]bool{}
	for _, token := range splitValidateTokens(args[0]) {
		allowed[token] = true
	}
	var kept []string
	seen := map[string]bool{}
	for _, token := range splitValidateTokens(in) {
		if allowed[token] && !seen[token] {
			seen[token] = true
			kept = append(kept, token)
		}
	}
	return strings.Join(kept, ", "), nil
}

// validateDelimiters mirrors upstream's token separators for the list form of
// the validate filter.
func splitValidateTokens(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		switch r {
		case ',', ' ', '/', ')', '(', '.', ';', '[', ']', '"', '|', ':':
			return true
		}
		return false
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func applyBoolCombine(in, name string, raw any) (string, error) {
	args := argsList(raw)
	current := strings.EqualFold(strings.TrimSpace(in), "true")
	for _, a := range args {
		v := strings.EqualFold(strings.TrimSpace(a), "true")
		if name == "and" {
			current = current && v
		} else {
			current = current || v
		}
	}
	return strconv.FormatBool(current), nil
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprint(t)
	}
}

func argsList(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return []string{t}
	case []string:
		out := make([]string, 0, len(t))
		for _, s := range t {
			out = append(out, asString(s))
		}
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, s := range t {
			out = append(out, asString(s))
		}
		return out
	default:
		return []string{asString(t)}
	}
}

// javaLayoutToGo converts the date patterns Cardigann definitions use (Java /
// JS style) into Go reference layouts. Unknown characters pass through so a
// definition that already uses a Go layout still works.
func javaLayoutToGo(layout string) string {
	replacer := strings.NewReplacer(
		"yyyy", "2006",
		"yy", "06",
		"MMMM", "January",
		"MMM", "Jan",
		"MM", "01",
		"dd", "02",
		"HH", "15",
		"hh", "03",
		"mm", "04",
		"ss", "05",
		"ZZ", "-0700",
		"zzz", "MST",
		"Z", "-07:00",
	)
	// Only translate when the value actually looks like a Java pattern.
	if strings.Contains(layout, "yyyy") || strings.Contains(layout, "HH") || strings.Contains(layout, "zzz") {
		return replacer.Replace(layout)
	}
	return layout
}

var filenameIllegal = regexp.MustCompile(`[\\/:*?"<>|]`)

func sanitizeFilename(in string) string {
	cleaned := filenameIllegal.ReplaceAllString(in, " ")
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	return strings.TrimSpace(cleaned)
}

var diacriticReplacer = strings.NewReplacer(
	"à", "a", "á", "a", "â", "a", "ã", "a", "ä", "a", "å", "a",
	"è", "e", "é", "e", "ê", "e", "ë", "e",
	"ì", "i", "í", "i", "î", "i", "ï", "i",
	"ò", "o", "ó", "o", "ô", "o", "õ", "o", "ö", "o",
	"ù", "u", "ú", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n", "ß", "ss",
	"À", "A", "Á", "A", "Â", "A", "Ã", "A", "Ä", "A", "Å", "A",
	"È", "E", "É", "E", "Ê", "E", "Ë", "E",
	"Ì", "I", "Í", "I", "Î", "I", "Ï", "I",
	"Ò", "O", "Ó", "O", "Ô", "O", "Õ", "O", "Ö", "O",
	"Ù", "U", "Ú", "U", "Û", "U", "Ü", "U",
	"Ç", "C", "Ñ", "N",
)

func stripDiacritics(in string) string {
	return diacriticReplacer.Replace(in)
}
