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

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/pkg/fileutil"
	"github.com/bujic-movie/bujic-movie/pkg/logger"
	"github.com/fsnotify/fsnotify"
)

type WatcherService interface {
	Start() error
	Stop()
	WatchCard(card *entity.MediaCard) error
	UnwatchCard(cardID uint)
}

type watcherService struct {
	watcher         *fsnotify.Watcher
	transferService TransferService
	cardRepo        repository.MediaCardRepository

	watchedCards    map[uint]string
	watchedMu       sync.RWMutex

	debounces       map[string]*time.Timer
	debounceMu      sync.Mutex

	cancel          chan struct{}
	wg              sync.WaitGroup
}

func NewWatcherService(transferService TransferService, cardRepo repository.MediaCardRepository) WatcherService {
	return &watcherService{
		transferService: transferService,
		cardRepo:        cardRepo,
		watchedCards:    make(map[uint]string),
		debounces:       make(map[string]*time.Timer),
		cancel:          make(chan struct{}),
	}
}

func (s *watcherService) Start() error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("创建 fsnotify watcher 失败: %w", err)
	}
	s.watcher = w

	// Start event loop
	s.wg.Add(1)
	go s.eventLoop()

	// Load all cards from DB and start watching
	cards, err := s.cardRepo.List()
	if err != nil {
		logger.Warn("[监控] 加载卡片列表失败: %v", err)
		return nil
	}

	for i := range cards {
		card := &cards[i]
		if card.WatchDirectory && card.DownloadPath != "" {
			if err := s.WatchCard(card); err != nil {
				logger.Error("[监控] 启动卡片 [%s] 目录监控失败: %v", card.Name, err)
			}
		}
	}

	return nil
}

func (s *watcherService) Stop() {
	close(s.cancel)
	if s.watcher != nil {
		s.watcher.Close()
	}
	s.wg.Wait()

	s.debounceMu.Lock()
	for _, t := range s.debounces {
		t.Stop()
	}
	s.debounces = make(map[string]*time.Timer)
	s.debounceMu.Unlock()
}

func (s *watcherService) WatchCard(card *entity.MediaCard) error {
	if card.DownloadPath == "" {
		return nil
	}

	// Clean path and ensure it exists and is a directory
	path := filepath.Clean(card.DownloadPath)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Try to create it
			if err := os.MkdirAll(path, 0755); err != nil {
				return fmt.Errorf("无法创建下载源目录 %s: %w", path, err)
			}
		} else {
			return err
		}
	} else if !info.IsDir() {
		return fmt.Errorf("路径不是目录: %s", path)
	}

	s.watchedMu.Lock()
	s.watchedCards[card.ID] = path
	s.watchedMu.Unlock()

	// Add recursively
	return s.watchDirRecursive(path)
}

func (s *watcherService) UnwatchCard(cardID uint) {
	s.watchedMu.Lock()
	path, exists := s.watchedCards[cardID]
	if exists {
		delete(s.watchedCards, cardID)
	}
	s.watchedMu.Unlock()

	if exists && path != "" {
		// Remove recursively (we can walk and try to remove all subdirectories from fsnotify)
		_ = filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
			if err == nil && info != nil && info.IsDir() {
				_ = s.watcher.Remove(walkPath)
			}
			return nil
		})
		logger.Info("[监控] 已停止监视卡片 %d 对应的下载源目录: %s", cardID, path)
	}
}

func (s *watcherService) watchDirRecursive(path string) error {
	return filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			err = s.watcher.Add(walkPath)
			if err != nil {
				// Don't fail the whole process if one subdir fails
				logger.Warn("[监控] 无法添加目录监视 %s: %v", walkPath, err)
			} else {
				logger.Info("[监控] 正在监视目录: %s", walkPath)
			}
		}
		return nil
	})
}

func (s *watcherService) eventLoop() {
	defer s.wg.Done()

	for {
		select {
		case <-s.cancel:
			return
		case event, ok := <-s.watcher.Events:
			if !ok {
				return
			}
			s.handleEvent(event)
		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			logger.Error("[监控] 收到监视器错误: %v", err)
		}
	}
}

func (s *watcherService) handleEvent(event fsnotify.Event) {
	// We handle Create, Write, and Rename events
	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) || event.Has(fsnotify.Rename) {
		info, err := os.Stat(event.Name)
		if err != nil {
			// File/directory might have been deleted or renamed away
			return
		}

		if info.IsDir() {
			if event.Has(fsnotify.Create) {
				logger.Info("[监控] 检测到新建目录，添加监视: %s", event.Name)
				_ = s.watchDirRecursive(event.Name)
				// Walk the newly created directory to find any existing video files and trigger processing
				s.scanAndProcessExistingFiles(event.Name)
			}
			return
		}

		// It is a file. Check if it's a video file.
		if fileutil.IsVideo(event.Name) {
			s.handleWriteEvent(event.Name)
		}
	}
}

func (s *watcherService) scanAndProcessExistingFiles(dirPath string) {
	videoFiles, err := fileutil.FindFiles(dirPath, fileutil.IsVideo)
	if err != nil {
		logger.Error("[监控] 扫描新建目录 %s 失败: %v", dirPath, err)
		return
	}

	if len(videoFiles) > 0 {
		logger.Info("[监控] 新建目录中发现 %d 个视频文件，将触发整理", len(videoFiles))
		for _, vf := range videoFiles {
			s.handleWriteEvent(vf)
		}
	}
}

func (s *watcherService) handleWriteEvent(filePath string) {
	if !fileutil.IsVideo(filePath) {
		return
	}

	card, err := s.findCardForPath(filePath)
	if err != nil {
		// Not watched by any card
		return
	}

	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()

	// Reset timer if it exists
	if t, ok := s.debounces[filePath]; ok {
		t.Stop()
	}

	// Schedule task after 10 seconds of silence
	s.debounces[filePath] = time.AfterFunc(10*time.Second, func() {
		s.debounceMu.Lock()
		delete(s.debounces, filePath)
		s.debounceMu.Unlock()

		logger.Info("[监控] 文件写入完成防抖结束: %s", filePath)

		ctx := context.Background()
		err := s.transferService.SubmitTask(ctx, filePath, TransferOptions{
			MediaType: card.MediaType,
			CardID:    card.ID,
		})
		if err != nil {
			logger.Error("[监控] 自动触发文件整理失败 %s: %v", filePath, err)
		} else {
			logger.Info("[监控] 已自动提交整理任务: %s (卡片: %s)", filePath, card.Name)
		}
	})
}

func (s *watcherService) findCardForPath(path string) (*entity.MediaCard, error) {
	s.watchedMu.RLock()
	defer s.watchedMu.RUnlock()

	cleanPath := filepath.ToSlash(filepath.Clean(path))
	var bestMatchID uint
	var longestMatchLen int

	for cardID, downloadPath := range s.watchedCards {
		cleanDownload := filepath.ToSlash(filepath.Clean(downloadPath))
		if strings.HasPrefix(strings.ToLower(cleanPath), strings.ToLower(cleanDownload)) {
			matchLen := len(cleanDownload)
			if len(cleanPath) == matchLen || cleanPath[matchLen] == '/' {
				if matchLen > longestMatchLen {
					longestMatchLen = matchLen
					bestMatchID = cardID
				}
			}
		}
	}

	if bestMatchID == 0 {
		return nil, errors.New("no matching watched card found for path")
	}

	return s.cardRepo.GetByID(bestMatchID)
}
