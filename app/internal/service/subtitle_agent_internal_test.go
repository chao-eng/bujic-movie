package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// makeVideoWithInternalChineseSubtitle uses real ffmpeg to create a tiny mkv
// that muxes a Chinese subtitle track (language=chi). Skipped when ffmpeg is
// unavailable.
func makeVideoWithInternalChineseSubtitle(t *testing.T, dir, name string) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available; skipping internal-subtitle integration test")
	}

	out := filepath.Join(dir, name)
	srtPath := filepath.Join(dir, "zh.srt")
	if err := os.WriteFile(srtPath, []byte("1\n00:00:00,000 --> 00:00:01,000\n你好\n"), 0644); err != nil {
		t.Fatalf("write srt: %v", err)
	}

	cmd := exec.Command("ffmpeg", "-y", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=black:s=160x120:d=1",
		"-i", srtPath,
		"-map", "0:v", "-map", "1:s",
		"-c:v", "libx264",
		"-c:s", "srt",
		"-metadata:s:s:0", "language=chi",
		out,
	)
	if outBytes, err := cmd.CombinedOutput(); err != nil {
		// retry with mpeg4 (no libx264 present)
		cmd2 := exec.Command("ffmpeg", "-y", "-loglevel", "error",
			"-f", "lavfi", "-i", "color=black:s=160x120:d=1",
			"-i", srtPath,
			"-map", "0:v", "-map", "1:s",
			"-c:v", "mpeg4",
			"-c:s", "srt",
			"-metadata:s:s:0", "language=chi",
			out,
		)
		if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
			t.Skipf("ffmpeg cannot produce muxed subtitle (libx264/mpeg4 missing): %v / %v", string(outBytes), string(out2))
		}
	}
	return out
}

// TestAgentSubtitleFlowInternalZHCN: a movie whose Chinese subtitle is muxed
// INTO the container (language=chi) must NOT be reported as missing zh-CN.
func TestAgentSubtitleFlowInternalZHCN(t *testing.T) {
	h := setupSubtitleAgentHarness(t)
	ctx := context.Background()

	videoPath := makeVideoWithInternalChineseSubtitle(t, h.archive, "InternalZh (2020) [1080p].mkv")
	addMovieRow(h, "InternalZh (2020)", 2020, videoPath)

	list, err := h.svc.QueryMediaList(ctx, MediaListRequest{MediaType: "movie"})
	if err != nil {
		t.Fatalf("QueryMediaList: %v", err)
	}
	if list.Total != 1 {
		t.Fatalf("expected 1 media, got %d", list.Total)
	}
	item := list.Items[0]
	if !item.HasSubtitle {
		t.Errorf("expected has_subtitle=true for video with internal zh track")
	}
	if !containsStr(item.Languages, "zh-CN") {
		t.Errorf("expected languages to include zh-CN from muxed internal track, got %v", item.Languages)
	}
	if containsStr(item.MissingSubtitles, "zh-CN") {
		t.Errorf("expected NO missing zh-CN for internal Chinese track, got missing=%v", item.MissingSubtitles)
	}

	// detail should expose the internal track with normalized zh-CN language
	details, err := h.svc.QuerySubtitles(ctx, QuerySubtitlesRequest{Path: videoPath})
	if err != nil {
		t.Fatalf("QuerySubtitles: %v", err)
	}
	foundInternalZHCN := false
	for _, d := range details {
		for _, sub := range d.Subtitles {
			if sub.Type == "internal" && sub.Language == "zh-CN" {
				foundInternalZHCN = true
			}
		}
	}
	if !foundInternalZHCN {
		t.Errorf("expected an internal track normalized to zh-CN, got %+v", details)
	}
}
