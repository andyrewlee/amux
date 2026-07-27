package tmux

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// probeLine renders one list-panes row in sessionProbeFormat's field order, so
// the parser tests read as data rather than as tab-counting.
type probeLine struct {
	sessionCreated  string
	sessionAttached string
	windowActivity  string
	windowActive    string
	windowID        string
	paneID          string
	paneActive      string
	paneDead        string
	paneWidth       string
	paneHeight      string
	cursorX         string
	cursorY         string
	alternateOn     string
	altSavedX       string
	altSavedY       string
	cursorFlag      string
	originFlag      string
	scrollUpper     string
	scrollLower     string
}

// livePane is a plausible single-pane row; tests override the fields they care
// about.
func livePane() probeLine {
	return probeLine{
		sessionCreated: "1700000000", sessionAttached: "0",
		windowActivity: "1700000500", windowActive: "1", windowID: "@0",
		paneID: "%1", paneActive: "1", paneDead: "0",
		paneWidth: "80", paneHeight: "24",
		cursorX: "3", cursorY: "7",
		alternateOn: "0", altSavedX: "4294967295", altSavedY: "4294967295",
		cursorFlag: "1", originFlag: "0",
		scrollUpper: "0", scrollLower: "23",
	}
}

func (p probeLine) String() string {
	return strings.Join([]string{
		p.sessionCreated, p.sessionAttached, p.windowActivity, p.windowActive,
		p.windowID, p.paneID, p.paneActive, p.paneDead, p.paneWidth, p.paneHeight,
		p.cursorX, p.cursorY, p.alternateOn, p.altSavedX, p.altSavedY,
		p.cursorFlag, p.originFlag, p.scrollUpper, p.scrollLower,
	}, "\t")
}

func probeLines(rows ...probeLine) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.String())
	}
	return out
}

func TestParseSessionProbe_SinglePane(t *testing.T) {
	probe := parseSessionProbe(probeLines(livePane()))

	if !probe.Exists || !probe.HasLivePane {
		t.Fatalf("a live pane must report Exists and HasLivePane, got %+v", probe)
	}
	if probe.CreatedAt != 1700000000 {
		t.Fatalf("CreatedAt = %d, want 1700000000", probe.CreatedAt)
	}
	if probe.ClientCount != 0 || probe.HasClients() {
		t.Fatalf("session_attached=0 must mean no clients, got %d", probe.ClientCount)
	}
	if probe.PaneID != "%1" {
		t.Fatalf("PaneID = %q, want %q", probe.PaneID, "%1")
	}
	if probe.PaneCols != 80 || probe.PaneRows != 24 || !probe.HasPaneSize {
		t.Fatalf("pane size = (%d, %d, %v), want (80, 24, true)", probe.PaneCols, probe.PaneRows, probe.HasPaneSize)
	}
	if !probe.SinglePaneWindow {
		t.Fatal("a window with one pane must report SinglePaneWindow")
	}
	if probe.LatestActivity != 1700000500 {
		t.Fatalf("LatestActivity = %d, want 1700000500", probe.LatestActivity)
	}
	if probe.PaneMeta.CursorX != 3 || probe.PaneMeta.CursorY != 7 || !probe.PaneMeta.HasCursor {
		t.Fatalf("cursor = (%d, %d, %v), want (3, 7, true)", probe.PaneMeta.CursorX, probe.PaneMeta.CursorY, probe.PaneMeta.HasCursor)
	}
	if !probe.PaneMeta.ModeState.HasState {
		t.Fatal("expected mode state to be parsed from the probe row")
	}
	if !probe.SnapshotEligible() {
		t.Fatalf("a quiet single live pane with full metadata must be snapshot-eligible, got %+v", probe)
	}
}

func TestParseSessionProbe_PrefersActivePaneOfActiveWindow(t *testing.T) {
	// The pane a reattaching client renders is the active pane of the active
	// window, no matter what order tmux lists them in.
	inactiveWindow := livePane()
	inactiveWindow.windowActive = "0"
	inactiveWindow.windowID = "@1"
	inactiveWindow.paneID = "%9"

	activeButNotFirst := livePane()
	activeButNotFirst.paneID = "%2"

	notActivePane := livePane()
	notActivePane.paneID = "%3"
	notActivePane.paneActive = "0"

	probe := parseSessionProbe(probeLines(inactiveWindow, notActivePane, activeButNotFirst))
	if probe.PaneID != "%2" {
		t.Fatalf("PaneID = %q, want the active pane of the active window (%%2)", probe.PaneID)
	}
	// Two panes share window @0, so the pre-attach resize would disturb a sibling.
	if probe.SinglePaneWindow {
		t.Fatal("a window with two panes must not report SinglePaneWindow")
	}
	if probe.SnapshotEligible() {
		t.Fatal("a split window must not be snapshot-eligible")
	}
}

// TestParseSessionProbe_NeverChoosesPaneOutsideActiveWindow pins the scope
// difference between the probe's `list-panes -s` (every window) and
// sessionPaneID's `list-panes -t <session>` (active window only).
//
// Choosing a pane from an inactive window would be worse than choosing none:
// ResizePaneToSize targets the session, which tmux resolves to the active
// window, so the pre-attach resize would land on one window while the snapshot
// described another — at the other window's dimensions. The bootstrap's guards
// cannot catch that, because the pane being described never changes across
// them.
func TestParseSessionProbe_NeverChoosesPaneOutsideActiveWindow(t *testing.T) {
	// The active window's only pane is dead; another window has a live one.
	deadActiveWindow := livePane()
	deadActiveWindow.windowID = "@1"
	deadActiveWindow.paneID = "%1"
	deadActiveWindow.paneDead = "1"

	liveOtherWindow := livePane()
	liveOtherWindow.windowActive = "0"
	liveOtherWindow.windowID = "@0"
	liveOtherWindow.paneID = "%0"

	probe := parseSessionProbe(probeLines(liveOtherWindow, deadActiveWindow))
	if probe.PaneID != "" {
		t.Fatalf("PaneID = %q, want empty: a pane outside the active window must never be chosen", probe.PaneID)
	}
	if probe.SnapshotEligible() {
		t.Fatal("expected no snapshot when the active window has no live pane")
	}
	// Liveness is scoped the same way, so it agrees with SessionStateFor.
	if probe.HasLivePane {
		t.Fatal("a live pane in a non-active window must not report HasLivePane")
	}
	// The session itself is still reported as existing.
	if !probe.Exists {
		t.Fatal("expected the session to still be reported as existing")
	}
}

func TestParseSessionProbe_SkipsDeadPanes(t *testing.T) {
	dead := livePane()
	dead.paneID = "%1"
	dead.paneDead = "1"

	alive := livePane()
	alive.paneID = "%2"
	alive.paneActive = "0"

	probe := parseSessionProbe(probeLines(dead, alive))
	if probe.PaneID != "%2" {
		t.Fatalf("PaneID = %q, want the live pane (%%2) rather than the dead active one", probe.PaneID)
	}
	if !probe.HasLivePane {
		t.Fatal("one live pane among dead ones must report HasLivePane")
	}
}

func TestParseSessionProbe_AllPanesDead(t *testing.T) {
	dead := livePane()
	dead.paneDead = "1"

	probe := parseSessionProbe(probeLines(dead))
	if !probe.Exists {
		t.Fatal("a session with only dead panes still exists")
	}
	if probe.HasLivePane {
		t.Fatal("all panes dead must report HasLivePane=false")
	}
	if probe.PaneID != "" {
		t.Fatalf("PaneID = %q, want empty when every pane is dead", probe.PaneID)
	}
	if probe.SnapshotEligible() {
		t.Fatal("a session with no live pane must not be snapshot-eligible")
	}
}

func TestParseSessionProbe_MaximisesActivityAcrossWindows(t *testing.T) {
	// Activity is per-window; the session is only quiet if every window is.
	quiet := livePane()
	quiet.windowActivity = "1700000100"

	noisy := livePane()
	noisy.windowActive = "0"
	noisy.windowID = "@1"
	noisy.paneID = "%9"
	noisy.windowActivity = "1700009999"

	probe := parseSessionProbe(probeLines(quiet, noisy))
	if probe.LatestActivity != 1700009999 {
		t.Fatalf("LatestActivity = %d, want the max across windows (1700009999)", probe.LatestActivity)
	}
}

func TestParseSessionProbe_CountsAttachedClients(t *testing.T) {
	attached := livePane()
	attached.sessionAttached = "2"

	probe := parseSessionProbe(probeLines(attached))
	if probe.ClientCount != 2 || !probe.HasClients() {
		t.Fatalf("ClientCount = %d, want 2", probe.ClientCount)
	}
}

func TestParseSessionProbe_EmptyOutputIsZeroProbe(t *testing.T) {
	probe := parseSessionProbe(nil)
	if probe.Exists || probe.HasLivePane || probe.PaneID != "" {
		t.Fatalf("no output must yield the zero probe, got %+v", probe)
	}
	if probe.SnapshotEligible() {
		t.Fatal("the zero probe must not be snapshot-eligible")
	}
}

func TestParseSessionProbe_SkipsMalformedLines(t *testing.T) {
	// A short line is dropped rather than indexed into.
	probe := parseSessionProbe([]string{"nonsense", livePane().String()})
	if probe.PaneID != "%1" {
		t.Fatalf("PaneID = %q, want the well-formed row to still be used", probe.PaneID)
	}
}

func TestParseSessionProbe_MissingSizeIsNotEligible(t *testing.T) {
	noSize := livePane()
	noSize.paneWidth = "0"

	probe := parseSessionProbe(probeLines(noSize))
	if probe.HasPaneSize {
		t.Fatal("a zero pane width must not report HasPaneSize")
	}
	if probe.SnapshotEligible() {
		t.Fatal("a pane with no usable size must not be snapshot-eligible")
	}
}

func TestSessionProbe_SameGeneration(t *testing.T) {
	base := SessionProbe{CreatedAt: 100, PaneID: "%1"}

	if !base.SameGeneration(SessionProbe{CreatedAt: 100, PaneID: "%1"}) {
		t.Fatal("identical creation stamp and pane ID must be the same generation")
	}
	// A session killed and recreated under the same name gets both a new
	// creation stamp and a new pane ID; either alone is enough to detect it.
	if base.SameGeneration(SessionProbe{CreatedAt: 200, PaneID: "%1"}) {
		t.Fatal("a new creation stamp must be a different generation")
	}
	if base.SameGeneration(SessionProbe{CreatedAt: 100, PaneID: "%2"}) {
		t.Fatal("a new pane ID must be a different generation")
	}
	// An unusable baseline can never match, so a capture is never trusted after
	// a probe that failed to identify the session.
	if (SessionProbe{}).SameGeneration(SessionProbe{}) {
		t.Fatal("the zero probe must not match any generation, including itself")
	}
}

func TestSessionProbe_ActiveWithin(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	quiet := SessionProbe{LatestActivity: now.Add(-30 * time.Second).Unix()}
	recent := SessionProbe{LatestActivity: now.Add(-1 * time.Second).Unix()}

	if quiet.ActiveWithin(2*time.Second, now) {
		t.Fatal("activity 30s ago must be outside a 2s quiet window")
	}
	if !recent.ActiveWithin(2*time.Second, now) {
		t.Fatal("activity 1s ago must be inside a 2s quiet window")
	}
	if (SessionProbe{}).ActiveWithin(2*time.Second, now) {
		t.Fatal("no reported activity must never count as active")
	}
}

func TestProbeSession_EmptyNameSkipsExec(t *testing.T) {
	orig := runTmuxCmd
	runTmuxCmd = func(cmd *exec.Cmd) ([]byte, error) {
		t.Errorf("expected no tmux invocation for an empty session name, got %v", cmd.Args)
		return nil, errors.New("unexpected invocation")
	}
	t.Cleanup(func() { runTmuxCmd = orig })

	probe, err := ProbeSession("", testOpts())
	if err != nil || probe.Exists {
		t.Fatalf("empty session name must yield the zero probe, got %+v err=%v", probe, err)
	}
}

func TestProbeSession_MissingSessionIsZeroProbeNotError(t *testing.T) {
	skipIfNoTmux(t)
	orig := runTmuxCmd
	runTmuxCmd = func(*exec.Cmd) ([]byte, error) { return nil, exitCode1Err(t) }
	t.Cleanup(func() { runTmuxCmd = orig })

	probe, err := ProbeSession("amux-gone", testOpts())
	if err != nil {
		t.Fatalf("exit 1 means the session is gone, not an error; got %v", err)
	}
	if probe.Exists {
		t.Fatalf("a missing session must yield the zero probe, got %+v", probe)
	}
}

func TestProbeSession_ErrorPropagates(t *testing.T) {
	skipIfNoTmux(t)
	want := errors.New("no server running on /tmp/x")
	orig := runTmuxCmd
	runTmuxCmd = func(*exec.Cmd) ([]byte, error) { return nil, want }
	t.Cleanup(func() { runTmuxCmd = orig })

	if _, err := ProbeSession("amux-x", testOpts()); !errors.Is(err, want) {
		t.Fatalf("a non-exit-1 error must propagate, got %v", err)
	}
}
