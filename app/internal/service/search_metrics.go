package service

import (
	"sort"
	"sync"
	"time"
)

// SearchMetrics keeps in-process counters for the four front-end events and for
// server-side search latency. It is intentionally small: a single-user
// self-hosted build does not need an external metrics backend.
type SearchMetrics struct {
	mu sync.Mutex

	submitCount   int64
	renderSamples []int64 // first-render latency, last 200
	grabConfirmed int64
	grabSucceeded int64
	grabFailed    int64
	grabDuplicate int64

	searchSamples  []int64 // server search elapsed, last 200
	searchFailures int64
	searchTotal    int64
	lastEventAt    time.Time
}

// NewSearchMetrics builds an empty registry.
func NewSearchMetrics() *SearchMetrics { return &SearchMetrics{} }

// RecordEvent accepts one front-end event. Unknown names are ignored so a stale
// page cannot pollute the registry.
func (m *SearchMetrics) RecordEvent(name string, durationMS int64, success bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastEventAt = time.Now()
	switch name {
	case "search_submit":
		m.submitCount++
	case "search_first_render":
		m.renderSamples = appendSample(m.renderSamples, durationMS)
	case "grab_confirm":
		m.grabConfirmed++
	case "grab_result":
		switch {
		case success:
			m.grabSucceeded++
		default:
			m.grabFailed++
		}
	}
}

// RecordSearch records one server-side search outcome.
func (m *SearchMetrics) RecordSearch(elapsed time.Duration, failed bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.searchTotal++
	if failed {
		m.searchFailures++
	}
	m.searchSamples = appendSample(m.searchSamples, elapsed.Milliseconds())
}

// RecordDuplicateGrab counts a deduped grab.
func (m *SearchMetrics) RecordDuplicateGrab() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.grabDuplicate++
}

// MetricsSnapshot is the JSON shape returned by the metrics endpoint.
type MetricsSnapshot struct {
	SearchSubmit   int64  `json:"search_submit"`
	FirstRenderP50 int64  `json:"first_render_p50_ms"`
	FirstRenderP95 int64  `json:"first_render_p95_ms"`
	SearchP50      int64  `json:"search_p50_ms"`
	SearchP95      int64  `json:"search_p95_ms"`
	SearchTotal    int64  `json:"search_total"`
	SearchFailures int64  `json:"search_failures"`
	GrabConfirmed  int64  `json:"grab_confirm"`
	GrabSucceeded  int64  `json:"grab_succeeded"`
	GrabFailed     int64  `json:"grab_failed"`
	GrabDuplicate  int64  `json:"grab_duplicate"`
	LastEventAt    string `json:"last_event_at"`
}

// Snapshot returns a copy of the current counters.
func (m *SearchMetrics) Snapshot() MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	snap := MetricsSnapshot{
		SearchSubmit:   m.submitCount,
		FirstRenderP50: percentile(m.renderSamples, 0.50),
		FirstRenderP95: percentile(m.renderSamples, 0.95),
		SearchP50:      percentile(m.searchSamples, 0.50),
		SearchP95:      percentile(m.searchSamples, 0.95),
		SearchTotal:    m.searchTotal,
		SearchFailures: m.searchFailures,
		GrabConfirmed:  m.grabConfirmed,
		GrabSucceeded:  m.grabSucceeded,
		GrabFailed:     m.grabFailed,
		GrabDuplicate:  m.grabDuplicate,
	}
	if !m.lastEventAt.IsZero() {
		snap.LastEventAt = m.lastEventAt.Format(time.RFC3339)
	}
	return snap
}

func appendSample(samples []int64, value int64) []int64 {
	if value < 0 {
		value = 0
	}
	samples = append(samples, value)
	if len(samples) > 200 {
		samples = samples[len(samples)-200:]
	}
	return samples
}

func percentile(samples []int64, p float64) int64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]int64(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(float64(len(sorted)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
