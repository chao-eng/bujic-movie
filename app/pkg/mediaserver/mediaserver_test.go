package mediaserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbyRefreshAll(t *testing.T) {
	var gotPath, gotMethod, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotToken = r.Header.Get("X-Emby-Token")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	s, err := New(TypeEmby, srv.URL, "secret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Refresh(context.Background(), ""); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if gotMethod != "POST" || gotPath != "/emby/Library/Refresh" {
		t.Errorf("emby refresh = %s %q, want POST /emby/Library/Refresh", gotMethod, gotPath)
	}
	if gotToken != "secret" {
		t.Errorf("X-Emby-Token = %q, want secret", gotToken)
	}
}

func TestEmbyRefreshSingleLibrary(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	s, _ := New(TypeEmby, srv.URL, "secret")
	if err := s.Refresh(context.Background(), "42"); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if gotPath != "/emby/Items/42/Refresh" {
		t.Errorf("emby single refresh path = %q, want /emby/Items/42/Refresh", gotPath)
	}
}

func TestEmbyListLibraries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/emby/Library/VirtualFolders" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`[{"Name":"电影","ItemId":"1"},{"Name":"电视剧","ItemId":"2"}]`))
	}))
	defer srv.Close()

	s, _ := New(TypeEmby, srv.URL, "secret")
	libs, err := s.ListLibraries(context.Background())
	if err != nil {
		t.Fatalf("ListLibraries: %v", err)
	}
	if len(libs) != 2 || libs[0].ID != "1" || libs[0].Name != "电影" || libs[1].ID != "2" {
		t.Errorf("libs = %+v, want [{1 电影} {2 电视剧}]", libs)
	}
}

func TestJellyfinRefreshAllUsesRootPrefix(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	s, _ := New(TypeJellyfin, srv.URL, "secret")
	if err := s.Refresh(context.Background(), ""); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if gotPath != "/Library/Refresh" {
		t.Errorf("jellyfin refresh path = %q, want /Library/Refresh (no /emby prefix)", gotPath)
	}
}

func TestPlexRefreshAllRefreshesEverySection(t *testing.T) {
	refreshed := map[string]bool{}
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/library/sections":
			gotToken = r.URL.Query().Get("X-Plex-Token")
			resp := plexSectionsResponse{}
			resp.MediaContainer.Directory = []struct {
				Key   string `json:"key"`
				Title string `json:"title"`
			}{
				{Key: "1", Title: "Movies"},
				{Key: "2", Title: "TV"},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/library/sections/1/refresh":
			refreshed["1"] = true
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/library/sections/2/refresh":
			refreshed["2"] = true
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	s, _ := New(TypePlex, srv.URL, "plextoken")
	if err := s.Refresh(context.Background(), ""); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if gotToken != "plextoken" {
		t.Errorf("X-Plex-Token = %q, want plextoken", gotToken)
	}
	if !refreshed["1"] || !refreshed["2"] {
		t.Errorf("expected both sections refreshed, got %v", refreshed)
	}
}

func TestPlexRefreshSingleSection(t *testing.T) {
	var refreshedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s, _ := New(TypePlex, srv.URL, "plextoken")
	if err := s.Refresh(context.Background(), "3"); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if refreshedPath != "/library/sections/3/refresh" {
		t.Errorf("plex single refresh = %q, want /library/sections/3/refresh", refreshedPath)
	}
}

func TestUnsupportedType(t *testing.T) {
	if _, err := New("kodi", "http://x", "k"); err == nil {
		t.Error("expected error for unsupported type")
	}
	if _, err := New(TypeEmby, "", "k"); err == nil {
		t.Error("expected error for empty url")
	}
}
