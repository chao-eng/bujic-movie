package crypto

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
)

// MaskSentinel is submitted by clients to keep an existing secret unchanged.
// Empty input means the same thing on update, so a form that never pre-fills a
// password cannot silently erase it.
const MaskSentinel = "__unchanged__"

// ClearSentinel is the explicit way to remove a stored secret.
const ClearSentinel = "__clear__"

// ErrNoKey is returned when the box has no usable key material.
var ErrNoKey = errors.New("secret box: empty key")

// SecretBox encrypts credentials with AES-256-GCM. The key is derived from the
// server secret key with a domain-separated SHA-256 digest, so the JWT signing
// key is never used directly as an encryption key.
type SecretBox struct {
	aead cipher.AEAD
}

// NewSecretBox derives a domain-separated key from secret and returns a box.
func NewSecretBox(secret string) (*SecretBox, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, ErrNoKey
	}
	sum := sha256.Sum256([]byte("bujic.search.secret.v1|" + secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretBox{aead: aead}, nil
}

// Encrypt returns base64(nonce||ciphertext||tag). Empty input yields empty
// output so callers can store "not configured" without a special case.
func (b *SecretBox) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt reverses Encrypt. An empty ciphertext yields an empty string.
func (b *SecretBox) Decrypt(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("secret box: invalid encoding: %w", err)
	}
	ns := b.aead.NonceSize()
	if len(raw) < ns+b.aead.Overhead() {
		return "", errors.New("secret box: ciphertext too short")
	}
	plain, err := b.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("secret box: decrypt failed: %w", err)
	}
	return string(plain), nil
}

// Mask renders a secret for read APIs: only the last four characters survive.
func Mask(secret string) string {
	if secret == "" {
		return ""
	}
	runes := []rune(secret)
	if len(runes) <= 4 {
		return "****"
	}
	return "****" + string(runes[len(runes)-4:])
}

// Resolve keeps the stored secret when the submitted value is the mask
// sentinel, otherwise returns the submitted value.
func Resolve(submitted, stored string) string {
	if submitted == MaskSentinel {
		return stored
	}
	return submitted
}

// ResolveForUpdate is Resolve with "leave blank means keep" semantics: a form
// that is not pre-filled with the secret cannot wipe it by submitting an empty
// string. Clearing is explicit (ClearSentinel) so it is never accidental.
func ResolveForUpdate(submitted, stored string) string {
	switch submitted {
	case MaskSentinel, "":
		return stored
	case ClearSentinel:
		return ""
	}
	return submitted
}
