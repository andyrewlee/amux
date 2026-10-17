package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func newEventLogApp(t *testing.T) *App {
	t.Helper()
	return &App{
		toast:  common.NewToastModel(),
		width:  120,
		height: 40,
	}
}

// TestHandleShowEventLog_RendersNewestFirst proves every severity of toast
// lands in the recall viewer in newest-first order.
func TestHandleShowEventLog_RendersNewestFirst(t *testing.T) {
	app := newEventLogApp(t)
	app.toast.ShowInfo("first info")
	app.toast.ShowError("second err")
	app.toast.ShowSuccess("third ok")

	app.handleShowEventLog()

	d := app.overlays.runOutput
	if d == nil || !d.Visible() {
		t.Fatal("event log viewer did not open")
	}
	view := ansi.Strip(d.View())
	first := strings.Index(view, "first info")
	second := strings.Index(view, "second err")
	third := strings.Index(view, "third ok")
	if first < 0 || second < 0 || third < 0 {
		t.Fatalf("viewer missing events:\n%s", view)
	}
	if !(third < second && second < first) {
		t.Fatalf("events not newest-first (third=%d second=%d first=%d):\n%s", third, second, first, view)
	}
	for _, want := range []string{"Notifications", "error", "info", "ok"} {
		if !strings.Contains(view, want) {
			t.Fatalf("viewer missing %q:\n%s", want, view)
		}
	}
	// Static flavor: no refresh/session/attach semantics may leak in.
	if app.overlays.runOutputSession != "" || app.overlays.runOutputAttachable ||
		app.overlays.runOutputLifecycle || app.overlays.runOutputReleaseCount > 0 {
		t.Fatal("event log viewer picked up non-static flavor flags")
	}
}

// TestHandleShowEventLog_EmptyShowsInfo proves an empty ring reports via
// toast rather than opening a blank viewer.
func TestHandleShowEventLog_EmptyShowsInfo(t *testing.T) {
	app := newEventLogApp(t)
	app.handleShowEventLog()
	if app.overlays.runOutput != nil {
		t.Fatal("viewer opened with no events")
	}
	if view := ansi.Strip(app.toast.View()); !strings.Contains(view, "No notifications") {
		t.Fatalf("expected info toast, got %q", view)
	}
}

// TestShowEventLog_DeferredOpenArbiter proves the recall surface queues
// behind a visible modal instead of stacking.
func TestShowEventLog_DeferredOpenArbiter(t *testing.T) {
	app := newEventLogApp(t)
	app.toast.ShowInfo("queued event")
	app.overlays.env = common.NewEnvDialog(nil)
	app.overlays.env.Show()

	app.handleShowEventLog()
	if app.overlays.runOutput != nil {
		t.Fatal("event log opened over a visible overlay")
	}
	if len(app.pendingOverlayOpens) != 1 {
		t.Fatalf("pendingOverlayOpens = %d, want 1", len(app.pendingOverlayOpens))
	}
	app.overlays.env.Hide() // modal gone
	app.drainPendingOverlayOpens()
	if app.overlays.runOutput == nil || !app.overlays.runOutput.Visible() {
		t.Fatal("deferred event log did not open after drain")
	}
}

// TestShowEventLog_ScrollAndEscClose drives the dialog's own contract:
// scrolling moves the viewport and Esc closes through the arbiter slot.
func TestShowEventLog_ScrollAndEscClose(t *testing.T) {
	app := newEventLogApp(t)
	for i := 0; i < 40; i++ {
		app.toast.ShowInfo("event " + string(rune('a'+i%26)) + string(rune('0'+i/26)))
	}
	app.handleShowEventLog()
	d := app.overlays.runOutput
	if d == nil || !d.Visible() {
		t.Fatal("viewer did not open")
	}
	// Scroll: a keypress reaches the dialog through the app-level handler.
	var cmds []tea.Cmd
	if !app.handleRunOutputInput(tea.KeyPressMsg{Code: tea.KeyPgDown}, &cmds) {
		t.Fatal("pgdown not consumed by the event log slot")
	}
	// Esc: the dialog emits OutputDialogResult which closes the slot.
	cmds = nil
	if !app.handleRunOutputInput(tea.KeyPressMsg{Code: tea.KeyEscape}, &cmds) {
		t.Fatal("esc not consumed by the event log slot")
	}
	if len(cmds) != 1 {
		t.Fatalf("esc produced %d cmds, want 1 (dismiss result)", len(cmds))
	}
	result, ok := cmds[0]().(common.OutputDialogResult)
	if !ok {
		t.Fatalf("esc cmd emitted %T, want common.OutputDialogResult", cmds[0]())
	}
	app.closeRunOutputDialog() // the dispatch path for OutputDialogResult
	_ = result
	if app.overlays.runOutput != nil {
		t.Fatal("event log did not close on esc result")
	}
}

// TestEventLogPrefixCommandDispatches proves C-Space e resolves to the
// event log action and produces the ShowEventLog message.
func TestEventLogPrefixCommandDispatches(t *testing.T) {
	app := newEventLogApp(t)
	matches := app.matchingPrefixCommands([]string{"e"})
	if len(matches) != 1 || matches[0].Action != prefixActionEventLog {
		t.Fatalf("prefix 'e' matched %v, want exactly event_log", matches)
	}
	cmd := app.runPrefixAction(prefixActionEventLog)
	if cmd == nil {
		t.Fatal("event log action returned nil cmd")
	}
	if _, ok := cmd().(messages.ShowEventLog); !ok {
		t.Fatalf("event log action emitted %T, want messages.ShowEventLog", cmd())
	}
}
