package service

import (
	"context"
	"errors"
	"strings"

	"github.com/bujic-movie/bujic-movie/pkg/parser"
	"github.com/bujic-movie/bujic-movie/pkg/tmdb"
)

type RecognizeService interface {
	Recognize(ctx context.Context, path string) (*parser.Metadata, interface{}, error)
	RecognizeWithType(ctx context.Context, path string, mediaType string) (*parser.Metadata, interface{}, error)
}

type recognizeService struct {
	tmdbClient *tmdb.Client
}

func NewRecognizeService(tmdbClient *tmdb.Client) RecognizeService {
	return &recognizeService{tmdbClient: tmdbClient}
}

// Recognize parses path, queries TMDB and selects the best matching Movie or TV show metadata
func (s *recognizeService) Recognize(ctx context.Context, path string) (*parser.Metadata, interface{}, error) {
	return s.RecognizeWithType(ctx, path, "")
}

// RecognizeWithType parses path and queries TMDB with optional manual media type override.
// mediaType: "movie" forces movie search, "tv" forces TV search, "" auto-detects.
func (s *recognizeService) RecognizeWithType(ctx context.Context, path string, mediaType string) (*parser.Metadata, interface{}, error) {
	meta := parser.ParseFilename(path)
	if meta.Title == "" {
		return nil, nil, errors.New("failed to parse video title from filename")
	}

	// Override media type if manually specified
	isMovie := meta.IsMovie
	if mediaType == "movie" {
		isMovie = true
		meta.IsMovie = true
	} else if mediaType == "tv" {
		isMovie = false
		meta.IsMovie = false
	}

	if isMovie {
		results, err := s.tmdbClient.SearchMovie(ctx, meta.Title, meta.Year)
		if err != nil {
			return nil, nil, err
		}

		if len(results) == 0 {
			// Try without year if we failed to find any
			if meta.Year > 0 {
				results, err = s.tmdbClient.SearchMovie(ctx, meta.Title, 0)
			}
			if err != nil || len(results) == 0 {
				return nil, nil, errors.New("no matching movie found on TMDB")
			}
		}

		// Select best match (first result or exact title match)
		bestIdx := 0
		for idx, r := range results {
			if strings.EqualFold(r.Title, meta.Title) || strings.EqualFold(r.OriginalTitle, meta.Title) {
				bestIdx = idx
				break
			}
		}

		detail, err := s.tmdbClient.GetMovieDetail(ctx, results[bestIdx].ID)
		if err != nil {
			return nil, nil, err
		}
		return meta, detail, nil
	} else {
		results, err := s.tmdbClient.SearchTV(ctx, meta.Title, meta.Year)
		if err != nil {
			return nil, nil, err
		}

		if len(results) == 0 {
			if meta.Year > 0 {
				results, err = s.tmdbClient.SearchTV(ctx, meta.Title, 0)
			}
			if err != nil || len(results) == 0 {
				return nil, nil, errors.New("no matching TV series found on TMDB")
			}
		}

		bestIdx := 0
		for idx, r := range results {
			if strings.EqualFold(r.Name, meta.Title) || strings.EqualFold(r.OriginalName, meta.Title) {
				bestIdx = idx
				break
			}
		}

		detail, err := s.tmdbClient.GetTVDetail(ctx, results[bestIdx].ID)
		if err != nil {
			return nil, nil, err
		}
		return meta, detail, nil
	}
}
