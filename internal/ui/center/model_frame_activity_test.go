package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

// TestActivityVersion_TracksBackgroundTabs pins the contract App's full-frame
// cache depends on: a background chat tab that starts or stops working changes
// the fingerprint, because the tab bar highlights it.
func TestActivityVersion_TracksBackgroundTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	active := &Tab{ID: TabID("tab-0"), Assistant: "claude", Workspace: ws, Running: true}
	background := &Tab{ID: TabID("tab-1"), Assistant: "claude", Workspace: ws, Running: true}
	m.tabs.ByWorkspace[wsID] = []*Tab{active, background}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.workspace = ws

	idle := m.ActivityVersion()
	if idle != 0 {
		t.Fatalf("expected no activity bits with no output, got %#x", idle)
	}

	background.mu.Lock()
	background.lastVisibleOutput = time.Now()
	background.mu.Unlock()

	working := m.ActivityVersion()
	if working == idle {
		t.Fatalf("background tab activity did not change the fingerprint (still %#x)", working)
	}

	background.mu.Lock()
	background.lastVisibleOutput = time.Now().Add(-2 * tabActiveWindow)
	background.mu.Unlock()

	if got := m.ActivityVersion(); got != idle {
		t.Fatalf("expired background activity = %#x, want %#x", got, idle)
	}
}

// TestActivityVersion_TracksActiveTabCursorWindow covers the other half: PTY
// bytes that change no cell at all still open the active chat tab's cursor-trust
// window, which moves where the cursor is drawn.
func TestActivityVersion_TracksActiveTabCursorWindow(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab := &Tab{ID: TabID("tab-0"), Assistant: "claude", Workspace: ws, Running: true}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.workspace = ws

	idle := m.ActivityVersion()

	// LastOutputAt without lastVisibleOutput is exactly the invisible-output case:
	// no cell changed, so no vterm version moved either.
	tab.mu.Lock()
	tab.LastOutputAt = time.Now()
	tab.mu.Unlock()

	if got := m.ActivityVersion(); got == idle {
		t.Fatalf("invisible output did not change the fingerprint (still %#x)", got)
	}

	tab.mu.Lock()
	tab.LastOutputAt = time.Now().Add(-2 * tabActiveWindow)
	tab.mu.Unlock()

	if got := m.ActivityVersion(); got != idle {
		t.Fatalf("expired cursor window = %#x, want %#x", got, idle)
	}
}

// TestVisibleTerminalVersions_SplitsContentAndTitle covers the reason the title
// is keyed separately: an OSC title update repaints the window title without
// touching a single cell.
func TestVisibleTerminalVersions_SplitsContentAndTitle(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab := &Tab{
		ID:        TabID("tab-0"),
		Assistant: "claude",
		Workspace: ws,
		Running:   true,
		Terminal:  vterm.New(80, 24),
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.workspace = ws

	content, title := m.VisibleTerminalVersions()

	tab.WriteToTerminal([]byte("\x1b]0;agent working\x07"))

	gotContent, gotTitle := m.VisibleTerminalVersions()
	if gotContent != content {
		t.Fatalf("title-only output moved the content version: %d -> %d", content, gotContent)
	}
	if gotTitle == title {
		t.Fatalf("title-only output did not move the title version (still %d)", gotTitle)
	}
}
