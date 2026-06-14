package diff

import (
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func TestNormalizeSourcePath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "plain", in: "main.go", want: "main.go"},
		{name: "dot-slash prefix", in: "./main.go", want: "main.go"},
		{name: "redundant slashes", in: "a//b/c.go", want: "a/b/c.go"},
		{name: "parent traversal", in: "a/b/../c.go", want: "a/c.go"},
		{name: "trailing slash", in: "dir/sub/", want: "dir/sub"},
		{name: "current dir", in: ".", want: "."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeSourcePath(tt.in); got != tt.want {
				t.Fatalf("normalizeSourcePath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMatchesSource(t *testing.T) {
	tests := []struct {
		name       string
		model      *Model
		changePath string
		mode       git.DiffMode
		want       bool
	}{
		{
			name:       "nil model",
			model:      nil,
			changePath: "main.go",
			mode:       git.DiffModeUnstaged,
			want:       false,
		},
		{
			name:       "nil change",
			model:      &Model{change: nil, mode: git.DiffModeUnstaged},
			changePath: "main.go",
			mode:       git.DiffModeUnstaged,
			want:       false,
		},
		{
			name:       "exact match",
			model:      &Model{change: &git.Change{Path: "main.go"}, mode: git.DiffModeUnstaged},
			changePath: "main.go",
			mode:       git.DiffModeUnstaged,
			want:       true,
		},
		{
			name:       "match after normalization",
			model:      &Model{change: &git.Change{Path: "./a//b.go"}, mode: git.DiffModeStaged},
			changePath: "a/b.go",
			mode:       git.DiffModeStaged,
			want:       true,
		},
		{
			name:       "same path different mode",
			model:      &Model{change: &git.Change{Path: "main.go"}, mode: git.DiffModeUnstaged},
			changePath: "main.go",
			mode:       git.DiffModeStaged,
			want:       false,
		},
		{
			name:       "different path same mode",
			model:      &Model{change: &git.Change{Path: "main.go"}, mode: git.DiffModeBranch},
			changePath: "other.go",
			mode:       git.DiffModeBranch,
			want:       false,
		},
		{
			name:       "both empty paths match",
			model:      &Model{change: &git.Change{Path: ""}, mode: git.DiffModeUnstaged},
			changePath: "",
			mode:       git.DiffModeUnstaged,
			want:       true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.model.MatchesSource(tt.changePath, tt.mode); got != tt.want {
				t.Fatalf("MatchesSource(%q, %v) = %v, want %v", tt.changePath, tt.mode, got, tt.want)
			}
		})
	}
}

// TestFocusHelpers covers the full focus lifecycle through every accessor and
// mutator: SetFocused both ways, Focus, Blur, and the Focused reader.
func TestFocusHelpers(t *testing.T) {
	m := &Model{}

	if m.Focused() {
		t.Fatal("expected zero-value model to be unfocused")
	}

	m.Focus()
	if !m.Focused() {
		t.Fatal("Focus did not set focused state")
	}

	m.Blur()
	if m.Focused() {
		t.Fatal("Blur did not clear focused state")
	}

	m.SetFocused(true)
	if !m.Focused() {
		t.Fatal("SetFocused(true) did not focus model")
	}

	m.SetFocused(false)
	if m.Focused() {
		t.Fatal("SetFocused(false) did not blur model")
	}
}

func TestSetSize(t *testing.T) {
	tests := []struct {
		name  string
		w, h  int
		wantW int
		wantH int
	}{
		{name: "typical", w: 100, h: 30, wantW: 100, wantH: 30},
		{name: "zero", w: 0, h: 0, wantW: 0, wantH: 0},
		{name: "negative", w: -5, h: -10, wantW: -5, wantH: -10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{width: 1, height: 1}
			m.SetSize(tt.w, tt.h)
			if m.width != tt.wantW || m.height != tt.wantH {
				t.Fatalf("SetSize(%d,%d) => width=%d height=%d, want %d/%d",
					tt.w, tt.h, m.width, m.height, tt.wantW, tt.wantH)
			}
		})
	}
}

// TestSetSizeAffectsVisibleHeight is a behavioral check: resizing should feed
// through to the derived visibleHeight used by scroll math.
func TestSetSizeAffectsVisibleHeight(t *testing.T) {
	m := &Model{}
	m.SetSize(80, 13)
	if got := m.visibleHeight(); got != 10 {
		t.Fatalf("visibleHeight after SetSize = %d, want 10", got)
	}
}

func TestSetStyles(t *testing.T) {
	m := &Model{styles: common.DefaultStyles()}

	custom := common.DefaultStyles()
	custom.Body = lipgloss.NewStyle().Foreground(lipgloss.Color("#ABCDEF")).Bold(true)
	m.SetStyles(custom)

	if m.styles.Body.GetForeground() != lipgloss.Color("#ABCDEF") {
		t.Fatalf("SetStyles did not apply custom foreground, got %v", m.styles.Body.GetForeground())
	}
	if !m.styles.Body.GetBold() {
		t.Fatal("SetStyles did not apply custom bold attribute")
	}
}

func TestGetPath(t *testing.T) {
	tests := []struct {
		name   string
		change *git.Change
		want   string
	}{
		{name: "nil change", change: nil, want: ""},
		{name: "empty path", change: &git.Change{Path: ""}, want: ""},
		{name: "populated path", change: &git.Change{Path: "pkg/file.go"}, want: "pkg/file.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{change: tt.change}
			if got := m.GetPath(); got != tt.want {
				t.Fatalf("GetPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScrollToBottom(t *testing.T) {
	t.Run("nil diff stays at zero", func(t *testing.T) {
		m := &Model{height: 10}
		m.scroll = 4
		m.scrollToBottom()
		if m.scroll != 0 {
			t.Fatalf("scrollToBottom with nil diff = %d, want 0", m.scroll)
		}
	})

	t.Run("long diff jumps to maxScroll", func(t *testing.T) {
		m := newModelWithDiff(6, 10, nil)
		m.scrollToBottom()
		if m.scroll != m.maxScroll() {
			t.Fatalf("scrollToBottom = %d, want maxScroll %d", m.scroll, m.maxScroll())
		}
		if m.scroll != 7 {
			t.Fatalf("scrollToBottom = %d, want 7", m.scroll)
		}
	})

	t.Run("short diff stays at zero", func(t *testing.T) {
		m := newModelWithDiff(20, 3, nil)
		m.scroll = 2
		m.scrollToBottom()
		if m.scroll != 0 {
			t.Fatalf("scrollToBottom on short diff = %d, want 0", m.scroll)
		}
	})
}
