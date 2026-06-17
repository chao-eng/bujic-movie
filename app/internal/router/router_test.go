package router

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
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

func TestEncryptedLoginAndPasswordUpdate(t *testing.T) {
	// 1. Setup GORM Memory Database
	database, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open SQLite memory DB: %v", err)
	}

	// Migrate settings table
	if err := database.AutoMigrate(&entity.SystemSetting{}); err != nil {
		t.Fatalf("Failed to migrate settings table: %v", err)
	}

	// 2. Setup config parameters
	cfg := &config.Config{}
	cfg.Server.Mode = "test"
	cfg.Server.SecretKey = "test_secret_key"
	cfg.Server.Username = "admin"
	cfg.Server.Password = "admin123"

	r := SetupRouter(database, cfg)

	// 3. Test GetLoginKey
	req, _ := http.NewRequest("GET", "/api/v1/auth/login-key", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected login-key status 200, got %d", w.Code)
	}

	var keyResponse struct {
		Code int `json:"code"`
		Data struct {
			KeyID string `json:"key_id"`
			Key   string `json:"key"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &keyResponse)
	keyID := keyResponse.Data.KeyID
	keyHex := keyResponse.Data.Key

	if keyID == "" || keyHex == "" {
		t.Fatalf("Failed to retrieve key_id or key: %v", keyResponse)
	}

	// 4. Encrypt password locally using the key retrieved
	keyBytes, _ := hex.DecodeString(keyHex)
	block, _ := aes.NewCipher(keyBytes)
	aesGCM, _ := cipher.NewGCM(block)
	
	iv := make([]byte, 12)
	rand.Read(iv)
	
	ciphertext := aesGCM.Seal(nil, iv, []byte("admin123"), nil)

	// 5. Submit encrypted login request
	loginData := map[string]string{
		"username":           "admin",
		"encrypted_password": hex.EncodeToString(ciphertext),
		"key_id":             keyID,
		"iv":                 hex.EncodeToString(iv),
	}
	body, _ := json.Marshal(loginData)
	req, _ = http.NewRequest("POST", "/api/v1/auth/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected encrypted login status 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var loginResponse struct {
		Code int `json:"code"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &loginResponse)
	token := loginResponse.Data.Token

	// 6. Test change password via settings/password
	// Fetch new challenge key
	req, _ = http.NewRequest("GET", "/api/v1/auth/login-key", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	_ = json.Unmarshal(w.Body.Bytes(), &keyResponse)
	keyID2 := keyResponse.Data.KeyID
	keyHex2 := keyResponse.Data.Key

	keyBytes2, _ := hex.DecodeString(keyHex2)
	block2, _ := aes.NewCipher(keyBytes2)
	aesGCM2, _ := cipher.NewGCM(block2)

	ivOld := make([]byte, 12)
	ivNew := make([]byte, 12)
	rand.Read(ivOld)
	rand.Read(ivNew)

	encryptedOld := aesGCM2.Seal(nil, ivOld, []byte("admin123"), nil)
	encryptedNew := aesGCM2.Seal(nil, ivNew, []byte("new_password_123"), nil)

	pwdChangeData := map[string]string{
		"key_id":                 keyID2,
		"encrypted_old_password": hex.EncodeToString(encryptedOld),
		"old_iv":                 hex.EncodeToString(ivOld),
		"encrypted_new_password": hex.EncodeToString(encryptedNew),
		"new_iv":                 hex.EncodeToString(ivNew),
	}
	bodyChange, _ := json.Marshal(pwdChangeData)
	req, _ = http.NewRequest("PUT", "/api/v1/settings/password", bytes.NewBuffer(bodyChange))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected password change status 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	// Verify that password has updated in config
	if cfg.Server.Password != "new_password_123" {
		t.Fatalf("Expected config password to be new_password_123, got %s", cfg.Server.Password)
	}
}
