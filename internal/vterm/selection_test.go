package vterm

import (
	"testing"
)

// newVTermWith builds a VTerm of the given screen size and replaces its
// scrollback and screen with deterministic text rows. Each string in
// scrollback/screen becomes one row; the row width matches the VTerm width.
func newVTermWith(width, height int, scrollback, screen []string) *VTerm {
	vt := New(width, height)
	vt.Scrollback = vt.Scrollback[:0]
	for _, s := range scrollback {
		vt.Scrollback = append(vt.Scrollback, lineFromString(width, s))
	}
	for i, s := range screen {
		if i >= len(vt.Screen) {
			break
		}
		vt.Screen[i] = lineFromString(width, s)
	}
	return vt
}

func TestHasSelection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		set  func(v *VTerm)
		want bool
	}{
		{
			name: "fresh vterm has no selection",
			set:  func(v *VTerm) {},
			want: false,
		},
		{
			name: "after SetSelection active=true",
			set:  func(v *VTerm) { v.SetSelection(0, 0, 1, 0, true, false) },
			want: true,
		},
		{
			name: "after SetSelection active=false",
			set:  func(v *VTerm) { v.SetSelection(0, 0, 1, 0, false, false) },
			want: false,
		},
		{
			name: "set active then cleared",
			set: func(v *VTerm) {
				v.SetSelection(0, 0, 1, 0, true, false)
				v.ClearSelection()
			},
			want: false,
		},
		{
			name: "rectangular selection still active",
			set:  func(v *VTerm) { v.SetSelection(0, 0, 2, 2, true, true) },
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vt := New(4, 2)
			tt.set(vt)
			if got := vt.HasSelection(); got != tt.want {
				t.Fatalf("HasSelection() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectedText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		width      int
		height     int
		scrollback []string
		screen     []string
		// selection coordinates (absolute line indices for the combined buffer)
		startX, startLine, endX, endLine int
		want                             string
	}{
		{
			name:      "single cell on screen",
			width:     5,
			height:    1,
			screen:    []string{"hello"},
			startX:    0,
			startLine: 0,
			endX:      0,
			endLine:   0,
			want:      "h",
		},
		{
			name:      "partial single line",
			width:     5,
			height:    1,
			screen:    []string{"hello"},
			startX:    1,
			startLine: 0,
			endX:      3,
			endLine:   0,
			want:      "ell",
		},
		{
			name:      "full single line trims trailing blanks",
			width:     8,
			height:    1,
			screen:    []string{"hi"},
			startX:    0,
			startLine: 0,
			endX:      7,
			endLine:   0,
			want:      "hi",
		},
		{
			name:       "spans scrollback into screen",
			width:      4,
			height:     1,
			scrollback: []string{"abcd"},
			screen:     []string{"efgh"},
			startX:     0,
			startLine:  0,
			endX:       3,
			endLine:    1,
			want:       "abcd\nefgh",
		},
		{
			name:      "reversed coordinates are normalized",
			width:     5,
			height:    1,
			screen:    []string{"world"},
			startX:    3,
			startLine: 0,
			endX:      1,
			endLine:   0,
			want:      "orl",
		},
		{
			name:       "reversed multi-line coordinates normalized",
			width:      4,
			height:     1,
			scrollback: []string{"abcd"},
			screen:     []string{"efgh"},
			startX:     2,
			startLine:  1,
			endX:       1,
			endLine:    0,
			want:       "bcd\nefg",
		},
		{
			name:      "negative coordinates clamp to start",
			width:     5,
			height:    1,
			screen:    []string{"clamp"},
			startX:    -3,
			startLine: -2,
			endX:      2,
			endLine:   0,
			want:      "cla",
		},
		{
			name:      "out-of-range end clamps to buffer bounds",
			width:     5,
			height:    1,
			screen:    []string{"edge"},
			startX:    0,
			startLine: 0,
			endX:      99,
			endLine:   99,
			want:      "edge",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vt := newVTermWith(tt.width, tt.height, tt.scrollback, tt.screen)
			vt.SetSelection(tt.startX, tt.startLine, tt.endX, tt.endLine, true, false)
			if got := vt.SelectedText(); got != tt.want {
				t.Fatalf("SelectedText() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSelectedTextIgnoresActiveFlag documents that SelectedText reads the
// stored coordinates even after ClearSelection flips selActive to false, which
// is what its doc comment promises for callers holding their own Active flag.
func TestSelectedTextIgnoresActiveFlag(t *testing.T) {
	t.Parallel()
	vt := newVTermWith(5, 1, nil, []string{"hello"})
	vt.SetSelection(0, 0, 4, 0, true, false)
	vt.ClearSelection()

	if vt.HasSelection() {
		t.Fatalf("expected selection inactive after ClearSelection")
	}
	if got := vt.SelectedText(); got != "hello" {
		t.Fatalf("SelectedText() after clear = %q, want %q", got, "hello")
	}
}

func TestSelectedTextEmptyBuffer(t *testing.T) {
	t.Parallel()
	// A VTerm whose combined buffer is empty must yield empty text.
	vt := &VTerm{Width: 5}
	if got := vt.SelectedText(); got != "" {
		t.Fatalf("SelectedText() on empty buffer = %q, want empty", got)
	}
}

func TestGetTextRangeNilReceiver(t *testing.T) {
	t.Parallel()
	var vt *VTerm
	if got := vt.GetTextRange(0, 0, 0, 0); got != "" {
		t.Fatalf("GetTextRange() on nil receiver = %q, want empty", got)
	}
}

func TestLineCells(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		width      int
		height     int
		scrollback []string
		screen     []string
		line       int
		wantText   string // expected leading text of the row, "" when nil expected
		wantNil    bool
	}{
		{
			name:    "negative index returns nil",
			width:   4,
			height:  1,
			screen:  []string{"abcd"},
			line:    -1,
			wantNil: true,
		},
		{
			name:       "first scrollback line",
			width:      4,
			height:     1,
			scrollback: []string{"scr0"},
			screen:     []string{"scrn"},
			line:       0,
			wantText:   "scr0",
		},
		{
			name:       "last scrollback line",
			width:      4,
			height:     1,
			scrollback: []string{"scr0", "scr1"},
			screen:     []string{"scrn"},
			line:       1,
			wantText:   "scr1",
		},
		{
			name:       "first screen line after scrollback",
			width:      4,
			height:     2,
			scrollback: []string{"scr0"},
			screen:     []string{"row0", "row1"},
			line:       1,
			wantText:   "row0",
		},
		{
			name:       "last screen line",
			width:      4,
			height:     2,
			scrollback: []string{"scr0"},
			screen:     []string{"row0", "row1"},
			line:       2,
			wantText:   "row1",
		},
		{
			name:    "index past end returns nil",
			width:   4,
			height:  1,
			screen:  []string{"abcd"},
			line:    99,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vt := newVTermWith(tt.width, tt.height, tt.scrollback, tt.screen)
			got := vt.LineCells(tt.line)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("LineCells(%d) = %v, want nil", tt.line, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("LineCells(%d) = nil, want row %q", tt.line, tt.wantText)
			}
			if text := cellRowText(got); text != tt.wantText {
				t.Fatalf("LineCells(%d) text = %q, want %q", tt.line, text, tt.wantText)
			}
		})
	}
}

// TestLineCellsReturnsBackingSlice verifies LineCells exposes the live backing
// row (not a copy), which callers rely on for selection rendering. It asserts
// backing-array identity rather than value equality: mutating through the
// returned slice must be observable in the underlying buffer, so the test fails
// if LineCells ever starts returning a copy.
func TestLineCellsReturnsBackingSlice(t *testing.T) {
	t.Parallel()
	vt := newVTermWith(4, 1, []string{"abcd"}, []string{"efgh"})

	// Scrollback row: mutate through the returned slice and observe it in the
	// live scrollback backing row. Also confirm element-address identity.
	got := vt.LineCells(0)
	if len(got) == 0 || &got[0] != &vt.Scrollback[0][0] {
		t.Fatalf("LineCells(0) did not return the live scrollback backing row")
	}
	got[0] = Cell{Rune: 'Z', Width: 1}
	if vt.Scrollback[0][0].Rune != 'Z' {
		t.Fatalf("LineCells(0) did not return the live scrollback backing row")
	}

	// Screen row: LineCells(1) maps to vt.Screen[0] (scrollbackLen==1, idx=0).
	gotScreen := vt.LineCells(1)
	if len(gotScreen) == 0 || &gotScreen[0] != &vt.Screen[0][0] {
		t.Fatalf("LineCells(1) did not return the live screen backing row")
	}
	gotScreen[0] = Cell{Rune: 'Y', Width: 1}
	if vt.Screen[0][0].Rune != 'Y' {
		t.Fatalf("LineCells(1) did not return the live screen backing row")
	}
}

func TestLineCellsNilReceiver(t *testing.T) {
	t.Parallel()
	var vt *VTerm
	if got := vt.LineCells(0); got != nil {
		t.Fatalf("LineCells() on nil receiver = %v, want nil", got)
	}
}

// cellRowText extracts the rune text of a cell row, ignoring trailing blanks.
func cellRowText(row []Cell) string {
	var b []rune
	for _, c := range row {
		if c.Rune == 0 {
			b = append(b, ' ')
			continue
		}
		b = append(b, c.Rune)
	}
	// trim trailing spaces
	end := len(b)
	for end > 0 && b[end-1] == ' ' {
		end--
	}
	return string(b[:end])
}
