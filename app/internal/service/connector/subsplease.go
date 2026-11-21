package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
)

// SubsPleaseConnector talks to the SubsPlease JSON API. Prowlarr implements it
// natively because the payload is a nested object keyed by show name, which the
// Cardigann YAML subset cannot express.
type SubsPleaseConnector struct {
	name    string
	baseURL string
	proxy   string
	exec    *httpclient.Executor
	allowed []string
	limitMS int
	session Session
}

// SetSession installs the manually supplied browser session.
func (s *SubsPleaseConnector) SetSession(session Session) { s.session = session }

func (s *SubsPleaseConnector) headers() map[string]string {
	h := map[string]string{"Accept": "application/json", "User-Agent": userAgent()}
	s.session.Apply(h)
	return h
}

// SubsPleaseBaseURL is the upstream site.
const SubsPleaseBaseURL = "https://subsplease.org"

// NewSubsPlease builds the connector.
func NewSubsPlease(name, baseURL, proxyURL string, exec *httpclient.Executor, limitMS int) *SubsPleaseConnector {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = SubsPleaseBaseURL
	}
	if limitMS <= 0 {
		limitMS = 2000
	}
	return &SubsPleaseConnector{
		name: name, baseURL: strings.TrimRight(baseURL, "/"), proxy: proxyURL, exec: exec,
		allowed: httpclient.HostsFromLinks([]string{baseURL, SubsPleaseBaseURL}),
		limitMS: limitMS,
	}
}

func (s *SubsPleaseConnector) Name() string {
	if s.name != "" {
		return s.name
	}
	return "SubsPlease"
}

func (s *SubsPleaseConnector) rateKey() string { return "subsplease:" + s.baseURL }

// subPleaseRelease is one show entry; the API keys the object by show name.
type subPleaseRelease struct {
	Time        string              `json:"time"`
	ReleaseDate string              `json:"release_date"`
	Show        string              `json:"show"`
	Episode     string              `json:"episode"`
	Downloads   []subPleaseDownload `json:"downloads"`
	Page        string              `json:"page"`
}

type subPleaseDownload struct {
	Resolution string `json:"res"`
	Magnet     string `json:"magnet"`
}

var subPleaseResolutionRe = regexp.MustCompile(`(?i)\d{3,4}p`)

// Test queries the latest feed to prove the API answers.
func (s *SubsPleaseConnector) Test(ctx context.Context) (*Capabilities, error) {
	start := time.Now()
	if _, err := s.fetch(ctx, "", true); err != nil {
		return nil, err
	}
	return &Capabilities{
		Name:        s.Name(),
		Protocol:    "torrent",
		Categories:  2,
		SearchModes: []string{"search", "tv-search", "movie-search"},
		Elapsed:     time.Since(start),
		LoginState:  "无需登录",
	}, nil
}

// Search maps the nested payload onto normalised releases.
func (s *SubsPleaseConnector) Search(ctx context.Context, q Query) ([]Release, error) {
	term := strings.TrimSpace(q.Keyword)
	term = subPleaseKeywordRe.ReplaceAllString(term, "")
	// Only season > 1 is added, matching the upstream indexer's behaviour.
	if q.Season > 1 {
		term = strings.TrimSpace(term + " S" + fmt.Sprint(q.Season))
	}
	if q.Episode > 0 {
		term = strings.TrimSpace(term + " " + fmt.Sprintf("%02d", q.Episode))
	}
	term = subPleaseResolutionRe.ReplaceAllString(term, "")
	term = strings.TrimSpace(term)

	entries, err := s.fetch(ctx, term, term == "")
	if err != nil {
		return nil, err
	}

	releases := make([]Release, 0, len(entries)*2)
	for _, entry := range entries {
		for _, download := range entry.Downloads {
			magnet := strings.TrimSpace(download.Magnet)
			if magnet == "" {
				continue
			}
			title := fmt.Sprintf("[SubsPlease] %s - %s (%sp)", entry.Show, entry.Episode, download.Resolution)
			categories := []int{5070}
			if strings.EqualFold(entry.Episode, "movie") {
				categories = append(categories, 2010)
			}
			rel := Release{
				Title:        title,
				DownloadURL:  magnet,
				DownloadType: "magnet",
				InfoHash:     infoHashFromMagnet(magnet),
				SizeBytes:    subPleaseSize(magnet, download.Resolution),
				PublishDate:  normaliseDate(entry.ReleaseDate),
				Categories:   categories,
				IndexerName:  s.Name(),
				// The API exposes no seeder counts; the upstream indexer uses 1.
				Seeders: 1,
				Peers:   2,
			}
			releases = append(releases, rel)
		}
	}
	return releases, nil
}

var subPleaseKeywordRe = regexp.MustCompile(`(?i)\[?SubsPlease\]?\s*`)

func (s *SubsPleaseConnector) fetch(ctx context.Context, term string, latest bool) ([]subPleaseRelease, error) {
	target := s.baseURL + "/api/?tz=UTC"
	if latest || term == "" {
		target += "&f=latest"
	} else {
		target += "&f=search&s=" + urlQueryEscape(term)
	}

	resp, err := s.exec.Do(ctx, httpclient.Request{
		URL:       target,
		Header:    s.headers(),
		Allowed:   s.allowed,
		RateKey:   s.rateKey(),
		RateLimit: time.Duration(s.limitMS) * time.Millisecond,
		Proxy:     s.proxy,
	})
	if err != nil {
		return nil, translateHTTPError(err)
	}

	body := strings.TrimSpace(string(resp.Body))
	if body == "" || body == "[]" {
		return nil, nil
	}
	// The API returns an object keyed by show name; an array means "no results".
	if strings.HasPrefix(body, "[") {
		return nil, nil
	}

	var keyed map[string]subPleaseRelease
	if err := json.Unmarshal([]byte(body), &keyed); err != nil {
		return nil, NewError(KindParse, "返回内容不是有效的 JSON", "search", err)
	}
	out := make([]subPleaseRelease, 0, len(keyed))
	for show, entry := range keyed {
		if entry.Show == "" {
			entry.Show = show
		}
		out = append(out, entry)
	}
	return out, nil
}

// ResolveDownload returns the magnet unchanged.
func (s *SubsPleaseConnector) ResolveDownload(ctx context.Context, rel Release) (*DownloadRef, error) {
	if rel.DownloadURL == "" {
		return nil, NewError(KindParse, "结果缺少下载链接", "download", nil)
	}
	return &DownloadRef{URL: rel.DownloadURL, InfoHash: rel.InfoHash, Type: "magnet", Title: rel.Title}, nil
}

// SupportsCategories: SubsPlease serves anime and the occasional movie.
func (s *SubsPleaseConnector) SupportsCategories(categories []int) bool {
	if len(categories) == 0 {
		return true
	}
	for _, cat := range categories {
		switch {
		case cat >= 5000 && cat < 6000, cat == 5070, cat >= 2000 && cat < 3000:
			return true
		}
	}
	return false
}

var subPleaseSizeRe = regexp.MustCompile(`[?&]xl=(\d+)`)

// subPleaseSize reads the exact size from the magnet's xl parameter, falling
// back to a resolution estimate like the upstream indexer does.
func subPleaseSize(magnet, resolution string) int64 {
	if m := subPleaseSizeRe.FindStringSubmatch(magnet); m != nil {
		var size int64
		if _, err := fmt.Sscanf(m[1], "%d", &size); err == nil && size > 0 {
			return size
		}
	}
	switch resolution {
	case "1080":
		return 1395864371 // ~1.3GB
	case "720":
		return 734003200 // 700MB
	case "480":
		return 367001600 // 350MB
	default:
		return 1073741824 // 1GB
	}
}

var subPleaseHashRe = regexp.MustCompile(`(?i)btih:([a-z0-9]+)`)

func infoHashFromMagnet(magnet string) string {
	if m := subPleaseHashRe.FindStringSubmatch(magnet); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

func urlQueryEscape(value string) string {
	replacer := strings.NewReplacer(" ", "+", "&", "%26", "=", "%3D", "?", "%3F", "#", "%23")
	return replacer.Replace(value)
}
