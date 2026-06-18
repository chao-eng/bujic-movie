package nfo

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/bujic-movie/bujic-movie/pkg/tmdb"
)

func TestNFOXMLGeneration(t *testing.T) {
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

	data, err := GenerateMovieNFO(movie, cast)
	if err != nil {
		t.Fatalf("GenerateMovieNFO failed: %v", err)
	}

	xmlStr := string(data)
	if !strings.HasPrefix(xmlStr, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`) {
		t.Errorf("Missing XML header")
	}

	var parsed MovieNFO
	rawXML := strings.TrimPrefix(xmlStr, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+"\n")
	if err := xml.Unmarshal([]byte(rawXML), &parsed); err != nil {
		t.Fatalf("Failed to parse back XML: %v", err)
	}

	if parsed.Title != "Inception" || parsed.Year != 2010 || len(parsed.Genres) != 2 || parsed.UniqueID.Value != 123 {
		t.Errorf("Parsed mismatch: %+v", parsed)
	}

	// 仅保留 Acting 部门的演员，Sound 部门的应被过滤
	if len(parsed.Actors) != 2 {
		t.Fatalf("expected 2 actors, got %d: %+v", len(parsed.Actors), parsed.Actors)
	}
	first := parsed.Actors[0]
	if first.Name != "Leonardo DiCaprio" || first.Role != "Cobb" || first.Type != "Actor" || first.TMDBID != 6193 {
		t.Errorf("actor[0] mismatch: %+v", first)
	}
	if first.Thumb == "" {
		t.Errorf("actor[0] with profile_path should have a thumb URL")
	}
	if first.Profile != "https://www.themoviedb.org/person/6193" {
		t.Errorf("actor[0] profile mismatch: %s", first.Profile)
	}
	// 无 profile_path 的演员不应写出 thumb
	if parsed.Actors[1].Thumb != "" {
		t.Errorf("actor[1] without profile_path should omit thumb, got %s", parsed.Actors[1].Thumb)
	}
}
