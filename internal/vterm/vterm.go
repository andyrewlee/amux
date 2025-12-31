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

	// Alt screen mode (vim, etc.)
	AltScreen    bool
	altScreenBuf [][]Cell
	altCursorX   int
	altCursorY   int

	// Scrolling region (for DECSTBM)
	ScrollTop    int
	ScrollBottom int

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
	if width == v.Width && height == v.Height {
		return
	}

	// If height shrinks, move lines to scrollback
	if height < v.Height && !v.AltScreen {
		overflow := v.Height - height
		if overflow > 0 {
			for i := 0; i < overflow; i++ {
				if len(v.Screen) > 0 {
					v.Scrollback = append(v.Scrollback, v.Screen[0])
					v.Screen = v.Screen[1:]
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
		v.Scrollback = v.Scrollback[len(v.Scrollback)-MaxScrollback:]
	}
}

// ScrollView scrolls the view by delta lines (positive = up into history)
func (v *VTerm) ScrollView(delta int) {
	v.ViewOffset += delta
	maxOffset := len(v.Scrollback)
	if v.ViewOffset > maxOffset {
		v.ViewOffset = maxOffset
	}
	if v.ViewOffset < 0 {
		v.ViewOffset = 0
	}
}

// ScrollViewTo sets absolute scroll position
func (v *VTerm) ScrollViewTo(offset int) {
	v.ViewOffset = offset
	maxOffset := len(v.Scrollback)
	if v.ViewOffset > maxOffset {
		v.ViewOffset = maxOffset
	}
	if v.ViewOffset < 0 {
		v.ViewOffset = 0
	}
}

// ScrollViewToTop scrolls to oldest content
func (v *VTerm) ScrollViewToTop() {
	v.ViewOffset = len(v.Scrollback)
}

// ScrollViewToBottom returns to live view
func (v *VTerm) ScrollViewToBottom() {
	v.ViewOffset = 0
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
