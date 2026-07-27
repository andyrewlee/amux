package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

// installHistoryFallbackSeams wires the seams shared by the history-fallback
// tests below: the session exists and is live, the probe reports whatever
// ineligibility the caller set up, and any resize or full-pane capture is a test
// failure because the fallback path must reach neither.
func installHistoryFallbackSeams(t *testing.T, calls *[]string, probe tmux.SessionProbe) {
	t.Helper()
	restoreReattachSeams(t)

	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		*calls = append(*calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	probeSeq(calls, probe)
	resizePaneToSizeFn = func(string, int, int, tmux.Options) error {
		*calls = append(*calls, "resize")
		return nil
	}
	capturePaneFullDataFn = func(string, tmux.Options) ([]byte, error) {
		*calls = append(*calls, "snapshot")
		return []byte("should not use"), nil
	}
	capturePaneHistoryDataFn = func(string, tmux.Options) ([]byte, error) {
		*calls = append(*calls, "scrollback")
		return []byte("history"), nil
	}
	capturePaneFn = func(string, tmux.Options) ([]byte, error) {
		*calls = append(*calls, "scrollback")
		return []byte("history"), nil
	}
	createAgentWithTagsFn = func(
		manager *appPty.AgentManager,
		ws *data.Workspace,
		agentType appPty.AgentType,
		sessionName string,
		rows, cols uint16,
		tags tmux.SessionTags,
	) (*appPty.Agent, error) {
		*calls = append(*calls, "attach")
		return &appPty.Agent{Session: sessionName}, nil
	}
}

// assertHistoryFallback asserts the reattach fell back to replaying history at
// the live pane size, without ever resizing or snapshotting the pane.
func assertHistoryFallback(t *testing.T, result ptyTabReattachResult, calls []string) {
	t.Helper()
	if result.CaptureFullPane {
		t.Fatal("expected an ineligible session to skip the pre-attach full-pane snapshot")
	}
	if got := string(result.ScrollbackCapture); got != "history" {
		t.Fatalf("expected history-only fallback, got %q", got)
	}
	if result.Cols != 123 || result.Rows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", result.Cols, result.Rows)
	}
	assertCallOrder(t, calls, "state", "probe", "attach", "scrollback")
	for _, call := range calls {
		if call == "snapshot" || call == "resize" {
			t.Fatalf("expected an ineligible session to avoid pre-attach resize/snapshot, got %v", calls)
		}
	}
}

func TestReattachActiveTab_BusySessionCapturesHistoryAfterAttach(t *testing.T) {
	var calls []string
	busy := eligibleReattachProbe()
	busy.LatestActivity = time.Now().Unix()
	installHistoryFallbackSeams(t, &calls, busy)

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-busy-reattach"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "session-busy",
		Detached:    true,
	}
	m.workspace = ws
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0

	msg := m.ReattachActiveTab()()
	result, ok := msg.(ptyTabReattachResult)
	if !ok {
		t.Fatalf("expected ptyTabReattachResult, got %T", msg)
	}
	assertHistoryFallback(t, result, calls)
}

func TestReattachActiveTab_SharedClientCapturesHistoryAfterAttach(t *testing.T) {
	var calls []string
	shared := eligibleReattachProbe()
	shared.ClientCount = 1
	installHistoryFallbackSeams(t, &calls, shared)

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-shared-reattach"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "session-shared",
		Detached:    true,
	}
	m.workspace = ws
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0

	msg := m.ReattachActiveTab()()
	result, ok := msg.(ptyTabReattachResult)
	if !ok {
		t.Fatalf("expected ptyTabReattachResult, got %T", msg)
	}
	assertHistoryFallback(t, result, calls)
}

func TestReattachToSession_BusySessionCapturesHistoryAfterAttach(t *testing.T) {
	var calls []string
	busy := eligibleReattachProbe()
	busy.LatestActivity = time.Now().Unix()
	installHistoryFallbackSeams(t, &calls, busy)

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")

	msg := m.reattachToSession(ws, TabID("tab-restore-busy"), "codex", "session-busy")()
	result, ok := msg.(ptyTabReattachResult)
	if !ok {
		t.Fatalf("expected ptyTabReattachResult, got %T", msg)
	}
	assertHistoryFallback(t, result, calls)
}
