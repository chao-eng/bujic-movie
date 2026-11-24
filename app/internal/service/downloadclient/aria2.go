package downloadclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
)

// Aria2Client talks to the Aria2 JSON-RPC endpoint (/jsonrpc). Aria2 has no
// categories, so a push carries only the optional download directory.
//
// Aria2 addresses tasks by GID; the task list reports the GID in Hash so the
// pause/resume/delete calls the UI sends back are directly usable.
type Aria2Client struct {
	cfg    Config
	exec   *httpclient.Executor
	rpcURL string
}

// NewAria2Client builds an Aria2 JSON-RPC client.
func NewAria2Client(cfg Config, exec *httpclient.Executor) *Aria2Client {
	path := strings.Trim(strings.TrimSpace(cfg.URLBase), "/")
	if path == "" {
		path = "jsonrpc"
	}
	return &Aria2Client{cfg: cfg, exec: exec, rpcURL: baseURL(cfg) + "/" + path}
}

type aria2Response struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// call invokes one JSON-RPC method with the configured secret token.
func (c *Aria2Client) call(ctx context.Context, method string, params []any, out any) error {
	// Every Aria2 call takes "token:<secret>" as its first parameter.
	full := make([]any, 0, len(params)+1)
	full = append(full, "token:"+c.cfg.Password)
	full = append(full, params...)

	body := map[string]any{"jsonrpc": "2.0", "id": "bujic", "method": method, "params": full}
	payload, err := json.Marshal(body)
	if err != nil {
		return NewError(KindUnsupported, "请求构建失败", err)
	}

	resp, derr := c.exec.Do(ctx, httpclient.Request{
		Method:      http.MethodPost,
		URL:         c.rpcURL,
		Header:      map[string]string{"Content-Type": "application/json"},
		Body:        strings.NewReader(string(payload)),
		NoRetry:     true,
		Proxy:       c.cfg.Proxy,
		BypassProxy: c.cfg.Proxy == "",
	})
	if derr != nil {
		return translateHTTP(derr)
	}

	var parsed aria2Response
	if err := json.Unmarshal(resp.Body, &parsed); err != nil {
		return NewError(KindUnsupported, "下载器返回了无法识别的内容", err)
	}
	if parsed.Error != nil {
		message := strings.ToLower(parsed.Error.Message)
		if strings.Contains(message, "unauthorized") || parsed.Error.Code == 1 {
			return NewError(KindAuth, "RPC 密钥错误", nil)
		}
		if strings.Contains(message, "already") || strings.Contains(message, "duplicate") {
			return NewError(KindDuplicate, "下载器中已存在该种子", nil)
		}
		return NewError(KindRejected, "下载器拒绝了该操作："+parsed.Error.Message, nil)
	}
	if out != nil && len(parsed.Result) > 0 {
		if err := json.Unmarshal(parsed.Result, out); err != nil {
			return NewError(KindUnsupported, "下载器返回了无法识别的内容", err)
		}
	}
	return nil
}

// Test reports the Aria2 version.
func (c *Aria2Client) Test(ctx context.Context, categories []string) (*Status, error) {
	var info struct {
		Version string `json:"version"`
	}
	if err := c.call(ctx, "aria2.getVersion", nil, &info); err != nil {
		return nil, err
	}
	return &Status{Version: strings.TrimSpace(info.Version)}, nil
}

// Push adds a magnet or torrent link. Aria2 also accepts an http(s) .torrent
// URL through addUri, so both kinds use the same call.
func (c *Aria2Client) Push(ctx context.Context, opts PushOptions) error {
	options := map[string]string{}
	if path := normalizeSavePath(opts.SavePath); path != "" {
		options["dir"] = path
	}
	if opts.Paused {
		options["pause"] = "true"
	}
	params := []any{[]string{opts.Payload}}
	if len(options) > 0 {
		params = append(params, options)
	}

	var gid string
	if err := c.call(ctx, "aria2.addUri", params, &gid); err != nil {
		return err
	}
	if strings.TrimSpace(gid) == "" {
		return NewError(KindRejected, "下载器没有返回任务标识", nil)
	}
	return nil
}

// aria2Fields keeps the status payload small; Aria2 returns every field unless
// asked otherwise.
var aria2Fields = []string{
	"gid", "infoHash", "bittorrent", "status", "totalLength", "completedLength",
	"downloadSpeed", "errorMessage", "dir", "numSeeders",
}

// Tasks lists active, waiting and stopped torrents in one snapshot.
func (c *Aria2Client) Tasks(ctx context.Context) ([]Task, error) {
	var all []aria2Status
	for _, call := range []struct {
		method string
		params []any
	}{
		{"aria2.tellActive", []any{aria2Fields}},
		{"aria2.tellWaiting", []any{0, 200, aria2Fields}},
		{"aria2.tellStopped", []any{0, 200, aria2Fields}},
	} {
		var batch []aria2Status
		if err := c.call(ctx, call.method, call.params, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
	}
	out := make([]Task, 0, len(all))
	for _, s := range all {
		if s.GID == "" {
			continue
		}
		total := int64(atoiOrZero(s.TotalLength))
		completed := int64(atoiOrZero(s.CompletedLength))
		progress := 0.0
		if total > 0 {
			progress = float64(completed) / float64(total)
		}
		out = append(out, Task{
			Hash:           s.GID,
			Name:           s.BitTorrentNameOrDefault(),
			SizeBytes:      total,
			Progress:       progress,
			SpeedBPS:       int64(atoiOrZero(s.DownloadSpeed)),
			State:          aria2State(s.Status),
			ConnectedSeeds: int(atoiOrZero(s.NumSeeders)),
			AvailableSeeds: int(atoiOrZero(s.NumSeeders)),
		})
	}
	return out, nil
}

// aria2Status decodes Aria2's mixed string/number payload. Every numeric field
// arrives as a string there, so the raw shape is kept and converted on read.
type aria2Status struct {
	GID             string
	InfoHash        string
	Status          string
	TotalLength     string
	CompletedLength string
	DownloadSpeed   string
	ErrorMessage    string
	Dir             string
	NumSeeders      string
	BittorrentName  string
}

func (s *aria2Status) UnmarshalJSON(data []byte) error {
	var raw struct {
		GID             string `json:"gid"`
		InfoHash        string `json:"infoHash"`
		Status          string `json:"status"`
		TotalLength     any    `json:"totalLength"`
		CompletedLength any    `json:"completedLength"`
		DownloadSpeed   any    `json:"downloadSpeed"`
		ErrorMessage    string `json:"errorMessage"`
		Dir             string `json:"dir"`
		NumSeeders      any    `json:"numSeeders"`
		Bittorrent      struct {
			Info struct {
				Name string `json:"name"`
			} `json:"info"`
		} `json:"bittorrent"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.GID = raw.GID
	s.InfoHash = raw.InfoHash
	s.Status = raw.Status
	s.TotalLength = anyString(raw.TotalLength)
	s.CompletedLength = anyString(raw.CompletedLength)
	s.DownloadSpeed = anyString(raw.DownloadSpeed)
	s.ErrorMessage = raw.ErrorMessage
	s.Dir = raw.Dir
	s.NumSeeders = anyString(raw.NumSeeders)
	s.BittorrentName = raw.Bittorrent.Info.Name
	return nil
}

func (s aria2Status) BitTorrentNameOrDefault() string {
	if s.BittorrentName != "" {
		return s.BittorrentName
	}
	if s.GID != "" {
		return s.GID
	}
	return s.InfoHash
}

// aria2State maps Aria2's status onto the shared vocabulary.
func aria2State(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active":
		return "downloading"
	case "waiting":
		return "queuedDL"
	case "paused":
		return "pausedDL"
	case "complete":
		return "uploading"
	case "error", "removed":
		return "error"
	default:
		return status
	}
}

// Action pauses, resumes or removes one task, addressed by GID.
func (c *Aria2Client) Action(ctx context.Context, hash, action string, deleteFiles bool) error {
	if strings.TrimSpace(hash) == "" {
		return NewError(KindUnsupported, "缺少任务标识", nil)
	}
	gids := []string{hash}
	switch action {
	case "pause":
		return c.call(ctx, "aria2.forcePause", []any{gids}, nil)
	case "resume":
		return c.call(ctx, "aria2.unpause", []any{gids}, nil)
	case "delete":
		// forceRemove stops the download but keeps the files; nothing else is
		// safe to call here because Aria2 has no remove-with-data method.
		return c.call(ctx, "aria2.forceRemove", []any{gids}, nil)
	default:
		return NewError(KindUnsupported, "不支持的操作", nil)
	}
}

// Categories reports none: Aria2 has no label concept.
func (c *Aria2Client) Categories(ctx context.Context) ([]CategoryInfo, error) {
	return []CategoryInfo{}, nil
}

func anyString(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case json.Number:
		return value.String()
	default:
		return ""
	}
}

var _ Client = (*Aria2Client)(nil)
