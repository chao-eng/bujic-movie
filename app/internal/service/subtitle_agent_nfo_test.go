package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
)

const nfoWithChineseInternal = `<?xml version="1.0" encoding="utf-8" standalone="yes"?>
<movie>
  <title>NfoZh (2021)</title>
  <fileinfo>
    <streamdetails>
      <subtitle><codec>subrip</codec><index>1</index><language>chi</language></subtitle>
    </streamdetails>
  </fileinfo>
</movie>
`

// TestAgentSubtitleFlowNFOFastPath: with a scraped NFO recording an internal
// Chinese track, the list/detail services must report zh-CN present by reading
// the NFO (no real ffprobe needed), and NOT as missing.
func TestAgentSubtitleFlowNFOFastPath(t *testing.T) {
	h := setupSubtitleAgentHarness(t)
	ctx := context.Background()

	base := "NfoZh (2021) [1080p]"
	videoPath := writeFile(t, filepath.Join(h.archive, base+".mkv"), "video")
	writeFile(t, filepath.Join(h.archive, base+".nfo"), nfoWithChineseInternal)
	addMovieRow(h, "NfoZh (2021)", 2021, videoPath)

	list, err := h.svc.QueryMediaList(ctx, MediaListRequest{MediaType: "movie"})
	if err != nil {
		t.Fatalf("QueryMediaList: %v", err)
	}
	if list.Total != 1 {
		t.Fatalf("expected 1 media, got %d", list.Total)
	}
	item := list.Items[0]
	if !item.HasSubtitle {
		t.Errorf("expected has_subtitle=true (internal zh via NFO)")
	}
	if containsStr(item.MissingSubtitles, "zh-CN") {
		t.Errorf("expected NO missing zh-CN via NFO fast path, got missing=%v", item.MissingSubtitles)
	}

	details, err := h.svc.QuerySubtitles(ctx, QuerySubtitlesRequest{Path: videoPath})
	if err != nil {
		t.Fatalf("QuerySubtitles: %v", err)
	}
	found := false
	for _, d := range details {
		for _, sub := range d.Subtitles {
			if sub.Type == "internal" && sub.Language == "zh-CN" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected internal zh-CN from NFO, got %+v", details)
	}
}

// TestAgentSubtitleFlowNFOAbsentFallsBack: with no NFO, an external-only movie
// must still report zh-CN missing (regression guard for the fallback path).
func TestAgentSubtitleFlowNFOAbsentFallsBack(t *testing.T) {
	h := setupSubtitleAgentHarness(t)
	ctx := context.Background()

	base := "NoNfo (2022) [1080p]"
	videoPath := writeFile(t, filepath.Join(h.archive, base+".mkv"), "video")
	writeFile(t, filepath.Join(h.archive, base+".en.srt"), "1\n00:00:01,000 --> 00:00:04,000\nHello\n")
	m := addMovieRow(h, "NoNfo (2022)", 2022, videoPath)
	_ = m

	list, err := h.svc.QueryMediaList(ctx, MediaListRequest{MediaType: "movie"})
	if err != nil {
		t.Fatalf("QueryMediaList: %v", err)
	}
	item := list.Items[0]
	if !item.HasSubtitle {
		t.Errorf("expected has_subtitle=true (external en)")
	}
	if len(item.MissingSubtitles) != 1 || item.MissingSubtitles[0] != "zh-CN" {
		t.Errorf("expected missing zh-CN when only external en, got %v", item.MissingSubtitles)
	}
	_ = entity.Media{}
}
