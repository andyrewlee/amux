package activity

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// TestActiveWorkspaceIDsFromTags_PrunesLongUnseenStates proves a session state
// that goes unseen for more than pruneAfterScans is dropped (reported in removed
// and omitted from updatedStates) while a session seen every scan persists. This
// bounds the otherwise unbounded growth of the hysteresis state map.
func TestActiveWorkspaceIDsFromTags_PrunesLongUnseenStates(t *testing.T) {
	const seen = "sess-seen"
	const gone = "sess-gone"
	now := time.Now()

	// Only the seen session is reported each scan; the gone session lives only in
	// the persistent state map and never reappears (e.g. a deleted workspace).
	sessions := []TaggedSession{
		{
			Session:       tmux.SessionActivity{Name: seen, WorkspaceID: "ws-seen", Type: "agent"},
			LastOutputAt:  now.Add(-10 * time.Second),
			HasLastOutput: true,
		},
	}
	infoBySession := map[string]SessionInfo{
		seen: {WorkspaceID: "ws-seen", IsChat: true},
		gone: {WorkspaceID: "ws-gone", IsChat: true},
	}
	states := map[string]*SessionState{
		seen: {Score: ScoreThreshold, Initialized: true},
		gone: {Score: ScoreThreshold, Initialized: true},
	}
	recentActivity := map[string]bool{seen: true}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "output", true }
	hashFn := func(string) [16]byte { return [16]byte{1} }

	var lastRemoved []string
	var lastUpdated map[string]*SessionState
	// pruneAfterScans retains; the (pruneAfterScans+1)-th unseen scan crosses the
	// bound and prunes.
	for i := 0; i <= pruneAfterScans; i++ {
		_, updated, removed := ActiveWorkspaceIDsFromTagsWithRemoved(
			infoBySession, sessions, recentActivity, states, tmux.Options{}, captureFn, hashFn,
		)
		// Apply removals like the app's merge step does.
		for _, name := range removed {
			delete(states, name)
		}
		lastRemoved, lastUpdated = removed, updated

		if i < pruneAfterScans {
			if containsString(removed, gone) {
				t.Fatalf("scan %d: gone session pruned too early (within retain window)", i)
			}
			if _, ok := updated[gone]; !ok {
				t.Fatalf("scan %d: gone session should still be reset-and-retained, not dropped", i)
			}
		}
	}

	if !containsString(lastRemoved, gone) {
		t.Fatalf("expected %q in removed on the crossing scan, got %v", gone, lastRemoved)
	}
	if _, ok := lastUpdated[gone]; ok {
		t.Fatalf("expected pruned %q to be absent from updatedStates", gone)
	}
	if _, ok := lastUpdated[seen]; !ok {
		t.Fatalf("expected continuously-seen %q to persist in updatedStates", seen)
	}
	if _, ok := states[gone]; ok {
		t.Fatalf("expected %q removed from the state map after pruning", gone)
	}
	if _, ok := states[seen]; !ok {
		t.Fatalf("expected %q retained in the state map", seen)
	}
}

func containsString(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}
