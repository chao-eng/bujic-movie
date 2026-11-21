package connector

import "testing"

// BUG-008 / NFR-01: a site that ignores the keyword must not pollute results.
func TestTST063_DropIrrelevantBatch(t *testing.T) {
	keyword := "Oppenheimer"
	irrelevant := make([]Release, 0, 6)
	for i := 0; i < 6; i++ {
		irrelevant = append(irrelevant, Release{Title: "SNOS-462-U 无码破解 无水印版"})
	}

	kept, dropped := DropIrrelevantBatch(irrelevant, keyword)
	if !dropped {
		t.Fatal("an irrelevant batch must be dropped")
	}
	if len(kept) != 0 {
		t.Fatalf("kept = %d, want 0", len(kept))
	}

	mixed := []Release{
		{Title: "Oppenheimer 2023 2160p"},
		{Title: "SNOS-462-U 无码破解"},
	}
	kept, dropped = DropIrrelevantBatch(mixed, keyword)
	if dropped || len(kept) != 2 {
		t.Fatalf("mixed batch must be kept, got %d dropped=%v", len(kept), dropped)
	}
}

func TestTST063_MatchesKeyword(t *testing.T) {
	cases := []struct {
		title, keyword string
		want           bool
	}{
		{"Oppenheimer 2023 2160p", "Oppenheimer", true},
		{"[DBD-Raws][奥本海默/Oppenheimer]", "Oppenheimer", true},
		{"Sousou no Frieren S01E05", "Frieren", true},
		{"葬送的芙莉蓮 第29話", "芙莉蓮", true},
		{"One Piece 海賊王 1180", "One Piece", true},
		{"SNOS-462-U 无码破解", "Oppenheimer", false},
		{"Unrelated Release", "of", true}, // too short to judge: never dropped
		{"anything", "", true},            // empty keyword means latest feed
	}
	for _, c := range cases {
		if got := MatchesKeyword(c.title, c.keyword); got != c.want {
			t.Errorf("MatchesKeyword(%q, %q) = %v, want %v", c.title, c.keyword, got, c.want)
		}
	}
}

// A small batch is never dropped: a site may legitimately return one oddly
// named release.
func TestTST063_SmallBatchKept(t *testing.T) {
	kept, dropped := DropIrrelevantBatch([]Release{{Title: "odd name"}}, "Oppenheimer")
	if dropped || len(kept) != 1 {
		t.Fatalf("small batch dropped=%v kept=%d", dropped, len(kept))
	}
}
