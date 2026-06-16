package storage

import "sync"

var (
	backendsMu sync.RWMutex
	backends   = make(map[string]Storage)
)

// Register registers a storage backend under a specific name
func Register(name string, s Storage) {
	backendsMu.Lock()
	defer backendsMu.Unlock()
	backends[name] = s
}

// Get retrieves a registered storage backend by name
func Get(name string) (Storage, bool) {
	backendsMu.RLock()
	defer backendsMu.RUnlock()
	s, ok := backends[name]
	return s, ok
}
