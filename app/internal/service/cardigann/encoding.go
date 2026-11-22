package cardigann

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/transform"
)

// Decode converts a response body to UTF-8 using the definition's `encoding`.
//
// Many trackers serve windows-1251/1252 or Shift_JIS while their definition
// declares the charset. Without this the titles and dates arrive as mojibake
// ("5 ??????? 2024"), which is why those sites looked like they had no usable
// result data at all.
//
// The body is returned unchanged when it already decodes as UTF-8 or when the
// declared charset is unknown: a wrong conversion is worse than none.
func Decode(raw []byte, charsetName string) []byte {
	name := strings.TrimSpace(strings.ToLower(charsetName))
	if len(raw) == 0 || name == "" || name == "utf-8" || name == "utf8" {
		return raw
	}
	if utf8.Valid(raw) {
		return raw
	}
	enc := lookupEncoding(name)
	if enc == nil {
		return raw
	}
	decoded, _, err := transform.Bytes(enc.NewDecoder(), raw)
	if err != nil || len(decoded) == 0 {
		return raw
	}
	return decoded
}

// lookupEncoding resolves an IANA or common alias to an x/text encoding.
func lookupEncoding(name string) encoding.Encoding {
	// ianaindex does not know every tracker spelling, so map the usual gaps.
	switch name {
	case "windows-1251", "win-1251", "cp1251":
		name = "windows-1251"
	case "windows-1252", "win-1252", "cp1252":
		name = "windows-1252"
	}
	enc, err := ianaindex.MIME.Encoding(name)
	if err != nil || enc == nil {
		enc, err = ianaindex.IANA.Encoding(name)
		if err != nil {
			return nil
		}
	}
	return enc
}
