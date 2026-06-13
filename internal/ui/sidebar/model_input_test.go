package sidebar

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
)

// twoSectionStatus builds a status with two sections (Staged + Unstaged) so the
// display list contains header rows separating change rows:
//
//	index 0: header "Staged (2)"
//	index 1: a.go  (Staged[0])
//	index 2: b.go  (Staged[1])
//	index 3: header "Unstaged (2)"
//	index 4: c.go  (Unstaged[0])
//	index 5: d.go  (Unstaged[1])
func twoSectionStatus() *git.StatusResult {
	return &git.StatusResult{
		Clean: false,
		Staged: []git.Change{
			{Path: "a.go", Kind: git.ChangeModified, Staged: true},
			{Path: "b.go", Kind: git.ChangeModified, Staged: true},
		},
		Unstaged: []git.Change{
			{Path: "c.go", Kind: git.ChangeModified},
			{Path: "d.go", Kind: git.ChangeModified},
		},
	}
}

func newInputModel(t *testing.T) *Model {
	t.Helper()
	m := New()
	m.SetSize(80, 20)
	m.Focus()
	m.SetGitStatus(twoSectionStatus())
	// Sanity-check the layout the cursor math depends on.
	if len(m.displayItems) != 6 {
		t.Fatalf("expected 6 display items (2 headers + 4 changes), got %d", len(m.displayItems))
	}
	if !m.displayItems[0].isHeader || !m.displayItems[3].isHeader {
		t.Fatalf("expected headers at indices 0 and 3, got %+v", m.displayItems)
	}
	return m
}

func keyPress(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Text: string(code)}
}

func TestInputNavSkipsHeaders(t *testing.T) {
	m := newInputModel(t)

	// After SetGitStatus the cursor lands on the first non-header row.
	if m.cursor != 1 {
		t.Fatalf("expected initial cursor on first change (index 1), got %d", m.cursor)
	}

	// Walk all the way down with 'j' and assert the cursor never rests on a
	// header row, and that it crosses the Staged/Unstaged header boundary.
	seen := map[int]bool{}
	for i := 0; i < 10; i++ {
		m, _ = m.Update(keyPress('j'))
		if m.displayItems[m.cursor].isHeader {
			t.Fatalf("cursor landed on a header at index %d after %d down moves", m.cursor, i+1)
		}
		seen[m.cursor] = true
	}
	// Should have reached and stopped on the last change row (index 5),
	// and visited the first row after the second header (index 4).
	if m.cursor != 5 {
		t.Fatalf("expected cursor clamped to last change (index 5), got %d", m.cursor)
	}
	if !seen[4] {
		t.Fatalf("expected cursor to cross the header boundary onto index 4, visited %v", seen)
	}

	// Walk back up with 'k' and assert the same header-skip invariant, and that
	// it clamps to the first change row (index 1), never to the header at 0.
	for i := 0; i < 10; i++ {
		m, _ = m.Update(keyPress('k'))
		if m.displayItems[m.cursor].isHeader {
			t.Fatalf("cursor landed on a header at index %d after %d up moves", m.cursor, i+1)
		}
	}
	if m.cursor != 1 {
		t.Fatalf("expected cursor clamped to first change (index 1), got %d", m.cursor)
	}
}

func TestInputNavArrowKeys(t *testing.T) {
	m := newInputModel(t)

	// down arrow behaves like 'j'.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 2 {
		t.Fatalf("expected down arrow to move cursor to index 2, got %d", m.cursor)
	}
	// up arrow behaves like 'k'.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != 1 {
		t.Fatalf("expected up arrow to move cursor to index 1, got %d", m.cursor)
	}
}

func TestInputOpenEmitsOpenDiffForSelectedChange(t *testing.T) {
	m := newInputModel(t)

	// Move onto the first Unstaged change (index 4 == c.go).
	m, _ = m.Update(keyPress('j')) // 2 (b.go)
	m, _ = m.Update(keyPress('j')) // 4 (c.go, skipping header at 3)
	if m.cursor != 4 {
		t.Fatalf("expected cursor on index 4 (c.go), got %d", m.cursor)
	}

	// 'enter' opens the current item; assert the emitted message is OpenDiff for
	// c.go with the Unstaged diff mode.
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from open key")
	}
	msg := cmd()
	open, ok := msg.(messages.OpenDiff)
	if !ok {
		t.Fatalf("expected messages.OpenDiff, got %T", msg)
	}
	if open.Change == nil || open.Change.Path != "c.go" {
		t.Fatalf("expected OpenDiff for c.go, got %+v", open.Change)
	}
	if open.Mode != git.DiffModeUnstaged {
		t.Fatalf("expected DiffModeUnstaged for unstaged change, got %v", open.Mode)
	}
}

func TestInputOpenViaOAndSpaceKeys(t *testing.T) {
	// 'o' and 'space' are also bound to open; assert they emit OpenDiff for the
	// initially-selected staged change (index 1 == a.go).
	for _, key := range []tea.KeyPressMsg{
		keyPress('o'),
		{Code: tea.KeySpace},
	} {
		m := newInputModel(t)
		_, cmd := m.Update(key)
		if cmd == nil {
			t.Fatalf("expected non-nil cmd for open key %+v", key)
		}
		open, ok := cmd().(messages.OpenDiff)
		if !ok {
			t.Fatalf("expected messages.OpenDiff for key %+v, got %T", key, cmd())
		}
		if open.Change == nil || open.Change.Path != "a.go" {
			t.Fatalf("expected OpenDiff for a.go, got %+v", open.Change)
		}
		if open.Mode != git.DiffModeStaged {
			t.Fatalf("expected DiffModeStaged for staged change, got %v", open.Mode)
		}
	}
}

func TestInputClickResolvesRowToChange(t *testing.T) {
	m := newInputModel(t)

	// listHeaderLines() == 1 here (no workspace branch, not filtering, +1 for the
	// "changed files" line) and helpLineCount() == 0 (hints disabled by default),
	// so screen row Y maps to displayItems[Y-1]. Click Y=5 -> index 4 (c.go).
	_, cmd := m.Update(tea.MouseClickMsg{X: 2, Y: 5, Button: tea.MouseLeft})
	if m.cursor != 4 {
		t.Fatalf("expected click on Y=5 to move cursor to index 4, got %d", m.cursor)
	}
	if cmd == nil {
		t.Fatal("expected click on a change row to emit an open cmd")
	}
	open, ok := cmd().(messages.OpenDiff)
	if !ok {
		t.Fatalf("expected messages.OpenDiff from click, got %T", cmd())
	}
	if open.Change == nil || open.Change.Path != "c.go" {
		t.Fatalf("expected click to open c.go, got %+v", open.Change)
	}
}

func TestInputClickOnHeaderIsNoOpOpen(t *testing.T) {
	m := newInputModel(t)
	start := m.cursor

	// Y=4 maps to displayItems[3], which is the "Unstaged" header. rowIndexAt
	// still resolves the row, but openCurrentItem must refuse to open a header.
	_, cmd := m.Update(tea.MouseClickMsg{X: 2, Y: 4, Button: tea.MouseLeft})
	if m.cursor != 3 {
		t.Fatalf("expected click on header row to set cursor to header index 3, got %d", m.cursor)
	}
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("expected no OpenDiff when clicking a header, got %T", msg)
		}
	}
	_ = start
}

func TestInputClickOutsideRowsIsNoOp(t *testing.T) {
	m := newInputModel(t)
	before := m.cursor

	// Y=0 is above the list content (within the header area), so rowIndexAt
	// returns !ok and the click is ignored.
	_, cmd := m.Update(tea.MouseClickMsg{X: 2, Y: 0, Button: tea.MouseLeft})
	if cmd != nil {
		t.Fatalf("expected nil cmd for click above the change list, got non-nil")
	}
	if m.cursor != before {
		t.Fatalf("expected cursor unchanged on out-of-range click, got %d (was %d)", m.cursor, before)
	}
}

func TestInputIgnoredWhenUnfocused(t *testing.T) {
	m := New()
	m.SetSize(80, 20)
	m.SetGitStatus(twoSectionStatus())
	// not focused
	before := m.cursor
	m, cmd := m.Update(keyPress('j'))
	if m.cursor != before {
		t.Fatalf("expected unfocused key press to be ignored, cursor moved to %d", m.cursor)
	}
	if cmd != nil {
		t.Fatal("expected nil cmd when unfocused")
	}
}
