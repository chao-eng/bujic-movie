package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/internal/storage"
	"github.com/bujic-movie/bujic-movie/pkg/fileutil"
	"github.com/bujic-movie/bujic-movie/pkg/nfo"
	"github.com/bujic-movie/bujic-movie/pkg/parser"
	"github.com/bujic-movie/bujic-movie/pkg/tmdb"
)

type ScrapeService interface {
	ScrapePath(ctx context.Context, path string, overwrite bool) error
}

type scrapeService struct {
	mediaRepo        repository.MediaRepository
	recognizeService RecognizeService
	tmdbClient       *tmdb.Client
	storage          storage.Storage
}

func NewScrapeService(
	mediaRepo repository.MediaRepository,
	recognizeService RecognizeService,
	tmdbClient *tmdb.Client,
	stg storage.Storage,
) ScrapeService {
	return &scrapeService{
		mediaRepo:        mediaRepo,
		recognizeService: recognizeService,
		tmdbClient:       tmdbClient,
		storage:          stg,
	}
}

// ScrapePath handles scraping metadata for a given path (file or directory)
func (s *scrapeService) ScrapePath(ctx context.Context, path string, overwrite bool) error {
	// Redirect Season folder to the TV show root directory
	if s.storage.IsDir(path) {
		dirName := filepath.Base(path)
		seasonDirReg := regexp.MustCompile(`(?i)\b(season|specials?)\s*\d*\b`)
		if seasonDirReg.MatchString(dirName) {
			path = filepath.Dir(path)
		}
	}

	isDir := s.storage.IsDir(path)

	if !isDir {
		// 1. Scrape Single Video File
		return s.scrapeSingleFile(ctx, path, overwrite)
	}

	// 2. Check for Blu-ray folder structure
	if s.storage.IsBluray(path) {
		// Scrape ONLY the root of Blu-ray directory and do NOT recurse
		return s.scrapeBluRayFolder(ctx, path, overwrite)
	}

	// 3. Recursive directory scraping:
	// A. Collect all directories and sort by depth ascending (shallowest first)
	subDirs, err := fileutil.GetDirsSortedByDepth(path)
	if err != nil {
		return err
	}

	// Perform initial match on the root path
	meta, details, err := s.recognizeService.Recognize(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to recognize root directory %s: %w", path, err)
	}

	// B. Initialize directories metadata
	if !meta.IsMovie {
		tvDetail := details.(*tmdb.TVDetail)
		// Process main TV root
		if err := s.scrapeTVDirectory(ctx, path, tvDetail, overwrite); err != nil {
			return err
		}

		// Process sub-directories (look for Season folders)
		seasonDirReg := regexp.MustCompile(`(?i)\bseason\s*(\d+)\b`)
		for _, dir := range subDirs {
			dirName := filepath.Base(dir)
			if match := seasonDirReg.FindStringSubmatch(dirName); match != nil {
				seasonNum := 1
				fmt.Sscanf(match[1], "%d", &seasonNum)
				if err := s.scrapeSeasonDirectory(ctx, dir, tvDetail, seasonNum, overwrite); err != nil {
					return err
				}
			}
		}
	} else {
		// For movies, if root is a directory but not Blu-ray, initialize root NFO and movie images
		movieDetail := details.(*tmdb.MovieDetail)
		if err := s.initializeMovieDirectory(ctx, path, movieDetail, overwrite); err != nil {
			return err
		}
	}

	// C. Find all video files recursively and scrape them
	videoFiles, err := fileutil.FindFiles(path, fileutil.IsVideo)
	if err != nil {
		return err
	}

	for _, vf := range videoFiles {
		if err := s.scrapeSingleFile(ctx, vf, overwrite); err != nil {
			// Log error but continue with other files
			fmt.Printf("Error scraping video file %s: %v\n", vf, err)
		}
	}

	return nil
}

func (s *scrapeService) scrapeSingleFile(ctx context.Context, path string, overwrite bool) error {
	meta, details, err := s.recognizeService.Recognize(ctx, path)
	if err != nil {
		return err
	}

	if meta.IsMovie {
		movieDetail := details.(*tmdb.MovieDetail)
		return s.scrapeMovieFile(ctx, path, movieDetail, overwrite)
	} else {
		tvDetail := details.(*tmdb.TVDetail)
		return s.scrapeTVEpisodeFile(ctx, path, meta, tvDetail, overwrite)
	}
}

func (s *scrapeService) scrapeMovieFile(ctx context.Context, path string, detail *tmdb.MovieDetail, overwrite bool) error {
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	baseName := strings.TrimSuffix(filepath.Base(path), ext)
	nfoPath := filepath.Join(dir, baseName+".nfo")

	// 1. Write NFO File if not exists or overwrite is true
	if overwrite || !fileExists(nfoPath) {
		xmlData, err := nfo.GenerateMovieNFO(detail)
		if err != nil {
			return err
		}
		if err := s.storage.Write(nfoPath, strings.NewReader(string(xmlData))); err != nil {
			return err
		}
		_ = fileutil.ChmodWithUmask(nfoPath, false)
	}

	// 2. Download Movie Images (poster, backdrop)
	if detail.PosterPath != "" {
		posterURL := tmdb.GetImageURL(detail.PosterPath, "w500")
		posterDst := filepath.Join(dir, "poster.jpg")
		if overwrite || !fileExists(posterDst) {
			if err := tmdb.DownloadImage(posterURL, posterDst); err == nil {
				_ = fileutil.ChmodWithUmask(posterDst, false)
				// Create Plex/Kodi alias copies
				s.createAliases(posterDst, "folder.jpg")
			}
		}
	}

	if detail.BackdropPath != "" {
		backdropURL := tmdb.GetImageURL(detail.BackdropPath, "original")
		backdropDst := filepath.Join(dir, "backdrop.jpg")
		if overwrite || !fileExists(backdropDst) {
			if err := tmdb.DownloadImage(backdropURL, backdropDst); err == nil {
				_ = fileutil.ChmodWithUmask(backdropDst, false)
				s.createAliases(backdropDst, "fanart.jpg")
			}
		}
	}

	// 3. Record in Database
	s.saveToDB(detail.ID, detail.Title, detail.ReleaseDate, "movie", path, detail.PosterPath, detail.BackdropPath)

	return nil
}

func (s *scrapeService) scrapeTVEpisodeFile(ctx context.Context, path string, meta *parser.Metadata, detail *tmdb.TVDetail, overwrite bool) error {
	if len(meta.Episodes) == 0 {
		return errors.New("no episode number identified from TV filename")
	}

	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	baseName := strings.TrimSuffix(filepath.Base(path), ext)
	nfoPath := filepath.Join(dir, baseName+".nfo")

	// Get season detail from TMDB
	seasonDetail, err := s.tmdbClient.GetTVSeasonDetail(ctx, detail.ID, meta.Season)
	if err != nil {
		return err
	}

	// Find the exact episode detail
	var matchedEpisode *tmdb.TVEpisode
	targetEpisodeNum := meta.Episodes[0]
	for _, ep := range seasonDetail.Episodes {
		if ep.EpisodeNumber == targetEpisodeNum {
			matchedEpisode = &ep
			break
		}
	}

	if matchedEpisode == nil {
		return fmt.Errorf("episode %d not found in TV Season details for TMDB %d", targetEpisodeNum, detail.ID)
	}

	// 1. Write Episode NFO
	if overwrite || !fileExists(nfoPath) {
		xmlData, err := nfo.GenerateEpisodeNFO(matchedEpisode)
		if err != nil {
			return err
		}
		if err := s.storage.Write(nfoPath, strings.NewReader(string(xmlData))); err != nil {
			return err
		}
		_ = fileutil.ChmodWithUmask(nfoPath, false)
	}

	// 2. Download Episode Still (Thumbnail)
	if matchedEpisode.StillPath != "" {
		stillURL := tmdb.GetImageURL(matchedEpisode.StillPath, "w300")
		stillDst := filepath.Join(dir, baseName+"-thumb.jpg")
		if overwrite || !fileExists(stillDst) {
			if err := tmdb.DownloadImage(stillURL, stillDst); err == nil {
				_ = fileutil.ChmodWithUmask(stillDst, false)
			}
		}
	}

	// 3. Save reference in DB (points to the main TV show ID)
	s.saveToDB(detail.ID, detail.Name, detail.FirstAirDate, "tv", path, detail.PosterPath, detail.BackdropPath)

	return nil
}

func (s *scrapeService) scrapeBluRayFolder(ctx context.Context, path string, overwrite bool) error {
	// Scrape root directory of Blu-ray as if it was a movie directory
	_, details, err := s.recognizeService.Recognize(ctx, path)
	if err != nil {
		return err
	}

	movieDetail := details.(*tmdb.MovieDetail)
	nfoPath := filepath.Join(path, "movie.nfo")

	if overwrite || !fileExists(nfoPath) {
		xmlData, err := nfo.GenerateMovieNFO(movieDetail)
		if err != nil {
			return err
		}
		if err := s.storage.Write(nfoPath, strings.NewReader(string(xmlData))); err != nil {
			return err
		}
		_ = fileutil.ChmodWithUmask(nfoPath, false)
	}

	// Images
	if movieDetail.PosterPath != "" {
		posterDst := filepath.Join(path, "poster.jpg")
		if overwrite || !fileExists(posterDst) {
			if err := tmdb.DownloadImage(tmdb.GetImageURL(movieDetail.PosterPath, "w500"), posterDst); err == nil {
				_ = fileutil.ChmodWithUmask(posterDst, false)
				s.createAliases(posterDst, "folder.jpg")
			}
		}
	}

	return nil
}

func (s *scrapeService) scrapeTVDirectory(ctx context.Context, path string, detail *tmdb.TVDetail, overwrite bool) error {
	nfoPath := filepath.Join(path, "tvshow.nfo")

	// 1. tvshow.nfo
	if overwrite || !fileExists(nfoPath) {
		xmlData, err := nfo.GenerateTVShowNFO(detail)
		if err != nil {
			return err
		}
		if err := s.storage.Write(nfoPath, strings.NewReader(string(xmlData))); err != nil {
			return err
		}
		_ = fileutil.ChmodWithUmask(nfoPath, false)
	}

	// 2. TV Root Images
	if detail.PosterPath != "" {
		posterDst := filepath.Join(path, "poster.jpg")
		if overwrite || !fileExists(posterDst) {
			if err := tmdb.DownloadImage(tmdb.GetImageURL(detail.PosterPath, "w500"), posterDst); err == nil {
				_ = fileutil.ChmodWithUmask(posterDst, false)
				s.createAliases(posterDst, "folder.jpg")
			}
		}
	}

	if detail.BackdropPath != "" {
		backdropDst := filepath.Join(path, "backdrop.jpg")
		if overwrite || !fileExists(backdropDst) {
			if err := tmdb.DownloadImage(tmdb.GetImageURL(detail.BackdropPath, "original"), backdropDst); err == nil {
				_ = fileutil.ChmodWithUmask(backdropDst, false)
				s.createAliases(backdropDst, "fanart.jpg")
			}
		}
	}

	return nil
}

func (s *scrapeService) scrapeSeasonDirectory(ctx context.Context, path string, tvDetail *tmdb.TVDetail, seasonNum int, overwrite bool) error {
	nfoPath := filepath.Join(path, "season.nfo")

	// 1. Generate season.nfo
	if overwrite || !fileExists(nfoPath) {
		xmlData, err := nfo.GenerateSeasonNFO(tvDetail, seasonNum)
		if err != nil {
			return err
		}
		if err := s.storage.Write(nfoPath, strings.NewReader(string(xmlData))); err != nil {
			return err
		}
		_ = fileutil.ChmodWithUmask(nfoPath, false)
	}

	// 2. Download Season Poster
	var seasonPosterPath string
	for _, s := range tvDetail.Seasons {
		if s.SeasonNumber == seasonNum {
			seasonPosterPath = s.PosterPath
			break
		}
	}

	if seasonPosterPath != "" {
		posterDst := filepath.Join(path, "poster.jpg")
		if overwrite || !fileExists(posterDst) {
			if err := tmdb.DownloadImage(tmdb.GetImageURL(seasonPosterPath, "w500"), posterDst); err == nil {
				_ = fileutil.ChmodWithUmask(posterDst, false)
				s.createAliases(posterDst, "folder.jpg")
			}
		}
	}

	return nil
}

func (s *scrapeService) initializeMovieDirectory(ctx context.Context, path string, detail *tmdb.MovieDetail, overwrite bool) error {
	nfoPath := filepath.Join(path, "movie.nfo")

	if overwrite || !fileExists(nfoPath) {
		xmlData, err := nfo.GenerateMovieNFO(detail)
		if err != nil {
			return err
		}
		if err := s.storage.Write(nfoPath, strings.NewReader(string(xmlData))); err != nil {
			return err
		}
		_ = fileutil.ChmodWithUmask(nfoPath, false)
	}

	if detail.PosterPath != "" {
		posterDst := filepath.Join(path, "poster.jpg")
		if overwrite || !fileExists(posterDst) {
			if err := tmdb.DownloadImage(tmdb.GetImageURL(detail.PosterPath, "w500"), posterDst); err == nil {
				_ = fileutil.ChmodWithUmask(posterDst, false)
				s.createAliases(posterDst, "folder.jpg")
			}
		}
	}

	if detail.BackdropPath != "" {
		backdropDst := filepath.Join(path, "backdrop.jpg")
		if overwrite || !fileExists(backdropDst) {
			if err := tmdb.DownloadImage(tmdb.GetImageURL(detail.BackdropPath, "original"), backdropDst); err == nil {
				_ = fileutil.ChmodWithUmask(backdropDst, false)
				s.createAliases(backdropDst, "fanart.jpg")
			}
		}
	}

	return nil
}

func (s *scrapeService) createAliases(srcPath string, aliasName string) {
	dir := filepath.Dir(srcPath)
	aliasPath := filepath.Join(dir, aliasName)
	if !fileExists(aliasPath) {
		_ = s.storage.Copy(srcPath, aliasPath)
		_ = fileutil.ChmodWithUmask(aliasPath, false)
	}
}

func (s *scrapeService) saveToDB(tmdbID int, title, date, mediaType, path, poster, backdrop string) {
	year := 0
	if len(date) >= 4 {
		fmt.Sscanf(date[:4], "%d", &year)
	}

	existing, err := s.mediaRepo.GetByPath(path)
	if err == nil && existing != nil {
		existing.TMDBID = tmdbID
		existing.Title = title
		existing.Year = year
		existing.Type = mediaType
		existing.PosterPath = poster
		existing.BackdropPath = backdrop
		existing.ScrapedAt = time.Now()
		_ = s.mediaRepo.Update(existing)
	} else {
		media := &entity.Media{
			TMDBID:       tmdbID,
			Title:        title,
			Year:         year,
			Type:         mediaType,
			Path:         path,
			PosterPath:   poster,
			BackdropPath: backdrop,
			ScrapedAt:    time.Now(),
		}
		_ = s.mediaRepo.Create(media)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
