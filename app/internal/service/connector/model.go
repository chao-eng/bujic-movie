package connector

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Error kinds shared by every connector and surfaced to the UI.
const (
	KindAuth        = "auth"
	KindUnreachable = "unreachable"
	KindTimeout     = "timeout"
	KindParse       = "parse"
	KindCaptcha     = "captcha"
	KindBlocked     = "blocked"
	KindChallenge   = "challenge"
	KindUnsupported = "unsupported"
)

// Error is a classified connector failure with a user-facing message and an
// optional field pointing at the definition element that failed (BR-25).
type Error struct {
	Kind    string
	Message string
	Field   string
	Err     error
}

func (e *Error) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s (%s): %s", e.Kind, e.Field, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// NewError builds a classified error.
func NewError(kind, message, field string, err error) *Error {
	return &Error{Kind: kind, Message: message, Field: field, Err: err}
}

// Capabilities is what a connector reports after a successful test.
type Capabilities struct {
	Name         string
	Protocol     string // torrent
	Categories   int
	SearchModes  []string
	NeedsLogin   bool
	NeedsCaptcha bool
	Elapsed      time.Duration
	LoginState   string
}

// Query is the normalised search input.
type Query struct {
	Keyword    string
	Categories []int
	Season     int
	Episode    int
	Year       int
	IMDBID     string
	TMDBID     string
	Page       int
	Limit      int
	Mode       string // search / tv-search / movie-search
}

// Release is one normalised search result.
type Release struct {
	Title        string
	DownloadURL  string
	InfoHash     string
	SizeBytes    int64
	Seeders      int
	Peers        int
	PublishDate  string
	Categories   []int
	DownloadType string
	IndexerID    uint
	IndexerName  string
	// DetailsURL is the site's page for this release, when known.
	DetailsURL string
}

// DownloadRef is the resolved payload handed to a download client.
type DownloadRef struct {
	URL      string
	InfoHash string
	Type     string // magnet / torrent
	Title    string
}

// Connector is implemented by every indexer kind. SearchService depends only on
// this interface, so adding a protocol never touches the search pipeline.
type Connector interface {
	// Test validates connectivity and login, returning capabilities.
	Test(ctx context.Context) (*Capabilities, error)
	// Search runs one query and returns normalised releases.
	Search(ctx context.Context, q Query) ([]Release, error)
	// ResolveDownload expands a release into a downloadable reference. It may
	// fetch a detail page, so it honours the same timeout budget.
	ResolveDownload(ctx context.Context, rel Release) (*DownloadRef, error)
	// Name identifies the indexer in logs and results.
	Name() string
}

// ErrCaptchaRequired signals that the user must enter a captcha before login
// can complete (D-09).
var ErrCaptchaRequired = errors.New("connector: captcha required")

func normaliseTitle(title string) string {
	lower := make([]rune, 0, len(title))
	lastSpace := false
	for _, r := range toLowerRunes(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r > 127:
			lower = append(lower, r)
			lastSpace = false
		default:
			if !lastSpace && len(lower) > 0 {
				lower = append(lower, ' ')
				lastSpace = true
			}
		}
	}
	for len(lower) > 0 && lower[len(lower)-1] == ' ' {
		lower = lower[:len(lower)-1]
	}
	return string(lower)
}

func toLowerRunes(s string) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			out = append(out, r+32)
			continue
		}
		out = append(out, r)
	}
	return out
}
