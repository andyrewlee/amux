package git

import (
	"sync"
	"time"
)

// StatusCache holds cached git status with TTL
type StatusCache struct {
	Status    *StatusResult
	FetchedAt time.Time
}

// IsExpired checks if the cache entry has expired
func (c *StatusCache) IsExpired(ttl time.Duration) bool {
	return time.Since(c.FetchedAt) > ttl
}

// StatusManager caches git status results by workspace root with a TTL.
type StatusManager struct {
	mu sync.RWMutex

	// Cache of status results by workspace root
	cache map[string]*StatusCache

	// Configuration
	cacheTTL time.Duration
	// backgroundTTL is the much longer freshness window used for workspaces
	// that are not active: their status only decorates dashboard rows, so
	// re-spawning git for every workspace on every project reload is waste
	// that compounds badly on a loaded machine.
	backgroundTTL time.Duration
}

// NewStatusManager creates a new status manager
func NewStatusManager() *StatusManager {
	return &StatusManager{
		cache:         make(map[string]*StatusCache),
		cacheTTL:      5 * time.Second,
		backgroundTTL: 60 * time.Second,
	}
}

// GetCached returns the cached status for a workspace, or nil if not cached/expired
func (m *StatusManager) GetCached(root string) *StatusResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if cache, ok := m.cache[root]; ok && !cache.IsExpired(m.cacheTTL) {
		return cache.Status
	}
	return nil
}

// GetCachedBackground returns the cached status under the background TTL, or
// nil if not cached/expired. Explicit invalidation (file-watcher events,
// commits) still forces a refresh regardless of TTL.
func (m *StatusManager) GetCachedBackground(root string) *StatusResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if cache, ok := m.cache[root]; ok && !cache.IsExpired(m.backgroundTTL) {
		return cache.Status
	}
	return nil
}

// Invalidate removes a workspace from the cache
func (m *StatusManager) Invalidate(root string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cache, root)
}

// UpdateCache directly updates the cache with a status result (no fetch)
func (m *StatusManager) UpdateCache(root string, status *StatusResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache[root] = &StatusCache{
		Status:    status,
		FetchedAt: time.Now(),
	}
}

// InvalidateAll clears the entire cache
func (m *StatusManager) InvalidateAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache = make(map[string]*StatusCache)
}

// SetCacheTTL sets the cache time-to-live
func (m *StatusManager) SetCacheTTL(ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cacheTTL = ttl
}
