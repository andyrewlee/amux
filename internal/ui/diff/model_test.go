package diff

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
)

func newModelWithDiff(height, lines int, hunks []git.Hunk) *Model {
	m := &Model{height: height}
	m.diff = &git.DiffResult{
		Lines: make([]git.DiffLine, lines),
		Hunks: hunks,
	}
	return m
}

func TestVisibleHeight(t *testing.T) {
	m := &Model{height: 2}
	if got := m.visibleHeight(); got != 1 {
		t.Fatalf("expected visible height 1, got %d", got)
	}

	m.height = 10
	if got := m.visibleHeight(); got != 7 {
		t.Fatalf("expected visible height 7, got %d", got)
	}
}

func TestMaxScrollAndScrollClamp(t *testing.T) {
	m := newModelWithDiff(6, 10, nil)
	if got := m.maxScroll(); got != 7 {
		t.Fatalf("expected maxScroll 7, got %d", got)
	}

	m.scrollDown(100)
	if m.scroll != 7 {
		t.Fatalf("expected scroll clamp to 7, got %d", m.scroll)
	}

	m.scrollUp(50)
	if m.scroll != 0 {
		t.Fatalf("expected scroll clamp to 0, got %d", m.scroll)
	}

	m = newModelWithDiff(6, 2, nil)
	if got := m.maxScroll(); got != 0 {
		t.Fatalf("expected maxScroll 0 with short diff, got %d", got)
	}
}

func TestHunkNavigation(t *testing.T) {
	hunks := []git.Hunk{
		{StartLine: 2},
		{StartLine: 5},
		{StartLine: 8},
	}
	m := newModelWithDiff(8, 20, hunks)

	m.scroll = 0
	m.nextHunk()
	if m.scroll != 2 || m.hunkIdx != 0 {
		t.Fatalf("expected first hunk at 2, idx 0, got scroll=%d idx=%d", m.scroll, m.hunkIdx)
	}

	m.nextHunk()
	if m.scroll != 5 || m.hunkIdx != 1 {
		t.Fatalf("expected next hunk at 5, idx 1, got scroll=%d idx=%d", m.scroll, m.hunkIdx)
	}

	m.scroll = 9
	m.nextHunk()
	if m.scroll != 2 || m.hunkIdx != 0 {
		t.Fatalf("expected wrap to first hunk, got scroll=%d idx=%d", m.scroll, m.hunkIdx)
	}

	m.scroll = 5
	m.prevHunk()
	if m.scroll != 2 || m.hunkIdx != 0 {
		t.Fatalf("expected previous hunk at 2, idx 0, got scroll=%d idx=%d", m.scroll, m.hunkIdx)
	}

	m.scroll = 1
	m.prevHunk()
	if m.scroll != 8 || m.hunkIdx != 2 {
		t.Fatalf("expected wrap to last hunk at 8, idx 2, got scroll=%d idx=%d", m.scroll, m.hunkIdx)
	}
}

func TestResetSource_ResetsScrollState(t *testing.T) {
	ws := data.NewWorkspace("ws", "ws", "main", "/repo", "/repo")
	m := &Model{
		workspace: ws,
		change:    &git.Change{Path: "before.go", Kind: git.ChangeModified},
		mode:      git.DiffModeUnstaged,
		diff:      &git.DiffResult{Lines: make([]git.DiffLine, 20)},
		loading:   false,
		scroll:    12,
		hunkIdx:   3,
		err:       nil,
	}

	m.ResetSource(ws, &git.Change{Path: "after.go", Kind: git.ChangeModified}, git.DiffModeStaged)

	if !m.loading {
		t.Fatal("expected reset source to mark model loading")
	}
	if m.scroll != 0 {
		t.Fatalf("expected scroll reset to 0, got %d", m.scroll)
	}
	if m.hunkIdx != 0 {
		t.Fatalf("expected hunk index reset to 0, got %d", m.hunkIdx)
	}
	if m.diff != nil {
		t.Fatal("expected previous diff content to be cleared")
	}
	if m.change == nil || m.change.Path != "after.go" {
		t.Fatalf("expected source change to update, got %+v", m.change)
	}
	if m.mode != git.DiffModeStaged {
		t.Fatalf("expected mode update to staged, got %v", m.mode)
	}
}

func TestDiffLoaded_IgnoresStaleLoadCompletion(t *testing.T) {
	m := &Model{
		loadID:  2,
		loading: true,
	}
	staleDiff := &git.DiffResult{Path: "stale.go"}
	currentDiff := &git.DiffResult{Path: "current.go"}

	updated, _ := m.Update(diffLoaded{loadID: 1, diff: staleDiff})
	if !updated.loading {
		t.Fatal("expected stale load to keep current load in progress")
	}
	if updated.diff != nil {
		t.Fatalf("expected stale load to be ignored, got %+v", updated.diff)
	}

	updated, _ = updated.Update(diffLoaded{loadID: 2, diff: currentDiff})
	if updated.loading {
		t.Fatal("expected current load to finish")
	}
	if updated.diff != currentDiff {
		t.Fatalf("expected current diff to win, got %+v", updated.diff)
	}
}
