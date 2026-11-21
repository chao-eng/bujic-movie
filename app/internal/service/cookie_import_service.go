package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/service/connector"
	"github.com/goccy/go-yaml"
)

// CookieImportInput is what the browser extension posts: the cookies for the
// page the user just cleared, plus the exact User-Agent that solved it.
type CookieImportInput struct {
	Host      string `json:"host"`
	URL       string `json:"url"`
	Cookies   string `json:"cookies"`
	UserAgent string `json:"user_agent"`
	IndexerID uint   `json:"indexer_id"`
}

// CookieImportResult tells the extension which indexer was updated, or which
// candidates the user must choose from when the host matches more than one.
type CookieImportResult struct {
	Updated    bool            `json:"updated"`
	IndexerID  uint            `json:"indexer_id,omitempty"`
	Indexer    string          `json:"indexer_name,omitempty"`
	Candidates []IndexerOption `json:"candidates,omitempty"`
	Message    string          `json:"message,omitempty"`
}

// IndexerOption is a minimal selector for the extension.
type IndexerOption struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
}

// ImportCookies stores a manual browser session. The host is matched against
// each indexer's declared links and base URL so the user does not need to know
// internal IDs (the extension can post whatever host the tab is on).
func (s *IndexerService) ImportCookies(ctx context.Context, input CookieImportInput) (*CookieImportResult, error) {
	cookies := connector.NormaliseCookies(input.Cookies)
	if cookies == "" {
		return nil, &InputError{Field: "cookies", Message: "没有可导入的 Cookie"}
	}
	if strings.TrimSpace(input.UserAgent) == "" {
		return nil, &InputError{Field: "user_agent", Message: "缺少 User-Agent（Cloudflare 校验会失败）"}
	}

	host := normaliseHost(input.Host, input.URL)
	if input.IndexerID == 0 && host == "" {
		return nil, &InputError{Field: "host", Message: "缺少站点地址"}
	}

	indexers, err := s.repo.ListIndexers()
	if err != nil {
		return nil, err
	}

	matches := make([]IndexerOption, 0, 2)
	for _, idx := range indexers {
		if input.IndexerID > 0 && idx.ID != input.IndexerID {
			continue
		}
		indexerHosts := s.indexerHosts(idx)
		if input.IndexerID > 0 || len(indexerHosts) == 0 {
			matches = append(matches, IndexerOption{ID: idx.ID, Name: idx.Name, Host: firstOr(indexerHosts)})
			continue
		}
		for _, h := range indexerHosts {
			if h == host {
				matches = append(matches, IndexerOption{ID: idx.ID, Name: idx.Name, Host: h})
				break
			}
		}
	}

	if len(matches) == 0 {
		return &CookieImportResult{Message: fmt.Sprintf("没有站点匹配 %s", host)}, nil
	}
	if len(matches) > 1 {
		return &CookieImportResult{Candidates: matches, Message: "多个站点匹配该域名，请选择"}, nil
	}

	target, err := s.repo.GetIndexer(matches[0].ID)
	if err != nil {
		return nil, err
	}
	enc, err := s.box.Encrypt(cookies)
	if err != nil {
		return nil, err
	}
	target.ExtraCookiesEnc = enc
	target.UserAgent = strings.TrimSpace(input.UserAgent)
	if err := s.repo.UpdateIndexer(target); err != nil {
		return nil, &StorageError{Err: err}
	}
	// The cached connector carries the old session.
	s.sites.forget(target.ID)

	return &CookieImportResult{
		Updated: true, IndexerID: target.ID, Indexer: target.Name,
		Message: fmt.Sprintf("已导入 %d 个 Cookie 到「%s」", len(connector.CookieNames(cookies)), target.Name),
	}, nil
}

// indexerHosts resolves the hosts an indexer talks to.
func (s *IndexerService) indexerHosts(idx entity.SearchIndexer) []string {
	hosts := []string{}
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return
		}
		h := strings.ToLower(u.Hostname())
		for _, existing := range hosts {
			if existing == h {
				return
			}
		}
		hosts = append(hosts, h)
	}

	add(idx.BaseURL)
	// Cardigann links live in the definition snapshot.
	if idx.DefinitionYAML != "" {
		var parsed struct {
			Links []string `yaml:"links"`
		}
		if err := yaml.Unmarshal([]byte(idx.DefinitionYAML), &parsed); err == nil {
			for _, link := range parsed.Links {
				add(link)
			}
		}
	}
	return hosts
}

func normaliseHost(host, rawURL string) string {
	host = strings.TrimSpace(host)
	if host != "" {
		if u, err := url.Parse(host); err == nil && u.Hostname() != "" {
			return strings.ToLower(u.Hostname())
		}
		return strings.ToLower(host)
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func firstOr(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return items[0]
}
