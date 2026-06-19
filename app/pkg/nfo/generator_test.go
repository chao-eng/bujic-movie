package nfo

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/bujic-movie/bujic-movie/pkg/mediainfo"
	"github.com/bujic-movie/bujic-movie/pkg/tmdb"
)

func TestMovieNFOXMLGeneration(t *testing.T) {
	movie := &tmdb.MovieDetail{
		ID:            123,
		Title:         "Inception",
		OriginalTitle: "Inception",
		Overview:      "Dream thief",
		ReleaseDate:   "2010-07-16",
		VoteAverage:   8.3,
		Runtime:       148,
		Genres: []tmdb.Genre{
			{Name: "Action"},
			{Name: "Sci-Fi"},
		},
	}

	cast := []tmdb.Cast{
		{ID: 6193, Name: "Leonardo DiCaprio", Character: "Cobb", Order: 0, ProfilePath: "/abc.jpg", KnownForDepartment: "Acting"},
		{ID: 24045, Name: "Joseph Gordon-Levitt", Character: "Arthur", Order: 1, KnownForDepartment: "Acting"},
		{ID: 999, Name: "Hans Zimmer", Character: "", Order: 2, KnownForDepartment: "Sound"},
	}

	data, err := GenerateMovieNFO(MovieOptions{
		LockData:  "false",
		Detail:    movie,
		Actors:    cast,
		Directors: []string{"Christopher Nolan"},
		TMDBID:    123,
		Stream: &mediainfo.StreamDetails{
			Video: []mediainfo.VideoStream{{Codec: "h264", Width: 1920, Height: 1080}},
		},
	})
	if err != nil {
		t.Fatalf("GenerateMovieNFO failed: %v", err)
	}

	xmlStr := string(data)
	if !strings.HasPrefix(xmlStr, `<?xml version="1.0" encoding="utf-8" standalone="yes"?>`) {
		t.Errorf("Missing XML header (got %q)", xmlStr[:80])
	}

	var parsed MovieNFO
	rawXML := strings.TrimPrefix(xmlStr, `<?xml version="1.0" encoding="utf-8" standalone="yes"?>`+"\n")
	if err := xml.Unmarshal([]byte(rawXML), &parsed); err != nil {
		t.Fatalf("Failed to parse back XML: %v", err)
	}

	if parsed.Title != "Inception" || parsed.Year != 2010 || len(parsed.Genres) != 2 || parsed.UniqueID.Value != 123 {
		t.Errorf("Parsed mismatch: %+v", parsed)
	}
	if parsed.Lockdata != "false" {
		t.Errorf("lockdata: %q", parsed.Lockdata)
	}
	if len(parsed.Director) != 1 || parsed.Director[0] != "Christopher Nolan" {
		t.Errorf("director: %+v", parsed.Director)
	}
	if parsed.Fileinfo == nil || len(parsed.Fileinfo.StreamDetails.Video) != 1 {
		t.Errorf("streamdetails missing: %+v", parsed.Fileinfo)
	}

	// Acting only — Sound department dropped.
	if len(parsed.Actors) != 2 {
		t.Fatalf("expected 2 actors, got %d", len(parsed.Actors))
	}
	first := parsed.Actors[0]
	if first.Name != "Leonardo DiCaprio" || first.Role != "Cobb" || first.Type != "Actor" {
		t.Errorf("actor[0] mismatch: %+v", first)
	}
	if first.Thumb != "https://image.tmdb.org/t/p/original/abc.jpg" {
		t.Errorf("actor[0] thumb mismatch: expected %q, got %q", "https://image.tmdb.org/t/p/original/abc.jpg", first.Thumb)
	}
	// No profile_path → no thumb.
	if parsed.Actors[1].Thumb != "" {
		t.Errorf("actor[1] without profile_path should omit thumb, got %s", parsed.Actors[1].Thumb)
	}
}

func TestEpisodeNFOGeneration(t *testing.T) {
	ep := &tmdb.TVEpisode{
		ID:            5678,
		Name:          "下注",
		Overview:      "海关职员禧主在机长男友度庆的请托下…",
		AirDate:       "2026-05-27",
		EpisodeNumber: 1,
		SeasonNumber:  1,
		VoteAverage:   7.5,
	}
	cast := []tmdb.Cast{
		{ID: 1, Name: "演员甲", Character: "角色甲", KnownForDepartment: "Acting", ProfilePath: "/p.jpg"},
	}
	crew := []tmdb.CrewMember{
		{ID: 10, Name: "导演甲", Department: "Directing", Job: "Director"},
		{ID: 11, Name: "编剧甲", Department: "Writing", Job: "Screenplay"},
	}
	data, err := GenerateEpisodeNFO(EpisodeOptions{
		Episode:     ep,
		ShowTitle:   "赌金",
		ShowTMDBID:  7122310,
		ArtPoster:   "/media/TV/赌金 (2026)/Season 01/赌金 (2026) - S01E01 - 下注.jpg",
		Directors:   ExtractDirectors(crew),
		Actors:      cast,
		MPAA:        "TV-MA",
		Year:        2026,
		Country:     []string{"KR"},
		DateAdded:   mustParseTime("2026-06-18 16:51:02"),
		LockData:    "false",
		Runtime:     60,
		Stream: &mediainfo.StreamDetails{
			Video:    []mediainfo.VideoStream{{Codec: "hevc", Micodec: "hevc", Width: 1920, Height: 960, Aspect: "2:1"}},
			Audio:    []mediainfo.AudioStream{{Codec: "eac3", Micodec: "eac3", Language: "kor", Channels: 6}},
			Subtitle: []mediainfo.SubtitleStream{{Codec: "subrip", Language: "zho"}},
		},
	})
	if err != nil {
		t.Fatalf("GenerateEpisodeNFO failed: %v", err)
	}

	raw := strings.TrimPrefix(string(data), `<?xml version="1.0" encoding="utf-8" standalone="yes"?>`+"\n")
	var parsed EpisodeNFO
	if err := xml.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("parse back: %v", err)
	}

	if parsed.Title != "下注" {
		t.Errorf("title: %q", parsed.Title)
	}
	if parsed.Showtitle != "赌金" {
		t.Errorf("showtitle: %q", parsed.Showtitle)
	}
	if parsed.Season == nil || *parsed.Season != 1 {
		t.Errorf("season: %v", parsed.Season)
	}
	if parsed.Episode == nil || *parsed.Episode != 1 {
		t.Errorf("episode: %v", parsed.Episode)
	}
	if parsed.TMDBID == nil || *parsed.TMDBID != 7122310 {
		t.Errorf("tmdbid: %v", parsed.TMDBID)
	}
	if parsed.CollectionNumber == nil || *parsed.CollectionNumber != 7122310 {
		t.Errorf("collectionnumber: %v", parsed.CollectionNumber)
	}
	if parsed.Art.Poster == "" {
		t.Error("art/poster missing")
	}
	if parsed.Aired != "2026-05-27" {
		t.Errorf("aired: %q", parsed.Aired)
	}
	if parsed.DateAdded != "2026-06-18 16:51:02" {
		t.Errorf("dateadded: %q", parsed.DateAdded)
	}
	if parsed.Lockdata != "false" {
		t.Errorf("lockdata: %q", parsed.Lockdata)
	}
	if len(parsed.Director) != 1 || parsed.Director[0] != "导演甲" {
		t.Errorf("director: %+v (crew writer should be filtered out)", parsed.Director)
	}
	if len(parsed.Actors) != 1 {
		t.Errorf("actors: %+v", parsed.Actors)
	}
	if parsed.Fileinfo == nil || len(parsed.Fileinfo.StreamDetails.Video) != 1 {
		t.Errorf("fileinfo/streamdetails missing: %+v", parsed.Fileinfo)
	}
}

func TestEpisodeNFOSpecialsKeepsZero(t *testing.T) {
	// Season 0 / Episode 0 are valid (Specials); *int must keep them.
	ep := &tmdb.TVEpisode{
		ID:            9999,
		Name:          "Pilot Special",
		Overview:      "Special preview.",
		EpisodeNumber: 0,
		SeasonNumber:  0,
	}
	data, err := GenerateEpisodeNFO(EpisodeOptions{
		Episode:    ep,
		ShowTitle:  "Show",
		ShowTMDBID: 1,
	})
	if err != nil {
		t.Fatalf("GenerateEpisodeNFO failed: %v", err)
	}
	raw := string(data)
	if !strings.Contains(raw, "<season>0</season>") {
		t.Errorf("expected <season>0</season> in NFO, got: %s", raw)
	}
	if !strings.Contains(raw, "<episode>0</episode>") {
		t.Errorf("expected <episode>0</episode> in NFO, got: %s", raw)
	}
}

func TestExtractDirectors(t *testing.T) {
	crew := []tmdb.CrewMember{
		{Name: "A", Department: "Directing", Job: "Director"},
		{Name: "B", Department: "Writing", Job: "Screenplay"},
		{Name: "C", Department: "Directing", Job: "Director"},
		{Name: "D", Department: "Directing", Job: "Assistant Director"},
		{Name: "", Department: "Directing", Job: "Director"},
	}
	got := ExtractDirectors(crew)
	if len(got) != 2 || got[0] != "A" || got[1] != "C" {
		t.Errorf("got %+v, want [A C]", got)
	}
}

func mustParseTime(s string) time.Time {
	const layout = "2006-01-02 15:04:05"
	tt, err := time.Parse(layout, s)
	if err != nil {
		panic(err)
	}
	return tt
}
