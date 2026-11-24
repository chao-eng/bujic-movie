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

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/service/downloadclient"
)

// qBittorrent's torrents run under automatic management: the category decides
// the folder and an explicit savepath is ignored, so this app never resolves or
// sends one.
func TestResolveTarget_QBittorrentCategoryOnly(t *testing.T) {
	env := newTestEnv(t)
	client, err := env.clients.Create(DownloadClientInput{
		Name: "qB", Kind: downloadclient.KindQBittorrent,
		Host: "127.0.0.1", Port: 8080, DefaultCategory: "other",
		Categories: []CategoryMappingInput{{SourceCategory: 2040, ClientCategory: "4k"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	target, err := env.clients.ResolveTarget(client.ID, []int{2040})
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if target.Category != "4k" || target.SavePath != "" {
		t.Errorf("target = %+v, want the category alone to decide the folder", target)
	}

	// Even a stored default save path (from a previous kind) must not leak into
	// the push: qBittorrent would ignore it anyway.
	if _, err := env.clients.Update(client.ID, DownloadClientInput{
		Name: "qB", Kind: downloadclient.KindQBittorrent,
		Host: "127.0.0.1", Port: 8080, DefaultCategory: "other",
		DefaultSavePath: "/downloads/loose",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	target, err = env.clients.ResolveTarget(client.ID, []int{3000})
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if target.Category != "other" || target.SavePath != "" {
		t.Errorf("target = %+v, want no save path on qBittorrent", target)
	}
	stored, err := env.clients.Get(client.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.DefaultSavePath != "" {
		t.Errorf("default_save_path = %q, want cleared for a category-managed client", stored.DefaultSavePath)
	}
}

// A directory on a qBittorrent mapping would never take effect, so saving one
// is rejected instead of silently ignored.
func TestCreateClient_QBittorrentRejectsMappingSavePath(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.clients.Create(DownloadClientInput{
		Name: "qB", Kind: downloadclient.KindQBittorrent, Host: "127.0.0.1", Port: 8080,
		Categories: []CategoryMappingInput{
			{SourceCategory: 5000, ClientCategory: "tv", SavePath: "/downloads/tv"},
		},
	})
	var inputErr *InputError
	if !errors.As(err, &inputErr) || inputErr.Field != "categories" {
		t.Fatalf("error = %v, want a categories field error", err)
	}
	if !strings.Contains(inputErr.Message, "分类") {
		t.Errorf("message should point at the category: %q", inputErr.Message)
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

// The qBittorrent push carries the category and nothing else: no savepath, so
// the torrent stays under automatic management.
func TestGrab_QBittorrentSendsCategoryOnly(t *testing.T) {
	env := newTestEnv(t)
	site := newCardigannSite(t, 1)
	defer site.Close()
	indexerID := env.createSiteNamed(t, "x", site.URL)

	qbSrv, pushed := newQBSiteCapturing(t)
	defer qbSrv.Close()

	client, err := env.clients.Create(DownloadClientInput{
		Name: "qB", Kind: downloadclient.KindQBittorrent,
		Host: hostOf(t, qbSrv.URL), Port: portOf(t, qbSrv.URL), Username: "a", Password: "b",
		Categories: []CategoryMappingInput{{SourceCategory: 5000, ClientCategory: "tv"}},
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	result, err := env.grabs.Grab(context.Background(), GrabRequest{
		Fingerprint: "fp-qb-only", IndexerID: indexerID, Title: "剧集",
		DownloadURL: "magnet:?xt=urn:btih:abc", DownloadType: "magnet",
		Categories: []int{5000}, ClientID: client.ID,
	})
	if err != nil {
		t.Fatalf("Grab: %v", err)
	}
	if result.Category != "tv" || result.SavePath != "" {
		t.Errorf("result = %+v", result)
	}
	body := pushed()
	if !strings.Contains(body, `"tv"`) {
		t.Errorf("push body = %s, want the category", body)
	}
	if strings.Contains(body, "savepath") || strings.Contains(body, "autoTMM") {
		t.Errorf("push body = %s, must not carry a directory for qBittorrent", body)
	}
}

// A directory stored while the kind supported one (or written by an older
// build) must not be read back as if it were still applied.
func TestView_QBittorrentHidesStoredSavePaths(t *testing.T) {
	env := newTestEnv(t)
	view, err := env.clients.Create(DownloadClientInput{
		Name: "qB", Kind: downloadclient.KindQBittorrent, Host: "127.0.0.1", Port: 8080,
		Categories: []CategoryMappingInput{{SourceCategory: 5000, ClientCategory: "tv"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Simulate a legacy row written before the capability was withdrawn.
	if _, err := env.clients.Update(view.ID, DownloadClientInput{
		Name: "qB", Kind: downloadclient.KindQBittorrent, Host: "127.0.0.1", Port: 8080,
		Categories: []CategoryMappingInput{{SourceCategory: 5000, ClientCategory: "tv"}},
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := env.repo.ReplaceClientCategories(view.ID, []entity.SearchDownloadClientCategory{
		{SourceCategory: 5000, ClientCategory: "tv", SavePath: "/downloads/legacy"},
	}); err != nil {
		t.Fatalf("seed mapping: %v", err)
	}
	if err := env.repo.UpdateClient(&entity.SearchDownloadClient{
		ID: view.ID, Name: "qB", Kind: downloadclient.KindQBittorrent,
		Host: "127.0.0.1", Port: 8080, DefaultSavePath: "/downloads/legacy", Enabled: true,
	}); err != nil {
		t.Fatalf("seed client: %v", err)
	}

	got, err := env.clients.Get(view.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DefaultSavePath != "" {
		t.Errorf("default save path = %q, want empty", got.DefaultSavePath)
	}
	if len(got.Categories) != 1 || got.Categories[0].SavePath != "" {
		t.Errorf("mappings = %+v, want the stored path hidden", got.Categories)
	}
}
