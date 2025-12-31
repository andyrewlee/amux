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

// StatusManager manages async git status with caching and debouncing
type StatusManager struct {
	mu sync.RWMutex

	// Cache of status results by worktree root
	cache map[string]*StatusCache

	// Pending refresh requests (for debouncing)
	pending map[string]time.Time

	// Configuration
	cacheTTL      time.Duration
	debounceDelay time.Duration

	// Callback for status updates
	onUpdate func(root string, status *StatusResult, err error)
}

// NewStatusManager creates a new status manager
func NewStatusManager(onUpdate func(root string, status *StatusResult, err error)) *StatusManager {
	return &StatusManager{
		cache:         make(map[string]*StatusCache),
		pending:       make(map[string]time.Time),
		cacheTTL:      5 * time.Second,
		debounceDelay: 500 * time.Millisecond,
		onUpdate:      onUpdate,
	}
}

// GetCached returns the cached status for a worktree, or nil if not cached/expired
func (m *StatusManager) GetCached(root string) *StatusResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if cache, ok := m.cache[root]; ok && !cache.IsExpired(m.cacheTTL) {
		return cache.Status
	}
	return nil
}

// RequestRefresh requests an async status refresh for a worktree
// Uses debouncing to prevent too frequent refreshes
func (m *StatusManager) RequestRefresh(root string) {
	m.mu.Lock()

	// Check if there's already a pending request within debounce window
	if lastRequest, ok := m.pending[root]; ok {
		if time.Since(lastRequest) < m.debounceDelay {
			m.mu.Unlock()
			return
		}
	}

	m.pending[root] = time.Now()
	m.mu.Unlock()

	// Perform async refresh
	go m.refresh(root)
}

// refresh performs the actual git status fetch
func (m *StatusManager) refresh(root string) {
	// Wait for debounce delay
	time.Sleep(m.debounceDelay)

	// Check if this is still the latest request
	m.mu.Lock()
	pendingTime, ok := m.pending[root]
	if !ok {
		m.mu.Unlock()
		return
	}
	// If there's a newer request, skip this one
	if time.Since(pendingTime) < m.debounceDelay {
		m.mu.Unlock()
		return
	}
	delete(m.pending, root)
	m.mu.Unlock()

	// Fetch status
	status, err := GetStatus(root)

	// Update cache
	m.mu.Lock()
	if err == nil {
		m.cache[root] = &StatusCache{
			Status:    status,
			FetchedAt: time.Now(),
		}
	}
	m.mu.Unlock()

	// Notify callback
	if m.onUpdate != nil {
		m.onUpdate(root, status, err)
	}
}

// RefreshAll refreshes status for all cached worktrees
func (m *StatusManager) RefreshAll() {
	m.mu.RLock()
	roots := make([]string, 0, len(m.cache))
	for root := range m.cache {
		roots = append(roots, root)
	}
	m.mu.RUnlock()

	for _, root := range roots {
		m.RequestRefresh(root)
	}
}

// Invalidate removes a worktree from the cache
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

// SetDebounceDelay sets the debounce delay
func (m *StatusManager) SetDebounceDelay(delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.debounceDelay = delay
}
