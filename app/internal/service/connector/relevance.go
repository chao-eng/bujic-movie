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
// It requires the whole phrase, or one token of at least three characters
// (two for CJK, which is already a word).
func MatchesKeyword(title, keyword string) bool {
	title = strings.ToLower(title)
	phrase := strings.ToLower(strings.Join(strings.Fields(keyword), " "))
	if phrase == "" {
		return true
	}
	if strings.Contains(title, phrase) {
		return true
	}
	judged := false
	for _, token := range strings.Fields(phrase) {
		if len([]rune(token)) < minTokenRunes(token) {
			continue
		}
		judged = true
		if strings.Contains(title, token) {
			return true
		}
	}
	// Every token was too short to judge (for example "of"): do not drop.
	return !judged
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
	if strings.TrimSpace(keyword) == "" || len(releases) < RelevanceMinBatch {
		return releases, false
	}
	for _, rel := range releases {
		if MatchesKeyword(rel.Title, keyword) {
			return releases, false
		}
	}
	return nil, true
}
