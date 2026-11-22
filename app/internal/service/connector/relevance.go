package connector

import (
	"strings"
	"unicode"
)

// Relevance threshold: a site that returns at least this many releases while
// matching none of the query terms is assumed to have ignored the keyword (its
// definition broke upstream), so its whole batch is dropped instead of polluting
// the merged list with unrelated results (BUG-008).
const RelevanceMinBatch = 5

// MatchesKeyword reports whether a release title plausibly answers the query.
//
// It accepts the whole phrase, or a substantial share of the query's word
// tokens. Two rules keep unrelated batches out:
//
//   - A token must sit on a word boundary. Substring matching let "day" match
//     "Days" and "lie" match "Lies", so a JAV batch answered a Korean drama
//     query through "2 Days in Guadalajara".
//   - One token out of a multi-word title is not enough when the title is long.
//     "Hundred Days of Lies" matched "A Hundred Lies (2024)" on two words, which
//     is fine, but it also matched "Waterloo - The Last Hundred Days" on one.
//
// Short queries (one or two tokens) stay permissive: "Oppenheimer" and
// "Squid Game" must match on their single meaningful word.
func MatchesKeyword(title, keyword string) bool {
	titleLower := strings.ToLower(title)
	phrase := strings.ToLower(strings.Join(strings.Fields(keyword), " "))
	if phrase == "" {
		return true
	}
	if strings.Contains(titleLower, phrase) {
		return true
	}

	// Words of the query that are worth matching on.
	tokens := make([]string, 0, 6)
	rawTokens := 0
	for _, token := range strings.Fields(phrase) {
		rawTokens++
		if isNumericToken(token) {
			continue
		}
		if len([]rune(token)) < minTokenRunes(token) {
			continue
		}
		tokens = append(tokens, token)
	}
	if len(tokens) == 0 {
		// Nothing to judge on (all numeric or too short): never drop.
		return true
	}

	titleWords := splitWords(titleLower)
	hits := 0
	for _, token := range tokens {
		if containsWord(titleWords, token) {
			hits++
		}
	}
	if hits == 0 {
		return false
	}
	// Permissive only for genuinely short queries like "Oppenheimer" or
	// "Squid Game". A title such as "100 Days of Lies" has three words even
	// though the number is skipped, so a single "days" hit must not qualify —
	// that was letting an adult batch answer a Korean drama query.
	if rawTokens <= 2 {
		return true
	}
	// A longer title needs a clear majority of its words present.
	return hits*2 > len(tokens)
}

// splitWords lower-cases and splits a title into alphanumeric words so token
// matching respects boundaries. CJK text has no spaces, so each CJK rune is kept
// as its own word.
func splitWords(s string) []string {
	out := make([]string, 0, 16)
	var buf []rune
	flush := func() {
		if len(buf) > 0 {
			// Copy: reusing buf's backing array would let the next word overwrite
			// this one (that silently turned "hundred" into "undred").
			out = append(out, string(buf))
			buf = nil
		}
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			buf = append(buf, r)
		case r > unicode.MaxASCII:
			// CJK and other non-ASCII scripts are words on their own.
			flush()
			out = append(out, string(r))
		default:
			flush()
		}
	}
	flush()
	return out
}

// containsWord reports whether the word list holds the token. When the token is
// itself multi-word (CJK alternative titles arrive that way), its runes are
// matched individually.
func containsWord(words []string, token string) bool {
	for _, word := range words {
		if word == token {
			return true
		}
	}
	// CJK titles have no separators, so compare rune by rune.
	if len([]rune(token)) >= 2 && !isASCII(token) {
		return strings.Contains(strings.Join(words, ""), token)
	}
	return false
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

// isNumericToken reports whether a token carries no word characters. Pure
// numbers ("100", "2", "2024") and number-plus-punctuation forms are excluded
// from single-token matching.
func isNumericToken(token string) bool {
	hasDigit := false
	for _, r := range token {
		switch {
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsLetter(r), r > unicode.MaxASCII:
			return false
		}
	}
	return hasDigit
}

// nonVideoMarkers are words that identify software, games, ebooks or adult
// material rather than a film or show. They matter because a site's own category
// is sometimes wrong: FileMood tagged "Marvel Avengers ... Crack Patch" as a
// movie, so the category filter alone let it through.
var nonVideoMarkers = []string{
	// software / cracks
	" crack", "cracked", "patch", "keygen", "serial key", "license key",
	"installer", "installer.exe", " portable", "repack", "pre-activated",
	"activator", "uninstaller", "antivirus", "software",
	" loader", "windows loader", "windowsloader",
	"registration", "torrent downloader",
	// explicit application names
	"microsoft office", "adobe ", "photoshop", "premiere pro", "after effects",
	"windows 1", "windows 2", "windows.loader", "office professional",
	"acrobat", "autocad", "vmware", "virtualbox",
	// games
	" pc game", " repack-", "codex", "plaza", "fitgirl", "skidrow", "elamigos",
	"gog.com", "steamrip",
	// ebooks / comics
	" epub", " mobi", " azw3", " pdf ", " audiobook",
}

// videoMarkers are strong signals that a release really is a film or episode.
// They win over a stray marker such as a "repack" that a legitimate release may
// mention.
var videoMarkers = []string{
	"1080p", "2160p", "720p", "480p", "4k", "uhd", "hdr",
	"webrip", "web-dl", "webdl", "bluray", "blu-ray", "bdrip", "brrip",
	"hdtv", "dvdrip", "remux", "x264", "x265", "h264", "h265", "hevc", "av1",
	"aac", "ddp", "dts", "atmos", "truehd",
	"s01", "s02", "s03", "s04", "s05", "e01", "e02", "season",
	"complete", "imax", "proper", "extended",
}

// LooksLikeVideo reports whether a release title carries film/episode signals.
func LooksLikeVideo(title string) bool {
	lower := strings.ToLower(title)
	for _, marker := range videoMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// LooksLikeNonVideo reports whether a title is clearly software, a game or an
// ebook. It yields to LooksLikeVideo so a film whose name contains "Repack"
// is not mistaken for software.
func LooksLikeNonVideo(title string) bool {
	lower := strings.ToLower(title)
	// Normalise separators so "windows.loader" and "windows loader" match alike.
	normalised := strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(lower)
	matched := false
	for _, marker := range nonVideoMarkers {
		if strings.Contains(normalised, marker) || strings.Contains(lower, marker) {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	return !LooksLikeVideo(lower)
}

// MatchesAnyKeyword reports whether the title answers any of the accepted
// keywords. It exists because a Chinese query resolves to several titles (the
// localized one plus TMDB's original/en/ko alternatives) and a site may answer
// with any of them.
func MatchesAnyKeyword(title string, keywords []string) bool {
	accepted := false
	for _, keyword := range keywords {
		if strings.TrimSpace(keyword) == "" {
			continue
		}
		accepted = true
		if MatchesKeyword(title, keyword) {
			return true
		}
	}
	// No usable keyword was supplied: do not drop the batch.
	return !accepted
}

// minTokenRunes avoids matching on very short tokens such as "of" or "a".
func minTokenRunes(token string) int {
	for _, r := range token {
		if r > unicode.MaxASCII {
			return 2
		}
	}
	return 3
}

// DropIrrelevantBatch removes a whole site batch when the site clearly ignored
// the keyword. It returns the kept releases and whether the batch was dropped.
func DropIrrelevantBatch(releases []Release, keyword string) ([]Release, bool) {
	return DropIrrelevantBatchFor(releases, []string{keyword})
}

// DropIrrelevantBatchFor is DropIrrelevantBatch with several accepted keywords,
// so a batch answering any resolved alias is kept.
func DropIrrelevantBatchFor(releases []Release, keywords []string) ([]Release, bool) {
	if len(releases) < RelevanceMinBatch {
		return releases, false
	}
	usable := false
	for _, keyword := range keywords {
		if strings.TrimSpace(keyword) != "" {
			usable = true
			break
		}
	}
	if !usable {
		return releases, false
	}
	for _, rel := range releases {
		if MatchesAnyKeyword(rel.Title, keywords) {
			return releases, false
		}
	}
	return nil, true
}

// DropNonVideoReleases removes individual releases that are clearly software,
// games or ebooks rather than a film or episode.
//
// It filters per release instead of dropping a whole batch because a site can
// mix them: FileMood returned genuine Squid Game episodes alongside "Marvel
// Avengers ... Crack Patch". Its own category said "movie" for both, so the
// category filter alone cannot catch this.
//
// Titles that carry a resolution/release tag are trusted, so a film whose name
// contains a marker word ("The Repack") survives.
func DropNonVideoReleases(releases []Release) []Release {
	kept := make([]Release, 0, len(releases))
	dropped := 0
	for _, rel := range releases {
		if LooksLikeNonVideo(rel.Title) {
			dropped++
			continue
		}
		kept = append(kept, rel)
	}
	if dropped == 0 {
		return releases
	}
	return kept
}
