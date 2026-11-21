package crypto

import (
	"strings"
	"testing"
)

// TST-020 / TST-058 / NFR-07: credential encryption, masking and key rotation.
func TestTST020_SecretBoxRoundTrip(t *testing.T) {
	box, err := NewSecretBox("test-secret-key")
	if err != nil {
		t.Fatalf("NewSecretBox: %v", err)
	}

	enc, err := box.Encrypt("hunter2-pass")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if enc == "" || strings.Contains(enc, "hunter2") {
		t.Fatalf("ciphertext must be non-empty and must not contain plaintext, got %q", enc)
	}

	got, err := box.Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "hunter2-pass" {
		t.Fatalf("round trip mismatch: got %q", got)
	}
}

func TestTST020_SecretBoxEmptyValues(t *testing.T) {
	box, _ := NewSecretBox("k")
	enc, err := box.Encrypt("")
	if err != nil || enc != "" {
		t.Fatalf("empty encrypt: got %q err %v", enc, err)
	}
	dec, err := box.Decrypt("")
	if err != nil || dec != "" {
		t.Fatalf("empty decrypt: got %q err %v", dec, err)
	}
}

// NFR-07: rotating server.secret_key must fail closed, not return garbage.
func TestTST020_SecretBoxKeyRotationFailsClosed(t *testing.T) {
	box1, _ := NewSecretBox("key-one")
	box2, _ := NewSecretBox("key-two")

	enc, err := box1.Encrypt("secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := box2.Decrypt(enc); err == nil {
		t.Fatal("decrypt with a different key must fail")
	}
}

func TestTST020_SecretBoxRejectsEmptyKey(t *testing.T) {
	if _, err := NewSecretBox("   "); err == nil {
		t.Fatal("empty secret must be rejected")
	}
}

// TST-020: read APIs return a mask that keeps only the last four characters.
func TestTST020_Mask(t *testing.T) {
	cases := map[string]string{
		"":                 "",
		"abc":              "****",
		"abcd":             "****",
		"abcde":            "****bcde",
		"0123456789abcdef": "****cdef",
	}
	for in, want := range cases {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
}

// TST-006 / TST-020: the sentinel keeps the stored value on update.
func TestTST006_ResolveSentinel(t *testing.T) {
	if got := Resolve(MaskSentinel, "stored"); got != "stored" {
		t.Fatalf("sentinel must keep stored value, got %q", got)
	}
	if got := Resolve("new", "stored"); got != "new" {
		t.Fatalf("explicit value must replace stored value, got %q", got)
	}
	if got := Resolve("", "stored"); got != "" {
		t.Fatalf("empty submits empty (clear), got %q", got)
	}
}
