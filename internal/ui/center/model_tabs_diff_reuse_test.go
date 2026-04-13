package center

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/diff"
)

func TestCreateDiffTab_ReusesExistingTabForSamePathAndMode(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	m.SetWorkspace(ws)

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:        TabID("tab-chat"),
			Name:      "claude",
			Assistant: "claude",
			Workspace: ws,
			Running:   true,
		},
		{
			ID:         TabID("tab-diff"),
			Name:       "Diff: main.go",
			Assistant:  "diff",
			Workspace:  ws,
			DiffViewer: diff.New(ws, &git.Change{Path: "main.go", Kind: git.ChangeModified}, git.DiffModeUnstaged, 80, 24),
		},
	}
	m.activeTabByWorkspace[wsID] = 0

	cmd := m.createDiffTab(&git.Change{Path: "./main.go", Kind: git.ChangeModified}, git.DiffModeUnstaged, ws)
	if cmd == nil {
		t.Fatal("expected reuse command for existing diff tab")
	}
	if got := len(m.tabsByWorkspace[wsID]); got != 2 {
		t.Fatalf("expected existing diff tab reuse, got %d tabs", got)
	}
	if got := m.activeTabByWorkspace[wsID]; got != 1 {
		t.Fatalf("expected existing diff tab to become active, got index %d", got)
	}

	msg := cmd()
	sawSelection := false
	sawReload := false

	switch typed := msg.(type) {
	case tea.BatchMsg:
		for _, subcmd := range typed {
			if subcmd == nil {
				continue
			}
			switch submsg := subcmd().(type) {
			case messages.TabSelectionChanged:
				sawSelection = true
				if submsg.WorkspaceID != wsID || submsg.ActiveIndex != 1 {
					t.Fatalf("unexpected selection payload: %+v", submsg)
				}
			default:
				if strings.HasSuffix(fmt.Sprintf("%T", submsg), ".diffLoaded") {
					sawReload = true
				}
			}
		}
	default:
		t.Fatalf("expected batched reuse command, got %T", typed)
	}

	if !sawSelection {
		t.Fatal("expected tab selection change for reused diff tab")
	}
	if !sawReload {
		t.Fatal("expected reused diff tab to reload its diff")
	}
}

func TestReuseDiffTab_RefreshesChangeKindBeforeReload(t *testing.T) {
	repo := t.TempDir()
	mustRunGit(t, repo, "init", "-b", "main")
	mustRunGit(t, repo, "config", "user.name", "Test")
	mustRunGit(t, repo, "config", "user.email", "test@example.com")

	filePath := filepath.Join(repo, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	ws := data.NewWorkspace("ws", "ws", "main", repo, repo)
	wsID := string(ws.ID())
	m := newTestModel()
	m.SetWorkspace(ws)

	dv := diff.New(ws, &git.Change{Path: "main.go", Kind: git.ChangeUntracked}, git.DiffModeUnstaged, 80, 24)
	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:         TabID("tab-diff"),
			Name:       "Diff: main.go",
			Assistant:  "diff",
			Workspace:  ws,
			DiffViewer: dv,
		},
	}
	m.activeTabByWorkspace[wsID] = 0

	mustRunGit(t, repo, "add", "main.go")
	mustRunGit(t, repo, "commit", "-m", "track main.go")
	if err := os.WriteFile(filePath, []byte("package main\n\nfunc main() { println(\"hi\") }\n"), 0o644); err != nil {
		t.Fatalf("write tracked modification: %v", err)
	}

	cmd := m.createDiffTab(&git.Change{Path: "main.go", Kind: git.ChangeModified}, git.DiffModeUnstaged, ws)
	if cmd == nil {
		t.Fatal("expected reuse command for tracked modification")
	}

	msg := cmd()
	switch typed := msg.(type) {
	case tea.BatchMsg:
		for _, subcmd := range typed {
			if subcmd == nil {
				continue
			}
			submsg := subcmd()
			updatedDV, _ := dv.Update(submsg)
			dv = updatedDV
		}
	default:
		updatedDV, _ := dv.Update(typed)
		dv = updatedDV
	}

	rendered := dv.View()
	if strings.Contains(rendered, "/dev/null") {
		t.Fatalf("expected tracked diff reload, got stale untracked diff:\n%s", rendered)
	}
	if !strings.Contains(rendered, "--- a/main.go") {
		t.Fatalf("expected tracked diff header after reuse, got:\n%s", rendered)
	}
}

func mustRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := git.RunGit(dir, args...); err != nil {
		t.Fatalf("git %s failed: %v", strings.Join(args, " "), err)
	}
}
