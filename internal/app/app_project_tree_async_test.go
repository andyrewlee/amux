package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// TestProjectTreeAsyncRoutingAppDeliversRegardlessOfFocus obtains the real
// project-load command a workspace switch issues, then delivers its result
// while the sidebar is unfocused — the message must still reach the tree
// (app-level routing for background results, not input).
func TestProjectTreeAsyncRoutingAppDeliversRegardlessOfFocus(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "landed.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	a := &App{sidebar: sidebar.NewTabbedSidebar()}
	a.sidebar.SetSize(60, 20)
	ws := data.NewWorkspace("ws", "ws", "main", filepath.Dir(root), root)

	var loaded sidebar.ProjectTreeDirectoryLoaded
	found := false
	for _, msg := range collectAppCmdMsgs(t, a.sidebar.SetWorkspace(ws)) {
		if l, ok := msg.(sidebar.ProjectTreeDirectoryLoaded); ok {
			loaded, found = l, true
		}
	}
	if !found {
		t.Fatal("sidebar SetWorkspace produced no ProjectTreeDirectoryLoaded")
	}

	// The sidebar is not focused (zero-value App); the result must still land.
	a.update(loaded)

	view := ansi.Strip(a.sidebar.ProjectTree().View())
	if !strings.Contains(view, "landed.txt") {
		t.Fatalf("tree result did not reach the sidebar; view:\n%s", view)
	}
}

// collectAppCmdMsgs runs a command batch and returns every leaf message it
// produced.
func collectAppCmdMsgs(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collectAppCmdMsgs(t, c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}
