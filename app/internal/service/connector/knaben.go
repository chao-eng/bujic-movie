package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
)

// KnabenConnector talks to the Knaben meta-search API. Prowlarr implements this
// site as a native C# indexer (POST JSON), so it cannot be expressed as a
// Cardigann YAML definition.
type KnabenConnector struct {
	name    string
	baseURL string
	proxy   string
	exec    *httpclient.Executor
	allowed []string
	limitMS int
	session Session
}

// SetSession installs the manually supplied browser session.
func (k *KnabenConnector) SetSession(session Session) { k.session = session }

func (k *KnabenConnector) headers() map[string]string {
	h := map[string]string{"Content-Type": "application/json", "Accept": "application/json", "User-Agent": userAgent()}
	k.session.Apply(h)
	return h
}

// KnabenAPIEndpoint is the upstream JSON endpoint.
const KnabenAPIEndpoint = "https://api.knaben.org/v1"

// NewKnaben builds the connector. baseURL defaults to the public API host.
func NewKnaben(name, baseURL, proxyURL string, exec *httpclient.Executor, limitMS int) *KnabenConnector {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = KnabenAPIEndpoint
	}
	if limitMS <= 0 {
		limitMS = 2000
	}
	return &KnabenConnector{
		name: name, baseURL: baseURL, proxy: proxyURL, exec: exec,
		allowed: httpclient.HostsFromLinks([]string{KnabenAPIEndpoint, baseURL}),
		limitMS: limitMS,
	}
}

func (k *KnabenConnector) Name() string {
	if k.name != "" {
		return k.name
	}
	return "Knaben"
}

func (k *KnabenConnector) rateKey() string { return "knaben:" + k.baseURL }

// knabenResponse mirrors the upstream JSON envelope.
type knabenResponse struct {
	Hits []knabenHit `json:"hits"`
}

type knabenHit struct {
	Title      string `json:"title"`
	CategoryID []int  `json:"categoryId"`
	Hash       string `json:"hash"`
	Details    string `json:"details"`
	Link       string `json:"link"`
	MagnetURL  string `json:"magnetUrl"`
	Bytes      int64  `json:"bytes"`
	Seeders    int    `json:"seeders"`
	Peers      int    `json:"peers"`
	Date       string `json:"date"`
}

// Test performs a small query to prove the endpoint answers.
func (k *KnabenConnector) Test(ctx context.Context) (*Capabilities, error) {
	start := time.Now()
	hits, err := k.query(ctx, Query{Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(hits) > 0 {
		// A response means the endpoint is reachable; an empty result is valid.
	}
	return &Capabilities{
		Name:        k.Name(),
		Protocol:    "torrent",
		Categories:  len(knabenCategoryMap),
		SearchModes: []string{"search", "tv-search", "movie-search"},
		Elapsed:     time.Since(start),
		LoginState:  "无需登录",
	}, nil
}

// Search runs the JSON query and maps hits onto normalised releases.
func (k *KnabenConnector) Search(ctx context.Context, q Query) ([]Release, error) {
	hits, err := k.query(ctx, q)
	if err != nil {
		return nil, err
	}
	releases := make([]Release, 0, len(hits))
	for _, hit := range hits {
		link := strings.TrimSpace(hit.MagnetURL)
		if link == "" {
			link = strings.TrimSpace(hit.Link)
		}
		if link == "" && hit.Hash != "" {
			link = "magnet:?xt=urn:btih:" + hit.Hash
		}
		if strings.TrimSpace(hit.Title) == "" || link == "" {
			continue
		}
		rel := Release{
			Title:        hit.Title,
			DownloadURL:  link,
			InfoHash:     strings.ToLower(hit.Hash),
			SizeBytes:    hit.Bytes,
			Seeders:      hit.Seeders,
			SeedersKnown: true,
			Peers:        hit.Peers,
			PublishDate:  normaliseDate(hit.Date),
			Categories:   knabenCategoriesToNewznab(hit.CategoryID),
			IndexerName:  k.Name(),
		}
		if strings.HasPrefix(strings.ToLower(link), "magnet:") {
			rel.DownloadType = "magnet"
		} else {
			rel.DownloadType = "torrent"
		}
		releases = append(releases, rel)
	}
	return releases, nil
}

func (k *KnabenConnector) query(ctx context.Context, q Query) ([]knabenHit, error) {
	body := map[string]any{
		"order_by":        "date",
		"order_direction": "desc",
		"from":            0,
		"size":            clampSize(q.Limit),
		"hide_unsafe":     true,
	}
	if keyword := strings.TrimSpace(q.Keyword); keyword != "" {
		body["search_type"] = "100%"
		body["search_field"] = "title"
		body["query"] = keyword
	}
	if cats, ok := knabenCategoriesFor(q.Categories); ok && len(cats) > 0 {
		body["categories"] = cats
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, NewError(KindUnsupported, "查询构建失败", "", err)
	}

	resp, err := k.exec.Do(ctx, httpclient.Request{
		Method:    "POST",
		URL:       k.baseURL,
		Header:    k.headers(),
		Body:      bytes.NewReader(payload),
		Allowed:   k.allowed,
		RateKey:   k.rateKey(),
		RateLimit: time.Duration(k.limitMS) * time.Millisecond,
		Proxy:     k.proxy,
		NoRetry:   true,
	})
	if err != nil {
		return nil, translateHTTPError(err)
	}

	var parsed knabenResponse
	if err := json.Unmarshal(resp.Body, &parsed); err != nil {
		return nil, NewError(KindParse, "返回内容不是有效的 JSON", "search", err)
	}
	return parsed.Hits, nil
}

func clampSize(size int) int {
	if size <= 0 {
		return 100
	}
	if size > 100 {
		return 100
	}
	return size
}

// ResolveDownload passes the magnet or torrent link through unchanged.
func (k *KnabenConnector) ResolveDownload(ctx context.Context, rel Release) (*DownloadRef, error) {
	if rel.DownloadURL == "" {
		return nil, NewError(KindParse, "结果缺少下载链接", "download", nil)
	}
	kind := rel.DownloadType
	if kind == "" {
		kind = "torrent"
		if strings.HasPrefix(strings.ToLower(rel.DownloadURL), "magnet:") {
			kind = "magnet"
		}
	}
	return &DownloadRef{URL: rel.DownloadURL, InfoHash: rel.InfoHash, Type: kind, Title: rel.Title}, nil
}

// SupportsCategories reports whether any requested Newznab group is served.
func (k *KnabenConnector) SupportsCategories(categories []int) bool {
	if len(categories) == 0 {
		return true
	}
	for _, want := range categories {
		if _, ok := knabenCategoriesFor([]int{want}); ok {
			return true
		}
	}
	// Unknown category means "do not filter": keep the site in the fan-out.
	for _, want := range categories {
		if !knownNewznabGroup(want) {
			return true
		}
	}
	return false
}

// knabenCategoryGroups maps a Newznab top-level group to Knaben category ids.
var knabenCategoryGroups = map[int][]int{
	1000: {7000000},
	2000: {3000000},
	3000: {6007000},
	4000: {4000000},
	5000: {1000000, 2000000},
	7000: {9000000},
	8000: {8000000},
}

// knabenCategoriesFor maps requested Newznab categories onto Knaben ids and
// reports whether any mapping exists.
func knabenCategoriesFor(categories []int) ([]int, bool) {
	if len(categories) == 0 {
		return nil, false
	}
	seen := map[int]bool{}
	out := []int{}
	for _, cat := range categories {
		// Anime (5070) lives in its own Knaben group, not under TV.
		if cat == 5070 {
			if !seen[6007000] && !seen[6000000] {
				seen[6000000] = true
				out = append(out, 6000000)
			}
			continue
		}
		group := cat / 100 * 100
		if group == 0 {
			group = cat
		}
		for _, id := range knabenCategoryGroups[group] {
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, len(out) > 0
}

// knownNewznabGroup reports whether a category belongs to a group Knaben serves.
func knownNewznabGroup(cat int) bool {
	_, ok := knabenCategoryGroups[cat/100*100]
	return ok
}

// knabenCategoryMap converts common Knaben category ids to Newznab ids.
var knabenCategoryMap = map[int]int{
	1000000: 5000, 2000000: 5000, 2004000: 5080, 2007000: 5070,
	3000000: 2000, 3001000: 2040, 3002000: 2030, 3003000: 2045,
	3004000: 2010, 3005000: 2010, 3007000: 2060,
	4000000: 4000, 4001000: 4050, 4002000: 4030,
	5000000: 6000,
	6000000: 5070, 6001000: 5070, 6002000: 5070, 6003000: 5070,
	6004000: 5070, 6007000: 3000,
	7000000: 1000, 7001000: 1080, 7009000: 1030,
	8000000: 4000, 9000000: 7000, 9001000: 7020, 9002000: 7030,
	10000000: 8000,
}

func knabenCategoriesToNewznab(ids []int) []int {
	out := make([]int, 0, len(ids))
	seen := map[int]bool{}
	for _, id := range ids {
		newznab, ok := knabenCategoryMap[id]
		if !ok {
			// Fall back to the top-level group of the id.
			top := id / 1000000 * 1000000
			newznab = knabenCategoryMap[top]
		}
		if newznab == 0 || seen[newznab] {
			continue
		}
		seen[newznab] = true
		out = append(out, newznab)
	}
	if len(out) == 0 {
		out = append(out, 8000)
	}
	return out
}

// normaliseDate accepts the ISO timestamps Knaben returns and keeps them as
// RFC3339. Values without a timezone are treated as +01:00 like Prowlarr does.
func normaliseDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return raw
}
