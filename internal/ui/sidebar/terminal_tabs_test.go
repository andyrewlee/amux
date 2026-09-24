package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/vterm"
)

func TestAddTabsFromSessionInfosAttachRespectsFlag(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())

	cmds := m.AddTabsFromSessionInfos(ws, []SessionAttachInfo{
		{Name: "sess-1", Attach: true, DetachExisting: true},
		{Name: "sess-2", Attach: true, DetachExisting: false},
	})
	if len(cmds) != 2 {
		t.Fatalf("expected 2 cmds for attachable sessions, got %d", len(cmds))
	}
	if got := len(m.tabs.ByWorkspace[wsID]); got != 2 {
		t.Fatalf("expected 2 tabs, got %d", got)
	}
}

// TestAddTabsFromSessionInfos covers the non-happy-path branches of the tmux
// discovery funnel: intra-call dedup, Attach:false detached tabs, empty-name
// skips, and the refusals for user-detached / healthy-running existing tabs.
func TestAddTabsFromSessionInfos(t *testing.T) {
	newWS := func() (*data.Workspace, string) {
		ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
		return ws, string(ws.ID())
	}
	seedTab := func(m *TerminalModel, wsID, session string, st *TerminalState) {
		st.SessionName = session
		m.tabs.ByWorkspace[wsID] = append(m.tabs.ByWorkspace[wsID], &TerminalTab{
			ID:    generateTerminalTabID(),
			Name:  "Terminal 1",
			State: st,
		})
	}

	t.Run("intra-call dedup refuses the second entry", func(t *testing.T) {
		m := NewTerminalModel()
		ws, wsID := newWS()
		cmds := m.AddTabsFromSessionInfos(ws, []SessionAttachInfo{
			{Name: "s", Attach: true},
			{Name: "s", Attach: true},
		})
		tabs := m.tabs.ByWorkspace[wsID]
		if len(tabs) != 1 {
			t.Fatalf("expected 1 tab after dedup, got %d", len(tabs))
		}
		if len(cmds) != 1 {
			t.Fatalf("expected 1 cmd after dedup, got %d", len(cmds))
		}
		// The second entry resolved through tabBySession and was refused by
		// the in-flight reattach flag the first attach set — prove it stays
		// set rather than being silently treated as unknown.
		tabs[0].State.mu.Lock()
		inFlight := tabs[0].State.Reattach.InFlight
		tabs[0].State.mu.Unlock()
		if !inFlight {
			t.Fatal("expected Reattach.InFlight to remain set after intra-call dedup")
		}
	})

	t.Run("Attach false creates a detached tab with no cmd", func(t *testing.T) {
		m := NewTerminalModel()
		ws, wsID := newWS()
		cmds := m.AddTabsFromSessionInfos(ws, []SessionAttachInfo{{Name: "s", Attach: false}})
		if len(cmds) != 0 {
			t.Fatalf("expected 0 cmds for Attach:false, got %d", len(cmds))
		}
		tabs := m.tabs.ByWorkspace[wsID]
		if len(tabs) != 1 {
			t.Fatalf("expected 1 tab, got %d", len(tabs))
		}
		if !tabs[0].State.Detached || tabs[0].State.Running {
			t.Fatalf("expected detached non-running tab, got %+v", tabs[0].State)
		}
	})

	t.Run("empty session name is skipped", func(t *testing.T) {
		m := NewTerminalModel()
		ws, wsID := newWS()
		cmds := m.AddTabsFromSessionInfos(ws, []SessionAttachInfo{
			{Name: ""},
			{Name: "ok", Attach: true},
		})
		tabs := m.tabs.ByWorkspace[wsID]
		if len(tabs) != 1 || tabs[0].State.SessionName != "ok" {
			t.Fatalf("expected only the named session to produce a tab: %+v", tabs)
		}
		if len(cmds) != 1 {
			t.Fatalf("expected 1 cmd, got %d", len(cmds))
		}
	})

	t.Run("user-detached existing tab is not resurrected", func(t *testing.T) {
		m := NewTerminalModel()
		ws, wsID := newWS()
		seedTab(m, wsID, "s", &TerminalState{UserDetached: true, Detached: true})
		cmds := m.AddTabsFromSessionInfos(ws, []SessionAttachInfo{{Name: "s", Attach: true}})
		if len(cmds) != 0 {
			t.Fatalf("expected no cmd for user-detached tab, got %d", len(cmds))
		}
		if len(m.tabs.ByWorkspace[wsID]) != 1 {
			t.Fatal("expected the existing tab to be reused, not duplicated")
		}
	})

	t.Run("healthy-running existing tab is not re-attached", func(t *testing.T) {
		m := NewTerminalModel()
		ws, wsID := newWS()
		seedTab(m, wsID, "s", &TerminalState{
			Running:  true,
			Terminal: &pty.Terminal{},
			VTerm:    vterm.New(10, 3),
			Detached: false,
		})
		cmds := m.AddTabsFromSessionInfos(ws, []SessionAttachInfo{{Name: "s", Attach: true}})
		if len(cmds) != 0 {
			t.Fatalf("expected no cmd for a healthy running tab, got %d", len(cmds))
		}
		if len(m.tabs.ByWorkspace[wsID]) != 1 {
			t.Fatal("expected the existing tab to be reused, not duplicated")
		}
	})

	t.Run("nil workspace and empty sessions are no-ops", func(t *testing.T) {
		m := NewTerminalModel()
		ws, _ := newWS()
		if cmds := m.AddTabsFromSessionInfos(nil, []SessionAttachInfo{{Name: "s", Attach: true}}); cmds != nil {
			t.Fatalf("expected nil cmds for nil workspace, got %v", cmds)
		}
		if cmds := m.AddTabsFromSessionInfos(ws, nil); cmds != nil {
			t.Fatalf("expected nil cmds for empty sessions, got %v", cmds)
		}
	})

	t.Run("first added tab becomes active", func(t *testing.T) {
		m := NewTerminalModel()
		ws, wsID := newWS()
		m.AddTabsFromSessionInfos(ws, []SessionAttachInfo{{Name: "s", Attach: false}})
		if idx, ok := m.tabs.ActiveByWorkspace[wsID]; !ok || idx != 0 {
			t.Fatalf("expected first tab to activate at index 0, got %v/%v", idx, ok)
		}
	})
}
