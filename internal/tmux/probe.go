package tmux

import (
	"strconv"
	"time"
)

// SessionProbe is a one-round-trip snapshot of the session and active-pane facts
// the reattach bootstrap needs.
//
// Every field here is individually available from a dedicated helper
// (SessionCreatedAt, SessionHasClients, SessionPaneID, ...), but the bootstrap
// guards re-read the same handful of facts several times per reattach, and each
// helper is a separate tmux exec. amux runs one shared, single-threaded tmux
// server that is also pumping pane output for every attached agent, so a command
// round-trip costs anywhere from ~15ms idle to ~200ms under load. Collapsing a
// guard's worth of reads into a single exec is the difference between a reattach
// that feels instant and one the user watches happen.
//
// Reading the facts together is also strictly more consistent than reading them
// one at a time: a probe is a single point-in-time view, so the generation
// checks below cannot straddle a session change the way sequential reads can.
//
// The per-fact helpers remain as the independent reference this is checked
// against: TestProbeSession_MatchesDedicatedHelpers runs both against a live
// tmux server and asserts they agree, so a format-string or field-order mistake
// here cannot quietly change what the bootstrap sees.
type SessionProbe struct {
	// Exists reports whether the session was found at all.
	Exists bool
	// CreatedAt is #{session_created} in unix seconds; part of the session
	// generation identity used to detect a session being recreated underneath us.
	CreatedAt int64
	// ClientCount is the number of tmux clients currently attached.
	ClientCount int
	// PaneID identifies the pane a reattaching client will render, chosen with
	// the same precedence and the same active-window scope sessionPaneID uses:
	// the active pane of the active window, else any live pane in that window.
	// Empty when the active window has no live pane.
	PaneID string
	// PaneCols/PaneRows are that pane's current cell dimensions.
	PaneCols int
	PaneRows int
	// HasPaneSize reports whether tmux returned usable dimensions.
	HasPaneSize bool
	// HasLivePane reports whether the active window has a pane that is not dead,
	// the same scope hasLivePane (and so SessionStateFor) uses.
	HasLivePane bool
	// SinglePaneWindow reports whether the chosen pane is the only pane in its
	// window. Split (or zoomed-split) windows are ineligible for the pre-attach
	// resize because the resize would also SIGWINCH the hidden siblings.
	SinglePaneWindow bool
	// LatestActivity is the maximum #{window_activity} across the session's
	// windows, in unix seconds. Zero when tmux reported none.
	LatestActivity int64
	// PaneMeta is the chosen pane's snapshot metadata (geometry, cursor, VT mode
	// state). Carrying it here is what lets a single probe serve both as a guard
	// checkpoint and as the before/after anchor for a full-pane capture.
	PaneMeta PaneSnapshotMeta
}

// sessionProbeFormat lists the fields parseSessionProbe expects, in order. The
// trailing pane fields mirror the format paneSnapshotMetadataForPane requests,
// so a probe doubles as that call.
const sessionProbeFormat = "#{session_created}\t#{session_attached}\t#{window_activity}\t" +
	"#{window_active}\t#{window_id}\t#{pane_id}\t#{pane_active}\t#{pane_dead}\t" +
	"#{pane_width}\t#{pane_height}\t#{cursor_x}\t#{cursor_y}\t#{alternate_on}\t" +
	"#{alternate_saved_x}\t#{alternate_saved_y}\t#{cursor_flag}\t#{origin_flag}\t" +
	"#{scroll_region_upper}\t#{scroll_region_lower}"

// sessionProbeFieldCount is the number of fields sessionProbeFormat requests.
const sessionProbeFieldCount = 19

// ProbeSession collects the session/pane facts above in a single tmux exec.
// A session that does not exist yields a zero SessionProbe and no error, matching
// the exit-1-means-empty convention the other read paths use.
func ProbeSession(sessionName string, opts Options) (SessionProbe, error) {
	if sessionName == "" {
		return SessionProbe{}, nil
	}
	if err := EnsureAvailable(); err != nil {
		return SessionProbe{}, err
	}
	// -s: every pane in every window of the session, so window_activity can be
	// maximized across windows without a second list-windows call.
	lines, err := listTmux(opts, "list-panes", "-s", "-t", sessionTarget(sessionName), "-F", sessionProbeFormat)
	if err != nil {
		return SessionProbe{}, err
	}
	return parseSessionProbe(lines), nil
}

type probePaneRow struct {
	windowActive bool
	windowID     string
	paneID       string
	paneActive   bool
	paneDead     bool
	meta         PaneSnapshotMeta
}

// parseSessionProbe is the pure half of ProbeSession. Choosing the pane and
// maximizing activity across windows is the genuinely bug-prone part, so it is
// unit-testable without a live tmux server.
func parseSessionProbe(lines []string) SessionProbe {
	probe := SessionProbe{}
	rows := make([]probePaneRow, 0, len(lines))
	panesPerWindow := make(map[string]int)

	for _, line := range lines {
		parts, err := parseTabFields(line, sessionProbeFieldCount)
		if err != nil {
			continue
		}
		if !probe.Exists {
			probe.Exists = true
			if createdAt, err := strconv.ParseInt(parts[0], 10, 64); err == nil && createdAt > 0 {
				probe.CreatedAt = createdAt
			}
			if clients, err := strconv.Atoi(parts[1]); err == nil && clients > 0 {
				probe.ClientCount = clients
			}
		}
		if activity, err := strconv.ParseInt(parts[2], 10, 64); err == nil && activity > probe.LatestActivity {
			probe.LatestActivity = activity
		}

		row := probePaneRow{
			windowActive: parts[3] == "1",
			windowID:     parts[4],
			paneID:       parts[5],
			paneActive:   parts[6] == "1",
			paneDead:     parts[7] == "1",
		}
		if row.paneID == "" || row.paneID[0] != '%' {
			continue
		}
		row.meta = parseProbePaneMeta(parts, row.paneID)
		// Liveness is scoped to the active window, matching hasLivePane (and so
		// SessionStateFor). A live pane in some other window does not make the
		// window a reattaching client would actually render live.
		if row.windowActive && !row.paneDead {
			probe.HasLivePane = true
		}
		panesPerWindow[row.windowID]++
		rows = append(rows, row)
	}

	chosen, ok := chooseProbePane(rows)
	if !ok {
		return probe
	}
	probe.PaneID = chosen.paneID
	probe.PaneMeta = chosen.meta
	probe.PaneCols, probe.PaneRows = chosen.meta.Cols, chosen.meta.Rows
	probe.HasPaneSize = chosen.meta.HasSize
	probe.SinglePaneWindow = panesPerWindow[chosen.windowID] == 1
	return probe
}

// parseProbePaneMeta reads the pane-metadata tail of a probe row. It reuses
// parsePaneModeState so the mode-state parsing stays in one place: the probe
// requests those fields in the same order paneSnapshotMetadataForPane does.
func parseProbePaneMeta(parts []string, paneID string) PaneSnapshotMeta {
	meta := PaneSnapshotMeta{}
	cols, errCols := strconv.Atoi(parts[8])
	rows, errRows := strconv.Atoi(parts[9])
	if errCols == nil && errRows == nil && cols > 0 && rows > 0 {
		meta.Cols, meta.Rows, meta.HasSize = cols, rows, true
	}
	cursorX, errCursorX := strconv.Atoi(parts[10])
	cursorY, errCursorY := strconv.Atoi(parts[11])
	if errCursorX == nil && errCursorY == nil {
		meta.CursorX, meta.CursorY, meta.HasCursor = cursorX, cursorY, true
	}
	meta.ModeState, _ = parsePaneModeState([]string{
		paneID,
		parts[12], // alternate_on
		parts[13], // alternate_saved_x
		parts[14], // alternate_saved_y
		parts[15], // cursor_flag
		parts[16], // origin_flag
		parts[17], // scroll_region_upper
		parts[18], // scroll_region_lower
	}, paneID)
	return meta
}

// chooseProbePane picks the pane a reattaching client renders, mirroring
// sessionPaneID: the active pane of the active window wins, else any live pane
// in the active window. Dead panes are never chosen.
//
// Panes outside the active window are deliberately never chosen, even when the
// active window has nothing live. sessionPaneID cannot reach them — it reads
// `list-panes -t <session>`, which tmux scopes to the active window — whereas
// the probe reads `list-panes -s`, which spans every window. Picking a pane the
// client will not display would be actively harmful: the pre-attach
// ResizePaneToSize targets the session, which tmux also resolves to the active
// window, so the resize would land on one window while the snapshot described
// another. The guards could not catch that, because the pane being described
// never changes. Returning no pane instead falls back to history replay.
func chooseProbePane(rows []probePaneRow) (probePaneRow, bool) {
	var fallback probePaneRow
	var haveFallback bool
	for _, row := range rows {
		if row.paneDead || !row.windowActive {
			continue
		}
		if row.paneActive {
			return row, true
		}
		if !haveFallback {
			fallback, haveFallback = row, true
		}
	}
	return fallback, haveFallback
}

// ActiveWithin reports whether the probed session saw window activity inside the
// given window, applying the same whole-second slack SessionActiveWithin uses.
func (p SessionProbe) ActiveWithin(window time.Duration, now time.Time) bool {
	return activityWithinWindow(p.LatestActivity, window, now)
}

// HasClients reports whether any tmux client is attached to the probed session.
func (p SessionProbe) HasClients() bool { return p.ClientCount > 0 }

// SnapshotEligible reports whether the probed pane can anchor an authoritative
// full-pane snapshot: it must be the sole pane in its window (otherwise the
// pre-attach resize would also SIGWINCH hidden siblings) and report complete
// geometry and mode metadata.
func (p SessionProbe) SnapshotEligible() bool {
	return p.PaneID != "" && p.SinglePaneWindow && p.PaneMeta.Eligible()
}

// SameGeneration reports whether two probes describe the same incarnation of a
// session. A session killed and recreated under the same name gets a new
// creation timestamp and a new pane ID, so any capture taken across that gap
// must be discarded.
func (p SessionProbe) SameGeneration(other SessionProbe) bool {
	if p.CreatedAt <= 0 || p.PaneID == "" {
		return false
	}
	return p.CreatedAt == other.CreatedAt && p.PaneID == other.PaneID
}

// CapturePaneFullData captures a resolved pane's scrollback plus its visible
// screen. Unlike CapturePaneSnapshot it takes an already-resolved pane ID and
// performs no validation, so a caller holding probes on both sides of the
// capture pays one tmux round-trip instead of four.
func CapturePaneFullData(paneID string, opts Options) ([]byte, error) {
	if paneID == "" {
		return nil, errPaneSnapshotUnavailable
	}
	return capturePaneSnapshotData(paneID, opts)
}

// CapturePaneHistoryData captures a resolved pane's scrollback, excluding the
// visible screen. It is the shared implementation behind CapturePane, which
// differs only in resolving the pane from a session name first.
func CapturePaneHistoryData(paneID string, opts Options) ([]byte, error) {
	if paneID == "" {
		return nil, nil
	}
	// -p: output to stdout
	// -e: include escape sequences (ANSI styling)
	// -S -: start from beginning of history
	// -N: preserve trailing spaces in each captured row. History-only callers
	// also rely on this so post-attach deltas keep padded/status-bar rows intact.
	// -E -1: end at last scrollback line (excludes visible screen)
	// -t: target pane by globally unique pane ID
	cmd, cancel := tmuxCommand(opts, "capture-pane", "-p", "-e", "-N", "-S", "-", "-E", "-1", "-t", paneID)
	defer cancel()
	output, err := runTmuxCmd(cmd)
	if err != nil {
		return nil, err
	}
	if len(output) == 0 {
		return nil, nil
	}
	return output, nil
}
