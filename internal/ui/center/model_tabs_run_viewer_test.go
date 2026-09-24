package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// A run-viewer attach must never stamp tab tags or session options on the
// borrowed session — CreateRunViewerTab routes through the attach-only path
// and marks the resulting tab DetachOnly so close can never kill the script.
func TestCreateRunViewerTab_AttachesWithoutTags(t *testing.T) {
	var calls []string
	restoreReattachSeams(t)
	sessionStateForFn = func(string, tmux.Options) (tmux.SessionState, error) {
		calls = append(calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	probeSeq(&calls, eligibleReattachProbe())
	ptyio.ResizePaneToSizeFn = func(string, int, int, tmux.Options) error { return nil }
	ptyio.CapturePaneFullDataFn = func(string, tmux.Options) ([]byte, error) {
		return []byte("snap"), nil
	}
	ptyio.CapturePaneHistoryDataFn = func(string, tmux.Options) ([]byte, error) {
		return []byte("hist"), nil
	}
	capturePaneFn = func(string, tmux.Options) ([]byte, error) { return []byte("post"), nil }

	var attachSession string
	createRunAttachFn = func(_ *appPty.AgentManager, _ *data.Workspace, name string, _, _ uint16) (*appPty.Agent, error) {
		calls = append(calls, "attach")
		attachSession = name
		return &appPty.Agent{Session: name, Type: appPty.AgentType("run")}, nil
	}

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")

	msg := m.CreateRunViewerTab(ws, "amux-ws-run")()
	res, ok := msg.(ptyTabCreateResult)
	if !ok {
		t.Fatalf("got %T, want ptyTabCreateResult", msg)
	}
	if attachSession != "amux-ws-run" {
		t.Fatalf("attached session = %q, want amux-ws-run", attachSession)
	}
	if !res.DetachOnly {
		t.Fatal("run-viewer result must carry DetachOnly")
	}
	if res.Assistant != "run" {
		t.Fatalf("Assistant = %q, want run", res.Assistant)
	}
	if !res.Activate {
		t.Fatal("run-viewer tab should activate")
	}
}

func TestCreateRunViewerTab_DeadSessionToasts(t *testing.T) {
	restoreReattachSeams(t)
	sessionStateForFn = func(string, tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: false}, nil
	}
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	msg := m.CreateRunViewerTab(ws, "amux-ws-run")()
	toast, ok := msg.(messages.Toast)
	if !ok || toast.Level != messages.ToastInfo {
		t.Fatalf("got %T, want info toast", msg)
	}
}

func TestCreateRunViewerTab_NilArgs(t *testing.T) {
	m := newTestModel()
	if m.CreateRunViewerTab(nil, "s") != nil {
		t.Fatal("nil workspace should not produce a cmd")
	}
	if m.CreateRunViewerTab(newTestWorkspace("w", "/r"), "") != nil {
		t.Fatal("empty session should not produce a cmd")
	}
}

// The safety-critical contract: closing a DetachOnly tab never kills the
// borrowed session, while a normal tab still does.
func TestCloseTabAt_DetachOnlyNeverKills(t *testing.T) {
	restoreReattachSeams(t)
	var killed []string
	killSessionFn = func(name string, _ tmux.Options) error {
		killed = append(killed, name)
		return nil
	}

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")
	m.workspace = ws
	wsID := string(ws.ID())
	m.tabs.ByWorkspace[wsID] = []*Tab{
		{ID: "t1", Workspace: ws, SessionName: "borrowed-run", DetachOnly: true},
		{ID: "t2", Workspace: ws, SessionName: "owned-agent"},
	}
	m.tabs.ActiveByWorkspace[wsID] = 0

	runCmds := func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		if bm, ok := cmd().(tea.BatchMsg); ok {
			for _, c := range bm {
				c()
			}
		}
	}
	runCmds(m.closeTabAt(0))
	runCmds(m.closeTabAt(0))

	if len(killed) != 1 || killed[0] != "owned-agent" {
		t.Fatalf("killed = %v, want [owned-agent] — DetachOnly tab must not kill", killed)
	}
}
