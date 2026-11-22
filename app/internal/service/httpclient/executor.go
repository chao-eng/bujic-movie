package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
	"golang.org/x/time/rate"
)

// Limits come from BR-39 and are enforced on every request so a hostile or
// broken site cannot exhaust memory.
const (
	MaxResponseBytes = 20 << 20 // 20MB
	DefaultTimeout   = 8 * time.Second
)

// Error kinds are stable strings surfaced to the UI as failure reasons.
const (
	KindUnreachable = "unreachable"
	KindTimeout     = "timeout"
	KindAuth        = "auth"
	KindBlocked     = "blocked"
	KindChallenge   = "challenge"
	KindTooLarge    = "too_large"
	KindUnsupported = "unsupported"
)

// Error carries a kind plus a user-facing Chinese message. It never contains
// credentials, cookies or full magnet links.
type Error struct {
	Kind    string
	Message string
	Status  int
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

func newError(kind, message string, err error) *Error {
	return &Error{Kind: kind, Message: message, Err: err}
}

// Request describes one outbound call.
type Request struct {
	Method    string
	URL       string
	Header    map[string]string
	Body      io.Reader
	Timeout   time.Duration
	Allowed   []string // host allowlist; empty means only the URL host
	NoRetry   bool
	RateKey   string
	RateLimit time.Duration
	// RateBurst is how many calls may start before the interval applies. A
	// definition whose search spans several pages issues those requests at the
	// same time, and a burst of 1 would serialise them back into one per
	// interval — slower than before, because nothing else is running then.
	RateBurst int
	MaxBytes  int64
	// Proxy overrides the global proxy for this call (http/https/socks5).
	Proxy string
	// BypassProxy skips the global proxy. Download clients live on the LAN and
	// must not be tunnelled through the search proxy.
	BypassProxy bool
}

// Response is the bounded result of a call.
type Response struct {
	Status  int
	Body    []byte
	Header  http.Header
	Elapsed time.Duration
	URL     string
}

// Doer is the seam used by tests to stub transport behaviour.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Clock lets tests control retry backoff without sleeping.
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

type realClock struct{}

func (realClock) Now() time.Time        { return time.Now() }
func (realClock) Sleep(d time.Duration) { time.Sleep(d) }

// Executor applies timeouts, retries, rate limits, host allowlists and body
// caps to every outbound request.
type Executor struct {
	client  Doer
	clock   Clock
	limiter *limiterSet

	mu       sync.RWMutex
	global   *url.URL
	resolver func() string
}

// New builds an executor. globalProxy may be empty; when set it applies to all
// requests that do not specify their own proxy.
func New(client Doer, clock Clock, globalProxy string) (*Executor, error) {
	if client == nil {
		client = &http.Client{}
	}
	if clock == nil {
		clock = realClock{}
	}
	e := &Executor{client: client, clock: clock, limiter: newLimiterSet()}
	if strings.TrimSpace(globalProxy) != "" {
		u, err := ParseProxy(globalProxy)
		if err != nil {
			return nil, err
		}
		e.global = u
	}
	return e, nil
}

// SetGlobalProxyResolver makes the global proxy read from a live source, so a
// settings change takes effect without restarting the server.
func (e *Executor) SetGlobalProxyResolver(resolver func() string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.resolver = resolver
	if resolver != nil {
		if u, err := ParseProxy(resolver()); err == nil {
			e.global = u
		} else if strings.TrimSpace(resolver()) == "" {
			e.global = nil
		}
	}
}

// CurrentGlobalProxy reports the proxy in effect, for diagnostics.
func (e *Executor) CurrentGlobalProxy() string {
	if u := e.resolveGlobal(); u != nil {
		return u.String()
	}
	return ""
}

// resolveGlobal returns the proxy currently configured, preferring the live
// resolver over the value captured at construction.
func (e *Executor) resolveGlobal() *url.URL {
	e.mu.RLock()
	resolver := e.resolver
	captured := e.global
	e.mu.RUnlock()

	if resolver == nil {
		return captured
	}
	raw := strings.TrimSpace(resolver())
	if raw == "" {
		return nil
	}
	u, err := ParseProxy(raw)
	if err != nil {
		return nil
	}
	return u
}

// ParseProxy validates a proxy URL and rejects non-http/socks5 schemes.
func ParseProxy(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		// 不要包 url 错误：它的文本会原样回显整个地址，而代理 URL 常带
		// user:password。这条错误会被写进日志，日志流又是公开的。
		return nil, errors.New("代理地址无法解析")
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("代理只支持 http/socks5，收到 %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("代理地址缺少主机")
	}
	return u, nil
}

// Do performs the request with retries for idempotent calls only.
func (e *Executor) Do(ctx context.Context, req Request) (*Response, error) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	if req.URL == "" {
		return nil, newError(KindUnsupported, "请求地址为空", nil)
	}
	target, err := url.Parse(req.URL)
	if err != nil {
		return nil, newError(KindUnsupported, "请求地址无法解析", err)
	}
	switch target.Scheme {
	case "http", "https":
	default:
		return nil, newError(KindUnsupported, fmt.Sprintf("只支持 http/https，收到 %q", target.Scheme), nil)
	}
	if err := checkAllowed(target, req.Allowed); err != nil {
		return nil, err
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = MaxResponseBytes
	}

	// Wait for the per-site rate limiter before spending the timeout.
	if req.RateKey != "" && req.RateLimit > 0 {
		if err := e.limiter.wait(ctx, req.RateKey, req.RateLimit, req.RateBurst); err != nil {
			return nil, newError(KindTimeout, "排队等待超过预算", err)
		}
	}

	attempts := 1
	if method == http.MethodGet && !req.NoRetry {
		attempts = 3
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		resp, err := e.once(ctx, method, req, target, timeout, maxBytes)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		var eErr *Error
		if errors.As(err, &eErr) && !retryable(eErr) {
			return nil, err
		}
		if attempt < attempts {
			// Backoff 1s then 2s, matching BR-33.
			e.clock.Sleep(time.Duration(attempt) * time.Second)
		}
	}
	return nil, lastErr
}

func retryable(e *Error) bool {
	switch e.Kind {
	case KindTimeout, KindUnreachable:
		return true
	case KindBlocked:
		return e.Status == http.StatusTooManyRequests || e.Status >= 500
	default:
		return false
	}
}

func (e *Executor) once(ctx context.Context, method string, req Request, target *url.URL, timeout time.Duration, maxBytes int64) (*Response, error) {
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(callCtx, method, target.String(), req.Body)
	if err != nil {
		return nil, newError(KindUnsupported, "请求构建失败", err)
	}
	for k, v := range req.Header {
		httpReq.Header.Set(k, v)
	}

	start := e.clock.Now()
	client, err := e.clientFor(target, req)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return nil, newError(KindTimeout, "站点响应超时", err)
		}
		return nil, newError(KindUnreachable, "无法连接站点", err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxBytes+1)
	body, readErr := io.ReadAll(limited)
	if readErr != nil {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return nil, newError(KindTimeout, "读取响应超时", readErr)
		}
		return nil, newError(KindUnreachable, "读取响应失败", readErr)
	}
	if int64(len(body)) > maxBytes {
		return nil, &Error{Kind: KindTooLarge, Message: fmt.Sprintf("响应超过 %d 字节上限", maxBytes)}
	}

	out := &Response{Status: resp.StatusCode, Body: body, Header: resp.Header, Elapsed: e.clock.Now().Sub(start), URL: target.String()}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return out, &Error{Kind: KindAuth, Message: "站点拒绝访问，请检查账号或密钥", Status: resp.StatusCode}
	case resp.StatusCode == http.StatusForbidden:
		// A Cloudflare interactive challenge cannot be solved without a browser
		// runtime, which this module does not embed (PRD D-09). Report it as its
		// own kind so the UI can say so instead of hinting at a config problem.
		if isChallengeResponse(out) {
			return out, &Error{Kind: KindChallenge, Message: "站点要求浏览器验证（Cloudflare 挑战），当前不支持绕过；请换用其它站点", Status: resp.StatusCode}
		}
		// Any other 403 from a public site is bot protection, not a bad
		// credential: say "blocked" so the UI suggests a proxy or another network.
		return out, &Error{Kind: KindBlocked, Message: "站点拒绝访问（可能被反爬拦截）", Status: resp.StatusCode}
	case resp.StatusCode == http.StatusTooManyRequests:
		return out, &Error{Kind: KindBlocked, Message: "站点限流", Status: resp.StatusCode}
	case resp.StatusCode >= 400:
		return out, &Error{Kind: KindBlocked, Message: fmt.Sprintf("站点返回 %d", resp.StatusCode), Status: resp.StatusCode}
	}
	return out, nil
}

// clientFor returns an http client honouring the per-request or global proxy.
func (e *Executor) clientFor(target *url.URL, req Request) (Doer, error) {
	// Tests inject a Doer and expect it to receive every call.
	if _, ok := e.client.(*http.Client); !ok {
		return e.client, nil
	}
	hc := e.client.(*http.Client)
	// Redirects are followed by net/http, outside the pre-flight allowlist
	// check, so validate every hop here (BR-36).
	clone := *hc
	clone.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return newError(KindBlocked, "重定向次数过多", nil)
		}
		if err := checkAllowed(next.URL, req.Allowed); err != nil {
			return err
		}
		return nil
	}

	if proxyURL := e.proxyFor(req); proxyURL != nil {
		transport, err := transportFor(proxyURL)
		if err != nil {
			return nil, err
		}
		clone.Transport = transport
		return &clone, nil
	}
	if clone.Transport == nil {
		clone.Transport = http.DefaultTransport.(*http.Transport).Clone()
	}
	return &clone, nil
}

func (e *Executor) proxyFor(req Request) *url.URL {
	if req.BypassProxy {
		return nil
	}
	if strings.TrimSpace(req.Proxy) != "" {
		if u, err := ParseProxy(req.Proxy); err == nil {
			return u
		}
	}
	return e.resolveGlobal()
}

func transportFor(proxyURL *url.URL) (*http.Transport, error) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	switch proxyURL.Scheme {
	case "http", "https":
		base.Proxy = http.ProxyURL(proxyURL)
	case "socks5", "socks5h":
		dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, socksAuth(proxyURL), proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("SOCKS5 代理不可用: %w", err)
		}
		base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			type ctxDialer interface {
				DialContext(ctx context.Context, network, addr string) (net.Conn, error)
			}
			if cd, ok := dialer.(ctxDialer); ok {
				return cd.DialContext(ctx, network, addr)
			}
			return dialer.Dial(network, addr)
		}
		base.Proxy = nil
	default:
		return nil, fmt.Errorf("不支持的代理协议 %q", proxyURL.Scheme)
	}
	return base, nil
}

func socksAuth(u *url.URL) *proxy.Auth {
	if u.User == nil {
		return nil
	}
	pass, _ := u.User.Password()
	return &proxy.Auth{User: u.User.Username(), Password: pass}
}

// isChallengeResponse detects an interactive anti-bot challenge. Cloudflare
// marks it with `cf-mitigated: challenge`; other providers leave script or
// interstitial markers in the body.
func isChallengeResponse(resp *Response) bool {
	if resp == nil {
		return false
	}
	for key, values := range resp.Header {
		if !strings.EqualFold(key, "cf-mitigated") {
			continue
		}
		for _, v := range values {
			if strings.EqualFold(strings.TrimSpace(v), "challenge") {
				return true
			}
		}
	}
	body := strings.ToLower(string(resp.Body))
	for _, marker := range []string{
		"just a moment",
		"enable javascript and cookies to continue",
		"challenge-platform",
		"cf-chl-",
	} {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}

// checkAllowed enforces the host allowlist from BR-36.
func checkAllowed(target *url.URL, allowed []string) error {
	host := strings.ToLower(target.Hostname())
	if host == "" {
		return newError(KindUnsupported, "请求缺少主机名", nil)
	}
	allow := allowed
	if len(allow) == 0 {
		return nil
	}
	for _, a := range allow {
		if strings.EqualFold(strings.TrimSpace(a), host) {
			return nil
		}
	}
	return newError(KindBlocked, fmt.Sprintf("目标主机 %s 不在允许列表内", host), nil)
}

// HostsFromLinks extracts hosts from definition links for the allowlist.
func HostsFromLinks(links []string) []string {
	hosts := make([]string, 0, len(links))
	seen := map[string]bool{}
	for _, link := range links {
		u, err := url.Parse(link)
		if err != nil || u.Hostname() == "" {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if seen[host] {
			continue
		}
		seen[host] = true
		hosts = append(hosts, host)
	}
	return hosts
}

// Waiter enforces one site's minimum request interval.
type Waiter interface {
	Wait(ctx context.Context) error
}

// WaiterFactory builds a Waiter for an interval. It is a variable so tests can
// replace the real limiter with an instant one; production never reassigns it.
var WaiterFactory = func(interval time.Duration, burst int) Waiter {
	if burst < 1 {
		burst = 1
	}
	return rate.NewLimiter(rate.Every(interval), burst)
}

// limiterSet keeps one waiter per rate key so different sites never share a
// budget and the same site is serialised.
type limiterSet struct {
	mu    sync.Mutex
	byKey map[string]waiterEntry
}

type waiterEntry struct {
	interval time.Duration
	burst    int
	waiter   Waiter
}

func newLimiterSet() *limiterSet {
	return &limiterSet{byKey: map[string]waiterEntry{}}
}

func (s *limiterSet) wait(ctx context.Context, key string, interval time.Duration, burst int) error {
	if burst < 1 {
		burst = 1
	}
	s.mu.Lock()
	entry, ok := s.byKey[key]
	// Rebuild when the interval or burst changes. Raising the burst for one
	// multi-page search would otherwise persist: a later single-page request
	// would keep the larger allowance and quietly bypass the site's rate limit.
	// rate.NewLimiter already carries its own token bucket, so rebuilding does
	// not reset the reserved rate.
	if !ok || entry.interval != interval || entry.burst != burst {
		entry = waiterEntry{interval: interval, burst: burst, waiter: WaiterFactory(interval, burst)}
		s.byKey[key] = entry
	}
	s.mu.Unlock()
	return entry.waiter.Wait(ctx)
}
