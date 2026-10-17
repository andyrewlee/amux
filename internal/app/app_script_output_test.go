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
	}, "", false)
	setupIdx := strings.Index(out, "setup")
	archIdx := strings.Index(out, "archive")
	hookIdx := strings.Index(out, "on-done")
	if setupIdx == -1 || archIdx == -1 || hookIdx == -1 || !(setupIdx < archIdx && archIdx < hookIdx) {
		t.Fatalf("sections out of lifecycle order:\n%s", out)
	}
	if !strings.Contains(out, "failed — exit status 7") || !strings.Contains(out, "ok") {
		t.Fatalf("outcome labels wrong:\n%s", out)
	}
	if empty := composeScriptOutputView(nil, "", false); empty != "" {
		t.Fatalf("empty map rendered %q, want \"\"", empty)
	}
}

// TestComposeScriptOutputView_LiveSetup covers the in-flight variant: the
// setup section renders "running" with the live tail instead of the last
// recorded transcript, an empty tail still shows the section (a running
// setup with no bytes yet is the case that used to look like a hang), and
// the other sections keep their recorded form.
func TestComposeScriptOutputView_LiveSetup(t *testing.T) {
	at := time.Date(2026, 9, 22, 14, 3, 0, 0, time.Local)
	outputs := map[process.ScriptType]process.ScriptOutput{
		process.ScriptSetup:   {Text: "old-setup-text", FinishedAt: at},
		process.ScriptArchive: {Text: "arch-text", FinishedAt: at},
	}
	out := composeScriptOutputView(outputs, "npm install … 40%", true)
	if !strings.Contains(out, "── setup ── running") || !strings.Contains(out, "npm install") {
		t.Fatalf("live setup section missing:\n%s", out)
	}
	if strings.Contains(out, "old-setup-text") {
		t.Fatalf("live section must replace the recorded transcript:\n%s", out)
	}
	if !strings.Contains(out, "── archive ── ok") {
		t.Fatalf("recorded sections must survive the live one:\n%s", out)
	}

	out = composeScriptOutputView(nil, "", true)
	if !strings.Contains(out, "── setup ── running") || !strings.Contains(out, "(no output yet)") {
		t.Fatalf("running setup with empty tail must still render its section:\n%s", out)
	}
}

// TestHandleShowScriptOutput_LiveSetupFlow drives the whole live path: a
// real in-flight RunSetup opens the viewer showing the running section,
// the refresh tick re-composes with the live tail, and once the setup
// finishes the final fetch lands the recorded transcript and the loop
// disarms.
func TestHandleShowScriptOutput_LiveSetupFlow(t *testing.T) {
	app, scripts := newScriptOutputApp(t)
	ws := &data.Workspace{
		Name: "ws", Repo: t.TempDir(), Root: t.TempDir(),
		Scripts: data.ScriptsConfig{Setup: "echo live-line; sleep 0.4"},
	}

	done := make(chan error, 1)
	go func() { done <- scripts.RunSetup(ws) }()

	// Wait for the live publish to carry output before opening — the flag
	// publishes at admission, ahead of the first bytes, and the open + tick
	// reads are only meaningful once content is flowing.
	deadline := time.Now().Add(5 * time.Second)
	for {
		text, live := scripts.LiveSetupOutput(ws)
		if live && strings.Contains(text, "live-line") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("setup never published a live tail")
		}
		time.Sleep(5 * time.Millisecond)
	}

	app.handleShowScriptOutput(messages.ShowScriptOutput{Workspace: ws})
	if app.overlays.runOutput == nil || !app.overlays.runOutput.Visible() {
		t.Fatal("script output dialog did not open during live setup")
	}
	if !app.overlays.runOutputLifecycle {
		t.Fatal("open did not mark the dialog as the lifecycle flavor")
	}
	if len(app.drainedOverlayCmds) == 0 {
		t.Fatal("live open did not arm the refresh tick")
	}
	view := ansi.Strip(app.overlays.runOutput.View())
	if !strings.Contains(view, "setup ── running") {
		t.Fatalf("viewer missing the running section:\n%s", view)
	}

	// One tick → off-loop fetch → the refreshed content carries the live
	// tail and reports alive, so the handler re-arms.
	fetch := app.handleRunOutputTick(runOutputTickMsg{token: app.overlays.runOutputToken})
	if fetch == nil {
		t.Fatal("live tick produced no fetch")
	}
	msg, ok := fetch().(runOutputRefreshedMsg)
	if !ok || !msg.alive || !strings.Contains(msg.content, "live-line") {
		t.Fatalf("refreshed msg = %+v, want alive with live-line content", fetch())
	}
	if next := app.handleRunOutputRefreshed(msg); next == nil {
		t.Fatal("live refresh did not re-arm the tick")
	}

	// Setup finishes: the recorded transcript exists and the live flag
	// drops, so the next fetch composes the finished section and stops.
	if err := <-done; err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}
	fetch = app.handleRunOutputTick(runOutputTickMsg{token: app.overlays.runOutputToken})
	if fetch == nil {
		t.Fatal("post-finish tick produced no fetch")
	}
	msg, ok = fetch().(runOutputRefreshedMsg)
	if !ok {
		t.Fatalf("fetch emitted %T, want runOutputRefreshedMsg", fetch())
	}
	if msg.alive {
		t.Fatal("finished setup still reports live")
	}
	if !strings.Contains(msg.content, "── setup ── ok") || !strings.Contains(msg.content, "live-line") {
		t.Fatalf("final content = %q, want the recorded setup section", msg.content)
	}
	if next := app.handleRunOutputRefreshed(msg); next != nil {
		t.Fatal("dead refresh must not re-arm the tick")
	}
}

type errSentinel string

func (e errSentinel) Error() string { return string(e) }
