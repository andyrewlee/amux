package tmux

import (
	"errors"
	"strconv"
	"strings"
)

// PaneSnapshot contains a full-pane capture plus optional cursor metadata from tmux.
type PaneSnapshot struct {
	Data      []byte
	Cols      int
	Rows      int
	CursorX   int
	CursorY   int
	HasCursor bool
	ModeState PaneModeState
}

// PaneModeState describes the VT mode state tmux reports for a captured pane.
type PaneModeState struct {
	HasState          bool
	AltScreen         bool
	OriginMode        bool
	CursorHidden      bool
	ScrollTop         int
	ScrollBottom      int
	HasAltSavedCursor bool
	AltSavedCursorX   int
	AltSavedCursorY   int
}

var (
	errPaneSnapshotNotWholeWindow = errors.New("tmux pane snapshot does not cover full window")
	errPaneSnapshotUnavailable    = errors.New("tmux pane snapshot unavailable")
	errPaneSnapshotModeState      = errors.New("tmux pane snapshot missing mode metadata")
	errPaneSnapshotSizeMetadata   = errors.New("tmux pane snapshot missing size metadata")
	errPaneSnapshotMetadataDrift  = errors.New("tmux pane snapshot metadata changed during capture")
)

type paneSnapshotMetadata struct {
	Cols      int
	Rows      int
	HasSize   bool
	CursorX   int
	CursorY   int
	HasCursor bool
	ModeState PaneModeState
}

// sessionPaneID resolves the active pane ID for a session using exact name matching.
// Returns empty string if the session does not exist.
func sessionPaneID(sessionName string, opts Options) (string, error) {
	exists, err := hasSession(sessionName, opts)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", nil
	}
	// Use list-panes instead of display-message. display-message may return an
	// empty pane_id for detached sessions on some tmux versions.
	cmd, cancel := tmuxCommand(opts, "list-panes", "-t", sessionTarget(sessionName), "-F", "#{pane_id}\t#{pane_active}\t#{pane_dead}")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	var firstAlive string
	for _, line := range parseOutputLines(output) {
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		paneID := strings.TrimSpace(parts[0])
		paneActive := strings.TrimSpace(parts[1]) == "1"
		paneDead := strings.TrimSpace(parts[2]) == "1"
		if paneID == "" || paneID[0] != '%' || paneDead {
			continue
		}
		if paneActive {
			return paneID, nil
		}
		if firstAlive == "" {
			firstAlive = paneID
		}
	}
	return firstAlive, nil
}

func paneCursorPosition(paneID string, opts Options) (int, int, bool, error) {
	if paneID == "" {
		return 0, 0, false, nil
	}
	cmd, cancel := tmuxCommand(opts, "list-panes", "-t", paneID, "-F", "#{pane_id}\t#{cursor_x}\t#{cursor_y}")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, false, err
	}
	for _, line := range parseOutputLines(output) {
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		if strings.TrimSpace(parts[0]) != paneID {
			continue
		}
		cursorX, errX := strconv.Atoi(strings.TrimSpace(parts[1]))
		cursorY, errY := strconv.Atoi(strings.TrimSpace(parts[2]))
		if errX != nil || errY != nil {
			return 0, 0, false, nil
		}
		return cursorX, cursorY, true, nil
	}
	return 0, 0, false, nil
}

func paneSize(paneID string, opts Options) (int, int, bool, error) {
	if paneID == "" {
		return 0, 0, false, nil
	}
	cmd, cancel := tmuxCommand(opts, "list-panes", "-t", paneID, "-F", "#{pane_id}\t#{pane_width}\t#{pane_height}")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, false, err
	}
	for _, line := range parseOutputLines(output) {
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		if strings.TrimSpace(parts[0]) != paneID {
			continue
		}
		cols, errCols := strconv.Atoi(strings.TrimSpace(parts[1]))
		rows, errRows := strconv.Atoi(strings.TrimSpace(parts[2]))
		if errCols != nil || errRows != nil || cols <= 0 || rows <= 0 {
			return 0, 0, false, nil
		}
		return cols, rows, true, nil
	}
	return 0, 0, false, nil
}

// SessionPaneSnapshotInfo reports whether a session's active pane is eligible
// for an authoritative full-pane snapshot, along with its current size. This
// uses only pane metadata and does not capture pane history.
func SessionPaneSnapshotInfo(sessionName string, opts Options) (int, int, bool, error) {
	if sessionName == "" {
		return 0, 0, false, nil
	}
	paneID, err := sessionPaneID(sessionName, opts)
	if err != nil {
		return 0, 0, false, err
	}
	return paneSnapshotInfoForPane(paneID, opts, paneCoversVisibleWindow, paneSnapshotMetadataForPane)
}

// SessionPaneID reports the active pane ID for a session using only tmux
// metadata. Returns an empty string if the session does not exist.
func SessionPaneID(sessionName string, opts Options) (string, error) {
	if sessionName == "" {
		return "", nil
	}
	return sessionPaneID(sessionName, opts)
}

// SessionPaneSize reports the current size of a session's active pane using
// only tmux metadata. The returned size is independent of whether the pane is
// eligible for authoritative full-pane snapshots.
func SessionPaneSize(sessionName string, opts Options) (int, int, bool, error) {
	if sessionName == "" {
		return 0, 0, false, nil
	}
	paneID, err := sessionPaneID(sessionName, opts)
	if err != nil {
		return 0, 0, false, err
	}
	if paneID == "" {
		return 0, 0, false, nil
	}
	return paneSize(paneID, opts)
}

func parsePaneModeState(parts []string, paneID string) (PaneModeState, bool) {
	if len(parts) == 0 {
		return PaneModeState{}, false
	}
	if strings.TrimSpace(parts[0]) != paneID {
		return PaneModeState{}, false
	}
	if len(parts) < 8 {
		return PaneModeState{}, true
	}

	parseFlag := func(raw string) (bool, bool) {
		raw = strings.TrimSpace(raw)
		switch raw {
		case "0":
			return false, true
		case "1":
			return true, true
		default:
			return false, false
		}
	}
	parseInt := func(raw string) (int, bool) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return 0, false
		}
		v, err := strconv.Atoi(raw)
		if err != nil {
			return 0, false
		}
		return v, true
	}

	altScreen, ok := parseFlag(parts[1])
	if !ok {
		return PaneModeState{}, true
	}

	state := PaneModeState{
		HasState:  true,
		AltScreen: altScreen,
	}
	if cursorVisible, ok := parseFlag(parts[4]); ok {
		state.CursorHidden = !cursorVisible
	}
	if originMode, ok := parseFlag(parts[5]); ok {
		state.OriginMode = originMode
	}
	if upper, ok := parseInt(parts[6]); ok {
		if lower, ok := parseInt(parts[7]); ok {
			state.ScrollTop = upper
			state.ScrollBottom = lower + 1
		}
	}
	altSavedX, errSavedX := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
	altSavedY, errSavedY := strconv.ParseInt(strings.TrimSpace(parts[3]), 10, 64)
	if errSavedX == nil && errSavedY == nil && altSavedX >= 0 && altSavedY >= 0 &&
		altSavedX != 4294967295 && altSavedY != 4294967295 {
		state.HasAltSavedCursor = true
		state.AltSavedCursorX = int(altSavedX)
		state.AltSavedCursorY = int(altSavedY)
	}
	return state, true
}

func paneCoversVisibleWindow(paneID string, opts Options) (bool, error) {
	if paneID == "" {
		return false, nil
	}
	cmd, cancel := tmuxCommand(opts, "list-panes", "-t", paneID, "-F", "#{pane_id}")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}
	count := 0
	for _, line := range parseOutputLines(output) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		count++
	}
	// Zoomed split panes still share a tmux window with hidden siblings, and the
	// pre-attach resize step would send SIGWINCH into those hidden panes too.
	return count == 1, nil
}

// ResizePaneToSize updates a managed session's window size to the given cell
// dimensions before any new client attaches. This is used to capture an
// authoritative snapshot at the size a reattached client will render.
func ResizePaneToSize(sessionName string, cols, rows int, opts Options) error {
	if sessionName == "" || cols <= 0 || rows <= 0 {
		return nil
	}
	exists, err := hasSession(sessionName, opts)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	cmd, cancel := tmuxCommand(
		opts,
		"resize-window",
		"-t",
		sessionTarget(sessionName),
		"-x",
		strconv.Itoa(cols),
		"-y",
		strconv.Itoa(rows),
	)
	defer cancel()
	if err := cmd.Run(); err != nil {
		if isExitCode1(err) {
			return nil
		}
		return err
	}
	return nil
}

// CapturePane captures the scrollback history of a tmux pane (excluding the
// visible screen area) with ANSI escape codes preserved. Returns nil if the
// session has no scrollback or does not exist.
func CapturePane(sessionName string, opts Options) ([]byte, error) {
	if sessionName == "" {
		return nil, nil
	}
	paneID, err := sessionPaneID(sessionName, opts)
	if err != nil {
		return nil, err
	}
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
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	if len(output) == 0 {
		return nil, nil
	}
	return output, nil
}

// CapturePaneSnapshot captures the full tmux pane state (scrollback plus the
// current visible screen) with ANSI escape codes preserved, along with the pane
// cursor position when tmux reports it.
func CapturePaneSnapshot(sessionName string, opts Options) (PaneSnapshot, error) {
	if sessionName == "" {
		return PaneSnapshot{}, nil
	}
	paneID, err := sessionPaneID(sessionName, opts)
	if err != nil {
		return PaneSnapshot{}, err
	}
	if paneID == "" {
		return PaneSnapshot{}, errPaneSnapshotUnavailable
	}
	singlePane, err := paneCoversVisibleWindow(paneID, opts)
	if err != nil {
		return PaneSnapshot{}, err
	}
	if !singlePane {
		return PaneSnapshot{}, errPaneSnapshotNotWholeWindow
	}
	return capturePaneSnapshotForPane(
		paneID,
		opts,
		capturePaneSnapshotData,
		paneSnapshotMetadataForPane,
	)
}

// CapturePaneTail captures the last N lines of a session's active pane.
// Returns the content and a success flag. Returns ("", false) on error
// (e.g., session doesn't exist or capture times out).
func CapturePaneTail(sessionName string, lines int, opts Options) (string, bool) {
	if sessionName == "" || lines <= 0 {
		return "", false
	}
	paneID, err := sessionPaneID(sessionName, opts)
	if err != nil || paneID == "" {
		return "", false
	}
	startLine := -lines
	cmd, cancel := tmuxCommand(opts, "capture-pane", "-p", "-t", paneID, "-S", strconv.Itoa(startLine))
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		return "", false
	}
	// Normalize: trim trailing whitespace from each line and trailing empty lines
	text := strings.TrimRight(string(output), " \t\n\r")
	return text, true
}
