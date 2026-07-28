package sidebar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
)

// newRunScriptModel builds a focused Changes view bound to ws with a clean
// status, which is enough for the branch header (and so the indicator) to
// render.
func newRunScriptModel(ws *data.Workspace) *Model {
	m := New()
	m.SetSize(80, 20)
	m.Focus()
	m.SetWorkspace(ws)
	m.SetGitStatus(&git.StatusResult{Clean: true})
	return m
}

// TestRunScriptKeyEmitsToggle asserts 'r' asks the app to toggle the run script
// for the focused workspace. The sidebar deliberately does not decide between
// start and stop — it has no ScriptRunner — so the message it sends is a toggle.
func TestRunScriptKeyEmitsToggle(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Root: "/tmp/ws", Branch: "feature"}
	m := newRunScriptModel(ws)

	_, cmd := m.Update(keyPress('r'))
	if cmd == nil {
		t.Fatal("pressing 'r' produced no command")
	}
	toggle, ok := cmd().(messages.ToggleWorkspaceScript)
	if !ok {
		t.Fatalf("pressing 'r' emitted %T, want messages.ToggleWorkspaceScript", cmd())
	}
	if toggle.Workspace != ws {
		t.Fatal("toggle names the wrong workspace")
	}
}

// TestRunScriptKeyWithoutWorkspaceIsInert asserts the key does nothing before a
// workspace is selected, rather than emitting a toggle with a nil target.
func TestRunScriptKeyWithoutWorkspaceIsInert(t *testing.T) {
	m := newRunScriptModel(nil)

	if _, cmd := m.Update(keyPress('r')); cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("pressing 'r' with no workspace emitted %T", msg)
		}
	}
}

// TestRunScriptKeyIgnoredWhileFiltering asserts 'r' typed into the filter box is
// text, not a command — otherwise filtering for a file whose name contains 'r'
// would start a dev server.
func TestRunScriptKeyIgnoredWhileFiltering(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Root: "/tmp/ws", Branch: "feature"}
	m := newRunScriptModel(ws)

	m, _ = m.Update(keyPress('/'))
	if !m.filterMode {
		t.Fatal("'/' did not enter filter mode")
	}

	_, cmd := m.Update(keyPress('r'))
	if cmd != nil {
		if _, isToggle := cmd().(messages.ToggleWorkspaceScript); isToggle {
			t.Fatal("'r' typed into the filter box started the run script")
		}
	}
	if !strings.Contains(m.filterQuery, "r") {
		t.Fatalf("filter query = %q, want it to contain the typed 'r'", m.filterQuery)
	}
}

// TestRunIndicatorRendersOnlyWhileRunning asserts the marker is absent by
// default and present once the app reports a live script, so its presence
// carries information.
func TestRunIndicatorRendersOnlyWhileRunning(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Root: "/tmp/ws", Branch: "feature"}
	m := newRunScriptModel(ws)

	if strings.Contains(ansi.Strip(m.View()), "[run]") {
		t.Fatal("run indicator rendered before any script started")
	}

	m.SetScriptRunning(ws.Root, true)
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "[run]") {
		t.Fatalf("run indicator missing from view:\n%s", view)
	}
	// It belongs on the branch line, next to the thing it describes.
	branchLine := ""
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "branch:") {
			branchLine = line
			break
		}
	}
	if !strings.Contains(branchLine, "[run]") {
		t.Fatalf("run indicator is not on the branch line, got %q", branchLine)
	}

	m.SetScriptRunning(ws.Root, false)
	if strings.Contains(ansi.Strip(m.View()), "[run]") {
		t.Fatal("run indicator survived the script stopping")
	}
}

// TestRunIndicatorIsScopedToItsWorkspace asserts a running script reported for
// one workspace does not decorate another. The state arrives asynchronously, so
// it can land after the user has already switched away.
func TestRunIndicatorIsScopedToItsWorkspace(t *testing.T) {
	other := &data.Workspace{Name: "other", Root: "/tmp/other", Branch: "other"}
	m := newRunScriptModel(other)

	// A late state change for a workspace that is no longer displayed.
	m.SetScriptRunning("/tmp/runner", true)

	if strings.Contains(ansi.Strip(m.View()), "[run]") {
		t.Fatal("a run indicator for another workspace leaked into this one")
	}
}
