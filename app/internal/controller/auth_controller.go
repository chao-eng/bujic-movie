package controller

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/middleware"
	"github.com/bujic-movie/bujic-movie/pkg/response"
	"github.com/gin-gonic/gin"
)

type AuthController struct{}

type LoginRequest struct {
	Username          string `json:"username" binding:"required"`
	Password          string `json:"password"`            // Plain-text fallback for backward compatibility
	EncryptedPassword string `json:"encrypted_password"` // Encrypted password in hex format
	KeyID             string `json:"key_id"`             // Challenge key ID
	IV                string `json:"iv"`                 // Hex encoded IV
}

type Challenge struct {
	Key       []byte
	CreatedAt time.Time
}

var (
	challenges   = make(map[string]Challenge)
	challengesMu sync.RWMutex
)

func addChallenge(id string, key []byte) {
	challengesMu.Lock()
	defer challengesMu.Unlock()
	challenges[id] = Challenge{
		Key:       key,
		CreatedAt: time.Now(),
	}
	// Auto cleanup after 2 minutes
	go func() {
		time.Sleep(2 * time.Minute)
		challengesMu.Lock()
		delete(challenges, id)
		challengesMu.Unlock()
	}()
}

func getChallenge(id string) ([]byte, bool) {
	challengesMu.RLock()
	defer challengesMu.RUnlock()
	c, ok := challenges[id]
	if !ok {
		return nil, false
	}
	if time.Since(c.CreatedAt) > 2*time.Minute {
		return nil, false
	}
	return c.Key, true
}

func deleteChallenge(id string) {
	challengesMu.Lock()
	defer challengesMu.Unlock()
	delete(challenges, id)
}

func decryptWithKey(key []byte, encryptedHex, ivHex string) (string, error) {
	ciphertext, err := hex.DecodeString(encryptedHex)
	if err != nil {
		return "", fmt.Errorf("invalid ciphertext format: %v", err)
	}

	iv, err := hex.DecodeString(ivHex)
	if err != nil {
		return "", fmt.Errorf("invalid iv format: %v", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %v", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %v", err)
	}

	plaintext, err := aesGCM.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %v", err)
	}

	return string(plaintext), nil
}

// DecryptPassword decrypts a ciphertext using a cached challenge key
func DecryptPassword(keyID, encryptedHex, ivHex string) (string, error) {
	key, ok := getChallenge(keyID)
	if !ok {
		return "", fmt.Errorf("invalid or expired challenge key")
	}
	// Delete immediately after retrieval to prevent replay attacks
	deleteChallenge(keyID)

	return decryptWithKey(key, encryptedHex, ivHex)
}

func NewAuthController() *AuthController {
	return &AuthController{}
}

// GetLoginKey generates a random session key and registers a login challenge
func (ctrl *AuthController) GetLoginKey(c *gin.Context) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		response.InternalServerError(c, "Failed to generate session key")
		return
	}

	// Generate a secure random challenge ID
	idBytes := make([]byte, 16)
	_, _ = rand.Read(idBytes)
	challengeID := hex.EncodeToString(idBytes)

	addChallenge(challengeID, key)

	response.Success(c, gin.H{
		"key_id": challengeID,
		"key":    hex.EncodeToString(key),
	})
}

// Login checks credentials and returns a JWT token
func (ctrl *AuthController) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Username and password are required")
		return
	}

	password := req.Password

	// Decrypt password if it was transmitted securely using a challenge key
	if req.KeyID != "" && req.EncryptedPassword != "" && req.IV != "" {
		decrypted, err := DecryptPassword(req.KeyID, req.EncryptedPassword, req.IV)
		if err != nil {
			response.Unauthorized(c, fmt.Sprintf("Authentication failed: %v", err))
			return
		}
		password = decrypted
	}

	if password == "" {
		response.BadRequest(c, "Password is required")
		return
	}

	cfg := config.GlobalConfig
	if req.Username != cfg.Server.Username || password != cfg.Server.Password {
		response.Unauthorized(c, "Invalid username or password")
		return
	}

	token, err := middleware.GenerateToken(req.Username)
	if err != nil {
		response.InternalServerError(c, "Failed to generate authentication token")
		return
	}

	response.Success(c, gin.H{
		"token": token,
	})
}
