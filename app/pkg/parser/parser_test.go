package parser

import (
	"reflect"
	"testing"
)

func TestParseFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *Metadata
	}{
		{
			name:  "basic movie with brackets",
			input: "The Movie Title (2024) [1080p].mkv",
			expected: &Metadata{
				Title:      "The Movie Title",
				EnName:     "The Movie Title",
				Year:       2024,
				Season:     0,
				Episodes:   nil,
				Resolution: "1080p",
				IsMovie:    true,
			},
		},
		{
			name:  "basic TV single episode",
			input: "The TV Show S01E05 1080p.mkv",
			expected: &Metadata{
				Title:      "The TV Show",
				EnName:     "The TV Show",
				Year:       0,
				Season:     1,
				Episodes:   []int{5},
				Resolution: "1080p",
				IsMovie:    false,
			},
		},
		{
			name:  "dot-separated movie with full metadata",
			input: "The.Movie.Title.2024.1080p.WEB-DL.H264.mkv",
			expected: &Metadata{
				Title:      "The Movie Title",
				EnName:     "The Movie Title",
				Year:       2024,
				Season:     0,
				Episodes:   nil,
				Resolution: "1080p",
				Source:     "WEB-DL",
				Codec:      "h264",
				IsMovie:    true,
			},
		},
		{
			name:  "multi-episode range",
			input: "The TV Show S01E01-E03.mkv",
			expected: &Metadata{
				Title:    "The TV Show",
				EnName:   "The TV Show",
				Year:     0,
				Season:   1,
				Episodes: []int{1, 2, 3},
				IsMovie:  false,
			},
		},
		{
			name:  "website prefix + release group suffix",
			input: "www.UIndex.org    -    Born.to.Be.Wild.2025.S01E01.1080p.WEB.h264-GRACE.mkv",
			expected: &Metadata{
				Title:      "Born To Be Wild",
				EnName:     "Born To Be Wild",
				Year:       2025,
				Season:     1,
				Episodes:   []int{1},
				Resolution: "1080p",
				Source:     "WEB",
				Codec:      "h264",
				IsMovie:    false,
			},
		},
		{
			name:  "dot-separated TV with release group",
			input: "Hataraku.Maou-sama.S02E05.2022.1080p.CR.WEB-DL.X264.AAC-ADWeb.mkv",
			expected: &Metadata{
				Title:      "Hataraku Maou Sama",
				EnName:     "Hataraku Maou Sama",
				Year:       2022,
				Season:     2,
				Episodes:   []int{5},
				Resolution: "1080p",
				Source:     "WEB-DL",
				Codec:      "h264",
				IsMovie:    false,
			},
		},
		{
			name:  "Chinese movie title",
			input: "钢铁侠2 (2010) 1080p AC3.mp4",
			expected: &Metadata{
				Title:      "钢铁侠2",
				CnName:     "钢铁侠2",
				Year:       2010,
				Resolution: "1080p",
				IsMovie:    true,
			},
		},
		{
			name:  "Chinese TV with SxxExx",
			input: "一夜新娘 - S02E07 - 第 7 集.mp4",
			expected: &Metadata{
				Title:    "一夜新娘",
				CnName:   "一夜新娘",
				Season:   2,
				Episodes: []int{7},
				IsMovie:  false,
			},
		},
		{
			name:  "UHD BluRay",
			input: "30.Rock.S02E01.1080p.UHD.BluRay.X264-BORDURE.mkv",
			expected: &Metadata{
				Title:      "30 Rock",
				EnName:     "30 Rock",
				Season:     2,
				Episodes:   []int{1},
				Resolution: "1080p",
				Source:     "UHD",
				Codec:      "h264",
				IsMovie:    false,
			},
		},
		{
			name:  "H.264 with dot separator (two tokens)",
			input: "The Witch Part 2 The Other One 2022 1080p WEB-DL AAC5.1 H.264-tG1R0",
			expected: &Metadata{
				Title:      "The Witch Part 2 The Other One",
				EnName:     "The Witch Part 2 The Other One",
				Year:       2022,
				Resolution: "1080p",
				Source:     "WEB-DL",
				Codec:      "h264",
				IsMovie:    true,
			},
		},
		{
			name:  "season only without episode",
			input: "Cherry Season S01 2014 2160p WEB-DL H265 AAC-XXX",
			expected: &Metadata{
				Title:      "Cherry Season",
				EnName:     "Cherry Season",
				Year:       2014,
				Season:     1,
				Resolution: "2160p",
				Source:     "WEB-DL",
				Codec:      "h265",
				IsMovie:    false,
			},
		},
		{
			name:  "numeric title",
			input: "24 S01 1080p WEB-DL AAC2.0 H.264-BTN",
			expected: &Metadata{
				Title:      "24",
				EnName:     "24",
				Season:     1,
				Resolution: "1080p",
				Source:     "WEB-DL",
				Codec:      "h264",
				IsMovie:    false,
			},
		},
		{
			name:  "1920x1080 dimensional resolution",
			input: "Movie Title 2023 1920x1080 BluRay.mkv",
			expected: &Metadata{
				Title:      "Movie Title",
				EnName:     "Movie Title",
				Year:       2023,
				Resolution: "1080p",
				Source:     "BluRay",
				IsMovie:    true,
			},
		},
		{
			name:  "Wonder Woman with year in title",
			input: "Wonder.Woman.1984.2020.BluRay.1080p.X264.mkv",
			expected: &Metadata{
				Title:      "Wonder Woman 1984",
				EnName:     "Wonder Woman 1984",
				Year:       2020,
				Resolution: "1080p",
				Source:     "BluRay",
				Codec:      "h264",
				IsMovie:    true,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := ParseFilename(test.input)

			if actual.Title != test.expected.Title {
				t.Errorf("Title: expected %q, got %q", test.expected.Title, actual.Title)
			}
			if actual.CnName != test.expected.CnName {
				t.Errorf("CnName: expected %q, got %q", test.expected.CnName, actual.CnName)
			}
			if actual.EnName != test.expected.EnName {
				t.Errorf("EnName: expected %q, got %q", test.expected.EnName, actual.EnName)
			}
			if actual.Year != test.expected.Year {
				t.Errorf("Year: expected %d, got %d", test.expected.Year, actual.Year)
			}
			if actual.Season != test.expected.Season {
				t.Errorf("Season: expected %d, got %d", test.expected.Season, actual.Season)
			}
			if !reflect.DeepEqual(actual.Episodes, test.expected.Episodes) {
				t.Errorf("Episodes: expected %v, got %v", test.expected.Episodes, actual.Episodes)
			}
			if actual.Resolution != test.expected.Resolution {
				t.Errorf("Resolution: expected %q, got %q", test.expected.Resolution, actual.Resolution)
			}
			if test.expected.Source != "" && actual.Source != test.expected.Source {
				t.Errorf("Source: expected %q, got %q", test.expected.Source, actual.Source)
			}
			if test.expected.Codec != "" && actual.Codec != test.expected.Codec {
				t.Errorf("Codec: expected %q, got %q", test.expected.Codec, actual.Codec)
			}
			if actual.IsMovie != test.expected.IsMovie {
				t.Errorf("IsMovie: expected %t, got %t", test.expected.IsMovie, actual.IsMovie)
			}
		})
	}
}

func TestParseSubtitle(t *testing.T) {
	tests := []struct {
		input    string
		expected *SubtitleInfo
	}{
		{
			input: "movie.zh-cn.ass",
			expected: &SubtitleInfo{
				Language: "zh-CN",
				Format:   "ass",
			},
		},
		{
			input: "movie.tc.srt",
			expected: &SubtitleInfo{
				Language: "zh-TW",
				Format:   "srt",
			},
		},
		{
			input: "movie.traditional.chs.srt",
			expected: &SubtitleInfo{
				Language: "zh-TW",
				Format:   "srt",
			},
		},
		{
			input: "movie.en.vtt",
			expected: &SubtitleInfo{
				Language: "en",
				Format:   "vtt",
			},
		},
	}

	for _, test := range tests {
		actual := ParseSubtitle(test.input)
		if !reflect.DeepEqual(actual, test.expected) {
			t.Errorf("For %s: expected %v, got %v", test.input, test.expected, actual)
		}
	}
}
