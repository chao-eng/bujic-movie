package connector

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
)

// TorznabConnector talks to any Torznab/Newznab endpoint.
type TorznabConnector struct {
	name    string
	baseURL string
	apiKey  string
	proxy   string
	exec    *httpclient.Executor
	allowed []string
	limitMS int
	session Session
}

// SetSession installs the manually supplied browser session.
func (t *TorznabConnector) SetSession(session Session) { t.session = session }

func (t *TorznabConnector) withSession() map[string]string {
	headers := map[string]string{"User-Agent": userAgent()}
	t.session.Apply(headers)
	return headers
}

// NewTorznab builds a connector for a generic endpoint.
func NewTorznab(name, baseURL, apiKey, proxyURL string, exec *httpclient.Executor, limitMS int) *TorznabConnector {
	allowed := httpclient.HostsFromLinks([]string{baseURL})
	if limitMS <= 0 {
		limitMS = 2000
	}
	return &TorznabConnector{
		name: name, baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey,
		proxy: proxyURL, exec: exec, allowed: allowed, limitMS: limitMS,
	}
}

func (t *TorznabConnector) Name() string { return t.name }

func (t *TorznabConnector) rateKey() string { return "torznab:" + t.baseURL }

// capsResponse mirrors the Torznab capability document.
type capsResponse struct {
	XMLName xml.Name `xml:"caps"`
	Server  struct {
		Title string `xml:"title,attr"`
	} `xml:"server"`
	Limits struct {
		Max     int `xml:"max,attr"`
		Default int `xml:"default,attr"`
	} `xml:"limits"`
	Searching struct {
		Raw   available `xml:"search"`
		Tv    available `xml:"tv-search"`
		Movie available `xml:"movie-search"`
		Music available `xml:"music-search"`
		Book  available `xml:"book-search"`
	} `xml:"searching"`
	Categories struct {
		Items []struct {
			ID   int    `xml:"id,attr"`
			Name string `xml:"name,attr"`
			Sub  []struct {
				ID   int    `xml:"id,attr"`
				Name string `xml:"name,attr"`
			} `xml:"subcat"`
		} `xml:"category"`
	} `xml:"categories"`
}

type available struct {
	Available string `xml:"available,attr"`
	Params    string `xml:"supportedParams,attr"`
}

func (a available) enabled() bool {
	return strings.EqualFold(a.Available, "yes") || strings.EqualFold(a.Available, "true")
}

// Test fetches the capability document.
func (t *TorznabConnector) Test(ctx context.Context) (*Capabilities, error) {
	start := time.Now()
	resp, err := t.exec.Do(ctx, httpclient.Request{
		URL:       t.capsURL(),
		Header:    t.withSession(),
		Timeout:   httpclient.DefaultTimeout,
		Allowed:   t.allowed,
		RateKey:   t.rateKey(),
		RateLimit: time.Duration(t.limitMS) * time.Millisecond,
		Proxy:     t.proxy,
	})
	if err != nil {
		return nil, translateHTTPError(err)
	}
	var caps capsResponse
	if err := xml.Unmarshal(resp.Body, &caps); err != nil {
		return nil, NewError(KindParse, "返回内容不是有效的 Torznab 能力文档", "caps", err)
	}

	categories := 0
	for _, c := range caps.Categories.Items {
		categories++
		categories += len(c.Sub)
	}
	modes := []string{}
	if caps.Searching.Raw.enabled() {
		modes = append(modes, "search")
	}
	if caps.Searching.Tv.enabled() {
		modes = append(modes, "tv-search")
	}
	if caps.Searching.Movie.enabled() {
		modes = append(modes, "movie-search")
	}

	name := t.name
	if name == "" {
		name = caps.Server.Title
	}
	return &Capabilities{
		Name:        name,
		Protocol:    "torrent",
		Categories:  categories,
		SearchModes: modes,
		Elapsed:     time.Since(start),
		LoginState:  "无需登录",
	}, nil
}

// Search runs a Torznab t= query and parses the RSS result.
func (t *TorznabConnector) Search(ctx context.Context, q Query) ([]Release, error) {
	mode := q.Mode
	if mode == "" {
		mode = "search"
	}
	params := url.Values{}
	params.Set("t", mapMode(mode))
	params.Set("apikey", t.apiKey)
	if q.Keyword != "" {
		params.Set("q", q.Keyword)
	}
	if len(q.Categories) > 0 {
		params.Set("cat", joinInts(q.Categories))
	}
	if q.Season > 0 {
		params.Set("season", strconv.Itoa(q.Season))
	}
	if q.Episode > 0 {
		params.Set("ep", strconv.Itoa(q.Episode))
	}
	if q.Limit > 0 {
		params.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.IMDBID != "" {
		params.Set("imdbid", q.IMDBID)
	}

	resp, err := t.exec.Do(ctx, httpclient.Request{
		URL:       t.searchURL() + "?" + params.Encode(),
		Header:    t.withSession(),
		Timeout:   httpclient.DefaultTimeout,
		Allowed:   t.allowed,
		RateKey:   t.rateKey(),
		RateLimit: time.Duration(t.limitMS) * time.Millisecond,
		Proxy:     t.proxy,
	})
	if err != nil {
		return nil, translateHTTPError(err)
	}
	releases, err := parseTorznabRSS(resp.Body)
	if err != nil {
		return nil, NewError(KindParse, "结果解析失败", "search", err)
	}
	for i := range releases {
		releases[i].IndexerName = t.name
	}
	return releases, nil
}

// ResolveDownload returns the release URL unchanged; Torznab links are already
// downloadable and the API key stays in the connector, never in logs.
func (t *TorznabConnector) ResolveDownload(ctx context.Context, rel Release) (*DownloadRef, error) {
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

func (t *TorznabConnector) capsURL() string { return t.baseURL + "/api?t=caps" }

func (t *TorznabConnector) searchURL() string { return t.baseURL + "/api" }

func mapMode(mode string) string {
	switch mode {
	case "tv-search":
		return "tvsearch"
	case "movie-search":
		return "movie"
	default:
		return "search"
	}
}

func joinInts(values []int) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, strconv.Itoa(v))
	}
	return strings.Join(parts, ",")
}

// rssFeed is the Torznab search result envelope.
type rssFeed struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title    string `xml:"title"`
	Link     string `xml:"link"`
	Guid     string `xml:"guid"`
	Comments string `xml:"comments"`
	Details  string `xml:"details"`
	PubDate  string `xml:"pubDate"`
	Size     string `xml:"size"`
	Category []int  `xml:"category"`
	Attrs    []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
	} `xml:"attr"`
	Enclosure struct {
		URL    string `xml:"url,attr"`
		Length string `xml:"length,attr"`
	} `xml:"enclosure"`
}

func parseTorznabRSS(body []byte) ([]Release, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, fmt.Errorf("空响应")
	}
	if body[0] != '<' {
		return nil, fmt.Errorf("返回内容不是 XML/RSS")
	}
	var feed rssFeed
	decoder := xml.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&feed); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("返回内容不是有效的 RSS")
		}
		return nil, err
	}
	releases := make([]Release, 0, len(feed.Channel.Items))
	for _, item := range feed.Channel.Items {
		rel := Release{
			Title:       strings.TrimSpace(item.Title),
			DownloadURL: strings.TrimSpace(item.Enclosure.URL),
			PublishDate: strings.TrimSpace(item.PubDate),
			Categories:  item.Category,
			DetailsURL:  firstNonEmpty(item.Details, item.Comments, item.Guid),
		}
		if rel.DownloadURL == "" {
			rel.DownloadURL = strings.TrimSpace(item.Link)
		}
		if rel.DownloadURL == "" {
			rel.DownloadURL = strings.TrimSpace(item.Guid)
		}
		rel.SizeBytes = parseBytes(item.Size)
		if rel.SizeBytes == 0 {
			rel.SizeBytes = parseBytes(item.Enclosure.Length)
		}
		for _, attr := range item.Attrs {
			switch strings.ToLower(attr.Name) {
			case "seeders":
				rel.Seeders = atoiSafe(attr.Value)
			case "peers":
				rel.Peers = atoiSafe(attr.Value)
			case "infohash":
				rel.InfoHash = strings.ToLower(strings.TrimSpace(attr.Value))
			case "category":
				if v := atoiSafe(attr.Value); v > 0 {
					rel.Categories = append(rel.Categories, v)
				}
			}
		}
		if rel.DownloadURL == "" && rel.InfoHash != "" {
			rel.DownloadURL = "magnet:?xt=urn:btih:" + rel.InfoHash
		}
		rel.DownloadType = "torrent"
		if strings.HasPrefix(strings.ToLower(rel.DownloadURL), "magnet:") {
			rel.DownloadType = "magnet"
		}
		if rel.Title != "" && rel.DownloadURL != "" {
			releases = append(releases, rel)
		}
	}
	return releases, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func parseBytes(raw string) int64 {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return int64(v)
	}
	return 0
}

func atoiSafe(raw string) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return v
}

// translateHTTPError maps transport errors onto connector kinds.
func translateHTTPError(err error) error {
	var hErr *httpclient.Error
	if !asHTTPError(err, &hErr) {
		return NewError(KindUnreachable, "请求失败", "", err)
	}
	switch hErr.Kind {
	case httpclient.KindAuth:
		return NewError(KindAuth, "站点认证失败，请检查账号或密钥", "", err)
	case httpclient.KindTimeout:
		return NewError(KindTimeout, "站点响应超时", "", err)
	case httpclient.KindTooLarge:
		return NewError(KindParse, "站点响应过大", "", err)
	case httpclient.KindChallenge:
		return NewError(KindChallenge, "站点要求浏览器验证，当前不支持绕过", "", err)
	case httpclient.KindBlocked:
		return NewError(KindBlocked, "站点拒绝或限流", "", err)
	default:
		return NewError(KindUnreachable, "无法连接站点", "", err)
	}
}

func asHTTPError(err error, target **httpclient.Error) bool {
	for err != nil {
		if h, ok := err.(*httpclient.Error); ok {
			*target = h
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
