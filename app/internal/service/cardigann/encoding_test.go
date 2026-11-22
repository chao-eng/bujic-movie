package cardigann

import (
	"bytes"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

// A tracker that declares windows-1251 must have its body converted before
// extraction, otherwise titles and dates arrive as mojibake.
func TestDecodeLegacyCharset(t *testing.T) {
	original := []byte("5 октября 2024 г. 10:23:04 +03:00")
	encoded, err := charmap.Windows1251.NewEncoder().Bytes(original)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}

	if got := Decode(encoded, "windows-1251"); !bytes.Equal(got, original) {
		t.Errorf("decoded = %q, want %q", got, original)
	}

	// Already UTF-8 is passed through untouched.
	utf8Body := []byte("Дюна 2021")
	if got := Decode(utf8Body, "windows-1251"); !bytes.Equal(got, utf8Body) {
		t.Errorf("utf-8 body was converted: %q", got)
	}

	// An unknown charset leaves the body alone rather than guessing.
	if got := Decode(encoded, "no-such-charset"); !bytes.Equal(got, encoded) {
		t.Errorf("unknown charset must pass through")
	}
}
