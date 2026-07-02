package common

import (
	"testing"

	"github.com/andyrewlee/amux/internal/vterm"
)

// seedSelection returns a VTerm with an active one-point selection at
// (startX, startLine) so ExtendSelection has a valid anchor to extend from.
func seedSelection(startX, startLine int) (*vterm.VTerm, *SelectionState) {
	v := vterm.New(80, 24)
	v.SetSelection(startX, startLine, startX, startLine, true, false)
	sel := &SelectionState{
		Active:    true,
		StartX:    startX,
		StartLine: startLine,
		EndX:      startX,
		EndLine:   startLine,
	}
	return v, sel
}

func TestDragSelect(t *testing.T) {
	t.Parallel()

	const termWidth, termHeight = 80, 24

	tests := []struct {
		name string
		// Drag pointer position, in terminal coordinates.
		termX, termY int
		// Scroll direction state going in.
		startDir    int
		startActive bool
		// Expectations.
		wantScrollDelta int  // 0 means scroll closure not invoked
		wantAbsScreenY  int  // screen row passed to screenYToAbs
		wantLastTermX   int  // value written to *lastTermX
		wantDir         int  // scrollScroll.ScrollDir after the call
		wantNeedTick    bool // returned needTick
	}{
		{
			name:  "in-bounds drag does not scroll",
			termX: 10, termY: 5,
			wantScrollDelta: 0,
			wantAbsScreenY:  5,
			wantLastTermX:   10,
			wantDir:         0,
			wantNeedTick:    false,
		},
		{
			name:  "clamps negative X to zero",
			termX: -7, termY: 5,
			wantScrollDelta: 0,
			wantAbsScreenY:  5,
			wantLastTermX:   0,
			wantDir:         0,
			wantNeedTick:    false,
		},
		{
			name:  "clamps X past width to width-1",
			termX: 999, termY: 5,
			wantScrollDelta: 0,
			wantAbsScreenY:  5,
			wantLastTermX:   termWidth - 1,
			wantDir:         0,
			wantNeedTick:    false,
		},
		{
			name:  "above viewport scrolls up and pins Y to top, needs tick",
			termX: 10, termY: -3,
			wantScrollDelta: 1,
			wantAbsScreenY:  0,
			wantLastTermX:   10,
			wantDir:         1,
			wantNeedTick:    true,
		},
		{
			name:  "below viewport scrolls down and pins Y to bottom, needs tick",
			termX: 10, termY: termHeight + 4,
			wantScrollDelta: -1,
			wantAbsScreenY:  termHeight - 1,
			wantLastTermX:   10,
			wantDir:         -1,
			wantNeedTick:    true,
		},
		{
			// Motion while a chain is active re-requests the expected tick
			// sequence (self-healing when a tick was shed by a queue).
			name:  "already-active tick loop re-requests the chain",
			termX: 10, termY: -3,
			startDir: 1, startActive: true,
			wantScrollDelta: 1,
			wantAbsScreenY:  0,
			wantLastTermX:   10,
			wantDir:         1,
			wantNeedTick:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, sel := seedSelection(3, 3)
			scrollState := &SelectionScrollState{ScrollDir: tt.startDir, Active: tt.startActive}

			var gotScrollDelta int
			var gotScreenY int
			lastTermX := -999

			needTick, gen, seq := DragSelect(
				v, sel, scrollState,
				tt.termX, tt.termY, termWidth, termHeight,
				&lastTermX,
				func(delta int) { gotScrollDelta += delta },
				func(screenY int) int { gotScreenY = screenY; return screenY * 100 },
			)

			if gotScrollDelta != tt.wantScrollDelta {
				t.Errorf("scroll delta = %d, want %d", gotScrollDelta, tt.wantScrollDelta)
			}
			if gotScreenY != tt.wantAbsScreenY {
				t.Errorf("screenYToAbs got screenY %d, want %d", gotScreenY, tt.wantAbsScreenY)
			}
			if lastTermX != tt.wantLastTermX {
				t.Errorf("*lastTermX = %d, want %d", lastTermX, tt.wantLastTermX)
			}
			if scrollState.ScrollDir != tt.wantDir {
				t.Errorf("ScrollDir = %d, want %d", scrollState.ScrollDir, tt.wantDir)
			}
			if needTick != tt.wantNeedTick {
				t.Errorf("needTick = %v, want %v", needTick, tt.wantNeedTick)
			}
			// The selection endpoint X must track the clamped termX, and its
			// end line must be the absLine the closure returned (screenY*100).
			if sel.EndX != tt.wantLastTermX {
				t.Errorf("sel.EndX = %d, want %d", sel.EndX, tt.wantLastTermX)
			}
			if sel.EndLine != tt.wantAbsScreenY*100 {
				t.Errorf("sel.EndLine = %d, want %d", sel.EndLine, tt.wantAbsScreenY*100)
			}
			if needTick && gen == 0 {
				t.Error("needTick true but gen is zero")
			}
			if needTick && seq == 0 {
				t.Error("needTick true but seq is zero")
			}
		})
	}
}

func TestSelectionScrollTickStep(t *testing.T) {
	t.Parallel()

	const termHeight = 24

	tests := []struct {
		name            string
		scrollDir       int
		lastTermX       int
		wantScrollDelta int
		wantScreenY     int // edge row mapped to abs line
	}{
		{
			name:            "scrolling up extends to top edge",
			scrollDir:       1,
			lastTermX:       12,
			wantScrollDelta: 1,
			wantScreenY:     0,
		},
		{
			name:            "scrolling down extends to bottom edge",
			scrollDir:       -1,
			lastTermX:       7,
			wantScrollDelta: -1,
			wantScreenY:     termHeight - 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, sel := seedSelection(3, 3)
			scrollState := &SelectionScrollState{ScrollDir: tt.scrollDir, Active: true}

			var gotScrollDelta int
			var gotScreenY int

			SelectionScrollTickStep(
				v, sel, scrollState,
				termHeight, tt.lastTermX,
				func(delta int) { gotScrollDelta += delta },
				func(screenY int) int { gotScreenY = screenY; return screenY * 100 },
			)

			if gotScrollDelta != tt.wantScrollDelta {
				t.Errorf("scroll delta = %d, want %d", gotScrollDelta, tt.wantScrollDelta)
			}
			if gotScreenY != tt.wantScreenY {
				t.Errorf("screenYToAbs got screenY %d, want %d", gotScreenY, tt.wantScreenY)
			}
			if sel.EndX != tt.lastTermX {
				t.Errorf("sel.EndX = %d, want %d (lastTermX)", sel.EndX, tt.lastTermX)
			}
			if sel.EndLine != tt.wantScreenY*100 {
				t.Errorf("sel.EndLine = %d, want %d", sel.EndLine, tt.wantScreenY*100)
			}
		})
	}
}
