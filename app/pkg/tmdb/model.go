package tmdb

type SearchMovieResult struct {
	ID            int     `json:"id"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	ReleaseDate   string  `json:"release_date"`
	PosterPath    string  `json:"poster_path"`
	BackdropPath  string  `json:"backdrop_path"`
	Overview      string  `json:"overview"`
	VoteAverage   float64 `json:"vote_average"`
	VoteCount     int     `json:"vote_count"`
	Popularity    float64 `json:"popularity"`
}

type SearchMovieResponse struct {
	Results []SearchMovieResult `json:"results"`
}

type SearchTVResult struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	OriginalName string   `json:"original_name"`
	FirstAirDate string   `json:"first_air_date"`
	PosterPath   string   `json:"poster_path"`
	BackdropPath string   `json:"backdrop_path"`
	Overview     string   `json:"overview"`
	VoteAverage  float64  `json:"vote_average"`
	VoteCount    int      `json:"vote_count"`
	Popularity   float64  `json:"popularity"`
}

type SearchTVResponse struct {
	Results []SearchTVResult `json:"results"`
}

type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type MovieDetail struct {
	ID           int     `json:"id"`
	Title        string  `json:"title"`
	Overview     string  `json:"overview"`
	ReleaseDate  string  `json:"release_date"`
	PosterPath   string  `json:"poster_path"`
	BackdropPath string  `json:"backdrop_path"`
	VoteAverage  float64 `json:"vote_average"`
	Runtime      int     `json:"runtime"`
	Genres       []Genre `json:"genres"`
	OriginalTitle string `json:"original_title"`
	// OriginalLanguage is the ISO-639-1 code of the original title, used to
	// tag aliases by language.
	OriginalLanguage string `json:"original_language"`
}

type Season struct {
	ID           int    `json:"id"`
	AirDate      string `json:"air_date"`
	EpisodeCount int    `json:"episode_count"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"poster_path"`
	SeasonNumber int    `json:"season_number"`
}

type TVDetail struct {
	ID            int      `json:"id"`
	Name          string   `json:"name"`
	Overview      string   `json:"overview"`
	FirstAirDate  string   `json:"first_air_date"`
	PosterPath    string   `json:"poster_path"`
	BackdropPath  string   `json:"backdrop_path"`
	VoteAverage   float64  `json:"vote_average"`
	Genres        []Genre  `json:"genres"`
	Seasons       []Season `json:"seasons"`
	OriginalName  string   `json:"original_name"`
	// OriginalLanguage mirrors MovieDetail for language-aware aliases.
	OriginalLanguage string `json:"original_language"`
}

// AlternativeTitle is one entry of TMDB's alternative_titles payload.
type AlternativeTitle struct {
	ISO3166_1 string `json:"iso_3166_1"`
	Title     string `json:"title"`
	Type      string `json:"type"`
}

// AlternativeTitlesResponse is the movie payload for /movie/{id}/alternative_titles.
type AlternativeTitlesResponse struct {
	ID     int                `json:"id"`
	Titles []AlternativeTitle `json:"titles"`
}

// TVAlternativeTitlesResponse nests the same data under "results" for TV.
type TVAlternativeTitlesResponse struct {
	ID      int                `json:"id"`
	Results []AlternativeTitle `json:"results"`
}

type TVSeasonDetail struct {
	ID           int         `json:"id"`
	Name         string      `json:"name"`
	Overview     string      `json:"overview"`
	PosterPath   string      `json:"poster_path"`
	SeasonNumber int         `json:"season_number"`
	Episodes     []TVEpisode `json:"episodes"`
}

type TVEpisode struct {
	ID             int     `json:"id"`
	Name           string  `json:"name"`
	Overview       string  `json:"overview"`
	AirDate        string  `json:"air_date"`
	EpisodeNumber  int     `json:"episode_number"`
	SeasonNumber   int     `json:"season_number"`
	StillPath      string  `json:"still_path"`
	VoteAverage    float64 `json:"vote_average"`
}

type ImageItem struct {
	FilePath string `json:"file_path"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type ImagesResponse struct {
	Backdrops []ImageItem `json:"backdrops"`
	Posters   []ImageItem `json:"posters"`
	Logos     []ImageItem `json:"logos"`
	Stills    []ImageItem `json:"stills"`
}

// Cast represents a single cast member from a credits response.
type Cast struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	Character          string `json:"character"`
	Order              int    `json:"order"`
	ProfilePath        string `json:"profile_path"`
	KnownForDepartment string `json:"known_for_department"`
}

// CrewMember represents a single crew member from a credits response.
// Used to derive <director> for movie/episode NFO.
type CrewMember struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Department string `json:"department"`
	Job        string `json:"job"`
}

// CreditsResponse is the payload of /movie/{id}/credits, /tv/{id}/credits
// and /tv/{id}/season/{s}/episode/{e}/credits. Cast is the on-screen
// performers; Crew is the behind-the-camera staff (directors, writers, etc.).
type CreditsResponse struct {
	Cast []Cast       `json:"cast"`
	Crew []CrewMember `json:"crew"`
}
