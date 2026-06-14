package common

import (
	"strings"
	"testing"
)

func TestSettingsRenderUpdateAvailable(t *testing.T) {
	dialog := NewSettingsDialog(ThemeAyuDark)
	dialog.SetUpdateInfo("v0.0.10", "v0.0.11", true)

	lines := dialog.renderLines()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Update to v0.0.11") {
		t.Fatalf("expected update line to be rendered, got:\n%s", joined)
	}
}

func TestSettingsRenderUpdateHiddenWhenUnavailable(t *testing.T) {
	dialog := NewSettingsDialog(ThemeAyuDark)
	dialog.SetUpdateInfo("v0.0.10", "", false)

	lines := dialog.renderLines()
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "Update to") {
		t.Fatalf("expected update line to be hidden, got:\n%s", joined)
	}
}

func TestSettingsRenderHomebrewHint(t *testing.T) {
	dialog := NewSettingsDialog(ThemeAyuDark)
	dialog.SetUpdateInfo("v0.0.10", "", false)
	dialog.SetUpdateHint("Installed via Homebrew - update with brew upgrade amux")

	lines := dialog.renderLines()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Installed via Homebrew - update with brew upgrade amux") {
		t.Fatalf("expected Homebrew hint to be rendered, got:\n%s", joined)
	}
}

// TestSettingsDialogFrame pins the frame geometry derived from the dialog
// style (rounded border + 1x2 padding). The offsets are exactly half the
// frame size on each axis, which the dialog uses to center its content.
func TestSettingsDialogFrame(t *testing.T) {
	// width  = border(2) + padding(2*2) = 6
	// height = border(2) + padding(2*1) = 4
	const (
		wantFrameX = 6
		wantFrameY = 4
	)

	tests := []struct {
		name   string
		width  int
		height int
	}{
		{name: "unset size", width: 0, height: 0},
		{name: "small terminal", width: 10, height: 4},
		{name: "large terminal", width: 200, height: 80},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialog := NewSettingsDialog(ThemeAyuDark)
			dialog.SetSize(tt.width, tt.height)

			frameX, frameY, offsetX, offsetY := dialog.dialogFrame()

			// Frame size is independent of the configured dialog size; it
			// is fixed by the border and padding of dialogStyle.
			if frameX != wantFrameX {
				t.Errorf("frameX = %d, want %d", frameX, wantFrameX)
			}
			if frameY != wantFrameY {
				t.Errorf("frameY = %d, want %d", frameY, wantFrameY)
			}
			if offsetX != frameX/2 {
				t.Errorf("offsetX = %d, want frameX/2 = %d", offsetX, frameX/2)
			}
			if offsetY != frameY/2 {
				t.Errorf("offsetY = %d, want frameY/2 = %d", offsetY, frameY/2)
			}
		})
	}
}

func TestSettingsDialogBounds(t *testing.T) {
	// Pin the frame size so the expected geometry below stays self-checking.
	const (
		frameX = 6
		frameY = 4
	)

	tests := []struct {
		name          string
		width         int
		height        int
		contentHeight int
		// dialogContentWidth depends on width: 40 when width<=0, else
		// clamped to [35,50] of (width-20).
		wantContentWidth int
		wantX            int
		wantY            int
	}{
		{
			name: "unset size centers nothing and clamps to origin",
			// width/height are 0, so the dialog cannot be centered and
			// both axes clamp to 0.
			width: 0, height: 0, contentHeight: 10,
			wantContentWidth: 40, wantX: 0, wantY: 0,
		},
		{
			name:  "roomy terminal centers the dialog",
			width: 120, height: 40, contentHeight: 10,
			// contentWidth = min(50, max(35, 100)) = 50
			wantContentWidth: 50,
			// w = 50+6 = 56, x = (120-56)/2 = 32
			wantX: 32,
			// h = 10+4 = 14, y = (40-14)/2 = 13
			wantY: 13,
		},
		{
			name:  "tiny terminal clamps negative coordinates to zero",
			width: 10, height: 4, contentHeight: 100,
			// contentWidth = min(50, max(35, -10)) = 35
			wantContentWidth: 35,
			// w = 35+6 = 41 > 10 -> x would be negative, clamped to 0
			wantX: 0,
			// h = 100+4 = 104 > 4 -> y would be negative, clamped to 0
			wantY: 0,
		},
		{
			name:  "zero content height still includes frame",
			width: 80, height: 24, contentHeight: 0,
			// contentWidth = min(50, max(35, 60)) = 50
			wantContentWidth: 50,
			// w = 56, x = (80-56)/2 = 12
			wantX: 12,
			// h = 0+4 = 4, y = (24-4)/2 = 10
			wantY: 10,
		},
		{
			name:  "negative content height shrinks height below frame",
			width: 80, height: 24, contentHeight: -2,
			wantContentWidth: 50,
			wantX:            12,
			// h = -2+4 = 2, y = (24-2)/2 = 11
			wantY: 11,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialog := NewSettingsDialog(ThemeAyuDark)
			dialog.SetSize(tt.width, tt.height)

			x, y, w, h := dialog.dialogBounds(tt.contentHeight)

			wantW := tt.wantContentWidth + frameX
			wantH := tt.contentHeight + frameY

			if w != wantW {
				t.Errorf("w = %d, want contentWidth+frameX = %d", w, wantW)
			}
			if h != wantH {
				t.Errorf("h = %d, want contentHeight+frameY = %d", h, wantH)
			}
			if x != tt.wantX {
				t.Errorf("x = %d, want %d", x, tt.wantX)
			}
			if y != tt.wantY {
				t.Errorf("y = %d, want %d", y, tt.wantY)
			}
			// Bounds origin must never be negative regardless of inputs.
			if x < 0 || y < 0 {
				t.Errorf("origin must be non-negative, got x=%d y=%d", x, y)
			}
		})
	}
}

// TestSettingsDialogBoundsConsistentWithFrame asserts the dialogBounds
// width/height always equal the content size plus the dialogFrame size,
// tying the two functions together as a regression guard.
func TestSettingsDialogBoundsConsistentWithFrame(t *testing.T) {
	dialog := NewSettingsDialog(ThemeAyuDark)
	dialog.SetSize(100, 50)

	frameX, frameY, _, _ := dialog.dialogFrame()
	contentWidth := dialog.dialogContentWidth()

	for _, contentHeight := range []int{0, 1, 5, 25, 200} {
		_, _, w, h := dialog.dialogBounds(contentHeight)
		if w != contentWidth+frameX {
			t.Errorf("contentHeight=%d: w=%d, want %d", contentHeight, w, contentWidth+frameX)
		}
		if h != contentHeight+frameY {
			t.Errorf("contentHeight=%d: h=%d, want %d", contentHeight, h, contentHeight+frameY)
		}
	}
}
