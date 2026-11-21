package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/bujic-movie/bujic-movie/pkg/logger"
	"github.com/gin-gonic/gin"
)

// LogEntry is one log line as pushed to browsers. Seq is a monotonic id so a
// client that reconnects can ask for only what it missed (Last-Event-ID).
type LogEntry struct {
	Seq       uint64 `json:"seq"`
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

// LogStreamController fans log lines out to browsers over Server-Sent Events.
//
// SSE rather than WebSocket: the stream is one-way, and EventSource semantics
// (event ids + Last-Event-ID) give resumption for free — a reconnecting client
// gets exactly the lines it missed instead of silently losing them.
type LogStreamController struct {
	mu   sync.RWMutex
	subs map[uint64]chan LogEntry
	next uint64

	historyMu sync.Mutex
	history   []LogEntry
	seq       uint64
}

// 历史窗口：重连时最多补发这么多行，避免新打开的页面空空如也。
const (
	logHistorySize    = 200
	logSubBuffer      = 256
	logHeartbeatEvery = 15 * time.Second
)

var GlobalLogStream *LogStreamController

// NewLogStreamController wires the controller and installs itself as the log
// broadcaster, replacing the old WebSocket fan-out.
func NewLogStreamController() *LogStreamController {
	ctrl := &LogStreamController{
		subs:    make(map[uint64]chan LogEntry),
		history: make([]LogEntry, 0, logHistorySize),
	}
	GlobalLogStream = ctrl

	logger.LogBroadcaster = func(level, message string) {
		ctrl.publish(level, message)
	}
	return ctrl
}

// publish appends to the replay window and fans out non-blockingly, so a slow
// browser can never block the logging path.
func (ctrl *LogStreamController) publish(level, message string) {
	entry := LogEntry{
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
		Level:     level,
		Message:   message,
	}

	ctrl.historyMu.Lock()
	ctrl.seq++
	entry.Seq = ctrl.seq
	if len(ctrl.history) == logHistorySize {
		ctrl.history = ctrl.history[1:]
	}
	ctrl.history = append(ctrl.history, entry)
	ctrl.historyMu.Unlock()

	ctrl.mu.RLock()
	defer ctrl.mu.RUnlock()
	for _, ch := range ctrl.subs {
		select {
		case ch <- entry:
		default:
			// 这个客户端已经积压太多（比如标签页被挂起），丢弃本行而不是阻塞。
		}
	}
}

// since returns entries with Seq > after, capped to the replay window.
func (ctrl *LogStreamController) since(after uint64) []LogEntry {
	ctrl.historyMu.Lock()
	defer ctrl.historyMu.Unlock()
	out := make([]LogEntry, 0, len(ctrl.history))
	for _, entry := range ctrl.history {
		if entry.Seq > after {
			out = append(out, entry)
		}
	}
	return out
}

// lastSeq reports the newest sequence number, used as the initial event id.
func (ctrl *LogStreamController) lastSeq() uint64 {
	ctrl.historyMu.Lock()
	defer ctrl.historyMu.Unlock()
	return ctrl.seq
}

func (ctrl *LogStreamController) subscribe() (uint64, chan LogEntry) {
	ch := make(chan LogEntry, logSubBuffer)
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	ctrl.next++
	id := ctrl.next
	ctrl.subs[id] = ch
	return id, ch
}

func (ctrl *LogStreamController) unsubscribe(id uint64) {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	delete(ctrl.subs, id)
}

// Stream serves the log stream (API-27).
//
// Protocol: every message is an SSE event named "log" carrying a LogEntry.
// A named "heartbeat" event is sent every 15s so proxies keep the connection
// open and the browser can tell a live stream from a dead one.
func (ctrl *LogStreamController) Stream(c *gin.Context) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.String(http.StatusInternalServerError, "当前服务器不支持流式响应")
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	// nginx 默认会缓冲响应，日志就变成一批一批地冒出来。
	c.Header("X-Accel-Buffering", "no")

	// 重连带上的 Last-Event-ID 决定从哪一行开始补发；首次连接给最近的历史。
	var after uint64
	if raw := c.GetHeader("Last-Event-ID"); raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 64); err == nil {
			after = parsed
		}
	}
	if last := ctrl.lastSeq(); after > last {
		// 客户端记着的序号比服务端最大序号还大：服务端重启过（序号从 1 重来），
		// 客户端状态已陈旧。此时按 after 过滤会一条都发不出来，应补发整个窗口。
		after = 0
	} else if after == 0 {
		if last > logHistorySize {
			after = last - logHistorySize
		}
	}

	subID, ch := ctrl.subscribe()
	defer ctrl.unsubscribe(subID)

	// 先告诉客户端断线后隔多久重连，以及后面发的 id 从哪开始。
	if _, err := fmt.Fprintf(c.Writer, "retry: 3000\nevent: ready\ndata: {\"last_seq\":%d}\n\n", ctrl.lastSeq()); err != nil {
		return
	}

	for _, entry := range ctrl.since(after) {
		if err := writeLogEvent(c, entry); err != nil {
			return
		}
	}
	flusher.Flush()

	ctx := c.Request.Context()
	heartbeat := time.NewTicker(logHeartbeatEvery)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			// 浏览器关掉页面、切走标签页，或网络断开。
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(c.Writer, "event: heartbeat\ndata: {\"time\":\"%s\"}\n\n", time.Now().Format(time.RFC3339)); err != nil {
				return
			}
			flusher.Flush()
		case entry := <-ch:
			if err := writeLogEvent(c, entry); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeLogEvent(c *gin.Context, entry LogEntry) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return nil // 单行编码失败不该终止整个流
	}
	// id 必须写在 data 之前，浏览器才会把它记成 Last-Event-ID。
	_, err = fmt.Fprintf(c.Writer, "id: %d\nevent: log\ndata: %s\n\n", entry.Seq, payload)
	return err
}

// Snapshot returns the replay window as JSON. Lets the page paint instantly
// without waiting for the SSE handshake, and is trivially testable.
func (ctrl *LogStreamController) Snapshot(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": gin.H{
			"items":    ctrl.since(0),
			"last_seq": ctrl.lastSeq(),
		},
	})
}
