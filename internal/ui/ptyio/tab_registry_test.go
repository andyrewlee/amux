package ptyio

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
)

type keyedTab struct {
	id    string
	alive bool
}

func resolveKeyedTabs(byWorkspace map[string][]*keyedTab, wsKey, id string) (*keyedTab, string) {
	return ResolveKeyedTab(byWorkspace, wsKey, id,
		func(t *keyedTab) string { return t.id },
		func(t *keyedTab) bool { return t != nil && t.alive },
		"test", "tab")
}

func TestResolveKeyedTab_ExactHit(t *testing.T) {
	tab := &keyedTab{id: "t1", alive: true}
	byWorkspace := map[string][]*keyedTab{"ws1": {tab}}

	got, key := resolveKeyedTabs(byWorkspace, "ws1", "t1")
	if got != tab || key != "ws1" {
		t.Fatalf("got (%v, %q), want (%v, %q)", got, key, tab, "ws1")
	}
}

func TestResolveKeyedTab_ScanFallbackReturnsActualKeyAndWarns(t *testing.T) {
	logDir := t.TempDir()
	if err := logging.Initialize(logDir, logging.LevelWarn); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	logPath := logging.GetLogPath()
	t.Cleanup(func() { _ = logging.Close() })

	tab := &keyedTab{id: "t1", alive: true}
	byWorkspace := map[string][]*keyedTab{"ws2": {tab}}

	got, key := resolveKeyedTabs(byWorkspace, "ws1", "t1")
	if got != tab || key != "ws2" {
		t.Fatalf("got (%v, %q), want (%v, %q)", got, key, tab, "ws2")
	}

	if err := logging.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !strings.Contains(string(data), "routed with workspace ws1 but filed under ws2") {
		t.Fatalf("expected stale-routing warning, log was: %s", data)
	}
}

func TestResolveKeyedTab_Miss(t *testing.T) {
	byWorkspace := map[string][]*keyedTab{"ws1": {{id: "t1", alive: true}}}

	got, key := resolveKeyedTabs(byWorkspace, "ws1", "nope")
	if got != nil || key != "" {
		t.Fatalf("got (%v, %q), want (nil, \"\")", got, key)
	}
	got, key = resolveKeyedTabs(byWorkspace, "unknown", "t1")
	if got == nil || key == "" {
		t.Fatal("stamped key missing from map should still find the tab by scan")
	}
}

func TestResolveKeyedTab_AliveFiltersBothPhases(t *testing.T) {
	dead := &keyedTab{id: "t1", alive: false}
	live := &keyedTab{id: "t1", alive: true}
	byWorkspace := map[string][]*keyedTab{
		"ws1": {dead},
		"ws2": {live},
	}

	// The dead tab in the exact bucket must not satisfy the lookup; the scan
	// finds the live one under the other key.
	got, key := resolveKeyedTabs(byWorkspace, "ws1", "t1")
	if got != live || key != "ws2" {
		t.Fatalf("got (%v, %q), want live tab under ws2", got, key)
	}

	byWorkspace = map[string][]*keyedTab{"ws1": {dead}, "ws2": {nil}}
	if got, key := resolveKeyedTabs(byWorkspace, "ws1", "t1"); got != nil || key != "" {
		t.Fatalf("dead-only and nil entries must miss, got (%v, %q)", got, key)
	}
}

func TestRebindMigratedTabs_UnlatchRearmAndRestart(t *testing.T) {
	var mu sync.Mutex
	st := &State{
		PendingOutput:     []byte("abc"),
		FlushScheduled:    true,
		FlushPendingSince: time.Now(),
		LastOutputAt:      time.Now().Add(-time.Second),
	}
	type tab struct{ st *State }
	tb := &tab{st: st}

	var events []string
	var lockedDuringExamine bool
	cmd := RebindMigratedTabs([]*tab{tb}, RebindHooks[*tab]{
		State: func(x *tab) *State { return x.st },
		Lock:  func(x *tab) *sync.Mutex { return &mu },
		ExamineLocked: func(x *tab) bool {
			lockedDuringExamine = !mu.TryLock()
			events = append(events, "examine")
			return true
		},
		Restart: func(x *tab) tea.Cmd {
			events = append(events, "restart")
			return func() tea.Msg { return "restart" }
		},
		FlushTiming: func(x *tab) time.Duration { return time.Minute },
		FlushMsg:    func(x *tab) tea.Msg { events = append(events, "flushmsg"); return "flush" },
	})

	if !lockedDuringExamine {
		t.Fatal("ExamineLocked ran without the tab lock held")
	}
	if len(events) != 2 || events[0] != "examine" || events[1] != "restart" {
		t.Fatalf("events = %v, want [examine restart]", events)
	}
	if !st.FlushScheduled {
		t.Fatal("pending tab should be re-armed")
	}
	if !st.FlushPendingSince.Equal(st.LastOutputAt) {
		t.Fatalf("FlushPendingSince = %v, want LastOutputAt %v", st.FlushPendingSince, st.LastOutputAt)
	}
	if cmd == nil {
		t.Fatal("expected batched restart+flush commands")
	}
}

func TestRebindMigratedTabs_UnlatchWithoutRearm(t *testing.T) {
	var mu sync.Mutex
	st := &State{FlushScheduled: true, FlushPendingSince: time.Now()}
	type tab struct{ st *State }

	flushMsgBuilt := false
	cmd := RebindMigratedTabs([]*tab{{st: st}}, RebindHooks[*tab]{
		State:         func(x *tab) *State { return x.st },
		Lock:          func(x *tab) *sync.Mutex { return &mu },
		ExamineLocked: func(x *tab) bool { return false },
		Restart:       func(x *tab) tea.Cmd { t.Fatal("no restart for dead terminal"); return nil },
		FlushTiming:   func(x *tab) time.Duration { return time.Minute },
		FlushMsg:      func(x *tab) tea.Msg { flushMsgBuilt = true; return "flush" },
	})

	if st.FlushScheduled {
		t.Fatal("idle tab's stale flush latch should stay cleared")
	}
	if flushMsgBuilt {
		t.Fatal("no flush message should be built without pending output")
	}
	if cmd != nil {
		t.Fatal("expected nil cmd when nothing restarted and nothing pended")
	}
}

func TestRebindMigratedTabs_SkipsNilState(t *testing.T) {
	type tab struct{ st *State }
	examined := false
	cmd := RebindMigratedTabs([]*tab{nil, {st: nil}}, RebindHooks[*tab]{
		State: func(x *tab) *State {
			if x == nil {
				return nil
			}
			return x.st
		},
		Lock:          func(x *tab) *sync.Mutex { t.Fatal("nil-state tab must not be locked"); return nil },
		ExamineLocked: func(x *tab) bool { examined = true; return false },
		Restart:       func(x *tab) tea.Cmd { return nil },
		FlushTiming:   func(x *tab) time.Duration { return time.Minute },
		FlushMsg:      func(x *tab) tea.Msg { return nil },
	})
	if examined {
		t.Fatal("nil-state tabs must not be examined")
	}
	if cmd != nil {
		t.Fatal("expected nil cmd when every tab is skipped")
	}
}
