// Package vterm is a terminal emulator: it parses a stream of ANSI/VT escape
// sequences into a cell grid plus scrollback and renders that state back to
// ANSI. It is the source of truth for what a hosted agent's terminal looks
// like, feeding the compositor and the center/sidebar UI models.
package vterm

import "time"

const MaxScrollback = 10000

// ResponseWriter is called when the terminal needs to send a response back to the PTY
type ResponseWriter func([]byte)

// VTerm is a virtual terminal emulator with scrollback support.
//
// Synchronization contract: VTerm has no internal mutex. All callers must provide
// external synchronization. In practice, every call site (WriteToTerminal,
// SidebarPTYFlush, and TerminalLayer snapshot creation) holds TerminalState.mu
// for the duration of the operation. Snapshot-based rendering (TerminalLayer)
// copies data under the lock and then renders the immutable snapshot without locks.
type VTerm struct {
	// Screen buffer (visible area)
	Screen [][]Cell

	// Scrollback buffer (oldest at index 0)
	Scrollback [][]Cell

	// Cursor position (0-indexed)
	CursorX, CursorY int

	// Dimensions
	Width, Height int

	// Scroll viewing position (0 = live, >0 = lines scrolled up)
	ViewOffset int

	// Alt screen mode (full-screen TUI applications).
	AltScreen bool
	// AllowAltScreenScrollback keeps scrollback active even in alt screen.
	// Useful for tmux-backed sessions where scrollback should remain available.
	AllowAltScreenScrollback bool
	// altCapture tracks the alt-screen frame currently reserved in scrollback
	// (see altScreenCaptureState).
	altCapture altScreenCaptureState
	// altScreenRestorePending tracks a freshly restored alt-screen frame until
	// the first attached clear-screen redraw so it is not re-captured as new
	// scrollback.
	altScreenRestorePending [][]Cell
	altScreenBuf            [][]Cell
	altCursorX              int
	altCursorY              int

	// Scrolling region (for DECSTBM)
	ScrollTop    int
	ScrollBottom int
	// Origin mode (DECOM) - cursor positions are relative to scroll region.
	OriginMode bool

	// Mouse reporting modes requested by the hosted terminal application.
	mouseTrackingMode int
	mouseSGRMode      bool

	// Current style for new characters
	CurrentStyle Style

	// Saved cursor state (for DECSC/DECRC)
	SavedCursorX int
	SavedCursorY int
	SavedStyle   Style

	// Parser state
	parser *Parser

	// Response writer for terminal queries (DSR, DA, etc.)
	responseWriter ResponseWriter

	// OSC-captured state (set by the parser; consumed by the owning UI layer).
	oscTitle         string
	oscWorkingDir    string
	pendingClipboard []byte

	// Selection state for copy/paste highlighting
	// Uses absolute line numbers (0 = first scrollback line)
	selActive               bool
	selStartX, selStartLine int
	selEndX, selEndLine     int
	selRect                 bool

	// Cursor visibility (controlled externally when pane is focused)
	ShowCursor     bool
	lastShowCursor bool
	lastCursorX    int
	lastCursorY    int

	// CursorHidden tracks if terminal app hid cursor via DECTCEM (mode 25)
	CursorHidden     bool
	lastCursorHidden bool
	// IgnoreCursorVisibilityControls ignores DECTCEM mode 25 hide/show toggles.
	// Used by chat-style tabs that render a steady cursor independent of app output.
	IgnoreCursorVisibilityControls bool
	// TreatLFAsCRLF makes bare LF advance to the next line and return to column 0.
	// This is useful for some chat agents that emit LF-only streams.
	TreatLFAsCRLF bool
	// CaptureNormalScreenOnClear preserves chat-style full-screen redraw frames
	// that use normal-screen CSI 2J/3J instead of the alternate screen.
	CaptureNormalScreenOnClear bool
	// preserveScrollbackOnNextClear3 is a one-shot guard for redraw sequences
	// that capture on CSI 2J and immediately follow with CSI 3J.
	preserveScrollbackOnNextClear3 bool

	// Synchronized output (DEC mode 2026)
	syncActive bool
	// syncStartedAt is when the open sync region began; used by the stall
	// failsafe (SyncStallTimeout) to force-release a sync whose end never came.
	syncStartedAt     time.Time
	syncScreen        [][]Cell
	syncScrollbackLen int
	syncDeferTrim     bool
	// syncViewOffsetDelta tracks the net hidden scrollback growth/shrink that
	// occurred while sync output was freezing the visible viewport.
	syncViewOffsetDelta int
	// syncPreserveViewport keeps the frozen viewport anchored when the user was
	// already scrolled or interacted with scrollback during sync output.
	syncPreserveViewport bool

	// Render cache for live screen (ViewOffset == 0)
	renderCache []string
	// Epoch-based dirty tracking (see cache.go): a line is dirty when its
	// epoch (or the global epoch) is newer than the last clear.
	renderEpoch        uint64
	renderLineEpoch    []uint64
	renderGlobalEpoch  uint64
	renderCleanEpoch   uint64
	renderDirtyScratch []bool

	// Version counter for snapshot caching - increments on visible content/cursor changes.
	// UI-driven cursor visibility (ShowCursor) is handled by the snapshot cache key.
	version uint64
}

// New creates a new VTerm with the given dimensions
func New(width, height int) *VTerm {
	v := &VTerm{
		Width:        width,
		Height:       height,
		ScrollTop:    0,
		ScrollBottom: height,
	}
	v.Screen = v.makeScreen(width, height)
	v.Scrollback = make([][]Cell, 0, MaxScrollback)
	v.parser = NewParser(v)
	// Initialize dirty tracking for layer-based rendering
	v.ensureRenderCache(height)
	return v
}

func (v *VTerm) scrollbackEnabled() bool {
	return !v.AltScreen || v.AllowAltScreenScrollback
}

// makeScreen creates a blank screen buffer
func (v *VTerm) makeScreen(width, height int) [][]Cell {
	screen := make([][]Cell, height)
	for i := range screen {
		screen[i] = MakeBlankLine(width)
	}
	return screen
}

// resizeRows builds a new buffer of the given height from old, preserving each
// existing row's content. A row already at least width wide is reused as-is
// (it may stay wider than width, so resizing back up can restore content); a
// narrower row is expanded into a fresh blank line of the new width; rows with
// no source content are blank-filled. Used to resize the screen, alt screen,
// and synchronized-output snapshot identically.
func resizeRows(old [][]Cell, width, height int) [][]Cell {
	rows := make([][]Cell, height)
	for y := 0; y < height; y++ {
		if y < len(old) && len(old[y]) > 0 {
			// Preserve the original row content (may be wider than new width)
			// but ensure it's at least as wide as new width.
			if len(old[y]) >= width {
				rows[y] = old[y]
			} else {
				// Expand row to new width.
				rows[y] = MakeBlankLine(width)
				copy(rows[y], old[y])
			}
		} else {
			rows[y] = MakeBlankLine(width)
		}
	}
	return rows
}

// Resize handles terminal resize.
func (v *VTerm) Resize(width, height int) {
	v.resize(width, height, true)
}

// ResizeWithoutHistoryReveal resizes the terminal without pulling rows out of
// scrollback to fill newly revealed viewport space.
func (v *VTerm) ResizeWithoutHistoryReveal(width, height int) {
	v.resize(width, height, false)
}

func (v *VTerm) resize(width, height int, revealHistoryOnGrow bool) {
	oldWidth := v.Width
	oldHeight := v.Height
	hadPendingRestoredAltScreen := len(v.altScreenRestorePending) > 0
	// Enforce minimum dimensions to prevent negative cursor positions
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	if width == oldWidth && height == oldHeight {
		return
	}

	// If height shrinks, move lines to scrollback
	if height < oldHeight && v.scrollbackEnabled() {
		overflow := oldHeight - height
		if overflow > 0 {
			added := 0
			for i := 0; i < overflow; i++ {
				if len(v.Screen) > 0 {
					v.Scrollback = append(v.Scrollback, v.Screen[0])
					v.Screen = v.Screen[1:]
					added++
				}
			}
			if added > 0 {
				v.invalidateAltScreenCapture()
			}
			v.anchorViewOffsetForAddedLines(added)
			v.trimScrollback()
		}
	}

	// If height grows, restore lines from scrollback so the screen fills.
	// This matches native terminal behavior where expanding reveals history above.
	if height > oldHeight && revealHistoryOnGrow && v.scrollbackEnabled() && v.ViewOffset == 0 {
		added := height - oldHeight
		restore := added
		reserved := v.altCapture.frameLen + v.altCapture.endOffset
		if reserved > len(v.Scrollback) {
			reserved = 0
			v.altCapture.frameLen = 0
			v.altCapture.tracked = false
			v.altCapture.endOffset = 0
		}
		available := len(v.Scrollback) - reserved
		if restore > available {
			restore = available
		}
		if restore > 0 {
			start := available - restore
			restored := make([][]Cell, restore)
			copy(restored, v.Scrollback[start:available])
			v.Scrollback = append(v.Scrollback[:start], v.Scrollback[available:]...)
			v.Screen = append(restored, v.Screen...)
			v.CursorY += restore
		}
	}

	// Resize screen buffer - preserve full row content to allow restoring
	// on resize back to larger width
	v.Screen = resizeRows(v.Screen, width, height)

	// Update dimensions
	v.Width = width
	v.Height = height

	// Adjust scroll region
	if v.ScrollBottom > height || v.ScrollBottom == 0 {
		v.ScrollBottom = height
	}
	if v.ScrollTop >= v.ScrollBottom {
		v.ScrollTop = 0
	}

	// Clamp cursor
	if v.CursorX >= width {
		v.CursorX = width - 1
	}
	if v.CursorY >= height {
		v.CursorY = height - 1
	}
	v.clampCursor()

	// Also resize alt screen if it exists - preserve full row content
	if v.altScreenBuf != nil {
		v.altScreenBuf = resizeRows(v.altScreenBuf, width, height)
	}
	v.clampAltSavedCursor()

	// Keep synchronized output snapshot aligned with new size - preserve full row content
	if v.syncScreen != nil {
		v.syncScreen = resizeRows(v.syncScreen, width, height)
	}
	if hadPendingRestoredAltScreen {
		if v.AltScreen {
			v.trackRestoredAltScreenFrame()
		} else {
			v.clearPendingRestoredAltScreenCapture()
		}
	}
	v.invalidateRenderCache()
	// Re-initialize dirty tracking for new size
	v.ensureRenderCache(height)
}

// Write processes input bytes from PTY
func (v *VTerm) Write(data []byte) {
	v.maybeReleaseStaleSync()
	v.parser.Parse(data)
}

// SetResponseWriter sets the callback for terminal query responses
func (v *VTerm) SetResponseWriter(w ResponseWriter) {
	v.responseWriter = w
}

// respond sends a response back to the PTY (for terminal queries)
func (v *VTerm) respond(data []byte) {
	if v.responseWriter != nil {
		v.responseWriter(data)
	}
}

// Title returns the most recent window/tab title reported via OSC 0/1/2.
func (v *VTerm) Title() string { return v.oscTitle }

// WorkingDir returns the most recent working directory reported via OSC 7
// (raw payload, e.g. "file://host/path").
func (v *VTerm) WorkingDir() string { return v.oscWorkingDir }

// TakePendingClipboard returns and clears any clipboard payload captured from an
// OSC 52 write. Returns nil when none is pending.
func (v *VTerm) TakePendingClipboard() []byte {
	b := v.pendingClipboard
	v.pendingClipboard = nil
	return b
}

func (v *VTerm) setOSCTitle(s string)         { v.oscTitle = s }
func (v *VTerm) setOSCWorkingDir(s string)    { v.oscWorkingDir = s }
func (v *VTerm) setPendingClipboard(b []byte) { v.pendingClipboard = b }

// ParserCarryState reports any in-flight parser state from previously flushed
// PTY bytes. Callers must provide external synchronization.
func (v *VTerm) ParserCarryState() ParserCarryState {
	if v.parser == nil {
		return ParserCarryState{}
	}
	return v.parser.CarryState()
}

// ResetParserState clears any carried parser state after buffered PTY bytes are
// forcibly discarded. Callers must provide external synchronization.
func (v *VTerm) ResetParserState() {
	if v.parser != nil {
		v.parser.Reset()
	}
}

// trimScrollback keeps scrollback under MaxScrollback
func (v *VTerm) trimScrollback() {
	if len(v.Scrollback) > MaxScrollback {
		if v.syncActive {
			v.syncDeferTrim = true
			return
		}
		trimmed := len(v.Scrollback) - MaxScrollback
		v.Scrollback = v.Scrollback[len(v.Scrollback)-MaxScrollback:]
		v.shiftSelectionAfterTrim(trimmed)
	}
	// Clamp ViewOffset after trim to prevent stale offsets
	v.clampViewOffsetToCurrentMax()
}
