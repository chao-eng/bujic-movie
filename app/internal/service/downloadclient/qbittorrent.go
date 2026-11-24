package downloadclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
)

// QBittorrentClient talks to the qBittorrent Web API (4.6 and 5.x).
type QBittorrentClient struct {
	cfg     Config
	exec    *httpclient.Executor
	baseURL string

	cookie  string
	version string
}

// NewQBittorrentClient builds a qBittorrent client. The executor is shared so
// timeouts, retries and proxies behave the same as for indexers.
func NewQBittorrentClient(cfg Config, exec *httpclient.Executor) *QBittorrentClient {
	return &QBittorrentClient{cfg: cfg, exec: exec, baseURL: baseURL(cfg)}
}

// Test logs in (unless an API key is used) and ensures every category exists.
func (c *QBittorrentClient) Test(ctx context.Context, categories []string) (*Status, error) {
	if err := c.login(ctx); err != nil {
		return nil, err
	}

	versionResp, err := c.get(ctx, "/api/v2/app/version")
	if err != nil {
		return nil, err
	}
	c.version = strings.TrimSpace(string(versionResp))

	status := &Status{Version: c.version}
	existing, err := c.categories(ctx)
	if err != nil {
		return nil, err
	}

	for _, category := range categories {
		category = strings.TrimSpace(category)
		if category == "" {
			continue
		}
		if existing[category] {
			status.CategoriesReady = append(status.CategoriesReady, category)
			continue
		}
		if err := c.createCategory(ctx, category, ""); err != nil {
			status.CategoriesFailed = append(status.CategoriesFailed, category)
			continue
		}
		status.CategoriesReady = append(status.CategoriesReady, category)
	}
	return status, nil
}

func (c *QBittorrentClient) login(ctx context.Context) error {
	if strings.TrimSpace(c.cfg.APIKey) != "" {
		// qBittorrent 5.x supports API key auth via the Authorization header.
		c.cookie = ""
		return nil
	}

	form := url.Values{}
	form.Set("username", c.cfg.Username)
	form.Set("password", c.cfg.Password)

	resp, err := c.exec.Do(ctx, httpclient.Request{
		Method:      http.MethodPost,
		URL:         c.baseURL + "/api/v2/auth/login",
		Header:      map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Referer": c.baseURL},
		Body:        strings.NewReader(form.Encode()),
		NoRetry:     true,
		Proxy:       c.cfg.Proxy,
		BypassProxy: c.cfg.Proxy == "",
	})
	if err != nil {
		return translateHTTP(err)
	}
	body := strings.TrimSpace(string(resp.Body))
	if body != "Ok." {
		return NewError(KindAuth, "账号或密码错误", nil)
	}
	for _, raw := range resp.Header.Values("Set-Cookie") {
		// A cookie may arrive without attributes ("SID=abc"), so cut at the
		// first semicolon only when one is present.
		pair := raw
		if idx := strings.Index(raw, ";"); idx > 0 {
			pair = raw[:idx]
		}
		if strings.Contains(pair, "=") {
			c.cookie = pair
			break
		}
	}
	if c.cookie == "" {
		return NewError(KindAuth, "下载器没有返回会话凭据", nil)
	}
	return nil
}

func (c *QBittorrentClient) headers() map[string]string {
	h := map[string]string{"Referer": c.baseURL}
	if c.cookie != "" {
		h["Cookie"] = c.cookie
	}
	if strings.TrimSpace(c.cfg.APIKey) != "" {
		h["Authorization"] = "Bearer " + c.cfg.APIKey
	}
	return h
}

func (c *QBittorrentClient) categories(ctx context.Context) (map[string]bool, error) {
	body, err := c.get(ctx, "/api/v2/torrents/categories")
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	// Response shape: {"name":{"name":"name","savePath":"..."}, ...}
	for _, key := range jsonKeys(body) {
		out[key] = true
	}
	return out, nil
}

// createCategory creates a category, optionally pinned to a save path. A 409
// means it already exists, which is fine: Push passes savepath per torrent.
func (c *QBittorrentClient) createCategory(ctx context.Context, name, savePath string) error {
	form := url.Values{}
	form.Set("category", name)
	form.Set("savePath", savePath)
	resp, err := c.exec.Do(ctx, httpclient.Request{
		Method:      http.MethodPost,
		URL:         c.baseURL + "/api/v2/torrents/createCategory",
		Header:      mergeHeaders(c.headers(), "Content-Type", "application/x-www-form-urlencoded"),
		Body:        strings.NewReader(form.Encode()),
		NoRetry:     true,
		Proxy:       c.cfg.Proxy,
		BypassProxy: c.cfg.Proxy == "",
	})
	if err != nil {
		return translateHTTP(err)
	}
	if resp.Status >= 400 && resp.Status != http.StatusConflict {
		return NewError(KindUnsupported, "分类创建失败", nil)
	}
	return nil
}

// Push adds a torrent. Payload is a magnet URI or an http(s) torrent URL. A save
// path is honoured by qBittorrent itself; a category also carries its own path.
func (c *QBittorrentClient) Push(ctx context.Context, opts PushOptions) error {
	if err := c.ensureLogin(ctx); err != nil {
		return err
	}
	category := strings.TrimSpace(opts.Category)
	savePath := normalizeSavePath(opts.SavePath)
	if category != "" {
		// Make sure the category exists so the mapping survives in qB's UI.
		_ = c.createCategory(ctx, category, savePath)
	}

	form := url.Values{}
	form.Set("paused", boolString(opts.Paused))
	form.Set("urls", opts.Payload)
	if category != "" {
		form.Set("category", category)
	}
	if savePath != "" {
		form.Set("savepath", savePath)
	}
	if opts.Sequential {
		form.Set("sequentialDownload", "true")
	}
	if opts.FirstLast {
		form.Set("firstLastPiecePrio", "true")
	}
	switch opts.ContentLayout {
	case "no-subfolder":
		form.Set("contentLayout", "NoSubfolder")
	case "subfolder":
		form.Set("contentLayout", "Subfolder")
	case "original":
		form.Set("contentLayout", "Original")
	}

	resp, err := c.exec.Do(ctx, httpclient.Request{
		Method:      http.MethodPost,
		URL:         c.baseURL + "/api/v2/torrents/add",
		Header:      mergeHeaders(c.headers(), "Content-Type", "application/x-www-form-urlencoded"),
		Body:        strings.NewReader(form.Encode()),
		NoRetry:     true,
		Proxy:       c.cfg.Proxy,
		BypassProxy: c.cfg.Proxy == "",
	})
	if err != nil {
		return translateHTTP(err)
	}
	body := strings.TrimSpace(string(resp.Body))
	if resp.Status == http.StatusConflict || strings.Contains(strings.ToLower(body), "duplicate") {
		return NewError(KindDuplicate, "下载器中已存在该种子", nil)
	}
	if resp.Status >= 400 {
		return NewError(KindRejected, fmt.Sprintf("下载器拒绝该任务（%d）", resp.Status), nil)
	}
	if strings.EqualFold(body, "Fails.") {
		// qBittorrent answers "Fails." both for a duplicate and for a link it
		// cannot parse. Nothing else is actionable for the user here, and the
		// record keeps the real task state from the session, so it is classified
		// as "already present" rather than a hard failure.
		return NewError(KindDuplicate, "下载器已存在该种子，或无法解析该链接", nil)
	}
	return nil
}

// Categories lists existing categories with their save paths. The path is shown
// read-only: it belongs to qBittorrent, not to this module (D-04).
func (c *QBittorrentClient) Categories(ctx context.Context) ([]CategoryInfo, error) {
	if err := c.ensureLogin(ctx); err != nil {
		return nil, err
	}
	body, err := c.get(ctx, "/api/v2/torrents/categories")
	if err != nil {
		return nil, err
	}
	return parseCategories(body), nil
}

// parseCategories decodes {"name":{"name":"x","savePath":"/y"}, ...}.
func parseCategories(body []byte) []CategoryInfo {
	entries := splitJSONObject(body)
	out := make([]CategoryInfo, 0, len(entries))
	for _, entry := range entries {
		name := jsonString(entry.value, "name")
		if name == "" {
			name = entry.key
		}
		out = append(out, CategoryInfo{Name: name, SavePath: jsonString(entry.value, "savePath")})
	}
	return out
}

// jsonObjectEntry is one top-level member of a JSON object.
type jsonObjectEntry struct {
	key   string
	value string
}

// splitJSONObject returns each top-level member's key and raw value, so the
// category map can be decoded without a fixed schema.
func splitJSONObject(body []byte) []jsonObjectEntry {
	text := strings.TrimSpace(string(body))
	if !strings.HasPrefix(text, "{") {
		return nil
	}
	out := []jsonObjectEntry{}
	depth := 0
	inString := false
	escaped := false
	keyStart := -1
	keyEnd := -1
	valueStart := -1
	var key strings.Builder

	for i := 0; i < len(text); i++ {
		ch := text[i]
		switch {
		case escaped:
			escaped = false
		case ch == '\\' && inString:
			escaped = true
		case ch == '"':
			if !inString {
				inString = true
				if depth == 1 && keyStart < 0 {
					keyStart = i + 1
					key.Reset()
				}
			} else {
				inString = false
				if depth == 1 && keyStart >= 0 && keyEnd < 0 {
					keyEnd = i
				}
			}
		case inString:
			if depth == 1 && keyStart >= 0 && keyEnd < 0 {
				key.WriteByte(ch)
			}
		case ch == '{' || ch == '[':
			if depth == 1 && keyEnd >= 0 && valueStart < 0 {
				valueStart = i
			}
			depth++
		case ch == '}' || ch == ']':
			depth--
			if depth == 1 && valueStart >= 0 {
				out = append(out, jsonObjectEntry{key: key.String(), value: text[valueStart : i+1]})
				keyStart, keyEnd, valueStart = -1, -1, -1
			}
		case ch == ':' && depth == 1 && keyEnd >= 0 && valueStart < 0:
			valueStart = i + 1
		case ch == ',' && depth == 1 && valueStart >= 0:
			out = append(out, jsonObjectEntry{key: key.String(), value: text[valueStart:i]})
			keyStart, keyEnd, valueStart = -1, -1, -1
		}
	}
	return out
}

// Tasks returns current torrents.
func (c *QBittorrentClient) Tasks(ctx context.Context) ([]Task, error) {
	if err := c.ensureLogin(ctx); err != nil {
		return nil, err
	}
	body, err := c.get(ctx, "/api/v2/torrents/info")
	if err != nil {
		return nil, err
	}
	return parseTasks(body), nil
}

// Action applies pause/resume/delete to one torrent.
func (c *QBittorrentClient) Action(ctx context.Context, hash, action string, deleteFiles bool) error {
	if err := c.ensureLogin(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(hash) == "" {
		return NewError(KindUnsupported, "缺少任务标识", nil)
	}

	endpoint := ""
	switch action {
	case "pause":
		endpoint = "/api/v2/torrents/pause"
	case "resume":
		endpoint = "/api/v2/torrents/resume"
	case "delete":
		endpoint = "/api/v2/torrents/delete"
	default:
		return NewError(KindUnsupported, "不支持的操作", nil)
	}

	form := url.Values{}
	form.Set("hashes", hash)
	if action == "delete" {
		form.Set("deleteFiles", boolString(deleteFiles))
	}

	resp, err := c.exec.Do(ctx, httpclient.Request{
		Method:      http.MethodPost,
		URL:         c.baseURL + endpoint,
		Header:      mergeHeaders(c.headers(), "Content-Type", "application/x-www-form-urlencoded"),
		Body:        strings.NewReader(form.Encode()),
		NoRetry:     true,
		Proxy:       c.cfg.Proxy,
		BypassProxy: c.cfg.Proxy == "",
	})
	if err != nil {
		return translateHTTP(err)
	}
	if resp.Status >= 400 {
		return NewError(KindRejected, "下载器未接受该操作", nil)
	}
	return nil
}

func (c *QBittorrentClient) ensureLogin(ctx context.Context) error {
	if c.cookie != "" || strings.TrimSpace(c.cfg.APIKey) != "" {
		return nil
	}
	return c.login(ctx)
}

func (c *QBittorrentClient) get(ctx context.Context, path string) ([]byte, error) {
	resp, err := c.exec.Do(ctx, httpclient.Request{
		URL:         c.baseURL + path,
		Header:      c.headers(),
		Timeout:     5 * time.Second,
		Proxy:       c.cfg.Proxy,
		BypassProxy: c.cfg.Proxy == "",
	})
	if err != nil {
		return nil, translateHTTP(err)
	}
	return resp.Body, nil
}

// jsonKeys extracts top-level keys from a JSON object without imposing a schema.
func jsonKeys(body []byte) []string {
	out := []string{}
	inString := false
	escaped := false
	expectKey := false
	depth := 0
	var current strings.Builder
	for _, r := range string(body) {
		switch {
		case escaped:
			escaped = false
			if inString {
				current.WriteRune(r)
			}
		case r == '\\' && inString:
			escaped = true
		case r == '"':
			if inString {
				inString = false
				if expectKey && depth == 1 {
					out = append(out, current.String())
					expectKey = false
				}
			} else {
				inString = true
				current.Reset()
			}
		case r == '{':
			depth++
			if depth == 1 {
				expectKey = true
			}
		case r == '}':
			depth--
		case r == ',':
			if depth == 1 && !inString {
				expectKey = true
			}
		default:
			if inString {
				current.WriteRune(r)
			}
		}
	}
	return out
}

// parseTasks decodes the torrents/info array using gjson-like traversal.
func parseTasks(body []byte) []Task {
	tasks := []Task{}
	entries := splitJSONArray(body)
	for _, entry := range entries {
		task := Task{
			Hash:       jsonString(entry, "hash"),
			Name:       jsonString(entry, "name"),
			State:      jsonString(entry, "state"),
			Category:   jsonString(entry, "category"),
			SizeBytes:  jsonInt(entry, "size"),
			SpeedBPS:   jsonInt(entry, "dlspeed"),
			ETASeconds: jsonInt(entry, "eta"),
			Progress:   jsonFloat(entry, "progress"),
			// qBittorrent 4.6+ reports seeds both connected and in the swarm.
			ConnectedSeeds: int(jsonInt(entry, "num_seeds")),
			AvailableSeeds: int(jsonInt(entry, "num_complete")),
		}
		if task.Hash != "" {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func splitJSONArray(body []byte) []string {
	text := strings.TrimSpace(string(body))
	if !strings.HasPrefix(text, "[") {
		return nil
	}
	var out []string
	depth := 0
	inString := false
	escaped := false
	start := -1
	for i, r := range text {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inString:
			escaped = true
		case r == '"':
			inString = !inString
		case inString:
		case r == '{':
			if depth == 0 {
				start = i
			}
			depth++
		case r == '}':
			depth--
			if depth == 0 && start >= 0 {
				out = append(out, text[start:i+1])
				start = -1
			}
		}
	}
	return out
}

func jsonValue(entry, key string) string {
	needle := `"` + key + `"`
	idx := strings.Index(entry, needle)
	if idx < 0 {
		return ""
	}
	rest := entry[idx+len(needle):]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return ""
	}
	rest = strings.TrimSpace(rest[colon+1:])
	if rest == "" {
		return ""
	}
	if rest[0] == '"' {
		end := 1
		for end < len(rest) {
			if rest[end] == '\\' {
				end += 2
				continue
			}
			if rest[end] == '"' {
				break
			}
			end++
		}
		if end < len(rest) {
			return rest[1:end]
		}
		return ""
	}
	end := 0
	for end < len(rest) && rest[end] != ',' && rest[end] != '}' {
		end++
	}
	return strings.TrimSpace(rest[:end])
}

func jsonString(entry, key string) string { return jsonValue(entry, key) }

func jsonInt(entry, key string) int64 {
	raw := jsonValue(entry, key)
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		f, ferr := strconv.ParseFloat(raw, 64)
		if ferr != nil {
			return 0
		}
		return int64(f)
	}
	return v
}

func jsonFloat(entry, key string) float64 {
	raw := jsonValue(entry, key)
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return v
}
