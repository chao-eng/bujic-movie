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

	data, err := GenerateMovieNFO(movie)
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
}
