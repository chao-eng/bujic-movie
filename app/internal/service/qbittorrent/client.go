package qbittorrent

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

// Task is one torrent as reported by the client. The download module never
// stores task state; this is read live and cached only for degraded display.
type Task struct {
	Hash       string  `json:"hash"`
	Name       string  `json:"name"`
	SizeBytes  int64   `json:"size_bytes"`
	Progress   float64 `json:"progress"`
	SpeedBPS   int64   `json:"speed_bps"`
	State      string  `json:"state"`
	ETASeconds int64   `json:"eta_seconds"`
	Category   string  `json:"category"`
	// ConnectedSeeds is how many seeders this client is actually connected to,
	// AvailableSeeds is how many the swarm has. A task that stays at 0 connected
	// explains a stuck download better than the state string does.
	ConnectedSeeds int `json:"connected_seeds"`
	AvailableSeeds int `json:"available_seeds"`
}

// Config is everything needed to talk to one qBittorrent instance.
type Config struct {
	Host     string
	Port     int
	UseSSL   bool
	URLBase  string
	APIKey   string
	Username string
	Password string
	Proxy    string
}

// Client is the seam GrabService depends on, so tests can stub the client.
type Client interface {
	// Test logs in and reports the version plus category readiness.
	Test(ctx context.Context, categories []string) (*Status, error)
	// Push adds a torrent by URL (magnet or http) with a category.
	Push(ctx context.Context, kind, payload, category, name string, paused bool) error
	// Tasks lists current torrents.
	Tasks(ctx context.Context) ([]Task, error)
	// Action pauses, resumes or deletes one torrent.
	Action(ctx context.Context, hash, action string, deleteFiles bool) error
	// Categories lists the category names that already exist in the client.
	Categories(ctx context.Context) ([]CategoryInfo, error)
}

// CategoryInfo is one category as reported by the download client.
type CategoryInfo struct {
	Name     string `json:"name"`
	SavePath string `json:"save_path"`
}

// Status is the outcome of a connectivity test.
type Status struct {
	Version          string
	CategoriesReady  []string
	CategoriesFailed []string
}

// Error kinds mirror the module-wide classification.
const (
	KindAuth        = "auth"
	KindUnreachable = "unreachable"
	KindUnsupported = "unsupported"
	KindRejected    = "rejected"
)

// Error is a classified client failure.
type Error struct {
	Kind    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// NewError builds a classified client error.
func NewError(kind, message string, err error) *Error {
	return &Error{Kind: kind, Message: message, Err: err}
}

// HTTPClient talks to the qBittorrent Web API (4.6 and 5.x).
type HTTPClient struct {
	cfg     Config
	exec    *httpclient.Executor
	baseURL string

	cookie  string
	version string
}

// NewHTTPClient builds a client. The executor is shared so timeouts, retries
// and proxies behave the same as for indexers.
func NewHTTPClient(cfg Config, exec *httpclient.Executor) *HTTPClient {
	scheme := "http"
	if cfg.UseSSL {
		scheme = "https"
	}
	base := fmt.Sprintf("%s://%s:%d", scheme, cfg.Host, cfg.Port)
	if strings.TrimSpace(cfg.URLBase) != "" {
		base += "/" + strings.Trim(cfg.URLBase, "/")
	}
	return &HTTPClient{cfg: cfg, exec: exec, baseURL: base}
}

// Test logs in (unless an API key is used) and ensures every category exists.
func (c *HTTPClient) Test(ctx context.Context, categories []string) (*Status, error) {
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
		if err := c.createCategory(ctx, category); err != nil {
			status.CategoriesFailed = append(status.CategoriesFailed, category)
			continue
		}
		status.CategoriesReady = append(status.CategoriesReady, category)
	}
	return status, nil
}

func (c *HTTPClient) login(ctx context.Context) error {
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
		return translate(err)
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

func (c *HTTPClient) headers() map[string]string {
	h := map[string]string{"Referer": c.baseURL}
	if c.cookie != "" {
		h["Cookie"] = c.cookie
	}
	if strings.TrimSpace(c.cfg.APIKey) != "" {
		h["Authorization"] = "Bearer " + c.cfg.APIKey
	}
	return h
}

func (c *HTTPClient) categories(ctx context.Context) (map[string]bool, error) {
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

func (c *HTTPClient) createCategory(ctx context.Context, name string) error {
	form := url.Values{}
	form.Set("category", name)
	form.Set("savePath", "")
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
		return translate(err)
	}
	if resp.Status >= 400 {
		return NewError(KindUnsupported, "分类创建失败", nil)
	}
	return nil
}

// Push adds a torrent. payload is a magnet URI or an http(s) torrent URL.
func (c *HTTPClient) Push(ctx context.Context, kind, payload, category, name string, paused bool) error {
	if err := c.ensureLogin(ctx); err != nil {
		return err
	}
	form := url.Values{}
	form.Set("paused", boolString(paused))
	if category != "" {
		form.Set("category", category)
	}

	endpoint := "/api/v2/torrents/add"
	if kind == "torrent" && !strings.HasPrefix(strings.ToLower(payload), "magnet:") {
		form.Set("urls", payload)
	} else {
		form.Set("urls", payload)
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
		return translate(err)
	}
	body := strings.TrimSpace(string(resp.Body))
	if resp.Status == http.StatusConflict || strings.Contains(strings.ToLower(body), "duplicate") {
		return NewError(KindRejected, "下载器中已存在该种子", nil)
	}
	if resp.Status >= 400 {
		return NewError(KindRejected, fmt.Sprintf("下载器拒绝该任务（%d）", resp.Status), nil)
	}
	if strings.EqualFold(body, "Fails.") {
		return NewError(KindRejected, "下载器拒绝了该任务", nil)
	}
	return nil
}

// Categories lists existing categories with their save paths. The path is shown
// read-only: it belongs to qBittorrent, not to this module (D-04).
func (c *HTTPClient) Categories(ctx context.Context) ([]CategoryInfo, error) {
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
func (c *HTTPClient) Tasks(ctx context.Context) ([]Task, error) {
	if err := c.ensureLogin(ctx); err != nil {
		return nil, err
	}
	body, err := c.getWithHeader(ctx, "/api/v2/torrents/info")
	if err != nil {
		return nil, err
	}
	return parseTasks(body), nil
}

// Action applies pause/resume/delete to one torrent.
func (c *HTTPClient) Action(ctx context.Context, hash, action string, deleteFiles bool) error {
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
		return translate(err)
	}
	if resp.Status >= 400 {
		return NewError(KindRejected, "下载器未接受该操作", nil)
	}
	return nil
}

func (c *HTTPClient) ensureLogin(ctx context.Context) error {
	if c.cookie != "" || strings.TrimSpace(c.cfg.APIKey) != "" {
		return nil
	}
	return c.login(ctx)
}

func (c *HTTPClient) get(ctx context.Context, path string) ([]byte, error) {
	return c.getWithHeader(ctx, path)
}

func (c *HTTPClient) getWithHeader(ctx context.Context, path string) ([]byte, error) {
	headers := c.headers()
	resp, err := c.exec.Do(ctx, httpclient.Request{
		URL:         c.baseURL + path,
		Header:      headers,
		Timeout:     5 * time.Second,
		Proxy:       c.cfg.Proxy,
		BypassProxy: c.cfg.Proxy == "",
	})
	if err != nil {
		return nil, translate(err)
	}
	return resp.Body, nil
}

func translate(err error) error {
	var hErr *httpclient.Error
	if asHTTP(err, &hErr) {
		switch hErr.Kind {
		case httpclient.KindAuth:
			return NewError(KindAuth, "下载器认证失败，请检查账号或密码", nil)
		case httpclient.KindTimeout:
			return NewError(KindUnreachable, "下载器响应超时，请检查地址和端口", nil)
		case httpclient.KindBlocked, httpclient.KindChallenge:
			// qBittorrent answers 403 when it thinks the request did not come from
			// its own Web UI — usually a wrong address, or a reverse proxy that
			// needs the Host header. The httpclient wording ("站点") is about
			// indexers, so give a downloader-specific one.
			return NewError(KindAuth, "下载器拒绝了访问：请确认 Web UI 已开启、地址端口正确，且未被反向代理拦截", nil)
		default:
			return NewError(KindUnreachable, "无法连接下载器，请确认地址端口与本机网络可达", nil)
		}
	}
	return NewError(KindUnreachable, "无法连接下载器，请确认地址端口与本机网络可达", nil)
}

func asHTTP(err error, target **httpclient.Error) bool {
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

func mergeHeaders(base map[string]string, key, value string) map[string]string {
	out := make(map[string]string, len(base)+1)
	for k, v := range base {
		out[k] = v
	}
	out[key] = value
	return out
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
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
