package parser

import (
	"reflect"
	"testing"
)

func TestParseFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected *Metadata
	}{
		{
			input: "The Movie Title (2024) [1080p].mkv",
			expected: &Metadata{
				Title:      "The Movie Title",
				Year:       2024,
				Season:     0,
				Episodes:   nil,
				Resolution: "1080p",
				IsMovie:    true,
			},
		},
		{
			input: "The TV Show S01E05 1080p.mkv",
			expected: &Metadata{
				Title:      "The TV Show",
				Year:       0,
				Season:     1,
				Episodes:   []int{5},
				Resolution: "1080p",
				IsMovie:    false,
			},
		},
		{
			input: "The.Movie.Title.2024.1080p.WEB-DL.H264.mkv",
			expected: &Metadata{
				Title:      "The Movie Title",
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
			input: "The TV Show S01E01-E03.mkv",
			expected: &Metadata{
				Title:      "The TV Show",
				Year:       0,
				Season:     1,
				Episodes:   []int{1, 2, 3},
				IsMovie:    false,
			},
		},
	}

	for _, test := range tests {
		actual := ParseFilename(test.input)
		if actual.Title != test.expected.Title {
			t.Errorf("For %s: expected Title %q, got %q", test.input, test.expected.Title, actual.Title)
		}
		if actual.Year != test.expected.Year {
			t.Errorf("For %s: expected Year %d, got %d", test.input, test.expected.Year, actual.Year)
		}
		if actual.Season != test.expected.Season {
			t.Errorf("For %s: expected Season %d, got %d", test.input, test.expected.Season, actual.Season)
		}
		if !reflect.DeepEqual(actual.Episodes, test.expected.Episodes) {
			t.Errorf("For %s: expected Episodes %v, got %v", test.input, test.expected.Episodes, actual.Episodes)
		}
		if actual.IsMovie != test.expected.IsMovie {
			t.Errorf("For %s: expected IsMovie %t, got %t", test.input, test.expected.IsMovie, actual.IsMovie)
		}
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
