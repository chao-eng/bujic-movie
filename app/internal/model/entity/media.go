package entity

import (
	"time"

	"gorm.io/gorm"
)

type Media struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	TMDBID       int       `gorm:"index;column:tmdb_id" json:"tmdb_id"`
	Title        string    `gorm:"column:title" json:"title"`
	Year         int       `gorm:"column:year" json:"year"`
	Season       int       `gorm:"column:season" json:"season"`
	Type         string    `gorm:"index;column:type" json:"type"` // "movie" or "tv"
	Path         string    `gorm:"column:path" json:"path"`
	PosterPath   string    `gorm:"column:poster_path" json:"poster_path"`
	BackdropPath string    `gorm:"column:backdrop_path" json:"backdrop_path"`
	ScrapedAt    time.Time `gorm:"column:scraped_at" json:"scraped_at"`
}

func (Media) TableName() string {
	return "medias"
}
