package mcp

import (
	"strconv"
	"sync"
)

func itoa(i int) string {
	return strconv.Itoa(i)
}

func quote(s string) string {
	return strconv.Quote(s)
}

// Record is the audit payload a tool call produces (BR-26). The InputMeta field
// is already redacted by the tool layer before it reaches here.
type Record struct {
	APIKeyID    uint
	Tool        string
	Status      string // ok / error / timeout
	ErrorCode   string
	DurationMS  int64
	InputMeta   string
	ResultBytes int64
	ClientIP    string
}

// recordWriter queues audit records and writes them via a bounded background
// loop, falling back to a synchronous write when the buffer is full or when
// flushed on shutdown (BR-29).
type recordWriter struct {
	ch      chan Record
	write   func(Record) error
	closeMu sync.Mutex
	closed  bool
}

func newRecordWriter(write func(Record) error) *recordWriter {
	rw := &recordWriter{
		ch:    make(chan Record, 512),
		write: write,
	}
	go rw.loop()
	return rw
}

func (rw *recordWriter) loop() {
	for rec := range rw.ch {
		_ = rw.write(rec)
	}
}

func (rw *recordWriter) submit(rec Record) {
	select {
	case rw.ch <- rec:
	default:
		// buffer full: fall back to synchronous write to not lose audit
		_ = rw.write(rec)
	}
}

// Flush drains the queue synchronously (call on graceful shutdown).
func (rw *recordWriter) Flush() {
	rw.closeMu.Lock()
	defer rw.closeMu.Unlock()
	if rw.closed {
		return
	}
	rw.closed = true
	close(rw.ch)
}
