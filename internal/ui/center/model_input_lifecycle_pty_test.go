package center

import (
	"testing"
	"time"

	appPty "github.com/andyrewlee/amux/internal/pty"
)

func TestHasVisiblePTYOutput(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{name: "empty", data: nil, want: false},
		{name: "whitespace only", data: []byte(" \t\r\n "), want: false},
		{name: "control sequences only", data: []byte("\x1b[?2004h\x1b[?2004l"), want: false},
		{name: "osc title only", data: []byte("\x1b]0;title\x07"), want: false},
		{name: "plain text", data: []byte("hello"), want: true},
		{name: "ansi text", data: []byte("\x1b[32mready\x1b[0m"), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := hasVisiblePTYOutput(tt.data, ansiActivityText)
			if got != tt.want {
				t.Fatalf("hasVisiblePTYOutput(%q) = %v, want %v", string(tt.data), got, tt.want)
			}
		})
	}
}

func TestHasVisiblePTYOutput_SplitControlSequenceAcrossChunks(t *testing.T) {
	state := ansiActivityText

	got, next := hasVisiblePTYOutput([]byte("\x1b[?2004"), state)
	if got {
		t.Fatal("expected split control prefix to be non-visible")
	}
	state = next

	got, next = hasVisiblePTYOutput([]byte("h"), state)
	if got {
		t.Fatal("expected split control suffix to be non-visible")
	}
	if next != ansiActivityText {
		t.Fatalf("expected parser to return to text state, got %v", next)
	}
}

func TestHasVisiblePTYOutput_UTF8BytesDoNotEnterControlState(t *testing.T) {
	// 😀 in UTF-8: f0 9f 98 80
	got, next := hasVisiblePTYOutput([]byte{0xf0, 0x9f, 0x98, 0x80}, ansiActivityText)
	if !got {
		t.Fatal("expected UTF-8 emoji bytes to be treated as visible output")
	}
	if next != ansiActivityText {
		t.Fatalf("expected parser to remain in text state for UTF-8 bytes, got %v", next)
	}
}

func TestHasVisiblePTYOutput_SplitUTF8ThenTextDoesNotWedgeState(t *testing.T) {
	// Feed the emoji continuation bytes in a split form, then normal text.
	state := ansiActivityText
	got, next := hasVisiblePTYOutput([]byte{0xf0, 0x9f}, state)
	if !got {
		t.Fatal("expected first UTF-8 chunk to be visible")
	}
	state = next
	got, next = hasVisiblePTYOutput([]byte{0x98, 0x80}, state)
	if !got {
		t.Fatal("expected second UTF-8 chunk to be visible")
	}
	state = next
	got, next = hasVisiblePTYOutput([]byte("ok"), state)
	if !got {
		t.Fatal("expected subsequent plain text to remain visible")
	}
	if next != ansiActivityText {
		t.Fatalf("expected parser to remain in text state, got %v", next)
	}
}

func TestHasVisiblePTYOutput_ESCParenBIsNonVisible(t *testing.T) {
	// ESC(B: designate G0 character set (non-printing control sequence).
	got, next := hasVisiblePTYOutput([]byte{0x1b, '(', 'B'}, ansiActivityText)
	if got {
		t.Fatal("expected ESC(B to be treated as non-visible control output")
	}
	if next != ansiActivityText {
		t.Fatalf("expected parser to return to text state, got %v", next)
	}
}

func TestHasVisiblePTYOutput_SplitESCParenBIsNonVisible(t *testing.T) {
	state := ansiActivityText
	got, next := hasVisiblePTYOutput([]byte{0x1b, '('}, state)
	if got {
		t.Fatal("expected ESC( prefix chunk to be non-visible")
	}
	state = next
	got, next = hasVisiblePTYOutput([]byte{'B'}, state)
	if got {
		t.Fatal("expected ESC(B suffix chunk to be non-visible")
	}
	if next != ansiActivityText {
		t.Fatalf("expected parser to return to text state, got %v", next)
	}
}

func TestUpdatePTYOutput_DoesNotTagControlOnlyOutput(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-1"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "amux-test-session",
		Running:     true,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0

	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("\x1b[?2004h\x1b[?2004l"),
	})

	if !tab.lastActivityTagAt.IsZero() {
		t.Fatalf("expected lastActivityTagAt to remain zero for control-only output, got %v", tab.lastActivityTagAt)
	}
	if !tab.lastVisibleOutput.IsZero() {
		t.Fatalf("expected lastVisibleOutput to remain zero for control-only output, got %v", tab.lastVisibleOutput)
	}
}

func TestUpdatePTYOutput_TagsVisibleOutput(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	before := time.Now().Add(-2 * activityTagThrottle)
	tab := &Tab{
		ID:                TabID("tab-1"),
		Assistant:         "codex",
		Workspace:         ws,
		SessionName:       "amux-test-session",
		Running:           true,
		lastActivityTagAt: before,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0

	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("visible output"),
	})

	if tab.lastVisibleOutput.IsZero() {
		t.Fatalf("expected lastVisibleOutput to be set for visible output")
	}
	if tab.lastVisibleOutput.Sub(before) <= 0 {
		t.Fatalf("expected lastVisibleOutput to move forward, before=%v after=%v", before, tab.lastVisibleOutput)
	}
	if !tab.lastActivityTagAt.After(before) {
		t.Fatalf("expected lastActivityTagAt to move forward, before=%v after=%v", before, tab.lastActivityTagAt)
	}
}

func TestUpdatePtyTabReattachResult_ResetsActivityANSIState(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                TabID("tab-1"),
		Assistant:         "codex",
		Workspace:         ws,
		activityANSIState: ansiActivityString,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_, _ = m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Agent:       &appPty.Agent{Session: "sess-reattach"},
		Rows:        24,
		Cols:        80,
	})

	if tab.activityANSIState != ansiActivityText {
		t.Fatalf("expected activityANSIState reset to text on reattach, got %v", tab.activityANSIState)
	}
}

func TestHandlePtyTabCreated_ExistingResetsActivityANSIState(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                TabID("tab-1"),
		Assistant:         "codex",
		Workspace:         ws,
		activityANSIState: ansiActivityOSC,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_ = m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace: ws,
		Assistant: "codex",
		Agent:     &appPty.Agent{Session: "sess-created"},
		TabID:     tab.ID,
		Rows:      24,
		Cols:      80,
		Activate:  true,
	})

	if tab.activityANSIState != ansiActivityText {
		t.Fatalf("expected activityANSIState reset to text on existing tab create path, got %v", tab.activityANSIState)
	}
}

func TestUpdatePTYStopped_ResetsActivityANSIState(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                TabID("tab-1"),
		Assistant:         "codex",
		Workspace:         ws,
		activityANSIState: ansiActivityOSC,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_ = m.updatePTYStopped(PTYStopped{
		WorkspaceID: wsID,
		TabID:       tab.ID,
	})

	if tab.activityANSIState != ansiActivityText {
		t.Fatalf("expected activityANSIState reset to text on PTY stop, got %v", tab.activityANSIState)
	}
}

func TestUpdatePTYRestart_ResetsActivityANSIState(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                TabID("tab-1"),
		Assistant:         "codex",
		Workspace:         ws,
		activityANSIState: ansiActivityCSI,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_ = m.updatePTYRestart(PTYRestart{
		WorkspaceID: wsID,
		TabID:       tab.ID,
	})

	if tab.activityANSIState != ansiActivityText {
		t.Fatalf("expected activityANSIState reset to text on PTY restart, got %v", tab.activityANSIState)
	}
}
