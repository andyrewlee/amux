package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/diff"
)

func TestFocusSyncsActiveDiffViewerFocus(t *testing.T) {
	m := New(&config.Config{})
	ws := data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/repo/feature")
	m.SetWorkspace(ws)

	dv := &diff.Model{}
	dv.SetFocused(false)
	tab := &Tab{
		ID:         generateTabID(),
		Name:       "diff",
		Workspace:  ws,
		DiffViewer: dv,
	}
	wsID := string(ws.ID())
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0

	m.Focus()
	if !dv.Focused() {
		t.Fatal("expected active diff viewer to become focused with center pane")
	}

	m.Blur()
	if dv.Focused() {
		t.Fatal("expected active diff viewer to blur with center pane")
	}
}
