package tmdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTMDBClient(t *testing.T) {
	// Setup mock test server to emulate TMDB API responses
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("api_key") != "test_key" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"status_message": "Invalid API key"}`))
			return
		}

		switch r.URL.Path {
		case "/search/movie":
			resp := SearchMovieResponse{
				Results: []SearchMovieResult{
					{
						ID:    123,
						Title: "Mock Movie",
					},
				},
			}
			data, _ := json.Marshal(resp)
			w.Write(data)
		case "/movie/123":
			detail := MovieDetail{
				ID:    123,
				Title: "Mock Movie Detail",
			}
			data, _ := json.Marshal(detail)
			w.Write(data)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient("test_key", server.URL, "zh-CN")
	ctx := context.Background()

	// 1. Test search movie
	results, err := client.SearchMovie(ctx, "Mock", 2024)
	if err != nil {
		t.Fatalf("SearchMovie failed: %v", err)
	}
	if len(results) != 1 || results[0].Title != "Mock Movie" {
		t.Errorf("Unexpected search results: %v", results)
	}

	// 2. Test movie detail
	detail, err := client.GetMovieDetail(ctx, 123)
	if err != nil {
		t.Fatalf("GetMovieDetail failed: %v", err)
	}
	if detail.Title != "Mock Movie Detail" {
		t.Errorf("Unexpected movie detail: %v", detail)
	}

	// 3. Test invalid API key
	badClient := NewClient("bad_key", server.URL, "zh-CN")
	_, err = badClient.SearchMovie(ctx, "Mock", 2024)
	if err == nil {
		t.Errorf("Expected failure for invalid API key, but succeeded")
	}
}
