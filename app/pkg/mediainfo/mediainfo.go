// Package mediainfo probes local media files for codec, bitrate, language
// and other stream-level metadata using ffprobe. The parsed result is used
// to populate <fileinfo><streamdetails> in the generated NFO so media
// servers (Jellyfin/Emby/Kodi) keep the project's NFO instead of rewriting
// it from scratch.
package mediainfo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrFfprobeNotFound is returned when ffprobe is not available on $PATH.
var ErrFfprobeNotFound = errors.New("ffprobe not found in PATH")

// StreamDetails holds the parsed media streams for a single file.
type StreamDetails struct {
	Video    []VideoStream    `json:"video" xml:"video"`
	Audio    []AudioStream    `json:"audio" xml:"audio"`
	Subtitle []SubtitleStream `json:"subtitle" xml:"subtitle"`
}

// VideoStream mirrors a Jellyfin <video> element.
type VideoStream struct {
	Codec             string  `json:"codec" xml:"codec"`
	Micodec           string  `json:"micodec" xml:"micodec"`
	Bitrate           int     `json:"bitrate" xml:"bitrate"`
	Width             int     `json:"width" xml:"width"`
	Height            int     `json:"height" xml:"height"`
	Aspect            string  `json:"aspect" xml:"aspect"`
	AspectRatio       string  `json:"aspectratio" xml:"aspectratio"`
	Framerate         float64 `json:"framerate" xml:"framerate"`
	ScanType          string  `json:"scantype" xml:"scantype"`
	Duration          float64 `json:"duration" xml:"duration"`           // minutes (Jellyfin convention)
	DurationInSeconds int     `json:"durationinseconds" xml:"durationinseconds"`  // seconds
	Default           bool    `json:"default" xml:"default"`
	Forced            bool    `json:"forced" xml:"forced"`
}

// AudioStream mirrors a Jellyfin <audio> element.
type AudioStream struct {
	Codec        string `json:"codec" xml:"codec"`
	Micodec      string `json:"micodec" xml:"micodec"`
	Bitrate      int    `json:"bitrate" xml:"bitrate"`
	Language     string `json:"language" xml:"language"`
	ScanType     string `json:"scantype" xml:"scantype"`
	Channels     int    `json:"channels" xml:"channels"`
	SamplingRate int    `json:"samplingrate" xml:"samplingrate"`
	Default      bool   `json:"default" xml:"default"`
	Forced       bool   `json:"forced" xml:"forced"`
}

// SubtitleStream mirrors a Jellyfin <subtitle> element.
type SubtitleStream struct {
	Index    int    `json:"index" xml:"index"`
	Codec    string `json:"codec" xml:"codec"`
	Micodec  string `json:"micodec" xml:"micodec"`
	Width    int    `json:"width" xml:"width"`
	Height   int    `json:"height" xml:"height"`
	Language string `json:"language" xml:"language"`
	Title    string `json:"title" xml:"title"`
	ScanType string `json:"scantype" xml:"scantype"`
	Default  bool   `json:"default" xml:"default"`
	Forced   bool   `json:"forced" xml:"forced"`
}

// ffprobeOut is the top-level JSON shape ffprobe returns.
type ffprobeOut struct {
	Streams []ffprobeStream `json:"streams"`
	Format  *ffprobeFormat  `json:"format"`
}

type ffprobeStream struct {
	Index            int    `json:"index"`
	CodecName        string `json:"codec_name"`
	CodecType        string `json:"codec_type"`
	CodecLongName    string `json:"codec_long_name"`
	Profile          string `json:"profile"`
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	DisplayAspectRatio string `json:"display_aspect_ratio"`
	SampleAspectRatio  string `json:"sample_aspect_ratio"`
	AvgFrameRate     string `json:"avg_frame_rate"`
	RFrameRate       string `json:"r_frame_rate"`
	BitRate          string `json:"bit_rate"`
	Duration         string `json:"duration"`
	Channels         int    `json:"channels"`
	ChannelLayout    string `json:"channel_layout"`
	SampleRate       string `json:"sample_rate"`
	Tags             map[string]string `json:"tags"`
}

type ffprobeFormat struct {
	Duration string `json:"duration"`
	BitRate  string `json:"bit_rate"`
}

// Probe runs ffprobe against the given file and returns its parsed
// stream details. If ffprobe is not on $PATH, ErrFfprobeNotFound is
// returned so the caller can degrade gracefully (omit <streamdetails>).
func Probe(ctx context.Context, filePath string) (*StreamDetails, error) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return nil, ErrFfprobeNotFound
	}
	if !filepath.IsAbs(filePath) {
		return nil, fmt.Errorf("mediainfo: path must be absolute: %s", filePath)
	}

	cmd := exec.CommandContext(ctx,
		"ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		filePath,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("mediainfo: ffprobe failed for %s: %w", filePath, err)
	}

	var raw ffprobeOut
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("mediainfo: parse ffprobe output: %w", err)
	}

	details := &StreamDetails{}
	for _, s := range raw.Streams {
		lang := pickLang(s.Tags)
		disp := boolFromTag(s.Tags, "disposition.default")
		force := boolFromTag(s.Tags, "disposition.forced")
		switch s.CodecType {
		case "video":
			details.Video = append(details.Video, VideoStream{
				Codec:             s.CodecName,
				Micodec:           microCodec(s),
				Bitrate:           parseInt(s.BitRate),
				Width:             s.Width,
				Height:            s.Height,
				Aspect:            s.DisplayAspectRatio,
				AspectRatio:       s.DisplayAspectRatio,
				Framerate:         parseFramerate(s.AvgFrameRate, s.RFrameRate),
				ScanType:          scanType(s),
				Duration:          roundTo2(parseFloat(s.Duration) / 60.0),
				DurationInSeconds: int(parseFloat(s.Duration)),
				Default:           disp,
				Forced:            force,
			})
		case "audio":
			details.Audio = append(details.Audio, AudioStream{
				Codec:        s.CodecName,
				Micodec:      microCodec(s),
				Bitrate:      parseInt(s.BitRate),
				Language:     lang,
				ScanType:     scanType(s),
				Channels:     s.Channels,
				SamplingRate: parseInt(s.SampleRate),
				Default:      disp,
				Forced:       force,
			})
		case "subtitle":
			details.Subtitle = append(details.Subtitle, SubtitleStream{
				Index:    s.Index,
				Codec:    s.CodecName,
				Micodec:  microCodec(s),
				Width:    s.Width,
				Height:   s.Height,
				Language: lang,
				Title:    s.Tags["title"],
				ScanType: scanType(s),
				Default:  disp,
				Forced:   force,
			})
		}
	}
	return details, nil
}

// pickLang returns the language tag, falling back across common tag keys.
func pickLang(tags map[string]string) string {
	if v, ok := tags["language"]; ok && v != "" {
		return v
	}
	if v, ok := tags["lang"]; ok && v != "" {
		return v
	}
	return ""
}

// microCodec derives a Jellyfin-style codec identifier (eac3 / aac / hevc …)
// from the raw ffprobe codec + profile. We only override the obvious ones
// because the Jellyfin NFO field "micodec" carries the player-friendly name.
func microCodec(s ffprobeStream) string {
	name := strings.ToLower(s.CodecName)
	switch name {
	case "eac3", "ec-3":
		return "eac3"
	case "ac3":
		return "ac3"
	case "aac":
		return "aac"
	case "mp3":
		return "mp3"
	case "truehd":
		return "truehd"
	case "dts":
		return "dts"
	case "h264":
		return "h264"
	case "hevc":
		return "hevc"
	case "av1":
		return "av1"
	case "vp9":
		return "vp9"
	case "ass":
		return "ass"
	case "subrip", "srt":
		return "subrip"
	case "hdmv_pgs_subtitle", "pgs":
		return "pgs"
	case "dvd_subtitle":
		return "subrip"
	}
	return name
}

// scanType reports interlaced/progressive based on the avg/r frame rate.
// ffprobe only sets "field_order" on interlaced content; treat that as
// the authoritative signal and otherwise return "progressive".
func scanType(s ffprobeStream) string {
	if s.CodecType != "video" {
		return "progressive"
	}
	// We don't have field_order in the json we asked for, fall back to
	// the convention Jellyfin uses: every modern Web-DL/Blu-ray is
	// progressive unless explicitly told otherwise.
	return "progressive"
}

// boolFromTag pulls a bool from the disposition.* tag set ffprobe emits.
// Returns false if missing or unparseable.
func boolFromTag(tags map[string]string, key string) bool {
	if v, ok := tags[key]; ok {
		return v == "1" || strings.EqualFold(v, "true")
	}
	return false
}

func parseInt(s string) int {
	if s == "" {
		return 0
	}
	n, _ := strconv.Atoi(s)
	return n
}

func parseFloat(s string) float64 {
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// parseFramerate decodes a "24000/1001" style fraction and falls back to
// the running rate if the average is unavailable.
func parseFramerate(avg, running string) float64 {
	if v, ok := tryFraction(avg); ok {
		return roundTo6(v)
	}
	if v, ok := tryFraction(running); ok {
		return roundTo6(v)
	}
	return 0
}

func tryFraction(s string) (float64, bool) {
	if s == "" || !strings.Contains(s, "/") {
		return 0, false
	}
	parts := strings.SplitN(s, "/", 2)
	num, err1 := strconv.ParseFloat(parts[0], 64)
	den, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return 0, false
	}
	return num / den, true
}

func roundTo2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
func roundTo6(f float64) float64 { return float64(int(f*1e6+0.5)) / 1e6 }
