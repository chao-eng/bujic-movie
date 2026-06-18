package nfo

import (
	"encoding/xml"
	"fmt"

	"github.com/bujic-movie/bujic-movie/pkg/tmdb"
)

// UniqueID represents a metadata provider ID tag in NFO XML
type UniqueID struct {
	Type    string `xml:"type,attr"`
	Default string `xml:"default,attr"`
	Value   int    `xml:",chardata"`
}

// Actor represents a cast member in NFO XML (Emby/Jellyfin/Kodi format).
// Field order mirrors MoviePilot's scraper output.
type Actor struct {
	Name    string `xml:"name"`
	Type    string `xml:"type"`
	Role    string `xml:"role"`
	TMDBID  int    `xml:"tmdbid"`
	Thumb   string `xml:"thumb,omitempty"`
	Profile string `xml:"profile"`
}

// MovieNFO structure for movie metadata NFO
type MovieNFO struct {
	XMLName       xml.Name `xml:"movie"`
	Title         string   `xml:"title"`
	OriginalTitle string   `xml:"originaltitle"`
	Overview      string   `xml:"overview"`
	Year          int      `xml:"year"`
	ReleaseDate   string   `xml:"releasedate"`
	UniqueID      UniqueID `xml:"uniqueid"`
	Rating        float64  `xml:"rating"`
	Runtime       int      `xml:"runtime"`
	Genres        []string `xml:"genre"`
	Actors        []Actor  `xml:"actor"`
}

// TVShowNFO structure for TV show metadata NFO
type TVShowNFO struct {
	XMLName       xml.Name `xml:"tvshow"`
	Title         string   `xml:"title"`
	OriginalTitle string   `xml:"originaltitle"`
	Overview      string   `xml:"overview"`
	Year          int      `xml:"year"`
	FirstAired    string   `xml:"firstaired"`
	UniqueID      UniqueID `xml:"uniqueid"`
	Rating        float64  `xml:"rating"`
	Genres        []string `xml:"genre"`
	Actors        []Actor  `xml:"actor"`
}

// SeasonNFO structure for TV season metadata NFO
type SeasonNFO struct {
	XMLName      xml.Name `xml:"season"`
	Title        string   `xml:"title"`
	SeasonNumber int      `xml:"seasonnumber"`
	UniqueID     UniqueID `xml:"uniqueid"`
}

// EpisodeNFO structure for TV episode details NFO
type EpisodeNFO struct {
	XMLName       xml.Name `xml:"episodedetails"`
	Title         string   `xml:"title"`
	Season        int      `xml:"season"`
	Episode       int      `xml:"episode"`
	Aired         string   `xml:"aired"`
	Overview      string   `xml:"overview"`
	UniqueID      UniqueID `xml:"uniqueid"`
	Rating        float64  `xml:"rating"`
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

func marshalXML(val interface{}) ([]byte, error) {
	body, err := xml.MarshalIndent(val, "", "    ")
	if err != nil {
		return nil, err
	}
	return []byte(xmlHeader + string(body)), nil
}

// GenerateMovieNFO creates NFO data for a movie. When cast is non-empty, its
// members are written as <actor> entries (演职人员).
func GenerateMovieNFO(detail *tmdb.MovieDetail, cast []tmdb.Cast) ([]byte, error) {
	year := 0
	if len(detail.ReleaseDate) >= 4 {
		fmt.Sscanf(detail.ReleaseDate[:4], "%d", &year)
	}

	var genres []string
	for _, g := range detail.Genres {
		genres = append(genres, g.Name)
	}

	nfo := MovieNFO{
		Title:         detail.Title,
		OriginalTitle: detail.OriginalTitle,
		Overview:      detail.Overview,
		Year:          year,
		ReleaseDate:   detail.ReleaseDate,
		UniqueID: UniqueID{
			Type:    "tmdb",
			Default: "true",
			Value:   detail.ID,
		},
		Rating:  detail.VoteAverage,
		Runtime: detail.Runtime,
		Genres:  genres,
		Actors:  buildActors(cast),
	}

	return marshalXML(nfo)
}

// GenerateTVShowNFO creates NFO data for a TV series. When cast is non-empty,
// its members are written as <actor> entries (演职人员).
func GenerateTVShowNFO(detail *tmdb.TVDetail, cast []tmdb.Cast) ([]byte, error) {
	year := 0
	if len(detail.FirstAirDate) >= 4 {
		fmt.Sscanf(detail.FirstAirDate[:4], "%d", &year)
	}

	var genres []string
	for _, g := range detail.Genres {
		genres = append(genres, g.Name)
	}

	nfo := TVShowNFO{
		Title:         detail.Name,
		OriginalTitle: detail.OriginalName,
		Overview:      detail.Overview,
		Year:          year,
		FirstAired:    detail.FirstAirDate,
		UniqueID: UniqueID{
			Type:    "tmdb",
			Default: "true",
			Value:   detail.ID,
		},
		Rating: detail.VoteAverage,
		Genres: genres,
		Actors: buildActors(cast),
	}

	return marshalXML(nfo)
}

// buildActors converts TMDB cast members into NFO <actor> entries. Only people
// in the Acting department are kept (matching MoviePilot); profile images are
// referenced by their TMDB URL and the thumb is omitted when none exists.
func buildActors(cast []tmdb.Cast) []Actor {
	if len(cast) == 0 {
		return nil
	}
	actors := make([]Actor, 0, len(cast))
	for _, m := range cast {
		if m.Name == "" {
			continue
		}
		if m.KnownForDepartment != "" && m.KnownForDepartment != "Acting" {
			continue
		}
		a := Actor{
			Name:    m.Name,
			Type:    "Actor",
			Role:    m.Character,
			TMDBID:  m.ID,
			Profile: fmt.Sprintf("https://www.themoviedb.org/person/%d", m.ID),
		}
		if m.ProfilePath != "" {
			a.Thumb = tmdb.GetImageURL(m.ProfilePath, "w500")
		}
		actors = append(actors, a)
	}
	if len(actors) == 0 {
		return nil
	}
	return actors
}

// GenerateSeasonNFO creates NFO data for a specific TV season
func GenerateSeasonNFO(tvDetail *tmdb.TVDetail, seasonNumber int) ([]byte, error) {
	seasonName := fmt.Sprintf("季 %d", seasonNumber)
	for _, s := range tvDetail.Seasons {
		if s.SeasonNumber == seasonNumber {
			seasonName = s.Name
			break
		}
	}

	nfo := SeasonNFO{
		Title:        seasonName,
		SeasonNumber: seasonNumber,
		UniqueID: UniqueID{
			Type:    "tmdb",
			Default: "true",
			Value:   tvDetail.ID,
		},
	}

	return marshalXML(nfo)
}

// GenerateEpisodeNFO creates NFO data for a specific TV episode
func GenerateEpisodeNFO(episode *tmdb.TVEpisode) ([]byte, error) {
	nfo := EpisodeNFO{
		Title:    episode.Name,
		Season:   episode.SeasonNumber,
		Episode:  episode.EpisodeNumber,
		Aired:    episode.AirDate,
		Overview: episode.Overview,
		UniqueID: UniqueID{
			Type:    "tmdb",
			Default: "true",
			Value:   episode.ID,
		},
		Rating: episode.VoteAverage,
	}

	return marshalXML(nfo)
}
