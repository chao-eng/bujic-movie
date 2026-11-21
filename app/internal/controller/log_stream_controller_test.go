package controller

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bujic-movie/bujic-movie/pkg/logger"
	"github.com/gin-gonic/gin"
)

// newTestContext builds a gin context whose writer is backed by an unbuffered
// pipe, so the test can read the stream while the handler is still running.
func newStreamServer(t *testing.T) (*LogStreamController, *httptest.Server) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctrl := &LogStreamController{
		subs:    map[uint64]chan LogEntry{},
		history: make([]LogEntry, 0, logHistorySize),
	}
	GlobalLogStream = ctrl
	logger.LogBroadcaster = ctrl.publish

	r := gin.New()
	r.GET("/stream", ctrl.Stream)
	r.GET("/recent", ctrl.Snapshot)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return ctrl, srv
}

// TST-401: the stream announces SSE framing (retry + ready) and replays the
// history window, each line carrying a monotonic id.
func TestLogStream_ReplaysHistoryWithIds(t *testing.T) {
	ctrl, srv := newStreamServer(t)
	ctrl.publish("INFO", "first")
	ctrl.publish("WARN", "second")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("content-type = %q", got)
	}
	// nginx 会缓冲流式响应，这条头必须有。
	if resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Errorf("X-Accel-Buffering = %q", resp.Header.Get("X-Accel-Buffering"))
	}

	reader := bufio.NewReader(resp.Body)
	body := readUntil(t, reader, `"message":"second"`)
	for _, want := range []string{"retry: 3000", "event: ready", "event: log", "id: 1", "id: 2"} {
		if !strings.Contains(body, want) {
			t.Errorf("stream missing %q, got:\n%s", want, body)
		}
	}
}

// TST-402: Last-Event-ID resumes after the given sequence, so a client that
// reconnects does not repeat or lose lines.
func TestLogStream_ResumesFromLastEventID(t *testing.T) {
	ctrl, srv := newStreamServer(t)
	ctrl.publish("INFO", "one")
	ctrl.publish("INFO", "two")
	ctrl.publish("INFO", "three")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream", nil)
	req.Header.Set("Last-Event-ID", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	body := readUntil(t, bufio.NewReader(resp.Body), `"message":"three"`)
	if strings.Contains(body, `"message":"one"`) {
		t.Errorf("resumed stream should not replay id 1:\n%s", body)
	}
	for _, want := range []string{`"message":"two"`, `"message":"three"`} {
		if !strings.Contains(body, want) {
			t.Errorf("resumed stream missing %q:\n%s", want, body)
		}
	}
}

// TST-403: new lines published while a client is attached are pushed live.
func TestLogStream_PushesLiveLines(t *testing.T) {
	ctrl, srv := newStreamServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	readUntil(t, reader, "event: ready")

	ctrl.publish("ERROR", "live-line")
	body := readUntil(t, reader, `"message":"live-line"`)
	if !strings.Contains(body, `"level":"ERROR"`) {
		t.Errorf("live line missing level:\n%s", body)
	}
}

// TST-404: /logs/recent returns the same replay window as JSON, so the page can
// paint before the stream handshake finishes.
func TestLogSnapshot_ReturnsWindow(t *testing.T) {
	ctrl, srv := newStreamServer(t)
	ctrl.publish("INFO", "hello")

	resp, err := http.Get(srv.URL + "/recent")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	var payload struct {
		Code int `json:"code"`
		Data struct {
			Items   []LogEntry `json:"items"`
			LastSeq uint64     `json:"last_seq"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Code != 0 || len(payload.Data.Items) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Data.Items[0].Message != "hello" || payload.Data.LastSeq != 1 {
		t.Errorf("unexpected entry: %+v", payload.Data.Items[0])
	}
}

// TST-405: a slow client must never block the logging path. publish drops into
// the buffer instead of waiting.
func TestLogStream_SlowClientDoesNotBlock(t *testing.T) {
	ctrl, _ := newStreamServer(t)
	subID, ch := ctrl.subscribe()
	defer ctrl.unsubscribe(subID)

	done := make(chan struct{})
	go func() {
		// 远超缓冲区容量，如果 publish 会阻塞这里就永远不结束。
		for i := 0; i < logSubBuffer*4; i++ {
			ctrl.publish("INFO", "flood")
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publish blocked on a slow subscriber")
	}
	if len(ch) > logSubBuffer {
		t.Errorf("buffer overflowed: %d", len(ch))
	}
}

// TST-406: the replay window is bounded, so a long-running server does not grow
// memory without limit.
func TestLogStream_HistoryIsBounded(t *testing.T) {
	ctrl, _ := newStreamServer(t)
	for i := 0; i < logHistorySize+50; i++ {
		ctrl.publish("INFO", "line")
	}
	items := ctrl.since(0)
	if len(items) != logHistorySize {
		t.Errorf("history length = %d, want %d", len(items), logHistorySize)
	}
	// 序号是全局单调的，不受窗口裁剪影响。
	if last := items[len(items)-1].Seq; last != uint64(logHistorySize+50) {
		t.Errorf("newest seq = %d", last)
	}
}

// readUntil reads the stream until `want` appears or the deadline hits. It
// returns everything read so far so failures show the actual framing.
func readUntil(t *testing.T, reader *bufio.Reader, want string) string {
	t.Helper()
	var sb strings.Builder
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if line != "" {
			sb.WriteString(line)
			if strings.Contains(sb.String(), want) {
				return sb.String()
			}
		}
		if err != nil {
			break
		}
	}
	t.Fatalf("timed out waiting for %q, got:\n%s", want, sb.String())
	return ""
}

// TST-407: a client whose Last-Event-ID is ahead of the server (it restarted and
// the sequence began again) must still receive the window, not an empty stream.
func TestLogStream_StaleLastEventIDReplaysWindow(t *testing.T) {
	ctrl, srv := newStreamServer(t)
	ctrl.publish("INFO", "fresh-after-restart")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream", nil)
	// 客户端以为已经收到第 120 行，服务端才刚重启到第 1 行。
	req.Header.Set("Last-Event-ID", "120")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	body := readUntil(t, bufio.NewReader(resp.Body), `"message":"fresh-after-restart"`)
	if !strings.Contains(body, "id: 1") {
		t.Errorf("stale client should get the window, got:\n%s", body)
	}
}
