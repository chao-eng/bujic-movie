package service

import (
	"sync"

	"github.com/bujic-movie/bujic-movie/internal/service/connector"
)

// siteState caches one connector per indexer so login cookies and rate limiters
// survive across searches. The cache is process-local and rebuilt on restart.
type siteState struct {
	mu    sync.RWMutex
	items map[uint]connector.Connector
}

func newSiteState() *siteState {
	return &siteState{items: map[uint]connector.Connector{}}
}

func (s *siteState) get(id uint) connector.Connector {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.items[id]
}

func (s *siteState) put(id uint, conn connector.Connector) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[id] = conn
}

func (s *siteState) forget(id uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, id)
}
