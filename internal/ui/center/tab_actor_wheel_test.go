package center

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// The tabEventWheel handler's decision table: a stale pane state is refreshed
// with one tmux probe (only when the event came through the actor queue —
// see the wheelAllowProbe cases), then the wheel is routed to the pane
// (cursor keys for a mouseless pane-alternate app, verified here at the byte
// level through a real PTY) or to local history (verified via ViewOffset).
// Byte delivery for the full Update path is pinned end-to-end by
// internal/e2e/alt_scroll_wheel_test.go.

// newWheelCaptureAgent spawns a raw-mode `cat > file` on a real PTY so the
// bytes the wheel path forwards to the hosted app are observable on disk.
// The slave is switched to raw mode first: a fresh PTY is in canonical mode,
// where escape-byte input is line-buffered and cat would never see it.
func newWheelCaptureAgent(t *testing.T) (*appPty.Agent, func() string) {
	t.Helper()
	dir := t.TempDir()
	outPath := filepath.Join(dir, "sent.txt")
	term, err := appPty.NewWithSize("stty raw -echo; cat > "+outPath, dir, nil, 24, 80)
	if err != nil {
		t.Fatalf("expected test PTY terminal: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })
	agent := &appPty.Agent{Terminal: term, Session: "sess-wheel-test"}
	return agent, func() string {
		data, err := os.ReadFile(outPath)
		if err != nil {
			return ""
		}
		return string(data)
	}
}

// overrideWheelProbePoint replaces ptyio.ProbeSessionFn for the test and
// counts calls. Tests overriding the seam must not use t.Parallel.
func overrideWheelProbe(t *testing.T, probe tmux.SessionProbe, probeErr error) *int {
	t.Helper()
	calls := 0
	orig := ptyio.ProbeSessionFn
	ptyio.ProbeSessionFn = func(string, tmux.Options) (tmux.SessionProbe, error) {
		calls++
		return probe, probeErr
	}
	t.Cleanup(func() { ptyio.ProbeSessionFn = orig })
	return &calls
}

func altPaneProbe() tmux.SessionProbe {
	return tmux.SessionProbe{
		Exists: true,
		PaneMeta: tmux.PaneSnapshotMeta{
			ModeState: tmux.PaneModeState{HasState: true, AltScreen: true},
		},
	}
}

func primaryPaneProbe() tmux.SessionProbe {
	return tmux.SessionProbe{
		Exists: true,
		PaneMeta: tmux.PaneSnapshotMeta{
			ModeState: tmux.PaneModeState{HasState: true, AltScreen: false},
		},
	}
}

func wheelEvent(tab *Tab) tabEvent {
	return tabEvent{
		tab:             tab,
		tabID:           tab.ID,
		workspaceID:     "ws-wheel",
		kind:            tabEventWheel,
		termX:           5,
		termY:           5,
		inBounds:        true,
		mouseButton:     tea.MouseWheelUp,
		delta:           3,
		wheelAllowProbe: true,
	}
}

func waitForSent(t *testing.T, read func() string, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(read(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("agent never received %q; sent so far: %q", want, read())
}

func TestHandleWheelProbesThenForwardsCursorKeysToAltPane(t *testing.T) {
	agent, read := newWheelCaptureAgent(t)
	calls := overrideWheelProbe(t, altPaneProbe(), nil)

	m, tab := setupSelectionModel(t)
	tab.mu.Lock()
	tab.Agent = agent
	tab.SessionName = "sess-wheel-test"
	tab.paneStateProbedAt = time.Time{} // stale: forces the probe
	tab.mu.Unlock()

	m.handleWheel(wheelEvent(tab))

	if *calls != 1 {
		t.Fatalf("expected exactly one pane-state probe, got %d", *calls)
	}
	waitForSent(t, read, "\x1b[A\x1b[A\x1b[A")
	tab.mu.Lock()
	alt := tab.Terminal.PaneAlternateScreen
	stale := tab.paneStateIsStaleLocked()
	offset, _ := tab.Terminal.GetScrollInfo()
	tab.mu.Unlock()
	if !alt {
		t.Fatal("expected probe to set PaneAlternateScreen")
	}
	if stale {
		t.Fatal("expected a successful probe to stamp the cache fresh")
	}
	if offset != 0 {
		t.Fatalf("alt pane must not scroll local history, got offset=%d", offset)
	}
}

func TestHandleWheelForwardsSS3WhenApplicationCursorKeys(t *testing.T) {
	agent, read := newWheelCaptureAgent(t)
	overrideWheelProbe(t, altPaneProbe(), nil)

	m, tab := setupSelectionModel(t)
	tab.mu.Lock()
	tab.Agent = agent
	tab.SessionName = "sess-wheel-test"
	tab.paneStateProbedAt = time.Time{}
	tab.Terminal.Write([]byte("\x1b[?1h")) // DECCKM: arrows go SS3
	tab.mu.Unlock()

	m.handleWheel(wheelEvent(tab))

	waitForSent(t, read, "\x1bOA\x1bOA\x1bOA")
}

func TestHandleWheelProbesThenScrollsLocalHistoryOnPrimaryPane(t *testing.T) {
	agent, read := newWheelCaptureAgent(t)
	calls := overrideWheelProbe(t, primaryPaneProbe(), nil)

	m, tab := setupSelectionModel(t)
	tab.mu.Lock()
	tab.Agent = agent
	tab.SessionName = "sess-wheel-test"
	tab.paneStateProbedAt = time.Time{}
	tab.Terminal.Write([]byte(strings.Repeat("line\n", 40)))
	tab.mu.Unlock()

	m.handleWheel(wheelEvent(tab))

	if *calls != 1 {
		t.Fatalf("expected exactly one pane-state probe, got %d", *calls)
	}
	tab.mu.Lock()
	offset, _ := tab.Terminal.GetScrollInfo()
	alt := tab.Terminal.PaneAlternateScreen
	tab.mu.Unlock()
	if alt {
		t.Fatal("primary-screen probe must clear the pane-alt flag")
	}
	if offset == 0 {
		t.Fatal("expected wheel to scroll primary-screen scrollback after probe")
	}
	if sent := read(); sent != "" {
		t.Fatalf("primary pane must receive no pane input, got %q", sent)
	}
}

func TestHandleWheelProbeFailureRoutesOnCacheAndStaysStale(t *testing.T) {
	calls := overrideWheelProbe(t, tmux.SessionProbe{}, errors.New("tmux busy"))

	m, tab := setupSelectionModel(t)
	tab.mu.Lock()
	tab.SessionName = "sess-wheel-test"
	tab.paneStateProbedAt = time.Time{}
	tab.Terminal.Write([]byte(strings.Repeat("line\n", 40)))
	tab.mu.Unlock()

	m.handleWheel(wheelEvent(tab))

	if *calls != 1 {
		t.Fatalf("expected one probe attempt, got %d", *calls)
	}
	tab.mu.Lock()
	stale := tab.paneStateIsStaleLocked()
	offset, _ := tab.Terminal.GetScrollInfo()
	tab.mu.Unlock()
	if !stale {
		t.Fatal("a failed probe must not stamp the cache fresh")
	}
	// Cached flag says primary, so the wheel still routes to local history;
	// the next interaction re-probes because the cache stayed stale.
	if offset == 0 {
		t.Fatal("expected cached-primary routing to scroll local history")
	}
}

func TestHandleWheelFallbackNeverProbesAndRoutesOnCache(t *testing.T) {
	agent, read := newWheelCaptureAgent(t)
	calls := overrideWheelProbe(t, altPaneProbe(), nil)

	m, tab := setupSelectionModel(t)
	tab.mu.Lock()
	tab.Agent = agent
	tab.SessionName = "sess-wheel-test"
	tab.paneStateProbedAt = time.Time{} // stale — but the fallback must not probe
	tab.Terminal.SetPaneAlternateScreen(true)
	tab.mu.Unlock()

	// The synchronous fallback path (actor queue full / not ready) clears
	// wheelAllowProbe: it must not block the UI goroutine on tmux.
	ev := wheelEvent(tab)
	ev.wheelAllowProbe = false
	m.handleWheel(ev)

	if *calls != 0 {
		t.Fatalf("fallback must not probe tmux, got %d calls", *calls)
	}
	// The cached alt flag still routes the wheel to the pane...
	waitForSent(t, read, "\x1b[A\x1b[A\x1b[A")
	tab.mu.Lock()
	offset, _ := tab.Terminal.GetScrollInfo()
	stale := tab.paneStateIsStaleLocked()
	tab.mu.Unlock()
	if offset != 0 {
		t.Fatalf("cached alt flag must keep the wheel off local history, got offset=%d", offset)
	}
	if !stale {
		t.Fatal("fallback routing must leave the stale cache untouched")
	}
}
