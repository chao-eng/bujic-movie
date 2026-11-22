package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
}
func (c *fakeClock) sleepDurations() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]time.Duration, len(c.sleeps))
	copy(out, c.sleeps)
	return out
}

// TST-044 / BR-27: GET retries twice with 1s then 2s backoff.
func TestTST044_RetryBackoffOnServerError(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	clock := newFakeClock()
	exec, err := New(nil, clock, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := exec.Do(context.Background(), Request{URL: srv.URL})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.Status != http.StatusOK || string(resp.Body) != "ok" {
		t.Fatalf("unexpected response: %d %q", resp.Status, resp.Body)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	sleeps := clock.sleepDurations()
	if len(sleeps) != 2 || sleeps[0] != time.Second || sleeps[1] != 2*time.Second {
		t.Fatalf("backoff = %v, want [1s 2s]", sleeps)
	}
}

// TST-033 / BR-33: non-idempotent requests are never retried.
func TestTST033_PostIsNotRetried(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	exec, _ := New(nil, newFakeClock(), "")
	_, err := exec.Do(context.Background(), Request{Method: http.MethodPost, URL: srv.URL, NoRetry: true})
	if err == nil {
		t.Fatal("expected an error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

// TST-010: auth failures are classified and not retried.
func TestTST010_Classification(t *testing.T) {
	cases := []struct {
		status int
		kind   string
	}{
		{http.StatusUnauthorized, KindAuth},
		{http.StatusForbidden, KindBlocked},
		{http.StatusTooManyRequests, KindBlocked},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
		}))
		exec, _ := New(nil, newFakeClock(), "")
		_, err := exec.Do(context.Background(), Request{URL: srv.URL})
		var eErr *Error
		if !errors.As(err, &eErr) || eErr.Kind != c.kind {
			t.Errorf("status %d: kind = %v, want %s", c.status, err, c.kind)
		}
		srv.Close()
	}
}

// TST-051 / BR-39: responses larger than the cap are rejected.
func TestTST051_ResponseBodyLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", 2048)))
	}))
	defer srv.Close()

	exec, _ := New(nil, newFakeClock(), "")
	_, err := exec.Do(context.Background(), Request{URL: srv.URL, MaxBytes: 1024})
	var eErr *Error
	if !errors.As(err, &eErr) || eErr.Kind != KindTooLarge {
		t.Fatalf("kind = %v, want %s", err, KindTooLarge)
	}
}

// TST-025 / BR-04: a slow site is cut at the timeout and classified.
func TestTST025_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exec, _ := New(nil, newFakeClock(), "")
	_, err := exec.Do(context.Background(), Request{URL: srv.URL, Timeout: 50 * time.Millisecond})
	var eErr *Error
	if !errors.As(err, &eErr) || eErr.Kind != KindTimeout {
		t.Fatalf("kind = %v, want %s", err, KindTimeout)
	}
}

// TST-050 / BR-36: only http/https is allowed and the host allowlist is enforced.
func TestTST050_SchemeAndAllowlist(t *testing.T) {
	exec, _ := New(nil, newFakeClock(), "")

	if _, err := exec.Do(context.Background(), Request{URL: "file:///etc/passwd"}); err == nil {
		t.Fatal("file scheme must be rejected")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	_, err := exec.Do(context.Background(), Request{URL: srv.URL, Allowed: []string{"only.example"}})
	var eErr *Error
	if !errors.As(err, &eErr) || eErr.Kind != KindBlocked {
		t.Fatalf("allowlist not enforced: %v", err)
	}
}

func TestTST050_HostsFromLinks(t *testing.T) {
	hosts := HostsFromLinks([]string{
		"https://A.example/", "http://a.example", "https://b.example/x", "not a url",
	})
	if len(hosts) != 2 || hosts[0] != "a.example" || hosts[1] != "b.example" {
		t.Fatalf("hosts = %v", hosts)
	}
}

// TST-017 / BR-08: per-key rate limiting serialises calls to one site.
func TestTST017_RateLimitPerKey(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer srv.Close()

	exec, _ := New(nil, newFakeClock(), "")
	const interval = 250 * time.Millisecond

	// Measure from the client side: the wait happens before the request is sent,
	// so arrival timestamps would hide part of the interval behind request latency.
	start := time.Now()
	for i := 0; i < 2; i++ {
		if _, err := exec.Do(context.Background(), Request{URL: srv.URL, RateKey: "k", RateLimit: interval}); err != nil {
			t.Fatalf("Do: %v", err)
		}
	}
	elapsed := time.Since(start)
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	// Two calls with one interval between them must take at least ~1 interval.
	if elapsed < interval*4/5 {
		t.Fatalf("elapsed = %v, want >= %v (rate limit not applied)", elapsed, interval*4/5)
	}
}

func TestTST058_ParseProxyRejectsUnknownScheme(t *testing.T) {
	if _, err := ParseProxy("ftp://proxy:1"); err == nil {
		t.Fatal("ftp proxy must be rejected")
	}
	if _, err := ParseProxy("socks5://127.0.0.1:1080"); err != nil {
		t.Fatalf("socks5 must be accepted: %v", err)
	}
	if _, err := ParseProxy("http://user:pass@127.0.0.1:8080"); err != nil {
		t.Fatalf("http with credentials must be accepted: %v", err)
	}
}

func TestTST058_ErrorNeverLeaksSecrets(t *testing.T) {
	exec, _ := New(nil, newFakeClock(), "")
	_, err := exec.Do(context.Background(), Request{URL: "http://user:secret@127.0.0.1:1/x"})
	if err == nil {
		t.Fatal("expected connection error")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("error leaked credentials: %v", err)
	}
}

// TST-050 / BR-36: a redirect to a host outside the allowlist is blocked.
func TestTST050_RedirectOutsideAllowlistBlocked(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://blocked.invalid/evil", http.StatusFound)
	}))
	defer target.Close()

	exec, _ := New(nil, newFakeClock(), "")
	_, err := exec.Do(context.Background(), Request{
		URL:     target.URL,
		Allowed: []string{strings.TrimPrefix(strings.TrimPrefix(target.URL, "http://"), "https://")},
	})
	var eErr *Error
	if err == nil || !errors.As(err, &eErr) || eErr.Kind != KindBlocked {
		t.Fatalf("redirect must be blocked, got %v", err)
	}
}

// A redirect within the allowlist is followed.
func TestTST050_RedirectInsideAllowlistAllowed(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	host := strings.Split(strings.TrimPrefix(srv.URL, "http://"), ":")[0]

	exec, _ := New(nil, newFakeClock(), "")
	resp, err := exec.Do(context.Background(), Request{URL: srv.URL + "/start", Allowed: []string{host}})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if string(resp.Body) != "ok" {
		t.Fatalf("body = %q", resp.Body)
	}
}

// TST-010 / 42204: a Cloudflare interactive challenge gets its own kind so the
// UI can say "not supported" instead of "check your password".
func TestTST010_CloudflareChallengeDetected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cf-Mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<html><title>Just a moment...</title><script src="/cdn-cgi/challenge-platform/x.js"></script></html>`))
	}))
	defer srv.Close()

	exec, _ := New(nil, newFakeClock(), "")
	_, err := exec.Do(context.Background(), Request{URL: srv.URL})
	var eErr *Error
	if !errors.As(err, &eErr) || eErr.Kind != KindChallenge {
		t.Fatalf("kind = %v, want %s", err, KindChallenge)
	}
	if !strings.Contains(eErr.Message, "浏览器验证") {
		t.Errorf("message must explain the challenge: %q", eErr.Message)
	}
}

// A plain 403 stays "blocked": it is recoverable with a proxy or another IP.
func TestTST010_PlainForbiddenStaysBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html>forbidden</html>"))
	}))
	defer srv.Close()

	exec, _ := New(nil, newFakeClock(), "")
	_, err := exec.Do(context.Background(), Request{URL: srv.URL})
	var eErr *Error
	if !errors.As(err, &eErr) || eErr.Kind != KindBlocked {
		t.Fatalf("kind = %v, want %s", err, KindBlocked)
	}
}

// TST-058 / proxy: the global proxy is read live, so a settings change applies
// without restarting the server.
func TestTST058_GlobalProxyResolverIsLive(t *testing.T) {
	exec, _ := New(nil, newFakeClock(), "")
	if got := exec.CurrentGlobalProxy(); got != "" {
		t.Fatalf("initial proxy = %q", got)
	}

	current := "http://127.0.0.1:8080"
	exec.SetGlobalProxyResolver(func() string { return current })
	if got := exec.CurrentGlobalProxy(); got != current {
		t.Fatalf("proxy = %q, want %q", got, current)
	}

	current = "socks5h://127.0.0.1:1080"
	if got := exec.CurrentGlobalProxy(); got != "" && got != current {
		t.Fatalf("proxy after change = %q", got)
	}

	current = ""
	if got := exec.CurrentGlobalProxy(); got != "" {
		t.Fatalf("cleared proxy = %q", got)
	}
}

// Download-client requests must never be tunnelled through the search proxy.
func TestTST058_BypassProxy(t *testing.T) {
	var proxied int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxied, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("direct"))
	}))
	defer target.Close()

	exec, _ := New(nil, newFakeClock(), proxy.URL)
	exec.SetGlobalProxyResolver(func() string { return proxy.URL })

	resp, err := exec.Do(context.Background(), Request{URL: target.URL, BypassProxy: true})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if string(resp.Body) != "direct" {
		t.Fatalf("body = %q", resp.Body)
	}
	if atomic.LoadInt32(&proxied) != 0 {
		t.Fatalf("proxy was used %d times, want 0", proxied)
	}
}

// 一次多 path 搜索把 burst 抬高后，后续单 path 请求必须回到自己的 burst，
// 否则站点限流会被悄悄放宽。
func TestLimiterBurstDoesNotPersist(t *testing.T) {
	var seen []int
	restore := WaiterFactory
	WaiterFactory = func(_ time.Duration, burst int) Waiter {
		seen = append(seen, burst)
		return instantWaiterHttp{}
	}
	t.Cleanup(func() { WaiterFactory = restore })

	set := newLimiterSet()
	ctx := context.Background()
	const key = "site:a"
	const interval = time.Second

	// 多 path：burst 4
	if err := set.wait(ctx, key, interval, 4); err != nil {
		t.Fatalf("wait1: %v", err)
	}
	// 单 path：burst 1，必须重建而不是复用 4
	if err := set.wait(ctx, key, interval, 1); err != nil {
		t.Fatalf("wait2: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("expected two limiter builds, got %v", seen)
	}
	if seen[0] != 4 || seen[1] != 1 {
		t.Errorf("bursts = %v, want [4 1]", seen)
	}
}

type instantWaiterHttp struct{}

func (instantWaiterHttp) Wait(context.Context) error { return nil }
