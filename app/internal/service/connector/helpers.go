package connector

import (
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
)

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// cookiesFromResponse collects Set-Cookie values into a name→value map.
func cookiesFromResponse(resp *httpclient.Response) map[string]string {
	out := map[string]string{}
	if resp == nil || resp.Header == nil {
		return out
	}
	for _, raw := range resp.Header.Values("Set-Cookie") {
		cookie := parseSetCookie(raw)
		if cookie != nil {
			out[cookie.Name] = cookie.Value
		}
	}
	return out
}

func parseSetCookie(raw string) *http.Cookie {
	header := http.Header{}
	header.Add("Set-Cookie", raw)
	resp := http.Response{Header: header}
	for _, c := range resp.Cookies() {
		return c
	}
	return nil
}

// containsSelector reports whether a CSS selector matches anything in body.
// Login error blocks use plain selectors such as `div.error`.
func containsSelector(body []byte, selector string) bool {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return false
	}
	return doc.Find(selector).Length() > 0
}

// extractFragment pulls an attribute or text from a selector in a document.
func extractFragment(body []byte, selector, attribute string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return ""
	}
	nodes := doc.Find(selector)
	if nodes.Length() == 0 {
		return ""
	}
	first := nodes.First()
	if attribute != "" {
		v, _ := first.Attr(attribute)
		return strings.TrimSpace(v)
	}
	return strings.TrimSpace(first.Text())
}

// absoluteURL resolves a possibly relative link against the site base.
func absoluteURL(base, link string) string {
	link = strings.TrimSpace(link)
	if link == "" {
		return ""
	}
	lower := strings.ToLower(link)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "magnet:") {
		return link
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return link
	}
	rel, err := url.Parse(link)
	if err != nil {
		return link
	}
	return baseURL.ResolveReference(rel).String()
}
