package service

import (
	"encoding/base64"
	"strings"

	"github.com/saintfish/chardet"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// base64Encode / base64Decode wrap stdlib base64.
func base64Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// decodeToUTF8 detects the charset of b and transcodes to UTF-8. Returns the
// decoded text and the detected original charset ("utf-8" when already UTF-8).
// Falls back to raw pass-through with charset "unknown" on failure.
func decodeToUTF8(b []byte) (string, string) {
	if isUTF8(b) {
		return string(b), "utf-8"
	}

	det := chardet.NewTextDetector()
	res, err := det.DetectBest(b)
	charset := "unknown"
	if err == nil {
		charset = strings.ToLower(res.Charset)
	}

	if charset == "utf-16le" || charset == "utf-16be" || charset == "utf-16" {
		dec := unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewDecoder()
		if out, _, derr := transform.Bytes(dec, b); derr == nil {
			return string(out), charset
		}
	}

	if enc := charsetEncoder(charset); enc != nil {
		if out, _, derr := transform.Bytes(enc.NewDecoder(), b); derr == nil && looksReadable(string(out)) {
			return string(out), charset
		}
	}

	return string(b), "unknown"
}

func charsetEncoder(name string) encoding.Encoding {
	if name == "" {
		return nil
	}
	enc, err := ianaindex.MIME.Encoding(name)
	if err != nil {
		return nil
	}
	return enc
}

func isUTF8(b []byte) bool {
	return strings.ToValidUTF8(string(b), "") == string(b)
}

func looksReadable(s string) bool {
	bad := 0
	for _, r := range s {
		if r == 0xFFFD || r < 9 {
			bad++
		}
	}
	return bad < 8
}
