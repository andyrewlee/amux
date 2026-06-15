package git

import (
	"testing"
	"time"
)

func TestStatusManagerCacheAndInvalidate(t *testing.T) {
	m := NewStatusManager()
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
	m := NewStatusManager()
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
