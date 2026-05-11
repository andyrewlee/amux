package sidebar

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
)

func topVisibleChangePath(m *Model) string {
	start := m.scrollOffset
	if start < 0 {
		start = 0
	}
	end := start + m.visibleHeight()
	if end > len(m.displayItems) {
		end = len(m.displayItems)
	}
	for i := start; i < end; i++ {
		if m.displayItems[i].change != nil {
			return m.displayItems[i].change.Path
		}
	}
	return ""
}

func topVisibleItem(m *Model) displayItem {
	if len(m.displayItems) == 0 {
		return displayItem{}
	}
	if m.scrollOffset < 0 || m.scrollOffset >= len(m.displayItems) {
		return displayItem{}
	}
	return m.displayItems[m.scrollOffset]
}

func TestChangesSetGitStatusPreservesManualScrollAnchor(t *testing.T) {
	m := New()
	m.SetSize(80, 10)
	unstaged := make([]git.Change, 0, 20)
	for i := 0; i < 20; i++ {
		unstaged = append(unstaged, git.Change{
			Path: "u" + strconv.Itoa(i/10) + strconv.Itoa(i%10) + ".go",
			Kind: git.ChangeModified,
		})
	}
	m.SetGitStatus(&git.StatusResult{
		Clean:    false,
		Unstaged: unstaged,
	})
	m.cursor = 1
	m.scrollOffset = 8

	if got := topVisibleChangePath(m); got != "u07.go" {
		t.Fatalf("top visible change = %q, want %q", got, "u07.go")
	}

	m.SetGitStatus(&git.StatusResult{
		Clean: false,
		Staged: []git.Change{
			{Path: "s00.go", Kind: git.ChangeModified, Staged: true},
		},
		Unstaged: unstaged,
	})

	if m.scrollOffset != 10 {
		t.Fatalf("scrollOffset = %d, want 10", m.scrollOffset)
	}
	if got := topVisibleChangePath(m); got != "u07.go" {
		t.Fatalf("top visible change after rebuild = %q, want %q", got, "u07.go")
	}
	cmd := m.openCurrentItem()
	if cmd == nil {
		t.Fatal("expected openCurrentItem command after rebuild")
	}
	msg := cmd()
	diff, ok := msg.(messages.OpenDiff)
	if !ok {
		t.Fatalf("expected OpenDiff, got %T", msg)
	}
	if diff.Change == nil || diff.Change.Path != "u07.go" {
		got := "<nil>"
		if diff.Change != nil {
			got = diff.Change.Path
		}
		t.Fatalf("opened change path = %q, want %q", got, "u07.go")
	}
}

func TestChangesSetGitStatusPreservesVisibleCursorManualScrollAnchor(t *testing.T) {
	m := New()
	m.SetSize(80, 10)
	unstaged := make([]git.Change, 0, 20)
	for i := 0; i < 20; i++ {
		unstaged = append(unstaged, git.Change{
			Path: "u" + strconv.Itoa(i/10) + strconv.Itoa(i%10) + ".go",
			Kind: git.ChangeModified,
		})
	}
	m.SetGitStatus(&git.StatusResult{
		Clean:    false,
		Unstaged: unstaged,
	})
	m.cursor = 3
	m.scrollOffset = 1

	if !m.cursorVisible() {
		t.Fatalf("expected cursor to start visible, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}
	if got := topVisibleChangePath(m); got != "u00.go" {
		t.Fatalf("top visible change = %q, want %q", got, "u00.go")
	}

	m.SetGitStatus(&git.StatusResult{
		Clean: false,
		Staged: []git.Change{
			{Path: "s00.go", Kind: git.ChangeModified, Staged: true},
		},
		Unstaged: unstaged,
	})

	if m.scrollOffset != 3 {
		t.Fatalf("scrollOffset = %d, want 3", m.scrollOffset)
	}
	if got := topVisibleChangePath(m); got != "u00.go" {
		t.Fatalf("top visible change after rebuild = %q, want %q", got, "u00.go")
	}
	if !m.cursorVisible() {
		t.Fatalf("expected cursor to remain visible after rebuild, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}
}

func TestChangesSetGitStatusPreservesVisibleCursorAfterAnchorRestore(t *testing.T) {
	m := New()
	m.SetSize(80, 10)
	staged := make([]git.Change, 0, 8)
	for i := 0; i < 8; i++ {
		staged = append(staged, git.Change{
			Path:   "s" + strconv.Itoa(i/10) + strconv.Itoa(i%10) + ".go",
			Kind:   git.ChangeModified,
			Staged: true,
		})
	}
	unstaged := []git.Change{
		{Path: "u00.go", Kind: git.ChangeModified},
		{Path: "u01.go", Kind: git.ChangeModified},
		{Path: "u02.go", Kind: git.ChangeModified},
	}
	m.SetGitStatus(&git.StatusResult{
		Clean:    false,
		Staged:   staged,
		Unstaged: unstaged,
	})
	m.cursor = 11
	m.scrollOffset = 6

	if !m.cursorVisible() {
		t.Fatalf("expected cursor to start visible, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}
	if got := topVisibleChangePath(m); got != "s05.go" {
		t.Fatalf("top visible change = %q, want %q", got, "s05.go")
	}

	m.SetGitStatus(&git.StatusResult{
		Clean: false,
		Staged: []git.Change{
			staged[5], staged[6], staged[7],
		},
		Unstaged: unstaged,
	})

	if got := topVisibleChangePath(m); got != "s05.go" {
		t.Fatalf("top visible change after rebuild = %q, want %q", got, "s05.go")
	}
	if !m.cursorVisible() {
		t.Fatalf("expected cursor to remain visible after anchor restore, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}
}

func TestChangesSetGitStatusDoesNotFallbackAcrossSections(t *testing.T) {
	m := New()
	m.SetSize(80, 10)
	unstaged := []git.Change{
		{Path: "u00.go", Kind: git.ChangeModified},
		{Path: "shared.go", Kind: git.ChangeModified},
		{Path: "u01.go", Kind: git.ChangeModified},
		{Path: "u02.go", Kind: git.ChangeModified},
		{Path: "u03.go", Kind: git.ChangeModified},
		{Path: "u04.go", Kind: git.ChangeModified},
		{Path: "u05.go", Kind: git.ChangeModified},
		{Path: "u06.go", Kind: git.ChangeModified},
		{Path: "u07.go", Kind: git.ChangeModified},
		{Path: "u08.go", Kind: git.ChangeModified},
	}
	m.SetGitStatus(&git.StatusResult{
		Clean:    false,
		Unstaged: unstaged,
	})
	m.cursor = 3
	m.scrollOffset = 2

	if got := topVisibleChangePath(m); got != "shared.go" {
		t.Fatalf("top visible change = %q, want %q", got, "shared.go")
	}

	m.SetGitStatus(&git.StatusResult{
		Clean: false,
		Staged: []git.Change{
			{Path: "shared.go", Kind: git.ChangeModified, Staged: true},
		},
		Unstaged: []git.Change{
			{Path: "u00.go", Kind: git.ChangeModified},
			{Path: "u01.go", Kind: git.ChangeModified},
			{Path: "u02.go", Kind: git.ChangeModified},
			{Path: "u03.go", Kind: git.ChangeModified},
			{Path: "u04.go", Kind: git.ChangeModified},
			{Path: "u05.go", Kind: git.ChangeModified},
			{Path: "u06.go", Kind: git.ChangeModified},
			{Path: "u07.go", Kind: git.ChangeModified},
			{Path: "u08.go", Kind: git.ChangeModified},
		},
	})

	if got := topVisibleChangePath(m); got != "u00.go" {
		t.Fatalf("top visible change after rebuild = %q, want %q", got, "u00.go")
	}
}

func TestChangesSetGitStatusDoesNotExactMatchAcrossUntrackedAndUnstaged(t *testing.T) {
	m := New()
	m.SetSize(80, 10)
	m.SetGitStatus(&git.StatusResult{
		Clean: false,
		Untracked: []git.Change{
			{Path: "shared.go", Kind: git.ChangeUntracked},
			{Path: "u01.go", Kind: git.ChangeUntracked},
			{Path: "u02.go", Kind: git.ChangeUntracked},
			{Path: "u03.go", Kind: git.ChangeUntracked},
			{Path: "u04.go", Kind: git.ChangeUntracked},
			{Path: "u05.go", Kind: git.ChangeUntracked},
			{Path: "u06.go", Kind: git.ChangeUntracked},
			{Path: "u07.go", Kind: git.ChangeUntracked},
			{Path: "u08.go", Kind: git.ChangeUntracked},
		},
	})
	m.cursor = 2
	m.scrollOffset = 1

	if got := topVisibleChangePath(m); got != "shared.go" {
		t.Fatalf("top visible change = %q, want %q", got, "shared.go")
	}

	m.SetGitStatus(&git.StatusResult{
		Clean: false,
		Staged: []git.Change{
			{Path: "s00.go", Kind: git.ChangeModified, Staged: true},
		},
		Unstaged: []git.Change{
			{Path: "shared.go", Kind: git.ChangeModified},
			{Path: "u01.go", Kind: git.ChangeModified},
			{Path: "u02.go", Kind: git.ChangeModified},
			{Path: "u03.go", Kind: git.ChangeModified},
			{Path: "u04.go", Kind: git.ChangeModified},
			{Path: "u05.go", Kind: git.ChangeModified},
			{Path: "u06.go", Kind: git.ChangeModified},
			{Path: "u07.go", Kind: git.ChangeModified},
			{Path: "u08.go", Kind: git.ChangeModified},
		},
	})

	if got := topVisibleChangePath(m); got == "shared.go" {
		t.Fatalf("top visible change after rebuild = %q, want a non-cross-section match", got)
	}
}

func TestChangesSetGitStatusPreservesHeaderAlignedManualScrollAnchor(t *testing.T) {
	m := New()
	m.SetSize(80, 10)
	unstaged := make([]git.Change, 0, 20)
	for i := 0; i < 20; i++ {
		unstaged = append(unstaged, git.Change{
			Path: "u" + strconv.Itoa(i/10) + strconv.Itoa(i%10) + ".go",
			Kind: git.ChangeModified,
		})
	}
	m.SetGitStatus(&git.StatusResult{
		Clean:    false,
		Unstaged: unstaged,
	})
	m.cursor = 10
	m.scrollOffset = 0

	item := topVisibleItem(m)
	if !item.isHeader || item.header != "Unstaged (20)" {
		t.Fatalf("top visible item = %+v, want Unstaged header", item)
	}

	m.SetGitStatus(&git.StatusResult{
		Clean: false,
		Staged: []git.Change{
			{Path: "s00.go", Kind: git.ChangeModified, Staged: true},
		},
		Unstaged: unstaged,
	})

	if m.scrollOffset != 2 {
		t.Fatalf("scrollOffset = %d, want 2", m.scrollOffset)
	}
	item = topVisibleItem(m)
	if !item.isHeader || item.header != "Unstaged (20)" {
		t.Fatalf("top visible item after rebuild = %+v, want Unstaged header", item)
	}
}

func TestChangesSetGitStatusPreservesHeaderOnlyManualScrollAnchor(t *testing.T) {
	m := New()
	m.SetSize(80, 2)
	unstaged := []git.Change{
		{Path: "u00.go", Kind: git.ChangeModified},
		{Path: "u01.go", Kind: git.ChangeModified},
	}
	m.SetGitStatus(&git.StatusResult{
		Clean:    false,
		Unstaged: unstaged,
	})
	m.cursor = 1
	m.scrollOffset = 0

	if m.visibleHeight() != 1 {
		t.Fatalf("visibleHeight = %d, want 1", m.visibleHeight())
	}
	item := topVisibleItem(m)
	if !item.isHeader || item.header != "Unstaged (2)" {
		t.Fatalf("top visible item = %+v, want Unstaged header", item)
	}

	m.SetGitStatus(&git.StatusResult{
		Clean: false,
		Staged: []git.Change{
			{Path: "s00.go", Kind: git.ChangeModified, Staged: true},
		},
		Unstaged: unstaged,
	})

	if m.scrollOffset != 2 {
		t.Fatalf("scrollOffset = %d, want 2", m.scrollOffset)
	}
	item = topVisibleItem(m)
	if !item.isHeader || item.header != "Unstaged (2)" {
		t.Fatalf("top visible item after rebuild = %+v, want Unstaged header", item)
	}
}

func TestProjectTreeReloadPreservesManualScrollAnchor(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		"b.txt", "c.txt", "d.txt", "e.txt", "f.txt", "g.txt", "h.txt", "i.txt",
		"j.txt", "k.txt", "l.txt", "m.txt", "n.txt", "o.txt", "p.txt", "q.txt",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatalf("write %q: %v", name, err)
		}
	}

	tree := NewProjectTree()
	tree.SetSize(80, 10)
	tree.Focus()
	tree.SetWorkspace(data.NewWorkspace("feature", "feature", "main", root, root))
	tree.cursor = 1
	tree.scrollOffset = 5

	if got := filepath.Base(tree.flatNodes[tree.scrollOffset].Path); got != "g.txt" {
		t.Fatalf("top visible node = %q, want %q", got, "g.txt")
	}

	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	_, _ = tree.Update(tea.KeyPressMsg{Text: "r", Code: 'r'})

	if tree.scrollOffset != 6 {
		t.Fatalf("scrollOffset = %d, want 6", tree.scrollOffset)
	}
	if got := filepath.Base(tree.flatNodes[tree.scrollOffset].Path); got != "g.txt" {
		t.Fatalf("top visible node after reload = %q, want %q", got, "g.txt")
	}
	cmd := tree.handleEnter()
	if cmd == nil {
		t.Fatal("expected handleEnter command after reload")
	}
	msg := cmd()
	opened, ok := msg.(OpenFileInEditor)
	if !ok {
		t.Fatalf("expected OpenFileInEditor, got %T", msg)
	}
	if got := filepath.Base(opened.Path); got != "g.txt" {
		t.Fatalf("opened path = %q, want %q", got, "g.txt")
	}
}

func TestProjectTreeReloadPreservesVisibleCursorManualScrollAnchor(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		"b.txt", "c.txt", "d.txt", "e.txt", "f.txt", "g.txt", "h.txt", "i.txt",
		"j.txt", "k.txt", "l.txt", "m.txt", "n.txt", "o.txt", "p.txt", "q.txt",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatalf("write %q: %v", name, err)
		}
	}

	tree := NewProjectTree()
	tree.SetSize(80, 10)
	tree.Focus()
	tree.SetWorkspace(data.NewWorkspace("feature", "feature", "main", root, root))
	tree.cursor = 3
	tree.scrollOffset = 1

	if !treeCursorVisible(tree) {
		t.Fatalf("expected cursor to start visible, cursor=%d scrollOffset=%d visibleHeight=%d",
			tree.cursor, tree.scrollOffset, tree.visibleHeight())
	}
	if got := filepath.Base(tree.flatNodes[tree.scrollOffset].Path); got != "c.txt" {
		t.Fatalf("top visible node = %q, want %q", got, "c.txt")
	}

	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	_, _ = tree.Update(tea.KeyPressMsg{Text: "r", Code: 'r'})

	if tree.scrollOffset != 2 {
		t.Fatalf("scrollOffset = %d, want 2", tree.scrollOffset)
	}
	if got := filepath.Base(tree.flatNodes[tree.scrollOffset].Path); got != "c.txt" {
		t.Fatalf("top visible node after reload = %q, want %q", got, "c.txt")
	}
	if !treeCursorVisible(tree) {
		t.Fatalf("expected cursor to remain visible after reload, cursor=%d scrollOffset=%d visibleHeight=%d",
			tree.cursor, tree.scrollOffset, tree.visibleHeight())
	}
}

func TestProjectTreeReloadPreservesVisibleCursorAfterAnchorRestore(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		"a.txt", "b.txt", "c.txt", "d.txt", "e.txt", "f.txt", "g.txt", "h.txt",
		"i.txt", "j.txt", "k.txt", "l.txt", "m.txt",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatalf("write %q: %v", name, err)
		}
	}

	tree := NewProjectTree()
	tree.SetSize(80, 10)
	tree.Focus()
	tree.SetWorkspace(data.NewWorkspace("feature", "feature", "main", root, root))
	tree.cursor = 10
	tree.scrollOffset = 5

	if !treeCursorVisible(tree) {
		t.Fatalf("expected cursor to start visible, cursor=%d scrollOffset=%d visibleHeight=%d",
			tree.cursor, tree.scrollOffset, tree.visibleHeight())
	}
	if got := filepath.Base(tree.flatNodes[tree.scrollOffset].Path); got != "f.txt" {
		t.Fatalf("top visible node = %q, want %q", got, "f.txt")
	}

	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatalf("remove %q: %v", name, err)
		}
	}
	_, _ = tree.Update(tea.KeyPressMsg{Text: "r", Code: 'r'})

	if got := filepath.Base(tree.flatNodes[tree.scrollOffset].Path); got != "f.txt" {
		t.Fatalf("top visible node after reload = %q, want %q", got, "f.txt")
	}
	if !treeCursorVisible(tree) {
		t.Fatalf("expected cursor to remain visible after anchor restore, cursor=%d scrollOffset=%d visibleHeight=%d",
			tree.cursor, tree.scrollOffset, tree.visibleHeight())
	}
}
