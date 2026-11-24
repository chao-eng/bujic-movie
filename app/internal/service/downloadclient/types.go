package downloadclient

import (
	"context"
	"fmt"
	"strings"

	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
)

// Task is one torrent as reported by a client. The download module never stores
// task state; this is read live and cached only for degraded display. State
// strings reuse the qBittorrent vocabulary so the UI stays kind-agnostic.
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

// Kind identifiers. They are stored in search_download_clients.kind.
const (
	KindQBittorrent  = "qbittorrent"
	KindTransmission = "transmission"
	KindDeluge       = "deluge"
	KindAria2        = "aria2"
)

// Auth describes which credential fields a kind uses, so the form can hide the
// rest instead of asking for values the client will ignore.
const (
	AuthAPIKeyOrUserPass = "api_key_or_user_pass" // qBittorrent 5.x key or Web UI login
	AuthUserPass         = "user_pass"            // Transmission RPC basic auth
	AuthPassword         = "password"             // Deluge Web UI password
	AuthToken            = "token"                // Aria2 RPC secret token
)

// Capabilities tells the UI (and the grab path) what a kind can actually do.
type Capabilities struct {
	// Categories reports whether the client has a category/label feature.
	Categories bool `json:"categories"`
	// CategoryPath reports whether a category already decides the save folder
	// (qBittorrent). When false, a download directory must be supplied
	// explicitly or the client falls back to its own default.
	CategoryPath bool `json:"category_path"`
	// SavePath reports whether this app may pin a torrent to an explicit
	// directory. It is false for qBittorrent: its torrents run under Automatic
	// Torrent Management, where the folder always comes from the category and a
	// savepath is ignored unless autoTMM is turned off for that torrent. Rather
	// than silently flipping a user's management mode, the form offers no
	// directory at all and the category stays the single source of truth.
	SavePath bool `json:"save_path"`
	// Queue reports whether sequential / first-last-piece hints are supported.
	Queue         bool `json:"queue"`
	ContentLayout bool `json:"content_layout"`
}

// KindInfo is the static description of one supported client kind.
type KindInfo struct {
	Kind         string       `json:"kind"`
	Name         string       `json:"name"`
	DefaultPort  int          `json:"default_port"`
	DefaultBase  string       `json:"default_url_base"`
	Auth         string       `json:"auth"`
	Capabilities Capabilities `json:"capabilities"`
}

// Kinds lists every implemented torrent client, in UI order.
func Kinds() []KindInfo {
	return []KindInfo{
		{
			Kind: KindQBittorrent, Name: "qBittorrent", DefaultPort: 8080, Auth: AuthAPIKeyOrUserPass,
			// SavePath stays false: a qBittorrent torrent under automatic
			// management takes its folder from the category and ignores a
			// savepath. The directory decision belongs to the category alone.
			Capabilities: Capabilities{Categories: true, CategoryPath: true, Queue: true, ContentLayout: true},
		},
		{
			Kind: KindTransmission, Name: "Transmission", DefaultPort: 9091, DefaultBase: "transmission", Auth: AuthUserPass,
			Capabilities: Capabilities{SavePath: true},
		},
		{
			Kind: KindDeluge, Name: "Deluge", DefaultPort: 8112, Auth: AuthPassword,
			Capabilities: Capabilities{Categories: true, SavePath: true},
		},
		{
			Kind: KindAria2, Name: "Aria2", DefaultPort: 6800, DefaultBase: "jsonrpc", Auth: AuthToken,
			Capabilities: Capabilities{SavePath: true},
		},
	}
}

// LookupKind returns the metadata for a kind, or false when it is unknown.
func LookupKind(kind string) (KindInfo, bool) {
	for _, info := range Kinds() {
		if info.Kind == kind {
			return info, true
		}
	}
	return KindInfo{}, false
}

// KindNames returns the supported kinds as a comma separated list for errors.
func KindNames() string {
	names := make([]string, 0, len(Kinds()))
	for _, info := range Kinds() {
		names = append(names, info.Name)
	}
	return strings.Join(names, "、")
}

// Config is everything needed to talk to one instance of any kind.
type Config struct {
	Kind     string
	Host     string
	Port     int
	UseSSL   bool
	URLBase  string
	APIKey   string
	Username string
	Password string
	Proxy    string
}

// PushOptions is one torrent handed to a client. SavePath is only meaningful
// when the kind's SavePath capability is set; Category likewise.
type PushOptions struct {
	// Kind is "magnet" or "torrent" (an http(s) .torrent link).
	Kind          string
	Payload       string
	Name          string
	Category      string
	SavePath      string
	Paused        bool
	Sequential    bool
	FirstLast     bool
	ContentLayout string
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

// Client is the seam the download module depends on. Every kind implements it,
// so GrabService and TaskService never branch on the client name.
type Client interface {
	// Test logs in and reports the version plus category readiness.
	Test(ctx context.Context, categories []string) (*Status, error)
	// Push adds a torrent by URL (magnet or http) with an optional category and
	// download directory.
	Push(ctx context.Context, opts PushOptions) error
	// Tasks lists current torrents.
	Tasks(ctx context.Context) ([]Task, error)
	// Action pauses, resumes or deletes one torrent.
	Action(ctx context.Context, hash, action string, deleteFiles bool) error
	// Categories lists the category names that already exist in the client.
	Categories(ctx context.Context) ([]CategoryInfo, error)
}

// Error kinds mirror the module-wide classification.
const (
	KindAuth        = "auth"
	KindUnreachable = "unreachable"
	KindUnsupported = "unsupported"
	KindRejected    = "rejected"
	KindDuplicate   = "duplicate"
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

// New builds a client for one configured instance.
func New(cfg Config, exec *httpclient.Executor) Client {
	switch strings.ToLower(strings.TrimSpace(cfg.Kind)) {
	case KindTransmission:
		return NewTransmissionClient(cfg, exec)
	case KindDeluge:
		return NewDelugeClient(cfg, exec)
	case KindAria2:
		return NewAria2Client(cfg, exec)
	default:
		return NewQBittorrentClient(cfg, exec)
	}
}

// baseURL builds scheme://host:port[/urlBase] for a config.
func baseURL(cfg Config) string {
	scheme := "http"
	if cfg.UseSSL {
		scheme = "https"
	}
	base := fmt.Sprintf("%s://%s:%d", scheme, cfg.Host, cfg.Port)
	if strings.TrimSpace(cfg.URLBase) != "" {
		base += "/" + strings.Trim(cfg.URLBase, "/")
	}
	return base
}

// translateHTTP maps a transport failure onto a downloader-specific message.
// The httpclient wording talks about indexer sites, which would confuse users
// here.
func translateHTTP(err error) error {
	var hErr *httpclient.Error
	if asHTTP(err, &hErr) {
		switch hErr.Kind {
		case httpclient.KindAuth:
			return NewError(KindAuth, "下载器认证失败，请检查账号、密码或密钥", nil)
		case httpclient.KindTimeout:
			return NewError(KindUnreachable, "下载器响应超时，请检查地址和端口", nil)
		case httpclient.KindBlocked, httpclient.KindChallenge:
			// A downloader answers 403 when it thinks the request is not from its
			// own Web UI — usually a wrong address or a reverse proxy.
			return NewError(KindAuth, "下载器拒绝了访问：请确认 Web UI/RPC 已开启、地址端口正确，且未被反向代理拦截", nil)
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

// As finds a classified client error anywhere in the chain.
func As(err error, target **Error) bool {
	for err != nil {
		if e, ok := err.(*Error); ok {
			*target = e
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

// normalizeSavePath trims a trailing slash so the same folder compares equal.
func normalizeSavePath(path string) string {
	return strings.TrimRight(strings.TrimSpace(path), "/")
}
