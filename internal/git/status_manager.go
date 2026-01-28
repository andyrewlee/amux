package git

import (
	"context"
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

	// Cache of status results by workspace root
	cache map[string]*StatusCache

	// Pending refresh requests (for debouncing)
	pending map[string]time.Time

	// Configuration
	cacheTTL      time.Duration
	debounceDelay time.Duration

	// Callback for status updates
	onUpdate func(root string, status *StatusResult, err error)

	reqCh chan string
}

// NewStatusManager creates a new status manager
func NewStatusManager(onUpdate func(root string, status *StatusResult, err error)) *StatusManager {
	return &StatusManager{
		cache:         make(map[string]*StatusCache),
		pending:       make(map[string]time.Time),
		cacheTTL:      5 * time.Second,
		debounceDelay: 500 * time.Millisecond,
		onUpdate:      onUpdate,
		reqCh:         make(chan string, 64),
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

// RequestRefresh requests an async status refresh for a workspace
// Uses debouncing to prevent too frequent refreshes
func (m *StatusManager) RequestRefresh(root string) {
	if root == "" {
		return
	}
	if m.reqCh == nil {
		return
	}
	select {
	case m.reqCh <- root:
	default:
		// Drop if backlogged; next tick will pick up further changes.
	}
}

// Run processes refresh requests until the context is canceled.
func (m *StatusManager) Run(ctx context.Context) error {
	if m == nil || m.reqCh == nil {
		return nil
	}
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	for {
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil
		case root := <-m.reqCh:
			m.mu.Lock()
			m.pending[root] = time.Now().Add(m.debounceDelay)
			next, ok := m.nextPendingLocked()
			m.mu.Unlock()
			resetTimer(timer, next, ok)
		case <-timer.C:
			due := m.popDue(time.Now())
			for _, root := range due {
				m.refreshNow(root)
			}
			m.mu.Lock()
			next, ok := m.nextPendingLocked()
			m.mu.Unlock()
			resetTimer(timer, next, ok)
		}
	}
}

func (m *StatusManager) nextPendingLocked() (time.Time, bool) {
	var next time.Time
	for _, t := range m.pending {
		if next.IsZero() || t.Before(next) {
			next = t
		}
	}
	if next.IsZero() {
		return time.Time{}, false
	}
	return next, true
}

func (m *StatusManager) popDue(now time.Time) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var due []string
	for root, t := range m.pending {
		if !t.After(now) {
			due = append(due, root)
			delete(m.pending, root)
		}
	}
	return due
}

func (m *StatusManager) refreshNow(root string) {
	status, err := GetStatus(root)
	m.mu.Lock()
	if err == nil {
		m.cache[root] = &StatusCache{
			Status:    status,
			FetchedAt: time.Now(),
		}
	}
	m.mu.Unlock()
	if m.onUpdate != nil {
		m.onUpdate(root, status, err)
	}
}

func resetTimer(t *time.Timer, next time.Time, ok bool) {
	if t == nil {
		return
	}
	if !ok {
		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
		return
	}
	d := time.Until(next)
	if d < 0 {
		d = 0
	}
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

// RefreshAll refreshes status for all cached workspaces
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

// SetDebounceDelay sets the debounce delay
func (m *StatusManager) SetDebounceDelay(delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.debounceDelay = delay
}
