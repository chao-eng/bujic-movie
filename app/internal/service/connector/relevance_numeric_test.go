package connector

import "testing"

// A bare number in an alternative title must not make an unrelated batch look
// relevant. TMDB lists "100 Days of Lies" for 百日谎言, and "100" appears in
// countless unrelated release names, so a whole JAV batch slipped through.
func TestMatchesKeywordIgnoresNumericTokens(t *testing.T) {
	jav := "Nakashima Minami - In The Childhood Home Where Everyone Else 100"
	if MatchesKeyword(jav, "100 Days of Lies") {
		t.Error("a bare number must not match on its own")
	}
	// The real title still matches through the phrase check.
	if !MatchesKeyword("100 Days of Lies 2024 1080p", "100 Days of Lies") {
		t.Error("the full phrase must still match")
	}
	// A lone shared word is not enough once the number is skipped: the alias has
	// three words, so one hit must not qualify.
	if MatchesKeyword("Some Release About Lies", "100 Days of Lies") {
		t.Error("a single shared word out of three must not count")
	}
}

// Guarding against over-correction: a title that really is numeric must not be
// dropped wholesale, because there is nothing left to judge on.
func TestMatchesKeywordNumericOnlyQuery(t *testing.T) {
	if !MatchesKeyword("Anything At All", "1917") {
		t.Error("a purely numeric query cannot judge, so it must not drop")
	}
	if !MatchesKeyword("1917 2019 1080p", "1917") {
		t.Error("the phrase check still matches a numeric title")
	}
}

// A JAV batch answering a Chinese drama keyword must be dropped as a whole.
func TestDropIrrelevantNumericAliasBatch(t *testing.T) {
	releases := make([]Release, 0, 12)
	for i := 0; i < 12; i++ {
		releases = append(releases, Release{Title: "Hoshi Maria - Maria Hoshi explodes the sexual 100"})
	}
	// The alias list TMDB produced for 百日谎言, including the numeric prefix.
	keywords := []string{"百日谎言", "100일의 거짓말", "Hundred Days of Lies", "100 Days of Lies"}
	kept, dropped := DropIrrelevantBatchFor(releases, keywords)
	if !dropped {
		t.Fatalf("kept = %d, want the batch dropped", len(kept))
	}
	if len(kept) != 0 {
		t.Errorf("kept %d releases", len(kept))
	}
}

// The guard must not eat legitimate small batches.
func TestDropIrrelevantKeepsSmallBatch(t *testing.T) {
	releases := []Release{
		{Title: "Hundred Days of Lies S01E01"},
		{Title: "unrelated thing"},
	}
	kept, dropped := DropIrrelevantBatchFor(releases, []string{"Hundred Days of Lies"})
	if dropped || len(kept) != 2 {
		t.Fatalf("dropped=%v kept=%d, want the batch kept", dropped, len(kept))
	}
}
