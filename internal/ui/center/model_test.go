package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// TestSetInstanceID verifies the instance tag is stored verbatim, including the
// empty and reassignment cases.
func TestSetInstanceID(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{name: "empty", id: ""},
		{name: "simple", id: "host-1"},
		{name: "uuid-like", id: "9f8c2b1a-0000-4444-8888-abcdef012345"},
		{name: "whitespace preserved", id: "  spaced  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			if m.instanceID != "" {
				t.Fatalf("expected fresh model to start with empty instanceID, got %q", m.instanceID)
			}

			m.SetInstanceID(tt.id)

			if m.instanceID != tt.id {
				t.Fatalf("SetInstanceID(%q) = %q, want %q", tt.id, m.instanceID, tt.id)
			}
		})
	}
}

// TestSetInstanceID_Overwrites confirms the setter replaces (not appends to) any
// previously stored value.
func TestSetInstanceID_Overwrites(t *testing.T) {
	m := newTestModel()

	m.SetInstanceID("first")
	if m.instanceID != "first" {
		t.Fatalf("expected first assignment to stick, got %q", m.instanceID)
	}

	m.SetInstanceID("second")
	if m.instanceID != "second" {
		t.Fatalf("expected second assignment to overwrite, got %q", m.instanceID)
	}

	m.SetInstanceID("")
	if m.instanceID != "" {
		t.Fatalf("expected empty assignment to clear instanceID, got %q", m.instanceID)
	}
}

// TestSetTmuxOptions verifies the resolved options are stored on the model. The
// options are also forwarded to the agent manager (covered separately below).
func TestSetTmuxOptions(t *testing.T) {
	tests := []struct {
		name string
		opts tmux.Options
	}{
		{
			name: "zero value",
			opts: tmux.Options{},
		},
		{
			name: "fully populated",
			opts: tmux.Options{
				ServerName:      "amux-test",
				ConfigPath:      "/tmp/tmux.conf",
				HideStatus:      true,
				DisableMouse:    true,
				DefaultTerminal: "tmux-256color",
				CommandTimeout:  3 * time.Second,
			},
		},
		{
			name: "partial",
			opts: tmux.Options{
				ServerName:     "only-server",
				CommandTimeout: 7 * time.Second,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()

			m.SetTmuxOptions(tt.opts)

			if m.tmuxOpts != tt.opts {
				t.Fatalf("SetTmuxOptions stored %+v, want %+v", m.tmuxOpts, tt.opts)
			}
		})
	}
}

// TestSetTmuxOptions_OverwritesDefaults confirms the setter replaces the
// constructor-seeded DefaultOptions rather than merging with them.
func TestSetTmuxOptions_OverwritesDefaults(t *testing.T) {
	m := newTestModel()

	// New() seeds tmux.DefaultOptions(); ensure a follow-up call fully replaces it.
	if m.tmuxOpts == (tmux.Options{}) {
		t.Fatal("expected constructor to seed non-zero default tmux options")
	}

	want := tmux.Options{ServerName: "replaced", CommandTimeout: time.Second}
	m.SetTmuxOptions(want)

	if m.tmuxOpts != want {
		t.Fatalf("expected options to be replaced, got %+v want %+v", m.tmuxOpts, want)
	}
}

// TestSetTmuxOptions_NilAgentManager guards the nil-agentManager branch: the
// setter must still store options and must not panic when no manager is present.
func TestSetTmuxOptions_NilAgentManager(t *testing.T) {
	m := newTestModel()
	m.agentManager = nil

	opts := tmux.Options{ServerName: "no-manager", CommandTimeout: 2 * time.Second}
	m.SetTmuxOptions(opts)

	if m.tmuxOpts != opts {
		t.Fatalf("expected options stored even without an agent manager, got %+v", m.tmuxOpts)
	}
}

// TestSetTmuxOptions_DoesNotPanicWithAgentManagerWired exercises the
// m.agentManager != nil branch in SetTmuxOptions: with a manager wired by the
// constructor, the setter must store the model copy and run the forwarding call
// path without panicking. The manager-side value is asserted separately by
// TestAgentManager_SetTmuxOptions, so this is a branch-coverage / no-panic check.
func TestSetTmuxOptions_DoesNotPanicWithAgentManagerWired(t *testing.T) {
	m := newTestModel()
	if m.agentManager == nil {
		t.Fatal("expected constructor to wire an agent manager for forwarding")
	}

	opts := tmux.Options{ServerName: "forwarded", DisableMouse: true, CommandTimeout: 4 * time.Second}
	m.SetTmuxOptions(opts)

	if m.tmuxOpts != opts {
		t.Fatalf("model copy of options = %+v, want %+v", m.tmuxOpts, opts)
	}
}

// TestContentWidth verifies the pane-content width accounting: it subtracts the
// pane frame from the model width and clamps to a floor of 1. The frame size is
// derived from the live style so the expectation stays exact without hardcoding
// a magic number that would drift with theme changes.
func TestContentWidth(t *testing.T) {
	frameX, _ := newTestModel().styles.Pane.GetFrameSize()

	tests := []struct {
		name  string
		width int
		want  int
	}{
		{name: "zero width clamps to floor", width: 0, want: 1},
		{name: "negative width clamps to floor", width: -42, want: 1},
		{name: "width below frame clamps to floor", width: frameX, want: 1},
		{name: "width just above frame", width: frameX + 1, want: 1},
		{name: "typical width", width: 120, want: 120 - frameX},
		{name: "large width", width: 1000, want: 1000 - frameX},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.width = tt.width

			if got := m.ContentWidth(); got != tt.want {
				t.Fatalf("ContentWidth() with width=%d = %d, want %d", tt.width, got, tt.want)
			}
		})
	}
}

// TestContentWidth_NeverBelowOne is a property check: across a sweep of widths
// (including negatives and the boundary around the frame size) ContentWidth must
// never return a value below 1.
func TestContentWidth_NeverBelowOne(t *testing.T) {
	m := newTestModel()
	for w := -5; w <= 30; w++ {
		m.width = w
		if got := m.ContentWidth(); got < 1 {
			t.Fatalf("ContentWidth() with width=%d returned %d, want >= 1", w, got)
		}
	}
}

// TestContentWidth_MatchesPaneWidthMinusFrame ties ContentWidth to its documented
// definition once the width clears the floor: content == paneWidth - frameX.
func TestContentWidth_MatchesPaneWidthMinusFrame(t *testing.T) {
	m := newTestModel()
	frameX, _ := m.styles.Pane.GetFrameSize()

	for _, w := range []int{frameX + 2, 50, 200, 4096} {
		m.width = w
		want := w - frameX
		if got := m.ContentWidth(); got != want {
			t.Fatalf("ContentWidth() with width=%d = %d, want paneWidth-frame=%d", w, got, want)
		}
	}
}
