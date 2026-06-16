package service

import (
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
)

type MediaCardService interface {
	Create(card *entity.MediaCard) error
	Update(card *entity.MediaCard) error
	Delete(id uint) error
	GetByID(id uint) (*entity.MediaCard, error)
	List() ([]entity.MediaCard, error)
	GetDefault() (*entity.MediaCard, error)
	SetDefault(id uint) error
}

type mediaCardService struct {
	repo       repository.MediaCardRepository
	watcherSvc WatcherService
}

func NewMediaCardService(repo repository.MediaCardRepository, watcherSvc WatcherService) MediaCardService {
	return &mediaCardService{repo: repo, watcherSvc: watcherSvc}
}

func (s *mediaCardService) Create(card *entity.MediaCard) error {
	if err := s.repo.Create(card); err != nil {
		return err
	}
	if card.WatchDirectory {
		_ = s.watcherSvc.WatchCard(card)
	}
	return nil
}

func (s *mediaCardService) Update(card *entity.MediaCard) error {
	oldCard, err := s.repo.GetByID(card.ID)
	if err == nil && oldCard != nil {
		if oldCard.WatchDirectory {
			s.watcherSvc.UnwatchCard(oldCard.ID)
		}
	}

	if err := s.repo.Update(card); err != nil {
		return err
	}

	if card.WatchDirectory {
		_ = s.watcherSvc.WatchCard(card)
	}
	return nil
}

func (s *mediaCardService) Delete(id uint) error {
	s.watcherSvc.UnwatchCard(id)
	return s.repo.Delete(id)
}

func (s *mediaCardService) GetByID(id uint) (*entity.MediaCard, error) {
	return s.repo.GetByID(id)
}

func (s *mediaCardService) List() ([]entity.MediaCard, error) {
	return s.repo.List()
}

func (s *mediaCardService) GetDefault() (*entity.MediaCard, error) {
	return s.repo.GetDefault()
}

func (s *mediaCardService) SetDefault(id uint) error {
	return s.repo.SetDefault(id)
}
