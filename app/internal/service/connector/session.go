package connector

import "strings"

// Session carries a manually supplied browser session. It exists because some
// sites sit behind a Cloudflare interactive challenge: the user completes the
// challenge in their own browser, then pastes the resulting cookies and the
// exact User-Agent here. Both are required, since cf_clearance is bound to the
// IP and the UA that solved the challenge (PRD D-09 amendment).
type Session struct {
	Cookies   string
	UserAgent string
}

// SetSession replaces the manual session for a connector.
func (s Session) Apply(headers map[string]string) {
	if headers == nil {
		return
	}
	if ua := strings.TrimSpace(s.UserAgent); ua != "" {
		headers["User-Agent"] = ua
	} else if headers["User-Agent"] == "" {
		headers["User-Agent"] = userAgent()
	}
	cookies := strings.TrimSpace(s.Cookies)
	if cookies == "" {
		return
	}
	if existing := headers["Cookie"]; existing != "" {
		headers["Cookie"] = existing + "; " + cookies
	} else {
		headers["Cookie"] = cookies
	}
}

// NormaliseCookies cleans a pasted cookie string: it accepts a full Cookie
// header, one "name=value" per line, or a Chrome DevTools copy-paste.
func NormaliseCookies(raw string) string {
	raw = strings.ReplaceAll(raw, "\r", "\n")
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == ';'
	})
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || !strings.Contains(part, "=") {
			continue
		}
		name := strings.TrimSpace(strings.SplitN(part, "=", 2)[0])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, part)
	}
	return strings.Join(out, "; ")
}

// CookieNames lists cookie names without values, for masking in read APIs.
func CookieNames(raw string) []string {
	names := make([]string, 0, 8)
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.Index(part, "="); idx > 0 {
			names = append(names, strings.TrimSpace(part[:idx]))
		}
	}
	return names
}
