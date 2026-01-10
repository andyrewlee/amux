package center

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func TestOverlayCenter(t *testing.T) {
	m := &Model{width: 20, height: 10}

	bg := "background line 1   \nbackground line 2   \nbackground line 3   "
	dialog := "┌──────┐\n│Dialog│\n└──────┘"

	out := m.overlayCenter(bg, dialog)

	if !strings.Contains(out, "Dialog") {
		t.Errorf("Overlay should contain dialog content. Got:\n%q", out)
	}
	// Basic check that it didn't just return the dialog (length should roughly match bg lines count if preserved)
	if strings.Count(out, "\n") < 2 {
		t.Error("Overlay output seems too short")
	}
}

func TestSanitizeFilenamePart(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal", "normal"},
		{"Hello World", "Hello-World"},
		{"foo/bar", "foo-bar"},
		{"foo\\bar", "foo-bar"},
		{"..", ""},
		{"   trim   ", "trim"},
		{"a---b", "a-b"},
		{"_foo_", "foo"}, // logic trims leading/trailing dashes/underscores if sanitizeFilenamePart does?
		// Let's check implementation of sanitizeFilenamePart again.
		// It trims spaces first.
		// Replaces non-alphanum with -.
		// Collapses multiple dashes.
		// Trims leading/trailing dashes.
	}

	for _, tc := range tests {
		got := sanitizeFilenamePart(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizeFilenamePart(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}
}

func TestThreadFilename(t *testing.T) {
	now := time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		worktree *data.Worktree
		tab      *Tab
		expected string
	}{
		{
			name:     "basic",
			worktree: &data.Worktree{Name: "my-project"},
			tab:      &Tab{Assistant: "claude"},
			expected: "20260106-120000-my-project-claude.txt",
		},
		{
			name:     "tab name",
			worktree: &data.Worktree{Name: "work"},
			tab:      &Tab{Name: "fix-bug", Assistant: "gpt"},
			expected: "20260106-120000-work-fix-bug.txt",
		},
		{
			name:     "weird chars",
			worktree: &data.Worktree{Name: "c++/rust"},
			tab:      &Tab{Name: "test / case", Assistant: "gpt"},
			expected: "20260106-120000-c-rust-test-case.txt",
		},
	}

	m := &Model{}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m.worktree = tc.worktree
			got := m.threadFilename(now, tc.tab)
			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestTrimTrailingEmptyLines(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "no trailing empty",
			input:    []string{"line1", "line2"},
			expected: []string{"line1", "line2"},
		},
		{
			name:     "trailing empty lines",
			input:    []string{"line1", "line2", "", "  ", ""},
			expected: []string{"line1", "line2"},
		},
		{
			name:     "all empty",
			input:    []string{"", "", ""},
			expected: []string{},
		},
		{
			name:     "empty input",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "whitespace only lines",
			input:    []string{"content", "  \t  "},
			expected: []string{"content"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := trimTrailingEmptyLines(tc.input)
			if len(got) != len(tc.expected) {
				t.Errorf("expected %d lines, got %d", len(tc.expected), len(got))
				return
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Errorf("line %d: expected %q, got %q", i, tc.expected[i], got[i])
				}
			}
		})
	}
}

func TestUniqueThreadPath(t *testing.T) {
	t.Run("returns path when file does not exist", func(t *testing.T) {
		dir := t.TempDir()
		path, err := uniqueThreadPath(dir, "thread.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(dir, "thread.txt")
		if path != expected {
			t.Errorf("expected %q, got %q", expected, path)
		}
	})

	t.Run("increments suffix when file exists", func(t *testing.T) {
		dir := t.TempDir()

		// Create existing file
		existing := filepath.Join(dir, "thread.txt")
		if err := os.WriteFile(existing, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		path, err := uniqueThreadPath(dir, "thread.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(dir, "thread-2.txt")
		if path != expected {
			t.Errorf("expected %q, got %q", expected, path)
		}
	})

	t.Run("finds next available number", func(t *testing.T) {
		dir := t.TempDir()

		// Create thread.txt, thread-2.txt, thread-3.txt
		for _, name := range []string{"thread.txt", "thread-2.txt", "thread-3.txt"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0644); err != nil {
				t.Fatalf("failed to create test file: %v", err)
			}
		}

		path, err := uniqueThreadPath(dir, "thread.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(dir, "thread-4.txt")
		if path != expected {
			t.Errorf("expected %q, got %q", expected, path)
		}
	})

	t.Run("preserves extension", func(t *testing.T) {
		dir := t.TempDir()

		existing := filepath.Join(dir, "20260106-120000-project.txt")
		if err := os.WriteFile(existing, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		path, err := uniqueThreadPath(dir, "20260106-120000-project.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasSuffix(path, "-2.txt") {
			t.Errorf("expected suffix -2.txt, got %q", path)
		}
	})
}

func TestPTYMessagesProcessedWhileDialogVisible(t *testing.T) {
	m := &Model{
		tabsByWorktree:      make(map[string][]*Tab),
		activeTabByWorktree: make(map[string]int),
		width:               80,
		height:              24,
	}

	m.saveDialog = common.NewSelectDialog(
		"save-thread",
		"Save Thread",
		"Save current thread to file?",
		[]string{"Save & Copy Path", "Cancel"},
	)
	m.saveDialog.Show()
	m.dialogOpenTime = time.Now().Add(-time.Second)

	wtID := "test-worktree"
	tabID := TabID("test-tab")
	m.tabsByWorktree[wtID] = []*Tab{{ID: tabID}}

	outputMsg := PTYOutput{
		WorktreeID: wtID,
		TabID:      tabID,
		Data:       []byte("test output"),
	}

	_, cmd := m.Update(outputMsg)

	if cmd == nil {
		t.Error("PTYOutput should return a command to continue reading, but got nil")
	}

	tab := m.tabsByWorktree[wtID][0]
	if len(tab.pendingOutput) == 0 {
		t.Error("PTYOutput data should be buffered in pendingOutput")
	}
	if string(tab.pendingOutput) != "test output" {
		t.Errorf("expected pending output %q, got %q", "test output", string(tab.pendingOutput))
	}
}
