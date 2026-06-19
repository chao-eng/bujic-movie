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

	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/internal/storage"
	"github.com/bujic-movie/bujic-movie/pkg/fileutil"
	"github.com/bujic-movie/bujic-movie/pkg/logger"
	"github.com/bujic-movie/bujic-movie/pkg/mediainfo"
	"github.com/bujic-movie/bujic-movie/pkg/nfo"
	"github.com/bujic-movie/bujic-movie/pkg/parser"
	"github.com/bujic-movie/bujic-movie/pkg/tmdb"
)

type ScrapeService interface {
	ScrapePath(ctx context.Context, path string, overwrite bool) error
	ScrapePathWithType(ctx context.Context, path string, overwrite bool, mediaType string) error
}

type scrapeService struct {
	mediaRepo        repository.MediaRepository
	recognizeService RecognizeService
	tmdbClient       *tmdb.Client
	storage          storage.Storage
	msgNotifier      MessageNotifyService
}

func NewScrapeService(
	mediaRepo repository.MediaRepository,
	recognizeService RecognizeService,
	tmdbClient *tmdb.Client,
	stg storage.Storage,
	msgNotifier MessageNotifyService,
) ScrapeService {
	return &scrapeService{
		mediaRepo:        mediaRepo,
		recognizeService: recognizeService,
		tmdbClient:       tmdbClient,
		storage:          stg,
		msgNotifier:      msgNotifier,
	}
}

// fetchCast returns the TMDB cast (演职人员) for the given media when the
// "刮削演职人员" setting is enabled. It is best-effort: a disabled toggle, a nil
// config, or a failed request all yield nil so NFO generation simply omits actors
// rather than failing the scrape.
func (s *scrapeService) fetchCast(ctx context.Context, mediaType string, tmdbID int) []tmdb.Cast {
	if config.GlobalConfig == nil || !config.GlobalConfig.Transfer.ScrapePerson {
		return nil
	}
	var (
		credits *tmdb.CreditsResponse
		err     error
	)
	if mediaType == "movie" {
		credits, err = s.tmdbClient.GetMovieCredits(ctx, tmdbID)
	} else {
		credits, err = s.tmdbClient.GetTVCredits(ctx, tmdbID)
	}
	if err != nil {
		logger.Warn("[刮削] 获取演职人员失败 (tmdbid=%d): %v", tmdbID, err)
		return nil
	}
	return credits.Cast
}

// ScrapePath handles scraping metadata for a given path (file or directory)
// Flow:
//   A[外部手动刮削事件 / 整理完毕自动触发] --> B{判断输入类型}
//   B -- 单个文件 --> C[1. 识别媒体信息]
//   B -- 目录 --> D[1. 收集该目录下所有子目录并按深度排序]
//   D --> E[2. 由浅入深先初始化各个子目录的元数据和海报]
//   E --> F[3. 遍历并刮削目录下的具体视频文件]
func (s *scrapeService) ScrapePath(ctx context.Context, path string, overwrite bool) error {
	return s.ScrapePathWithType(ctx, path, overwrite, "")
}

func (s *scrapeService) ScrapePathWithType(ctx context.Context, path string, overwrite bool, mediaType string) error {
	logger.Info("[刮削] 开始刮削: %s", path)
	if mediaType != "" {
		logger.Info("[刮削] 手动指定媒体类型: %s", mediaType)
	}

	// Redirect Season folder to the TV show root directory
	if s.storage.IsDir(path) {
		dirName := filepath.Base(path)
		seasonDirReg := regexp.MustCompile(`(?i)\b(season|specials?)\s*\d*\b`)
		if seasonDirReg.MatchString(dirName) {
			path = filepath.Dir(path)
			logger.Info("[刮削] Season 目录重定向到根目录: %s", path)
		}
	}

	isDir := s.storage.IsDir(path)

	if !isDir {
		// Single file: recognize and scrape
		logger.Info("[刮削] 单个文件模式: %s", path)
		return s.scrapeSingleFileWithType(ctx, path, overwrite, mediaType)
	}

	// Directory scraping
	logger.Info("[刮削] 目录模式: %s", path)

	// 2. Check for Blu-ray folder structure
	if s.storage.IsBluray(path) {
		logger.Info("[刮削] 检测到蓝光原盘目录，仅刮削根目录: %s", path)
		// Scrape ONLY the root of Blu-ray directory and do NOT recurse
		return s.scrapeBluRayFolderWithType(ctx, path, overwrite, mediaType)
	}

	// 3. Recursive directory scraping:
	// A. Collect all directories and sort by depth ascending (shallowest first)
	subDirs, err := fileutil.GetDirsSortedByDepth(path)
	if err != nil {
		return err
	}
	logger.Info("[刮削] 发现 %d 个子目录", len(subDirs))

	// Perform initial match on the root path to determine media type
	logger.Info("[刮削] 正在识别媒体信息...")
	meta, details, err := s.recognizeService.RecognizeWithType(ctx, path, mediaType)
	if err != nil {
		logger.Warn("[刮削] 识别失败: %s, 错误: %v", path, err)
		return nil
	}
	logger.Info("[刮削] 识别成功: %s", meta.Title)

	// B. Initialize directories metadata (shallow to deep)
	if !meta.IsMovie {
		// TV branch: K[电视剧刮削分支]
		logger.Info("[刮削] 媒体类型: 电视剧")
		tvDetail, ok := details.(*tmdb.TVDetail)
		if !ok {
			return fmt.Errorf("元数据详情类型不匹配: 期望 *tmdb.TVDetail, 实际 %T", details)
		}
		if err := s.handleTVScraping(ctx, path, subDirs, tvDetail, overwrite); err != nil {
			return err
		}
	} else {
		// Movie branch: J[电影刮削分支]
		logger.Info("[刮削] 媒体类型: 电影")
		movieDetail, ok := details.(*tmdb.MovieDetail)
		if !ok {
			return fmt.Errorf("元数据详情类型不匹配: 期望 *tmdb.MovieDetail, 实际 %T", details)
		}
		if err := s.handleMovieScraping(ctx, path, subDirs, movieDetail, overwrite); err != nil {
			return err
		}
	}

	// 刮削完成后异步推送入库通知（不阻塞、失败仅记日志）
	if s.msgNotifier != nil {
		nTitle, nYear, nPoster := extractNotifyInfo(details)
		mt := "tv"
		if meta.IsMovie {
			mt = "movie"
		}
		if nTitle != "" {
			s.msgNotifier.NotifyScrapeDone(nTitle, nYear, mt, nPoster)
		}
	}

	logger.Info("[刮削] 刮削完成: %s", path)
	return nil
}

// extractNotifyInfo pulls a display title, year and poster URL from TMDB detail.
func extractNotifyInfo(details interface{}) (title string, year int, posterURL string) {
	switch d := details.(type) {
	case *tmdb.MovieDetail:
		return d.Title, yearFromDate(d.ReleaseDate), posterURLOf(d.PosterPath)
	case *tmdb.TVDetail:
		return d.Name, yearFromDate(d.FirstAirDate), posterURLOf(d.PosterPath)
	}
	return "", 0, ""
}

func yearFromDate(date string) int {
	if len(date) >= 4 {
		y := 0
		fmt.Sscanf(date[:4], "%d", &y)
		return y
	}
	return 0
}

func posterURLOf(posterPath string) string {
	if posterPath == "" {
		return ""
	}
	return tmdb.GetImageURL(posterPath, "w500")
}

// handleMovieScraping implements the movie branch from the flow chart
// J --> J1{是文件还是目录?}
// J1 -- 目录 --> J3{是否为蓝光原盘?}
// J3 -- 否 --> J5[递归刮削子目录下所有视频文件 / 下载目录图片]
func (s *scrapeService) handleMovieScraping(ctx context.Context, path string, subDirs []string, movieDetail *tmdb.MovieDetail, overwrite bool) error {
	// Initialize root directory metadata and images
	logger.Info("[刮削] 正在初始化电影根目录元数据...")
	if err := s.initializeMovieDirectory(ctx, path, movieDetail, overwrite); err != nil {
		return err
	}

	// Recursively scrape all video files in the directory and sub-directories
	videoFiles, err := fileutil.FindFiles(path, fileutil.IsVideo)
	if err != nil {
		return err
	}
	logger.Info("[刮削] 发现 %d 个视频文件", len(videoFiles))

	for i, vf := range videoFiles {
		logger.Info("[刮削] [%d/%d] 正在刮削电影视频: %s", i+1, len(videoFiles), filepath.Base(vf))
		if err := s.scrapeMovieFile(ctx, vf, movieDetail, overwrite); err != nil {
			logger.Warn("[刮削] 刮削失败 %s: %v", vf, err)
		} else {
			logger.Info("[刮削] [%d/%d] 电影视频刮削完成: %s", i+1, len(videoFiles), filepath.Base(vf))
		}
	}

	return nil
}

// handleTVScraping implements the TV branch from the flow chart
// K --> K1{是文件还是目录?}
// K1 -- 目录 --> K3[递归刮削子目录/子文件]
// K3 --> K4[初始化剧集目录元数据]
// K4 --> K5{识别目录类型}
// K5 -- 季目录 Season --> K6[生成 season.nfo / 下载季 poster/banner]
// K5 -- 电视剧根目录 TV --> K7[生成 tvshow.nfo / 下载整剧 poster/backdrop/logo]
func (s *scrapeService) handleTVScraping(ctx context.Context, path string, subDirs []string, tvDetail *tmdb.TVDetail, overwrite bool) error {
	// Initialize TV root directory
	logger.Info("[刮削] 正在初始化电视剧根目录元数据...")
	if err := s.scrapeTVDirectory(ctx, path, tvDetail, overwrite); err != nil {
		return err
	}

	// Process sub-directories (look for Season folders) - shallow to deep
	seasonDirReg := regexp.MustCompile(`(?i)\bseason\s*(\d+)\b`)
	seasonCount := 0
	for _, dir := range subDirs {
		dirName := filepath.Base(dir)
		if match := seasonDirReg.FindStringSubmatch(dirName); match != nil {
			seasonNum := 1
			fmt.Sscanf(match[1], "%d", &seasonNum)
			logger.Info("[刮削] 正在刮削季目录: %s (Season %d)", dirName, seasonNum)
			if err := s.scrapeSeasonDirectory(ctx, dir, tvDetail, seasonNum, overwrite); err != nil {
				logger.Warn("[刮削] 季目录刮削失败 %s: %v", dir, err)
			} else {
				logger.Info("[刮削] 季目录刮削完成: %s", dirName)
				seasonCount++
			}
		}
	}
	logger.Info("[刮削] 共刮削 %d 个季目录", seasonCount)

	// Recursively scrape all video files (episodes)
	videoFiles, err := fileutil.FindFiles(path, fileutil.IsVideo)
	if err != nil {
		return err
	}
	logger.Info("[刮削] 发现 %d 个剧集文件", len(videoFiles))

	for i, vf := range videoFiles {
		logger.Info("[刮削] [%d/%d] 正在刮削剧集: %s", i+1, len(videoFiles), filepath.Base(vf))
		if err := s.scrapeTVEpisodeFile(ctx, vf, tvDetail, overwrite); err != nil {
			logger.Warn("[刮削] 剧集刮削失败 %s: %v", vf, err)
		} else {
			logger.Info("[刮削] [%d/%d] 剧集刮削完成: %s", i+1, len(videoFiles), filepath.Base(vf))
		}
	}

	return nil
}

func (s *scrapeService) scrapeSingleFile(ctx context.Context, path string, overwrite bool) error {
	return s.scrapeSingleFileWithType(ctx, path, overwrite, "")
}

func (s *scrapeService) scrapeSingleFileWithType(ctx context.Context, path string, overwrite bool, mediaType string) error {
	meta, details, err := s.recognizeService.RecognizeWithType(ctx, path, mediaType)
	if err != nil {
		logger.Warn("Failed to recognize file %s: %v", path, err)
		return nil
	}

	if meta.IsMovie {
		movieDetail, ok := details.(*tmdb.MovieDetail)
		if !ok {
			return fmt.Errorf("元数据详情类型不匹配: 期望 *tmdb.MovieDetail, 实际 %T", details)
		}
		return s.scrapeMovieFile(ctx, path, movieDetail, overwrite)
	} else {
		tvDetail, ok := details.(*tmdb.TVDetail)
		if !ok {
			return fmt.Errorf("元数据详情类型不匹配: 期望 *tmdb.TVDetail, 实际 %T", details)
		}
		return s.scrapeTVEpisodeFile(ctx, path, tvDetail, overwrite)
	}
}

func (s *scrapeService) scrapeMovieFile(ctx context.Context, path string, detail *tmdb.MovieDetail, overwrite bool) error {
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	baseName := strings.TrimSuffix(filepath.Base(path), ext)
	nfoPath := filepath.Join(dir, baseName+".nfo")

	// 1. Write NFO File if not exists or overwrite is true
	if overwrite || !fileExists(nfoPath) {
		var actors []tmdb.Cast
		var directors []string
		if credits, err := s.tmdbClient.GetMovieCredits(ctx, detail.ID); err == nil {
			if config.GlobalConfig != nil && config.GlobalConfig.Transfer.ScrapePerson {
				actors = credits.Cast
			}
			directors = nfo.ExtractDirectors(credits.Crew)
		} else {
			logger.Warn("[刮削] 获取电影演职人员失败 (tmdbid=%d): %v", detail.ID, err)
		}
		var dateAdded time.Time
		if info, err := os.Stat(path); err == nil {
			dateAdded = info.ModTime()
		} else {
			dateAdded = time.Now()
		}
		lockDataStr := "false"
		if config.GlobalConfig != nil && config.GlobalConfig.Media.LockNFO {
			lockDataStr = "true"
		}
		var streamDetails *mediainfo.StreamDetails
		if sd, err := mediainfo.Probe(ctx, path); err == nil {
			streamDetails = sd
		} else {
			logger.Warn("[刮削] 获取电影媒体信息失败 %s: %v", path, err)
		}
		opts := nfo.MovieOptions{
			LockData:  lockDataStr,
			DateAdded: dateAdded,
			Detail:    detail,
			Actors:    actors,
			Directors: directors,
			Stream:    streamDetails,
		}
		xmlData, err := nfo.GenerateMovieNFO(opts)
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
	s.saveToDB(detail.ID, detail.Title, detail.ReleaseDate, "movie", path, detail.PosterPath, detail.BackdropPath, 0)

	return nil
}

func (s *scrapeService) scrapeTVEpisodeFile(ctx context.Context, path string, detail *tmdb.TVDetail, overwrite bool) error {
	// K2[重新识别季集信息]: Re-parse episode-specific metadata from filename
	meta := parser.ParseFilename(path)
	if meta.Title == "" {
		return errors.New("failed to parse video title from filename")
	}
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
		var actors []tmdb.Cast
		var directors []string
		if credits, err := s.tmdbClient.GetTVEpisodeCredits(ctx, detail.ID, meta.Season, targetEpisodeNum); err == nil {
			if config.GlobalConfig != nil && config.GlobalConfig.Transfer.ScrapePerson {
				actors = credits.Cast
			}
			directors = nfo.ExtractDirectors(credits.Crew)
		} else {
			logger.Warn("[刮削] 获取剧集演职人员失败 (show_tmdbid=%d, s=%d, e=%d): %v", detail.ID, meta.Season, targetEpisodeNum, err)
		}
		var dateAdded time.Time
		if info, err := os.Stat(path); err == nil {
			dateAdded = info.ModTime()
		} else {
			dateAdded = time.Now()
		}
		lockDataStr := "false"
		if config.GlobalConfig != nil && config.GlobalConfig.Media.LockNFO {
			lockDataStr = "true"
		}
		var streamDetails *mediainfo.StreamDetails
		if sd, err := mediainfo.Probe(ctx, path); err == nil {
			streamDetails = sd
		} else {
			logger.Warn("[刮削] 获取剧集媒体信息失败 %s: %v", path, err)
		}
		episodePoster := strings.TrimSuffix(path, filepath.Ext(path)) + ".jpg"
		opts := nfo.EpisodeOptions{
			LockData:   lockDataStr,
			DateAdded:  dateAdded,
			Episode:    matchedEpisode,
			ShowTitle:  detail.Name,
			ShowTMDBID: detail.ID,
			ArtPoster:  episodePoster,
			Directors:  directors,
			Actors:     actors,
			Stream:     streamDetails,
		}
		xmlData, err := nfo.GenerateEpisodeNFO(opts)
		if err != nil {
			return err
		}
		if err := s.storage.Write(nfoPath, strings.NewReader(string(xmlData))); err != nil {
			return err
		}
		_ = fileutil.ChmodWithUmask(nfoPath, false)
	}

	// 2. Download Episode Still (Thumbnail)
	// L[图片别名复制 thumb -> landscape]
	if matchedEpisode.StillPath != "" {
		stillURL := tmdb.GetImageURL(matchedEpisode.StillPath, "w300")
		stillDst := filepath.Join(dir, baseName+"-thumb.jpg")
		if overwrite || !fileExists(stillDst) {
			if err := tmdb.DownloadImage(stillURL, stillDst); err == nil {
				_ = fileutil.ChmodWithUmask(stillDst, false)
				// Create alias: thumb -> landscape for compatibility
				s.createAliases(stillDst, baseName+"-landscape.jpg")
			}
		}
	}

	// 3. Save reference in DB (points to the main TV show ID)
	s.saveToDB(detail.ID, detail.Name, detail.FirstAirDate, "tv", path, detail.PosterPath, detail.BackdropPath, meta.Season)

	return nil
}

func (s *scrapeService) scrapeBluRayFolder(ctx context.Context, path string, overwrite bool) error {
	return s.scrapeBluRayFolderWithType(ctx, path, overwrite, "")
}

func (s *scrapeService) scrapeBluRayFolderWithType(ctx context.Context, path string, overwrite bool, mediaType string) error {
	// J4[只刮削根目录 NFO / 不递归子目录]
	// Scrape root directory of Blu-ray as if it was a movie directory
	_, details, err := s.recognizeService.RecognizeWithType(ctx, path, mediaType)
	if err != nil {
		logger.Warn("Failed to recognize Blu-ray folder %s: %v", path, err)
		return nil
	}

	movieDetail, ok := details.(*tmdb.MovieDetail)
	if !ok {
		return fmt.Errorf("元数据详情类型不匹配: 期望 *tmdb.MovieDetail, 实际 %T", details)
	}
	nfoPath := filepath.Join(path, "movie.nfo")

	if overwrite || !fileExists(nfoPath) {
		var actors []tmdb.Cast
		var directors []string
		if credits, err := s.tmdbClient.GetMovieCredits(ctx, movieDetail.ID); err == nil {
			if config.GlobalConfig != nil && config.GlobalConfig.Transfer.ScrapePerson {
				actors = credits.Cast
			}
			directors = nfo.ExtractDirectors(credits.Crew)
		} else {
			logger.Warn("[刮削] 获取电影演职人员失败 (tmdbid=%d): %v", movieDetail.ID, err)
		}
		var dateAdded time.Time
		if info, err := os.Stat(path); err == nil {
			dateAdded = info.ModTime()
		} else {
			dateAdded = time.Now()
		}
		lockDataStr := "false"
		if config.GlobalConfig != nil && config.GlobalConfig.Media.LockNFO {
			lockDataStr = "true"
		}
		opts := nfo.MovieOptions{
			LockData:  lockDataStr,
			DateAdded: dateAdded,
			Detail:    movieDetail,
			Actors:    actors,
			Directors: directors,
		}
		xmlData, err := nfo.GenerateMovieNFO(opts)
		if err != nil {
			return err
		}
		if err := s.storage.Write(nfoPath, strings.NewReader(string(xmlData))); err != nil {
			return err
		}
		_ = fileutil.ChmodWithUmask(nfoPath, false)
	}

	// Images: poster + backdrop
	if movieDetail.PosterPath != "" {
		posterDst := filepath.Join(path, "poster.jpg")
		if overwrite || !fileExists(posterDst) {
			if err := tmdb.DownloadImage(tmdb.GetImageURL(movieDetail.PosterPath, "w500"), posterDst); err == nil {
				_ = fileutil.ChmodWithUmask(posterDst, false)
				s.createAliases(posterDst, "folder.jpg")
			}
		}
	}

	if movieDetail.BackdropPath != "" {
		backdropDst := filepath.Join(path, "backdrop.jpg")
		if overwrite || !fileExists(backdropDst) {
			if err := tmdb.DownloadImage(tmdb.GetImageURL(movieDetail.BackdropPath, "original"), backdropDst); err == nil {
				_ = fileutil.ChmodWithUmask(backdropDst, false)
				s.createAliases(backdropDst, "fanart.jpg")
			}
		}
	}

	return nil
}

func (s *scrapeService) scrapeTVDirectory(ctx context.Context, path string, detail *tmdb.TVDetail, overwrite bool) error {
	nfoPath := filepath.Join(path, "tvshow.nfo")

	// 1. tvshow.nfo
	if overwrite || !fileExists(nfoPath) {
		var dateAdded time.Time
		if info, err := os.Stat(path); err == nil {
			dateAdded = info.ModTime()
		} else {
			dateAdded = time.Now()
		}
		lockDataStr := "false"
		if config.GlobalConfig != nil && config.GlobalConfig.Media.LockNFO {
			lockDataStr = "true"
		}
		var actors []tmdb.Cast
		if config.GlobalConfig != nil && config.GlobalConfig.Transfer.ScrapePerson {
			actors = s.fetchCast(ctx, "tv", detail.ID)
		}
		opts := nfo.TVShowOptions{
			LockData:  lockDataStr,
			DateAdded: dateAdded,
			Detail:    detail,
			Actors:    actors,
		}
		xmlData, err := nfo.GenerateTVShowNFO(opts)
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
		var dateAdded time.Time
		if info, err := os.Stat(path); err == nil {
			dateAdded = info.ModTime()
		} else {
			dateAdded = time.Now()
		}
		lockDataStr := "false"
		if config.GlobalConfig != nil && config.GlobalConfig.Media.LockNFO {
			lockDataStr = "true"
		}
		opts := nfo.SeasonOptions{
			LockData:  lockDataStr,
			DateAdded: dateAdded,
			Detail:    tvDetail,
			Season:    seasonNum,
		}
		xmlData, err := nfo.GenerateSeasonNFO(opts)
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
		var actors []tmdb.Cast
		var directors []string
		if credits, err := s.tmdbClient.GetMovieCredits(ctx, detail.ID); err == nil {
			if config.GlobalConfig != nil && config.GlobalConfig.Transfer.ScrapePerson {
				actors = credits.Cast
			}
			directors = nfo.ExtractDirectors(credits.Crew)
		} else {
			logger.Warn("[刮削] 获取电影演职人员失败 (tmdbid=%d): %v", detail.ID, err)
		}
		var dateAdded time.Time
		if info, err := os.Stat(path); err == nil {
			dateAdded = info.ModTime()
		} else {
			dateAdded = time.Now()
		}
		lockDataStr := "false"
		if config.GlobalConfig != nil && config.GlobalConfig.Media.LockNFO {
			lockDataStr = "true"
		}
		opts := nfo.MovieOptions{
			LockData:  lockDataStr,
			DateAdded: dateAdded,
			Detail:    detail,
			Actors:    actors,
			Directors: directors,
		}
		xmlData, err := nfo.GenerateMovieNFO(opts)
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

func (s *scrapeService) saveToDB(tmdbID int, title, date, mediaType, path, poster, backdrop string, season int) {
	year := 0
	if len(date) >= 4 {
		fmt.Sscanf(date[:4], "%d", &year)
	}

	existing, err := s.mediaRepo.GetByPath(path)
	if err == nil && existing != nil {
		existing.TMDBID = tmdbID
		existing.Title = title
		existing.Year = year
		existing.Season = season
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
			Season:       season,
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
