// Package netproxy builds HTTP clients that follow a proxy address which can
// change at runtime. It exists so packages without access to the search module
// (TMDB scraping, image downloads) obey the same network settings.
package netproxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	xproxy "golang.org/x/net/proxy"
)

// DefaultTimeout is used when a provider is created without one.
const DefaultTimeout = 10 * time.Second

// ParseProxy validates a proxy URL and rejects unsupported schemes. It never
// returns the raw address in the error text: proxy URLs often carry credentials
// and this message can end up in logs.
func ParseProxy(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
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

// Transport builds a transport that routes through the given proxy. An empty
// address yields a direct transport.
func Transport(raw string) (*http.Transport, error) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	raw = strings.TrimSpace(raw)
	if raw == "" {
		base.Proxy = nil
		return base, nil
	}
	proxyURL, err := ParseProxy(raw)
	if err != nil {
		return nil, err
	}
	switch proxyURL.Scheme {
	case "http", "https":
		base.Proxy = http.ProxyURL(proxyURL)
	case "socks5", "socks5h":
		dialer, err := xproxy.SOCKS5("tcp", proxyURL.Host, socksAuth(proxyURL), xproxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("SOCKS5 代理不可用: %w", err)
		}
		base.Proxy = nil
		base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			type ctxDialer interface {
				DialContext(ctx context.Context, network, addr string) (net.Conn, error)
			}
			if cd, ok := dialer.(ctxDialer); ok {
				return cd.DialContext(ctx, network, addr)
			}
			return dialer.Dial(network, addr)
		}
	}
	return base, nil
}

func socksAuth(u *url.URL) *xproxy.Auth {
	if u.User == nil {
		return nil
	}
	pass, _ := u.User.Password()
	return &xproxy.Auth{User: u.User.Username(), Password: pass}
}

// Provider hands out a client for the proxy currently configured. It rebuilds
// the transport only when the address actually changes, so a settings update
// applies without a restart.
type Provider struct {
	mu       sync.Mutex
	resolver func() string
	timeout  time.Duration
	raw      string
	client   *http.Client
}

// NewProvider builds a provider whose proxy comes from resolver on every
// request. A nil resolver means direct connections.
func NewProvider(resolver func() string, timeout time.Duration) *Provider {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Provider{resolver: resolver, timeout: timeout}
}

// SetTimeout changes the per-request timeout for future clients.
func (p *Provider) SetTimeout(d time.Duration) {
	if d <= 0 {
		return
	}
	p.mu.Lock()
	p.timeout = d
	p.client = nil
	p.mu.Unlock()
}

// CurrentProxy reports the resolved address, for diagnostics. Credentials are
// stripped.
func (p *Provider) CurrentProxy() string {
	raw := p.current()
	if raw == "" {
		return ""
	}
	u, err := ParseProxy(raw)
	if err != nil {
		return "配置无效"
	}
	u.User = nil
	return u.String()
}

func (p *Provider) current() string {
	if p.resolver == nil {
		return ""
	}
	return strings.TrimSpace(p.resolver())
}

// Client returns a client for the current proxy.
func (p *Provider) Client() (*http.Client, error) {
	raw := p.current()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client != nil && p.raw == raw {
		return p.client, nil
	}
	transport, err := Transport(raw)
	if err != nil {
		if raw != "" {
			// Fall back to a direct transport rather than refusing every call
			// because of one malformed setting; the error is surfacing through
			// the test endpoint.
			transport, err = Transport("")
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	p.client = &http.Client{Transport: transport, Timeout: p.timeout}
	p.raw = raw
	return p.client, nil
}

// Do performs the request with the client for the current proxy.
func (p *Provider) Do(req *http.Request) (*http.Response, error) {
	client, err := p.Client()
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}
