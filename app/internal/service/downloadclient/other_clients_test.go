package downloadclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
)

func newExecutor(t *testing.T) *httpclient.Executor {
	t.Helper()
	exec, err := httpclient.New(nil, nil, "")
	if err != nil {
		t.Fatalf("httpclient.New: %v", err)
	}
	return exec
}

// A Transmission daemon answers 409 with a session id until the request echoes
// it back; a client that treats that as a failure can never talk to it.
func TestTransmissionSessionHandshake(t *testing.T) {
	var addCalls int
	var sawSession string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") != "session-1" {
			w.Header().Set("X-Transmission-Session-Id", "session-1")
			w.WriteHeader(http.StatusConflict)
			return
		}
		sawSession = r.Header.Get("X-Transmission-Session-Id")
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Method    string          `json:"method"`
			Arguments json.RawMessage `json:"arguments"`
		}
		_ = json.Unmarshal(body, &payload)
		switch payload.Method {
		case "session-get":
			_, _ = w.Write([]byte(`{"result":"success","arguments":{"version":"4.0.5","rpc-version":17}}`))
		case "torrent-add":
			addCalls++
			_, _ = w.Write([]byte(`{"result":"success","arguments":{}}`))
		default:
			_, _ = w.Write([]byte(`{"result":"success"}`))
		}
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	client := NewTransmissionClient(Config{Kind: KindTransmission, Host: host, Port: port}, newExecutor(t))

	status, err := client.Test(context.Background(), nil)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if !strings.Contains(status.Version, "4.0.5") {
		t.Errorf("version = %q", status.Version)
	}
	if sawSession != "session-1" {
		t.Errorf("session id not echoed: %q", sawSession)
	}
	if err := client.Push(context.Background(), PushOptions{
		Kind: "magnet", Payload: "magnet:?xt=urn:btih:abc", SavePath: "/downloads/movies/",
	}); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if addCalls != 1 {
		t.Errorf("torrent-add calls = %d, want 1", addCalls)
	}
}

// The save path is what selects the folder on Transmission, since it has no
// categories: it must arrive trimmed and only when configured.
func TestTransmissionPushCarriesSavePathOnly(t *testing.T) {
	var args map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") != "s" {
			w.Header().Set("X-Transmission-Session-Id", "s")
			w.WriteHeader(http.StatusConflict)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(body, &payload)
		args = payload.Arguments
		_, _ = w.Write([]byte(`{"result":"success"}`))
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	client := NewTransmissionClient(Config{Kind: KindTransmission, Host: host, Port: port}, newExecutor(t))
	if err := client.Push(context.Background(), PushOptions{
		Kind: "magnet", Payload: "magnet:?xt=urn:btih:abc", Category: "tv", SavePath: "/downloads/tv/",
	}); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if args["download-dir"] != "/downloads/tv" {
		t.Errorf("download-dir = %v", args["download-dir"])
	}
	if _, ok := args["category"]; ok {
		t.Errorf("Transmission has no categories; got %v", args["category"])
	}
}

func TestTransmissionTasksMapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") != "s" {
			w.Header().Set("X-Transmission-Session-Id", "s")
			w.WriteHeader(http.StatusConflict)
			return
		}
		_, _ = w.Write([]byte(`{"result":"success","arguments":{"torrents":[
			{"hashString":"abc","name":"电影","totalSize":100,"percentDone":0.5,"rateDownload":10,"eta":60,"status":4,"peersSendingToUs":3,"trackers":[{"seederCount":9}]}
		]}}`))
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	client := NewTransmissionClient(Config{Kind: KindTransmission, Host: host, Port: port}, newExecutor(t))
	tasks, err := client.Tasks(context.Background())
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d", len(tasks))
	}
	got := tasks[0]
	if got.Hash != "ABC" || got.State != "downloading" || got.Progress != 0.5 {
		t.Errorf("task = %+v", got)
	}
	if got.ConnectedSeeds != 3 || got.AvailableSeeds != 9 {
		t.Errorf("seeds = %d/%d", got.ConnectedSeeds, got.AvailableSeeds)
	}
}

func TestTransmissionAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	client := NewTransmissionClient(Config{Kind: KindTransmission, Host: host, Port: port, Username: "a", Password: "b"}, newExecutor(t))
	_, err := client.Test(context.Background(), nil)
	var cErr *Error
	if !As(err, &cErr) || cErr.Kind != KindAuth {
		t.Fatalf("kind = %v, want auth", err)
	}
}

// Deluge authenticates with a Web UI password and returns a session cookie that
// later calls must carry.
func TestDelugeLoginKeepsCookie(t *testing.T) {
	var loginCalls, addCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(body, &payload)
		switch payload.Method {
		case "auth.login":
			loginCalls++
			http.SetCookie(w, &http.Cookie{Name: "_session_id", Value: "sess"})
			_, _ = w.Write([]byte(`{"result":true,"error":null,"id":1}`))
		default:
			if !strings.Contains(r.Header.Get("Cookie"), "_session_id=sess") {
				_, _ = w.Write([]byte(`{"result":null,"error":{"message":"Not authenticated","code":2},"id":2}`))
				return
			}
			if payload.Method == "core.add_torrent_magnet" {
				addCalls++
				_, _ = w.Write([]byte(`{"result":"abc123","error":null,"id":3}`))
				return
			}
			_, _ = w.Write([]byte(`{"result":"2.1.1","error":null,"id":4}`))
		}
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	client := NewDelugeClient(Config{Kind: KindDeluge, Host: host, Port: port, Password: "deluge"}, newExecutor(t))
	defer srv.Close()

	status, err := client.Test(context.Background(), nil)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if status.Version != "2.1.1" {
		t.Errorf("version = %q", status.Version)
	}
	if loginCalls != 1 {
		t.Errorf("login calls = %d, want 1", loginCalls)
	}
	if err := client.Push(context.Background(), PushOptions{Kind: "magnet", Payload: "magnet:?xt=urn:btih:abc"}); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if addCalls != 1 {
		t.Errorf("add calls = %d, want 1", addCalls)
	}
}

func TestDelugeWrongPassword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":false,"error":null,"id":1}`))
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	client := NewDelugeClient(Config{Kind: KindDeluge, Host: host, Port: port, Password: "nope"}, newExecutor(t))
	_, err := client.Test(context.Background(), nil)
	var cErr *Error
	if !As(err, &cErr) || cErr.Kind != KindAuth {
		t.Fatalf("kind = %v, want auth", err)
	}
}

// Aria2 takes the secret as a "token:" parameter; a wrong token is an auth
// failure, not an unreachable client.
func TestAria2PushUsesToken(t *testing.T) {
	var sawToken any
	var sawDir any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		_ = json.Unmarshal(body, &payload)
		if len(payload.Params) > 0 {
			sawToken = payload.Params[0]
		}
		if payload.Method == "aria2.addUri" {
			if len(payload.Params) > 2 {
				if opts, ok := payload.Params[2].(map[string]any); ok {
					sawDir = opts["dir"]
				}
			}
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"bujic","result":"gid1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"bujic","result":{"version":"1.36.0"}}`))
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	client := NewAria2Client(Config{Kind: KindAria2, Host: host, Port: port, Password: "s3cret"}, newExecutor(t))

	status, err := client.Test(context.Background(), nil)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if status.Version != "1.36.0" {
		t.Errorf("version = %q", status.Version)
	}
	if sawToken != "token:s3cret" {
		t.Errorf("token param = %v", sawToken)
	}
	if err := client.Push(context.Background(), PushOptions{
		Kind: "magnet", Payload: "magnet:?xt=urn:btih:abc", SavePath: "/downloads/tv",
	}); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if sawDir != "/downloads/tv" {
		t.Errorf("dir = %v", sawDir)
	}
}

func TestAria2UnauthorizedMappedToAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"bujic","error":{"code":1,"message":"Unauthorized"}}`))
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	client := NewAria2Client(Config{Kind: KindAria2, Host: host, Port: port, Password: "wrong"}, newExecutor(t))
	_, err := client.Test(context.Background(), nil)
	var cErr *Error
	if !As(err, &cErr) || cErr.Kind != KindAuth {
		t.Fatalf("kind = %v, want auth", err)
	}
}

func TestAria2TasksParseStringNumbers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &payload)
		switch payload.Method {
		case "aria2.tellActive":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"bujic","result":[
				{"gid":"gid1","status":"active","totalLength":"1000","completedLength":"500","downloadSpeed":"120","numSeeders":"4","bittorrent":{"info":{"name":"剧集"}}}
			]}`))
		default:
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"bujic","result":[]}`))
		}
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	client := NewAria2Client(Config{Kind: KindAria2, Host: host, Port: port}, newExecutor(t))
	tasks, err := client.Tasks(context.Background())
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d", len(tasks))
	}
	got := tasks[0]
	if got.Hash != "gid1" || got.Name != "剧集" {
		t.Errorf("task = %+v", got)
	}
	if got.SizeBytes != 1000 || got.Progress != 0.5 || got.SpeedBPS != 120 {
		t.Errorf("numbers = %d %v %d", got.SizeBytes, got.Progress, got.SpeedBPS)
	}
	if got.State != "downloading" || got.ConnectedSeeds != 4 {
		t.Errorf("state/seeds = %s/%d", got.State, got.ConnectedSeeds)
	}
}

// Kinds() drives the form and the validation; a kind missing here cannot be
// configured at all.
func TestKindsCoverAllImplementations(t *testing.T) {
	want := map[string]bool{KindQBittorrent: false, KindTransmission: false, KindDeluge: false, KindAria2: false}
	for _, info := range Kinds() {
		if _, ok := want[info.Kind]; !ok {
			t.Errorf("unexpected kind %q", info.Kind)
			continue
		}
		want[info.Kind] = true
		if info.DefaultPort <= 0 {
			t.Errorf("%s has no default port", info.Kind)
		}
	}
	for kind, ok := range want {
		if !ok {
			t.Errorf("kind %q missing", kind)
		}
	}
	if info, _ := LookupKind(KindQBittorrent); !info.Capabilities.CategoryPath {
		t.Error("qBittorrent should report category-to-folder support")
	}
	if info, _ := LookupKind(KindTransmission); info.Capabilities.Categories {
		t.Error("Transmission has no categories")
	}
}
