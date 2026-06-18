package service

import (
	"context"
	"sync"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/pkg/mediaserver"
)

// LibraryStatus is the online state of a media server, used for heartbeat display.
type LibraryStatus struct {
	ID     uint `json:"id"`
	Online bool `json:"online"`
}

// MediaLibraryService manages user-configured media servers (CRUD), connectivity
// checks, heartbeat status, library enumeration and refresh.
type MediaLibraryService interface {
	Create(lib *entity.MediaLibrary) error
	Update(lib *entity.MediaLibrary) error
	Delete(id uint) error
	GetByID(id uint) (*entity.MediaLibrary, error)
	List() ([]entity.MediaLibrary, error)
	TestConnection(ctx context.Context, id uint) error
	// Refresh refreshes the server's selected library (or all if none selected).
	Refresh(ctx context.Context, id uint) error
	// ProbeLibraries lists the libraries on a server identified by raw credentials
	// (used by the edit form before the record is saved).
	ProbeLibraries(ctx context.Context, serverType, url, apiKey string) ([]mediaserver.Library, error)
	// Statuses returns the online state of every configured media server.
	Statuses(ctx context.Context) []LibraryStatus
}

type mediaLibraryService struct {
	repo repository.MediaLibraryRepository
}

func NewMediaLibraryService(repo repository.MediaLibraryRepository) MediaLibraryService {
	return &mediaLibraryService{repo: repo}
}

func (s *mediaLibraryService) Create(lib *entity.MediaLibrary) error { return s.repo.Create(lib) }
func (s *mediaLibraryService) Update(lib *entity.MediaLibrary) error { return s.repo.Update(lib) }
func (s *mediaLibraryService) Delete(id uint) error                  { return s.repo.Delete(id) }

func (s *mediaLibraryService) GetByID(id uint) (*entity.MediaLibrary, error) {
	return s.repo.GetByID(id)
}

func (s *mediaLibraryService) List() ([]entity.MediaLibrary, error) { return s.repo.List() }

func (s *mediaLibraryService) TestConnection(ctx context.Context, id uint) error {
	lib, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	srv, err := mediaserver.New(mediaserver.ServerType(lib.Type), lib.URL, lib.APIKey)
	if err != nil {
		return err
	}
	return srv.TestConnection(ctx)
}

func (s *mediaLibraryService) Refresh(ctx context.Context, id uint) error {
	lib, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	srv, err := mediaserver.New(mediaserver.ServerType(lib.Type), lib.URL, lib.APIKey)
	if err != nil {
		return err
	}
	return srv.Refresh(ctx, lib.LibraryID)
}

func (s *mediaLibraryService) ProbeLibraries(ctx context.Context, serverType, url, apiKey string) ([]mediaserver.Library, error) {
	srv, err := mediaserver.New(mediaserver.ServerType(serverType), url, apiKey)
	if err != nil {
		return nil, err
	}
	return srv.ListLibraries(ctx)
}

func (s *mediaLibraryService) Statuses(ctx context.Context) []LibraryStatus {
	libs, err := s.repo.List()
	if err != nil {
		return nil
	}
	results := make([]LibraryStatus, len(libs))
	var wg sync.WaitGroup
	for i := range libs {
		wg.Add(1)
		go func(i int, lib entity.MediaLibrary) {
			defer wg.Done()
			online := false
			if lib.Enabled {
				if srv, err := mediaserver.New(mediaserver.ServerType(lib.Type), lib.URL, lib.APIKey); err == nil {
					cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
					defer cancel()
					online = srv.TestConnection(cctx) == nil
				}
			}
			results[i] = LibraryStatus{ID: lib.ID, Online: online}
		}(i, libs[i])
	}
	wg.Wait()
	return results
}
