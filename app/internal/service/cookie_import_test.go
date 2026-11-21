package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	internalcrypto "github.com/bujic-movie/bujic-movie/internal/crypto"

	"github.com/bujic-movie/bujic-movie/internal/service/connector"
)

// TST-058 / AC-BUG010: a manual browser session is stored and used for requests.
func TestTST058_CookieImportStoresSession(t *testing.T) {
	env := newTestEnv(t)
	site := newCardigannSite(t, 1)
	defer site.Close()
	id := env.createSiteNamed(t, "挑战站点", site.URL)

	result, err := env.indexers.ImportCookies(context.Background(), CookieImportInput{
		Host:      hostOf(t, site.URL),
		Cookies:   "cf_clearance=abc123; PHPSESSID=xyz",
		UserAgent: "Mozilla/5.0 (Macintosh) Chrome/131",
	})
	if err != nil {
		t.Fatalf("ImportCookies: %v", err)
	}
	if !result.Updated || result.IndexerID != id {
		t.Fatalf("result = %+v", result)
	}

	// The stored cookie is masked on read: names only, never values.
	view, err := env.indexers.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(view.CookieNames) != 2 || view.CookieNames[0] != "cf_clearance" {
		t.Fatalf("cookie names = %v", view.CookieNames)
	}
	if view.UserAgent == "" {
		t.Fatal("user agent must be stored")
	}
	if fmt.Sprint(view.CookieMask) == "abc123" {
		t.Fatal("cookie values must not be returned")
	}

	// The connector must carry the session.
	conn, err := env.indexers.Connector(context.Background(), id)
	if err != nil {
		t.Fatalf("Connector: %v", err)
	}
	type sessionAware interface{ SetSession(connector.Session) }
	if _, ok := conn.(sessionAware); !ok {
		t.Fatal("connector must accept a manual session")
	}
}

// The extension may post only a URL; the host is derived from it.
func TestTST058_CookieImportDerivesHostFromURL(t *testing.T) {
	env := newTestEnv(t)
	site := newCardigannSite(t, 1)
	defer site.Close()
	env.createSiteNamed(t, "站点", site.URL)

	result, err := env.indexers.ImportCookies(context.Background(), CookieImportInput{
		URL:       site.URL + "/search.php?q=x",
		Cookies:   "cf_clearance=abc",
		UserAgent: "UA/1.0",
	})
	if err != nil {
		t.Fatalf("ImportCookies: %v", err)
	}
	if !result.Updated {
		t.Fatalf("result = %+v", result)
	}
}

// A host that matches several indexers returns the candidates instead of
// guessing, and an unknown host reports not-found.
func TestTST058_CookieImportAmbiguousHost(t *testing.T) {
	env := newTestEnv(t)
	site := newCardigannSite(t, 1)
	defer site.Close()
	env.createSiteNamed(t, "A", site.URL)
	env.createSiteNamed(t, "B", site.URL)

	result, err := env.indexers.ImportCookies(context.Background(), CookieImportInput{
		Host: hostOf(t, site.URL), Cookies: "a=b", UserAgent: "UA",
	})
	if err != nil {
		t.Fatalf("ImportCookies: %v", err)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(result.Candidates))
	}
	if result.Updated {
		t.Fatal("ambiguous host must not update anything")
	}

	unknown, err := env.indexers.ImportCookies(context.Background(), CookieImportInput{
		Host: "nowhere.example", Cookies: "a=b", UserAgent: "UA",
	})
	if err != nil {
		t.Fatalf("ImportCookies: %v", err)
	}
	if unknown.Updated || len(unknown.Candidates) != 0 {
		t.Fatalf("unknown host = %+v", unknown)
	}
}

// Missing UA is rejected: Cloudflare binds the clearance to it.
func TestTST058_CookieImportRequiresUserAgent(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.indexers.ImportCookies(context.Background(), CookieImportInput{
		Host: "x.example", Cookies: "a=b",
	})
	if err == nil {
		t.Fatal("missing user agent must be rejected")
	}
}

// Explicit indexer id bypasses host matching.
func TestTST058_CookieImportByID(t *testing.T) {
	env := newTestEnv(t)
	site := newCardigannSite(t, 1)
	defer site.Close()
	id := env.createSiteNamed(t, "站点", site.URL)

	result, err := env.indexers.ImportCookies(context.Background(), CookieImportInput{
		IndexerID: id, Cookies: "a=b", UserAgent: "UA",
	})
	if err != nil {
		t.Fatalf("ImportCookies: %v", err)
	}
	if !result.Updated || result.IndexerID != id {
		t.Fatalf("result = %+v", result)
	}
}

// NormaliseCookies accepts DevTools paste, header format and newlines.
func TestTST058_NormaliseCookies(t *testing.T) {
	cases := []string{
		"a=1; b=2",
		"a=1;\nb=2",
		"a=1\r\nb=2\r\n",
		"  a=1 ;  b=2 ",
	}
	for _, raw := range cases {
		if got := connector.NormaliseCookies(raw); got != "a=1; b=2" {
			t.Errorf("NormaliseCookies(%q) = %q", raw, got)
		}
	}
	if got := connector.NormaliseCookies("not a cookie"); got != "" {
		t.Errorf("garbage must be dropped, got %q", got)
	}
}

// TST-078 / BUG-010: the imported session is what makes a challenged site work.
// The stub answers 403 until the request carries the clearance cookie and the
// matching User-Agent, mirroring Cloudflare's binding.
func TestTST078_CookieSessionUnlocksChallenge(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "cf_clearance=abc123" || r.Header.Get("User-Agent") != "Browser/9.9" {
			w.Header().Set("Cf-Mitigated", "challenge")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("<html><title>Just a moment...</title></html>"))
			return
		}
		_, _ = w.Write([]byte(`<table class="torrents"><tbody>
			<tr><td><a class="title">Release</a><a class="download" href="/d/1">d</a></td>
			<td class="size">1 GB</td><td class="seeders">5</td><td class="cat" data-cat="2000">m</td></tr>
			</tbody></table>`))
	}))
	defer srv.Close()

	id := env.createSiteNamed(t, "挑战站点", srv.URL)

	// Before the import the site is challenged.
	before, err := env.indexers.Test(context.Background(), id)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if before.OK || before.ErrorKind != "challenge" {
		t.Fatalf("before import: %+v", before)
	}

	if _, err := env.indexers.ImportCookies(context.Background(), CookieImportInput{
		Host:      hostOf(t, srv.URL),
		Cookies:   "cf_clearance=abc123",
		UserAgent: "Browser/9.9",
	}); err != nil {
		t.Fatalf("ImportCookies: %v", err)
	}

	after, err := env.indexers.Test(context.Background(), id)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if !after.OK {
		t.Fatalf("after import: %+v", after)
	}
}

// BUG-016: editing a download client without re-typing the password must keep
// the stored one. The form never pre-fills secrets, so blank has to mean "keep".
func TestTST020_UpdateKeepsStoredPassword(t *testing.T) {
	env := newTestEnv(t)

	created, err := env.clients.Create(DownloadClientInput{
		Name: "NAS", Host: "127.0.0.1", Port: 19191,
		Username: "admin", Password: "s3cret",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.PasswordMask != "****cret" {
		t.Fatalf("mask = %q", created.PasswordMask)
	}

	// Edit everything except the password.
	updated, err := env.clients.Update(created.ID, DownloadClientInput{
		Name: "NAS", Host: "127.0.0.1", Port: 19191,
		Username: "admin", Password: "",
		DefaultCategory: "电影",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.PasswordMask != "****cret" {
		t.Fatalf("password was erased by an empty submit: mask = %q", updated.PasswordMask)
	}
	// The stored secret must still decrypt to the original value.
	stored, err := env.repo.GetClient(created.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	plain := mustDecrypt(env.box, stored.PasswordEnc)
	if plain != "s3cret" {
		t.Fatalf("stored password = %q, want s3cret", plain)
	}
}

// An explicit sentinel is the only way to clear a secret.
func TestTST020_UpdateClearsWithSentinel(t *testing.T) {
	env := newTestEnv(t)
	created, err := env.clients.Create(DownloadClientInput{
		Name: "NAS", Host: "127.0.0.1", Port: 19191, Username: "a", Password: "pw",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	updated, err := env.clients.Update(created.ID, DownloadClientInput{
		Name: "NAS", Host: "127.0.0.1", Port: 19191,
		Username: internalcrypto.ClearSentinel, Password: internalcrypto.ClearSentinel,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.PasswordMask != "" {
		t.Fatalf("password should be cleared, mask = %q", updated.PasswordMask)
	}
}

// The same rule applies to an indexer's API key and proxy.
func TestTST020_IndexerUpdateKeepsSecrets(t *testing.T) {
	env := newTestEnv(t)
	view, err := env.indexers.Create(IndexerInput{
		Name: "通用站点", Connector: "torznab",
		BaseURL: "https://example.invalid", APIKey: "abcdef123456",
		ProxyURL: "socks5h://127.0.0.1:1080",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if view.APIKeyMask == "" {
		t.Fatal("api key must be stored")
	}

	updated, err := env.indexers.Update(view.ID, IndexerInput{
		Name: "通用站点", Connector: "torznab",
		BaseURL: "https://example.invalid", APIKey: "", ProxyURL: "",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.APIKeyMask == "" {
		t.Error("api key was erased by an empty submit")
	}
	if updated.ProxyMask == "" {
		t.Error("proxy was erased by an empty submit")
	}
}
