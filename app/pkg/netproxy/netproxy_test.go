package netproxy

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// The provider must send traffic through the proxy only while the address is
// configured, and pick up a change without a restart.
func TestProviderFollowsResolver(t *testing.T) {
	var proxied int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxied, 1)
		// A forward proxy receives the absolute URL; answer directly so the
		// caller sees a successful response.
		_, _ = w.Write([]byte("via-proxy"))
	}))
	defer proxyServer.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("direct"))
	}))
	defer target.Close()

	var current atomic.Value
	current.Store("")
	provider := NewProvider(func() string {
		value, _ := current.Load().(string)
		return value
	}, 0)

	resp, err := provider.Do(mustRequest(t, target.URL))
	if err != nil {
		t.Fatalf("direct request: %v", err)
	}
	resp.Body.Close()
	if got := atomic.LoadInt32(&proxied); got != 0 {
		t.Fatalf("direct mode must not touch the proxy, hits = %d", got)
	}

	current.Store(proxyServer.URL)
	resp, err = provider.Do(mustRequest(t, target.URL))
	if err != nil {
		t.Fatalf("proxied request: %v", err)
	}
	resp.Body.Close()
	if got := atomic.LoadInt32(&proxied); got != 1 {
		t.Fatalf("proxy hits = %d, want 1", got)
	}

	// Switching back must stop using the proxy immediately.
	current.Store("")
	resp, err = provider.Do(mustRequest(t, target.URL))
	if err != nil {
		t.Fatalf("second direct request: %v", err)
	}
	resp.Body.Close()
	if got := atomic.LoadInt32(&proxied); got != 1 {
		t.Fatalf("proxy hits after clearing = %d, want 1", got)
	}
}

// A malformed address must not break every outbound call.
func TestProviderFallsBackOnBadProxy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close()

	provider := NewProvider(func() string { return "ftp://nope:1" }, 0)
	resp, err := provider.Do(mustRequest(t, target.URL))
	if err != nil {
		t.Fatalf("request must fall back to direct: %v", err)
	}
	resp.Body.Close()
}

func TestParseProxyRejectsUnknownScheme(t *testing.T) {
	for _, raw := range []string{"ftp://127.0.0.1:1", "127.0.0.1:1080", "socks5://"} {
		if _, err := ParseProxy(raw); err == nil {
			t.Errorf("ParseProxy(%q) must fail", raw)
		}
	}
	for _, raw := range []string{"http://127.0.0.1:1080", "socks5h://user:pass@127.0.0.1:1080"} {
		if _, err := ParseProxy(raw); err != nil {
			t.Errorf("ParseProxy(%q) = %v", raw, err)
		}
	}
}

func mustRequest(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return req
}
