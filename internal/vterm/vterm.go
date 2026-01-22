package vterm

const MaxScrollback = 10000

// ResponseWriter is called when the terminal needs to send a response back to the PTY
type ResponseWriter func([]byte)

// VTerm is a virtual terminal emulator with scrollback support
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
	AltScreen    bool
	altScreenBuf [][]Cell
	altCursorX   int
	altCursorY   int

	// Scrolling region (for DECSTBM)
	ScrollTop    int
	ScrollBottom int
	// Origin mode (DECOM) - cursor positions are relative to scroll region.
	OriginMode bool

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

	// Selection state for copy/paste highlighting
	// Uses absolute line numbers (0 = first scrollback line)
	selActive                  bool
	selStartX, selStartLine    int
	selEndX, selEndLine        int
	selRect                    bool

	// Cursor visibility (controlled externally when pane is focused)
	ShowCursor     bool
	lastShowCursor bool
	lastCursorX    int
	lastCursorY    int

	// CursorHidden tracks if terminal app hid cursor via DECTCEM (mode 25)
	CursorHidden     bool
	lastCursorHidden bool

	// Synchronized output (DEC mode 2026)
	syncActive        bool
	syncScreen        [][]Cell
	syncScrollbackLen int
	syncDeferTrim     bool

	// Render cache for live screen (ViewOffset == 0)
	renderCache    []string
	renderDirty    []bool
	renderDirtyAll bool

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

// makeScreen creates a blank screen buffer
func (v *VTerm) makeScreen(width, height int) [][]Cell {
	screen := make([][]Cell, height)
	for i := range screen {
		screen[i] = MakeBlankLine(width)
	}
	return screen
}

// Resize handles terminal resize
func (v *VTerm) Resize(width, height int) {
	oldWidth := v.Width
	oldHeight := v.Height
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
	if height < oldHeight && !v.AltScreen {
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
			if added > 0 && v.ViewOffset > 0 {
				v.ViewOffset += added
				if v.ViewOffset > len(v.Scrollback) {
					v.ViewOffset = len(v.Scrollback)
				}
			}
			v.trimScrollback()
		}
	}

	// If height grows, restore lines from scrollback so the screen fills.
	// This matches native terminal behavior where expanding reveals history above.
	if height > oldHeight && !v.AltScreen && v.ViewOffset == 0 {
		added := height - oldHeight
		restore := added
		if restore > len(v.Scrollback) {
			restore = len(v.Scrollback)
		}
		if restore > 0 {
			start := len(v.Scrollback) - restore
			restored := v.Scrollback[start:]
			v.Scrollback = v.Scrollback[:start]
			v.Screen = append(restored, v.Screen...)
			v.CursorY += restore
		}
	}

	// Resize screen buffer - preserve full row content to allow restoring
	// on resize back to larger width (e.g., exiting monitor mode)
	newScreen := make([][]Cell, height)
	for y := 0; y < height; y++ {
		if y < len(v.Screen) && len(v.Screen[y]) > 0 {
			// Preserve the original row content (may be wider than new width)
			// but ensure it's at least as wide as new width
			if len(v.Screen[y]) >= width {
				newScreen[y] = v.Screen[y]
			} else {
				// Expand row to new width
				newScreen[y] = MakeBlankLine(width)
				copy(newScreen[y], v.Screen[y])
			}
		} else {
			newScreen[y] = MakeBlankLine(width)
		}
	}
	v.Screen = newScreen

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
		newAlt := make([][]Cell, height)
		for y := 0; y < height; y++ {
			if y < len(v.altScreenBuf) && len(v.altScreenBuf[y]) > 0 {
				if len(v.altScreenBuf[y]) >= width {
					newAlt[y] = v.altScreenBuf[y]
				} else {
					newAlt[y] = MakeBlankLine(width)
					copy(newAlt[y], v.altScreenBuf[y])
				}
			} else {
				newAlt[y] = MakeBlankLine(width)
			}
		}
		v.altScreenBuf = newAlt
	}

	// Keep synchronized output snapshot aligned with new size - preserve full row content
	if v.syncScreen != nil {
		newSync := make([][]Cell, height)
		for y := 0; y < height; y++ {
			if y < len(v.syncScreen) && len(v.syncScreen[y]) > 0 {
				if len(v.syncScreen[y]) >= width {
					newSync[y] = v.syncScreen[y]
				} else {
					newSync[y] = MakeBlankLine(width)
					copy(newSync[y], v.syncScreen[y])
				}
			} else {
				newSync[y] = MakeBlankLine(width)
			}
		}
		v.syncScreen = newSync
	}
	v.invalidateRenderCache()
	// Re-initialize dirty tracking for new size
	v.ensureRenderCache(height)
}

// Write processes input bytes from PTY
func (v *VTerm) Write(data []byte) {
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
	if v.ViewOffset > len(v.Scrollback) {
		v.ViewOffset = len(v.Scrollback)
	}
}

// ScreenYToAbsoluteLine converts a screen Y coordinate (0 to Height-1) to an absolute line number.
// Absolute line 0 is the first line in scrollback.
func (v *VTerm) ScreenYToAbsoluteLine(screenY int) int {
	// Total lines = scrollback + screen (respect sync snapshot if active)
	screen, scrollbackLen := v.RenderBuffers()
	screenLen := len(screen)
	totalLines := scrollbackLen + screenLen

	// The visible window starts at this absolute line
	startLine := totalLines - v.Height - v.ViewOffset
	if startLine < 0 {
		startLine = 0
	}

	return startLine + screenY
}

// AbsoluteLineToScreenY converts an absolute line number to a screen Y coordinate.
// Returns -1 if the line is not currently visible.
func (v *VTerm) AbsoluteLineToScreenY(absLine int) int {
	screen, scrollbackLen := v.RenderBuffers()
	screenLen := len(screen)
	totalLines := scrollbackLen + screenLen

	// The visible window starts at this absolute line
	startLine := totalLines - v.Height - v.ViewOffset
	if startLine < 0 {
		startLine = 0
	}

	screenY := absLine - startLine
	if screenY < 0 || screenY >= v.Height {
		return -1
	}
	return screenY
}

// ScrollView scrolls the view by delta lines (positive = up into history)
func (v *VTerm) ScrollView(delta int) {
	oldOffset := v.ViewOffset
	v.ViewOffset += delta
	maxOffset := len(v.Scrollback)
	if v.ViewOffset > maxOffset {
		v.ViewOffset = maxOffset
	}
	if v.ViewOffset < 0 {
		v.ViewOffset = 0
	}
	if v.ViewOffset != oldOffset {
		v.bumpVersion()
	}
}

// ScrollViewTo sets absolute scroll position
func (v *VTerm) ScrollViewTo(offset int) {
	oldOffset := v.ViewOffset
	v.ViewOffset = offset
	maxOffset := len(v.Scrollback)
	if v.ViewOffset > maxOffset {
		v.ViewOffset = maxOffset
	}
	if v.ViewOffset < 0 {
		v.ViewOffset = 0
	}
	if v.ViewOffset != oldOffset {
		v.bumpVersion()
	}
}

// ScrollViewToTop scrolls to oldest content
func (v *VTerm) ScrollViewToTop() {
	oldOffset := v.ViewOffset
	v.ViewOffset = len(v.Scrollback)
	if v.ViewOffset != oldOffset {
		v.bumpVersion()
	}
}

// ScrollViewToBottom returns to live view
func (v *VTerm) ScrollViewToBottom() {
	oldOffset := v.ViewOffset
	v.ViewOffset = 0
	if v.ViewOffset != oldOffset {
		v.bumpVersion()
	}
}

// IsScrolled returns true if viewing scrollback
func (v *VTerm) IsScrolled() bool {
	return v.ViewOffset > 0
}

// GetScrollInfo returns (current offset, max offset)
func (v *VTerm) GetScrollInfo() (int, int) {
	return v.ViewOffset, len(v.Scrollback)
}
