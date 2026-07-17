package sidebar

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// EnforceAttachedTerminalTabLimit operates purely on in-memory model state:
// seeded states have no Terminal and no running reader, so detachState only
// flips flags. No tea.Cmd is returned and no tmux call is made.

func limitTestWorkspace(name string) *data.Workspace {
	return data.NewWorkspace(name, name, "main", "/repo/"+name, "/repo/"+name+"/ws")
}

func seedAttachedTerminal(m *TerminalModel, wsID string, tabID TerminalTabID) *TerminalState {
	ts := &TerminalState{
		Running:     true,
		SessionName: "sess-" + string(tabID),
	}
	tab := &TerminalTab{ID: tabID, Name: "Terminal", State: ts}
	m.tabs.ByWorkspace[wsID] = append(m.tabs.ByWorkspace[wsID], tab)
	if len(m.tabs.ByWorkspace[wsID]) == 1 {
		m.tabs.ActiveByWorkspace[wsID] = 0
	}
	return ts
}

func assertAttached(t *testing.T, ts *TerminalState, want bool) {
	t.Helper()
	ts.mu.Lock()
	defer ts.mu.Unlock()
	attached := ts.Running && !ts.Detached
	if attached != want {
		t.Fatalf("attached = %v, want %v (Running=%v Detached=%v)", attached, want, ts.Running, ts.Detached)
	}
	if !want {
		if !ts.Detached {
			t.Fatal("expected Detached=true after auto-detach")
		}
		if ts.UserDetached {
			t.Fatal("auto-detach must not set UserDetached")
		}
	}
}

func TestEnforceAttachedTerminalTabLimit_DetachesLRUBeyondLimit(t *testing.T) {
	m := NewTerminalModel()
	base := time.Now().Add(-time.Hour)
	states := make(map[string]*TerminalState)
	for i, name := range []string{"ws1", "ws2", "ws3"} {
		ws := limitTestWorkspace(name)
		wsID := string(ws.ID())
		states[name] = seedAttachedTerminal(m, wsID, TerminalTabID(name+"-tab"))
		m.lastActiveAt[wsID] = base.Add(time.Duration(i) * time.Minute)
	}
	active := limitTestWorkspace("ws4")
	states["ws4"] = seedAttachedTerminal(m, string(active.ID()), "ws4-tab")
	m.setWorkspace(active)

	detached := m.EnforceAttachedTerminalTabLimit(2)

	if len(detached) != 2 {
		t.Fatalf("detached %d tabs, want 2: %+v", len(detached), detached)
	}
	if detached[0].TabID != "ws1-tab" || detached[1].TabID != "ws2-tab" {
		t.Fatalf("expected LRU order ws1-tab, ws2-tab; got %+v", detached)
	}
	assertAttached(t, states["ws1"], false)
	assertAttached(t, states["ws2"], false)
	assertAttached(t, states["ws3"], true)
	assertAttached(t, states["ws4"], true)
}

func TestEnforceAttachedTerminalTabLimit_NeverDetachesActiveWorkspace(t *testing.T) {
	m := NewTerminalModel()
	active := limitTestWorkspace("active")
	wsID := string(active.ID())
	first := seedAttachedTerminal(m, wsID, "active-tab-1")
	second := seedAttachedTerminal(m, wsID, "active-tab-2")
	m.setWorkspace(active)

	if detached := m.EnforceAttachedTerminalTabLimit(1); detached != nil {
		t.Fatalf("expected no detaches when only the active workspace is over the limit, got %+v", detached)
	}
	assertAttached(t, first, true)
	assertAttached(t, second, true)
}

func TestEnforceAttachedTerminalTabLimit_ZeroDisablesEnforcement(t *testing.T) {
	m := NewTerminalModel()
	background := limitTestWorkspace("bg")
	ts := seedAttachedTerminal(m, string(background.ID()), "bg-tab")
	m.setWorkspace(limitTestWorkspace("active"))

	if detached := m.EnforceAttachedTerminalTabLimit(0); detached != nil {
		t.Fatalf("limit 0 must disable enforcement, got %+v", detached)
	}
	assertAttached(t, ts, true)
}

func TestEnforceAttachedTerminalTabLimit_SkipsReattachInFlight(t *testing.T) {
	m := NewTerminalModel()
	inFlightWs := limitTestWorkspace("inflight")
	inFlight := seedAttachedTerminal(m, string(inFlightWs.ID()), "inflight-tab")
	inFlight.reattachInFlight = true
	idleWs := limitTestWorkspace("idle")
	idle := seedAttachedTerminal(m, string(idleWs.ID()), "idle-tab")
	m.setWorkspace(limitTestWorkspace("active"))

	detached := m.EnforceAttachedTerminalTabLimit(1)

	if len(detached) != 1 || detached[0].TabID != "idle-tab" {
		t.Fatalf("expected only idle-tab detached, got %+v", detached)
	}
	assertAttached(t, inFlight, true)
	assertAttached(t, idle, false)
}

func TestEnforceAttachedTerminalTabLimit_RecentOutputOutranksStaleSelection(t *testing.T) {
	m := NewTerminalModel()
	now := time.Now()

	// Selected recently but silent since.
	quietWs := limitTestWorkspace("quiet")
	quiet := seedAttachedTerminal(m, string(quietWs.ID()), "quiet-tab")
	m.lastActiveAt[string(quietWs.ID())] = now.Add(-5 * time.Minute)

	// Selected long ago but still streaming output (e.g. a build).
	busyWs := limitTestWorkspace("busy")
	busy := seedAttachedTerminal(m, string(busyWs.ID()), "busy-tab")
	m.lastActiveAt[string(busyWs.ID())] = now.Add(-time.Hour)
	busy.LastOutputAt = now.Add(-time.Second)

	m.setWorkspace(limitTestWorkspace("active"))

	detached := m.EnforceAttachedTerminalTabLimit(1)

	if len(detached) != 1 || detached[0].TabID != "quiet-tab" {
		t.Fatalf("expected quiet-tab detached before busy-tab, got %+v", detached)
	}
	assertAttached(t, quiet, false)
	assertAttached(t, busy, true)
}

func TestRebindWorkspaceIDMigratesLastActive(t *testing.T) {
	m := NewTerminalModel()
	previous := limitTestWorkspace("old")
	current := limitTestWorkspace("new")
	oldID := string(previous.ID())
	newID := string(current.ID())
	seedAttachedTerminal(m, oldID, "old-tab")
	stamp := time.Now().Add(-time.Minute)
	m.lastActiveAt[oldID] = stamp

	m.RebindWorkspaceID(previous, current)

	if _, ok := m.lastActiveAt[oldID]; ok {
		t.Fatal("old workspace ID must be removed from lastActiveAt on rebind")
	}
	if got := m.lastActiveAt[newID]; !got.Equal(stamp) {
		t.Fatalf("lastActiveAt not migrated to new ID: got %v, want %v", got, stamp)
	}
}

func TestCloseTerminalPrunesLastActive(t *testing.T) {
	m := NewTerminalModel()
	ws := limitTestWorkspace("gone")
	wsID := string(ws.ID())
	seedAttachedTerminal(m, wsID, "gone-tab")
	m.lastActiveAt[wsID] = time.Now()

	m.CloseTerminal(wsID)

	if _, ok := m.lastActiveAt[wsID]; ok {
		t.Fatal("CloseTerminal must prune the workspace's lastActiveAt entry")
	}
}

func TestSetWorkspaceStampsLastActive(t *testing.T) {
	m := NewTerminalModel()
	ws := limitTestWorkspace("stamp")
	before := time.Now()
	m.SetWorkspacePreview(ws)
	stamped, ok := m.lastActiveAt[string(ws.ID())]
	if !ok {
		t.Fatal("setWorkspace must record lastActiveAt for the workspace")
	}
	if stamped.Before(before) {
		t.Fatalf("lastActiveAt %v predates setWorkspace call at %v", stamped, before)
	}
}
