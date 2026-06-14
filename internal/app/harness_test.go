package app

import (
	"strconv"
	"strings"
	"testing"
)

// newCenterHarnessForStep builds a center-mode harness with the supplied
// hot-tab / payload knobs. It is a thin convenience over NewHarness used by the
// Step tests, which need direct access to the per-tab terminals.
func newCenterHarnessForStep(t *testing.T, tabs, hotTabs, payloadBytes, newlineEvery int) *Harness {
	t.Helper()
	h, err := NewHarness(HarnessOptions{
		Mode:         HarnessCenter,
		Tabs:         tabs,
		Width:        120,
		Height:       40,
		HotTabs:      hotTabs,
		PayloadBytes: payloadBytes,
		NewlineEvery: newlineEvery,
	})
	if err != nil {
		t.Fatalf("center harness init: %v", err)
	}
	return h
}

// TestNewMonitorHarness_ReusesSidebarWiringWithMonitorMode verifies the monitor
// harness is a sidebar harness with only the mode switched: it shares the
// sidebar terminal wiring (no center tabs) but reports HarnessMonitor so callers
// can distinguish it.
func TestNewMonitorHarness_ReusesSidebarWiringWithMonitorMode(t *testing.T) {
	h, err := NewHarness(HarnessOptions{
		Mode:    HarnessMonitor,
		Tabs:    4,
		Width:   120,
		Height:  40,
		HotTabs: 2,
	})
	if err != nil {
		t.Fatalf("monitor harness init: %v", err)
	}

	if h.mode != HarnessMonitor {
		t.Fatalf("mode = %q, want %q", h.mode, HarnessMonitor)
	}
	// Monitor reuses the sidebar harness, which streams into a single sidebar
	// terminal rather than per-center tabs.
	if h.sidebarTerm == nil {
		t.Fatal("expected sidebar terminal wiring on monitor harness, got nil")
	}
	if len(h.tabs) != 0 {
		t.Fatalf("monitor harness should not build center tabs, got %d", len(h.tabs))
	}
	if h.app == nil {
		t.Fatal("expected app to be wired on monitor harness")
	}
}

// TestNewMonitorHarness_InheritsHarnessOptions confirms the monitor harness
// carries through the configurable knobs (hot tabs, payload sizing, newline
// cadence) onto the Harness it produces, since it delegates to the sidebar
// constructor before flipping the mode.
func TestNewMonitorHarness_InheritsHarnessOptions(t *testing.T) {
	const (
		hot          = 3
		payloadBytes = 48
		newlineEvery = 5
	)
	h, err := NewHarness(HarnessOptions{
		Mode:         HarnessMonitor,
		Tabs:         8,
		Width:        100,
		Height:       30,
		HotTabs:      hot,
		PayloadBytes: payloadBytes,
		NewlineEvery: newlineEvery,
	})
	if err != nil {
		t.Fatalf("monitor harness init: %v", err)
	}

	if h.hotTabs != hot {
		t.Errorf("hotTabs = %d, want %d", h.hotTabs, hot)
	}
	if h.payloadBytes != payloadBytes {
		t.Errorf("payloadBytes = %d, want %d", h.payloadBytes, payloadBytes)
	}
	if h.newlineEvery != newlineEvery {
		t.Errorf("newlineEvery = %d, want %d", h.newlineEvery, newlineEvery)
	}
	// The payload buffer is pre-sized for payloadBytes plus headroom and the
	// spinner glyphs are seeded; both are inherited from the sidebar path.
	if cap(h.payloadBuf) < payloadBytes {
		t.Errorf("payloadBuf cap = %d, want >= %d", cap(h.payloadBuf), payloadBytes)
	}
	if string(h.spinner) != `|/-\` {
		t.Errorf("spinner = %q, want %q", string(h.spinner), `|/-\`)
	}
}

// TestNewMonitorHarness_DiffersFromSidebarOnlyInMode pins the documented
// invariant that monitor and sidebar harnesses are identically wired apart from
// the mode tag, which is what lets the golden test diverge them on render input
// rather than structure.
func TestNewMonitorHarness_DiffersFromSidebarOnlyInMode(t *testing.T) {
	opts := HarnessOptions{
		Mode:    HarnessSidebar,
		Tabs:    6,
		Width:   120,
		Height:  40,
		HotTabs: 1,
	}
	sidebar, err := NewHarness(opts)
	if err != nil {
		t.Fatalf("sidebar harness init: %v", err)
	}

	opts.Mode = HarnessMonitor
	monitor, err := NewHarness(opts)
	if err != nil {
		t.Fatalf("monitor harness init: %v", err)
	}

	if sidebar.mode != HarnessSidebar {
		t.Fatalf("sidebar mode = %q, want %q", sidebar.mode, HarnessSidebar)
	}
	if monitor.mode != HarnessMonitor {
		t.Fatalf("monitor mode = %q, want %q", monitor.mode, HarnessMonitor)
	}
	if (monitor.sidebarTerm == nil) != (sidebar.sidebarTerm == nil) {
		t.Fatal("monitor and sidebar should agree on sidebar-terminal wiring")
	}
	if len(monitor.tabs) != len(sidebar.tabs) {
		t.Fatalf("monitor tabs = %d, sidebar tabs = %d; expected identical structure",
			len(monitor.tabs), len(sidebar.tabs))
	}
}

// TestBuildPayload_FormatAndLength exercises the payload formatting across a
// range of sizes and frame numbers. Every payload must start with the framed
// header and be padded out to at least payloadBytes.
func TestBuildPayload_FormatAndLength(t *testing.T) {
	tests := []struct {
		name         string
		payloadBytes int
		newlineEvery int
		frame        int
	}{
		{name: "default size frame 0", payloadBytes: 64, frame: 0},
		{name: "default size mid frame", payloadBytes: 64, frame: 7},
		{name: "small payload", payloadBytes: 16, frame: 3},
		{name: "large frame number", payloadBytes: 32, frame: 123456},
		{name: "header longer than payload", payloadBytes: 1, frame: 999},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Harness{
				payloadBytes: tt.payloadBytes,
				newlineEvery: tt.newlineEvery,
				payloadBuf:   make([]byte, 0, tt.payloadBytes+32),
				spinner:      []byte{'|', '/', '-', '\\'},
			}

			got := h.buildPayload(tt.frame)

			// Header: carriage-return + "frame <n> <spinner>".
			wantPrefix := "\rframe " + strconv.Itoa(tt.frame) + " "
			if !strings.HasPrefix(string(got), wantPrefix) {
				t.Fatalf("payload %q missing prefix %q", got, wantPrefix)
			}
			// Padding must bring it to at least payloadBytes; when the header
			// already exceeds payloadBytes there is no padding but the header
			// still survives in full.
			minLen := tt.payloadBytes
			if len(wantPrefix)+1 > minLen { // +1 for the spinner glyph
				minLen = len(wantPrefix) + 1
			}
			if len(got) < minLen {
				t.Fatalf("payload length = %d, want >= %d (%q)", len(got), minLen, got)
			}
		})
	}
}

// TestBuildPayload_SpinnerCycles confirms the spinner glyph advances with the
// frame counter, cycling through the four-glyph set deterministically.
func TestBuildPayload_SpinnerCycles(t *testing.T) {
	h := &Harness{
		payloadBytes: 16,
		payloadBuf:   make([]byte, 0, 48),
		spinner:      []byte{'|', '/', '-', '\\'},
	}

	wantGlyph := []byte{'|', '/', '-', '\\', '|', '/'}
	for frame, want := range wantGlyph {
		got := h.buildPayload(frame)
		// Glyph sits right after "\rframe <n> ".
		header := "\rframe " + strconv.Itoa(frame) + " "
		glyph := got[len(header)]
		if glyph != want {
			t.Errorf("frame %d spinner = %q, want %q", frame, string(glyph), string(want))
		}
	}
}

// TestBuildPayload_NewlineCadence verifies a trailing newline is appended only
// when newlineEvery is positive and evenly divides the frame, and is absent for
// the disabled (zero) and off-beat cases.
func TestBuildPayload_NewlineCadence(t *testing.T) {
	tests := []struct {
		name         string
		newlineEvery int
		frame        int
		wantNewline  bool
	}{
		{name: "disabled never appends", newlineEvery: 0, frame: 0, wantNewline: false},
		{name: "disabled mid frame", newlineEvery: 0, frame: 9, wantNewline: false},
		{name: "every-1 always appends", newlineEvery: 1, frame: 3, wantNewline: true},
		{name: "on the beat", newlineEvery: 4, frame: 8, wantNewline: true},
		{name: "frame zero is on every beat", newlineEvery: 4, frame: 0, wantNewline: true},
		{name: "off the beat", newlineEvery: 4, frame: 5, wantNewline: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Harness{
				payloadBytes: 16,
				newlineEvery: tt.newlineEvery,
				payloadBuf:   make([]byte, 0, 48),
				spinner:      []byte{'|', '/', '-', '\\'},
			}

			got := h.buildPayload(tt.frame)

			hasNewline := strings.HasSuffix(string(got), "\n")
			if hasNewline != tt.wantNewline {
				t.Fatalf("frame %d newlineEvery %d: trailing newline = %v, want %v (%q)",
					tt.frame, tt.newlineEvery, hasNewline, tt.wantNewline, got)
			}
		})
	}
}

// TestBuildPayload_ReusesBackingBuffer documents the allocation contract: the
// returned slice aliases the harness's reusable payloadBuf, so successive calls
// reuse the same backing array (the last call's bytes overwrite the previous).
func TestBuildPayload_ReusesBackingBuffer(t *testing.T) {
	h := &Harness{
		payloadBytes: 32,
		payloadBuf:   make([]byte, 0, 64),
		spinner:      []byte{'|', '/', '-', '\\'},
	}

	first := h.buildPayload(0)
	firstCap := cap(first)
	second := h.buildPayload(1)

	// Same backing array: cap is preserved and no reallocation happened because
	// payloadBytes still fits the existing buffer.
	if cap(second) != firstCap {
		t.Errorf("buffer reallocated unexpectedly: cap %d -> %d", firstCap, cap(second))
	}
	if &first[:1][0] != &second[:1][0] {
		t.Error("expected buildPayload to reuse the same backing buffer across calls")
	}
}

// TestBuildPayload_GrowsBufferWhenPayloadExceedsCap covers the resize branch:
// when payloadBytes outgrows the current buffer capacity, buildPayload must
// allocate a larger buffer and still produce a well-formed payload.
func TestBuildPayload_GrowsBufferWhenPayloadExceedsCap(t *testing.T) {
	h := &Harness{
		payloadBytes: 256, // far larger than the seeded cap
		payloadBuf:   make([]byte, 0, 8),
		spinner:      []byte{'|', '/', '-', '\\'},
	}

	got := h.buildPayload(0)

	if len(got) < 256 {
		t.Fatalf("payload length = %d, want >= 256 after grow", len(got))
	}
	if cap(h.payloadBuf) < 256 {
		t.Fatalf("payloadBuf cap = %d, want >= 256 after grow", cap(h.payloadBuf))
	}
	if !strings.HasPrefix(string(got), "\rframe 0 ") {
		t.Fatalf("payload lost its header after grow: %q", got[:16])
	}
}

// TestBuildPayload_EmptySpinnerOmitsGlyph guards the defensive len(h.spinner)>0
// branch: with no spinner configured the header is emitted without a glyph and
// padding still fills the payload to size.
func TestBuildPayload_EmptySpinnerOmitsGlyph(t *testing.T) {
	h := &Harness{
		payloadBytes: 16,
		payloadBuf:   make([]byte, 0, 48),
		spinner:      nil,
	}

	got := h.buildPayload(2)

	// "\rframe 2 " then immediately padding 'x', no spinner glyph in between.
	if !strings.HasPrefix(string(got), "\rframe 2 x") {
		t.Fatalf("expected header followed directly by padding, got %q", got)
	}
	if len(got) < 16 {
		t.Fatalf("payload length = %d, want >= 16", len(got))
	}
}

// TestStep_NilHarnessAndZeroHotTabsAreNoops covers the two early-return guards:
// a nil receiver and a harness with no hot tabs must both return without writing
// or panicking.
func TestStep_NilHarnessAndZeroHotTabsAreNoops(t *testing.T) {
	t.Run("nil harness", func(t *testing.T) {
		var h *Harness
		h.Step(0) // must not panic
	})

	t.Run("zero hot tabs leaves terminals untouched", func(t *testing.T) {
		h := newCenterHarnessForStep(t, 3, 0, 64, 0)
		versionsBefore := make([]uint64, len(h.tabs))
		for i, tab := range h.tabs {
			versionsBefore[i] = tab.Terminal.Version()
		}

		h.Step(0)

		for i, tab := range h.tabs {
			if got := tab.Terminal.Version(); got != versionsBefore[i] {
				t.Fatalf("tab %d version changed (%d -> %d); Step with hotTabs=0 must be inert",
					i, versionsBefore[i], got)
			}
		}
	})
}

// TestStep_CenterWritesOnlyHotTabs verifies center-mode Step streams the frame
// payload into exactly the first hotTabs terminals and leaves the cold tabs
// blank.
func TestStep_CenterWritesOnlyHotTabs(t *testing.T) {
	const (
		tabs = 4
		hot  = 2
	)
	h := newCenterHarnessForStep(t, tabs, hot, 64, 0)

	h.Step(0)

	for i, tab := range h.tabs {
		rendered := tab.Terminal.Render()
		contains := strings.Contains(rendered, "frame 0")
		if i < hot {
			if !contains {
				t.Errorf("hot tab %d should contain streamed payload, got %q", i, rendered)
			}
		} else if contains {
			t.Errorf("cold tab %d should be blank, got %q", i, rendered)
		}
	}
}

// TestStep_CenterClampsHotTabsToTabCount checks the i < len(h.tabs) bound: a
// hotTabs value larger than the number of tabs must write every tab without
// indexing past the slice.
func TestStep_CenterClampsHotTabsToTabCount(t *testing.T) {
	const tabs = 2
	h := newCenterHarnessForStep(t, tabs, 10 /* more hot than tabs */, 64, 0)

	h.Step(0) // must not panic on the out-of-range hot count

	for i, tab := range h.tabs {
		if rendered := tab.Terminal.Render(); !strings.Contains(rendered, "frame 0") {
			t.Errorf("tab %d should have received payload when hotTabs exceeds tab count, got %q", i, rendered)
		}
	}
}

// TestStep_CenterAccumulatesAcrossFrames confirms successive Step calls feed
// distinct frame payloads into the same terminal, so the latest frame number is
// visible.
func TestStep_CenterAccumulatesAcrossFrames(t *testing.T) {
	h := newCenterHarnessForStep(t, 1, 1, 64, 0)

	versionStart := h.tabs[0].Terminal.Version()
	h.Step(0)
	h.Step(1)
	h.Step(2)

	if h.tabs[0].Terminal.Version() <= versionStart {
		t.Fatal("expected terminal version to advance after Step writes")
	}
	if rendered := h.tabs[0].Terminal.Render(); !strings.Contains(rendered, "frame 2") {
		t.Fatalf("expected latest frame 2 to be visible, got %q", rendered)
	}
}

// TestStep_SidebarAndMonitorStreamPayloadIntoSidebarTerminal verifies the
// sidebar/monitor branch streams the frame payload into the single sidebar
// terminal. The payload pads with a run of 'x' glyphs, so a fresh (blank)
// terminal must gain that signature after Step while changing the rendered
// view. Each write begins with a carriage return that rewrites the header line,
// so the stable observable is the padding run, not the (overwritten) header.
func TestStep_SidebarAndMonitorStreamPayloadIntoSidebarTerminal(t *testing.T) {
	for _, mode := range []string{HarnessSidebar, HarnessMonitor} {
		t.Run(mode, func(t *testing.T) {
			h, err := NewHarness(HarnessOptions{
				Mode:         mode,
				Tabs:         1,
				Width:        120,
				Height:       40,
				HotTabs:      3,
				PayloadBytes: 64,
			})
			if err != nil {
				t.Fatalf("%s harness init: %v", mode, err)
			}
			if h.sidebarTerm == nil {
				t.Fatalf("%s harness missing sidebar terminal", mode)
			}

			// The fresh sidebar terminal has no streamed payload glyphs yet; its
			// chrome ("Terminal 1", "+ New") contains no 'x'.
			before := h.sidebarTerm.View()
			if strings.ContainsRune(before, 'x') {
				t.Fatalf("%s sidebar terminal already contained payload before Step:\n%s", mode, before)
			}

			h.Step(0)

			after := h.sidebarTerm.View()
			if after == before {
				t.Fatalf("%s sidebar terminal view unchanged after Step; payload was not streamed", mode)
			}
			// The 'x' padding glyph is the deterministic signature of
			// buildPayload's output landing in the active VTerm.
			if !strings.ContainsRune(after, 'x') {
				t.Fatalf("%s sidebar terminal missing streamed payload padding after Step:\n%s",
					mode, after)
			}
		})
	}
}

// TestStep_SidebarNilTerminalIsSafe guards the h.sidebarTerm != nil branch in
// the sidebar/monitor path: a harness whose sidebar terminal is nil must skip
// the write rather than panic.
func TestStep_SidebarNilTerminalIsSafe(t *testing.T) {
	h := &Harness{
		mode:         HarnessSidebar,
		hotTabs:      2,
		payloadBytes: 64,
		payloadBuf:   make([]byte, 0, 96),
		spinner:      []byte{'|', '/', '-', '\\'},
		sidebarTerm:  nil,
	}

	h.Step(0) // must not panic with a nil sidebar terminal
}

// TestStep_CenterNilTabIsSkipped feeds Step a harness whose hot tab slot is nil
// to exercise the tab == nil continue guard.
func TestStep_CenterNilTabIsSkipped(t *testing.T) {
	h := newCenterHarnessForStep(t, 2, 2, 64, 0)
	// Null out the first hot tab; Step must skip it and still write the second.
	h.tabs[0] = nil

	h.Step(0) // must not panic on the nil tab

	if rendered := h.tabs[1].Terminal.Render(); !strings.Contains(rendered, "frame 0") {
		t.Fatalf("expected surviving tab to receive payload, got %q", rendered)
	}
}
