package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNewUnsupported(t *testing.T) {
	if _, err := New("telegram", nil); err == nil {
		t.Error("expected error for unsupported type")
	}
	for _, ct := range Types() {
		if _, err := New(ct.Type, map[string]string{}); err != nil {
			t.Errorf("New(%s): %v", ct.Type, err)
		}
	}
}

func TestBarkSend(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"code":200,"message":"success"}`))
	}))
	defer srv.Close()

	ch, _ := New("bark", map[string]string{"server_url": srv.URL, "push_key": "devkey", "group": "Movie"})
	if err := ch.Send(context.Background(), Message{Title: "片名", Text: "已入库"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotPath != "/devkey" {
		t.Errorf("path = %q, want /devkey", gotPath)
	}
	if !strings.Contains(gotBody, "片名") || !strings.Contains(gotBody, "Movie") {
		t.Errorf("body missing fields: %s", gotBody)
	}
}

func TestGotifySendSuccessNoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/message" || r.URL.Query().Get("token") != "tok" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"id":1,"title":"x"}`))
	}))
	defer srv.Close()

	ch, _ := New("gotify", map[string]string{"server_url": srv.URL, "token": "tok"})
	if err := ch.Send(context.Background(), Message{Title: "t", Text: "m"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestNtfySendHeaders(t *testing.T) {
	var gotTitle, gotAuth, gotBody, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTitle = r.Header.Get("X-Title")
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	}))
	defer srv.Close()

	ch, _ := New("ntfy", map[string]string{"server_url": srv.URL, "topic": "mp", "token": "tk_1"})
	if err := ch.Send(context.Background(), Message{Title: "标题", Text: "正文"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotPath != "/mp" || gotTitle != "标题" || gotAuth != "Bearer tk_1" || gotBody != "正文" {
		t.Errorf("unexpected: path=%q title=%q auth=%q body=%q", gotPath, gotTitle, gotAuth, gotBody)
	}
}

func TestWebhookTemplate(t *testing.T) {
	var gotBody, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch, _ := New("webhook", map[string]string{
		"url":          srv.URL + "/hook",
		"method":       "POST",
		"content_type": "application/json",
		"body":         `{"t":"{title}","x":"{text}"}`,
	})
	if err := ch.Send(context.Background(), Message{Title: "标题A", Text: "正文B"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	if gotBody != `{"t":"标题A","x":"正文B"}` {
		t.Errorf("rendered body = %q", gotBody)
	}
}

func TestRenderTemplate(t *testing.T) {
	out := renderTemplate("{title}-{text}", Message{Title: "A", Text: "B"})
	if out != "A-B" {
		t.Errorf("renderTemplate = %q, want A-B", out)
	}
}

func TestServerChan3Send(t *testing.T) {
	var gotMethod, gotPath, gotCT, gotRaw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotRaw = string(b)
		_, _ = w.Write([]byte(`{"code":0,"message":"success"}`))
	}))
	defer srv.Close()

	// serverChan3 在源码里写死了 https://<uid>.push.ft07.com 域名，没法用 httptest 直接打桩。
	// 我们通过一个本地回环的轻量替身：把 serverChan3 的 base.http 替换成 transport，
	// 把任意 URL 重定向到 httptest.Server，从而端到端校验路径、Content-Type、URL 编码。
	origHTTP := (&serverChan3{}).base // 仅用于触发类型断言；用真 channel 拿到 base 即可
	_ = origHTTP
	ch, err := New("serverchan3", map[string]string{
		"uid":      "u123",
		"send_key": "k456",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sc3, ok := ch.(*serverChan3)
	if !ok {
		t.Fatalf("expected *serverChan3, got %T", ch)
	}
	// 替换 HTTP client，让请求无论打哪个 host，都落到本地 srv 上
	sc3.base.http = &http.Client{
		Timeout: 5 * time.Second,
		Transport: rewriteHostTransport{target: srv.URL},
	}
	if err := sc3.Send(context.Background(), Message{Title: "标题", Text: "正文"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/send/k456.send" {
		t.Errorf("path = %q, want /send/k456.send", gotPath)
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Errorf("content-type = %q", gotCT)
	}
	// form 编码后中文标题应为 title=%E6%A0%87%E9%A2%98 这种形式
	if !strings.Contains(gotRaw, "title=%E6%A0%87%E9%A2%98") {
		t.Errorf("form body missing urlencoded title: %s", gotRaw)
	}
	if !strings.Contains(gotRaw, "desp=%E6%AD%A3%E6%96%87") {
		t.Errorf("form body missing urlencoded desp: %s", gotRaw)
	}
}

func TestServerChan3MissingFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  map[string]string
		want string
	}{
		{"missing uid", map[string]string{"send_key": "k"}, "缺少 UID 或 SendKey"},
		{"missing send_key", map[string]string{"uid": "u"}, "缺少 UID 或 SendKey"},
		{"empty both", map[string]string{}, "缺少 UID 或 SendKey"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ch, _ := New("serverchan3", tc.cfg)
			err := ch.Send(context.Background(), Message{Title: "t", Text: "x"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want contains %q", err, tc.want)
			}
		})
	}
}

// rewriteHostTransport 是一个 http.RoundTripper：把所有请求的 scheme+host 改写到 target，
// path/query/body 保持不变，从而让 hard-coded endpoint 也能打到 httptest server。
type rewriteHostTransport struct {
	target string
}

func (r rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(r.target)
	if err != nil {
		return nil, err
	}
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = u.Scheme
	req2.URL.Host = u.Host
	return http.DefaultTransport.RoundTrip(req2)
}
