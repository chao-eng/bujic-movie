package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/pkg/logger"
	"github.com/bujic-movie/bujic-movie/pkg/notify"
)

// MessageNotifyService manages third-party notification channels (CRUD), test
// sends, the channel field schema, and dispatching a notification when scraping
// completes.
type MessageNotifyService interface {
	Create(ch *entity.NotifyChannel) error
	Update(ch *entity.NotifyChannel) error
	Delete(id uint) error
	GetByID(id uint) (*entity.NotifyChannel, error)
	List() ([]entity.NotifyChannel, error)
	// ChannelTypes returns the per-type field schema for the frontend form.
	ChannelTypes() []notify.ChannelType
	// Test sends a test message through the given channel.
	Test(ctx context.Context, id uint) error
	// NotifyScrapeDone asynchronously notifies all enabled channels that a media
	// item finished scraping. Never blocks the caller.
	NotifyScrapeDone(title string, year int, mediaType, posterURL string)
}

type messageNotifyService struct {
	repo repository.NotifyChannelRepository
}

func NewMessageNotifyService(repo repository.NotifyChannelRepository) MessageNotifyService {
	return &messageNotifyService{repo: repo}
}

func (s *messageNotifyService) Create(ch *entity.NotifyChannel) error { return s.repo.Create(ch) }
func (s *messageNotifyService) Update(ch *entity.NotifyChannel) error { return s.repo.Update(ch) }
func (s *messageNotifyService) Delete(id uint) error                  { return s.repo.Delete(id) }

func (s *messageNotifyService) GetByID(id uint) (*entity.NotifyChannel, error) {
	return s.repo.GetByID(id)
}

func (s *messageNotifyService) List() ([]entity.NotifyChannel, error) { return s.repo.List() }

func (s *messageNotifyService) ChannelTypes() []notify.ChannelType { return notify.Types() }

func parseChannelConfig(raw string) map[string]string {
	cfg := map[string]string{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	return cfg
}

func (s *messageNotifyService) Test(ctx context.Context, id uint) error {
	ch, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	client, err := notify.New(ch.Type, parseChannelConfig(ch.Config))
	if err != nil {
		return err
	}
	return client.Send(ctx, notify.Message{
		Title: "测试通知",
		Text:  "这是一条来自 Bujic Movie 的测试消息，收到即表示渠道配置成功。",
	})
}

func (s *messageNotifyService) NotifyScrapeDone(title string, year int, mediaType, posterURL string) {
	channels, err := s.repo.List()
	if err != nil || len(channels) == 0 {
		return
	}

	typeLabel := "电视剧"
	if mediaType == "movie" {
		typeLabel = "电影"
	}
	name := title
	if year > 0 {
		name = fmt.Sprintf("%s (%d)", title, year)
	}
	msg := notify.Message{
		Title: fmt.Sprintf("【入库】%s", name),
		Text:  fmt.Sprintf("%s · 已刮削完成并入库", typeLabel),
		Image: posterURL,
	}

	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		ch := ch
		go func() {
			client, err := notify.New(ch.Type, parseChannelConfig(ch.Config))
			if err != nil {
				logger.Warn("[通知] 渠道 %s 初始化失败: %v", ch.Name, err)
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := client.Send(ctx, msg); err != nil {
				logger.Warn("[通知] 渠道 %s 发送失败: %v", ch.Name, err)
				return
			}
			logger.Info("[通知] 渠道 %s 已发送入库通知: %s", ch.Name, name)
		}()
	}
}
