package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/pkg/logger"
	"github.com/bujic-movie/bujic-movie/pkg/mediaserver"
)

// NotificationService orchestrates media-server refreshes after content is
// transferred and scraped. The refresh target is decided by the media card's
// bound media library; the actual HTTP call is debounced and run asynchronously
// so it never blocks (or fails) the transfer/scrape flow.
type NotificationService interface {
	// NotifyRefreshForCard requests a refresh of the media library bound to the
	// given card. No-op when the card has no binding or the library is disabled.
	NotifyRefreshForCard(ctx context.Context, cardID uint)
}

type notificationService struct {
	libraryRepo repository.MediaLibraryRepository
	cardRepo    repository.MediaCardRepository

	mu     sync.Mutex
	timers map[string]*time.Timer
}

func NewNotificationService(libraryRepo repository.MediaLibraryRepository, cardRepo repository.MediaCardRepository) NotificationService {
	return &notificationService{
		libraryRepo: libraryRepo,
		cardRepo:    cardRepo,
		timers:      make(map[string]*time.Timer),
	}
}

func (s *notificationService) NotifyRefreshForCard(ctx context.Context, cardID uint) {
	if cardID == 0 {
		return
	}

	card, err := s.cardRepo.GetByID(cardID)
	if err != nil || card == nil || card.MediaLibraryID == 0 {
		// 卡片未绑定媒体库，跳过刷新
		return
	}

	lib, err := s.libraryRepo.GetByID(card.MediaLibraryID)
	if err != nil || lib == nil {
		logger.Warn("[媒体库] 卡片绑定的媒体库不存在 (cardID=%d, libraryID=%d)", cardID, card.MediaLibraryID)
		return
	}
	if !lib.Enabled {
		return
	}

	// 按媒体库做 5 秒防抖，合并短时间内对同一媒体库的多次触发为一次刷新
	key := fmt.Sprintf("%d", lib.ID)
	s.mu.Lock()
	if t, ok := s.timers[key]; ok {
		t.Stop()
	}
	libCopy := *lib
	s.timers[key] = time.AfterFunc(5*time.Second, func() {
		s.mu.Lock()
		delete(s.timers, key)
		s.mu.Unlock()
		s.refresh(&libCopy)
	})
	s.mu.Unlock()
}

func (s *notificationService) refresh(lib *entity.MediaLibrary) {
	srv, err := mediaserver.New(mediaserver.ServerType(lib.Type), lib.URL, lib.APIKey)
	if err != nil {
		logger.Warn("[媒体库] 构建客户端失败 %s: %v", lib.Name, err)
		return
	}

	logger.Info("[媒体库] 通知 %s (%s) 刷新媒体库", lib.Name, lib.Type)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Refresh(ctx, lib.LibraryID); err != nil {
		logger.Warn("[媒体库] %s 刷新失败: %v", lib.Name, err)
		return
	}
	logger.Info("[媒体库] %s 刷新通知已发送", lib.Name)
}
