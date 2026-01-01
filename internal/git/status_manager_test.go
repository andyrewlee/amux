package git

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestStatusManagerCacheAndInvalidate(t *testing.T) {
	m := NewStatusManager(nil)
	status := &StatusResult{Clean: true}

	if cached := m.GetCached("/tmp"); cached != nil {
		t.Fatalf("expected nil cache before update")
	}

	m.UpdateCache("/tmp", status)
	if cached := m.GetCached("/tmp"); cached == nil {
		t.Fatalf("expected cached status after UpdateCache")
	}

	m.Invalidate("/tmp")
	if cached := m.GetCached("/tmp"); cached != nil {
		t.Fatalf("expected cache to be invalidated")
	}
}

func TestStatusManagerCacheExpiry(t *testing.T) {
	m := NewStatusManager(nil)
	m.SetCacheTTL(10 * time.Millisecond)
	m.UpdateCache("/tmp", &StatusResult{Clean: true})

	if cached := m.GetCached("/tmp"); cached == nil {
		t.Fatalf("expected cached status immediately after UpdateCache")
	}

	time.Sleep(15 * time.Millisecond)
	if cached := m.GetCached("/tmp"); cached != nil {
		t.Fatalf("expected cache to expire")
	}
}

func TestStatusManagerRequestRefreshDebounced(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)

	var count int32
	done := make(chan struct{}, 1)
	m := NewStatusManager(func(root string, status *StatusResult, err error) {
		atomic.AddInt32(&count, 1)
		done <- struct{}{}
	})
	m.SetDebounceDelay(10 * time.Millisecond)

	m.RequestRefresh(repo)
	m.RequestRefresh(repo)

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for status refresh")
	}

	// Give a little extra time for any duplicate callbacks.
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Fatalf("expected 1 refresh callback after debouncing, got %d", got)
	}
}
