package center

import (
	"bytes"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
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
	if tab.pendingVisibleOutput {
		t.Fatalf("expected pendingVisibleOutput to remain false for control-only output")
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

	if !tab.lastVisibleOutput.IsZero() {
		t.Fatalf("expected lastVisibleOutput to remain zero until flush, got %v", tab.lastVisibleOutput)
	}
	if !tab.lastActivityTagAt.Equal(before) {
		t.Fatalf("expected lastActivityTagAt unchanged before flush, before=%v after=%v", before, tab.lastActivityTagAt)
	}
	if !tab.pendingVisibleOutput {
		t.Fatalf("expected pendingVisibleOutput to be set for visible output")
	}
}

func TestUpdatePTYOutput_ResetsActivityStateAfterOverflowCarryTrim(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                TabID("tab-1"),
		Assistant:         "codex",
		Workspace:         ws,
		Running:           true,
		overflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryCSI},
		activityANSIState: ansiActivityCSI,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0

	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("31mX"),
	})

	if string(tab.pendingOutput) != "X" {
		t.Fatalf("expected overflow carry trim to retain visible suffix, got %q", tab.pendingOutput)
	}
	if tab.overflowTrimCarry != (vterm.ParserCarryState{}) {
		t.Fatalf("expected overflow trim carry cleared, got %+v", tab.overflowTrimCarry)
	}
	if tab.activityANSIState != ansiActivityText {
		t.Fatalf("expected activity ANSI state reset to text, got %v", tab.activityANSIState)
	}
	if !tab.pendingVisibleOutput {
		t.Fatal("expected trimmed visible byte to mark pending visible output")
	}
}

func TestUpdatePTYOutput_EndsBootstrapAfterQuietGap(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                    TabID("tab-1"),
		Assistant:             "codex",
		Workspace:             ws,
		SessionName:           "amux-test-session",
		Terminal:              vterm.New(80, 24),
		Running:               true,
		bootstrapActivity:     true,
		bootstrapLastOutputAt: time.Now().Add(-(bootstrapQuietGap + 200*time.Millisecond)),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("real output after quiet"),
	})

	tab.mu.Lock()
	bootstrap := tab.bootstrapActivity
	tab.mu.Unlock()
	if bootstrap {
		t.Fatal("expected bootstrapActivity to end after quiet gap before new output")
	}

	tab.lastOutputAt = time.Now().Add(-time.Second)
	tab.flushPendingSince = tab.lastOutputAt
	m.tabEvents = nil
	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})
	if tab.lastVisibleOutput.IsZero() {
		t.Fatal("expected output after bootstrap quiet gap to mark visible activity")
	}
}

func TestUpdatePTYFlush_TagsVisibleScreenDelta(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	before := time.Now().Add(-2 * activityTagThrottle)
	tab := &Tab{
		ID:                TabID("tab-1"),
		Assistant:         "codex",
		Workspace:         ws,
		SessionName:       "amux-test-session",
		Terminal:          vterm.New(80, 24),
		Running:           true,
		lastActivityTagAt: before,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("visible output"),
	})
	tab.lastOutputAt = time.Now().Add(-time.Second)
	tab.flushPendingSince = tab.lastOutputAt
	m.tabEvents = nil
	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})

	if tab.lastVisibleOutput.IsZero() {
		t.Fatalf("expected first visible delta flush to set lastVisibleOutput")
	}
	if !tab.lastActivityTagAt.After(before) {
		t.Fatalf("expected first visible delta flush to move lastActivityTagAt, before=%v after=%v", before, tab.lastActivityTagAt)
	}
	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("\nmore visible output"),
	})
	tab.lastOutputAt = time.Now().Add(-time.Second)
	tab.flushPendingSince = tab.lastOutputAt
	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})
	if tab.lastVisibleOutput.IsZero() {
		t.Fatalf("expected second visible delta flush to set lastVisibleOutput")
	}
	if !tab.lastActivityTagAt.After(before) {
		t.Fatalf("expected second visible delta flush to move lastActivityTagAt, before=%v after=%v", before, tab.lastActivityTagAt)
	}
	if tab.pendingVisibleOutput {
		t.Fatalf("expected pendingVisibleOutput cleared after flush")
	}
}

func TestUpdatePTYFlush_DoesNotTagUnchangedVisibleScreen(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-1"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "amux-test-session",
		Terminal:    vterm.New(80, 24),
		Running:     true,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("stable"),
	})
	tab.lastOutputAt = time.Now().Add(-time.Second)
	tab.flushPendingSince = tab.lastOutputAt
	m.tabEvents = nil
	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})
	firstVisible := tab.lastVisibleOutput
	firstTag := tab.lastActivityTagAt
	if firstVisible.IsZero() {
		t.Fatalf("expected initial visible output to set activity timestamp")
	}
	if firstTag.IsZero() {
		t.Fatalf("expected initial visible output to set activity tag timestamp")
	}

	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("\rstable"),
	})
	tab.lastOutputAt = time.Now().Add(-time.Second)
	tab.flushPendingSince = tab.lastOutputAt
	m.tabEvents = nil
	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})

	if !tab.lastVisibleOutput.Equal(firstVisible) {
		t.Fatalf("expected unchanged rendered content not to refresh lastVisibleOutput: before=%v after=%v", firstVisible, tab.lastVisibleOutput)
	}
	if !tab.lastActivityTagAt.Equal(firstTag) {
		t.Fatalf("expected unchanged rendered content not to refresh lastActivityTagAt: before=%v after=%v", firstTag, tab.lastActivityTagAt)
	}
}

func TestUpdatePTYFlush_RestoresQueuedParserStateAfterActorFallback(t *testing.T) {
	m := newTestModel()
	m.setTabActorReady()
	m.tabEvents = make(chan tabEvent, 1)
	m.tabEvents <- tabEvent{kind: tabEventWriteOutput}

	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	prevCarry := vterm.ParserCarryState{Mode: vterm.ParserCarryCSIParam}
	chunk := []byte("1mX\nproc(1) malloc")
	tab := &Tab{
		ID:                       TabID("tab-1"),
		Assistant:                "codex",
		Workspace:                ws,
		Terminal:                 vterm.New(80, 24),
		Running:                  true,
		pendingOutput:            append([]byte(nil), chunk...),
		actorWritesPending:       1,
		actorWriteEpoch:          11,
		actorQueuedCarry:         prevCarry,
		actorQueuedNoiseTrailing: []byte("queued-noise"),
	}
	prevNoiseTrailing := append([]byte(nil), tab.actorQueuedNoiseTrailing...)
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})

	if tab.actorWritesPending != 1 {
		t.Fatalf("expected older queued write to remain pending, got %d", tab.actorWritesPending)
	}
	if tab.actorQueuedCarry != prevCarry {
		t.Fatalf("expected queued carry restored to %+v, got %+v", prevCarry, tab.actorQueuedCarry)
	}
	if !bytes.Equal(tab.actorQueuedNoiseTrailing, prevNoiseTrailing) {
		t.Fatalf("expected queued noise trailing restored to %q, got %q", prevNoiseTrailing, tab.actorQueuedNoiseTrailing)
	}
	if !bytes.Equal(tab.ptyNoiseTrailing, []byte("proc(1) malloc")) {
		t.Fatalf("expected synchronous fallback to update live trailing noise, got %q", tab.ptyNoiseTrailing)
	}
}

func TestUpdatePTYFlush_SuppressesImmediateUserInputEcho(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:              TabID("tab-1"),
		Assistant:       "codex",
		Workspace:       ws,
		SessionName:     "amux-test-session",
		Terminal:        vterm.New(80, 24),
		Running:         true,
		lastUserInputAt: time.Now(),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("echoed input"),
	})
	tab.lastOutputAt = time.Now().Add(-time.Second)
	tab.flushPendingSince = tab.lastOutputAt
	m.tabEvents = nil
	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})
	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("still echoed input"),
	})
	tab.lastOutputAt = time.Now().Add(-time.Second)
	tab.flushPendingSince = tab.lastOutputAt
	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})

	if !tab.lastVisibleOutput.IsZero() {
		t.Fatalf("expected user-input echo not to set lastVisibleOutput, got %v", tab.lastVisibleOutput)
	}
	if !tab.lastActivityTagAt.IsZero() {
		t.Fatalf("expected user-input echo not to set lastActivityTagAt, got %v", tab.lastActivityTagAt)
	}
}
