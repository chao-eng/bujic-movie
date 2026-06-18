// Package mediaserver provides thin HTTP clients to notify media servers
// (Emby / Jellyfin / Plex) to incrementally refresh a path after bujic-movie
// transfers and scrapes new content. The HTTP style mirrors pkg/tmdb:
// a shared *http.Client with a timeout, one retry, and masked request logging.
package mediaserver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ServerType string

const (
	TypeEmby     ServerType = "emby"
	TypeJellyfin ServerType = "jellyfin"
	TypePlex     ServerType = "plex"
)

// Library is one library/section on a media server.
type Library struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Server is the unified abstraction over a single media server instance.
type Server interface {
	// TestConnection verifies the URL + credential are usable.
	TestConnection(ctx context.Context) error
	// ListLibraries returns the libraries/sections configured on the server.
	ListLibraries(ctx context.Context) ([]Library, error)
	// Refresh refreshes (rescans) a library. An empty libraryID refreshes all libraries.
	Refresh(ctx context.Context, libraryID string) error
}

// New builds a Server for the given type. baseURL is normalized (trailing slash removed).
func New(t ServerType, baseURL, apiKey string) (Server, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("媒体库地址为空")
	}
	base := &baseClient{
		baseURL: baseURL,
		apiKey:  strings.TrimSpace(apiKey),
		http:    &http.Client{Timeout: 15 * time.Second},
	}
	switch ServerType(strings.ToLower(string(t))) {
	case TypeEmby:
		return &embyServer{base: base, prefix: "/emby"}, nil
	case TypeJellyfin:
		return &embyServer{base: base, prefix: ""}, nil
	case TypePlex:
		return &plexServer{base: base}, nil
	default:
		return nil, fmt.Errorf("不支持的媒体库类型: %s", t)
	}
}

// baseClient holds the shared HTTP plumbing for all server implementations.
type baseClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// do performs an HTTP request with a single retry on transport errors and logs each attempt.
func (c *baseClient) do(ctx context.Context, method, urlStr string, headers map[string]string, body []byte) ([]byte, int, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(time.Second):
			}
		}

		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, urlStr, reader)
		if err != nil {
			return nil, 0, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		start := time.Now()
		resp, err := c.http.Do(req)
		dur := time.Since(start)
		if err != nil {
			logRequest(method, urlStr, 0, dur, err)
			lastErr = err
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		logRequest(method, urlStr, resp.StatusCode, dur, nil)
		return respBody, resp.StatusCode, nil
	}
	return nil, 0, lastErr
}

// logRequest appends a masked one-line record to data/logs/mediaserver_api.log.
func logRequest(method, urlStr string, statusCode int, duration time.Duration, reqErr error) {
	logFilePath := "data/logs/mediaserver_api.log"
	if err := os.MkdirAll(filepath.Dir(logFilePath), 0755); err != nil {
		return
	}
	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	ts := time.Now().Format("2006-01-02 15:04:05.000")
	masked := maskToken(urlStr)
	var line string
	if reqErr != nil {
		line = fmt.Sprintf("[%s] %s %s | DURATION: %v | ERROR: %v\n", ts, method, masked, duration, reqErr)
	} else {
		line = fmt.Sprintf("[%s] %s %s | STATUS: %d | DURATION: %v\n", ts, method, masked, statusCode, duration)
	}
	_, _ = f.WriteString(line)
}

// maskToken hides credentials carried in the URL query (Plex X-Plex-Token / api_key).
func maskToken(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return urlStr
	}
	q := u.Query()
	for _, key := range []string{"X-Plex-Token", "api_key"} {
		if q.Get(key) != "" {
			q.Set(key, "******")
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
