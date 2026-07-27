package ptyio

import (
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// FullPaneCaptureQuietWindow is how long a session must be free of recent
// activity before a pre-attach full-pane bootstrap snapshot is taken.
const FullPaneCaptureQuietWindow = 2 * time.Second

type SessionBootstrapCapture struct {
	Snapshot         tmux.PaneSnapshot
	CaptureFullPane  bool
	SnapshotCaptured time.Time
	SessionCreatedAt int64
	PaneID           string
	RollbackCols     int
	RollbackRows     int
	NeedsRollback    bool
}

// SessionBootstrapFns are the tmux operations the bootstrap performs, injected
// so the whole flow can be driven without a tmux server.
//
// Every read goes through ProbeSession, a single tmux round-trip returning all
// the session and pane facts the guards below test. That is deliberate: amux
// talks to one shared, single-threaded tmux server that is also pumping output
// for every attached agent, so round-trips are the dominant cost of a reattach
// and a busy server can make each one take hundreds of milliseconds. It also
// makes each guard a genuine point-in-time check — reading the facts separately
// let a guard straddle a change and reach a conclusion true of no single moment.
type SessionBootstrapFns struct {
	// ProbeSession reads the session and active-pane state in one round-trip.
	ProbeSession func(string, tmux.Options) (tmux.SessionProbe, error)
	// ResizePaneToSize sets the session's window size before any client attaches.
	ResizePaneToSize func(string, int, int, tmux.Options) error
	// CapturePaneFullData captures a resolved pane's scrollback and visible
	// screen. Validation is the caller's job, via the probes taken around it.
	CapturePaneFullData func(paneID string, opts tmux.Options) ([]byte, error)
	// CapturePaneHistoryData captures a resolved pane's scrollback only.
	CapturePaneHistoryData func(paneID string, opts tmux.Options) ([]byte, error)
}

// SessionBootstrap bundles a SessionBootstrapFns with the bootstrap operations
// that consume them, so each caller constructs a single instance from its own
// test-seam vars and invokes the operations as methods instead of re-declaring a
// parallel set of package-level wrappers. The instance is value-typed and cheap:
// callers rebuild it per call from their seam vars so a test override of a seam
// var flows through the next operation.
type SessionBootstrap struct {
	Fns SessionBootstrapFns
}

// CaptureExisting captures a pre-attach full-pane bootstrap snapshot of an
// existing session, using the standard full-pane quiet window.
func (s SessionBootstrap) CaptureExisting(sessionName string, cols, rows int, opts tmux.Options) SessionBootstrapCapture {
	return CaptureExistingSessionBootstrap(sessionName, cols, rows, FullPaneCaptureQuietWindow, opts, s.Fns)
}

// SnapshotStillMatches reports whether the captured bootstrap snapshot is still
// authoritative for the session.
func (s SessionBootstrap) SnapshotStillMatches(sessionName string, bootstrap SessionBootstrapCapture, opts tmux.Options) bool {
	return BootstrapSnapshotStillMatchesSession(sessionName, bootstrap, opts, s.Fns)
}

// Rollback restores the pane size mutated while capturing the bootstrap snapshot.
func (s SessionBootstrap) Rollback(sessionName string, bootstrap SessionBootstrapCapture, opts tmux.Options) {
	RollbackExistingSessionBootstrap(sessionName, bootstrap, opts, s.Fns)
}

// HistoryCaptureSize resolves the capture dimensions for a history fallback.
func (s SessionBootstrap) HistoryCaptureSize(sessionName string, fallbackCols, fallbackRows int, opts tmux.Options) (int, int) {
	return SessionHistoryCaptureSize(sessionName, fallbackCols, fallbackRows, opts, s.Fns)
}

// CaptureHistory captures the session scrollback plus its capture dimensions.
func (s SessionBootstrap) CaptureHistory(sessionName string, fallbackCols, fallbackRows int, opts tmux.Options) ([]byte, int, int) {
	return CaptureSessionHistory(sessionName, fallbackCols, fallbackRows, opts, s.Fns)
}

// probe reads the session state, mapping any error to a zero probe. Callers
// treat "could not read" and "not eligible" identically: both mean fall back to
// the history-only bootstrap, which is always safe.
func probe(sessionName string, opts tmux.Options, fns SessionBootstrapFns) tmux.SessionProbe {
	if fns.ProbeSession == nil {
		return tmux.SessionProbe{}
	}
	result, err := fns.ProbeSession(sessionName, opts)
	if err != nil {
		return tmux.SessionProbe{}
	}
	return result
}

// bootstrapExclusive reports whether the session is quiet enough to mutate: no
// client attached, and no window activity inside the quiet window. Both must
// hold, because the pre-attach resize is visible to anything already watching.
func bootstrapExclusive(p tmux.SessionProbe, quietWindow time.Duration) bool {
	return p.Exists && !p.HasClients() && !p.ActiveWithin(quietWindow, time.Now())
}

// CaptureExistingSessionBootstrap takes an authoritative full-pane snapshot of a
// detached session at the size the reattaching client will render, so the local
// terminal can be seeded with the true screen instead of replayed history.
//
// The snapshot is only trustworthy if the session stayed unobserved and
// unchanged throughout, so the flow is bracketed by probes: one to establish
// eligibility and the session generation, one after the resize, and one after
// the capture. Any drift between them abandons the snapshot (rolling the resize
// back when it is safe to do so) and the caller falls back to history replay.
func CaptureExistingSessionBootstrap(
	sessionName string,
	cols, rows int,
	quietWindow time.Duration,
	opts tmux.Options,
	fns SessionBootstrapFns,
) SessionBootstrapCapture {
	if cols <= 0 || rows <= 0 {
		return SessionBootstrapCapture{}
	}

	before := probe(sessionName, opts, fns)
	if !bootstrapExclusive(before, quietWindow) || !before.SnapshotEligible() {
		return SessionBootstrapCapture{}
	}

	bootstrap := SessionBootstrapCapture{
		SessionCreatedAt: before.CreatedAt,
		PaneID:           before.PaneID,
		RollbackCols:     before.PaneMeta.Cols,
		RollbackRows:     before.PaneMeta.Rows,
		NeedsRollback:    before.PaneMeta.Cols > 0 && before.PaneMeta.Rows > 0,
	}

	// `before` is the only activity sample taken before this mutation, so a
	// session that starts producing output between that probe and here is still
	// resized. That is an accepted tradeoff, not an oversight: activity cannot be
	// rechecked after the resize because the resize itself bumps
	// window_activity, and it cannot be resampled just before the resize without
	// spending the round-trip this design exists to avoid. The exposure is
	// smaller than it was when each guard read its facts separately — the gap is
	// now one probe wide rather than three tmux calls wide — and the consequence
	// is bounded: the post-attach validation discards the resulting snapshot, so
	// the user sees a correctly replayed screen either way. What survives is a
	// brief resize of a pane that just woke up, which tmux re-lays-out on attach
	// regardless.
	if err := fns.ResizePaneToSize(sessionName, cols, rows, opts); err != nil {
		return SessionBootstrapCapture{}
	}

	// This checkpoint (and the one after the capture) deliberately tests only
	// clients and generation, for the reason above: post-resize activity is
	// self-inflicted and says nothing about the session.
	snapshotCapturedAt := time.Now()
	atCapture := probe(sessionName, opts, fns)
	if !before.SameGeneration(atCapture) || atCapture.HasClients() || !atCapture.SnapshotEligible() {
		rollbackSessionBootstrap(sessionName, bootstrap, opts, fns)
		return SessionBootstrapCapture{}
	}

	data, err := fns.CapturePaneFullData(before.PaneID, opts)
	if err != nil {
		rollbackSessionBootstrap(sessionName, bootstrap, opts, fns)
		return SessionBootstrapCapture{}
	}

	after := probe(sessionName, opts, fns)
	if !before.SameGeneration(after) || after.HasClients() {
		rollbackSessionBootstrap(sessionName, bootstrap, opts, fns)
		return SessionBootstrapCapture{}
	}
	// Identical metadata on both sides of the capture is what proves the bytes
	// describe one coherent screen rather than a pane that moved mid-read.
	if after.PaneMeta != atCapture.PaneMeta {
		rollbackSessionBootstrap(sessionName, bootstrap, opts, fns)
		return SessionBootstrapCapture{}
	}

	bootstrap.Snapshot = tmux.PaneSnapshot{
		Data:      data,
		Cols:      atCapture.PaneMeta.Cols,
		Rows:      atCapture.PaneMeta.Rows,
		CursorX:   atCapture.PaneMeta.CursorX,
		CursorY:   atCapture.PaneMeta.CursorY,
		HasCursor: atCapture.PaneMeta.HasCursor,
		ModeState: atCapture.PaneMeta.ModeState,
	}
	bootstrap.CaptureFullPane = true
	bootstrap.SnapshotCaptured = snapshotCapturedAt
	return bootstrap
}

// BootstrapSnapshotStillMatchesSession reports whether a snapshot taken before
// the attach is still an accurate picture of the session now that a client is
// attached. The attaching client is expected, so up to one client is tolerated;
// a second means something else is driving the session.
func BootstrapSnapshotStillMatchesSession(
	sessionName string,
	bootstrap SessionBootstrapCapture,
	opts tmux.Options,
	fns SessionBootstrapFns,
) bool {
	if !bootstrap.CaptureFullPane {
		return false
	}
	if bootstrap.Snapshot.Cols <= 0 || bootstrap.Snapshot.Rows <= 0 {
		return false
	}

	current := probe(sessionName, opts, fns)
	if !bootstrapGenerationMatches(bootstrap, current) {
		return false
	}
	if !current.HasPaneSize ||
		current.PaneMeta.Cols != bootstrap.Snapshot.Cols ||
		current.PaneMeta.Rows != bootstrap.Snapshot.Rows {
		return false
	}
	if current.ClientCount > 1 {
		return false
	}
	if bootstrap.SnapshotCaptured.IsZero() {
		return true
	}
	if current.LatestActivity == 0 {
		return true
	}
	// tmux rounds window_activity down to whole seconds. Treat only a later
	// reported second as definite post-snapshot activity; same-second updates
	// may have happened before the snapshot started.
	return !time.Unix(current.LatestActivity, 0).After(bootstrap.SnapshotCaptured)
}

// bootstrapGenerationMatches reports whether a probe describes the same session
// incarnation the bootstrap was captured from.
func bootstrapGenerationMatches(bootstrap SessionBootstrapCapture, current tmux.SessionProbe) bool {
	if bootstrap.SessionCreatedAt <= 0 || bootstrap.PaneID == "" {
		return false
	}
	return current.CreatedAt == bootstrap.SessionCreatedAt && current.PaneID == bootstrap.PaneID
}

// RollbackExistingSessionBootstrap restores the pane size the bootstrap resized
// away from. It is skipped once anything else has taken an interest in the
// session — a client attached, or the session was recreated — because resizing
// then would disrupt a pane amux no longer exclusively controls.
func RollbackExistingSessionBootstrap(sessionName string, bootstrap SessionBootstrapCapture, opts tmux.Options, fns SessionBootstrapFns) {
	if !bootstrap.NeedsRollback || bootstrap.RollbackCols <= 0 || bootstrap.RollbackRows <= 0 {
		return
	}
	current := probe(sessionName, opts, fns)
	if !bootstrapGenerationMatches(bootstrap, current) || current.HasClients() {
		return
	}
	_ = fns.ResizePaneToSize(sessionName, bootstrap.RollbackCols, bootstrap.RollbackRows, opts)
}

// SessionHistoryCaptureSize resolves the dimensions a history capture should be
// interpreted at, preferring the live pane size over the caller's fallback.
func SessionHistoryCaptureSize(sessionName string, fallbackCols, fallbackRows int, opts tmux.Options, fns SessionBootstrapFns) (int, int) {
	current := probe(sessionName, opts, fns)
	if current.HasPaneSize && current.PaneCols > 0 && current.PaneRows > 0 {
		return current.PaneCols, current.PaneRows
	}
	return fallbackCols, fallbackRows
}

// CaptureSessionHistory captures a session's scrollback plus the dimensions it
// should be replayed at, in a single probe and a single capture.
func CaptureSessionHistory(
	sessionName string,
	fallbackCols, fallbackRows int,
	opts tmux.Options,
	fns SessionBootstrapFns,
) ([]byte, int, int) {
	current := probe(sessionName, opts, fns)
	cols, rows := fallbackCols, fallbackRows
	if current.HasPaneSize && current.PaneCols > 0 && current.PaneRows > 0 {
		cols, rows = current.PaneCols, current.PaneRows
	}
	if current.PaneID == "" {
		return nil, cols, rows
	}
	// A capture error is not fatal: replaying whatever bytes came back (possibly
	// none) is always preferable to failing the reattach.
	scrollback, _ := fns.CapturePaneHistoryData(current.PaneID, opts)
	return scrollback, cols, rows
}

func rollbackSessionBootstrap(sessionName string, bootstrap SessionBootstrapCapture, opts tmux.Options, fns SessionBootstrapFns) {
	RollbackExistingSessionBootstrap(sessionName, bootstrap, opts, fns)
}
