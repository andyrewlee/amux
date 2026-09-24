package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/vterm"
)

// TestHandlePrefixCopyTranscript_NoTerminal warns instead of copying when the
// focused pane has no terminal transcript.
func TestHandlePrefixCopyTranscript_NoTerminal(t *testing.T) {
	app, _, _ := newPrefixTestApp(t)
	app.toast = common.NewToastModel()
	copied := make(chan string, 1)
	app.copyToClipboardFn = func(text, _ string) { copied <- text }

	status, _ := app.handlePrefixCommand(tea.KeyPressMsg{Code: 't', Text: "t"})
	if status != prefixMatchPartial {
		t.Fatalf("expected partial after 't', got %v", status)
	}
	status, cmd := app.handlePrefixCommand(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if status != prefixMatchComplete {
		t.Fatalf("expected 't y' to complete, got %v", status)
	}
	if cmd == nil {
		t.Fatal("expected the warning toast cmd")
	}
	select {
	case <-copied:
		t.Fatal("clipboard write happened with no transcript")
	case <-time.After(150 * time.Millisecond):
	}
	if view := ansi.Strip(app.toast.View()); !strings.Contains(view, "No transcript") {
		t.Fatalf("toast = %q, want the no-transcript warning", view)
	}
}

// TestHandlePrefixCopyTranscript_CopiesActiveTab proves the 't y' sequence
// routes to the focused pane's transcript and the confirm toast reports the
// copied size.
func TestHandlePrefixCopyTranscript_CopiesActiveTab(t *testing.T) {
	app, ws, centerModel := newPrefixTestApp(t)
	app.toast = common.NewToastModel()
	copied := make(chan string, 1)
	app.copyToClipboardFn = func(text, _ string) { copied <- text }

	tab := &center.Tab{
		ID:        center.TabID("tab-1"),
		Assistant: "claude",
		Workspace: ws,
		Terminal:  vterm.New(40, 5),
		Running:   true,
	}
	centerModel.AddTab(tab)
	tab.Terminal.Write([]byte("transcript-marker line\r\n"))

	app.handlePrefixCommand(tea.KeyPressMsg{Code: 't', Text: "t"})
	_, cmd := app.handlePrefixCommand(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Fatal("expected the success toast cmd")
	}
	select {
	case text := <-copied:
		if !strings.Contains(text, "transcript-marker line") {
			t.Fatalf("clipboard text = %q, missing transcript content", text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("clipboard write never ran")
	}
	if view := ansi.Strip(app.toast.View()); !strings.Contains(view, "Copied transcript") {
		t.Fatalf("toast = %q, want the copied confirmation", view)
	}
}

// TestHandlePrefixSaveTranscript_OpensPrefilledDialog proves the 't f'
// sequence opens the save-transcript input dialog prefilled with a
// ~/.amux/transcripts/<ws>-<ts>.txt default, and that the transcript was
// snapshotted at open time (dlg.transcript) — not re-read on confirm.
func TestHandlePrefixSaveTranscript_OpensPrefilledDialog(t *testing.T) {
	app, ws, centerModel := newPrefixTestApp(t)
	app.config = &config.Config{}
	app.activeWorkspace = ws
	app.toast = common.NewToastModel()

	tab := &center.Tab{
		ID:        center.TabID("tab-1"),
		Assistant: "claude",
		Workspace: ws,
		Terminal:  vterm.New(40, 5),
		Running:   true,
	}
	centerModel.AddTab(tab)
	tab.Terminal.Write([]byte("save-me-marker\r\n"))

	status, _ := app.handlePrefixCommand(tea.KeyPressMsg{Code: 't', Text: "t"})
	if status != prefixMatchPartial {
		t.Fatalf("expected partial after 't', got %v", status)
	}
	status, _ = app.handlePrefixCommand(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if status != prefixMatchComplete {
		t.Fatalf("expected 't f' to complete, got %v", status)
	}
	if app.dialog == nil {
		t.Fatal("expected the save-transcript input dialog to open")
	}
	if !strings.Contains(app.dlg.transcript, "save-me-marker") {
		t.Fatalf("dlg.transcript = %q, want the open-time snapshot", app.dlg.transcript)
	}
	view := ansi.Strip(app.dialog.View())
	if !strings.Contains(view, "Save Transcript") {
		t.Fatalf("dialog view = %q, want the Save Transcript title", view)
	}
	if !strings.Contains(view, "transcripts/") || !strings.Contains(view, "ws-") {
		t.Fatalf("prefill = %q, want ~/.amux/transcripts/ws-<ts>.txt", view)
	}
}

// TestHandlePrefixSaveTranscript_NoTerminal warns instead of opening a dialog
// when the focused pane has no transcript.
func TestHandlePrefixSaveTranscript_NoTerminal(t *testing.T) {
	app, _, _ := newPrefixTestApp(t)
	app.config = &config.Config{}
	app.toast = common.NewToastModel()

	app.handlePrefixCommand(tea.KeyPressMsg{Code: 't', Text: "t"})
	status, _ := app.handlePrefixCommand(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if status != prefixMatchComplete {
		t.Fatalf("expected 't f' to complete, got %v", status)
	}
	if app.dialog != nil {
		t.Fatal("dialog opened with no transcript to save")
	}
	if view := ansi.Strip(app.toast.View()); !strings.Contains(view, "No transcript") {
		t.Fatalf("toast = %q, want the no-transcript warning", view)
	}
}

// TestDialogResultSaveTranscript_WritesFullTranscript proves the confirm path
// writes the captured transcript verbatim — no TruncateTranscriptTail — under
// a user-chosen path, creating missing directories.
func TestDialogResultSaveTranscript_WritesFullTranscript(t *testing.T) {
	app, _, _ := newPrefixTestApp(t)
	app.toast = common.NewToastModel()

	transcript := "line-1\r\nline-2 with marker\r\n"
	dest := filepath.Join(t.TempDir(), "sub", "out.txt")
	cmd := dialogResultSaveTranscript(app, common.DialogResult{
		ID:        DialogSaveTranscript,
		Confirmed: true,
		Value:     dest,
	}, dialogContext{transcript: transcript})
	if cmd == nil {
		t.Fatal("expected the write cmd")
	}
	msg := cmd()
	toast, ok := msg.(messages.Toast)
	if !ok || toast.Level != messages.ToastSuccess {
		t.Fatalf("msg = %#v, want success toast", msg)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read written transcript: %v", err)
	}
	if string(got) != transcript {
		t.Fatalf("written = %q, want verbatim transcript (no tail-cut)", got)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
}

// TestDialogResultSaveTranscript_Errors covers the failure surfaces: empty
// path and an unwritable destination both produce error toasts.
func TestDialogResultSaveTranscript_Errors(t *testing.T) {
	app, _, _ := newPrefixTestApp(t)
	app.toast = common.NewToastModel()

	if cmd := dialogResultSaveTranscript(app, common.DialogResult{
		ID: DialogSaveTranscript, Confirmed: true, Value: "   ",
	}, dialogContext{transcript: "text"}); cmd == nil {
		t.Fatal("empty path should still surface an error toast")
	}

	blocked := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := dialogResultSaveTranscript(app, common.DialogResult{
		ID: DialogSaveTranscript, Confirmed: true,
		Value: filepath.Join(blocked, "out.txt"),
	}, dialogContext{transcript: "text"})
	msg := cmd()
	toast, ok := msg.(messages.Toast)
	if !ok || toast.Level != messages.ToastError {
		t.Fatalf("msg = %#v, want error toast for unwritable dir", msg)
	}
}
