package sidebar

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func TestSidebarModelSetShowKeymapHints(t *testing.T) {
	tests := []struct {
		name    string
		initial bool
		set     bool
		want    bool
	}{
		{name: "enable from default", initial: false, set: true, want: true},
		{name: "disable explicitly", initial: false, set: false, want: false},
		{name: "toggle on then off keeps last write", initial: true, set: false, want: false},
		{name: "idempotent enable", initial: true, set: true, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			// Establish a known starting point distinct from the value under test.
			m.SetShowKeymapHints(tt.initial)
			if m.showKeymapHints != tt.initial {
				t.Fatalf("precondition failed: showKeymapHints = %v, want %v", m.showKeymapHints, tt.initial)
			}

			m.SetShowKeymapHints(tt.set)
			if m.showKeymapHints != tt.want {
				t.Fatalf("SetShowKeymapHints(%v) = %v, want %v", tt.set, m.showKeymapHints, tt.want)
			}
		})
	}
}

func TestSidebarModelSetShowKeymapHintsDefaultIsFalse(t *testing.T) {
	m := New()
	if m.showKeymapHints {
		t.Fatal("expected showKeymapHints to default to false from New()")
	}
}

func TestSidebarModelSetStyles(t *testing.T) {
	m := New()

	// New() seeds DefaultStyles; sanity-check the baseline before mutating.
	if got := m.styles.Title.Value(); got != "" {
		t.Fatalf("unexpected default Title value %q", got)
	}

	custom := common.DefaultStyles()
	custom.Title = lipgloss.NewStyle().SetString("custom-title")
	m.SetStyles(custom)
	if got := m.styles.Title.Value(); got != "custom-title" {
		t.Fatalf("SetStyles did not apply: Title value = %q, want %q", got, "custom-title")
	}

	// A subsequent call replaces the styles wholesale rather than merging.
	replacement := common.DefaultStyles()
	replacement.Title = lipgloss.NewStyle().SetString("replacement")
	m.SetStyles(replacement)
	if got := m.styles.Title.Value(); got != "replacement" {
		t.Fatalf("SetStyles did not overwrite: Title value = %q, want %q", got, "replacement")
	}
}

func TestSidebarModelSetStylesZeroValue(t *testing.T) {
	m := New()
	// A zero-value Styles is a valid (if blank) assignment; it must not panic
	// and must replace the prior styles wholesale.
	m.SetStyles(common.Styles{})
	if got := m.styles.Title.Value(); got != "" {
		t.Fatalf("expected blank Title after zero-value SetStyles, got %q", got)
	}
	if got := m.styles.SidebarHeader.Value(); got != "" {
		t.Fatalf("expected blank SidebarHeader after zero-value SetStyles, got %q", got)
	}
}

func TestSidebarModelInitReturnsNoCmd(t *testing.T) {
	m := New()
	if cmd := m.Init(); cmd != nil {
		t.Fatalf("Init() = %v, want nil", cmd)
	}
	// Init must be side-effect free with respect to focus/filter state.
	if m.Focused() {
		t.Fatal("Init() should not focus the sidebar")
	}
	if m.filterMode {
		t.Fatal("Init() should not enter filter mode")
	}
}

func TestSidebarModelFocused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(m *Model)
		want   bool
	}{
		{name: "default unfocused", mutate: func(m *Model) {}, want: false},
		{name: "after Focus", mutate: func(m *Model) { m.Focus() }, want: true},
		{name: "after Focus then Blur", mutate: func(m *Model) { m.Focus(); m.Blur() }, want: false},
		{name: "Blur on already-unfocused stays false", mutate: func(m *Model) { m.Blur() }, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			tt.mutate(m)
			if got := m.Focused(); got != tt.want {
				t.Fatalf("Focused() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSidebarModelBlurClearsFocus(t *testing.T) {
	m := New()
	m.Focus()
	if !m.Focused() {
		t.Fatal("precondition failed: expected focused after Focus()")
	}

	m.Blur()
	if m.Focused() {
		t.Fatal("Blur() did not clear focus")
	}
}

func TestSidebarModelBlurExitsFilterMode(t *testing.T) {
	m := New()
	m.Focus()
	// Simulate the model being in active filter mode with a focused input.
	m.filterMode = true
	m.filterInput.Focus()
	if !m.filterInput.Focused() {
		t.Fatal("precondition failed: expected filter input to be focused")
	}

	m.Blur()

	if m.filterMode {
		t.Fatal("Blur() should exit filter mode")
	}
	if m.filterInput.Focused() {
		t.Fatal("Blur() should blur the filter input")
	}
	if m.Focused() {
		t.Fatal("Blur() should clear sidebar focus")
	}
}

func TestSidebarModelBlurWhenNotFilteringLeavesFilterInputUntouched(t *testing.T) {
	m := New()
	m.Focus()
	// filterMode is false, but the input could carry a stale query value;
	// Blur must not touch the input when not in filter mode.
	m.filterInput.SetValue("stale")

	m.Blur()

	if m.filterMode {
		t.Fatal("filter mode should remain false")
	}
	if got := m.filterInput.Value(); got != "stale" {
		t.Fatalf("Blur() should not clear filter input value when not filtering, got %q", got)
	}
	if m.Focused() {
		t.Fatal("Blur() should clear sidebar focus")
	}
}

func TestSetGitStatusFastResultDoesNotPreserveOldLineStats(t *testing.T) {
	m := New()
	m.SetGitStatus(&git.StatusResult{
		Clean:        false,
		Unstaged:     []git.Change{{Path: "README.md"}},
		TotalAdded:   12,
		TotalDeleted: 3,
		HasLineStats: true,
	})
	m.SetGitStatus(&git.StatusResult{
		Clean:        false,
		Unstaged:     []git.Change{{Path: "README.md"}},
		HasLineStats: false,
	})

	if m.gitStatus == nil {
		t.Fatal("expected git status to be set")
	}
	if m.gitStatus.TotalAdded != 0 || m.gitStatus.TotalDeleted != 0 {
		t.Fatalf("expected fast status to keep zero totals, got +%d -%d", m.gitStatus.TotalAdded, m.gitStatus.TotalDeleted)
	}
	if m.gitStatus.HasLineStats {
		t.Fatal("expected HasLineStats=false for fast status")
	}
}

func TestRenderBodyHidesLineTotalsWhenStatsUnknown(t *testing.T) {
	m := New()
	m.SetSize(80, 20)
	m.SetGitStatus(&git.StatusResult{
		Clean:        false,
		Unstaged:     []git.Change{{Path: "README.md"}},
		TotalAdded:   12,
		TotalDeleted: 3,
		HasLineStats: false,
	})

	body := m.renderChanges()
	if strings.Contains(body, "+12") || strings.Contains(body, "-3") {
		t.Fatalf("expected line totals to be hidden when stats are unknown, body=%q", body)
	}
}
