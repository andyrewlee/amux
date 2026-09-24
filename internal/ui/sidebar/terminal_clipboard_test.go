package sidebar

import (
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/vterm"
)

// TestHandlePTYFlush_DrainsPendingClipboard verifies the OSC 52 drain seam in
// the sidebar write path: after feeding an OSC 52 sequence through
// handlePTYFlush (which captures under the lock and hands off to safego),
// a second call to TakePendingClipboard on the VTerm returns nil — proving
// the production path already drained the clipboard payload.
func TestHandlePTYFlush_DrainsPendingClipboard(t *testing.T) {
	m := NewTerminalModel()
	m.width = 40
	m.height = 10

	wsID := "ws-clip"
	tabID := generateTerminalTabID()

	// OSC 52 write: "\x1b]52;c;aGk=\x07" sets clipboard to "hi"
	osc52 := []byte("\x1b]52;c;aGk=\x07")

	ts := &TerminalState{
		VTerm:   vterm.New(40, 10),
		Running: true,
	}
	// Seed PendingOutput with the OSC 52 sequence so handlePTYFlush writes it.
	ts.PendingOutput = append(ts.PendingOutput, osc52...)
	// Set LastOutputAt far in the past so FlushDelay returns immediately.
	ts.LastOutputAt = time.Now().Add(-1 * time.Second)

	m.tabs.ByWorkspace[wsID] = []*TerminalTab{
		{
			ID:    tabID,
			State: ts,
		},
	}
	m.tabs.ActiveByWorkspace[wsID] = 0

	m.handlePTYFlush(messages.SidebarPTYFlush{
		WorkspaceID: wsID,
		TabID:       string(tabID),
	})

	// Second TakePendingClipboard must return nil — handlePTYFlush drained it.
	if second := ts.VTerm.TakePendingClipboard(); second != nil {
		t.Fatalf("expected second TakePendingClipboard to return nil after sidebar drain, got %q", string(second))
	}
}

// TestHandlePTYFlush_NilClipboardWhenNoOSC52 verifies that plain text output
// via handlePTYFlush does not cause a spurious clipboard payload.
func TestHandlePTYFlush_NilClipboardWhenNoOSC52(t *testing.T) {
	m := NewTerminalModel()
	m.width = 40
	m.height = 10

	wsID := "ws-noclip"
	tabID := generateTerminalTabID()

	ts := &TerminalState{
		VTerm:   vterm.New(40, 10),
		Running: true,
	}
	ts.PendingOutput = append(ts.PendingOutput, []byte("hello world\n")...)
	ts.LastOutputAt = time.Now().Add(-1 * time.Second)

	m.tabs.ByWorkspace[wsID] = []*TerminalTab{
		{
			ID:    tabID,
			State: ts,
		},
	}
	m.tabs.ActiveByWorkspace[wsID] = 0

	m.handlePTYFlush(messages.SidebarPTYFlush{
		WorkspaceID: wsID,
		TabID:       string(tabID),
	})

	if clip := ts.VTerm.TakePendingClipboard(); clip != nil {
		t.Fatalf("expected nil clipboard after plain text flush, got %q", string(clip))
	}
}

// TestActiveTranscriptReturnsFullBuffer proves the sidebar export covers
// scrollback+screen: a tall write on a short screen must surface both the
// scrolled-off lines and the visible ones.
func TestActiveTranscriptReturnsFullBuffer(t *testing.T) {
	m := NewTerminalModel()
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	m.setWorkspace(ws)
	wsID := m.workspaceID()

	ts := &TerminalState{VTerm: vterm.New(40, 3), Running: true}
	ts.VTerm.Write([]byte("scrollback-line\r\nmid\r\nrecent-line\r\nscreen-line"))
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: "t1", State: ts}}
	m.tabs.ActiveByWorkspace[wsID] = 0

	got := m.ActiveTranscript()
	for _, want := range []string{"scrollback-line", "screen-line"} {
		if !strings.Contains(got, want) {
			t.Fatalf("transcript missing %q — got %q", want, got)
		}
	}
}

func TestActiveTranscriptNoTerminal(t *testing.T) {
	m := NewTerminalModel()
	if got := m.ActiveTranscript(); got != "" {
		t.Fatalf("no workspace/tabs: expected empty transcript, got %q", got)
	}
}
