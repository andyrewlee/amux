package sidebar

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestChangesKeyboardActionsReanchorAfterWheelScroll(t *testing.T) {
	m := setupChangesScrollModel()
	m.cursor = 1
	m.scrollOffset = 10

	cmd := m.openCurrentItem()
	if cmd == nil {
		t.Fatal("expected openCurrentItem command after wheel scroll")
	}
	msg := cmd()
	diff, ok := msg.(messages.OpenDiff)
	if !ok {
		t.Fatalf("expected OpenDiff, got %T", msg)
	}
	if diff.Change == nil || diff.Change.Path != "file-09.txt" {
		got := "<nil>"
		if diff.Change != nil {
			got = diff.Change.Path
		}
		t.Fatalf("opened change path = %q, want %q", got, "file-09.txt")
	}

	m.cursor = 1
	m.scrollOffset = 10
	_, _ = m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	if m.cursor != 11 || m.scrollOffset != 10 {
		t.Fatalf("after j cursor=%d scrollOffset=%d, want cursor=11 scrollOffset=10", m.cursor, m.scrollOffset)
	}
	if !changesCursorVisible(m) {
		t.Fatalf("expected cursor to be visible after j, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}
}

func TestChangesKeyboardActionsReanchorInsideShortViewport(t *testing.T) {
	m := New()
	m.SetSize(80, 2)
	m.SetGitStatus(&git.StatusResult{
		Clean: false,
		Staged: []git.Change{
			{Path: "staged.txt", Kind: git.ChangeModified, Staged: true},
		},
		Unstaged: []git.Change{
			{Path: "unstaged.txt", Kind: git.ChangeModified},
		},
	})
	m.cursor = 1
	m.scrollOffset = 2

	if m.cursorVisible() {
		t.Fatalf("expected cursor to start hidden, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}

	cmd := m.openCurrentItem()
	if cmd == nil {
		t.Fatal("expected openCurrentItem command after short-viewport reanchor")
	}
	if !changesCursorVisible(m) {
		t.Fatalf("expected cursor to be visible after reanchor, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}
	msg := cmd()
	diff, ok := msg.(messages.OpenDiff)
	if !ok {
		t.Fatalf("expected OpenDiff, got %T", msg)
	}
	if diff.Change == nil || diff.Change.Path != "unstaged.txt" {
		got := "<nil>"
		if diff.Change != nil {
			got = diff.Change.Path
		}
		t.Fatalf("opened change path = %q, want %q", got, "unstaged.txt")
	}
}

func TestChangesKeyboardActionsReanchorBelowHeaderOnlyViewport(t *testing.T) {
	m := New()
	m.SetSize(80, 2)
	m.SetGitStatus(&git.StatusResult{
		Clean: false,
		Staged: []git.Change{
			{Path: "staged.txt", Kind: git.ChangeModified, Staged: true},
		},
		Unstaged: []git.Change{
			{Path: "unstaged.txt", Kind: git.ChangeModified},
		},
	})
	m.cursor = 3
	m.scrollOffset = 2

	if m.cursorVisible() {
		t.Fatalf("expected cursor to start hidden, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}

	cmd := m.openCurrentItem()
	if cmd == nil {
		t.Fatal("expected openCurrentItem command after short-viewport downward reanchor")
	}
	if !changesCursorVisible(m) {
		t.Fatalf("expected cursor to be visible after reanchor, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}
	msg := cmd()
	diff, ok := msg.(messages.OpenDiff)
	if !ok {
		t.Fatalf("expected OpenDiff, got %T", msg)
	}
	if diff.Change == nil || diff.Change.Path != "unstaged.txt" {
		got := "<nil>"
		if diff.Change != nil {
			got = diff.Change.Path
		}
		t.Fatalf("opened change path = %q, want %q", got, "unstaged.txt")
	}
}

func TestChangesMoveCursorDoesNotSkipFirstFileInHeaderOnlyViewport(t *testing.T) {
	m := New()
	m.SetSize(80, 2)
	m.Focus()
	m.SetGitStatus(&git.StatusResult{
		Clean: false,
		Staged: []git.Change{
			{Path: "staged.txt", Kind: git.ChangeModified, Staged: true},
		},
		Unstaged: []git.Change{
			{Path: "unstaged.txt", Kind: git.ChangeModified},
		},
	})
	m.cursor = 3
	m.scrollOffset = 0

	if m.cursorVisible() {
		t.Fatalf("expected cursor to start hidden, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}

	_, _ = m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})

	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 after j", m.cursor)
	}
	if !changesCursorVisible(m) {
		t.Fatalf("expected cursor to be visible after j, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}
}

func TestChangesPageDownDoesNotSkipFirstFileInHeaderOnlyViewport(t *testing.T) {
	m := New()
	m.SetSize(80, 2)
	m.Focus()
	m.SetGitStatus(&git.StatusResult{
		Clean: false,
		Staged: []git.Change{
			{Path: "staged.txt", Kind: git.ChangeModified, Staged: true},
		},
		Unstaged: []git.Change{
			{Path: "unstaged.txt", Kind: git.ChangeModified},
		},
	})
	m.cursor = 3
	m.scrollOffset = 0

	if m.cursorVisible() {
		t.Fatalf("expected cursor to start hidden, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}

	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})

	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 after PgDown", m.cursor)
	}
	if !changesCursorVisible(m) {
		t.Fatalf("expected cursor to be visible after PgDown, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}
}

func TestProjectTreeKeyboardActionsReanchorAfterWheelScroll(t *testing.T) {
	tree := setupProjectTreeScrollModel(t)
	tree.cursor = 1
	tree.scrollOffset = 10

	cmd := tree.handleEnter()
	if cmd == nil {
		t.Fatal("expected handleEnter command after wheel scroll")
	}
	msg := cmd()
	opened, ok := msg.(OpenFileInEditor)
	if !ok {
		t.Fatalf("expected OpenFileInEditor, got %T", msg)
	}
	if got := filepath.Base(opened.Path); got != "file-10.txt" {
		t.Fatalf("opened path = %q, want %q", got, "file-10.txt")
	}

	tree.cursor = 1
	tree.scrollOffset = 10
	_, _ = tree.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	if tree.cursor != 11 || tree.scrollOffset != 10 {
		t.Fatalf("after j cursor=%d scrollOffset=%d, want cursor=11 scrollOffset=10", tree.cursor, tree.scrollOffset)
	}
	if !treeCursorVisible(tree) {
		t.Fatalf("expected cursor to be visible after j, cursor=%d scrollOffset=%d visibleHeight=%d",
			tree.cursor, tree.scrollOffset, tree.visibleHeight())
	}
}
