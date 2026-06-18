package mediaserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// plexServer implements Server for Plex. Plex refreshes are scoped to a library
// "section"; Refresh with an empty id refreshes every section.
type plexServer struct {
	base *baseClient
}

type plexSectionsResponse struct {
	MediaContainer struct {
		Directory []struct {
			Key   string `json:"key"`
			Title string `json:"title"`
		} `json:"Directory"`
	} `json:"MediaContainer"`
}

// tokenURL builds baseURL+path with X-Plex-Token (and any extra query) appended.
func (s *plexServer) tokenURL(path string, extra url.Values) string {
	u, err := url.Parse(s.base.baseURL + path)
	if err != nil {
		return s.base.baseURL + path
	}
	q := u.Query()
	for k, vals := range extra {
		for _, v := range vals {
			q.Set(k, v)
		}
	}
	q.Set("X-Plex-Token", s.base.apiKey)
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *plexServer) TestConnection(ctx context.Context) error {
	endpoint := s.tokenURL("/identity", nil)
	_, status, err := s.base.do(ctx, "GET", endpoint, map[string]string{"Accept": "application/json"}, nil)
	if err != nil {
		return err
	}
	switch {
	case status == 401:
		return fmt.Errorf("认证失败: Token 无效")
	case status < 200 || status >= 300:
		return fmt.Errorf("连接失败: HTTP %d", status)
	}
	return nil
}

func (s *plexServer) ListLibraries(ctx context.Context) ([]Library, error) {
	resp, err := s.fetchSections(ctx)
	if err != nil {
		return nil, err
	}
	libs := make([]Library, 0, len(resp.MediaContainer.Directory))
	for _, dir := range resp.MediaContainer.Directory {
		libs = append(libs, Library{ID: dir.Key, Name: dir.Title})
	}
	return libs, nil
}

func (s *plexServer) Refresh(ctx context.Context, libraryID string) error {
	// Refresh a single section.
	if libraryID != "" {
		_, st, err := s.base.do(ctx, "GET", s.tokenURL("/library/sections/"+libraryID+"/refresh", nil), nil, nil)
		if err != nil {
			return err
		}
		if st < 200 || st >= 300 {
			return fmt.Errorf("刷新分区 %s 失败: HTTP %d", libraryID, st)
		}
		return nil
	}

	// Refresh all sections.
	resp, err := s.fetchSections(ctx)
	if err != nil {
		return err
	}
	if len(resp.MediaContainer.Directory) == 0 {
		return fmt.Errorf("未发现任何媒体库分区")
	}
	var lastErr error
	for _, dir := range resp.MediaContainer.Directory {
		_, st, err := s.base.do(ctx, "GET", s.tokenURL("/library/sections/"+dir.Key+"/refresh", nil), nil, nil)
		if err != nil {
			lastErr = err
			continue
		}
		if st < 200 || st >= 300 {
			lastErr = fmt.Errorf("刷新分区 %s 失败: HTTP %d", dir.Key, st)
		}
	}
	return lastErr
}

func (s *plexServer) fetchSections(ctx context.Context) (*plexSectionsResponse, error) {
	body, status, err := s.base.do(ctx, "GET", s.tokenURL("/library/sections", nil), map[string]string{"Accept": "application/json"}, nil)
	if err != nil {
		return nil, err
	}
	if status == 401 {
		return nil, fmt.Errorf("认证失败: Token 无效")
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("获取媒体库分区失败: HTTP %d", status)
	}
	var resp plexSectionsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析媒体库分区失败: %w", err)
	}
	return &resp, nil
}
