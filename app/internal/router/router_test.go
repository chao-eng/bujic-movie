package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestAPIRoutes(t *testing.T) {
	// 1. Setup GORM Memory Database for route testing
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open SQLite memory DB: %v", err)
	}

	// 2. Setup config parameters
	cfg := &config.Config{}
	cfg.Server.Mode = "test"
	cfg.Server.SecretKey = "test_secret_key"
	cfg.Server.Username = "admin"
	cfg.Server.Password = "admin123"

	r := SetupRouter(db, cfg)

	// 3. Test Health Check (Public Route)
	req, _ := http.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected health status 200, got %d", w.Code)
	}

	// 4. Test Login (Public Route)
	loginData := map[string]string{
		"username": "admin",
		"password": "admin123",
	}
	body, _ := json.Marshal(loginData)
	req, _ = http.NewRequest("POST", "/api/v1/auth/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected login status 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var loginResponse struct {
		Code int `json:"code"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &loginResponse)
	token := loginResponse.Data.Token
	if token == "" {
		t.Fatalf("Expected login token to be returned, but it was empty")
	}

	// 5. Test Access Protected Route (Settings) without Token (Expect 401)
	req, _ = http.NewRequest("GET", "/api/v1/settings", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected unauthorized status 401, got %d", w.Code)
	}

	// 6. Test Access Protected Route with Valid Token (Expect 200)
	req, _ = http.NewRequest("GET", "/api/v1/settings", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected settings status 200, got %d (body: %s)", w.Code, w.Body.String())
	}
}
