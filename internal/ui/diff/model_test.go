package diff

import (
	"testing"

	tea "charm.land/bubbletea/v2"

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

func TestContentHeight(t *testing.T) {
	m := &Model{height: 2}
	if got := m.contentHeight(); got != 0 {
		t.Fatalf("expected content height 0 below the chrome tier, got %d", got)
	}

	m.height = 10
	if got := m.contentHeight(); got != 7 {
		t.Fatalf("expected content height 7, got %d", got)
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

func TestDiffLoaded_WithResultErrorShowsError(t *testing.T) {
	m := &Model{}

	updated, _ := m.Update(diffLoaded{diff: &git.DiffResult{Error: "fatal: bad revision"}})

	if updated.err == nil {
		t.Fatal("expected DiffResult.Error to populate model error")
	}
	if updated.err.Error() != "fatal: bad revision" {
		t.Fatalf("model error = %q, want %q", updated.err.Error(), "fatal: bad revision")
	}
	if updated.diff != nil {
		t.Fatalf("expected errored diff not to be retained, got %+v", updated.diff)
	}
}

func TestPageScrollUsesMinimumOneLine(t *testing.T) {
	m := newModelWithDiff(4, 10, nil)
	m.focused = true

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.scroll != 1 {
		t.Fatalf("expected PgDown on a short diff to scroll by 1 line, got %d", m.scroll)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.scroll != 0 {
		t.Fatalf("expected PgUp on a short diff to scroll back to 0, got %d", m.scroll)
	}
}

// TestHunkCycleWrapsPastClampedScrollBound exercises the geometry the
// original fixture missed: two hunk tops sit beyond maxScroll, so parking on
// either clamps scroll to the same value — position-derived navigation would
// re-select the same hunk forever instead of wrapping.
func TestHunkCycleWrapsPastClampedScrollBound(t *testing.T) {
	hunks := []git.Hunk{
		{StartLine: 2},
		{StartLine: 28},
		{StartLine: 29},
	}
	m := newModelWithDiff(6, 30, hunks)

	// Precondition (finding 38's fixture flaw): the clamp must actually
	// engage — both tail hunk tops must exceed maxScroll.
	rows := m.rows()
	if rows.topFor(28) <= m.maxScroll() || rows.topFor(29) <= m.maxScroll() {
		t.Fatalf("fixture must clamp tail hunk tops: top(28)=%d top(29)=%d maxScroll=%d",
			rows.topFor(28), rows.topFor(29), m.maxScroll())
	}

	m.scroll = 0
	var got []int
	for i := 0; i <= len(hunks); i++ {
		m.nextHunk()
		got = append(got, m.hunkIdx)
	}
	want := []int{0, 1, 2, 0}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("nextHunk sequence = %v, want %v — the cycle must advance past the clamped tail instead of sticking", got, want)
		}
	}

	// prevHunk mirrors: from the first hunk it wraps to the last.
	m.prevHunk()
	if m.hunkIdx != 2 {
		t.Fatalf("prevHunk from hunk 0 should wrap to the last hunk, got idx=%d", m.hunkIdx)
	}
	m.prevHunk()
	if m.hunkIdx != 1 {
		t.Fatalf("prevHunk should retreat to hunk 1, got idx=%d", m.hunkIdx)
	}
}

// TestHunkCycleAfterManualScroll preserves the position-derived path: when
// the user scrolls off the parked hunk, n jumps to the first hunk below the
// viewport top rather than resuming the stale index.
func TestHunkCycleAfterManualScroll(t *testing.T) {
	hunks := []git.Hunk{
		{StartLine: 2},
		{StartLine: 8},
		{StartLine: 14},
	}
	m := newModelWithDiff(10, 40, hunks)

	m.scroll = 10 // mid-diff, between hunk 1 (top ~8) and hunk 2 (top ~14)
	m.hunkIdx = 0 // stale selection left behind by earlier navigation

	m.nextHunk()
	if m.hunkIdx != 2 {
		t.Fatalf("manual scroll off the selected hunk: n should pick the first hunk below the viewport, got idx=%d", m.hunkIdx)
	}
}
