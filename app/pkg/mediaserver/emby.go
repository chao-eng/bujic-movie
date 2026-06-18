package mediaserver

import (
	"context"
	"encoding/json"
	"fmt"
)

// embyServer implements Server for both Emby and Jellyfin, which share the same
// HTTP API. The only difference is that Emby serves it under the "/emby" path
// prefix while Jellyfin serves it at the root (prefix == "").
type embyServer struct {
	base   *baseClient
	prefix string
}

// Refresh triggers a scan. An empty libraryID scans all libraries; otherwise it
// refreshes a single library item (POST /Items/{id}/Refresh).
func (s *embyServer) Refresh(ctx context.Context, libraryID string) error {
	var endpoint string
	if libraryID == "" {
		endpoint = s.base.baseURL + s.prefix + "/Library/Refresh"
	} else {
		endpoint = s.base.baseURL + s.prefix + "/Items/" + libraryID + "/Refresh?Recursive=true"
	}
	headers := map[string]string{"X-Emby-Token": s.base.apiKey}
	_, status, err := s.base.do(ctx, "POST", endpoint, headers, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("刷新失败: HTTP %d", status)
	}
	return nil
}

// ListLibraries returns the server's libraries via the VirtualFolders endpoint,
// which is supported by both Emby and Jellyfin (SelectableMediaFolders is
// Emby-only and 404s on Jellyfin). The library id is the VirtualFolder ItemId.
func (s *embyServer) ListLibraries(ctx context.Context) ([]Library, error) {
	endpoint := s.base.baseURL + s.prefix + "/Library/VirtualFolders"
	headers := map[string]string{"X-Emby-Token": s.base.apiKey}
	body, status, err := s.base.do(ctx, "GET", endpoint, headers, nil)
	if err != nil {
		return nil, err
	}
	if status == 401 || status == 403 {
		return nil, fmt.Errorf("认证失败: API Key 无效或权限不足")
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("获取媒体库失败: HTTP %d", status)
	}

	var raw []struct {
		Name   string `json:"Name"`
		ItemID string `json:"ItemId"`
		ID     string `json:"Id"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析媒体库失败: %w", err)
	}
	libs := make([]Library, 0, len(raw))
	for _, r := range raw {
		id := r.ItemID
		if id == "" {
			id = r.ID
		}
		if id == "" {
			continue // 无法单独刷新没有 ItemId 的库
		}
		libs = append(libs, Library{ID: id, Name: r.Name})
	}
	return libs, nil
}

// TestConnection hits /System/Info which requires a valid token.
func (s *embyServer) TestConnection(ctx context.Context) error {
	endpoint := s.base.baseURL + s.prefix + "/System/Info"
	headers := map[string]string{"X-Emby-Token": s.base.apiKey}
	_, status, err := s.base.do(ctx, "GET", endpoint, headers, nil)
	if err != nil {
		return err
	}
	switch {
	case status == 401:
		return fmt.Errorf("认证失败: API Key 无效")
	case status < 200 || status >= 300:
		return fmt.Errorf("连接失败: HTTP %d", status)
	}
	return nil
}
