package connector

import "testing"

// A site's own category can be wrong: FileMood filed "Marvel Avengers ...
// Crack Patch" under movies, so the category filter let software through.
func TestDropNonVideoReleases(t *testing.T) {
	software := []string{
		"Marvel Avengers The Definitive Edition Crack Patch one cli",
		"Microsoft Office Professional Plus 2021 v2109 Build 14332 x64",
		"Adobe Premiere Pro v31265 Lego Patch",
		"Utweb installer exe",
		"Revo Uninstaller Pro Crack",
		"Windows 11 Pro 22H2 Activator",
	}
	films := []string{
		"Squid.Game.S03E01.1080p.WEB.h264-ETHEL",
		"Avengers: Endgame (2019) BDRip 1080p",
		"Squid Game Season 1 S01 2021 1080p Dual Audio",
	}

	releases := make([]Release, 0, len(software)+len(films))
	for _, s := range software {
		releases = append(releases, Release{Title: s})
	}
	for _, f := range films {
		releases = append(releases, Release{Title: f})
	}

	kept := DropNonVideoReleases(releases)
	if len(kept) != len(films) {
		t.Fatalf("kept = %d, want %d films", len(kept), len(films))
	}
	for _, rel := range kept {
		if LooksLikeNonVideo(rel.Title) {
			t.Errorf("a non-video release survived: %q", rel.Title)
		}
	}
	// 每部影片都必须留下。
	for _, want := range films {
		found := false
		for _, rel := range kept {
			if rel.Title == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("a real film was dropped: %q", want)
		}
	}
}

// A film whose title contains a marker word must survive, because release tags
// prove it is video.
func TestDropNonVideoKeepsFilmWithMarkerWord(t *testing.T) {
	releases := []Release{
		{Title: "The Repack (2020) 1080p WEB-DL x264"},
		{Title: "Patch Adams (1998) BluRay 1080p"},
	}
	kept := DropNonVideoReleases(releases)
	if len(kept) != 2 {
		t.Fatalf("kept = %d, want both films kept: %+v", len(kept), kept)
	}
}

// An all-video batch must be returned untouched (same slice, no allocation churn
// on the hot path).
func TestDropNonVideoLeavesCleanBatchAlone(t *testing.T) {
	releases := []Release{
		{Title: "Show S01E01 1080p WEB-DL"},
		{Title: "Show S01E02 1080p WEB-DL"},
	}
	kept := DropNonVideoReleases(releases)
	if len(kept) != 2 {
		t.Fatalf("kept = %d", len(kept))
	}
}

// Guard against over-reach: an empty or single-item batch is never touched.
func TestDropNonVideoEdgeCases(t *testing.T) {
	if got := DropNonVideoReleases(nil); len(got) != 0 {
		t.Errorf("nil input = %d", len(got))
	}
	if got := DropNonVideoReleases([]Release{{Title: "Some Film"}}); len(got) != 1 {
		t.Errorf("unclassified film = %d, want 1", len(got))
	}
}
