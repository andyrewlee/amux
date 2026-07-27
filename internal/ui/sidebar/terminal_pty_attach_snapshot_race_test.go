package sidebar

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

// installSidebarDemotionSeams wires a session that looks eligible right up to
// the moment the client attaches, then reports postAttach on every later probe.
// That is the shape of every race the pre-attach snapshot has to survive: the
// snapshot is taken in good faith and only the post-attach validation can tell
// it went stale.
func installSidebarDemotionSeams(t *testing.T, calls *[]string, postAttach tmux.SessionProbe) {
	t.Helper()
	restoreAttachSeams(t)

	ensureTmuxAvailableFn = func() error { return nil }
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		*calls = append(*calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	// Three probes bracket the capture (eligibility, post-resize, post-capture);
	// everything after them is the post-attach world.
	probeSeq(calls, eligibleAttachProbe(), eligibleAttachProbe(), eligibleAttachProbe(), postAttach)
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
	newPTYWithSizeFn = func(command, dir string, env []string, rows, cols uint16) (*pty.Terminal, error) {
		*calls = append(*calls, "attach")
		return &pty.Terminal{}, nil
	}
	verifyTerminalSessionTagsFn = func(sessionName string, tags tmux.SessionTags, opts tmux.Options) error {
		*calls = append(*calls, "verify")
		return nil
	}
}

// sidebarRestore is the subset of a create/reattach result the demotion
// assertions care about, so both message types share one assertion.
type sidebarRestore struct {
	captureFullPane bool
	scrollback      []byte
	postAttach      []byte
	captureCols     int
	captureRows     int
	snapshotCols    int
	snapshotRows    int
	checkSizes      bool
}

func assertSidebarSnapshotDemoted(t *testing.T, r sidebarRestore, calls []string) {
	t.Helper()
	if r.captureFullPane {
		t.Fatal("expected a stale pre-attach snapshot to be discarded")
	}
	if got := string(r.scrollback); got != "post history" {
		t.Fatalf("expected post-attach history recapture, got %q", got)
	}
	if len(r.postAttach) != 0 {
		t.Fatalf("expected no reconciliation delta after demotion, got %q", string(r.postAttach))
	}
	if r.checkSizes {
		if r.captureCols != 123 || r.captureRows != 45 {
			t.Fatalf("expected history-only capture size 123x45, got %dx%d", r.captureCols, r.captureRows)
		}
		if r.snapshotCols != 0 || r.snapshotRows != 0 {
			t.Fatalf("expected stale snapshot metadata to be cleared, got %dx%d", r.snapshotCols, r.snapshotRows)
		}
	}
	assertSidebarCallOrder(t, calls, "state", "probe", "resize", "snapshot", "attach", "probe", "scrollback")
}

// recreatedSidebarProbe is the same session name running a different
// incarnation: new creation stamp and new pane ID.
func recreatedSidebarProbe() tmux.SessionProbe {
	p := eligibleAttachProbe()
	p.ClientCount = 1
	p.CreatedAt = 999
	p.PaneID = "%new"
	return p
}

func newSidebarTestModel() (*TerminalModel, *data.Workspace) {
	m := NewTerminalModel()
	m.width = 20
	m.height = 5
	return m, data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
}

func TestCreateTerminalTab_DiscardsPreAttachSnapshotWhenSessionRecreated(t *testing.T) {
	var calls []string
	installSidebarDemotionSeams(t, &calls, recreatedSidebarProbe())

	m, ws := newSidebarTestModel()
	msg := m.createTerminalTab(ws)()
	created, ok := msg.(SidebarTerminalCreated)
	if !ok {
		t.Fatalf("expected SidebarTerminalCreated, got %T", msg)
	}
	assertSidebarSnapshotDemoted(t, sidebarRestore{
		captureFullPane: created.CaptureFullPane,
		scrollback:      created.ScrollbackCapture,
		postAttach:      created.PostAttachScrollbackCapture,
		captureCols:     created.CaptureCols,
		captureRows:     created.CaptureRows,
		snapshotCols:    created.SnapshotCols,
		snapshotRows:    created.SnapshotRows,
		checkSizes:      true,
	}, calls)
}

func TestAttachToSession_DiscardsPreAttachSnapshotWhenSessionRecreated(t *testing.T) {
	var calls []string
	installSidebarDemotionSeams(t, &calls, recreatedSidebarProbe())

	m, ws := newSidebarTestModel()
	msg := m.attachToSession(ws, TerminalTabID("term-tab-race"), "session-race", true, "reattach")()
	reattach, ok := msg.(SidebarTerminalReattachResult)
	if !ok {
		t.Fatalf("expected SidebarTerminalReattachResult, got %T", msg)
	}
	assertSidebarSnapshotDemoted(t, sidebarRestore{
		captureFullPane: reattach.CaptureFullPane,
		scrollback:      reattach.ScrollbackCapture,
		postAttach:      reattach.PostAttachScrollbackCapture,
		captureCols:     reattach.CaptureCols,
		captureRows:     reattach.CaptureRows,
		snapshotCols:    reattach.SnapshotCols,
		snapshotRows:    reattach.SnapshotRows,
		checkSizes:      true,
	}, calls)
}

func TestAttachToSession_DiscardsPreAttachSnapshotWhenSessionBecomesActive(t *testing.T) {
	var calls []string
	// The pane produced output after the snapshot was taken, so the snapshot no
	// longer describes the current screen.
	active := eligibleAttachProbe()
	active.ClientCount = 1
	active.LatestActivity = time.Now().Add(time.Hour).Unix()
	installSidebarDemotionSeams(t, &calls, active)

	m, ws := newSidebarTestModel()
	msg := m.attachToSession(ws, TerminalTabID("term-tab-active-race"), "session-race", true, "reattach")()
	reattach, ok := msg.(SidebarTerminalReattachResult)
	if !ok {
		t.Fatalf("expected SidebarTerminalReattachResult, got %T", msg)
	}
	assertSidebarSnapshotDemoted(t, sidebarRestore{
		captureFullPane: reattach.CaptureFullPane,
		scrollback:      reattach.ScrollbackCapture,
		postAttach:      reattach.PostAttachScrollbackCapture,
	}, calls)
}

func TestAttachToSession_DiscardsPreAttachSnapshotWhenSessionBecomesShared(t *testing.T) {
	var calls []string
	// Two clients: one is the client amux just attached, the other is something
	// else driving the session, so the snapshot cannot be trusted.
	shared := eligibleAttachProbe()
	shared.ClientCount = 2
	installSidebarDemotionSeams(t, &calls, shared)

	m, ws := newSidebarTestModel()
	msg := m.attachToSession(ws, TerminalTabID("term-tab-shared-race"), "session-race", true, "reattach")()
	reattach, ok := msg.(SidebarTerminalReattachResult)
	if !ok {
		t.Fatalf("expected SidebarTerminalReattachResult, got %T", msg)
	}
	assertSidebarSnapshotDemoted(t, sidebarRestore{
		captureFullPane: reattach.CaptureFullPane,
		scrollback:      reattach.ScrollbackCapture,
		postAttach:      reattach.PostAttachScrollbackCapture,
	}, calls)
}
