package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// newTranscriptApp builds an app with a temp amux home so the transcripts
// dir is controllable per test.
func newTranscriptApp(t *testing.T) (*App, string) {
	t.Helper()
	home := t.TempDir()
	app, ws, _ := newPrefixTestApp(t)
	// newPrefixTestApp leaves app.config nil; the browse command reads
	// Paths.Home for the transcripts dir.
	app.config = &config.Config{Paths: &config.Paths{Home: home}}
	app.toast = common.NewToastModel()
	app.activeWorkspace = ws
	return app, home
}

func TestBrowseTranscripts_RequiresWorkspace(t *testing.T) {
	app, _ := newTranscriptApp(t)
	app.activeWorkspace = nil

	app.browseTranscriptsCommand() // returns a toast-timer cmd; don't invoke it
	if view := app.toast.View(); !strings.Contains(view, "Select a workspace") {
		t.Fatalf("expected workspace-selection toast, got %q", view)
	}
	if app.filePicker != nil {
		t.Fatal("picker opened without a workspace")
	}
}

func TestBrowseTranscripts_MissingDirToasts(t *testing.T) {
	app, _ := newTranscriptApp(t)
	// ~/.amux/transcripts never created — the picker must not open empty.

	app.browseTranscriptsCommand()
	if view := app.toast.View(); !strings.Contains(view, "No transcripts saved yet") {
		t.Fatalf("expected 'No transcripts saved yet' toast, got %q", view)
	}
	if app.filePicker != nil {
		t.Fatal("picker opened for a missing transcripts dir")
	}
}

func TestBrowseTranscripts_EmptyDirToasts(t *testing.T) {
	app, home := newTranscriptApp(t)
	if err := os.MkdirAll(filepath.Join(home, "transcripts"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	app.browseTranscriptsCommand()
	if view := app.toast.View(); !strings.Contains(view, "No transcripts saved yet") {
		t.Fatalf("expected 'No transcripts saved yet' toast, got %q", view)
	}
}

func TestBrowseTranscripts_OpensPickerRootedAtDir(t *testing.T) {
	app, home := newTranscriptApp(t)
	dir := filepath.Join(home, "transcripts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ws-20260925-120000.txt"), []byte("t"), 0o600); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}

	cmd := app.browseTranscriptsCommand()
	if app.filePicker == nil || !app.filePicker.Visible() {
		t.Fatal("picker did not open over the transcripts dir")
	}
	if cmd == nil {
		t.Fatal("expected the picker's initial directory load cmd")
	}
	// The picker is rooted at the transcripts dir: its path input/selection
	// lands inside it. (Directory listing loads async via LoadCmd — the cmd's
	// existence, not its content, is the wiring proof here.)
}

func TestDialogResultBrowseTranscripts_EmitsViewerOpen(t *testing.T) {
	app, _ := newTranscriptApp(t)

	cmd := dialogResultBrowseTranscripts(app, common.DialogResult{
		ID:        DialogBrowseTranscripts,
		Confirmed: true,
		Value:     "~/.amux/transcripts/ws-20260925-120000.txt",
	}, dialogContext{})
	if cmd == nil {
		t.Fatal("confirmed pick should emit a cmd")
	}
	msg, ok := cmd().(messages.OpenFileInVim)
	if !ok {
		t.Fatalf("emitted %T, want OpenFileInVim", cmd())
	}
	if msg.Path == "" || msg.Workspace != app.activeWorkspace {
		t.Fatalf("OpenFileInVim = %+v, want path + active workspace", msg)
	}
}

func TestDialogResultBrowseTranscripts_NoWorkspace(t *testing.T) {
	app, _ := newTranscriptApp(t)
	app.activeWorkspace = nil

	cmd := dialogResultBrowseTranscripts(app, common.DialogResult{
		ID: DialogBrowseTranscripts, Confirmed: true, Value: "/x.txt",
	}, dialogContext{})
	if cmd == nil {
		t.Fatal("missing workspace should still surface a toast cmd")
	}
	if view := app.toast.View(); !strings.Contains(view, "Select a workspace") {
		t.Fatalf("expected workspace toast, got %q", view)
	}
}
