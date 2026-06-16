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
	repo repository.MediaCardRepository
}

func NewMediaCardService(repo repository.MediaCardRepository) MediaCardService {
	return &mediaCardService{repo: repo}
}

func (s *mediaCardService) Create(card *entity.MediaCard) error {
	return s.repo.Create(card)
}

func (s *mediaCardService) Update(card *entity.MediaCard) error {
	return s.repo.Update(card)
}

func (s *mediaCardService) Delete(id uint) error {
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
