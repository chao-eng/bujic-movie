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
}

type TransferTask struct {
	ID        string    `json:"id"`
	SrcPath   string    `json:"src_path"`
	Status    string    `json:"status"` // "queued", "running", "success", "failed"
	Progress  float64   `json:"progress"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type transferService struct {
	historyRepo      repository.TransferHistoryRepository
	namingService    NamingService
	recognizeService RecognizeService
	scrapeService    ScrapeService
	tmdbClient       *tmdb.Client
	storage          storage.Storage
	config           *config.Config

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

func NewTransferService(
	historyRepo repository.TransferHistoryRepository,
	namingService NamingService,
	recognizeService RecognizeService,
	scrapeService ScrapeService,
	tmdbClient *tmdb.Client,
	stg storage.Storage,
	cfg *config.Config,
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

	task := &TransferTask{
		ID:        fmt.Sprintf("t-%d", time.Now().UnixNano()),
		SrcPath:   srcPath,
		Status:    "queued",
		CreatedAt: time.Now(),
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
			s.updateTaskStatus(task.ID, "running", 0, "")
			err := s.executeTransfer(task)
			if err != nil {
				s.updateTaskStatus(task.ID, "failed", 0, err.Error())
			} else {
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

	// Fetch configurations
	ignoreExts := make(map[string]bool)
	for _, ext := range s.config.Transfer.IgnoreExtensions {
		ignoreExts[strings.ToLower(ext)] = true
	}
	minSize := s.config.Transfer.MinFileSizeMB * 1024 * 1024

	// Recognize metadata
	meta, details, err := s.recognizeService.Recognize(context.Background(), srcPath)
	if err != nil {
		s.recordHistory(srcPath, "", "failed", 0, err.Error())
		return err
	}

	if isDir {
		// Verify if it is Blu-ray folder
		if s.storage.IsBluray(srcPath) {
			return s.transferBluRay(srcPath, meta, details)
		}

		// Recurse directory and transfer files
		videoFiles, err := fileutil.FindFiles(srcPath, fileutil.IsVideo)
		if err != nil {
			return err
		}

		var lastDestDir string
		var totalSize int64

		for _, vf := range videoFiles {
			// Check ignore list and size limit
			info, err := os.Stat(vf)
			if err != nil {
				continue
			}
			if info.Size() < minSize {
				continue // Skip samples/short video clips
			}

			destPath, err := s.transferSingleVideoFile(vf, meta, details)
			if err != nil {
				return err
			}
			lastDestDir = filepath.Dir(destPath)
			totalSize += info.Size()
		}

		// Transfer subtitles accompanying
		_ = s.transferSubtitlesForDir(srcPath, lastDestDir, meta)

		// Move mode: clean up source directory
		if s.config.Transfer.Mode == "move" {
			_ = s.storage.Delete(srcPath)
		}

		// Trigger batch scrape debounce
		if lastDestDir != "" && s.config.Transfer.AutoScrape {
			s.debounceScrape(lastDestDir)
		}

		s.recordHistory(srcPath, lastDestDir, "success", totalSize, "directory transfer complete")
		return nil
	} else {
		// Single File Transfer
		info, err := os.Stat(srcPath)
		if err != nil {
			return err
		}

		destPath, err := s.transferSingleVideoFile(srcPath, meta, details)
		if err != nil {
			s.recordHistory(srcPath, "", "failed", info.Size(), err.Error())
			return err
		}

		// Subtitles随行 for single file
		_ = s.transferAccompanyingSubtitles(srcPath, destPath, meta)

		// Move mode: delete source file
		if s.config.Transfer.Mode == "move" {
			_ = s.storage.Delete(srcPath)
		}

		// Trigger batch scrape debounce
		if s.config.Transfer.AutoScrape {
			s.debounceScrape(filepath.Dir(destPath))
		}

		s.recordHistory(srcPath, destPath, "success", info.Size(), "file transfer complete")
		return nil
	}
}

func (s *transferService) transferSingleVideoFile(srcPath string, meta *parser.Metadata, details interface{}) (string, error) {
	ext := filepath.Ext(srcPath)
	var destSubdir, destFilename string

	if meta.IsMovie {
		movieDetail := details.(*tmdb.MovieDetail)
		destSubdir, destFilename = s.namingService.GetMoviePath(movieDetail.Title, meta.Year, meta.Resolution, ext)
		destSubdir = filepath.Join(s.config.Media.MoviePath, destSubdir)
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
		destSubdir = filepath.Join(s.config.Media.TVPath, destSubdir)
	}

	destPath := filepath.Join(destSubdir, destFilename)

	// Check for overwrite conflicts
	if err := s.handleOverwrite(srcPath, destPath); err != nil {
		return "", err
	}

	// Create directories and perform transfer
	if err := s.storage.Mkdir(destSubdir); err != nil {
		return "", err
	}

	mode := s.config.Transfer.Mode
	var err error
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
		return "", err
	}

	_ = fileutil.ChmodWithUmask(destPath, false)
	return destPath, nil
}

func (s *transferService) transferBluRay(srcPath string, meta *parser.Metadata, details interface{}) error {
	var destDir string
	if meta.IsMovie {
		movieDetail := details.(*tmdb.MovieDetail)
		destDir, _ = s.namingService.GetMoviePath(movieDetail.Title, meta.Year, meta.Resolution, "")
		destDir = filepath.Join(s.config.Media.MoviePath, destDir)
	} else {
		tvDetail := details.(*tmdb.TVDetail)
		srcDirName := filepath.Base(srcPath)
		destDir = s.namingService.GetTVBlurayPath(tvDetail.Name, meta.Year, meta.Season, srcDirName)
		destDir = filepath.Join(s.config.Media.TVPath, destDir)
	}

	if err := s.storage.Mkdir(destDir); err != nil {
		return err
	}

	// Blu-ray folder structure must be copied completely (can't link subparts or it will break Blu-ray menu)
	// We run copy or move depending on configuration
	mode := s.config.Transfer.Mode
	var err error
	if mode == "move" {
		err = s.storage.Move(srcPath, destDir)
	} else {
		err = s.storage.Copy(srcPath, destDir)
	}

	if err != nil {
		s.recordHistory(srcPath, destDir, "failed", 0, err.Error())
		return err
	}

	if s.config.Transfer.AutoScrape {
		s.debounceScrape(destDir)
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
	switch overwriteMode {
	case "never":
		return fmt.Errorf("file already exists and overwrite mode is 'never': %s", destPath)
	case "always":
		return s.storage.Delete(destPath)
	case "size":
		srcInfo, err := os.Stat(srcPath)
		if err != nil {
			return err
		}
		if srcInfo.Size() > destInfo.Size() {
			// Overwrite because new version is larger (higher bitrate)
			return s.storage.Delete(destPath)
		}
		return fmt.Errorf("skipped overwrite: source file size is not larger than existing destination")
	case "latest":
		// Only delete existing files matching video formats, leaving subtitles and other configs intact
		if fileutil.IsVideo(destPath) {
			return s.storage.Delete(destPath)
		}
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

func (s *transferService) debounceScrape(dirPath string) {
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

		fmt.Printf("Debounced scrape triggered for folder: %s\n", dirPath)
		ctx := context.Background()
		_ = s.scrapeService.ScrapePath(ctx, dirPath, false)
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
