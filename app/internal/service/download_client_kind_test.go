package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bujic-movie/bujic-movie/internal/service/downloadclient"
)

// qBittorrent derives the folder from the category, so a mapping's save path is
// only a fallback for the "no category at all" case.
func TestResolveTarget_QBittorrentKeepsCategoryAsFolder(t *testing.T) {
	env := newTestEnv(t)
	client, err := env.clients.Create(DownloadClientInput{
		Name: "qB", Kind: downloadclient.KindQBittorrent,
		Host: "127.0.0.1", Port: 8080, DefaultCategory: "other",
		DefaultSavePath: "/downloads",
		Categories: []CategoryMappingInput{
			{SourceCategory: 2040, ClientCategory: "4k", SavePath: "/downloads/4k"},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	target, err := env.clients.ResolveTarget(client.ID, []int{2040})
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if target.Category != "4k" {
		t.Errorf("category = %q, want 4k", target.Category)
	}
	if target.SavePath != "" {
		t.Errorf("qBittorrent should decide the folder from the category, got %q", target.SavePath)
	}
	if target.Source != "mapping" {
		t.Errorf("source = %q", target.Source)
	}
}

// Transmission has no categories: the mapping's directory is the only thing
// that decides where the file lands.
func TestResolveTarget_TransmissionUsesMappingSavePath(t *testing.T) {
	env := newTestEnv(t)
	client, err := env.clients.Create(DownloadClientInput{
		Name: "TR", Kind: downloadclient.KindTransmission,
		Host: "127.0.0.1", Port: 9091, Username: "a", Password: "b",
		DefaultSavePath: "/downloads/other",
		Categories: []CategoryMappingInput{
			{SourceCategory: 5000, SavePath: "/downloads/tv"},
		},
	})
	if err != nil {
		t.Fatalf("create: %v (client caps should accept a directory-only mapping)", err)
	}

	target, err := env.clients.ResolveTarget(client.ID, []int{5000})
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if target.SavePath != "/downloads/tv" {
		t.Errorf("save path = %q, want /downloads/tv", target.SavePath)
	}
	if target.Category != "" {
		t.Errorf("category = %q; Transmission has none", target.Category)
	}

	// No matching mapping: the client default is the fallback.
	target, err = env.clients.ResolveTarget(client.ID, []int{2000})
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if target.SavePath != "/downloads/other" || target.Source != "default" {
		t.Errorf("fallback = %+v, want default path", target)
	}
}

// A parent mapping covers its children, same as the category rule.
func TestResolveTarget_ParentSavePath(t *testing.T) {
	env := newTestEnv(t)
	client, err := env.clients.Create(DownloadClientInput{
		Name: "Ar", Kind: downloadclient.KindAria2,
		Host: "127.0.0.1", Port: 6800, Password: "token",
		Categories: []CategoryMappingInput{
			{SourceCategory: 2000, SavePath: "/downloads/movies"},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	target, err := env.clients.ResolveTarget(client.ID, []int{2045})
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if target.SavePath != "/downloads/movies" || target.Source != "mapping" {
		t.Errorf("target = %+v", target)
	}
}

// Creating a client of an unsupported kind must name the allowed values instead
// of silently storing something that can never be contacted.
func TestCreateClient_RejectsUnknownKind(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.clients.Create(DownloadClientInput{Name: "x", Kind: "utorrent", Host: "127.0.0.1", Port: 1})
	var inputErr *InputError
	if !errors.As(err, &inputErr) || inputErr.Field != "kind" {
		t.Fatalf("error = %v, want kind field error", err)
	}
	if !strings.Contains(inputErr.Message, "Transmission") {
		t.Errorf("message should list the supported kinds: %q", inputErr.Message)
	}
}

// Deluge needs a Web UI password; saving without one would only fail at test
// time with a confusing auth error.
func TestCreateClient_DelugeRequiresPassword(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.clients.Create(DownloadClientInput{Name: "D", Kind: downloadclient.KindDeluge, Host: "127.0.0.1", Port: 8112})
	var inputErr *InputError
	if !errors.As(err, &inputErr) || inputErr.Field != "password" {
		t.Fatalf("error = %v, want password field error", err)
	}
}

// A kind without categories may still be saved with a default directory and no
// mapping at all.
func TestCreateClient_TransmissionWithoutMappings(t *testing.T) {
	env := newTestEnv(t)
	view, err := env.clients.Create(DownloadClientInput{
		Name: "TR", Kind: downloadclient.KindTransmission, Host: "127.0.0.1", Port: 9091,
		Username: "a", Password: "b", DefaultSavePath: "/downloads/",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if view.DefaultSavePath != "/downloads" {
		t.Errorf("save path = %q, want trimmed", view.DefaultSavePath)
	}
	if view.Capabilities.Categories {
		t.Error("view should report that Transmission has no categories")
	}
}

// The grab uses the resolved directory and records it, so the history says where
// the file went.
func TestGrab_TransmissionPassesDirectory(t *testing.T) {
	env := newTestEnv(t)
	site := newCardigannSite(t, 1)
	defer site.Close()
	indexerID := env.createSiteNamed(t, "x", site.URL)

	trSrv, args := newTransmissionStub(t)
	defer trSrv.Close()

	client, err := env.clients.Create(DownloadClientInput{
		Name: "TR", Kind: downloadclient.KindTransmission,
		Host: hostOf(t, trSrv.URL), Port: portOf(t, trSrv.URL), Username: "a", Password: "b",
		Categories: []CategoryMappingInput{{SourceCategory: 5000, SavePath: "/downloads/tv"}},
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	result, err := env.grabs.Grab(context.Background(), GrabRequest{
		Fingerprint: "fp-tr", IndexerID: indexerID, Title: "剧集",
		DownloadURL: "magnet:?xt=urn:btih:abc", DownloadType: "magnet",
		Categories: []int{5000}, ClientID: client.ID,
	})
	if err != nil {
		t.Fatalf("Grab: %v", err)
	}
	if result.SavePath != "/downloads/tv" {
		t.Errorf("result save path = %q", result.SavePath)
	}
	if dir, _ := args()["download-dir"].(string); dir != "/downloads/tv" {
		t.Errorf("download-dir sent = %v, want /downloads/tv", args()["download-dir"])
	}

	records, _ := env.grabs.Records(10)
	if len(records) != 1 || records[0].SavePath != "/downloads/tv" {
		t.Fatalf("records = %+v", records)
	}
}

// newTransmissionStub serves the RPC handshake and records torrent-add
// arguments, so a grab test can assert on the directory that was sent.
func newTransmissionStub(t *testing.T) (*httptest.Server, func() map[string]any) {
	t.Helper()
	var mu = make(chan struct{}, 1)
	mu <- struct{}{}
	captured := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") != "session" {
			w.Header().Set("X-Transmission-Session-Id", "session")
			w.WriteHeader(http.StatusConflict)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Method    string         `json:"method"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(body, &payload)
		if payload.Method == "torrent-add" {
			<-mu
			captured = payload.Arguments
			mu <- struct{}{}
		}
		switch payload.Method {
		case "session-get":
			_, _ = w.Write([]byte(`{"result":"success","arguments":{"version":"4.0.5"}}`))
		default:
			_, _ = w.Write([]byte(`{"result":"success","arguments":{}}`))
		}
	}))
	return srv, func() map[string]any {
		<-mu
		out := map[string]any{}
		for k, v := range captured {
			out[k] = v
		}
		mu <- struct{}{}
		return out
	}
}
