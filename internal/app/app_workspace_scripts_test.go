package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// newScriptStateApp builds an App wired to a real sidebar sized large enough to
// render its header, so tests can assert on what the user would actually see.
// A clean git status is installed because the Changes view short-circuits to
// "No status loaded" — and so renders no branch header — until one arrives.
func newScriptStateApp(ws *data.Workspace) (*App, *sidebar.TabbedSidebar) {
	sb := sidebar.NewTabbedSidebar()
	sb.SetSize(60, 20)
	sb.SetWorkspace(ws)
	sb.SetGitStatus(&git.StatusResult{Clean: true})
	return &App{toast: common.NewToastModel(), sidebar: sb, activeWorkspace: ws}, sb
}

// runIndicatorVisible reports whether the sidebar is currently rendering the
// live-run-script marker, with styling stripped so the assertion is about the
// text and not the theme.
func runIndicatorVisible(sb *sidebar.TabbedSidebar) bool {
	return strings.Contains(ansi.Strip(sb.View()), "[run]")
}

// TestHandleWorkspaceScriptStateChanged_ShowsAndClearsRunIndicator asserts the
// sidebar marker follows the reported state in both directions, so the user can
// tell a live run script from a stopped one.
func TestHandleWorkspaceScriptStateChanged_ShowsAndClearsRunIndicator(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Root: t.TempDir(), Branch: "feature"}
	app, sb := newScriptStateApp(ws)

	if runIndicatorVisible(sb) {
		t.Fatal("run indicator visible before any script was started")
	}

	app.handleWorkspaceScriptStateChanged(messages.WorkspaceScriptStateChanged{
		Workspace: ws,
		Running:   true,
	})
	if !runIndicatorVisible(sb) {
		t.Fatal("run indicator did not appear after the script started")
	}

	app.handleWorkspaceScriptStateChanged(messages.WorkspaceScriptStateChanged{
		Workspace: ws,
		Running:   false,
	})
	if runIndicatorVisible(sb) {
		t.Fatal("run indicator still visible after the script stopped")
	}
}

// TestHandleWorkspaceScriptStateChanged_FailedStartStaysStopped asserts the
// indicator follows the reported end state, not the attempted one: a start that
// errored must not leave the sidebar claiming a script is running.
func TestHandleWorkspaceScriptStateChanged_FailedStartStaysStopped(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Root: t.TempDir(), Branch: "feature"}
	app, sb := newScriptStateApp(ws)

	app.handleWorkspaceScriptStateChanged(messages.WorkspaceScriptStateChanged{
		Workspace: ws,
		Running:   false,
		Err:       errors.New("exec format error"),
	})

	if runIndicatorVisible(sb) {
		t.Fatal("a failed start left the sidebar showing the script as running")
	}
}

// TestHandleWorkspaceScriptStateChanged_IndicatorIsPerWorkspace asserts a state
// change for one workspace does not label a different one: the user switching
// workspaces mid-start must not see a borrowed [run] marker.
func TestHandleWorkspaceScriptStateChanged_IndicatorIsPerWorkspace(t *testing.T) {
	running := &data.Workspace{Name: "runner", Root: t.TempDir(), Branch: "runner"}
	other := &data.Workspace{Name: "other", Root: t.TempDir(), Branch: "other"}
	app, sb := newScriptStateApp(running)

	app.handleWorkspaceScriptStateChanged(messages.WorkspaceScriptStateChanged{
		Workspace: running,
		Running:   true,
	})
	if !runIndicatorVisible(sb) {
		t.Fatal("run indicator missing for the workspace whose script started")
	}

	sb.SetWorkspace(other)
	if runIndicatorVisible(sb) {
		t.Fatal("run indicator leaked onto a different workspace")
	}
}

// TestHandleWorkspaceScriptStateChanged_IgnoresInactiveWorkspace asserts an
// outcome that arrives after the user has switched away does not blank the
// indicator for the workspace now on screen, whose script is still running.
func TestHandleWorkspaceScriptStateChanged_IgnoresInactiveWorkspace(t *testing.T) {
	onScreen := &data.Workspace{Name: "on-screen", Root: t.TempDir(), Branch: "on-screen"}
	elsewhere := &data.Workspace{Name: "elsewhere", Root: t.TempDir(), Branch: "elsewhere"}
	app, sb := newScriptStateApp(onScreen)

	app.handleWorkspaceScriptStateChanged(messages.WorkspaceScriptStateChanged{
		Workspace: onScreen,
		Running:   true,
	})
	if !runIndicatorVisible(sb) {
		t.Fatal("setup: the active workspace's indicator did not appear")
	}

	// A late outcome for a workspace the user is no longer looking at.
	app.handleWorkspaceScriptStateChanged(messages.WorkspaceScriptStateChanged{
		Workspace: elsewhere,
		Running:   false,
	})
	if !runIndicatorVisible(sb) {
		t.Fatal("an outcome for another workspace cleared the active workspace's indicator")
	}
}

// TestHandleWorkspaceScriptStateChanged_DeadRecordAtSameRootDropped asserts a
// toggle outcome stamped by a DELETED record does not label the record that
// replaced it at the same root. Path-derived IDs are identical across
// recreate, so Created is the record-instance discriminator.
func TestHandleWorkspaceScriptStateChanged_DeadRecordAtSameRootDropped(t *testing.T) {
	root := t.TempDir()
	live := &data.Workspace{Name: "feature", Root: root, Branch: "feature", Created: time.Now()}
	dead := &data.Workspace{Name: "feature", Root: root, Branch: "feature", Created: live.Created.Add(-time.Minute)}
	app, sb := newScriptStateApp(live)

	app.handleWorkspaceScriptStateChanged(messages.WorkspaceScriptStateChanged{
		Workspace: dead,
		Running:   true,
	})
	if runIndicatorVisible(sb) {
		t.Fatal("a dead record's outcome labeled the recreated workspace")
	}

	// The live record's own outcome still applies.
	app.handleWorkspaceScriptStateChanged(messages.WorkspaceScriptStateChanged{
		Workspace: live,
		Running:   true,
	})
	if !runIndicatorVisible(sb) {
		t.Fatal("the live record's own outcome was dropped")
	}
}

// TestHandleRunAttachTarget_StaleViewerGenerationDropped asserts an attach
// result stamped by a viewer generation that has since moved on can neither
// close the viewer now on screen nor dispatch the attach. The token is the
// runOutputToken at dispatch — close, reopen, and new enumerations all bump
// it.
func TestHandleRunAttachTarget_StaleViewerGenerationDropped(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws", Scripts: data.ScriptsConfig{Run: "make dev"}}
	h := newRunOutputHarness(t, ws, "tick-1", true)
	openRunOutput(t, h, ws)
	staleToken := h.app.overlays.runOutputToken

	// The dispatch stamps the CURRENT viewer generation.
	cmd := h.app.attachRunViewerCmd(ws, "")
	if cmd == nil {
		t.Fatal("attach cmd missing for a live viewer")
	}
	stamped, ok := cmd().(runAttachTargetMsg)
	if !ok {
		t.Fatal("attach cmd did not emit runAttachTargetMsg")
	}
	if stamped.token != staleToken {
		t.Fatalf("attach msg stamped token %d, want viewer generation %d", stamped.token, staleToken)
	}

	// The viewer closed and a newer enumeration opened its own generation
	// while the lookup was in flight.
	h.app.closeRunOutputDialog()
	openRunOutput(t, h, ws)
	live := h.app.overlays.runOutput
	if live == nil || h.app.overlays.runOutputToken == staleToken {
		t.Fatal("setup: expected a newer viewer generation")
	}

	if cmd := h.app.handleRunAttachTarget(runAttachTargetMsg{
		token: staleToken, ws: ws, name: "amux-sess-run", ok: true,
	}); cmd != nil {
		t.Fatal("stale attach produced a cmd")
	}
	if h.app.overlays.runOutput != live {
		t.Fatal("stale attach result closed the live viewer")
	}

	// A current-generation result is still handled.
	if cmd := h.app.handleRunAttachTarget(runAttachTargetMsg{
		token: h.app.overlays.runOutputToken, ws: ws, ok: false,
	}); cmd == nil {
		t.Fatal("current-generation attach result was dropped")
	}
}

// TestHandleWorkspaceScriptStateChanged_UntrustedOpensTrustDialog asserts an
// untrusted repo is treated as a consent prompt rather than an error: nothing
// ran, so the user gets the same trust dialog the setup path offers.
func TestHandleWorkspaceScriptStateChanged_UntrustedOpensTrustDialog(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Root: t.TempDir(), Branch: "feature"}
	app, _ := newScriptStateApp(ws)

	cmd := app.handleWorkspaceScriptStateChanged(messages.WorkspaceScriptStateChanged{
		Workspace: ws,
		Err: &process.ScriptsNotTrustedError{
			Repo:       "/tmp/repo",
			Command:    "npm start",
			ConfigHash: "abc123",
		},
	})

	var dialog *messages.ShowTrustScriptsDialog
	for _, msg := range runCommandMessages(cmd) {
		if d, ok := msg.(messages.ShowTrustScriptsDialog); ok {
			dialog = &d
		}
	}
	if dialog == nil {
		t.Fatal("an untrusted run script did not offer the trust dialog")
	}
	if dialog.ConfigHash != "abc123" {
		t.Fatalf("trust dialog hash = %q, want the hash that was blocked (abc123)", dialog.ConfigHash)
	}
	if dialog.Workspace != ws {
		t.Fatal("trust dialog was opened for the wrong workspace")
	}
	if app.err != nil {
		t.Fatalf("an untrusted repo was reported as an error: %v", app.err)
	}
}

// TestHandleWorkspaceScriptStateChanged_NoScriptIsNotAnError asserts pressing
// the key in a repo that defines no run script informs the user instead of
// raising an error banner.
func TestHandleWorkspaceScriptStateChanged_NoScriptIsNotAnError(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Root: t.TempDir(), Branch: "feature"}
	app, _ := newScriptStateApp(ws)

	app.handleWorkspaceScriptStateChanged(messages.WorkspaceScriptStateChanged{
		Workspace: ws,
		Err:       fmt.Errorf("run: %w", process.ErrNoScriptConfigured),
	})

	if app.err != nil {
		t.Fatalf("a missing run script was reported as an error: %v", app.err)
	}
}

// TestHandleWorkspaceScriptStateChanged_RealFailureIsReported is the negative
// control for the two cases above: a genuine failure must still surface.
func TestHandleWorkspaceScriptStateChanged_RealFailureIsReported(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Root: t.TempDir(), Branch: "feature"}
	app, _ := newScriptStateApp(ws)

	cmd := app.handleWorkspaceScriptStateChanged(messages.WorkspaceScriptStateChanged{
		Workspace: ws,
		Err:       errors.New("fork/exec: resource temporarily unavailable"),
	})

	var reported bool
	for _, msg := range runCommandMessages(cmd) {
		if _, ok := msg.(messages.Error); ok {
			reported = true
		}
	}
	if !reported {
		t.Fatal("a genuine run-script failure was swallowed instead of reported")
	}
}

// TestHandleToggleWorkspaceScript_NilWorkspaceIsInert asserts the handler is
// safe when no workspace is selected, which is the state on an empty dashboard.
func TestHandleToggleWorkspaceScript_NilWorkspaceIsInert(t *testing.T) {
	app, _ := newScriptStateApp(nil)
	if cmd := app.handleToggleWorkspaceScript(messages.ToggleWorkspaceScript{}); cmd != nil {
		t.Fatal("expected no command when no workspace is selected")
	}
}

// TestHandleWorkspaceSetupComplete_BusyAndTeardownOutcomes pins the two new
// admission outcomes as informational rather than errors: a duplicate setup
// request reports the in-flight run, and a teardown rejection stays silent
// because the removal itself reports its outcome.
func TestHandleWorkspaceSetupComplete_BusyAndTeardownOutcomes(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")

	t.Run("busy setup reports in-flight run", func(t *testing.T) {
		app := &App{toast: common.NewToastModel()}
		cmd := app.handleWorkspaceSetupComplete(messages.WorkspaceSetupComplete{
			Workspace: ws, Rerun: true, Err: process.ErrSetupBusy,
		})
		if cmd == nil {
			t.Fatal("busy setup produced no toast")
		}
		if !strings.Contains(app.toast.View(), "Setup already running") {
			t.Fatalf("expected an 'already running' toast, got %q", app.toast.View())
		}
	})

	t.Run("teardown rejection is silent", func(t *testing.T) {
		app := &App{toast: common.NewToastModel()}
		if cmd := app.handleWorkspaceSetupComplete(messages.WorkspaceSetupComplete{
			Workspace: ws, Err: process.ErrWorkspaceTeardown,
		}); cmd != nil {
			t.Fatal("teardown-rejected setup must not toast or error")
		}
	})
}
