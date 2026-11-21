package connector

import (
	"strings"
	"testing"
)

func rel(title string, size int64, seeders int, indexer string) Release {
	return Release{
		Title:        title,
		SizeBytes:    size,
		Seeders:      seeders,
		IndexerID:    uint(len(indexer)),
		IndexerName:  indexer,
		DownloadURL:  "magnet:?xt=urn:btih:" + title,
		DownloadType: "magnet",
	}
}

// TST-030 / BR-07: same title, size within 2% merges into one card listing all
// sources.
func TestTST030_GroupCollapsesNearEqualSizes(t *testing.T) {
	merged := GroupReleases([]Release{
		rel("Show.S01E01.1080p-WEB", 1_000_000_000, 10, "A"),
		rel("show s01e01 1080p web", 1_015_000_000, 42, "B"),
		rel("Show S01E01 1080p WEB", 1_018_000_000, 3, "C"),
	})
	if len(merged) != 1 {
		t.Fatalf("merged = %d, want 1", len(merged))
	}
	if len(merged[0].Sources) != 3 {
		t.Fatalf("sources = %d, want 3", len(merged[0].Sources))
	}
	if merged[0].Seeders != 42 {
		t.Errorf("kept seeders = %d, want the highest (42)", merged[0].Seeders)
	}
	if merged[0].IndexerName != "B" {
		t.Errorf("kept indexer = %q, want B", merged[0].IndexerName)
	}
}

// TST-030: sizes beyond the 2% tolerance stay separate.
func TestTST030_GroupKeepsDifferentSizes(t *testing.T) {
	merged := GroupReleases([]Release{
		rel("Show.S01E01.1080p", 1_000_000_000, 10, "A"),
		rel("Show.S01E01.1080p", 1_400_000_000, 10, "B"),
	})
	if len(merged) != 2 {
		t.Fatalf("merged = %d, want 2", len(merged))
	}
}

// TST-030: Chinese titles are preserved and different seasons stay apart.
func TestTST030_GroupKeepsCJK(t *testing.T) {
	merged := GroupReleases([]Release{
		rel("怪奇物语 第四季 1080p", 5_000_000_000, 10, "A"),
		rel("怪奇物语 第三季 1080p", 5_000_000_000, 10, "B"),
	})
	if len(merged) != 2 {
		t.Fatalf("merged = %d, want 2", len(merged))
	}
	if !strings.Contains(merged[0].Title, "怪奇物语") {
		t.Fatalf("CJK title lost: %q", merged[0].Title)
	}
}

// TST-030: unknown (zero) size merges with the same title.
func TestTST030_GroupTreatsZeroSizeAsUnknown(t *testing.T) {
	merged := GroupReleases([]Release{
		rel("Same Title", 0, 5, "A"),
		rel("Same Title", 1234567, 9, "B"),
	})
	if len(merged) != 1 {
		t.Fatalf("merged = %d, want 1", len(merged))
	}
}

func TestTST030_FingerprintIsExact(t *testing.T) {
	a := Fingerprint("Show.S01E01", 1000)
	b := Fingerprint("Show.S01E01", 1001)
	if a == b {
		t.Fatal("grab fingerprint must be exact so a different encode is a different grab")
	}
	if Fingerprint("  Show S01E01 ", 1000) != a {
		t.Fatal("fingerprint must normalise punctuation and case")
	}
}

// TST-029 / BR-18: default sort is seeders descending with zero-seeder last.
func TestTST029_SortDefaultPutsZeroSeedersLast(t *testing.T) {
	cards := []MergedRelease{
		{Release: Release{Title: "none", Seeders: 0}},
		{Release: Release{Title: "low", Seeders: 5}},
		{Release: Release{Title: "high", Seeders: 50}},
	}
	SortReleases(cards, "")
	if cards[0].Title != "high" || cards[1].Title != "low" || cards[2].Title != "none" {
		t.Fatalf("order = %v", []string{cards[0].Title, cards[1].Title, cards[2].Title})
	}
}

func TestTST029_SortOtherKeys(t *testing.T) {
	cards := []MergedRelease{
		{Release: Release{Title: "a", SizeBytes: 10, PublishDate: "2026-01-01", IndexerName: "z"}},
		{Release: Release{Title: "b", SizeBytes: 30, PublishDate: "2026-03-01", IndexerName: "a"}},
	}

	bySize := append([]MergedRelease{}, cards...)
	SortReleases(bySize, "size")
	if bySize[0].Title != "b" {
		t.Errorf("size sort = %v", bySize[0].Title)
	}

	byDate := append([]MergedRelease{}, cards...)
	SortReleases(byDate, "date")
	if byDate[0].Title != "b" {
		t.Errorf("date sort = %v", byDate[0].Title)
	}

	byIndexer := append([]MergedRelease{}, cards...)
	SortReleases(byIndexer, "indexer")
	if byIndexer[0].Title != "b" {
		t.Errorf("indexer sort = %v", byIndexer[0].Title)
	}
}
