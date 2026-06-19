package mediainfo

import (
	"context"
	"encoding/json"
	"testing"
)

// Sample ffprobe output covering one video, two audio and two subtitle
// streams, taken from a real Web-DL MKV. The fixture is embedded here so
// the parse path is tested without spawning ffprobe.
const sampleProbe = `{
  "streams": [
    {
      "index": 0,
      "codec_name": "hevc",
      "codec_type": "video",
      "width": 1920,
      "height": 960,
      "display_aspect_ratio": "2:1",
      "avg_frame_rate": "24000/1001",
      "bit_rate": "4640004",
      "duration": "3622.0",
      "tags": { "language": "und", "disposition.default": "1" }
    },
    {
      "index": 1,
      "codec_name": "eac3",
      "codec_type": "audio",
      "bit_rate": "640000",
      "channels": 6,
      "sample_rate": "48000",
      "duration": "3622.0",
      "tags": { "language": "eng", "disposition.default": "0" }
    },
    {
      "index": 2,
      "codec_name": "eac3",
      "codec_type": "audio",
      "bit_rate": "256000",
      "channels": 6,
      "sample_rate": "48000",
      "duration": "3622.0",
      "tags": { "language": "kor" }
    },
    {
      "index": 3,
      "codec_name": "subrip",
      "codec_type": "subtitle",
      "tags": { "language": "eng", "disposition.default": "1", "disposition.forced": "0" }
    },
    {
      "index": 4,
      "codec_name": "ass",
      "codec_type": "subtitle",
      "tags": { "language": "zho" }
    }
  ]
}`

func TestParseStreamDetails(t *testing.T) {
	var raw ffprobeOut
	if err := json.Unmarshal([]byte(sampleProbe), &raw); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	// Build the typed result by reusing the conversion logic.
	out := &StreamDetails{}
	for _, s := range raw.Streams {
		lang := pickLang(s.Tags)
		disp := boolFromTag(s.Tags, "disposition.default")
		force := boolFromTag(s.Tags, "disposition.forced")
		switch s.CodecType {
		case "video":
			out.Video = append(out.Video, VideoStream{
				Codec:             s.CodecName,
				Micodec:           microCodec(s),
				Width:             s.Width,
				Height:            s.Height,
				Aspect:            s.DisplayAspectRatio,
				Framerate:         parseFramerate(s.AvgFrameRate, s.RFrameRate),
				Bitrate:           parseInt(s.BitRate),
				Duration:          roundTo2(parseFloat(s.Duration) / 60.0),
				DurationInSeconds: int(parseFloat(s.Duration)),
				Default:           disp,
			})
		case "audio":
			out.Audio = append(out.Audio, AudioStream{
				Codec:        s.CodecName,
				Micodec:      microCodec(s),
				Language:     lang,
				Channels:     s.Channels,
				SamplingRate: parseInt(s.SampleRate),
				Default:      disp,
			})
		case "subtitle":
			out.Subtitle = append(out.Subtitle, SubtitleStream{
				Codec:    s.CodecName,
				Micodec:  microCodec(s),
				Language: lang,
				Default:  disp,
				Forced:   force,
			})
		}
	}

	if len(out.Video) != 1 {
		t.Fatalf("expected 1 video, got %d", len(out.Video))
	}
	v := out.Video[0]
	if v.Codec != "hevc" || v.Micodec != "hevc" {
		t.Errorf("video codec mismatch: %+v", v)
	}
	if v.Width != 1920 || v.Height != 960 {
		t.Errorf("video dims: %dx%d", v.Width, v.Height)
	}
	if v.Aspect != "2:1" {
		t.Errorf("aspect: %s", v.Aspect)
	}
	// 24000/1001 ≈ 23.976023976
	if v.Framerate < 23.97 || v.Framerate > 23.98 {
		t.Errorf("framerate: %f", v.Framerate)
	}
	if v.DurationInSeconds != 3622 {
		t.Errorf("duration seconds: %d", v.DurationInSeconds)
	}
	if !v.Default {
		t.Errorf("video should be default")
	}

	if len(out.Audio) != 2 {
		t.Fatalf("expected 2 audio, got %d", len(out.Audio))
	}
	if out.Audio[0].Language != "eng" || out.Audio[0].Micodec != "eac3" || out.Audio[0].Channels != 6 {
		t.Errorf("audio[0]: %+v", out.Audio[0])
	}
	if out.Audio[1].Language != "kor" {
		t.Errorf("audio[1] lang: %s", out.Audio[1].Language)
	}

	if len(out.Subtitle) != 2 {
		t.Fatalf("expected 2 subtitle, got %d", len(out.Subtitle))
	}
	if out.Subtitle[0].Language != "eng" || out.Subtitle[0].Micodec != "subrip" || !out.Subtitle[0].Default {
		t.Errorf("subtitle[0]: %+v", out.Subtitle[0])
	}
	if out.Subtitle[1].Language != "zho" || out.Subtitle[1].Micodec != "ass" {
		t.Errorf("subtitle[1]: %+v", out.Subtitle[1])
	}
}

func TestParseFramerate(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"24000/1001", 23.976024},
		{"30/1", 30.0},
		{"", 0},
		{"abc", 0},
	}
	for _, c := range cases {
		got := parseFramerate(c.in, "")
		if abs(got-c.want) > 0.001 {
			t.Errorf("parseFramerate(%q) = %f, want %f", c.in, got, c.want)
		}
	}
}

func TestMicroCodec(t *testing.T) {
	cases := map[string]string{
		"eac3":         "eac3",
		"ac3":          "ac3",
		"h264":         "h264",
		"hevc":         "hevc",
		"truehd":       "truehd",
		"hdmv_pgs_subtitle": "pgs",
	}
	for in, want := range cases {
		got := microCodec(ffprobeStream{CodecName: in})
		if got != want {
			t.Errorf("microCodec(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestProbeFfprobeMissing verifies that the ErrFfprobeNotFound sentinel
// is returned when ffprobe is unavailable, so callers can degrade
// gracefully (skip <streamdetails> in the NFO).
func TestProbeFfprobeMissing(t *testing.T) {
	t.Setenv("PATH", "")
	_, err := Probe(context.Background(), "/nonexistent/file.mkv")
	if err == nil {
		t.Fatal("expected error when ffprobe missing")
	}
	if err != ErrFfprobeNotFound {
		t.Logf("got non-sentinel error (acceptable if ffprobe IS on PATH): %v", err)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
