package sidebar

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// newSeededProjectTree builds a ProjectTree backed by a temp directory so the
// disk-reading reloadTree/expandNode paths produce a deterministic, non-empty
// flat node list. The layout is:
//
//	root/
//	  alpha/      (dir, collapsed)
//	    nested.txt
//	  beta/       (dir, collapsed)
//	  one.txt
//	  two.txt
//
// Directories sort before files, so flatNodes is [alpha, beta, one.txt, two.txt].
func newSeededProjectTree(t *testing.T) *ProjectTree {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "repo", "feature")
	if err := os.MkdirAll(filepath.Join(root, "alpha"), 0o755); err != nil {
		t.Fatalf("MkdirAll(alpha): %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "beta"), 0o755); err != nil {
		t.Fatalf("MkdirAll(beta): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "alpha", "nested.txt"), []byte("n"), 0o644); err != nil {
		t.Fatalf("WriteFile(nested.txt): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "one.txt"), []byte("1"), 0o644); err != nil {
		t.Fatalf("WriteFile(one.txt): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "two.txt"), []byte("2"), 0o644); err != nil {
		t.Fatalf("WriteFile(two.txt): %v", err)
	}

	tree := NewProjectTree()
	tree.SetWorkspace(data.NewWorkspace("feature", "feature", "main", filepath.Join(base, "repo"), root))
	return tree
}

func TestProjectTreeSetShowKeymapHints(t *testing.T) {
	tests := []struct {
		name string
		show bool
	}{
		{name: "enable hints", show: true},
		{name: "disable hints", show: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewProjectTree()
			m.SetShowKeymapHints(tt.show)
			if m.showKeymapHints != tt.show {
				t.Fatalf("showKeymapHints = %v, want %v", m.showKeymapHints, tt.show)
			}
		})
	}
}

func TestProjectTreeSetShowKeymapHintsAffectsHelpLineCount(t *testing.T) {
	m := NewProjectTree()
	m.SetSize(40, 20)

	m.SetShowKeymapHints(false)
	if got := m.helpLineCount(); got != 0 {
		t.Fatalf("helpLineCount with hints off = %d, want 0", got)
	}

	m.SetShowKeymapHints(true)
	if got := m.helpLineCount(); got < 1 {
		t.Fatalf("helpLineCount with hints on = %d, want >= 1", got)
	}
}

func TestProjectTreeSetStyles(t *testing.T) {
	m := NewProjectTree()

	styles := common.DefaultStyles()
	styles.Muted = styles.Muted.SetString("MUTED-MARKER")
	m.SetStyles(styles)

	if got := m.styles.Muted.Value(); got != "MUTED-MARKER" {
		t.Fatalf("SetStyles did not propagate, got %q", got)
	}
}

func TestProjectTreeInitReturnsNoCmd(t *testing.T) {
	m := NewProjectTree()
	if cmd := m.Init(); cmd != nil {
		t.Fatalf("Init() = %v, want nil", cmd)
	}
}

func TestProjectTreeFocusBlurFocused(t *testing.T) {
	m := NewProjectTree()

	if m.Focused() {
		t.Fatal("expected tree unfocused on construction")
	}

	m.Focus()
	if !m.Focused() {
		t.Fatal("expected Focused() true after Focus()")
	}

	m.Blur()
	if m.Focused() {
		t.Fatal("expected Focused() false after Blur()")
	}
}

func TestProjectTreeFocusIsIdempotent(t *testing.T) {
	m := NewProjectTree()
	m.Focus()
	m.Focus()
	if !m.Focused() {
		t.Fatal("expected repeated Focus() to keep tree focused")
	}
	m.Blur()
	m.Blur()
	if m.Focused() {
		t.Fatal("expected repeated Blur() to keep tree unfocused")
	}
}

func TestProjectTreeVisibleHeight(t *testing.T) {
	tests := []struct {
		name      string
		height    int
		showHints bool
		want      int
	}{
		// Hints add several help lines; visibleHeight subtracts them but never
		// drops below 1.
		{name: "no hints uses full height", height: 20, showHints: false, want: 20},
		{name: "zero height clamps to 1", height: 0, showHints: false, want: 1},
		{name: "negative height clamps to 1", height: -5, showHints: false, want: 1},
		{name: "tiny height with hints clamps to 1", height: 1, showHints: true, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewProjectTree()
			m.SetSize(40, tt.height)
			m.SetShowKeymapHints(tt.showHints)
			if got := m.visibleHeight(); got != tt.want {
				t.Fatalf("visibleHeight() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestProjectTreeVisibleHeightSubtractsHelpLines(t *testing.T) {
	m := NewProjectTree()
	m.SetSize(40, 20)
	m.SetShowKeymapHints(true)

	want := 20 - m.helpLineCount()
	if got := m.visibleHeight(); got != want {
		t.Fatalf("visibleHeight() = %d, want height(20) - helpLines(%d) = %d", got, m.helpLineCount(), want)
	}
}

func TestProjectTreeRowIndexAt(t *testing.T) {
	m := newSeededProjectTree(t)
	if len(m.flatNodes) != 4 {
		t.Fatalf("expected 4 flat nodes (alpha, beta, one.txt, two.txt), got %d", len(m.flatNodes))
	}
	m.SetSize(40, 20)
	m.SetShowKeymapHints(false) // contentHeight == height with hints off

	tests := []struct {
		name      string
		screenY   int
		scroll    int
		wantIndex int
		wantOK    bool
	}{
		{name: "first visible row", screenY: 0, scroll: 0, wantIndex: 0, wantOK: true},
		{name: "interior row", screenY: 2, scroll: 0, wantIndex: 2, wantOK: true},
		{name: "negative y rejected", screenY: -1, scroll: 0, wantIndex: -1, wantOK: false},
		{name: "row beyond node count rejected", screenY: 10, scroll: 0, wantIndex: -1, wantOK: false},
		{name: "scroll offset applied", screenY: 1, scroll: 2, wantIndex: 3, wantOK: true},
		{name: "scroll pushes past end rejected", screenY: 2, scroll: 2, wantIndex: -1, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.scrollOffset = tt.scroll
			idx, ok := m.rowIndexAt(tt.screenY)
			if ok != tt.wantOK {
				t.Fatalf("rowIndexAt(%d) ok = %v, want %v", tt.screenY, ok, tt.wantOK)
			}
			if idx != tt.wantIndex {
				t.Fatalf("rowIndexAt(%d) idx = %d, want %d", tt.screenY, idx, tt.wantIndex)
			}
		})
	}
}

func TestProjectTreeRowIndexAtEmptyTree(t *testing.T) {
	m := NewProjectTree()
	m.SetSize(40, 20)
	// No workspace -> flatNodes is empty; any screenY must be rejected.
	if idx, ok := m.rowIndexAt(0); ok || idx != -1 {
		t.Fatalf("rowIndexAt on empty tree = (%d, %v), want (-1, false)", idx, ok)
	}
}

func TestProjectTreeRowIndexAtRespectsHelpLines(t *testing.T) {
	m := newSeededProjectTree(t)
	m.SetSize(40, 4) // height equals node count
	m.SetShowKeymapHints(true)

	// With hints on, contentHeight = height - helpLineCount. A screenY at or
	// beyond the reduced content height must be rejected even though a matching
	// node exists.
	contentHeight := m.height - m.helpLineCount()
	if contentHeight < 1 {
		t.Skipf("seeded geometry left no content rows (contentHeight=%d)", contentHeight)
	}
	if _, ok := m.rowIndexAt(contentHeight); ok {
		t.Fatalf("rowIndexAt(%d) accepted a row inside the help region", contentHeight)
	}
	if idx, ok := m.rowIndexAt(0); !ok || idx != 0 {
		t.Fatalf("rowIndexAt(0) = (%d, %v), want (0, true)", idx, ok)
	}
}

func TestProjectTreeMoveCursor(t *testing.T) {
	tests := []struct {
		name  string
		start int
		delta int
		want  int
	}{
		{name: "down one", start: 0, delta: 1, want: 1},
		{name: "up one", start: 2, delta: -1, want: 1},
		{name: "clamp at top", start: 0, delta: -5, want: 0},
		{name: "clamp at bottom", start: 0, delta: 99, want: 3},
		{name: "no-op zero delta", start: 2, delta: 0, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newSeededProjectTree(t)
			if len(m.flatNodes) != 4 {
				t.Fatalf("expected 4 flat nodes, got %d", len(m.flatNodes))
			}
			m.cursor = tt.start
			m.moveCursor(tt.delta)
			if m.cursor != tt.want {
				t.Fatalf("moveCursor(%d) from %d = %d, want %d", tt.delta, tt.start, m.cursor, tt.want)
			}
		})
	}
}

func TestProjectTreeMoveCursorEmptyTreeIsNoop(t *testing.T) {
	m := NewProjectTree()
	// No workspace: flatNodes empty, cursor must stay untouched and not panic.
	m.cursor = 0
	m.moveCursor(5)
	if m.cursor != 0 {
		t.Fatalf("expected cursor unchanged on empty tree, got %d", m.cursor)
	}
}

func TestProjectTreeUpdateIgnoresInputWhenBlurred(t *testing.T) {
	m := newSeededProjectTree(t)
	m.cursor = 1
	// Blurred model returns itself and a nil cmd, leaving cursor untouched.
	m2, cmd := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if cmd != nil {
		t.Fatalf("expected nil cmd while blurred, got %v", cmd)
	}
	if m2 != m {
		t.Fatal("expected Update to return the same model pointer")
	}
	if m.cursor != 1 {
		t.Fatalf("expected cursor unchanged while blurred, got %d", m.cursor)
	}
}

func TestProjectTreeUpdateNavigationKeys(t *testing.T) {
	tests := []struct {
		name       string
		key        tea.KeyPressMsg
		startIdx   int
		wantCursor int
	}{
		{name: "j moves down", key: tea.KeyPressMsg{Code: 'j', Text: "j"}, startIdx: 0, wantCursor: 1},
		{name: "down arrow moves down", key: tea.KeyPressMsg{Code: tea.KeyDown}, startIdx: 0, wantCursor: 1},
		{name: "k moves up", key: tea.KeyPressMsg{Code: 'k', Text: "k"}, startIdx: 2, wantCursor: 1},
		{name: "up arrow moves up", key: tea.KeyPressMsg{Code: tea.KeyUp}, startIdx: 2, wantCursor: 1},
		{name: "k clamps at top", key: tea.KeyPressMsg{Code: 'k', Text: "k"}, startIdx: 0, wantCursor: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newSeededProjectTree(t)
			m.Focus()
			m.cursor = tt.startIdx
			_, cmd := m.Update(tt.key)
			if cmd != nil {
				t.Fatalf("expected nil cmd for navigation key, got %v", cmd)
			}
			if m.cursor != tt.wantCursor {
				t.Fatalf("cursor after %s = %d, want %d", tt.name, m.cursor, tt.wantCursor)
			}
		})
	}
}

func TestProjectTreeUpdateExpandCollapseDirectory(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	// Cursor 0 is "alpha", a collapsed directory containing nested.txt.
	m.cursor = 0
	if m.flatNodes[0].Name != "alpha" || !m.flatNodes[0].IsDir {
		t.Fatalf("expected node 0 to be dir alpha, got %+v", m.flatNodes[0])
	}
	before := len(m.flatNodes)

	// 'l' expands the directory, inserting its child into the flat list.
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"}); cmd != nil {
		t.Fatalf("expand produced unexpected cmd: %v", cmd)
	}
	if !m.flatNodes[0].Expanded {
		t.Fatal("expected alpha to be expanded after 'l'")
	}
	if len(m.flatNodes) != before+1 {
		t.Fatalf("expected flat list to grow by 1 child, got %d (was %d)", len(m.flatNodes), before)
	}

	// 'h' collapses it again, removing the child.
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"}); cmd != nil {
		t.Fatalf("collapse produced unexpected cmd: %v", cmd)
	}
	if m.flatNodes[0].Expanded {
		t.Fatal("expected alpha to be collapsed after 'h'")
	}
	if len(m.flatNodes) != before {
		t.Fatalf("expected flat list to shrink back to %d, got %d", before, len(m.flatNodes))
	}
}

func TestProjectTreeUpdateCollapseMovesToParent(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	// Expand alpha so its child nested.txt becomes a flat node at index 1.
	m.cursor = 0
	m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m.cursor = 1
	if m.flatNodes[1].IsDir {
		t.Fatalf("expected node 1 to be a file child, got dir %+v", m.flatNodes[1])
	}

	// 'h' on a non-dir whose Parent is set should jump the cursor to the parent.
	m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if m.cursor != 0 {
		t.Fatalf("expected cursor to move to parent index 0, got %d", m.cursor)
	}
}

func TestProjectTreeUpdateToggleHiddenReloads(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	if !m.showHidden {
		t.Fatal("expected showHidden true by default")
	}
	// '.' flips the hidden flag and reloads the tree from disk.
	m.Update(tea.KeyPressMsg{Code: '.', Text: "."})
	if m.showHidden {
		t.Fatal("expected showHidden toggled to false after '.'")
	}
	m.Update(tea.KeyPressMsg{Code: '.', Text: "."})
	if !m.showHidden {
		t.Fatal("expected showHidden toggled back to true after second '.'")
	}
}

func TestProjectTreeUpdateRefreshKeepsNodes(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	before := len(m.flatNodes)
	// 'r' reloads from disk; the on-disk layout is unchanged so the node count
	// must be preserved.
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"}); cmd != nil {
		t.Fatalf("refresh produced unexpected cmd: %v", cmd)
	}
	if len(m.flatNodes) != before {
		t.Fatalf("expected node count preserved after refresh, got %d (was %d)", len(m.flatNodes), before)
	}
}

func TestProjectTreeUpdateEnterOnDirectoryToggles(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	m.cursor = 0 // alpha (directory)
	// Enter on a directory expands it and returns a nil cmd (no file open).
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected nil cmd for directory enter, got %v", cmd)
	}
	if !m.flatNodes[0].Expanded {
		t.Fatal("expected directory to expand on Enter")
	}
}

func TestProjectTreeUpdateEnterOnFileEmitsOpenCmd(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	// Cursor 2 is "one.txt" (files sort after the two directories).
	m.cursor = 2
	if m.flatNodes[2].IsDir || m.flatNodes[2].Name != "one.txt" {
		t.Fatalf("expected node 2 to be file one.txt, got %+v", m.flatNodes[2])
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	if cmd == nil {
		t.Fatal("expected an open-file command for a file node")
	}
	msg := cmd()
	open, ok := msg.(OpenFileInEditor)
	if !ok {
		t.Fatalf("expected OpenFileInEditor, got %T", msg)
	}
	if filepath.Base(open.Path) != "one.txt" {
		t.Fatalf("expected OpenFileInEditor for one.txt, got %q", open.Path)
	}
}

func TestProjectTreeUpdateMouseWheelMovesCursor(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	m.SetSize(40, 20)
	m.cursor = 2

	_, cmd := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if cmd != nil {
		t.Fatalf("expected nil cmd for wheel up, got %v", cmd)
	}
	if m.cursor >= 2 {
		t.Fatalf("expected wheel up to decrease cursor, got %d", m.cursor)
	}

	up := m.cursor
	_, cmd = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if cmd != nil {
		t.Fatalf("expected nil cmd for wheel down, got %v", cmd)
	}
	if m.cursor <= up {
		t.Fatalf("expected wheel down to increase cursor from %d, got %d", up, m.cursor)
	}
}

func TestProjectTreeUpdateMouseClickSelectsRow(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	m.SetSize(40, 20)
	m.SetShowKeymapHints(false)
	m.cursor = 0

	// A left click on screen row 2 maps to flat node index 2 (one.txt), which
	// emits an open command.
	_, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, Y: 2})
	if m.cursor != 2 {
		t.Fatalf("expected click to move cursor to row 2, got %d", m.cursor)
	}
	if cmd == nil {
		t.Fatal("expected open command from clicking a file row")
	}
	msg := cmd()
	if _, ok := msg.(OpenFileInEditor); !ok {
		t.Fatalf("expected OpenFileInEditor from click, got %T", msg)
	}
}

func TestProjectTreeUpdateMouseClickOutsideRowsIsNoop(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	m.SetSize(40, 20)
	m.SetShowKeymapHints(false)
	m.cursor = 1

	// Clicking far below the last node resolves to no row; cursor must stay put.
	_, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, Y: 50})
	if cmd != nil {
		t.Fatalf("expected nil cmd for click outside rows, got %v", cmd)
	}
	if m.cursor != 1 {
		t.Fatalf("expected cursor unchanged on missed click, got %d", m.cursor)
	}
}
