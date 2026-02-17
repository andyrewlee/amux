package cli

import (
	"sync"
	"testing"
)

func TestNewAgentTabIDUniqueAcrossConcurrentCalls(t *testing.T) {
	const workers = 16
	const perWorker = 128
	total := workers * perWorker

	ids := make(map[string]struct{}, total)
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				id := newAgentTabID()
				if id == "" {
					t.Errorf("newAgentTabID() returned empty string")
					return
				}
				mu.Lock()
				if _, exists := ids[id]; exists {
					mu.Unlock()
					t.Errorf("duplicate tab id generated: %s", id)
					return
				}
				ids[id] = struct{}{}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(ids) != total {
		t.Fatalf("generated unique ids = %d, want %d", len(ids), total)
	}
}
