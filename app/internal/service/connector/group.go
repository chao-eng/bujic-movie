package connector

import (
	"fmt"
	"sort"
	"strings"
)

// SizeTolerance is the relative size difference under which two releases with
// the same normalised title are considered the same resource (BR-07).
const SizeTolerance = 0.02

// MergedRelease is one deduped card with every source that carried it.
type MergedRelease struct {
	Release
	Sources []Source
	// CategoryNames are the human labels for Release.Categories.
	CategoryNames []string
}

// Source is one indexer that returned the merged card.
type Source struct {
	IndexerID    uint
	IndexerName  string
	DownloadURL  string
	InfoHash     string
	Seeders      int
	SeedersKnown bool
	Peers        int
	Grabs        int
	SizeBytes    int64
	DetailsURL   string
}

// Fingerprint identifies a grab attempt. It uses the exact size so two
// different encodes of the same title are not treated as the same download.
// Dedupe across indexers uses GroupReleases, which applies the 2% tolerance.
func Fingerprint(title string, sizeBytes int64) string {
	return fmt.Sprintf("%s|%d", normaliseTitle(title), sizeBytes)
}

// GroupReleases collapses releases that share a normalised title and a size
// within SizeTolerance. The surviving entry keeps the highest seeder count and
// lists every indexer as a source. Order is stable: first appearance wins.
func GroupReleases(releases []Release) []MergedRelease {
	groups := make([]*MergedRelease, 0, len(releases))
	index := map[string][]*MergedRelease{}

	for _, rel := range releases {
		titleKey := normaliseTitle(rel.Title)
		merged := false
		for _, candidate := range index[titleKey] {
			if withinTolerance(candidate.SizeBytes, rel.SizeBytes) {
				mergeInto(candidate, rel)
				merged = true
				break
			}
		}
		if merged {
			continue
		}
		card := &MergedRelease{Release: rel}
		card.Sources = []Source{sourceOf(rel)}
		index[titleKey] = append(index[titleKey], card)
		groups = append(groups, card)
	}

	out := make([]MergedRelease, 0, len(groups))
	for _, g := range groups {
		if len(g.CategoryNames) == 0 {
			g.CategoryNames = CategoryNames(g.Categories)
		}
		out = append(out, *g)
	}
	return out
}

func mergeInto(card *MergedRelease, rel Release) {
	if card.DetailsURL == "" {
		card.DetailsURL = rel.DetailsURL
	}
	card.Sources = append(card.Sources, sourceOf(rel))
	// The card takes the source with the most seeders, but a site that reports
	// seeders beats one that does not: an unknown count must not overwrite a
	// real 0 with the same value and hide the fact that data exists.
	if betterSeedSource(card.Release, rel) {
		card.Seeders = rel.Seeders
		card.Peers = rel.Peers
		card.SeedersKnown = rel.SeedersKnown
		card.DownloadURL = rel.DownloadURL
		card.InfoHash = rel.InfoHash
		card.IndexerID = rel.IndexerID
		card.IndexerName = rel.IndexerName
		card.DownloadType = rel.DownloadType
	}
	if card.SizeBytes == 0 {
		card.SizeBytes = rel.SizeBytes
	}
	if card.PublishDate == "" {
		card.PublishDate = rel.PublishDate
	}
	// Grabs is a lifetime counter, so the highest value is the most informative
	// even when another source wins on seeders.
	if rel.Grabs > card.Grabs {
		card.Grabs = rel.Grabs
	}
	card.Categories = unionInts(card.Categories, rel.Categories)
}

// betterSeedSource reports whether rel carries a more useful popularity figure
// than the card currently holds.
func betterSeedSource(card Release, rel Release) bool {
	if rel.SeedersKnown && !card.SeedersKnown {
		return true
	}
	if card.SeedersKnown && !rel.SeedersKnown {
		return false
	}
	return rel.Seeders > card.Seeders
}

func sourceOf(rel Release) Source {
	return Source{
		IndexerID:    rel.IndexerID,
		IndexerName:  rel.IndexerName,
		DownloadURL:  rel.DownloadURL,
		InfoHash:     rel.InfoHash,
		Seeders:      rel.Seeders,
		SeedersKnown: rel.SeedersKnown,
		Peers:        rel.Peers,
		Grabs:        rel.Grabs,
		SizeBytes:    rel.SizeBytes,
		DetailsURL:   rel.DetailsURL,
	}
}

// withinTolerance reports whether two sizes differ by at most SizeTolerance.
// A zero size is treated as unknown and merges with anything of the same title.
func withinTolerance(a, b int64) bool {
	if a <= 0 || b <= 0 {
		return true
	}
	larger := a
	if b > larger {
		larger = b
	}
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return float64(diff)/float64(larger) <= SizeTolerance
}

func unionInts(a, b []int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(a)+len(b))
	for _, v := range append(append([]int{}, a...), b...) {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

// SortReleases orders results per BR-18: default by seeders descending, with
// releases that have no seeder data pushed to the end.
func SortReleases(releases []MergedRelease, sortBy string) {
	switch sortBy {
	case "size":
		sort.SliceStable(releases, func(i, j int) bool { return releases[i].SizeBytes > releases[j].SizeBytes })
	case "date":
		sort.SliceStable(releases, func(i, j int) bool { return releases[i].PublishDate > releases[j].PublishDate })
	case "indexer":
		sort.SliceStable(releases, func(i, j int) bool {
			return strings.ToLower(releases[i].IndexerName) < strings.ToLower(releases[j].IndexerName)
		})
	default:
		sort.SliceStable(releases, func(i, j int) bool {
			si, sj := releases[i].Seeders, releases[j].Seeders
			if (si == 0) != (sj == 0) {
				return sj == 0 // zero-seeder entries go last
			}
			return si > sj
		})
	}
}
