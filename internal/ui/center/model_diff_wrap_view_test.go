package center

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/ui/diff"
)

// TestDiffTabWrappedTailReachable is the center-level regression for the
// visual-row repair: a diff tab's long wrapped line must be scrollable to its
// tail inside the content rectangle the center view allocates. The fixture
// loads a real git diff through the viewer's Init→Update→View path, not a
// hand-seeded struct.
func TestDiffTabWrappedTailReachable(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := testutil.InitRepo(t)
	file := filepath.Join(repo, "long.txt")
	content := "base line\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.RunGit(t, repo, "add", "long.txt")
	testutil.RunGit(t, repo, "commit", "-m", "base")

	// One edited line long enough to wrap several viewport heights.
	long := "head " + strings.Repeat("fill ", 60) + "TAILMARKER"
	if err := os.WriteFile(file, []byte(long+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newTestModel()
	ws := newTestWorkspace("ws", repo)
	m.SetWorkspace(ws)

	dv := diff.New(ws, &git.Change{Path: "long.txt", Kind: git.ChangeModified}, git.DiffModeUnstaged, 50, 10)
	dv.SetFocused(true)
	tab := &Tab{ID: "tab-diff", Workspace: ws, Assistant: "diff", DiffViewer: dv}
	m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[string(ws.ID())] = 0

	// Deliver the real load result: Init() returns the cmd that performs the
	// git diff; its message goes back through Update like bubbletea does.
	dv, _ = dv.Update(dv.Init()())

	// Toggle wrap through the viewer's real input path.
	dv, _ = dv.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})
	for i := 0; i < 10; i++ {
		dv, _ = dv.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	dv, _ = dv.Update(tea.KeyPressMsg{Code: tea.KeyEnd})

	out := ansi.Strip(dv.View())
	if !strings.Contains(out, "TAILMARKER") {
		t.Fatalf("wrapped tail unreachable in center-allocated viewport\n--- visible ---\n%s", out)
	}
	for i, row := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(row); w > 50 {
			t.Fatalf("rendered row %d overflows the allocated width: %d > 50", i, w)
		}
	}
}
