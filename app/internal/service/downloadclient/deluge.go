package downloadclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
)

// DelugeClient talks to the Deluge Web UI JSON-RPC endpoint. Its "category" is
// a label from the Label plugin, which does not influence the save folder, so a
// download directory is passed explicitly when one is configured.
type DelugeClient struct {
	cfg     Config
	exec    *httpclient.Executor
	rpcURL  string
	cookie  string
	request int
}

// NewDelugeClient builds a Deluge Web UI client.
func NewDelugeClient(cfg Config, exec *httpclient.Executor) *DelugeClient {
	path := strings.Trim(strings.TrimSpace(cfg.URLBase), "/")
	endpoint := baseURL(cfg)
	if path == "" {
		endpoint += "/json"
	} else {
		endpoint += "/" + path + "/json"
	}
	return &DelugeClient{cfg: cfg, exec: exec, rpcURL: endpoint}
}

type delugeResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

// login authenticates the Web UI session. Deluge answers with an empty result
// and sets the session cookie; a wrong password yields result=false.
func (c *DelugeClient) login(ctx context.Context) error {
	c.cookie = ""
	raw, err := c.callRaw(ctx, "auth.login", []any{c.cfg.Password})
	if err != nil {
		return err
	}
	var ok bool
	if err := json.Unmarshal(raw, &ok); err != nil || !ok {
		return NewError(KindAuth, "密码错误", nil)
	}
	if c.cookie == "" {
		return NewError(KindAuth, "下载器没有返回会话凭据", nil)
	}
	return nil
}

func (c *DelugeClient) headers() map[string]string {
	h := map[string]string{"Content-Type": "application/json"}
	if c.cookie != "" {
		h["Cookie"] = c.cookie
	}
	return h
}

// call invokes one RPC method and decodes its result into out.
func (c *DelugeClient) call(ctx context.Context, method string, params []any, out any) error {
	raw, err := c.callRaw(ctx, method, params)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return NewError(KindUnsupported, "下载器返回了无法识别的内容", err)
	}
	return nil
}

func (c *DelugeClient) callRaw(ctx context.Context, method string, params []any) (json.RawMessage, error) {
	if params == nil {
		params = []any{}
	}
	c.request++
	body := map[string]any{"method": method, "params": params, "id": c.request}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, NewError(KindUnsupported, "请求构建失败", err)
	}

	resp, derr := c.exec.Do(ctx, httpclient.Request{
		Method:      http.MethodPost,
		URL:         c.rpcURL,
		Header:      c.headers(),
		Body:        strings.NewReader(string(payload)),
		NoRetry:     true,
		Proxy:       c.cfg.Proxy,
		BypassProxy: c.cfg.Proxy == "",
	})
	if derr != nil {
		return nil, translateHTTP(derr)
	}
	// Deluge hands out the session cookie on the login response; keep it so the
	// next call is authenticated.
	for _, raw := range resp.Header.Values("Set-Cookie") {
		pair := raw
		if idx := strings.Index(raw, ";"); idx > 0 {
			pair = raw[:idx]
		}
		if strings.Contains(pair, "=") {
			c.cookie = pair
			break
		}
	}

	var parsed delugeResponse
	if err := json.Unmarshal(resp.Body, &parsed); err != nil {
		return nil, NewError(KindUnsupported, "下载器返回了无法识别的内容", err)
	}
	if parsed.Error != nil {
		// 1/2 mean "not authenticated"; everything else is a rejection.
		if parsed.Error.Code == 1 || parsed.Error.Code == 2 {
			return nil, NewError(KindAuth, "会话已失效，请重新测试连接", nil)
		}
		if strings.Contains(strings.ToLower(parsed.Error.Message), "already") {
			return nil, NewError(KindDuplicate, "下载器中已存在该种子", nil)
		}
		return nil, NewError(KindRejected, "下载器拒绝了该操作："+parsed.Error.Message, nil)
	}
	return parsed.Result, nil
}

// ensureLogin logs in when no session cookie is held.
func (c *DelugeClient) ensureLogin(ctx context.Context) error {
	if c.cookie != "" {
		return nil
	}
	return c.login(ctx)
}

// Test logs the Web UI in and reports the daemon version. Labels are reported
// as ready when the Label plugin is enabled.
func (c *DelugeClient) Test(ctx context.Context, categories []string) (*Status, error) {
	if err := c.login(ctx); err != nil {
		return nil, err
	}
	var version string
	if err := c.call(ctx, "daemon.info", nil, &version); err != nil {
		// Deluge 2.x beta used daemon.get_version; fall back rather than fail.
		if err := c.call(ctx, "daemon.get_version", nil, &version); err != nil {
			return nil, err
		}
	}

	status := &Status{Version: strings.TrimSpace(version)}
	labelsAvailable := c.labelPluginEnabled(ctx)
	for _, category := range categories {
		category = strings.TrimSpace(category)
		if category == "" {
			continue
		}
		if !labelsAvailable {
			status.CategoriesFailed = append(status.CategoriesFailed, category)
			continue
		}
		if err := c.call(ctx, "label.add", []any{category}, nil); err != nil {
			status.CategoriesFailed = append(status.CategoriesFailed, category)
			continue
		}
		status.CategoriesReady = append(status.CategoriesReady, category)
	}
	return status, nil
}

// labelPluginEnabled reports whether the Label plugin can be used at all.
func (c *DelugeClient) labelPluginEnabled(ctx context.Context) bool {
	var plugins []string
	if err := c.call(ctx, "core.get_enabled_plugins", nil, &plugins); err != nil {
		return false
	}
	for _, name := range plugins {
		if name == "Label" {
			return true
		}
	}
	return false
}

// Push adds a magnet or torrent link, then applies the label and save path.
func (c *DelugeClient) Push(ctx context.Context, opts PushOptions) error {
	if err := c.ensureLogin(ctx); err != nil {
		return err
	}

	options := map[string]any{"add_paused": opts.Paused, "remove_at_ratio": false}
	if path := normalizeSavePath(opts.SavePath); path != "" {
		options["download_location"] = path
	}

	payload := strings.TrimSpace(opts.Payload)
	var hash string
	var err error
	if isMagnet(payload) {
		err = c.call(ctx, "core.add_torrent_magnet", []any{payload, options}, &hash)
	} else {
		// Deluge 2.x has add_torrent_url; 1.3 does not. Try the URL first so the
		// daemon does the fetching, then fall back to handing over raw bytes
		// (which keeps the fetch inside the shared executor: proxy, timeout,
		// body cap).
		err = c.call(ctx, "core.add_torrent_url", []any{payload, options}, &hash)
		if err != nil && isUnknownMethod(err) {
			data, name, ferr := c.fetchTorrent(ctx, payload)
			if ferr != nil {
				return ferr
			}
			err = c.call(ctx, "core.add_torrent_file", []any{name, base64.StdEncoding.EncodeToString(data), options}, &hash)
		}
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(hash) == "" {
		return NewError(KindRejected, "下载器没有返回任务标识", nil)
	}

	if category := strings.TrimSpace(opts.Category); category != "" && c.labelPluginEnabled(ctx) {
		_ = c.call(ctx, "label.add", []any{category}, nil)
		if err := c.call(ctx, "label.set_torrent", []any{hash, category}, nil); err != nil {
			return err
		}
	}
	return nil
}

// isUnknownMethod reports the error a Deluge version without a method returns.
func isUnknownMethod(err error) bool {
	var cErr *Error
	if !As(err, &cErr) {
		return false
	}
	lower := strings.ToLower(cErr.Message)
	return strings.Contains(lower, "unknown method") || strings.Contains(lower, "not found") ||
		strings.Contains(lower, "does not exist")
}

// fetchTorrent downloads a .torrent link so a legacy Deluge can be fed bytes.
func (c *DelugeClient) fetchTorrent(ctx context.Context, rawURL string) ([]byte, string, error) {
	resp, err := c.exec.Do(ctx, httpclient.Request{
		URL:     rawURL,
		Timeout: 15 * time.Second,
		Proxy:   c.cfg.Proxy,
		// The torrent link is a public surface, unlike the LAN daemon, so the
		// global proxy stays in effect.
		BypassProxy: false,
	})
	if err != nil {
		return nil, "", translateHTTP(err)
	}
	name := "download.torrent"
	if parsed, perr := url.Parse(rawURL); perr == nil {
		if base := path.Base(parsed.Path); base != "" && base != "." && base != "/" {
			name = base
		}
	}
	return resp.Body, name, nil
}

// Tasks lists every torrent through the Web UI aggregate call.
func (c *DelugeClient) Tasks(ctx context.Context) ([]Task, error) {
	if err := c.ensureLogin(ctx); err != nil {
		return nil, err
	}
	fields := []string{
		"hash", "name", "state", "progress", "eta", "total_size",
		"download_payload_rate", "num_seeds", "total_seeds", "label",
	}
	var payload struct {
		Torrents map[string]struct {
			Hash       string  `json:"hash"`
			Name       string  `json:"name"`
			State      string  `json:"state"`
			Progress   float64 `json:"progress"`
			ETA        int64   `json:"eta"`
			TotalSize  int64   `json:"total_size"`
			Rate       int64   `json:"download_payload_rate"`
			NumSeeds   int     `json:"num_seeds"`
			TotalSeeds int     `json:"total_seeds"`
			Label      string  `json:"label"`
		} `json:"torrents"`
	}
	if err := c.call(ctx, "web.update_ui", []any{fields, map[string]any{}}, &payload); err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(payload.Torrents))
	for _, t := range payload.Torrents {
		if t.Hash == "" {
			continue
		}
		progress := t.Progress
		if progress > 1 {
			progress = progress / 100
		}
		out = append(out, Task{
			Hash:           strings.ToUpper(t.Hash),
			Name:           t.Name,
			SizeBytes:      t.TotalSize,
			Progress:       progress,
			SpeedBPS:       t.Rate,
			ETASeconds:     t.ETA,
			State:          delugeState(t.State),
			Category:       t.Label,
			ConnectedSeeds: t.NumSeeds,
			AvailableSeeds: t.TotalSeeds,
		})
	}
	return out, nil
}

// delugeState maps Deluge's state strings onto the shared vocabulary.
func delugeState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "downloading":
		return "downloading"
	case "seeding":
		return "uploading"
	case "paused":
		return "pausedDL"
	case "checking":
		return "checkingDL"
	case "queued":
		return "queuedDL"
	case "error":
		return "error"
	case "allocating":
		return "checkingDL"
	default:
		return state
	}
}

// Action pauses, resumes or deletes one torrent.
func (c *DelugeClient) Action(ctx context.Context, hash, action string, deleteFiles bool) error {
	if err := c.ensureLogin(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(hash) == "" {
		return NewError(KindUnsupported, "缺少任务标识", nil)
	}
	switch action {
	case "pause":
		return c.call(ctx, "core.pause_torrent", []any{[]string{hash}}, nil)
	case "resume":
		return c.call(ctx, "core.resume_torrent", []any{[]string{hash}}, nil)
	case "delete":
		return c.call(ctx, "core.remove_torrent", []any{hash, deleteFiles}, nil)
	default:
		return NewError(KindUnsupported, "不支持的操作", nil)
	}
}

// Categories lists the labels the Label plugin knows about.
func (c *DelugeClient) Categories(ctx context.Context) ([]CategoryInfo, error) {
	if err := c.ensureLogin(ctx); err != nil {
		return nil, err
	}
	if !c.labelPluginEnabled(ctx) {
		return []CategoryInfo{}, nil
	}
	var labels []string
	if err := c.call(ctx, "label.get_labels", nil, &labels); err != nil {
		return []CategoryInfo{}, nil
	}
	out := make([]CategoryInfo, 0, len(labels))
	for _, label := range labels {
		out = append(out, CategoryInfo{Name: label})
	}
	return out, nil
}

func isMagnet(payload string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(payload)), "magnet:")
}

var _ Client = (*DelugeClient)(nil)

// atoiOrZero keeps numeric string fields usable without extra error handling.
func atoiOrZero(raw string) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return v
}
