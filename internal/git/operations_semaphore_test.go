package git

import (
	"context"
	"testing"
	"time"
)

func TestAcquireGitSlotRespectsContext(t *testing.T) {
	// Fill every slot so the next acquire must wait.
	for range gitSubprocessLimit {
		if !acquireGitSlot(context.Background()) {
			t.Fatal("expected free slot")
		}
	}
	t.Cleanup(func() {
		for range gitSubprocessLimit {
			releaseGitSlot()
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if acquireGitSlot(ctx) {
		releaseGitSlot()
		t.Fatal("expected acquire to fail once all slots are held and ctx expires")
	}

	// Freeing a slot lets a waiting acquire through again.
	releaseGitSlot()
	if !acquireGitSlot(context.Background()) {
		t.Fatal("expected acquire after release")
	}
}

func TestStatusManagerBackgroundTTL(t *testing.T) {
	m := NewStatusManager()
	m.SetCacheTTL(1 * time.Millisecond)
	status := &StatusResult{}
	m.UpdateCache("/ws", status)
	time.Sleep(5 * time.Millisecond)

	if m.GetCached("/ws") != nil {
		t.Fatal("expected short-TTL cache to expire")
	}
	if m.GetCachedBackground("/ws") != status {
		t.Fatal("expected background TTL to still serve the entry")
	}
	m.Invalidate("/ws")
	if m.GetCachedBackground("/ws") != nil {
		t.Fatal("expected explicit invalidation to bypass the background TTL")
	}
}
