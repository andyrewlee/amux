package tmux

import (
	"bytes"
	"testing"
	"time"
)

// These exercise ProbeSession and the pane-targeted capture helpers against a
// real tmux server. They are the counterweight to the pure parser tests in
// probe_test.go: those feed parseSessionProbe hand-written rows in
// sessionProbeFormat's order and so can never notice if that format string is
// reordered, while these compare the probe's output against the dedicated
// single-purpose helpers that request the same fields through their own format
// strings. Shared fixtures (probeLine, livePane) live in probe_test.go.

// TestCapturePaneData_AgainstLiveTmux exercises the real bodies of the two
// pane-targeted capture helpers. Every other test injects fakes for them, so
// without this their tmux invocations never actually run.
func TestCapturePaneData_AgainstLiveTmux(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)
	// Emit more lines than the pane is tall so some are pushed into scrollback,
	// which is the only part a history capture is allowed to return.
	createSession(t, opts, "cap", `for i in $(seq 1 200); do echo "line-$i"; done; sleep 60`)

	probe := waitForScrollback(t, opts, "cap")

	history, err := CapturePaneHistoryData(probe.PaneID, opts)
	if err != nil {
		t.Fatalf("CapturePaneHistoryData: %v", err)
	}
	full, err := CapturePaneFullData(probe.PaneID, opts)
	if err != nil {
		t.Fatalf("CapturePaneFullData: %v", err)
	}

	// The full capture is scrollback plus the visible screen, so it must be the
	// strictly larger of the two. This is what makes them different calls.
	if len(full) <= len(history) {
		t.Fatalf("full capture (%d bytes) must exceed history-only (%d bytes)", len(full), len(history))
	}
	if !bytes.Contains(full, []byte("line-200")) {
		t.Error("expected the full capture to include the visible screen's last line")
	}
	if bytes.Contains(history, []byte("line-200")) {
		t.Error("expected the history capture to exclude the visible screen (-E -1)")
	}
	// -N pads each row with trailing spaces, so the first line reads "line-1 ".
	// The trailing space also disambiguates it from line-10 and line-100.
	if !bytes.Contains(history, []byte("line-1 ")) {
		t.Error("expected the history capture to reach the start of scrollback (-S -)")
	}

	// CapturePane is the same capture reached by session name instead of pane
	// ID; identical bytes is what lets the pre- and post-attach halves of a
	// restore be compared against each other.
	viaSession, err := CapturePane("cap", opts)
	if err != nil {
		t.Fatalf("CapturePane: %v", err)
	}
	if !bytes.Equal(viaSession, history) {
		t.Errorf("CapturePane and CapturePaneHistoryData disagree: %d vs %d bytes",
			len(viaSession), len(history))
	}
}

func TestCapturePaneData_EmptyPaneID(t *testing.T) {
	// The two helpers deliberately differ here: a history capture with nothing to
	// target is an ordinary empty result, but a full-pane capture is only ever
	// requested for a pane a probe already resolved, so an empty ID means the
	// caller's own bookkeeping is wrong.
	if data, err := CapturePaneHistoryData("", testOpts()); data != nil || err != nil {
		t.Errorf("CapturePaneHistoryData(\"\") = (%q, %v), want (nil, nil)", data, err)
	}
	if _, err := CapturePaneFullData("", testOpts()); err == nil {
		t.Error("CapturePaneFullData(\"\") must report an error rather than an empty capture")
	}
}

// waitForScrollback waits until the session's pane has pushed lines into
// scrollback, so the capture tests read a settled pane rather than racing the
// shell's output.
func waitForScrollback(t *testing.T, opts Options, session string) SessionProbe {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		probe, err := ProbeSession(session, opts)
		if err == nil && probe.PaneID != "" {
			if data, err := CapturePaneHistoryData(probe.PaneID, opts); err == nil &&
				bytes.Contains(data, []byte("line-1 ")) {
				return probe
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session %q never produced scrollback", session)
	return SessionProbe{}
}

// TestProbeSession_MultiWindowMatchesSessionPaneID is the live-tmux half of
// TestParseSessionProbe_NeverChoosesPaneOutsideActiveWindow. The probe reads
// `list-panes -s` (every window) while sessionPaneID reads
// `list-panes -t <session>` (active window only), so only a real server with a
// second window proves the two scopes still agree.
func TestProbeSession_MultiWindowMatchesSessionPaneID(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)
	createSession(t, opts, "multi", "sleep 60")
	addWindow(t, opts, "multi", "sleep 60")

	probe, err := ProbeSession("multi", opts)
	if err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}
	paneID, err := SessionPaneID("multi", opts)
	if err != nil {
		t.Fatalf("SessionPaneID: %v", err)
	}
	if probe.PaneID != paneID {
		t.Fatalf("PaneID: probe=%q helper=%q — the probe must not reach past the active window",
			probe.PaneID, paneID)
	}
	// A second window must not make the chosen pane's own window look shared.
	cols, rows, hasSize, err := SessionPaneSize("multi", opts)
	if err != nil || cols != probe.PaneCols || rows != probe.PaneRows || hasSize != probe.HasPaneSize {
		t.Fatalf("PaneSize: probe=(%d,%d,%v) helper=(%d,%d,%v) (err %v)",
			probe.PaneCols, probe.PaneRows, probe.HasPaneSize, cols, rows, hasSize, err)
	}
	_, _, supported, err := SessionPaneSnapshotInfo("multi", opts)
	if err != nil || supported != probe.SnapshotEligible() {
		t.Fatalf("SnapshotEligible: probe=%v helper=%v (err %v)", probe.SnapshotEligible(), supported, err)
	}
	state, err := SessionStateFor("multi", opts)
	if err != nil || state.HasLivePane != probe.HasLivePane {
		t.Fatalf("HasLivePane: probe=%v helper=%v (err %v)", probe.HasLivePane, state.HasLivePane, err)
	}
}

// TestProbeSession_MatchesDedicatedHelpers pins the probe against a real tmux
// server. The probe replaced a ladder of single-purpose reads in the reattach
// bootstrap; this asserts it reports exactly what those reads still report, so
// the round-trip savings never come at the cost of a different answer.
func TestProbeSession_MatchesDedicatedHelpers(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)
	// Emit output a second after the session is created. tmux reports both
	// session_created and window_activity as whole-second unix stamps, and in a
	// freshly made session they are the same second — which would make a
	// transposition between those two format fields invisible. Waiting for them
	// to diverge is what gives the CreatedAt/LatestActivity comparisons teeth.
	createSession(t, opts, "probe", "sleep 1.2; echo settled; sleep 60")
	probe := waitForActivityAfterCreation(t, opts, "probe")

	createdAt, err := SessionCreatedAt("probe", opts)
	if err != nil || createdAt != probe.CreatedAt {
		t.Errorf("CreatedAt: probe=%d helper=%d (err %v)", probe.CreatedAt, createdAt, err)
	}
	// Pin that the two stamps really did diverge, so this test cannot quietly
	// degrade into comparing one value against itself.
	if probe.LatestActivity <= probe.CreatedAt {
		t.Errorf("expected window_activity (%d) to be later than session_created (%d) so a transposition is visible",
			probe.LatestActivity, probe.CreatedAt)
	}
	clientCount, err := SessionClientCount("probe", opts)
	if err != nil || clientCount != probe.ClientCount {
		t.Errorf("ClientCount: probe=%d helper=%d (err %v)", probe.ClientCount, clientCount, err)
	}
	paneID, err := SessionPaneID("probe", opts)
	if err != nil || paneID != probe.PaneID {
		t.Errorf("PaneID: probe=%q helper=%q (err %v)", probe.PaneID, paneID, err)
	}
	cols, rows, hasSize, err := SessionPaneSize("probe", opts)
	if err != nil || cols != probe.PaneCols || rows != probe.PaneRows || hasSize != probe.HasPaneSize {
		t.Errorf("PaneSize: probe=(%d,%d,%v) helper=(%d,%d,%v) (err %v)",
			probe.PaneCols, probe.PaneRows, probe.HasPaneSize, cols, rows, hasSize, err)
	}
	infoCols, infoRows, supported, err := SessionPaneSnapshotInfo("probe", opts)
	if err != nil || supported != probe.SnapshotEligible() {
		t.Errorf("SnapshotEligible: probe=%v helper=%v (err %v)", probe.SnapshotEligible(), supported, err)
	}
	if supported && (infoCols != probe.PaneMeta.Cols || infoRows != probe.PaneMeta.Rows) {
		t.Errorf("snapshot dims: probe=(%d,%d) helper=(%d,%d)",
			probe.PaneMeta.Cols, probe.PaneMeta.Rows, infoCols, infoRows)
	}
	state, err := SessionStateFor("probe", opts)
	if err != nil || state.Exists != probe.Exists || state.HasLivePane != probe.HasLivePane {
		t.Errorf("state: probe=(%v,%v) helper=(%v,%v) (err %v)",
			probe.Exists, probe.HasLivePane, state.Exists, state.HasLivePane, err)
	}
	if latest, has, err := SessionLatestActivity("probe", opts); err == nil && has && latest.Unix() != probe.LatestActivity {
		t.Errorf("LatestActivity: probe=%d helper=%d", probe.LatestActivity, latest.Unix())
	}

	// Without real mode metadata the bootstrap could never take a full-pane
	// snapshot, so this pins that tmux actually reports it here.
	if !probe.PaneMeta.ModeState.HasState {
		t.Error("expected tmux to report pane mode state")
	}
	if !probe.SnapshotEligible() {
		t.Errorf("a detached single-pane session must be snapshot-eligible, got %+v", probe)
	}

	// Compare the whole metadata struct, not just the fields the guards read.
	// The cursor position and mode state flow into the restored snapshot and
	// drive cursor placement and alt-screen/scroll-region replay, but nothing
	// else here would notice if two same-typed fields were transposed between
	// sessionProbeFormat and parseProbePaneMeta's indices — swapping cursor_x
	// with cursor_y, or alternate_on with cursor_flag, would silently replay the
	// wrong screen. paneSnapshotMetadataForPane requests the same tmux fields
	// through its own format string, so it catches exactly that class of error.
	wantMeta, err := paneSnapshotMetadataForPane(probe.PaneID, opts)
	if err != nil {
		t.Fatalf("paneSnapshotMetadataForPane: %v", err)
	}
	if probe.PaneMeta != wantMeta {
		t.Errorf("PaneMeta mismatch — probe and paneSnapshotMetadataForPane disagree on a field:\n probe=%+v\nhelper=%+v",
			probe.PaneMeta, wantMeta)
	}
}

// TestProbeSession_PaneMetaSurvivesAltScreen re-runs the metadata cross-check
// against a pane in alt-screen mode with a moved cursor and a narrowed scroll
// region. The default pane state leaves most mode fields at their zero value,
// which makes a transposition between two zero fields invisible; driving them to
// distinct non-zero values is what gives the comparison teeth.
func TestProbeSession_PaneMetaSurvivesAltScreen(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)
	// Park the cursor somewhere distinctive, then enter the alt screen (which
	// saves that position into alternate_saved_x/y), narrow the scroll region
	// (DECSTBM), hide the cursor, and move again. tmux then reports
	// cursor=(3,7), altsaved=(10,5), scroll=(2,19) — every coordinate distinct
	// from every other, so any transposition among them changes a value.
	createSession(t, opts, "alt",
		`printf '\033[6;11H\033[?1049h\033[3;20r\033[?25l\033[8;4H'; sleep 60`)
	waitForPaneMeta(t, opts, "alt")

	probe, err := ProbeSession("alt", opts)
	if err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}
	wantMeta, err := paneSnapshotMetadataForPane(probe.PaneID, opts)
	if err != nil {
		t.Fatalf("paneSnapshotMetadataForPane: %v", err)
	}
	if probe.PaneMeta != wantMeta {
		t.Fatalf("PaneMeta mismatch under alt screen:\n probe=%+v\nhelper=%+v", probe.PaneMeta, wantMeta)
	}
	// Pin that the state really is distinctive, so this test cannot quietly
	// degrade into comparing two sets of zero values.
	if !probe.PaneMeta.ModeState.AltScreen {
		t.Errorf("expected the pane to be in alt screen, got %+v", probe.PaneMeta.ModeState)
	}
	if probe.PaneMeta.CursorX == probe.PaneMeta.CursorY {
		t.Errorf("expected distinct cursor coordinates so a transposition is visible, got %+v", probe.PaneMeta)
	}
	if probe.PaneMeta.ModeState.ScrollTop == 0 && probe.PaneMeta.ModeState.ScrollBottom == probe.PaneMeta.Rows {
		t.Errorf("expected a narrowed scroll region so a transposition is visible, got %+v", probe.PaneMeta.ModeState)
	}
	mode := probe.PaneMeta.ModeState
	if !mode.HasAltSavedCursor {
		t.Fatalf("expected entering the alt screen to save a cursor position, got %+v", mode)
	}
	if mode.AltSavedCursorX == mode.AltSavedCursorY {
		t.Errorf("expected distinct alt-saved coordinates so a transposition is visible, got %+v", mode)
	}
	// The saved pair must also differ from the live pair, so swapping a saved
	// field with a live one cannot go unnoticed either.
	if mode.AltSavedCursorX == probe.PaneMeta.CursorX || mode.AltSavedCursorY == probe.PaneMeta.CursorY {
		t.Errorf("expected the alt-saved cursor to differ from the live cursor, got saved=(%d,%d) live=(%d,%d)",
			mode.AltSavedCursorX, mode.AltSavedCursorY, probe.PaneMeta.CursorX, probe.PaneMeta.CursorY)
	}
}

// waitForPaneMeta waits for a session's pane to report alt-screen mode, so the
// tests above observe the shell's escape sequences after they land rather than
// racing them.
func waitForPaneMeta(t *testing.T, opts Options, session string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if probe, err := ProbeSession(session, opts); err == nil && probe.PaneMeta.ModeState.AltScreen {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pane for session %q never entered alt screen", session)
}

// waitForActivityAfterCreation waits until the session reports window activity
// strictly later than its creation stamp, so a probe taken afterwards
// distinguishes the two fields.
func waitForActivityAfterCreation(t *testing.T, opts Options, session string) SessionProbe {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		probe, err := ProbeSession(session, opts)
		if err == nil && probe.CreatedAt > 0 && probe.LatestActivity > probe.CreatedAt {
			return probe
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("session %q never reported activity later than its creation stamp", session)
	return SessionProbe{}
}
