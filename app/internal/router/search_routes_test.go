package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newTestRouter(t *testing.T, searchEnabled bool) *gin.Engine {
	t.Helper()
	r, _ := newTestRouterWithDB(t, searchEnabled, 0)
	return r
}

// newTestRouterWithDB also returns the database so tests can simulate storage
// failures; siteTimeoutMS overrides the per-site budget when non-zero.
func newTestRouterWithDB(t *testing.T, searchEnabled bool, siteTimeoutMS int) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	path := filepath.Join(t.TempDir(), "router.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	cfg := &config.Config{}
	cfg.Server.SecretKey = "router-test-secret"
	cfg.Server.Username = "admin"
	cfg.Server.Password = "admin123"
	cfg.Search.Enabled = searchEnabled
	cfg.Search.DefaultIntervalMS = 200
	cfg.Search.MaxGlobalSearches = 3
	cfg.Search.MaxSiteWorkers = 20
	cfg.Search.SearchDeadlineMS = 15000
	cfg.Search.SiteTimeoutMS = 8000
	if siteTimeoutMS > 0 {
		cfg.Search.SiteTimeoutMS = siteTimeoutMS
	}
	cfg.Search.GrabWindowMS = 60000
	cfg.Search.RecordRetentionDays = 90
	cfg.Search.HistoryLimit = 20
	return SetupRouter(db, cfg), db
}

func token(t *testing.T) string {
	t.Helper()
	tok, err := middleware.GenerateToken("admin")
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	return tok
}

func request(t *testing.T, r *gin.Engine, method, path string, body any, withAuth bool) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if withAuth {
		req.Header.Set("Authorization", "Bearer "+token(t))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TST-061 / BR-18: every module endpoint requires a JWT.
func TestTST061_AllEndpointsRequireAuth(t *testing.T) {
	r := newTestRouter(t, true)

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/search/definitions"},
		{"POST", "/api/v1/search/definitions/sync"},
		{"POST", "/api/v1/search/definitions/import"},
		{"GET", "/api/v1/search/definitions/x"},
		{"DELETE", "/api/v1/search/definitions/x"},
		{"GET", "/api/v1/search/indexers"},
		{"POST", "/api/v1/search/indexers"},
		{"GET", "/api/v1/search/indexers/1"},
		{"PUT", "/api/v1/search/indexers/1"},
		{"DELETE", "/api/v1/search/indexers/1"},
		{"POST", "/api/v1/search/indexers/1/test"},
		{"POST", "/api/v1/search/indexers/1/captcha"},
		{"GET", "/api/v1/search/download-clients"},
		{"POST", "/api/v1/search/download-clients"},
		{"GET", "/api/v1/search/download-clients/1"},
		{"PUT", "/api/v1/search/download-clients/1"},
		{"DELETE", "/api/v1/search/download-clients/1"},
		{"POST", "/api/v1/search/download-clients/1/test"},
		{"POST", "/api/v1/search/query"},
		{"POST", "/api/v1/search/grab/preview"},
		{"POST", "/api/v1/search/grab"},
		{"GET", "/api/v1/search/tasks"},
		{"POST", "/api/v1/search/tasks/1/hash/action"},
		{"GET", "/api/v1/search/records"},
		{"GET", "/api/v1/search/history"},
		{"DELETE", "/api/v1/search/history"},
		{"GET", "/api/v1/search/health"},
		{"POST", "/api/v1/search/events"},
		{"GET", "/api/v1/search/metrics"},
	}
	for _, e := range endpoints {
		w := request(t, r, e.method, e.path, nil, false)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s => %d, want 401", e.method, e.path, w.Code)
		}
	}
	// /status is also protected, it just reports the switch state.
	if w := request(t, r, http.MethodGet, "/api/v1/search/status", nil, false); w.Code != http.StatusUnauthorized {
		t.Errorf("status without auth => %d, want 401", w.Code)
	}
}

// TST-056 / BR-40: disabling the module keeps /status working and returns JSON
// 404 for everything else, so the SPA fallback never swallows API calls.
func TestTST056_ModuleDisabled(t *testing.T) {
	r := newTestRouter(t, false)

	w := request(t, r, http.MethodGet, "/api/v1/search/status", nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status => %d", w.Code)
	}
	var payload struct {
		Data struct {
			Enabled bool `json:"enabled"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &payload)
	if payload.Data.Enabled {
		t.Error("status must report enabled=false")
	}

	w = request(t, r, http.MethodGet, "/api/v1/search/indexers", nil, true)
	if w.Code != http.StatusNotFound {
		t.Fatalf("indexers while disabled => %d, want 404", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("未启用")) {
		t.Errorf("disabled body = %s", w.Body.String())
	}
}

// TST-061: a valid token reaches the handler.
func TestTST061_AuthorizedStatus(t *testing.T) {
	r := newTestRouter(t, true)
	w := request(t, r, http.MethodGet, "/api/v1/search/status", nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status => %d body %s", w.Code, w.Body.String())
	}
}

// TST-001 / AC-01: the definition import endpoint stores a YAML upload.
func TestTST001_ImportDefinitionEndpoint(t *testing.T) {
	r := newTestRouter(t, true)
	body := map[string]string{
		"file_name": "fake.yml",
		"content": `id: endpoint-fake
name: 端点测试站点
type: public
links:
  - https://endpoint.example/
caps:
  modes:
    search: [q]
search:
  path: /s
  rows:
    selector: li
  fields:
    title:
      selector: a
    download:
      selector: a
      attribute: href
`,
	}
	w := request(t, r, http.MethodPost, "/api/v1/search/definitions/import", body, true)
	if w.Code != http.StatusOK {
		t.Fatalf("import => %d body %s", w.Code, w.Body.String())
	}

	w = request(t, r, http.MethodGet, "/api/v1/search/definitions", nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("list => %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("端点测试站点")) {
		t.Errorf("definition not stored: %s", w.Body.String())
	}
}

// TST-003 / AC-03: an invalid YAML is rejected with 40002 and a location.
func TestTST003_InvalidImportRejected(t *testing.T) {
	r := newTestRouter(t, true)
	body := map[string]string{"file_name": "bad.yml", "content": "id: bad\nname: bad\n"}
	w := request(t, r, http.MethodPost, "/api/v1/search/definitions/import", body, true)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d body %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("40002")) {
		t.Errorf("body must carry code 40002: %s", w.Body.String())
	}
}

// TST-060 / BR-18: searching with no indexers answers 409, not an empty list.
func TestTST060_QueryWithoutIndexers(t *testing.T) {
	r := newTestRouter(t, true)
	w := request(t, r, http.MethodPost, "/api/v1/search/query", map[string]any{"keyword": "x"}, true)
	if w.Code != http.StatusConflict {
		t.Fatalf("code = %d body %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("没有可用")) {
		t.Errorf("message = %s", w.Body.String())
	}
}

// TST-019 / AC-19: a minimal download client can be created and never returns
// its password.
func TestTST019_CreateClientMasks(t *testing.T) {
	r := newTestRouter(t, true)
	body := map[string]any{
		"name": "NAS", "host": "127.0.0.1", "port": 8080,
		"username": "admin", "password": "supersecret",
	}
	w := request(t, r, http.MethodPost, "/api/v1/search/download-clients", body, true)
	if w.Code != http.StatusOK {
		t.Fatalf("create => %d body %s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("supersecret")) {
		t.Fatalf("response leaked the password: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("****")) {
		t.Errorf("response must include a mask: %s", w.Body.String())
	}
}

// TST-053 / BR-30: duplicate category mappings are rejected by the API.
func TestTST053_DuplicateMappingRejectedByAPI(t *testing.T) {
	r := newTestRouter(t, true)
	body := map[string]any{
		"name": "NAS", "host": "127.0.0.1", "port": 8080,
		"categories": []map[string]any{
			{"source_category": 5000, "client_category": "tv"},
			{"source_category": 5000, "client_category": "anime"},
		},
	}
	w := request(t, r, http.MethodPost, "/api/v1/search/download-clients", body, true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d body %s", w.Code, w.Body.String())
	}
}

// TST-013 / AC-13: a captcha requirement surfaces as 42201.
func TestTST013_CaptchaErrorCode(t *testing.T) {
	r := newTestRouter(t, true)
	importBody := map[string]string{
		"file_name": "captcha.yml",
		"content": `id: captcha-site
name: 验证码站点
type: private
links:
  - http://127.0.0.1:1/
caps:
  modes:
    search: [q]
login:
  path: /login
  method: form
  captcha:
    type: image
    selector: img.captcha
  inputs:
    username: "{{ .Config.username }}"
search:
  path: /s
  rows:
    selector: li
  fields:
    title:
      selector: a
    download:
      selector: a
      attribute: href
`,
	}
	if w := request(t, r, http.MethodPost, "/api/v1/search/definitions/import", importBody, true); w.Code != http.StatusOK {
		t.Fatalf("import => %d body %s", w.Code, w.Body.String())
	}

	createBody := map[string]any{
		"name": "验证码站点", "connector": "cardigann", "definition_id": "captcha-site",
		"secrets": map[string]string{"username": "u"},
	}
	w := request(t, r, http.MethodPost, "/api/v1/search/indexers", createBody, true)
	if w.Code != http.StatusOK {
		t.Fatalf("create => %d body %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	w = request(t, r, http.MethodPost, "/api/v1/search/indexers/"+itoa(int(created.Data.ID))+"/test", nil, true)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("test => %d body %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("42201")) {
		t.Errorf("expected captcha code 42201: %s", w.Body.String())
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	out := ""
	for v > 0 {
		out = string(rune('0'+v%10)) + out
		v /= 10
	}
	return out
}
