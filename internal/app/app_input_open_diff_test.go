package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/diff"
)

func TestOpenDiff_ReusesExistingChangedFileTab(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo")
	wsID := string(ws.ID())

	centerModel := center.New(&config.Config{})
	centerModel.SetWorkspace(ws)
	centerModel.AddTab(&center.Tab{
		ID:        center.TabID("tab-chat"),
		Name:      "claude",
		Assistant: "claude",
		Workspace: ws,
		Running:   true,
	})
	centerModel.AddTab(&center.Tab{
		ID:         center.TabID("tab-diff"),
		Name:       "Diff: main.go",
		Assistant:  "diff",
		Workspace:  ws,
		DiffViewer: diff.New(ws, &git.Change{Path: "main.go", Kind: git.ChangeModified}, git.DiffModeUnstaged, 80, 24),
	})
	centerModel.SelectTab(0)

	app := &App{
		activeWorkspace: ws,
		center:          centerModel,
		focusedPane:     messages.PaneSidebar,
	}

	_, cmd := app.Update(messages.OpenDiff{
		Change:    &git.Change{Path: "./main.go", Kind: git.ChangeModified},
		Mode:      git.DiffModeUnstaged,
		Workspace: ws,
	})
	if cmd == nil {
		t.Fatal("expected command when opening an already-open changed file")
	}

	tabs, activeIdx := app.center.GetTabsInfoForWorkspace(wsID)
	if len(tabs) != 2 {
		t.Fatalf("expected changed-file click to reuse existing diff tab, got %d tabs", len(tabs))
	}
	if activeIdx != 1 {
		t.Fatalf("expected existing diff tab to become active immediately, got index %d", activeIdx)
	}
	if app.focusedPane != messages.PaneCenter {
		t.Fatalf("expected center focus after opening diff, got %v", app.focusedPane)
	}

	msg := cmd()
	switch typed := msg.(type) {
	case tea.BatchMsg:
		for _, subcmd := range typed {
			if subcmd == nil {
				continue
			}
			submsg := subcmd()
			if submsg == nil {
				continue
			}
			if _, followup := app.Update(submsg); followup != nil {
				_ = followup()
			}
		}
	default:
		if _, followup := app.Update(typed); followup != nil {
			_ = followup()
		}
	}

	tabs, activeIdx = app.center.GetTabsInfoForWorkspace(wsID)
	if len(tabs) != 2 {
		t.Fatalf("expected no duplicate diff tab after follow-up updates, got %d tabs", len(tabs))
	}
	if activeIdx != 1 {
		t.Fatalf("expected reused diff tab to stay active, got index %d", activeIdx)
	}
}
