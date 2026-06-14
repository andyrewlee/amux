package layout

import (
	"strings"
	"testing"
)

// TestSidebarWidth verifies SidebarWidth reflects the width assigned by Resize
// across all three layout modes, including the boundary where the sidebar
// disappears.
func TestSidebarWidth(t *testing.T) {
	tests := []struct {
		name        string
		width       int
		height      int
		wantMode    LayoutMode
		wantSidebar func(m *Manager) bool
	}{
		{
			name:     "three pane keeps a positive sidebar",
			width:    200,
			height:   40,
			wantMode: LayoutThreePane,
			wantSidebar: func(m *Manager) bool {
				return m.SidebarWidth() >= m.minSidebarWidth
			},
		},
		{
			name:     "two pane collapses the sidebar to zero",
			width:    100,
			height:   40,
			wantMode: LayoutTwoPane,
			wantSidebar: func(m *Manager) bool {
				return m.SidebarWidth() == 0
			},
		},
		{
			name:     "one pane collapses the sidebar to zero",
			width:    50,
			height:   40,
			wantMode: LayoutOnePane,
			wantSidebar: func(m *Manager) bool {
				return m.SidebarWidth() == 0
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager()
			m.Resize(tt.width, tt.height)
			if m.Mode() != tt.wantMode {
				t.Fatalf("Mode() = %v, want %v", m.Mode(), tt.wantMode)
			}
			if !tt.wantSidebar(m) {
				t.Fatalf("SidebarWidth() = %d failed its constraint for mode %v",
					m.SidebarWidth(), tt.wantMode)
			}
		})
	}
}

// TestSidebarWidthZeroValue confirms a freshly-constructed manager that has not
// been resized reports a zero sidebar width.
func TestSidebarWidthZeroValue(t *testing.T) {
	m := NewManager()
	if got := m.SidebarWidth(); got != 0 {
		t.Fatalf("SidebarWidth() before Resize = %d, want 0", got)
	}
}

// TestGutters verifies the gutter accessors return the configured base outer
// gutter and that the right gutter is reduced by rightBias but never goes
// negative.
func TestGutters(t *testing.T) {
	tests := []struct {
		name          string
		rightBias     int
		wantLeft      int
		wantRight     int
		wantTop       int
		baseGutterVal int
	}{
		{
			name:          "default has symmetric outer gutters",
			rightBias:     0,
			wantLeft:      2,
			wantRight:     2,
			wantTop:       0,
			baseGutterVal: 2,
		},
		{
			name:          "right bias trims the right gutter",
			rightBias:     1,
			wantLeft:      2,
			wantRight:     1,
			wantTop:       0,
			baseGutterVal: 2,
		},
		{
			name:          "right bias larger than base clamps right gutter to zero",
			rightBias:     5,
			wantLeft:      2,
			wantRight:     0,
			wantTop:       0,
			baseGutterVal: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager()
			if m.baseOuterGutter != tt.baseGutterVal {
				t.Fatalf("baseOuterGutter = %d, want %d", m.baseOuterGutter, tt.baseGutterVal)
			}
			m.rightBias = tt.rightBias
			// Resize recomputes the gutters from baseOuterGutter and rightBias.
			m.Resize(200, 40)

			if got := m.LeftGutter(); got != tt.wantLeft {
				t.Errorf("LeftGutter() = %d, want %d", got, tt.wantLeft)
			}
			if got := m.RightGutter(); got != tt.wantRight {
				t.Errorf("RightGutter() = %d, want %d", got, tt.wantRight)
			}
			if got := m.TopGutter(); got != tt.wantTop {
				t.Errorf("TopGutter() = %d, want %d", got, tt.wantTop)
			}
		})
	}
}

// TestTopGutterCustom confirms TopGutter echoes a manually configured value and
// that Resize subtracts it from the usable height.
func TestTopGutterCustom(t *testing.T) {
	m := NewManager()
	m.topGutter = 3
	m.bottomGutter = 2
	m.Resize(200, 40)

	if got := m.TopGutter(); got != 3 {
		t.Fatalf("TopGutter() = %d, want 3", got)
	}
	// 40 - topGutter(3) - bottomGutter(2) = 35
	if got := m.Height(); got != 35 {
		t.Fatalf("Height() = %d, want 35", got)
	}
}

// TestGapX verifies GapX returns the fixed horizontal gap regardless of resize.
func TestGapX(t *testing.T) {
	m := NewManager()
	if got := m.GapX(); got != 1 {
		t.Fatalf("GapX() before Resize = %d, want 1", got)
	}
	m.Resize(200, 40)
	if got := m.GapX(); got != 1 {
		t.Fatalf("GapX() after Resize = %d, want 1", got)
	}
}

// TestHeight verifies Height tracks usable height after subtracting top/bottom
// gutters, clamping negative results to zero.
func TestHeight(t *testing.T) {
	tests := []struct {
		name         string
		topGutter    int
		bottomGutter int
		height       int
		want         int
	}{
		{name: "no gutters echoes height", topGutter: 0, bottomGutter: 0, height: 40, want: 40},
		{name: "gutters subtracted", topGutter: 2, bottomGutter: 3, height: 40, want: 35},
		{name: "exact zero", topGutter: 20, bottomGutter: 20, height: 40, want: 0},
		{name: "negative clamps to zero", topGutter: 30, bottomGutter: 30, height: 40, want: 0},
		{name: "zero height clamps to zero", topGutter: 0, bottomGutter: 0, height: 0, want: 0},
		{name: "negative height clamps to zero", topGutter: 0, bottomGutter: 0, height: -5, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager()
			m.topGutter = tt.topGutter
			m.bottomGutter = tt.bottomGutter
			m.Resize(200, tt.height)
			if got := m.Height(); got != tt.want {
				t.Fatalf("Height() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestHeightZeroValue confirms Height is zero before any Resize call.
func TestHeightZeroValue(t *testing.T) {
	m := NewManager()
	if got := m.Height(); got != 0 {
		t.Fatalf("Height() before Resize = %d, want 0", got)
	}
}

// TestRender exercises Render across every layout mode, asserting that each
// pane's content appears (or is omitted) according to the mode and that the
// top/bottom gutter padding is applied.
func TestRender(t *testing.T) {
	const (
		dash    = "DASH"
		center  = "CENTER"
		sidebar = "SIDE"
	)

	tests := []struct {
		name        string
		width       int
		height      int
		wantMode    LayoutMode
		wantPresent []string
		wantAbsent  []string
	}{
		{
			name:        "three pane renders all panes",
			width:       200,
			height:      40,
			wantMode:    LayoutThreePane,
			wantPresent: []string{dash, center, sidebar},
		},
		{
			name:        "two pane omits the sidebar",
			width:       100,
			height:      40,
			wantMode:    LayoutTwoPane,
			wantPresent: []string{dash, center},
			wantAbsent:  []string{sidebar},
		},
		{
			name:        "one pane renders only the dashboard",
			width:       50,
			height:      40,
			wantMode:    LayoutOnePane,
			wantPresent: []string{dash},
			wantAbsent:  []string{center, sidebar},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager()
			m.Resize(tt.width, tt.height)
			if m.Mode() != tt.wantMode {
				t.Fatalf("Mode() = %v, want %v", m.Mode(), tt.wantMode)
			}

			out := m.Render(dash, center, sidebar)
			for _, want := range tt.wantPresent {
				if !strings.Contains(out, want) {
					t.Errorf("Render() output missing %q\noutput:\n%s", want, out)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(out, absent) {
					t.Errorf("Render() output unexpectedly contains %q\noutput:\n%s", absent, out)
				}
			}
		})
	}
}

// TestRenderTopAndBottomPadding confirms Render prepends/appends newline padding
// equal to the top and bottom gutters.
func TestRenderTopAndBottomPadding(t *testing.T) {
	m := NewManager()
	m.topGutter = 2
	m.bottomGutter = 3
	m.Resize(200, 40)

	out := m.Render("DASH", "CENTER", "SIDE")

	if !strings.HasPrefix(out, "\n\n") {
		t.Errorf("Render() should start with %d newlines of top padding, got %q...",
			m.topGutter, out[:min(len(out), 8)])
	}
	if !strings.HasSuffix(out, "\n\n\n") {
		t.Errorf("Render() should end with %d newlines of bottom padding", m.bottomGutter)
	}
}

// TestRenderNoGutterFastPath ensures the left/right padding fast path is taken
// when both side gutters are zero: the rendered content has no leading spaces
// injected by lipgloss padding.
func TestRenderNoGutterFastPath(t *testing.T) {
	m := NewManager()
	// Force the side gutters to zero so padLines returns the view verbatim.
	m.baseOuterGutter = 0
	m.rightBias = 0
	m.gapX = 0
	m.Resize(200, 40)

	if m.LeftGutter() != 0 || m.RightGutter() != 0 {
		t.Fatalf("expected zero side gutters, got left=%d right=%d", m.LeftGutter(), m.RightGutter())
	}

	out := m.Render("DASH", "CENTER", "SIDE")
	// With no side gutters and no horizontal gap, the joined panes must not be
	// indented by padding spaces.
	if strings.HasPrefix(out, " ") {
		t.Errorf("Render() with zero gutters should not be left-padded, got %q", out)
	}
	if !strings.Contains(out, "DASH") {
		t.Errorf("Render() output missing dashboard content: %q", out)
	}
}

// TestRenderEmptyPanes verifies Render tolerates empty pane strings without
// panicking and still applies the selected mode.
func TestRenderEmptyPanes(t *testing.T) {
	m := NewManager()
	m.Resize(200, 40)

	out := m.Render("", "", "")
	if out == "" {
		// Even with empty panes, lipgloss padding emits non-empty output for a
		// three-pane layout; an empty result would signal the render path was
		// skipped entirely.
		t.Fatalf("Render() with empty panes returned an empty string")
	}
}
