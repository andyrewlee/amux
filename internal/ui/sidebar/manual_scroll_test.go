package sidebar

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
)

func TestChangesViewPreservesManualScrollOffset(t *testing.T) {
	m := setupChangesScrollModel()
	m.cursor = 1
	m.scrollOffset = 6

	_ = m.View()

	if m.scrollOffset != 6 {
		t.Fatalf("scrollOffset = %d, want 6", m.scrollOffset)
	}
}

func TestTabbedChangesViewPreservesManualScrollOffset(t *testing.T) {
	sidebar := setupTabbedChangesScrollModel()
	changes := sidebar.Changes()
	changes.cursor = 1
	changes.scrollOffset = 6

	_ = sidebar.View()

	if changes.scrollOffset != 6 {
		t.Fatalf("scrollOffset = %d, want 6", changes.scrollOffset)
	}
}

func TestTabbedChangesContentViewPreservesManualScrollOffset(t *testing.T) {
	sidebar := setupTabbedChangesScrollModel()
	changes := sidebar.Changes()
	changes.cursor = 1
	changes.scrollOffset = 6

	_ = sidebar.ContentView()

	if changes.scrollOffset != 6 {
		t.Fatalf("scrollOffset = %d, want 6", changes.scrollOffset)
	}
}

func TestChangesMouseWheelScrollMovesViewportNotCursor(t *testing.T) {
	m := setupChangesScrollModel()
	m.cursor = 1

	_, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})

	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
	if m.scrollOffset == 0 {
		t.Fatal("expected scrollOffset to increase after wheel scroll")
	}
}

func TestChangesPageScrollUsesViewportOffset(t *testing.T) {
	m := setupChangesScrollModel()
	m.cursor = 1

	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.cursor != 5 {
		t.Fatalf("cursor = %d, want 5 after PgDown", m.cursor)
	}
	if m.scrollOffset != 4 {
		t.Fatalf("scrollOffset = %d, want 4 after PgDown", m.scrollOffset)
	}
	if !changesCursorVisible(m) {
		t.Fatalf("expected cursor to stay visible after PgDown, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}
	cmd := m.openCurrentItem()
	if cmd == nil {
		t.Fatal("expected openCurrentItem command after PgDown")
	}
	msg := cmd()
	diff, ok := msg.(messages.OpenDiff)
	if !ok {
		t.Fatalf("expected OpenDiff, got %T", msg)
	}
	if diff.Change == nil || diff.Change.Path != "file-04.txt" {
		got := "<nil>"
		if diff.Change != nil {
			got = diff.Change.Path
		}
		t.Fatalf("opened change path = %q, want %q", got, "file-04.txt")
	}

	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 after PgUp", m.cursor)
	}
	if m.scrollOffset != 0 {
		t.Fatalf("scrollOffset = %d, want 0 after PgUp", m.scrollOffset)
	}

	m.cursor = 1
	m.scrollOffset = 10
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.cursor != 14 || m.scrollOffset != 12 {
		t.Fatalf("after hidden-cursor PgDown cursor=%d scrollOffset=%d, want cursor=14 scrollOffset=12", m.cursor, m.scrollOffset)
	}
	cmd = m.openCurrentItem()
	if cmd == nil {
		t.Fatal("expected openCurrentItem command after hidden-cursor PgDown")
	}
	msg = cmd()
	diff, ok = msg.(messages.OpenDiff)
	if !ok {
		t.Fatalf("expected OpenDiff after hidden-cursor PgDown, got %T", msg)
	}
	if diff.Change == nil || diff.Change.Path != "file-13.txt" {
		got := "<nil>"
		if diff.Change != nil {
			got = diff.Change.Path
		}
		t.Fatalf("hidden-cursor PgDown opened change path = %q, want %q", got, "file-13.txt")
	}
}

func TestChangesFilterModeKeepsCursorVisible(t *testing.T) {
	m := setupChangesScrollModel()
	m.cursor = 1
	m.scrollOffset = 6

	_, _ = m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	_ = m.View()

	if !changesCursorVisible(m) {
		t.Fatalf("expected cursor to be visible after entering filter mode, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}
}

func TestChangesSetShowKeymapHintsKeepsCursorVisible(t *testing.T) {
	m := setupChangesScrollModel()
	m.cursor = 1
	m.scrollOffset = 6

	m.SetShowKeymapHints(true)
	_ = m.View()

	if !changesCursorVisible(m) {
		t.Fatalf("expected cursor to be visible after enabling keymap hints, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}
}

func TestChangesHideKeymapHintsPreservesManualScrollOffset(t *testing.T) {
	m := setupChangesScrollModel()
	m.showKeymapHints = true
	m.cursor = 1
	m.scrollOffset = 6
	oldVisibleHeight := m.visibleHeight()

	m.SetShowKeymapHints(false)
	_ = m.View()

	if m.visibleHeight() <= oldVisibleHeight {
		t.Fatalf("visibleHeight = %d, want > %d", m.visibleHeight(), oldVisibleHeight)
	}
	if m.scrollOffset != 6 {
		t.Fatalf("scrollOffset = %d, want 6", m.scrollOffset)
	}
}

func TestChangesHeightIncreasePreservesManualScrollOffset(t *testing.T) {
	m := setupChangesScrollModel()
	m.cursor = 1
	m.scrollOffset = 6
	oldVisibleHeight := m.visibleHeight()

	m.SetSize(80, 12)
	_ = m.View()

	if m.visibleHeight() <= oldVisibleHeight {
		t.Fatalf("visibleHeight = %d, want > %d", m.visibleHeight(), oldVisibleHeight)
	}
	if m.scrollOffset != 6 {
		t.Fatalf("scrollOffset = %d, want 6", m.scrollOffset)
	}
}

func TestChangesWidthOnlyResizePreservesManualScrollOffset(t *testing.T) {
	m := setupChangesScrollModel()
	m.cursor = 1
	m.scrollOffset = 6
	oldVisibleHeight := m.visibleHeight()

	m.SetSize(60, 10)
	_ = m.View()

	if m.visibleHeight() != oldVisibleHeight {
		t.Fatalf("visibleHeight = %d, want %d", m.visibleHeight(), oldVisibleHeight)
	}
	if m.scrollOffset != 6 {
		t.Fatalf("scrollOffset = %d, want 6", m.scrollOffset)
	}
}

func TestProjectTreeViewPreservesManualScrollOffset(t *testing.T) {
	tree := setupProjectTreeScrollModel(t)
	tree.cursor = 1
	tree.scrollOffset = 6

	_ = tree.View()

	if tree.scrollOffset != 6 {
		t.Fatalf("scrollOffset = %d, want 6", tree.scrollOffset)
	}
}

func TestTabbedProjectViewPreservesManualScrollOffset(t *testing.T) {
	sidebar := setupTabbedProjectScrollModel(t)
	tree := sidebar.ProjectTree()
	tree.cursor = 1
	tree.scrollOffset = 6

	_ = sidebar.View()

	if tree.scrollOffset != 6 {
		t.Fatalf("scrollOffset = %d, want 6", tree.scrollOffset)
	}
}

func TestTabbedProjectContentViewPreservesManualScrollOffset(t *testing.T) {
	sidebar := setupTabbedProjectScrollModel(t)
	tree := sidebar.ProjectTree()
	tree.cursor = 1
	tree.scrollOffset = 6

	_ = sidebar.ContentView()

	if tree.scrollOffset != 6 {
		t.Fatalf("scrollOffset = %d, want 6", tree.scrollOffset)
	}
}

func TestProjectTreeMouseWheelScrollMovesViewportNotCursor(t *testing.T) {
	tree := setupProjectTreeScrollModel(t)
	tree.cursor = 1

	_, _ = tree.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})

	if tree.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", tree.cursor)
	}
	if tree.scrollOffset == 0 {
		t.Fatal("expected scrollOffset to increase after wheel scroll")
	}
}

func TestProjectTreePageScrollUsesViewportOffset(t *testing.T) {
	tree := setupProjectTreeScrollModel(t)
	tree.cursor = 1

	_, _ = tree.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if tree.cursor != 6 {
		t.Fatalf("cursor = %d, want 6 after PgDown", tree.cursor)
	}
	if tree.scrollOffset != 5 {
		t.Fatalf("scrollOffset = %d, want 5 after PgDown", tree.scrollOffset)
	}
	if !treeCursorVisible(tree) {
		t.Fatalf("expected cursor to stay visible after PgDown, cursor=%d scrollOffset=%d visibleHeight=%d",
			tree.cursor, tree.scrollOffset, tree.visibleHeight())
	}
	cmd := tree.handleEnter()
	if cmd == nil {
		t.Fatal("expected handleEnter command after PgDown")
	}
	msg := cmd()
	opened, ok := msg.(OpenFileInEditor)
	if !ok {
		t.Fatalf("expected OpenFileInEditor, got %T", msg)
	}
	if got := filepath.Base(opened.Path); got != "file-06.txt" {
		t.Fatalf("opened path = %q, want %q", got, "file-06.txt")
	}

	_, _ = tree.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if tree.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 after PgUp", tree.cursor)
	}
	if tree.scrollOffset != 0 {
		t.Fatalf("scrollOffset = %d, want 0 after PgUp", tree.scrollOffset)
	}

	tree.cursor = 1
	tree.scrollOffset = 10
	_, _ = tree.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if tree.cursor != 15 || tree.scrollOffset != 10 {
		t.Fatalf("after hidden-cursor PgDown cursor=%d scrollOffset=%d, want cursor=15 scrollOffset=10", tree.cursor, tree.scrollOffset)
	}
	cmd = tree.handleEnter()
	if cmd == nil {
		t.Fatal("expected handleEnter command after hidden-cursor PgDown")
	}
	msg = cmd()
	opened, ok = msg.(OpenFileInEditor)
	if !ok {
		t.Fatalf("expected OpenFileInEditor after hidden-cursor PgDown, got %T", msg)
	}
	if got := filepath.Base(opened.Path); got != "file-15.txt" {
		t.Fatalf("hidden-cursor PgDown opened path = %q, want %q", got, "file-15.txt")
	}
}

func TestProjectTreeParentJumpKeepsCursorVisible(t *testing.T) {
	tree := setupNestedProjectTreeScrollModel(t)
	tree.cursor = 9
	tree.scrollOffset = 6

	_, _ = tree.Update(tea.KeyPressMsg{Text: "h", Code: 'h'})

	if tree.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", tree.cursor)
	}
	if !treeCursorVisible(tree) {
		t.Fatalf("expected cursor to be visible after parent jump, cursor=%d scrollOffset=%d visibleHeight=%d",
			tree.cursor, tree.scrollOffset, tree.visibleHeight())
	}
}

func TestProjectTreeResizeKeepsCursorVisible(t *testing.T) {
	tree := setupProjectTreeScrollModel(t)
	tree.cursor = 1
	tree.scrollOffset = 6

	tree.SetSize(80, 8)
	_ = tree.View()

	if !treeCursorVisible(tree) {
		t.Fatalf("expected cursor to be visible after resize, cursor=%d scrollOffset=%d visibleHeight=%d",
			tree.cursor, tree.scrollOffset, tree.visibleHeight())
	}
}

func TestProjectTreeWidthOnlyResizePreservesManualScrollOffset(t *testing.T) {
	tree := setupProjectTreeScrollModel(t)
	tree.cursor = 1
	tree.scrollOffset = 6
	oldVisibleHeight := tree.visibleHeight()

	tree.SetSize(60, 10)
	_ = tree.View()

	if tree.visibleHeight() != oldVisibleHeight {
		t.Fatalf("visibleHeight = %d, want %d", tree.visibleHeight(), oldVisibleHeight)
	}
	if tree.scrollOffset != 6 {
		t.Fatalf("scrollOffset = %d, want 6", tree.scrollOffset)
	}
}

func TestProjectTreeSetShowKeymapHintsKeepsCursorVisible(t *testing.T) {
	tree := setupProjectTreeScrollModel(t)
	tree.cursor = 1
	tree.scrollOffset = 6

	tree.SetShowKeymapHints(true)
	_ = tree.View()

	if !treeCursorVisible(tree) {
		t.Fatalf("expected cursor to be visible after enabling keymap hints, cursor=%d scrollOffset=%d visibleHeight=%d",
			tree.cursor, tree.scrollOffset, tree.visibleHeight())
	}
}

func TestProjectTreeHideKeymapHintsPreservesManualScrollOffset(t *testing.T) {
	tree := setupProjectTreeScrollModel(t)
	tree.showKeymapHints = true
	tree.cursor = 1
	tree.scrollOffset = 6
	oldVisibleHeight := tree.visibleHeight()

	tree.SetShowKeymapHints(false)
	_ = tree.View()

	if tree.visibleHeight() <= oldVisibleHeight {
		t.Fatalf("visibleHeight = %d, want > %d", tree.visibleHeight(), oldVisibleHeight)
	}
	if tree.scrollOffset != 6 {
		t.Fatalf("scrollOffset = %d, want 6", tree.scrollOffset)
	}
}

func TestProjectTreeHeightIncreasePreservesManualScrollOffset(t *testing.T) {
	tree := setupProjectTreeScrollModel(t)
	tree.cursor = 1
	tree.scrollOffset = 6
	oldVisibleHeight := tree.visibleHeight()

	tree.SetSize(80, 12)
	_ = tree.View()

	if tree.visibleHeight() <= oldVisibleHeight {
		t.Fatalf("visibleHeight = %d, want > %d", tree.visibleHeight(), oldVisibleHeight)
	}
	if tree.scrollOffset != 6 {
		t.Fatalf("scrollOffset = %d, want 6", tree.scrollOffset)
	}
}
