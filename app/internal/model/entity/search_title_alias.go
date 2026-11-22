package entity

import "time"

// SearchTitleAlias caches the titles a keyword resolved to on TMDB. One row per
// (keyword, media type) so a manual confirmation stays stable across searches.
// CandidatesJSON holds the full result list shown to the user; AliasesJSON holds
// the titles actually used for indexer queries.
type SearchTitleAlias struct {
	ID          uint      `gorm:"primaryKey;column:id" json:"id"`
	Keyword     string    `gorm:"column:keyword;not null;index:idx_search_alias_key,unique" json:"keyword"`
	MediaType   string    `gorm:"column:media_type;not null;index:idx_search_alias_key,unique" json:"media_type"` // movie / tv
	TMDBID      int       `gorm:"column:tmdb_id;not null;default:0" json:"tmdb_id"`
	Title       string    `gorm:"column:title" json:"title"`
	OriginalTitle string  `gorm:"column:original_title" json:"original_title"`
	Year        int       `gorm:"column:year;not null;default:0" json:"year"`
	AliasesJSON string    `gorm:"column:aliases_json" json:"-"`
	CandidatesJSON string `gorm:"column:candidates_json" json:"-"`
	// Confirmed is set when the user picked a candidate, so resolution skips the
	// list and reuses the choice.
	Confirmed bool      `gorm:"column:confirmed;not null;default:false" json:"confirmed"`
	Stale     bool      `gorm:"column:stale;not null;default:false" json:"stale"`
	CreatedAt time.Time `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null;index" json:"updated_at"`
}

func (SearchTitleAlias) TableName() string {
	return "search_title_aliases"
}
