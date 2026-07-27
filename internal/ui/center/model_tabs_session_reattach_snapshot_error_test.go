package center

import (
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestReattachActiveTab_SnapshotCommandErrorFallsBackToHistoryOnly(t *testing.T) {
	var calls []string
	restoreReattachSeams(t)

	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		calls = append(calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	probeSeq(&calls, eligibleReattachProbe())
	resizePaneToSizeFn = func(string, int, int, tmux.Options) error {
		calls = append(calls, "resize")
		return nil
	}
	capturePaneFullDataFn = func(string, tmux.Options) ([]byte, error) {
		calls = append(calls, "snapshot")
		return nil, errors.New("snapshot command failed")
	}
	capturePaneHistoryDataFn = func(string, tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("history"), nil
	}
	capturePaneFn = func(string, tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
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
		calls = append(calls, "attach")
		return &appPty.Agent{Session: sessionName}, nil
	}

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-snapshot-error"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "session-snapshot-error",
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
	if result.CaptureFullPane {
		t.Fatal("expected snapshot error to disable authoritative full-pane restore")
	}
	if got := string(result.ScrollbackCapture); got != "history" {
		t.Fatalf("expected history-only fallback, got %q", got)
	}
	if result.Cols != 123 || result.Rows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", result.Cols, result.Rows)
	}
	// The second resize is the rollback: a failed capture must not leave the pane
	// at the size the aborted bootstrap set.
	assertCallOrder(t, calls, "state", "probe", "resize", "snapshot", "resize", "attach", "scrollback")
}
