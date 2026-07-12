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

type SessionBootstrapFns struct {
	SessionHasClients       func(string, tmux.Options) (bool, error)
	SessionClientCount      func(string, tmux.Options) (int, error)
	SessionActiveWithin     func(string, time.Duration, tmux.Options) (bool, error)
	SessionLatestActivity   func(string, tmux.Options) (time.Time, bool, error)
	SessionCreatedAt        func(string, tmux.Options) (int64, error)
	SessionPaneID           func(string, tmux.Options) (string, error)
	SessionPaneSnapshotInfo func(string, tmux.Options) (int, int, bool, error)
	SessionPaneSize         func(string, tmux.Options) (int, int, bool, error)
	ResizePaneToSize        func(string, int, int, tmux.Options) error
	CapturePaneSnapshot     func(string, tmux.Options) (tmux.PaneSnapshot, error)
}

// SessionBootstrap bundles a SessionBootstrapFns (and the pane-capture fn) with
// the bootstrap operations that consume them, so each caller constructs a single
// instance from its own test-seam vars and invokes the operations as methods
// instead of re-declaring a parallel set of package-level wrappers. The instance
// is value-typed and cheap: callers rebuild it per call from their seam vars so a
// test override of a seam var flows through the next operation.
type SessionBootstrap struct {
	Fns         SessionBootstrapFns
	CapturePane func(string, tmux.Options) ([]byte, error)
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
	return CaptureSessionHistory(sessionName, fallbackCols, fallbackRows, opts, s.Fns, s.CapturePane)
}

func SessionBootstrapExclusive(sessionName string, quietWindow time.Duration, opts tmux.Options, fns SessionBootstrapFns) bool {
	hasClients, clientsErr := fns.SessionHasClients(sessionName, opts)
	recentActivity, activityErr := fns.SessionActiveWithin(sessionName, quietWindow, opts)
	return clientsErr == nil && activityErr == nil && !hasClients && !recentActivity
}

func sessionBootstrapHasNoClients(sessionName string, opts tmux.Options, fns SessionBootstrapFns) bool {
	hasClients, err := fns.SessionHasClients(sessionName, opts)
	return err == nil && !hasClients
}

func SessionBootstrapGeneration(sessionName string, opts tmux.Options, fns SessionBootstrapFns) (int64, string, bool) {
	sessionCreatedAt, err := fns.SessionCreatedAt(sessionName, opts)
	if err != nil || sessionCreatedAt <= 0 {
		return 0, "", false
	}
	paneID, err := fns.SessionPaneID(sessionName, opts)
	if err != nil || paneID == "" {
		return 0, "", false
	}
	return sessionCreatedAt, paneID, true
}

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
	if !SessionBootstrapExclusive(sessionName, quietWindow, opts, fns) {
		return SessionBootstrapCapture{}
	}
	sessionCreatedAt, paneID, ok := SessionBootstrapGeneration(sessionName, opts, fns)
	if !ok {
		return SessionBootstrapCapture{}
	}
	rollbackCols, rollbackRows, supported, err := fns.SessionPaneSnapshotInfo(sessionName, opts)
	if err != nil || !supported {
		return SessionBootstrapCapture{}
	}
	bootstrap := SessionBootstrapCapture{
		SessionCreatedAt: sessionCreatedAt,
		PaneID:           paneID,
		RollbackCols:     rollbackCols,
		RollbackRows:     rollbackRows,
		NeedsRollback:    rollbackCols > 0 && rollbackRows > 0,
	}
	if !SessionBootstrapExclusive(sessionName, quietWindow, opts, fns) {
		return SessionBootstrapCapture{}
	}
	recheckCreatedAt, recheckPaneID, ok := SessionBootstrapGeneration(sessionName, opts, fns)
	if !ok || recheckCreatedAt != sessionCreatedAt || recheckPaneID != paneID {
		return SessionBootstrapCapture{}
	}
	if err := fns.ResizePaneToSize(sessionName, cols, rows, opts); err != nil {
		return SessionBootstrapCapture{}
	}
	if !sessionBootstrapHasNoClients(sessionName, opts, fns) {
		return SessionBootstrapCapture{}
	}
	recheckCreatedAt, recheckPaneID, ok = SessionBootstrapGeneration(sessionName, opts, fns)
	if !ok || recheckCreatedAt != sessionCreatedAt || recheckPaneID != paneID {
		return SessionBootstrapCapture{}
	}
	snapshotCapturedAt := time.Now()
	snapshot, err := fns.CapturePaneSnapshot(sessionName, opts)
	if err != nil {
		rollbackSessionBootstrap(sessionName, bootstrap, opts, fns)
		return SessionBootstrapCapture{}
	}
	if !sessionBootstrapHasNoClients(sessionName, opts, fns) {
		rollbackSessionBootstrap(sessionName, bootstrap, opts, fns)
		return SessionBootstrapCapture{}
	}
	recheckCreatedAt, recheckPaneID, ok = SessionBootstrapGeneration(sessionName, opts, fns)
	if !ok || recheckCreatedAt != sessionCreatedAt || recheckPaneID != paneID {
		rollbackSessionBootstrap(sessionName, bootstrap, opts, fns)
		return SessionBootstrapCapture{}
	}
	bootstrap.Snapshot = snapshot
	bootstrap.CaptureFullPane = true
	bootstrap.SnapshotCaptured = snapshotCapturedAt
	return bootstrap
}

func BootstrapSnapshotStillMatchesSession(
	sessionName string,
	bootstrap SessionBootstrapCapture,
	opts tmux.Options,
	fns SessionBootstrapFns,
) bool {
	if !bootstrap.CaptureFullPane || !bootstrapGenerationMatchesSession(sessionName, bootstrap, opts, fns) {
		return false
	}
	if bootstrap.Snapshot.Cols <= 0 || bootstrap.Snapshot.Rows <= 0 {
		return false
	}
	if fns.SessionPaneSize != nil {
		cols, rows, hasSize, err := fns.SessionPaneSize(sessionName, opts)
		if err != nil || !hasSize || cols != bootstrap.Snapshot.Cols || rows != bootstrap.Snapshot.Rows {
			return false
		}
	}
	if fns.SessionClientCount != nil {
		clientCount, err := fns.SessionClientCount(sessionName, opts)
		if err != nil || clientCount > 1 {
			return false
		}
	}
	if bootstrap.SnapshotCaptured.IsZero() {
		return true
	}
	if fns.SessionLatestActivity != nil {
		latestActivity, hasActivity, err := fns.SessionLatestActivity(sessionName, opts)
		if err != nil {
			return false
		}
		if !hasActivity {
			return true
		}
		// tmux rounds window_activity down to whole seconds. Treat only a later
		// reported second as definite post-snapshot activity; same-second updates
		// may have happened before the snapshot started.
		return !latestActivity.After(bootstrap.SnapshotCaptured)
	}
	elapsed := time.Since(bootstrap.SnapshotCaptured)
	if elapsed <= 0 {
		return true
	}
	recentActivity, err := fns.SessionActiveWithin(sessionName, elapsed, opts)
	return err == nil && !recentActivity
}

func bootstrapGenerationMatchesSession(
	sessionName string,
	bootstrap SessionBootstrapCapture,
	opts tmux.Options,
	fns SessionBootstrapFns,
) bool {
	if bootstrap.SessionCreatedAt <= 0 || bootstrap.PaneID == "" {
		return false
	}
	sessionCreatedAt, paneID, ok := SessionBootstrapGeneration(sessionName, opts, fns)
	if !ok {
		return false
	}
	return sessionCreatedAt == bootstrap.SessionCreatedAt && paneID == bootstrap.PaneID
}

func RollbackExistingSessionBootstrap(sessionName string, bootstrap SessionBootstrapCapture, opts tmux.Options, fns SessionBootstrapFns) {
	if !bootstrap.NeedsRollback || bootstrap.RollbackCols <= 0 || bootstrap.RollbackRows <= 0 {
		return
	}
	if !bootstrapGenerationMatchesSession(sessionName, bootstrap, opts, fns) {
		return
	}
	hasClients, err := fns.SessionHasClients(sessionName, opts)
	if err != nil || hasClients {
		return
	}
	_ = fns.ResizePaneToSize(sessionName, bootstrap.RollbackCols, bootstrap.RollbackRows, opts)
}

func SessionHistoryCaptureSize(sessionName string, fallbackCols, fallbackRows int, opts tmux.Options, fns SessionBootstrapFns) (int, int) {
	cols, rows, hasSize, err := fns.SessionPaneSize(sessionName, opts)
	if err == nil && hasSize && cols > 0 && rows > 0 {
		return cols, rows
	}
	return fallbackCols, fallbackRows
}

func CaptureSessionHistory(
	sessionName string,
	fallbackCols, fallbackRows int,
	opts tmux.Options,
	fns SessionBootstrapFns,
	capturePane func(string, tmux.Options) ([]byte, error),
) ([]byte, int, int) {
	cols, rows := SessionHistoryCaptureSize(sessionName, fallbackCols, fallbackRows, opts, fns)
	scrollback, _ := capturePane(sessionName, opts)
	return scrollback, cols, rows
}

func rollbackSessionBootstrap(sessionName string, bootstrap SessionBootstrapCapture, opts tmux.Options, fns SessionBootstrapFns) {
	RollbackExistingSessionBootstrap(sessionName, bootstrap, opts, fns)
}
