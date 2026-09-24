package app

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func newScriptOutputApp(t *testing.T) (*App, *process.ScriptRunner) {
	t.Helper()
	scripts := process.NewScriptRunner(6200, 10)
	return &App{
		toast:            common.NewToastModel(),
		workspaceService: workspacesvc.New(nil, nil, scripts, ""),
		width:            120,
		height:           40,
	}, scripts
}

// TestHandleShowScriptOutput_OpensCompositeViewer proves the recorded
// lifecycle transcripts reach the output dialog as headed sections.
func TestHandleShowScriptOutput_OpensCompositeViewer(t *testing.T) {
	app, scripts := newScriptOutputApp(t)
	ws := &data.Workspace{
		Name: "ws", Repo: t.TempDir(), Root: t.TempDir(),
		Scripts: data.ScriptsConfig{Archive: "echo archive-marker"},
	}
	if err := scripts.RunArchive(ws); err != nil {
		t.Fatalf("RunArchive() error = %v", err)
	}

	app.handleShowScriptOutput(messages.ShowScriptOutput{Workspace: ws})

	if app.overlays.runOutput == nil || !app.overlays.runOutput.Visible() {
		t.Fatal("script output dialog did not open")
	}
	view := ansi.Strip(app.overlays.runOutput.View())
	for _, want := range []string{"Script output", "archive", "archive-marker"} {
		if !strings.Contains(view, want) {
			t.Fatalf("viewer missing %q:\n%s", want, view)
		}
	}
}

func TestHandleShowScriptOutput_NothingRecordedShowsInfo(t *testing.T) {
	app, _ := newScriptOutputApp(t)
	ws := &data.Workspace{Name: "ws", Repo: t.TempDir(), Root: t.TempDir()}

	app.handleShowScriptOutput(messages.ShowScriptOutput{Workspace: ws})

	if app.overlays.runOutput != nil {
		t.Fatal("dialog opened with nothing recorded")
	}
	if view := ansi.Strip(app.toast.View()); !strings.Contains(view, "No lifecycle script output") {
		t.Fatalf("expected info toast, got %q", view)
	}
}

// TestHandleLifecycleScriptExited_ActiveWorkspaceHint proves the failure
// toast names the hook and points at the O viewer — and that the hint
// assumes O when the failed workspace is the one being shown.
func TestHandleLifecycleScriptExited_ActiveWorkspaceHint(t *testing.T) {
	app, _ := newScriptOutputApp(t)
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	app.activeWorkspace = ws

	app.handleLifecycleScriptExited(lifecycleScriptExitedMsg{
		workspace:  ws,
		scriptType: process.ScriptOnDone,
		err:        errSentinel("exit status 7"),
	})

	view := ansi.Strip(app.toast.View())
	for _, want := range []string{"on-done", "ws", "exit status 7", "press O"} {
		if !strings.Contains(view, want) {
			t.Fatalf("toast missing %q: %q", want, view)
		}
	}
}

// TestHandleLifecycleScriptExited_BackgroundWorkspaceHint proves the toast
// tells the truth when the failed workspace is not the one being shown —
// pressing O there would show the wrong transcripts.
func TestHandleLifecycleScriptExited_BackgroundWorkspaceHint(t *testing.T) {
	app, _ := newScriptOutputApp(t)
	ws := &data.Workspace{Name: "bg-ws", Repo: "/repo", Root: "/repo/bg"}
	app.activeWorkspace = &data.Workspace{Name: "other", Root: "/repo/other"}

	app.handleLifecycleScriptExited(lifecycleScriptExitedMsg{
		workspace:  ws,
		scriptType: process.ScriptOnDone,
		err:        errSentinel("exit status 1"),
	})

	view := ansi.Strip(app.toast.View())
	if !strings.Contains(view, "bg-ws") || !strings.Contains(view, "select it") {
		t.Fatalf("background-workspace toast = %q, want name + select hint", view)
	}
}

// TestComposeScriptOutputView covers the composite rendering: lifecycle
// order, outcome labels, and skipping types with nothing recorded.
func TestComposeScriptOutputView(t *testing.T) {
	at := time.Date(2026, 9, 22, 14, 3, 0, 0, time.Local)
	out := composeScriptOutputView(map[process.ScriptType]process.ScriptOutput{
		process.ScriptOnDone:  {Text: "hook-text", Err: "exit status 7", FinishedAt: at},
		process.ScriptSetup:   {Text: "setup-text", FinishedAt: at},
		process.ScriptArchive: {Text: "arch-text", FinishedAt: at},
	})
	setupIdx := strings.Index(out, "setup")
	archIdx := strings.Index(out, "archive")
	hookIdx := strings.Index(out, "on-done")
	if setupIdx == -1 || archIdx == -1 || hookIdx == -1 || !(setupIdx < archIdx && archIdx < hookIdx) {
		t.Fatalf("sections out of lifecycle order:\n%s", out)
	}
	if !strings.Contains(out, "failed — exit status 7") || !strings.Contains(out, "ok") {
		t.Fatalf("outcome labels wrong:\n%s", out)
	}
	if empty := composeScriptOutputView(nil); empty != "" {
		t.Fatalf("empty map rendered %q, want \"\"", empty)
	}
}

type errSentinel string

func (e errSentinel) Error() string { return string(e) }
