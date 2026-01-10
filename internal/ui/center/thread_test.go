package center

import (
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
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
