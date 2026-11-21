package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

// TST-075 / NFR-11: front-end events are accepted and reflected in the metrics
// snapshot used to judge G-01/G-02.
func TestTST075_EventMetricsEndpoint(t *testing.T) {
	r := newTestRouter(t, true)

	events := []map[string]any{
		{"name": "search_submit"},
		{"name": "search_first_render", "duration_ms": 1200, "success": true},
		{"name": "grab_confirm"},
		{"name": "grab_result", "success": true},
		{"name": "search_first_render", "duration_ms": 800, "success": true},
	}
	for _, event := range events {
		w := request(t, r, http.MethodPost, "/api/v1/search/events", event, true)
		if w.Code != http.StatusOK {
			t.Fatalf("event %v => %d %s", event["name"], w.Code, w.Body.String())
		}
	}

	w := request(t, r, http.MethodGet, "/api/v1/search/metrics", nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("metrics => %d", w.Code)
	}
	var payload struct {
		Data struct {
			SearchSubmit   int64 `json:"search_submit"`
			FirstRenderP95 int64 `json:"first_render_p95_ms"`
			GrabConfirmed  int64 `json:"grab_confirm"`
			GrabSucceeded  int64 `json:"grab_succeeded"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Data.SearchSubmit != 1 {
		t.Errorf("search_submit = %d", payload.Data.SearchSubmit)
	}
	if payload.Data.GrabConfirmed != 1 || payload.Data.GrabSucceeded != 1 {
		t.Errorf("grab counters = %+v", payload.Data)
	}
	if payload.Data.FirstRenderP95 < 800 {
		t.Errorf("p95 = %d, want >= 800", payload.Data.FirstRenderP95)
	}
}

// NFR-11: unknown events are rejected instead of polluting the registry.
func TestTST075_UnknownEventRejected(t *testing.T) {
	r := newTestRouter(t, true)
	w := request(t, r, http.MethodPost, "/api/v1/search/events", map[string]any{"name": "whatever"}, true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("40001")) {
		t.Errorf("body = %s", w.Body.String())
	}
}
