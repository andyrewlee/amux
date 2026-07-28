package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

// installSnapshotSeams wires an eligible session: the pre-attach resize and
// full-pane capture both succeed, and the post-attach probe reports the single
// attaching client so the snapshot survives validation.
func installSnapshotSeams(t *testing.T, calls *[]string) {
	t.Helper()
	restoreReattachSeams(t)

	attached := eligibleReattachProbe()
	attached.ClientCount = 1

	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		*calls = append(*calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	// Quiet and unattached through the capture; the attaching client shows up in
	// the post-attach validation probe.
	probeSeq(calls, eligibleReattachProbe(), eligibleReattachProbe(), eligibleReattachProbe(), attached)
	resizePaneToSizeFn = func(string, int, int, tmux.Options) error {
		*calls = append(*calls, "resize")
		return nil
	}
	capturePaneFullDataFn = func(string, tmux.Options) ([]byte, error) {
		*calls = append(*calls, "snapshot")
		return []byte("frame"), nil
	}
	capturePaneHistoryDataFn = func(string, tmux.Options) ([]byte, error) {
		*calls = append(*calls, "scrollback")
		return []byte("fallback"), nil
	}
	capturePaneFn = func(string, tmux.Options) ([]byte, error) {
		*calls = append(*calls, "scrollback")
		return []byte("fallback"), nil
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

// assertSnapshotBeforeAttach asserts the authoritative snapshot was taken before
// the attach, at the probed pane size, with exactly one post-attach delta
// capture to reconcile anything that landed in between.
func assertSnapshotBeforeAttach(t *testing.T, result ptyTabReattachResult, calls []string) {
	t.Helper()
	if !result.CaptureFullPane {
		t.Fatal("expected authoritative pane capture on live reattach")
	}
	if got := string(result.ScrollbackCapture); got != "frame" {
		t.Fatalf("expected captured pane snapshot, got %q", got)
	}
	if got := string(result.PostAttachScrollbackCapture); got != "fallback" {
		t.Fatalf("expected post-attach history reconciliation capture, got %q", got)
	}
	if result.SnapshotCols != 123 || result.SnapshotRows != 45 {
		t.Fatalf("expected probed snapshot size 123x45, got %dx%d", result.SnapshotCols, result.SnapshotRows)
	}
	assertCallOrder(t, calls, "state", "probe", "resize", "snapshot", "attach", "scrollback")

	snapshotCount, scrollbackCount := 0, 0
	scrollbackIdx, attachIdx := -1, -1
	for i, call := range calls {
		switch call {
		case "snapshot":
			snapshotCount++
		case "scrollback":
			scrollbackCount++
			if scrollbackIdx == -1 {
				scrollbackIdx = i
			}
		case "attach":
			attachIdx = i
		}
	}
	if snapshotCount != 1 {
		t.Fatalf("expected a single resized snapshot capture, got %v", calls)
	}
	if scrollbackCount != 1 {
		t.Fatalf("expected a single post-attach delta capture, got %v", calls)
	}
	if scrollbackIdx < attachIdx {
		t.Fatalf("expected reconciliation capture after attach, got %v", calls)
	}
}

func TestReattachActiveTab_CapturesSnapshotBeforeAttach(t *testing.T) {
	var calls []string
	installSnapshotSeams(t, &calls)

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-reattach"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "session-1",
		Detached:    true,
	}
	m.workspace = ws
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0

	cmd := m.ReattachActiveTab()
	if cmd == nil {
		t.Fatal("expected reattach command")
	}
	result, ok := cmd().(ptyTabReattachResult)
	if !ok {
		t.Fatal("expected ptyTabReattachResult")
	}
	assertSnapshotBeforeAttach(t, result, calls)
}

func TestReattachToSession_CapturesSnapshotBeforeAttach(t *testing.T) {
	var calls []string
	installSnapshotSeams(t, &calls)

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")

	result, ok := m.reattachToSession(ws, TabID("tab-restore"), "codex", "session-restore", 1)().(ptyTabReattachResult)
	if !ok {
		t.Fatal("expected ptyTabReattachResult")
	}
	assertSnapshotBeforeAttach(t, result, calls)
}

func assertCallOrder(t *testing.T, calls []string, expected ...string) {
	t.Helper()
	last := -1
	for _, want := range expected {
		idx := -1
		for i := last + 1; i < len(calls); i++ {
			call := calls[i]
			if call == want {
				idx = i
				break
			}
		}
		if idx == -1 {
			t.Fatalf("expected call %q, got %v", want, calls)
		}
		last = idx
	}
}
