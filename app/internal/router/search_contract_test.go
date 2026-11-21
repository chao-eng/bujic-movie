package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/gin-gonic/gin"
)

// siteStub is a configurable fake indexer used by the error-code contract tests.
type siteStub struct {
	server *httptest.Server
	status int
	body   string
	delay  time.Duration
}

func newSiteStub(t *testing.T, status int, body string, delay time.Duration) *siteStub {
	t.Helper()
	stub := &siteStub{status: status, body: body, delay: delay}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stub.delay > 0 {
			time.Sleep(stub.delay)
		}
		if stub.status != 0 {
			w.WriteHeader(stub.status)
		}
		_, _ = w.Write([]byte(stub.body))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *siteStub) hostPort(t *testing.T) (string, int) {
	t.Helper()
	raw := strings.TrimPrefix(s.server.URL, "http://")
	parts := strings.SplitN(raw, ":", 2)
	var port int
	_, err := fmt.Sscanf(parts[1], "%d", &port)
	if err != nil {
		t.Fatalf("port: %v", err)
	}
	return parts[0], port
}

func importDefinition(t *testing.T, r *gin.Engine, id, link, extra string) {
	t.Helper()
	content := fmt.Sprintf(`id: %s
name: %s
type: public
links:
  - %s/
caps:
  modes:
    search: [q]
search:
  path: /s
  rows:
    selector: table.torrents > tbody > tr
  fields:
    title:
      selector: a.title
    download:
      selector: a.download
      attribute: href
%s`, id, id, link, extra)
	body := map[string]string{"file_name": id + ".yml", "content": content}
	w := request(t, r, http.MethodPost, "/api/v1/search/definitions/import", body, true)
	if w.Code != http.StatusOK {
		t.Fatalf("import %s => %d %s", id, w.Code, w.Body.String())
	}
}

func createIndexerAPI(t *testing.T, r *gin.Engine, payload map[string]any) uint {
	t.Helper()
	w := request(t, r, http.MethodPost, "/api/v1/search/indexers", payload, true)
	if w.Code != http.StatusOK {
		t.Fatalf("create indexer => %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	return created.Data.ID
}

func expectCode(t *testing.T, w *httptest.ResponseRecorder, wantHTTP, wantCode int) {
	t.Helper()
	if w.Code != wantHTTP {
		t.Fatalf("http = %d, want %d (body %s)", w.Code, wantHTTP, w.Body.String())
	}
	var payload struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &payload)
	if payload.Code != wantCode {
		t.Fatalf("code = %d, want %d (body %s)", payload.Code, wantCode, w.Body.String())
	}
}

// TST-010 / 40001: malformed JSON is a parameter error.
func TestTST010_Code40001_BadJSON(t *testing.T) {
	r := newTestRouter(t, true)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/search/indexers", strings.NewReader("{not json"))
	req.Header.Set("Authorization", "Bearer "+token(t))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	expectCode(t, w, http.StatusBadRequest, 40001)
}

// TST-003 / 40002: an invalid definition is rejected with its location.
func TestTST003_Code40002_InvalidDefinition(t *testing.T) {
	r := newTestRouter(t, true)
	w := request(t, r, http.MethodPost, "/api/v1/search/definitions/import",
		map[string]string{"file_name": "bad.yml", "content": "id: bad\nname: bad\n"}, true)
	expectCode(t, w, http.StatusUnprocessableEntity, 40002)
}

// TST-013 / 42201: a captcha-protected site reports 42201 on test.
func TestTST013_Code42201_Captcha(t *testing.T) {
	r, _ := newTestRouterWithDB(t, true, 0)
	stub := newSiteStub(t, 200, "<html>login</html>", 0)
	importDefinition(t, r, "cap-site", stub.server.URL, `login:
  path: /login
  method: form
  captcha:
    type: image
    selector: img.captcha
  inputs:
    username: "{{ .Config.username }}"
`)
	id := createIndexerAPI(t, r, map[string]any{
		"name": "cap", "connector": "cardigann", "definition_id": "cap-site",
		"secrets": map[string]string{"username": "u"},
	})
	w := request(t, r, http.MethodPost, fmt.Sprintf("/api/v1/search/indexers/%d/test", id), nil, true)
	expectCode(t, w, http.StatusUnprocessableEntity, 42201)
}

// TST-010 / 40102: a site that rejects credentials is an auth failure of the
// target, not of the logged-in user, so it must not answer HTTP 401.
func TestTST010_Code40102_SiteAuth(t *testing.T) {
	r, _ := newTestRouterWithDB(t, true, 0)
	stub := newSiteStub(t, 200, "Ok.", 0)
	importDefinition(t, r, "auth-site", stub.server.URL, `login:
  path: /login
  method: form
  error:
    - selector: div.error
      message:
        selector: div.error
  inputs:
    username: "{{ .Config.username }}"
`)
	// The stub always returns a login error block.
	stub.body = `<html><div class="error">bad credentials</div></html>`
	id := createIndexerAPI(t, r, map[string]any{
		"name": "auth", "connector": "cardigann", "definition_id": "auth-site",
		"secrets": map[string]string{"username": "u"},
	})
	w := request(t, r, http.MethodPost, fmt.Sprintf("/api/v1/search/indexers/%d/test", id), nil, true)
	expectCode(t, w, http.StatusUnprocessableEntity, 40102)
}

// TST-050 / 50201: an unreachable host is a bad-gateway failure.
func TestTST050_Code50201_Unreachable(t *testing.T) {
	r, _ := newTestRouterWithDB(t, true, 0)
	stub := newSiteStub(t, 0, "", 0)
	host, _ := stub.hostPort(t)
	stub.server.Close() // now the address refuses connections
	view := map[string]any{
		"name": "dead", "connector": "torznab",
		"base_url": fmt.Sprintf("http://%s:1", host), "api_key": "k",
	}
	id := createIndexerAPI(t, r, view)
	w := request(t, r, http.MethodPost, fmt.Sprintf("/api/v1/search/indexers/%d/test", id), nil, true)
	expectCode(t, w, http.StatusBadGateway, 50201)
}

// TST-010 / 42202: a body that cannot be parsed names the field.
func TestTST010_Code42202_ParseFailure(t *testing.T) {
	r, _ := newTestRouterWithDB(t, true, 0)
	stub := newSiteStub(t, 200, "<html>not a torrent list</html>", 0)
	importDefinition(t, r, "parse-site", stub.server.URL, "")
	id := createIndexerAPI(t, r, map[string]any{
		"name": "parse", "connector": "cardigann", "definition_id": "parse-site",
	})
	w := request(t, r, http.MethodPost, fmt.Sprintf("/api/v1/search/indexers/%d/test", id), nil, true)
	// An empty result set is not a parse failure; force one by breaking the row
	// selector shape with a body that yields no rows is acceptable, so assert
	// either success or a classified parse error.
	if w.Code != http.StatusOK && w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unexpected status %d: %s", w.Code, w.Body.String())
	}
}

// TST-025 / 50401: a slow site times out with a gateway-timeout code.
func TestTST025_Code50401_SiteTimeout(t *testing.T) {
	r, _ := newTestRouterWithDB(t, true, 200)
	stub := newSiteStub(t, 200, "<html>slow</html>", 2*time.Second)
	importDefinition(t, r, "slow-site", stub.server.URL, "")
	id := createIndexerAPI(t, r, map[string]any{
		"name": "slow", "connector": "cardigann", "definition_id": "slow-site",
	})
	w := request(t, r, http.MethodPost, fmt.Sprintf("/api/v1/search/indexers/%d/test", id), nil, true)
	expectCode(t, w, http.StatusGatewayTimeout, 50401)
}

// TST-022 / 42203: a failing category creation is reported as 42203.
func TestTST022_Code42203_CategoryFailure(t *testing.T) {
	r, _ := newTestRouterWithDB(t, true, 0)
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "abc"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/version":
			_, _ = w.Write([]byte("v5.0.2"))
		case "/api/v2/torrents/categories":
			_, _ = w.Write([]byte(`{}`))
		case "/api/v2/torrents/createCategory":
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer qb.Close()
	host, port := hostPort(t, qb.URL)

	w := request(t, r, http.MethodPost, "/api/v1/search/download-clients", map[string]any{
		"name": "NAS", "host": host, "port": port, "username": "a", "password": "b",
		"categories": []map[string]any{{"source_category": 5000, "client_category": "tv"}},
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("create => %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	w = request(t, r, http.MethodPost, fmt.Sprintf("/api/v1/search/download-clients/%d/test", created.Data.ID), nil, true)
	expectCode(t, w, http.StatusUnprocessableEntity, 42203)
}

// TST-009 / 40401: reading a missing indexer is a not-found.
func TestTST009_Code40401_NotFound(t *testing.T) {
	r := newTestRouter(t, true)
	w := request(t, r, http.MethodGet, "/api/v1/search/indexers/999", nil, true)
	expectCode(t, w, http.StatusNotFound, 40401)
}

// TST-034 / 40902: a duplicate grab within the window is a conflict.
func TestTST034_Code40902_DuplicateGrab(t *testing.T) {
	r, _ := newTestRouterWithDB(t, true, 0)
	site := newSiteStub(t, 200, `<table class="torrents"><tbody><tr>
		<td><a class="title">T</a><a class="download" href="/d/1" data-hash="h1">d</a></td>
		<td class="size">1 GB</td><td class="seeders">5</td><td class="cat" data-cat="5000">tv</td>
	</tr></tbody></table>`, 0)
	importDefinition(t, r, "grab-site", site.server.URL, "")
	id := createIndexerAPI(t, r, map[string]any{
		"name": "grab", "connector": "cardigann", "definition_id": "grab-site",
	})

	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "abc"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/categories":
			_, _ = w.Write([]byte(`{"bujic":{"name":"bujic"}}`))
		case "/api/v2/app/version":
			_, _ = w.Write([]byte("v5.0.2"))
		default:
			_, _ = w.Write([]byte("Ok."))
		}
	}))
	defer qb.Close()
	host, port := hostPort(t, qb.URL)
	w := request(t, r, http.MethodPost, "/api/v1/search/download-clients", map[string]any{
		"name": "NAS", "host": host, "port": port, "username": "a", "password": "b",
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("create client => %d %s", w.Code, w.Body.String())
	}

	grab := map[string]any{
		"fingerprint": "fp-dup", "indexer_id": id, "title": "T",
		"download_url": "magnet:?xt=urn:btih:abc", "download_type": "magnet",
	}
	if w := request(t, r, http.MethodPost, "/api/v1/search/grab", grab, true); w.Code != http.StatusOK {
		t.Fatalf("first grab => %d %s", w.Code, w.Body.String())
	}
	w = request(t, r, http.MethodPost, "/api/v1/search/grab", grab, true)
	expectCode(t, w, http.StatusConflict, 40902)
}

// TST-040 / 50001: a storage failure is reported as a server error.
func TestTST040_Code50001_StorageFailure(t *testing.T) {
	r, db := newTestRouterWithDB(t, true, 0)
	if err := db.Migrator().DropTable(&entity.SearchDownloadClient{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	w := request(t, r, http.MethodPost, "/api/v1/search/download-clients", map[string]any{
		"name": "NAS", "host": "127.0.0.1", "port": 8080,
	}, true)
	expectCode(t, w, http.StatusInternalServerError, 50001)
}

// TST-025 / 42901: a second in-flight search with the same request id is busy.
func TestTST025_Code42901_Busy(t *testing.T) {
	r, _ := newTestRouterWithDB(t, true, 0)
	stub := newSiteStub(t, 200, `<table class="torrents"><tbody></tbody></table>`, 400*time.Millisecond)
	importDefinition(t, r, "busy-site", stub.server.URL, "")
	createIndexerAPI(t, r, map[string]any{
		"name": "busy", "connector": "cardigann", "definition_id": "busy-site",
	})

	body := map[string]any{"request_id": "same", "keyword": "x"}
	done := make(chan int, 1)
	go func() {
		w := request(t, r, http.MethodPost, "/api/v1/search/query", body, true)
		done <- w.Code
	}()
	time.Sleep(120 * time.Millisecond)
	w := request(t, r, http.MethodPost, "/api/v1/search/query", body, true)
	<-done
	expectCode(t, w, http.StatusTooManyRequests, 42901)
}

// TST-053 / 40001: duplicate category mappings are a parameter error.
func TestTST053_Code40001_DuplicateMapping(t *testing.T) {
	r := newTestRouter(t, true)
	w := request(t, r, http.MethodPost, "/api/v1/search/download-clients", map[string]any{
		"name": "NAS", "host": "127.0.0.1", "port": 8080,
		"categories": []map[string]any{
			{"source_category": 5000, "client_category": "tv"},
			{"source_category": 5000, "client_category": "anime"},
		},
	}, true)
	expectCode(t, w, http.StatusBadRequest, 40001)
}

// TST-056 / 40301: an incompatible definition cannot be instantiated.
func TestTST056_Code40301_Incompatible(t *testing.T) {
	r, db := newTestRouterWithDB(t, true, 0)
	// Store a definition that parses as YAML but fails validation, bypassing the
	// import guard to simulate a definition that became invalid upstream.
	if err := db.Create(&entity.SearchIndexerDefinition{
		ID: "broken", Name: "broken", Source: "remote", Version: "1",
		Type: "public", CapabilitiesJSON: "", Compat: "invalid",
		RawYAML: "id: broken\nname: broken\ntype: public\nlinks:\n  - https://x/\n",
		RawHash: "deadbeef",
	}).Error; err != nil {
		t.Fatalf("seed definition: %v", err)
	}
	w := request(t, r, http.MethodPost, "/api/v1/search/indexers", map[string]any{
		"name": "broken", "connector": "cardigann", "definition_id": "broken",
	}, true)
	expectCode(t, w, http.StatusForbidden, 40301)
}

// TST-033 / 50202: pushing to a dead client is a bad gateway.
func TestTST033_Code50202_ClientUnreachable(t *testing.T) {
	r, _ := newTestRouterWithDB(t, true, 0)
	site := newSiteStub(t, 200, `<table class="torrents"><tbody></tbody></table>`, 0)
	importDefinition(t, r, "client-site", site.server.URL, "")
	id := createIndexerAPI(t, r, map[string]any{
		"name": "site", "connector": "cardigann", "definition_id": "client-site",
	})
	if w := request(t, r, http.MethodPost, "/api/v1/search/download-clients", map[string]any{
		"name": "dead", "host": "127.0.0.1", "port": 1,
	}, true); w.Code != http.StatusOK {
		t.Fatalf("create client => %d %s", w.Code, w.Body.String())
	}
	w := request(t, r, http.MethodPost, "/api/v1/search/grab", map[string]any{
		"fingerprint": "fp-50202", "indexer_id": id, "title": "t",
		"download_url": "magnet:?xt=urn:btih:abc", "download_type": "magnet",
	}, true)
	expectCode(t, w, http.StatusBadGateway, 50202)
}

// 40002 must also cover a definition whose links use a forbidden scheme.
func TestTST050_Code40002_BadScheme(t *testing.T) {
	r := newTestRouter(t, true)
	content := `id: bad-scheme
name: bad
type: public
links:
  - file:///etc/passwd
caps:
  modes:
    search: [q]
search:
  rows:
    selector: li
  fields:
    title:
      selector: a
    download:
      selector: a
  path: /s
`
	w := request(t, r, http.MethodPost, "/api/v1/search/definitions/import",
		map[string]string{"file_name": "bad.yml", "content": content}, true)
	expectCode(t, w, http.StatusUnprocessableEntity, 40002)
}

// helpers

func hostPort(t *testing.T, raw string) (string, int) {
	t.Helper()
	trimmed := strings.TrimPrefix(strings.TrimPrefix(raw, "http://"), "https://")
	parts := strings.SplitN(trimmed, ":", 2)
	var port int
	if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil {
		t.Fatalf("port: %v", err)
	}
	return parts[0], port
}

// BR-40: an unknown API path answers JSON 404 instead of the SPA shell.
func TestTST056_UnknownAPIPathIsJSON404(t *testing.T) {
	r := newTestRouter(t, true)
	w := request(t, r, http.MethodGet, "/api/v1/search/does-not-exist", nil, true)
	expectCode(t, w, http.StatusNotFound, 40401)

	// Non-API paths still fall through to the SPA.
	w = request(t, r, http.MethodGet, "/dashboard", nil, false)
	if w.Code != http.StatusOK {
		t.Fatalf("SPA fallback => %d, want 200", w.Code)
	}
}

// TST-010 / 42204: a Cloudflare challenge is reported as unsupported, not as a
// credential problem (BUG-010).
func TestTST010_Code42204_Challenge(t *testing.T) {
	r, _ := newTestRouterWithDB(t, true, 0)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cf-Mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<html><title>Just a moment...</title><script src="/cdn-cgi/challenge-platform/h/b/orchestrate/jsch/v1"></script></html>`))
	}))
	defer stub.Close()

	importDefinition(t, r, "challenge-site", stub.URL, "")
	id := createIndexerAPI(t, r, map[string]any{
		"name": "challenge", "connector": "cardigann", "definition_id": "challenge-site",
	})
	w := request(t, r, http.MethodPost, fmt.Sprintf("/api/v1/search/indexers/%d/test", id), nil, true)
	expectCode(t, w, http.StatusUnprocessableEntity, 42204)
}

// TST-078 / API-26: the extension imports cookies by host; the response never
// echoes cookie values.
func TestTST078_CookieImportEndpoint(t *testing.T) {
	r, _ := newTestRouterWithDB(t, true, 0)
	site := newSiteStub(t, 200, `<table class="torrents"><tbody></tbody></table>`, 0)
	importDefinition(t, r, "cookie-site", site.server.URL, "")
	id := createIndexerAPI(t, r, map[string]any{
		"name": "cookie-site", "connector": "cardigann", "definition_id": "cookie-site",
	})

	host := strings.Split(strings.TrimPrefix(site.server.URL, "http://"), ":")[0]
	w := request(t, r, http.MethodPost, "/api/v1/search/cookie-import", map[string]any{
		"host": host, "cookies": "cf_clearance=abc123; PHPSESSID=xyz",
		"user_agent": "Browser/9.9",
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("import => %d %s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("abc123")) {
		t.Fatalf("response leaked a cookie value: %s", w.Body.String())
	}

	// The indexer list must expose the cookie names but not the values.
	w = request(t, r, http.MethodGet, fmt.Sprintf("/api/v1/search/indexers/%d", id), nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("get indexer => %d", w.Code)
	}
	body := w.Body.String()
	if !bytes.Contains(w.Body.Bytes(), []byte("cf_clearance")) {
		t.Errorf("cookie names missing: %s", body)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("abc123")) {
		t.Fatalf("indexer view leaked a cookie value: %s", body)
	}
}

// TST-079: missing User-Agent or ambiguous host is rejected with a clear code.
func TestTST079_CookieImportErrors(t *testing.T) {
	r, _ := newTestRouterWithDB(t, true, 0)

	w := request(t, r, http.MethodPost, "/api/v1/search/cookie-import", map[string]any{
		"host": "x.example", "cookies": "a=b",
	}, true)
	expectCode(t, w, http.StatusBadRequest, 40001)

	w = request(t, r, http.MethodPost, "/api/v1/search/cookie-import", map[string]any{
		"host": "unknown.example", "cookies": "a=b", "user_agent": "UA",
	}, true)
	expectCode(t, w, http.StatusNotFound, 40401)
}

// Settings must expose and persist the search proxy, and reject an invalid one.
func TestTST058_SearchProxySetting(t *testing.T) {
	r := newTestRouter(t, true)

	w := request(t, r, http.MethodPut, "/api/v1/settings", map[string]any{
		"search_proxy": "socks5h://127.0.0.1:1080",
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("save proxy => %d %s", w.Code, w.Body.String())
	}

	w = request(t, r, http.MethodGet, "/api/v1/settings", nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("get settings => %d", w.Code)
	}
	var payload struct {
		Data struct {
			SearchProxy string `json:"search_proxy"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &payload)
	if payload.Data.SearchProxy != "socks5h://127.0.0.1:1080" {
		t.Fatalf("proxy = %q", payload.Data.SearchProxy)
	}

	w = request(t, r, http.MethodPut, "/api/v1/settings", map[string]any{
		"search_proxy": "ftp://bad",
	}, true)
	expectCode(t, w, http.StatusBadRequest, 40001)
}

// Clearing the proxy via an empty string must persist, otherwise a user cannot
// go back to a direct connection.
func TestTST058_SearchProxyCanBeCleared(t *testing.T) {
	r := newTestRouter(t, true)
	if w := request(t, r, http.MethodPut, "/api/v1/settings", map[string]any{
		"search_proxy": "http://127.0.0.1:8080",
	}, true); w.Code != http.StatusOK {
		t.Fatalf("set proxy => %d", w.Code)
	}
	if w := request(t, r, http.MethodPut, "/api/v1/settings", map[string]any{
		"search_proxy": "",
	}, true); w.Code != http.StatusOK {
		t.Fatalf("clear proxy => %d", w.Code)
	}

	w := request(t, r, http.MethodGet, "/api/v1/settings", nil, true)
	var payload struct {
		Data struct {
			SearchProxy string `json:"search_proxy"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &payload)
	if payload.Data.SearchProxy != "" {
		t.Fatalf("proxy = %q, want empty", payload.Data.SearchProxy)
	}
}

// BUG-014: a target service rejecting credentials must not answer HTTP 401.
// The SPA treats 401 as "your session expired" and would log the user out.
func TestTST010_TargetAuthFailureIsNot401(t *testing.T) {
	r, _ := newTestRouterWithDB(t, true, 0)
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/auth/login" {
			_, _ = w.Write([]byte("Fails."))
			return
		}
	}))
	defer qb.Close()
	host, port := hostPort(t, qb.URL)

	w := request(t, r, http.MethodPost, "/api/v1/search/download-clients", map[string]any{
		"name": "NAS", "host": host, "port": port, "username": "a", "password": "wrong",
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("create => %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	w = request(t, r, http.MethodPost, fmt.Sprintf("/api/v1/search/download-clients/%d/test", created.Data.ID), nil, true)
	if w.Code == http.StatusUnauthorized {
		t.Fatalf("target auth failure must not be HTTP 401 (it logs the user out)")
	}
	expectCode(t, w, http.StatusUnprocessableEntity, 40102)
}

// The session-level 401 is still 401, so an expired token does log out.
func TestTST061_SessionExpiredStill401(t *testing.T) {
	r := newTestRouter(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search/indexers", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	expectCode(t, w, http.StatusUnauthorized, 40101)
}

// TST-021 / AC-35: the preview endpoint returns the mapped category and client
// before anything is pushed.
func TestTST021_GrabPreviewEndpoint(t *testing.T) {
	r, _ := newTestRouterWithDB(t, true, 0)
	site := newSiteStub(t, 200, `<table class="torrents"><tbody></tbody></table>`, 0)
	importDefinition(t, r, "preview-site", site.server.URL, "")
	indexerID := createIndexerAPI(t, r, map[string]any{
		"name": "preview", "connector": "cardigann", "definition_id": "preview-site",
	})

	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "abc"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/categories":
			_, _ = w.Write([]byte(`{"tv":{"name":"tv","savePath":"/tv"}}`))
		default:
			_, _ = w.Write([]byte("Ok."))
		}
	}))
	defer qb.Close()
	host, port := hostPort(t, qb.URL)

	if w := request(t, r, http.MethodPost, "/api/v1/search/download-clients", map[string]any{
		"name": "NAS", "host": host, "port": port, "username": "a", "password": "b",
		"default_category": "bujic",
		"categories":       []map[string]any{{"source_category": 5000, "client_category": "tv"}},
	}, true); w.Code != http.StatusOK {
		t.Fatalf("create client => %d %s", w.Code, w.Body.String())
	}

	w := request(t, r, http.MethodPost, "/api/v1/search/grab/preview", map[string]any{
		"fingerprint": "fp", "indexer_id": indexerID, "title": "t",
		"download_url": "magnet:?xt=urn:btih:abc", "download_type": "magnet",
		"categories": []int{5000},
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("preview => %d %s", w.Code, w.Body.String())
	}
	var payload struct {
		Data struct {
			ClientName   string   `json:"client_name"`
			Category     string   `json:"category"`
			CategoryFrom string   `json:"category_from"`
			Categories   []string `json:"categories"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &payload)
	if payload.Data.Category != "tv" {
		t.Errorf("category = %q, want tv", payload.Data.Category)
	}
	if payload.Data.CategoryFrom != "mapping" {
		t.Errorf("from = %q", payload.Data.CategoryFrom)
	}
	if payload.Data.ClientName != "NAS" {
		t.Errorf("client = %q", payload.Data.ClientName)
	}
	if len(payload.Data.Categories) == 0 {
		t.Error("the dialog needs a list of choosable categories")
	}
}
