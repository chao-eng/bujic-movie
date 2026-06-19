package nfo

import (
	"encoding/xml"
	"fmt"
	"time"

	"github.com/bujic-movie/bujic-movie/pkg/mediainfo"
	"github.com/bujic-movie/bujic-movie/pkg/tmdb"
)

// xmlHeader matches the Jellyfin/Emby/Kodi NFO header byte-for-byte.
const xmlHeader = `<?xml version="1.0" encoding="utf-8" standalone="yes"?>` + "\n"

// ----- atomic XML shapes -----

// UniqueID represents a metadata provider ID tag in NFO XML.
type UniqueID struct {
	Type    string `xml:"type,attr"`
	Default string `xml:"default,attr"`
	Value   int    `xml:",chardata"`
}

// Actor represents a cast member in NFO XML (Emby/Jellyfin/Kodi format).
// Field order mirrors MoviePilot's scraper output.
type Actor struct {
	Name  string `xml:"name"`
	Type  string `xml:"type"`
	Role  string `xml:"role,omitempty"`
	Thumb string `xml:"thumb,omitempty"`
}

// Art holds artwork references. Only <poster> is currently emitted.
type Art struct {
	Poster string `xml:"poster,omitempty"`
}

// Fileinfo / StreamDetails / *Stream mirror Jellyfin's <fileinfo> block.

type Fileinfo struct {
	StreamDetails *mediainfo.StreamDetails `xml:"streamdetails"`
}

// ----- NFO root types -----

// MovieNFO structure for movie metadata NFO.
type MovieNFO struct {
	XMLName          xml.Name        `xml:"movie"`
	Plot             string          `xml:"plot"`
	Lockdata         string          `xml:"lockdata"`
	DateAdded        string          `xml:"dateadded"`
	Title            string          `xml:"title"`
	OriginalTitle    string          `xml:"originaltitle"`
	Director         []string        `xml:"director"`
	Rating           float64         `xml:"rating"`
	Year             int             `xml:"year"`
	ReleaseDate      string          `xml:"releasedate"`
	Mpaa             string          `xml:"mpaa"`
	CollectionNumber int             `xml:"collectionnumber"`
	TMDBID           int             `xml:"tmdbid"`
	Runtime          int             `xml:"runtime"`
	Country          []string        `xml:"country"`
	UniqueID         UniqueID        `xml:"uniqueid"`
	Genres           []string        `xml:"genre"`
	Actors           []Actor         `xml:"actor"`
	Art              Art             `xml:"art"`
	Fileinfo         *Fileinfo       `xml:"fileinfo"`
	Overview         string          `xml:"overview,omitempty"`
}

// TVShowNFO structure for TV show metadata NFO.
type TVShowNFO struct {
	XMLName          xml.Name   `xml:"tvshow"`
	Plot             string     `xml:"plot"`
	Lockdata         string     `xml:"lockdata"`
	DateAdded        string     `xml:"dateadded"`
	Title            string     `xml:"title"`
	OriginalTitle    string     `xml:"originaltitle"`
	Rating           float64    `xml:"rating"`
	Year             int        `xml:"year"`
	FirstAired       string     `xml:"firstaired"`
	Mpaa             string     `xml:"mpaa"`
	CollectionNumber int        `xml:"collectionnumber"`
	TMDBID           int        `xml:"tmdbid"`
	Country          []string   `xml:"country"`
	UniqueID         UniqueID   `xml:"uniqueid"`
	Genres           []string   `xml:"genre"`
	Actors           []Actor    `xml:"actor"`
	Art              Art        `xml:"art"`
	Overview         string     `xml:"overview,omitempty"`
}

// SeasonNFO structure for TV season metadata NFO.
type SeasonNFO struct {
	XMLName          xml.Name  `xml:"season"`
	Plot             string    `xml:"plot"`
	Lockdata         string    `xml:"lockdata"`
	DateAdded        string    `xml:"dateadded"`
	Title            string    `xml:"title"`
	Year             int       `xml:"year"`
	SeasonNumber     int       `xml:"seasonnumber"`
	TMDBID           int       `xml:"tmdbid"`
	UniqueID         UniqueID  `xml:"uniqueid"`
	Art              Art       `xml:"art"`
	Overview         string    `xml:"overview,omitempty"`
}

// EpisodeNFO structure for TV episode details NFO. Season/Episode/Year/etc.
// are *int so a legal value of 0 (e.g. Season 0 for specials) is still
// emitted; encoding/xml silently drops int zero values otherwise.
type EpisodeNFO struct {
	XMLName          xml.Name  `xml:"episodedetails"`
	Plot             string    `xml:"plot"`
	Lockdata         string    `xml:"lockdata"`
	DateAdded        string    `xml:"dateadded"`
	Title            string    `xml:"title"`
	Director         []string  `xml:"director"`
	Rating           *float64  `xml:"rating"`
	Year             *int      `xml:"year"`
	Mpaa             string    `xml:"mpaa"`
	CollectionNumber *int      `xml:"collectionnumber"`
	TMDBID           *int      `xml:"tmdbid"`
	Runtime          int       `xml:"runtime"`
	Country          []string  `xml:"country"`
	Art              Art       `xml:"art"`
	Actors           []Actor   `xml:"actor"`
	Showtitle        string    `xml:"showtitle"`
	Episode          *int      `xml:"episode"`
	Season           *int      `xml:"season"`
	Aired            string    `xml:"aired"`
	Fileinfo         *Fileinfo `xml:"fileinfo"`
	UniqueID         UniqueID  `xml:"uniqueid"`
	Overview         string    `xml:"overview,omitempty"`
}

// ----- options / shared payload -----

// Common carries fields shared by every NFO type. Fields are duplicated
// (rather than embedded) on each options struct so struct literals in
// tests and call sites stay explicit and don't trip promoted-field
// init rules.
type Common struct {
	LockData  string
	DateAdded time.Time
	MPAA      string
	Year      int
	Runtime   int
	Country   []string
	Art       Art
}

// MovieOptions feeds GenerateMovieNFO.
type MovieOptions struct {
	LockData  string
	DateAdded time.Time
	MPAA      string
	Year      int
	Runtime   int
	Country   []string
	Art       Art

	Detail    *tmdb.MovieDetail
	Directors []string
	Actors    []tmdb.Cast
	TMDBID    int
	Stream    *mediainfo.StreamDetails
}

// TVShowOptions feeds GenerateTVShowNFO.
type TVShowOptions struct {
	LockData  string
	DateAdded time.Time
	MPAA      string
	Year      int
	Runtime   int
	Country   []string
	Art       Art

	Detail *tmdb.TVDetail
	Actors []tmdb.Cast
	TMDBID int
	Stream *mediainfo.StreamDetails
}

// SeasonOptions feeds GenerateSeasonNFO.
type SeasonOptions struct {
	LockData  string
	DateAdded time.Time
	MPAA      string
	Year      int
	Runtime   int
	Country   []string
	Art       Art

	Detail *tmdb.TVDetail
	Season int
	TMDBID int
	Stream *mediainfo.StreamDetails
}

// EpisodeOptions feeds GenerateEpisodeNFO.
type EpisodeOptions struct {
	LockData  string
	DateAdded time.Time
	MPAA      string
	Year      int
	Runtime   int
	Country   []string
	Art       Art

	Episode    *tmdb.TVEpisode
	ShowTitle  string
	ShowTMDBID int
	ArtPoster  string
	Directors  []string
	Actors     []tmdb.Cast
	Stream     *mediainfo.StreamDetails
}

// ----- helpers -----

func intPtr(v int) *int { return &v }
func floatPtr(v float64) *float64 { return &v }

func formatDateAdded(t time.Time) string {
	if t.IsZero() {
		return time.Now().Format("2006-01-02 15:04:05")
	}
	return t.Format("2006-01-02 15:04:05")
}

func buildFileinfo(stream *mediainfo.StreamDetails) *Fileinfo {
	if stream == nil {
		return nil
	}
	return &Fileinfo{StreamDetails: stream}
}

// buildActors converts TMDB cast members into NFO <actor> entries. Only
// people in the Acting department are kept (matching MoviePilot).
func buildActors(cast []tmdb.Cast) []Actor {
	if len(cast) == 0 {
		return nil
	}
	out := make([]Actor, 0, len(cast))
	for _, m := range cast {
		if m.Name == "" {
			continue
		}
		if m.KnownForDepartment != "" && m.KnownForDepartment != "Acting" {
			continue
		}
		a := Actor{
			Name: m.Name,
			Type: "Actor",
			Role: m.Character,
		}
		if m.ProfilePath != "" {
			a.Thumb = tmdb.GetImageURL(m.ProfilePath, "original")
		}
		out = append(out, a)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func marshalXML(val interface{}) ([]byte, error) {
	body, err := xml.MarshalIndent(val, "", "  ")
	if err != nil {
		return nil, err
	}
	return []byte(xmlHeader + string(body)), nil
}

func genres(detail interface{}) []string {
	type genreSource interface{ GenreNames() []string }
	switch d := detail.(type) {
	case *tmdb.MovieDetail:
		names := make([]string, 0, len(d.Genres))
		for _, g := range d.Genres {
			names = append(names, g.Name)
		}
		return names
	case *tmdb.TVDetail:
		names := make([]string, 0, len(d.Genres))
		for _, g := range d.Genres {
			names = append(names, g.Name)
		}
		return names
	}
	return nil
}

// ExtractDirectors filters crew members down to those whose department
// and job both indicate the director role, preserving order.
func ExtractDirectors(crew []tmdb.CrewMember) []string {
	if len(crew) == 0 {
		return nil
	}
	out := make([]string, 0, 2)
	for _, c := range crew {
		if c.Name == "" {
			continue
		}
		if c.Department == "Directing" && c.Job == "Director" {
			out = append(out, c.Name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ----- generators -----

// GenerateMovieNFO creates NFO data for a movie.
func GenerateMovieNFO(opts MovieOptions) ([]byte, error) {
	d := opts.Detail
	year := 0
	if len(d.ReleaseDate) >= 4 {
		fmt.Sscanf(d.ReleaseDate[:4], "%d", &year)
	}
	if opts.Year == 0 {
		opts.Year = year
	}

	tmdbID := opts.TMDBID
	if tmdbID == 0 {
		tmdbID = d.ID
	}

	nfo := MovieNFO{
		Plot:             d.Overview,
		Lockdata:         opts.LockData,
		DateAdded:        formatDateAdded(opts.DateAdded),
		Title:            d.Title,
		OriginalTitle:    d.OriginalTitle,
		Director:         opts.Directors,
		Rating:           d.VoteAverage,
		Year:             opts.Year,
		ReleaseDate:      d.ReleaseDate,
		Mpaa:             opts.MPAA,
		CollectionNumber: tmdbID,
		TMDBID:           tmdbID,
		Runtime:          opts.Runtime,
		Country:          opts.Country,
		UniqueID: UniqueID{
			Type:    "tmdb",
			Default: "true",
			Value:   tmdbID,
		},
		Genres:   genres(d),
		Actors:   buildActors(opts.Actors),
		Art:      opts.Art,
		Fileinfo: buildFileinfo(opts.Stream),
		Overview: d.Overview,
	}
	if nfo.Runtime == 0 {
		nfo.Runtime = d.Runtime
	}
	return marshalXML(nfo)
}

// GenerateTVShowNFO creates NFO data for a TV series.
func GenerateTVShowNFO(opts TVShowOptions) ([]byte, error) {
	d := opts.Detail
	year := 0
	if len(d.FirstAirDate) >= 4 {
		fmt.Sscanf(d.FirstAirDate[:4], "%d", &year)
	}
	if opts.Year == 0 {
		opts.Year = year
	}
	tmdbID := opts.TMDBID
	if tmdbID == 0 {
		tmdbID = d.ID
	}

	nfo := TVShowNFO{
		Plot:             d.Overview,
		Lockdata:         opts.LockData,
		DateAdded:        formatDateAdded(opts.DateAdded),
		Title:            d.Name,
		OriginalTitle:    d.OriginalName,
		Rating:           d.VoteAverage,
		Year:             opts.Year,
		FirstAired:       d.FirstAirDate,
		Mpaa:             opts.MPAA,
		CollectionNumber: tmdbID,
		TMDBID:           tmdbID,
		Country:          opts.Country,
		UniqueID: UniqueID{
			Type:    "tmdb",
			Default: "true",
			Value:   tmdbID,
		},
		Genres:   genres(d),
		Actors:   buildActors(opts.Actors),
		Art:      opts.Art,
		Overview: d.Overview,
	}
	return marshalXML(nfo)
}

// GenerateSeasonNFO creates NFO data for a specific TV season.
func GenerateSeasonNFO(opts SeasonOptions) ([]byte, error) {
	d := opts.Detail
	year := 0
	if len(d.FirstAirDate) >= 4 {
		fmt.Sscanf(d.FirstAirDate[:4], "%d", &year)
	}
	seasonName := fmt.Sprintf("季 %d", opts.Season)
	for _, s := range d.Seasons {
		if s.SeasonNumber == opts.Season {
			seasonName = s.Name
			if len(s.AirDate) >= 4 {
				fmt.Sscanf(s.AirDate[:4], "%d", &year)
			}
			break
		}
	}
	if opts.Year == 0 {
		opts.Year = year
	}
	tmdbID := opts.TMDBID
	if tmdbID == 0 {
		tmdbID = d.ID
	}

	nfo := SeasonNFO{
		Plot:         d.Overview,
		Lockdata:     opts.LockData,
		DateAdded:    formatDateAdded(opts.DateAdded),
		Title:        seasonName,
		Year:         opts.Year,
		SeasonNumber: opts.Season,
		TMDBID:       tmdbID,
		UniqueID: UniqueID{
			Type:    "tmdb",
			Default: "true",
			Value:   tmdbID,
		},
		Art:      opts.Art,
		Overview: d.Overview,
	}
	return marshalXML(nfo)
}

// GenerateEpisodeNFO creates NFO data for a specific TV episode.
func GenerateEpisodeNFO(opts EpisodeOptions) ([]byte, error) {
	ep := opts.Episode
	tmdbShowID := opts.ShowTMDBID
	year := 0
	if len(ep.AirDate) >= 4 {
		fmt.Sscanf(ep.AirDate[:4], "%d", &year)
	}
	if opts.Year == 0 {
		opts.Year = year
	}

	// Art.Poster — the per-episode thumbnail path under the library root.
	art := opts.Art
	if opts.ArtPoster != "" {
		art.Poster = opts.ArtPoster
	}

	nfo := EpisodeNFO{
		Plot:             ep.Overview,
		Lockdata:         opts.LockData,
		DateAdded:        formatDateAdded(opts.DateAdded),
		Title:            ep.Name,
		Director:         opts.Directors,
		Rating:           floatPtr(ep.VoteAverage),
		Year:             intPtr(opts.Year),
		Mpaa:             opts.MPAA,
		CollectionNumber: intPtr(tmdbShowID),
		TMDBID:           intPtr(tmdbShowID),
		Runtime:          opts.Runtime,
		Country:          opts.Country,
		UniqueID: UniqueID{
			Type:    "tmdb",
			Default: "true",
			Value:   ep.ID,
		},
		Art:       art,
		Actors:    buildActors(opts.Actors),
		Showtitle: opts.ShowTitle,
		Episode:   intPtr(ep.EpisodeNumber),
		Season:    intPtr(ep.SeasonNumber),
		Aired:     ep.AirDate,
		Fileinfo:  buildFileinfo(opts.Stream),
		Overview:  ep.Overview,
	}
	return marshalXML(nfo)
}
