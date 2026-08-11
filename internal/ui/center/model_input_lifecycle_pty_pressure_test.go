package center

import (
	"bytes"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

func TestUpdatePTYOutput_DetachesBackgroundTabAfterSustainedPressure(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	active := &Tab{ID: TabID("active"), Assistant: "codex", Workspace: ws, Running: true}
	background := &Tab{
		ID:          TabID("background"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "amux-pressure-session",
		Agent: &appPty.Agent{
			Workspace: ws,
			Session:   "amux-pressure-session",
		},
		Running: true,
		State: ptyio.State{
			PendingOutput: bytes.Repeat([]byte("x"), ptyBackgroundDetachThreshold),
		},
		backgroundPTYPressureSince: time.Now().Add(-ptyBackgroundDetachGrace - time.Second),
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{active, background}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.workspace = ws

	cmd := m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       background.ID,
		Data:        []byte("new frame"),
	})
	if cmd == nil {
		t.Fatal("expected pressure detach command")
	}
	msg, ok := cmd().(messages.TabDetached)
	if !ok {
		t.Fatalf("expected messages.TabDetached, got %T", msg)
	}
	if msg.WorkspaceID != wsID || msg.Index != 1 {
		t.Fatalf("detach target = (%q, %d), want (%q, 1)", msg.WorkspaceID, msg.Index, wsID)
	}

	background.mu.Lock()
	detached := background.Detached
	running := background.Running
	discardQueued := background.discardDetachedPTYOutput
	background.mu.Unlock()
	if !detached || running {
		t.Fatalf("pressure-detached state = (detached=%v running=%v), want (true false)", detached, running)
	}
	if !discardQueued {
		t.Fatal("expected queued output from the released reader to be discarded")
	}
	if background.Agent != nil {
		t.Fatal("expected amux PTY client released")
	}
	if background.SessionName != "amux-pressure-session" {
		t.Fatalf("session name = %q, want tmux session preserved", background.SessionName)
	}
	if background.PendingOutput != nil {
		t.Fatalf("expected stale backlog cleared, got %d bytes", len(background.PendingOutput))
	}

	_ = m.updatePTYOutput(PTYOutput{WorkspaceID: wsID, TabID: background.ID, Data: []byte("queued")})
	if background.PendingOutput != nil {
		t.Fatalf("expected queued post-detach output discarded, got %q", background.PendingOutput)
	}
}

func TestUpdatePTYOutput_KeepsBackgroundTabAttachedDuringPressureGrace(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	active := &Tab{ID: TabID("active"), Assistant: "codex", Workspace: ws, Running: true}
	background := &Tab{
		ID:          TabID("background-burst"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "amux-burst-session",
		Agent: &appPty.Agent{
			Workspace: ws,
			Session:   "amux-burst-session",
		},
		Running: true,
		State: ptyio.State{
			PendingOutput: bytes.Repeat([]byte("x"), ptyBackgroundDetachThreshold-1),
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{active, background}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYOutput(PTYOutput{WorkspaceID: wsID, TabID: background.ID, Data: []byte("burst")})
	if background.Detached || !background.Running || background.Agent == nil {
		t.Fatal("brief background pressure should remain attached during the grace window")
	}
	if background.backgroundPTYPressureSince.IsZero() {
		t.Fatal("expected background pressure grace window to be armed")
	}
}

func TestTabFocusClearsBackgroundPressureGrace(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                         TabID("focused"),
		Assistant:                  "codex",
		Workspace:                  ws,
		Running:                    true,
		backgroundPTYPressureSince: time.Now().Add(-ptyBackgroundDetachGrace),
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.workspace = ws

	m.setActiveTabIdxForWorkspace(wsID, 0)
	if !tab.backgroundPTYPressureSince.IsZero() {
		t.Fatal("focusing a tab should reset its background pressure grace window")
	}
}

func TestUpdatePTYOutput_NeverPressureDetachesVisibleTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("active"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "amux-active-session",
		Agent: &appPty.Agent{
			Workspace: ws,
			Session:   "amux-active-session",
		},
		Running: true,
		State: ptyio.State{
			PendingOutput: bytes.Repeat([]byte("x"), ptyMaxBufferedBytes-1),
		},
		backgroundPTYPressureSince: time.Now().Add(-ptyBackgroundDetachGrace - time.Second),
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYOutput(PTYOutput{WorkspaceID: wsID, TabID: tab.ID, Data: []byte("visible")})
	if tab.Detached || !tab.Running || tab.Agent == nil {
		t.Fatal("visible tab must remain attached under backlog pressure")
	}
	if !tab.backgroundPTYPressureSince.IsZero() {
		t.Fatal("visible tab should clear background pressure tracking")
	}
}
