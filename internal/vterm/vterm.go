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
	selActive            bool
	selStartX, selStartY int
	selEndX, selEndY     int

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

	// Version counter for snapshot caching - increments on any visible change
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
	// Enforce minimum dimensions to prevent negative cursor positions
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	if width == v.Width && height == v.Height {
		return
	}

	// If height shrinks, move lines to scrollback
	if height < v.Height && !v.AltScreen {
		overflow := v.Height - height
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

	// Resize screen buffer
	newScreen := v.makeScreen(width, height)
	for y := 0; y < min(height, len(v.Screen)); y++ {
		for x := 0; x < min(width, len(v.Screen[y])); x++ {
			newScreen[y][x] = v.Screen[y][x]
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

	// Also resize alt screen if it exists
	if v.altScreenBuf != nil {
		newAlt := v.makeScreen(width, height)
		for y := 0; y < min(height, len(v.altScreenBuf)); y++ {
			for x := 0; x < min(width, len(v.altScreenBuf[y])); x++ {
				newAlt[y][x] = v.altScreenBuf[y][x]
			}
		}
		v.altScreenBuf = newAlt
	}

	// Keep synchronized output snapshot aligned with new size
	if v.syncScreen != nil {
		newSync := v.makeScreen(width, height)
		for y := 0; y < min(height, len(v.syncScreen)); y++ {
			for x := 0; x < min(width, len(v.syncScreen[y])); x++ {
				newSync[y][x] = v.syncScreen[y][x]
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

// SyncActive reports whether synchronized output is currently active.
func (v *VTerm) SyncActive() bool {
	return v.syncActive
}

// respond sends a response back to the PTY (for terminal queries)
func (v *VTerm) respond(data []byte) {
	if v.responseWriter != nil {
		v.responseWriter(data)
	}
}

func (v *VTerm) setSynchronizedOutput(active bool) {
	if active {
		if v.syncActive {
			return
		}
		v.syncActive = true
		v.syncScreen = copyScreen(v.Screen)
		v.syncScrollbackLen = len(v.Scrollback)
		v.invalidateRenderCache()
		return
	}

	if !v.syncActive {
		return
	}
	v.syncActive = false
	v.syncScreen = nil
	v.syncScrollbackLen = 0
	if v.syncDeferTrim {
		v.syncDeferTrim = false
		v.trimScrollback()
	}
	v.invalidateRenderCache()
}

func copyScreen(src [][]Cell) [][]Cell {
	dst := make([][]Cell, len(src))
	for i := range src {
		dst[i] = CopyLine(src[i])
	}
	return dst
}

// trimScrollback keeps scrollback under MaxScrollback
func (v *VTerm) trimScrollback() {
	if len(v.Scrollback) > MaxScrollback {
		if v.syncActive {
			v.syncDeferTrim = true
			return
		}
		v.Scrollback = v.Scrollback[len(v.Scrollback)-MaxScrollback:]
	}
	// Clamp ViewOffset after trim to prevent stale offsets
	if v.ViewOffset > len(v.Scrollback) {
		v.ViewOffset = len(v.Scrollback)
	}
}

// ScrollView scrolls the view by delta lines (positive = up into history)
func (v *VTerm) ScrollView(delta int) {
	v.ClearSelection()
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
	v.ClearSelection()
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
	v.ClearSelection()
	oldOffset := v.ViewOffset
	v.ViewOffset = len(v.Scrollback)
	if v.ViewOffset != oldOffset {
		v.bumpVersion()
	}
}

// ScrollViewToBottom returns to live view
func (v *VTerm) ScrollViewToBottom() {
	v.ClearSelection()
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (v *VTerm) ensureRenderCache(height int) {
	if len(v.renderCache) != height {
		v.renderCache = make([]string, height)
		v.renderDirty = make([]bool, height)
		v.renderDirtyAll = true
	}
}

func (v *VTerm) markDirtyLine(y int) {
	if y < 0 || y >= v.Height {
		return
	}
	v.bumpVersion()
	if len(v.renderDirty) == v.Height {
		v.renderDirty[y] = true
	} else {
		v.renderDirtyAll = true
	}
}

func (v *VTerm) markDirtyRange(start, end int) {
	if start < 0 {
		start = 0
	}
	if end >= v.Height {
		end = v.Height - 1
	}
	if start > end {
		return
	}
	v.bumpVersion()
	if len(v.renderDirty) == v.Height {
		for y := start; y <= end; y++ {
			v.renderDirty[y] = true
		}
		return
	}
	v.renderDirtyAll = true
}

func (v *VTerm) invalidateRenderCache() {
	v.renderCache = nil
	v.renderDirty = nil
	v.renderDirtyAll = true
	v.bumpVersion()
}

// DirtyLines returns the dirty line flags and whether all lines are dirty.
// This is used by VTermLayer for optimized rendering.
func (v *VTerm) DirtyLines() ([]bool, bool) {
	// When scrolled, we can't use dirty tracking effectively
	if v.ViewOffset > 0 {
		return nil, true
	}
	// When sync is active, always redraw
	if v.syncActive {
		return nil, true
	}
	return v.renderDirty, v.renderDirtyAll
}

// ClearDirty resets dirty tracking state after a render.
func (v *VTerm) ClearDirty() {
	v.renderDirtyAll = false
	for i := range v.renderDirty {
		v.renderDirty[i] = false
	}
}

// ClearDirtyWithCursor resets dirty tracking state and updates cursor tracking.
// This should be called after snapshotting to track cursor position changes.
func (v *VTerm) ClearDirtyWithCursor(showCursor bool) {
	v.renderDirtyAll = false
	for i := range v.renderDirty {
		v.renderDirty[i] = false
	}
	// Track cursor state for next frame's dirty detection
	v.lastShowCursor = showCursor
	v.lastCursorHidden = v.CursorHidden
	v.lastCursorX = v.CursorX
	v.lastCursorY = v.CursorY
}

// LastCursorY returns the cursor Y position from the previous render frame.
// Used to detect cursor movement and mark old cursor line dirty.
func (v *VTerm) LastCursorY() int {
	return v.lastCursorY
}

// Version returns the current version counter.
// This increments whenever visible content changes.
func (v *VTerm) Version() uint64 {
	return v.version
}

// bumpVersion increments the version counter.
// Called internally when content changes.
func (v *VTerm) bumpVersion() {
	v.version++
}
