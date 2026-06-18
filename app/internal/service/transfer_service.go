package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/internal/storage"
	"github.com/bujic-movie/bujic-movie/pkg/fileutil"
	"github.com/bujic-movie/bujic-movie/pkg/logger"
	"github.com/bujic-movie/bujic-movie/pkg/parser"
	"github.com/bujic-movie/bujic-movie/pkg/tmdb"
)

type TransferService interface {
	SubmitTask(ctx context.Context, srcPath string, opts TransferOptions) error
	GetQueue() []TransferTask
	GetHistory(offset, limit int) ([]entity.TransferHistory, error)
}

type TransferOptions struct {
	Mode          string // copy, move, link, softlink
	OverwriteMode string // always, never, size, latest
	AutoScrape    bool
	MediaType     string // "", "movie", "tv" - 手动指定媒体类型，空字符串表示自动识别
	CardID        uint
}

type TransferTask struct {
	ID        string    `json:"id"`
	SrcPath   string    `json:"src_path"`
	Status    string    `json:"status"` // "queued", "running", "success", "failed"
	Progress  float64   `json:"progress"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
	MediaType string    `json:"media_type"` // 手动指定的媒体类型
	CardID    uint      `json:"card_id"`
}

type transferService struct {
	historyRepo      repository.TransferHistoryRepository
	namingService    NamingService
	recognizeService RecognizeService
	scrapeService    ScrapeService
	tmdbClient       *tmdb.Client
	storage          storage.Storage
	config           *config.Config
	cardRepo         repository.MediaCardRepository

	// Queue and Worker Pool
	taskChan  chan *TransferTask
	queue     []*TransferTask
	queueMu   sync.RWMutex
	workerWg  sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc

	// Debouncing scrape timers
	debounceMu     sync.Mutex
	debounceTimers map[string]*time.Timer
}

var ExtraFolderNames = []string{
	"featurettes",
	"behind the scenes",
	"deleted scenes",
	"interviews",
	"trailers",
	"shorts",
	"scenes",
	"specials",
}

func isExtraFile(path string) (bool, string) {
	cleanPath := filepath.Clean(path)
	dir := filepath.Dir(cleanPath)
	for {
		base := filepath.Base(dir)
		if base == "." || base == "/" || base == "" || base == dir {
			break
		}
		lowerBase := strings.ToLower(base)
		for _, extraName := range ExtraFolderNames {
			if lowerBase == extraName {
				return true, base
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return false, ""
}

func NewTransferService(
	historyRepo repository.TransferHistoryRepository,
	namingService NamingService,
	recognizeService RecognizeService,
	scrapeService ScrapeService,
	tmdbClient *tmdb.Client,
	stg storage.Storage,
	cfg *config.Config,
	cardRepo repository.MediaCardRepository,
) TransferService {
	ctx, cancel := context.WithCancel(context.Background())
	s := &transferService{
		historyRepo:      historyRepo,
		namingService:    namingService,
		recognizeService: recognizeService,
		scrapeService:    scrapeService,
		tmdbClient:       tmdbClient,
		storage:          stg,
		config:           cfg,
		cardRepo:         cardRepo,
		taskChan:         make(chan *TransferTask, 100),
		debounceTimers:   make(map[string]*time.Timer),
		ctx:              ctx,
		cancel:           cancel,
	}

	// Start 3 worker goroutines
	for i := 0; i < 3; i++ {
		s.workerWg.Add(1)
		go s.worker()
	}

	return s
}

func (s *transferService) SubmitTask(ctx context.Context, srcPath string, opts TransferOptions) error {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()

	// Fill defaults if empty
	if opts.Mode == "" {
		opts.Mode = s.config.Transfer.Mode
	}
	if opts.OverwriteMode == "" {
		opts.OverwriteMode = s.config.Transfer.OverwriteMode
	}

	if opts.CardID == 0 && s.cardRepo != nil {
		defaultCard, err := s.cardRepo.GetDefault()
		if err == nil && defaultCard != nil {
			opts.CardID = defaultCard.ID
		}
	}

	task := &TransferTask{
		ID:        fmt.Sprintf("t-%d", time.Now().UnixNano()),
		SrcPath:   srcPath,
		Status:    "queued",
		CreatedAt: time.Now(),
		MediaType: opts.MediaType,
		CardID:    opts.CardID,
	}

	s.queue = append(s.queue, task)

	// Send to channel in a non-blocking way
	select {
	case s.taskChan <- task:
		return nil
	default:
		task.Status = "failed"
		task.Message = "task queue is full"
		return errors.New("task queue is full")
	}
}

func (s *transferService) GetQueue() []TransferTask {
	s.queueMu.RLock()
	defer s.queueMu.RUnlock()

	var list []TransferTask
	for _, t := range s.queue {
		if t.Status == "queued" || t.Status == "running" {
			list = append(list, *t)
		}
	}
	return list
}

func (s *transferService) GetHistory(offset, limit int) ([]entity.TransferHistory, error) {
	return s.historyRepo.List(offset, limit)
}

func (s *transferService) worker() {
	defer s.workerWg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		case task := <-s.taskChan:
			logger.Info("[整理] 开始任务 %s, 源路径: %s", task.ID, task.SrcPath)
			s.updateTaskStatus(task.ID, "running", 0, "")
			err := s.executeTransfer(task)
			if err != nil {
				logger.Error("[整理] 任务 %s 失败: %v", task.ID, err)
				s.updateTaskStatus(task.ID, "failed", 0, err.Error())
				s.recordHistory(task.SrcPath, "", "failed", 0, err.Error())
			} else {
				logger.Info("[整理] 任务 %s 完成", task.ID)
				s.updateTaskStatus(task.ID, "success", 100, "completed")
			}
		}
	}
}

func (s *transferService) updateTaskStatus(id string, status string, progress float64, message string) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()

	for i, t := range s.queue {
		if t.ID == id {
			t.Status = status
			t.Progress = progress
			t.Message = message
			
			// Remove from active queue slice if finished
			if status == "success" || status == "failed" {
				s.queue = append(s.queue[:i], s.queue[i+1:]...)
			}
			break
		}
	}
}

func (s *transferService) executeTransfer(task *TransferTask) error {
	srcPath := task.SrcPath
	isDir := s.storage.IsDir(srcPath)

	logger.Info("[整理] 源路径: %s, 类型: %s", srcPath, map[bool]string{true: "目录", false: "单个文件"}[isDir])

	// Fetch configurations
	ignoreExts := make(map[string]bool)
	for _, ext := range s.config.Transfer.IgnoreExtensions {
		ignoreExts[strings.ToLower(ext)] = true
	}
	minSize := s.config.Transfer.MinFileSizeMB * 1024 * 1024

	// Recognize metadata
	logger.Info("[整理] 正在识别媒体信息...")
	meta, details, err := s.recognizeService.RecognizeWithType(context.Background(), srcPath, task.MediaType)
	if err != nil {
		logger.Warn("[整理] 识别失败: %v", err)
		return err
	}
	mediaType := map[bool]string{true: "电影", false: "电视剧"}[meta.IsMovie]
	logger.Info("[整理] 识别成功: %s (%s)", meta.Title, mediaType)

	if isDir {
		// Verify if it is Blu-ray folder
		if s.storage.IsBluray(srcPath) {
			logger.Info("[整理] 检测到蓝光原盘目录")
			return s.transferBluRay(srcPath, meta, details, task.MediaType, task.CardID)
		}

		// Recurse directory and transfer files
		videoFiles, err := fileutil.FindFiles(srcPath, fileutil.IsVideo)
		if err != nil {
			return err
		}
		logger.Info("[整理] 发现 %d 个视频文件", len(videoFiles))

		var lastDestDir string
		var totalSize int64

		totalFiles := len(videoFiles)
		for i, vf := range videoFiles {
			// Check ignore list and size limit
			info, err := os.Stat(vf)
			if err != nil {
				logger.Warn("[整理] 无法获取文件信息 %s: %v", vf, err)
				continue
			}
			if info.Size() < minSize {
				logger.Info("[整理] 跳过小文件: %s (%.2f MB)", filepath.Base(vf), float64(info.Size())/1024/1024)
				continue // Skip samples/short video clips
			}

			logger.Info("[整理] [%d/%d] 正在整理: %s", i+1, totalFiles, filepath.Base(vf))

			// Calculate progress percentage and update status
			progress := float64(i) / float64(totalFiles) * 100.0
			s.updateTaskStatus(task.ID, "running", progress, fmt.Sprintf("正在整理: %s (%d/%d)", filepath.Base(vf), i+1, totalFiles))

			vfMeta := meta
			if !meta.IsMovie {
				// For TV shows, we must parse the individual file's metadata to get the correct episode number
				vfMeta = parser.ParseFilename(vf)
				// If the file metadata season is 0, fallback to the directory's season
				if vfMeta.Season == 0 && meta.Season > 0 {
					vfMeta.Season = meta.Season
				}
				// Also fallback year if needed
				if vfMeta.Year == 0 && meta.Year > 0 {
					vfMeta.Year = meta.Year
				}
			}

			destPath, err := s.transferSingleVideoFile(vf, vfMeta, details, task.CardID)
			if err != nil {
				logger.Error("[整理] 整理失败 %s: %v", vf, err)
				return err
			}
			logger.Info("[整理] [%d/%d] 整理完成: %s -> %s", i+1, len(videoFiles), filepath.Base(vf), destPath)
			lastDestDir = filepath.Dir(destPath)
			totalSize += info.Size()
		}

		// Transfer subtitles accompanying
		logger.Info("[整理] 正在处理字幕文件...")
		_ = s.transferSubtitlesForDir(srcPath, lastDestDir, meta)

		// Move mode: clean up source directory
		if s.config.Transfer.Mode == "move" {
			logger.Info("[整理] Move 模式: 清理源目录")
			_ = s.storage.Delete(srcPath)
		}

		// Trigger batch scrape debounce
		if lastDestDir != "" && s.config.Transfer.AutoScrape {
			logger.Info("[整理] 触发自动刮削 (防抖): %s", lastDestDir)
			s.debounceScrape(lastDestDir, task.MediaType)
		}

		s.recordHistory(srcPath, lastDestDir, "success", totalSize, "directory transfer complete")
		logger.Info("[整理] 目录整理完成, 总大小: %.2f MB", float64(totalSize)/1024/1024)
		return nil
	} else {
		// Single File Transfer
		info, err := os.Stat(srcPath)
		if err != nil {
			return err
		}

		logger.Info("[整理] 正在整理单个文件: %s", filepath.Base(srcPath))
		destPath, err := s.transferSingleVideoFile(srcPath, meta, details, task.CardID)
		if err != nil {
			logger.Error("[整理] 整理失败: %v", err)
			return err
		}
		logger.Info("[整理] 整理完成: %s -> %s", filepath.Base(srcPath), destPath)

		// Subtitles随行 for single file
		logger.Info("[整理] 正在处理字幕文件...")
		_ = s.transferAccompanyingSubtitles(srcPath, destPath, meta)

		// Move mode: delete source file
		if s.config.Transfer.Mode == "move" {
			logger.Info("[整理] Move 模式: 删除源文件")
			_ = s.storage.Delete(srcPath)
		}

		// Trigger batch scrape debounce
		if s.config.Transfer.AutoScrape {
			logger.Info("[整理] 触发自动刮削 (防抖): %s", filepath.Dir(destPath))
			s.debounceScrape(filepath.Dir(destPath), task.MediaType)
		}

		s.recordHistory(srcPath, destPath, "success", info.Size(), "file transfer complete")
		logger.Info("[整理] 单个文件整理完成, 大小: %.2f MB", float64(info.Size())/1024/1024)
		return nil
	}
}

func (s *transferService) getBaseArchivePath(cardID uint) (string, error) {
	if s.cardRepo == nil {
		return "", errors.New("卡片仓储库未注入，无法获取归档路径")
	}
	card, err := s.cardRepo.GetByID(cardID)
	if err != nil {
		return "", fmt.Errorf("未找到指定的媒体卡片 (ID: %d): %w", cardID, err)
	}
	if card.ArchivePath == "" {
		return "", fmt.Errorf("指定的媒体卡片 (ID: %d) 未配置归档路径", cardID)
	}
	return card.ArchivePath, nil
}

func (s *transferService) transferSingleVideoFile(srcPath string, meta *parser.Metadata, details interface{}, cardID uint) (string, error) {
	isExtra, extraFolder := isExtraFile(srcPath)
	if !isExtra && !meta.IsMovie && len(meta.Episodes) == 0 {
		return "", fmt.Errorf("无法解析剧集的剧集编号: %s", srcPath)
	}

	ext := filepath.Ext(srcPath)
	var destSubdir, destFilename string

	basePath, err := s.getBaseArchivePath(cardID)
	if err != nil {
		return "", err
	}

	if isExtra {
		destFilename = filepath.Base(srcPath)
		if meta.IsMovie {
			movieDetail := details.(*tmdb.MovieDetail)
			var movieDir string
			if meta.Year > 0 {
				movieDir = fmt.Sprintf("%s (%d)", movieDetail.Title, meta.Year)
			} else {
				movieDir = movieDetail.Title
			}
			destSubdir = filepath.Join(basePath, movieDir, extraFolder)
		} else {
			tvDetail := details.(*tmdb.TVDetail)
			var tvDir string
			if meta.Year > 0 {
				tvDir = fmt.Sprintf("%s (%d)", tvDetail.Name, meta.Year)
			} else {
				tvDir = tvDetail.Name
			}
			if meta.Season > 0 {
				seasonDir := fmt.Sprintf("Season %02d", meta.Season)
				destSubdir = filepath.Join(basePath, tvDir, seasonDir, extraFolder)
			} else {
				destSubdir = filepath.Join(basePath, tvDir, extraFolder)
			}
		}
		logger.Info("[整理] 花絮/额外内容目标路径: %s", filepath.Join(destSubdir, destFilename))
	} else if meta.IsMovie {
		movieDetail := details.(*tmdb.MovieDetail)
		destSubdir, destFilename = s.namingService.GetMoviePath(movieDetail.Title, meta.Year, meta.Resolution, ext)
		destSubdir = filepath.Join(basePath, destSubdir)
		logger.Info("[整理] 电影目标路径: %s", filepath.Join(destSubdir, destFilename))
	} else {
		tvDetail := details.(*tmdb.TVDetail)
		// Find episode title from TMDB
		var epTitle string
		seasonDetail, err := s.tmdbClient.GetTVSeasonDetail(context.Background(), tvDetail.ID, meta.Season)
		if err == nil && len(meta.Episodes) > 0 {
			targetEpNum := meta.Episodes[0]
			for _, ep := range seasonDetail.Episodes {
				if ep.EpisodeNumber == targetEpNum {
					epTitle = ep.Name
					break
				}
			}
		}

		destSubdir, destFilename = s.namingService.GetTVPath(tvDetail.Name, meta.Year, meta.Season, meta.Episodes[0], epTitle, ext)
		destSubdir = filepath.Join(basePath, destSubdir)
		logger.Info("[整理] 电视剧目标路径: %s", filepath.Join(destSubdir, destFilename))
	}

	destPath := filepath.Join(destSubdir, destFilename)

	// Check for overwrite conflicts
	if err := s.handleOverwrite(srcPath, destPath); err != nil {
		logger.Warn("[整理] 覆盖检查失败: %v", err)
		return "", err
	}

	// Create directories and perform transfer
	logger.Info("[整理] 正在创建目标目录: %s", destSubdir)
	if err := s.storage.Mkdir(destSubdir); err != nil {
		return "", err
	}

	mode := s.config.Transfer.Mode
	logger.Info("[整理] 执行 %s 操作: %s -> %s", mode, filepath.Base(srcPath), destPath)
	switch mode {
	case "copy":
		err = s.storage.Copy(srcPath, destPath)
	case "move":
		err = s.storage.Move(srcPath, destPath)
	case "link":
		err = s.storage.Link(srcPath, destPath)
	case "softlink":
		err = s.storage.Symlink(srcPath, destPath)
	default:
		err = s.storage.Link(srcPath, destPath)
	}

	if err != nil {
		logger.Error("[整理] %s 操作失败: %v", mode, err)
		return "", err
	}

	_ = fileutil.ChmodWithUmask(destPath, false)
	return destPath, nil
}

func (s *transferService) transferBluRay(srcPath string, meta *parser.Metadata, details interface{}, mediaType string, cardID uint) error {
	basePath, err := s.getBaseArchivePath(cardID)
	if err != nil {
		return err
	}

	var destDir string
	if meta.IsMovie {
		movieDetail := details.(*tmdb.MovieDetail)
		destDir, _ = s.namingService.GetMoviePath(movieDetail.Title, meta.Year, meta.Resolution, "")
		destDir = filepath.Join(basePath, destDir)
	} else {
		tvDetail := details.(*tmdb.TVDetail)
		srcDirName := filepath.Base(srcPath)
		destDir = s.namingService.GetTVBlurayPath(tvDetail.Name, meta.Year, meta.Season, srcDirName)
		destDir = filepath.Join(basePath, destDir)
	}
	logger.Info("[整理] 蓝光原盘目标路径: %s", destDir)

	if err := s.storage.Mkdir(destDir); err != nil {
		return err
	}

	// Blu-ray folder structure must be copied completely (can't link subparts or it will break Blu-ray menu)
	// We run copy or move depending on configuration
	mode := s.config.Transfer.Mode
	logger.Info("[整理] 执行蓝光原盘 %s 操作: %s -> %s", mode, srcPath, destDir)
	if mode == "move" {
		err = s.storage.Move(srcPath, destDir)
	} else {
		err = s.storage.Copy(srcPath, destDir)
	}

	if err != nil {
		logger.Error("[整理] 蓝光原盘 %s 操作失败: %v", mode, err)
		return err
	}

	logger.Info("[整理] 蓝光原盘整理完成: %s", destDir)
	if s.config.Transfer.AutoScrape {
		logger.Info("[整理] 触发自动刮削 (防抖): %s", destDir)
		s.debounceScrape(destDir, mediaType)
	}

	s.recordHistory(srcPath, destDir, "success", 0, "Blu-ray folder transfer complete")
	return nil
}

func (s *transferService) handleOverwrite(srcPath, destPath string) error {
	destInfo, err := os.Stat(destPath)
	if os.IsNotExist(err) {
		return nil // Destination does not exist, safe to write
	}

	overwriteMode := s.config.Transfer.OverwriteMode
	logger.Info("[整理] 目标文件已存在, 覆盖模式: %s", overwriteMode)
	switch overwriteMode {
	case "never":
		return fmt.Errorf("file already exists and overwrite mode is 'never': %s", destPath)
	case "always":
		logger.Info("[整理] 覆盖模式 always: 删除旧文件")
		return s.storage.Delete(destPath)
	case "size":
		srcInfo, err := os.Stat(srcPath)
		if err != nil {
			return err
		}
		if srcInfo.Size() > destInfo.Size() {
			logger.Info("[整理] 覆盖模式 size: 源文件更大 (%.2f MB > %.2f MB), 执行覆盖", float64(srcInfo.Size())/1024/1024, float64(destInfo.Size())/1024/1024)
			return s.storage.Delete(destPath)
		}
		logger.Info("[整理] 覆盖模式 size: 源文件不大于目标文件, 跳过")
		return fmt.Errorf("skipped overwrite: source file size is not larger than existing destination")
	case "latest":
		// Only delete existing files matching video formats, leaving subtitles and other configs intact
		if fileutil.IsVideo(destPath) {
			logger.Info("[整理] 覆盖模式 latest: 删除旧视频文件")
			return s.storage.Delete(destPath)
		}
		logger.Info("[整理] 覆盖模式 latest: 非视频文件, 跳过")
		return nil
	default:
		return fmt.Errorf("unknown overwrite mode: %s", overwriteMode)
	}
}

func (s *transferService) transferAccompanyingSubtitles(srcVideoPath, destVideoPath string, meta *parser.Metadata) error {
	srcDir := filepath.Dir(srcVideoPath)
	destDir := filepath.Dir(destVideoPath)
	srcBase := strings.TrimSuffix(filepath.Base(srcVideoPath), filepath.Ext(srcVideoPath))
	destBase := strings.TrimSuffix(filepath.Base(destVideoPath), filepath.Ext(destVideoPath))

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, srcBase) && fileutil.IsSubtitle(name) {
			srcSubPath := filepath.Join(srcDir, name)
			subInfo := parser.ParseSubtitle(srcSubPath)
			
			// Format: destBase.language.format
			var destSubName string
			if subInfo.Language != "unknown" {
				destSubName = fmt.Sprintf("%s.%s.%s", destBase, subInfo.Language, subInfo.Format)
			} else {
				destSubName = fmt.Sprintf("%s.%s", destBase, subInfo.Format)
			}
			destSubPath := filepath.Join(destDir, destSubName)

			_ = s.storage.Copy(srcSubPath, destSubPath)
			_ = fileutil.ChmodWithUmask(destSubPath, false)
		}
	}
	return nil
}

func (s *transferService) transferSubtitlesForDir(srcDir, destDir string, meta *parser.Metadata) error {
	if destDir == "" {
		return nil
	}

	subFiles, err := fileutil.FindFiles(srcDir, fileutil.IsSubtitle)
	if err != nil {
		return err
	}

	for _, sf := range subFiles {
		sfName := filepath.Base(sf)
		
		// Find matching target structure. In multi-file, we map them based on best match name.
		destSubPath := filepath.Join(destDir, sfName)
		_ = s.storage.Copy(sf, destSubPath)
		_ = fileutil.ChmodWithUmask(destSubPath, false)
	}
	return nil
}

func (s *transferService) debounceScrape(dirPath string, mediaType string) {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()

	// If timer already exists, cancel it to reset
	if t, ok := s.debounceTimers[dirPath]; ok {
		t.Stop()
	}

	// Defer scraping by 5 seconds to buffer subsequent transfers in the same folder
	s.debounceTimers[dirPath] = time.AfterFunc(5*time.Second, func() {
		s.debounceMu.Lock()
		delete(s.debounceTimers, dirPath)
		s.debounceMu.Unlock()

		logger.Info("[整理] 触发自动刮削 (防抖): %s", dirPath)
		ctx := context.Background()
		if err := s.scrapeService.ScrapePathWithType(ctx, dirPath, false, mediaType); err != nil {
			logger.Error("[整理] 自动刮削失败 %s: %v", dirPath, err)
		} else {
			logger.Info("[整理] 自动刮削完成: %s", dirPath)
		}
	})
}

func (s *transferService) recordHistory(src, dest, status string, size int64, msg string) {
	history := &entity.TransferHistory{
		SrcPath:       src,
		DestPath:      dest,
		Status:        status,
		Size:          size,
		Mode:          s.config.Transfer.Mode,
		Message:       msg,
		TransferredAt: time.Now(),
	}
	_ = s.historyRepo.Create(history)
}
