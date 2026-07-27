package center

import (
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestReattachActiveTab_SnapshotIneligibleFallsBackWithoutResize(t *testing.T) {
	var calls []string
	// A pane with no VT mode metadata cannot anchor an authoritative snapshot,
	// so the bootstrap must fall back to history without touching the pane.
	ineligible := eligibleReattachProbe()
	ineligible.PaneMeta.ModeState = tmux.PaneModeState{}
	installHistoryFallbackSeams(t, &calls, ineligible)

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-snapshot-ineligible"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "session-snapshot-ineligible",
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

func TestReattachActiveTab_AttachFailureRollsBackBootstrapResize(t *testing.T) {
	var calls []string
	restoreReattachSeams(t)

	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		calls = append(calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	// Still unattached and unchanged at the rollback probe, so the rollback is
	// safe to perform.
	probeSeq(&calls, eligibleReattachProbe())
	resizePaneToSizeFn = func(string, int, int, tmux.Options) error {
		calls = append(calls, "resize")
		return nil
	}
	capturePaneFullDataFn = func(string, tmux.Options) ([]byte, error) {
		calls = append(calls, "snapshot")
		return []byte("resized"), nil
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
		return nil, errors.New("attach failed")
	}

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-snapshot-rollback"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "session-snapshot-rollback",
		Detached:    true,
	}
	m.workspace = ws
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0

	msg := m.ReattachActiveTab()()
	failed, ok := msg.(ptyTabReattachFailed)
	if !ok {
		t.Fatalf("expected ptyTabReattachFailed, got %T", msg)
	}
	if failed.Err == nil || failed.Err.Error() != "attach failed" {
		t.Fatalf("expected attach failure, got %+v", failed)
	}
	// The trailing resize is the rollback: a failed attach must not leave the
	// pane at the size the aborted bootstrap set.
	assertCallOrder(t, calls, "state", "probe", "resize", "snapshot", "attach", "resize")
}
