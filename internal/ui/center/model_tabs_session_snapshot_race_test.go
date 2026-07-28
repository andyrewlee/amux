package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

// installPostAttachDemotionSeams wires a session that looks eligible right up to
// the moment the client attaches, then reports postAttach on every later probe.
// That is the shape of every race the pre-attach snapshot has to survive: the
// snapshot is taken in good faith and only the post-attach validation can tell
// it went stale.
func installPostAttachDemotionSeams(t *testing.T, calls *[]string, postAttach tmux.SessionProbe) {
	t.Helper()
	restoreReattachSeams(t)

	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		*calls = append(*calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	// Three probes bracket the capture (eligibility, post-resize, post-capture);
	// everything after them is the post-attach world.
	probeSeq(calls, eligibleReattachProbe(), eligibleReattachProbe(), eligibleReattachProbe(), postAttach)
	resizePaneToSizeFn = func(string, int, int, tmux.Options) error {
		*calls = append(*calls, "resize")
		return nil
	}
	capturePaneFullDataFn = func(string, tmux.Options) ([]byte, error) {
		*calls = append(*calls, "snapshot")
		return []byte("stale frame"), nil
	}
	capturePaneHistoryDataFn = func(string, tmux.Options) ([]byte, error) {
		*calls = append(*calls, "scrollback")
		return []byte("post history"), nil
	}
	capturePaneFn = func(string, tmux.Options) ([]byte, error) {
		*calls = append(*calls, "scrollback")
		return []byte("post history"), nil
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

// assertSnapshotDemoted asserts the stale snapshot was thrown away in favor of
// a fresh history capture, with no leftover snapshot metadata and no
// reconciliation delta (there is nothing to reconcile against).
func assertSnapshotDemoted(t *testing.T, result ptyTabReattachResult, calls []string) {
	t.Helper()
	if result.CaptureFullPane {
		t.Fatal("expected a stale pre-attach snapshot to be discarded")
	}
	if got := string(result.ScrollbackCapture); got != "post history" {
		t.Fatalf("expected post-attach history recapture, got %q", got)
	}
	if len(result.PostAttachScrollbackCapture) != 0 {
		t.Fatalf("expected no reconciliation delta after demotion, got %q", string(result.PostAttachScrollbackCapture))
	}
	if result.Cols != 123 || result.Rows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", result.Cols, result.Rows)
	}
	if result.SnapshotCols != 0 || result.SnapshotRows != 0 {
		t.Fatalf("expected stale snapshot metadata to be cleared, got %dx%d", result.SnapshotCols, result.SnapshotRows)
	}
	assertCallOrder(t, calls, "state", "probe", "resize", "snapshot", "attach", "probe", "scrollback")
}

// reattachRaceTab builds a detached tab pointing at the race session.
func reattachRaceTab(m *Model, ws *data.Workspace, tabID TabID) {
	wsID := string(ws.ID())
	m.workspace = ws
	m.tabs.ByWorkspace[wsID] = []*Tab{{
		ID:          tabID,
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "session-race",
		Detached:    true,
	}}
	m.tabs.ActiveByWorkspace[wsID] = 0
}

// recreatedProbe is the same session name running a different incarnation: new
// creation stamp and new pane ID.
func recreatedProbe() tmux.SessionProbe {
	p := eligibleReattachProbe()
	p.ClientCount = 1
	p.CreatedAt = 999
	p.PaneID = "%new"
	return p
}

func TestReattachActiveTab_DiscardsPreAttachSnapshotWhenSessionRecreated(t *testing.T) {
	var calls []string
	installPostAttachDemotionSeams(t, &calls, recreatedProbe())

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")
	reattachRaceTab(m, ws, TabID("tab-reattach-race"))

	msg := m.ReattachActiveTab()()
	result, ok := msg.(ptyTabReattachResult)
	if !ok {
		t.Fatalf("expected ptyTabReattachResult, got %T", msg)
	}
	assertSnapshotDemoted(t, result, calls)
}

func TestReattachToSession_DiscardsPreAttachSnapshotWhenSessionRecreated(t *testing.T) {
	var calls []string
	installPostAttachDemotionSeams(t, &calls, recreatedProbe())

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")

	msg := m.reattachToSession(ws, TabID("tab-restore-race"), "codex", "session-race", 1)()
	result, ok := msg.(ptyTabReattachResult)
	if !ok {
		t.Fatalf("expected ptyTabReattachResult, got %T", msg)
	}
	assertSnapshotDemoted(t, result, calls)
}

func TestReattachActiveTab_DiscardsPreAttachSnapshotWhenSessionBecomesActive(t *testing.T) {
	var calls []string
	// The pane produced output after the snapshot was taken, so the snapshot no
	// longer describes the current screen.
	active := eligibleReattachProbe()
	active.ClientCount = 1
	active.LatestActivity = time.Now().Add(time.Hour).Unix()
	installPostAttachDemotionSeams(t, &calls, active)

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")
	reattachRaceTab(m, ws, TabID("tab-reattach-active-race"))

	msg := m.ReattachActiveTab()()
	result, ok := msg.(ptyTabReattachResult)
	if !ok {
		t.Fatalf("expected ptyTabReattachResult, got %T", msg)
	}
	assertSnapshotDemoted(t, result, calls)
}

func TestReattachActiveTab_DiscardsPreAttachSnapshotWhenSessionBecomesShared(t *testing.T) {
	var calls []string
	// Two clients: one is the client amux just attached, the other is something
	// else driving the session, so the snapshot cannot be trusted.
	shared := eligibleReattachProbe()
	shared.ClientCount = 2
	installPostAttachDemotionSeams(t, &calls, shared)

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")
	reattachRaceTab(m, ws, TabID("tab-reattach-shared-race"))

	msg := m.ReattachActiveTab()()
	result, ok := msg.(ptyTabReattachResult)
	if !ok {
		t.Fatalf("expected ptyTabReattachResult, got %T", msg)
	}
	assertSnapshotDemoted(t, result, calls)
}
