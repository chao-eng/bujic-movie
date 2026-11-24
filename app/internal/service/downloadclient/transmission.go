package downloadclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
)

// TransmissionClient talks to the Transmission RPC endpoint (2.40+).
//
// Transmission has no category/label feature: a "category" is really a
// sub-folder of its download directory. This client therefore never invents
// categories, and the download module passes an explicit save path instead.
type TransmissionClient struct {
	cfg       Config
	exec      *httpclient.Executor
	rpcURL    string
	sessionID string
}

// NewTransmissionClient builds a Transmission RPC client. url_base defaults to
// "transmission", which is what the daemon itself uses.
func NewTransmissionClient(cfg Config, exec *httpclient.Executor) *TransmissionClient {
	base := strings.Trim(strings.TrimSpace(cfg.URLBase), "/")
	if base == "" {
		base = "transmission"
	}
	return &TransmissionClient{
		cfg:    cfg,
		exec:   exec,
		rpcURL: baseURL(Config{Host: cfg.Host, Port: cfg.Port, UseSSL: cfg.UseSSL, URLBase: base + "/rpc"}),
	}
}

type transmissionResponse struct {
	Result    string          `json:"result"`
	Arguments json.RawMessage `json:"arguments"`
}

func (c *TransmissionClient) headers() map[string]string {
	h := map[string]string{"Content-Type": "application/json"}
	if c.sessionID != "" {
		h["X-Transmission-Session-Id"] = c.sessionID
	}
	if c.cfg.Username != "" || c.cfg.Password != "" {
		// The executor only carries static headers, so basic auth is encoded
		// here rather than through a custom transport.
		pair := c.cfg.Username + ":" + c.cfg.Password
		h["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(pair))
	}
	return h
}

// call posts one RPC method, performing the 409 session handshake first.
func (c *TransmissionClient) call(ctx context.Context, method string, args any, out any) error {
	payload := map[string]any{"method": method}
	if args != nil {
		payload["arguments"] = args
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return NewError(KindUnsupported, "请求构建失败", err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		resp, derr := c.exec.Do(ctx, httpclient.Request{
			Method:      http.MethodPost,
			URL:         c.rpcURL,
			Header:      c.headers(),
			Body:        strings.NewReader(string(body)),
			NoRetry:     true,
			Proxy:       c.cfg.Proxy,
			BypassProxy: c.cfg.Proxy == "",
			AllowStatus: []int{http.StatusConflict},
		})
		if derr != nil {
			return translateHTTP(derr)
		}
		if resp.Status == http.StatusConflict {
			// Handshake: the daemon answers 409 until a request echoes back its
			// session id. Remember it and retry once.
			id := resp.Header.Get("X-Transmission-Session-Id")
			if id == "" {
				return NewError(KindAuth, "下载器没有返回会话标识；请确认 RPC 白名单允许本机访问", nil)
			}
			c.sessionID = id
			continue
		}
		return c.decode(resp.Body, out)
	}
	return NewError(KindAuth, "下载器拒绝会话，请检查 RPC 白名单", nil)
}

func (c *TransmissionClient) decode(body []byte, out any) error {
	var parsed transmissionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return NewError(KindUnsupported, "下载器返回了无法识别的内容", err)
	}
	if parsed.Result != "" && parsed.Result != "success" {
		if strings.Contains(strings.ToLower(parsed.Result), "duplicate") {
			return NewError(KindDuplicate, "下载器中已存在该种子", nil)
		}
		return NewError(KindRejected, "下载器拒绝了该操作："+parsed.Result, nil)
	}
	if out != nil && len(parsed.Arguments) > 0 {
		if err := json.Unmarshal(parsed.Arguments, out); err != nil {
			return NewError(KindUnsupported, "下载器返回了无法识别的内容", err)
		}
	}
	return nil
}

// Test reports the daemon version. Transmission has no categories, so category
// readiness is always empty.
func (c *TransmissionClient) Test(ctx context.Context, categories []string) (*Status, error) {
	var session struct {
		Version    string `json:"version"`
		RPCVersion int    `json:"rpc-version"`
	}
	if err := c.call(ctx, "session-get", nil, &session); err != nil {
		return nil, err
	}
	version := strings.TrimSpace(session.Version)
	if session.RPCVersion > 0 {
		version = fmt.Sprintf("%s (RPC %d)", version, session.RPCVersion)
	}
	return &Status{Version: version}, nil
}

// Push adds a magnet or torrent link. Category is ignored: Transmission cannot
// map one to a folder, so only the explicit save path matters.
func (c *TransmissionClient) Push(ctx context.Context, opts PushOptions) error {
	args := map[string]any{
		"filename": opts.Payload,
		"paused":   opts.Paused,
	}
	if path := normalizeSavePath(opts.SavePath); path != "" {
		args["download-dir"] = path
	}
	return c.call(ctx, "torrent-add", args, nil)
}

var transmissionFields = []string{
	"hashString", "name", "totalSize", "percentDone", "rateDownload", "eta",
	"status", "downloadDir", "errorString", "peersSendingToUs", "trackers",
}

// Tasks lists every torrent.
func (c *TransmissionClient) Tasks(ctx context.Context) ([]Task, error) {
	var payload struct {
		Torrents []struct {
			HashString       string  `json:"hashString"`
			Name             string  `json:"name"`
			TotalSize        int64   `json:"totalSize"`
			PercentDone      float64 `json:"percentDone"`
			RateDownload     int64   `json:"rateDownload"`
			ETA              int64   `json:"eta"`
			Status           int     `json:"status"`
			ErrorString      string  `json:"errorString"`
			PeersSendingToUs int     `json:"peersSendingToUs"`
			Trackers         []struct {
				SeederCount int `json:"seederCount"`
			} `json:"trackers"`
		} `json:"torrents"`
	}
	if err := c.call(ctx, "torrent-get", map[string]any{"fields": transmissionFields}, &payload); err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(payload.Torrents))
	for _, t := range payload.Torrents {
		if t.HashString == "" {
			continue
		}
		swarm := 0
		for _, tracker := range t.Trackers {
			if tracker.SeederCount > swarm {
				swarm = tracker.SeederCount
			}
		}
		out = append(out, Task{
			Hash:           strings.ToUpper(t.HashString),
			Name:           t.Name,
			SizeBytes:      t.TotalSize,
			Progress:       t.PercentDone,
			SpeedBPS:       t.RateDownload,
			ETASeconds:     t.ETA,
			State:          transmissionState(t.Status, t.ErrorString),
			ConnectedSeeds: t.PeersSendingToUs,
			AvailableSeeds: swarm,
		})
	}
	return out, nil
}

// transmissionState maps the RPC status enum onto the shared vocabulary, which
// is qBittorrent's, so the UI needs no per-kind table.
func transmissionState(status int, errText string) string {
	if strings.TrimSpace(errText) != "" {
		return "error"
	}
	switch status {
	case 0:
		return "pausedUP" // stopped
	case 1, 2:
		return "checkingDL"
	case 3:
		return "queuedDL"
	case 4:
		return "downloading"
	case 5:
		return "queuedUP"
	case 6:
		return "uploading"
	default:
		return "unknown"
	}
}

// Action pauses, resumes or deletes one torrent.
func (c *TransmissionClient) Action(ctx context.Context, hash, action string, deleteFiles bool) error {
	if strings.TrimSpace(hash) == "" {
		return NewError(KindUnsupported, "缺少任务标识", nil)
	}
	ids := []string{hash}
	switch action {
	case "pause":
		return c.call(ctx, "torrent-stop", map[string]any{"ids": ids}, nil)
	case "resume":
		return c.call(ctx, "torrent-start", map[string]any{"ids": ids}, nil)
	case "delete":
		return c.call(ctx, "torrent-remove", map[string]any{"ids": ids, "delete-local-data": deleteFiles}, nil)
	default:
		return NewError(KindUnsupported, "不支持的操作", nil)
	}
}

// Categories reports none: Transmission has no labels, so the mapping UI keeps
// a download directory per source category instead of a category name.
func (c *TransmissionClient) Categories(ctx context.Context) ([]CategoryInfo, error) {
	return []CategoryInfo{}, nil
}

var _ Client = (*TransmissionClient)(nil)
