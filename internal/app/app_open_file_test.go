package app

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func TestHandleOpenFileInVim(t *testing.T) {
	ws := harnessWorkspace()

	// A real on-disk workspace for the pass-through and refusal cases — the
	// handler requires the target to be a regular file inside ws.Root.
	wsRoot := t.TempDir()
	realWS := &data.Workspace{Name: "real", Repo: wsRoot, Root: wsRoot}
	realFile := filepath.Join(wsRoot, "main.go")
	if err := os.WriteFile(realFile, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(wsRoot, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(wsRoot, "pipe")
	if err := unix.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	// Transcript picks live under the amux state home, not ws.Root.
	amuxHome := t.TempDir()
	transcriptsDir := filepath.Join(amuxHome, "transcripts")
	if err := os.MkdirAll(transcriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(transcriptsDir, "t.txt")
	if err := os.WriteFile(transcript, []byte("log"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		msg       messages.OpenFileInVim
		wantCmd   bool
		wantToast bool // refusal toasts vs silent noops
		setHome   bool // point config.Paths.Home at the fixture state home
	}{
		{
			name:    "nil workspace is a noop",
			msg:     messages.OpenFileInVim{Workspace: nil, Path: "/repo/file.go"},
			wantCmd: false,
		},
		{
			name:    "empty path is a noop",
			msg:     messages.OpenFileInVim{Workspace: ws, Path: ""},
			wantCmd: false,
		},
		{
			name:    "nil workspace and empty path is a noop",
			msg:     messages.OpenFileInVim{Workspace: nil, Path: ""},
			wantCmd: false,
		},
		{
			name:    "valid workspace and path returns a command",
			msg:     messages.OpenFileInVim{Workspace: realWS, Path: realFile},
			wantCmd: true,
		},
		{
			name:      "symlink to outside file is refused",
			msg:       messages.OpenFileInVim{Workspace: realWS, Path: link},
			wantCmd:   true,
			wantToast: true,
		},
		{
			name:      "fifo is refused",
			msg:       messages.OpenFileInVim{Workspace: realWS, Path: fifo},
			wantCmd:   true,
			wantToast: true,
		},
		{
			name:      "workspace directory is refused",
			msg:       messages.OpenFileInVim{Workspace: realWS, Path: wsRoot},
			wantCmd:   true,
			wantToast: true,
		},
		{
			name:      "path outside the workspace is refused",
			msg:       messages.OpenFileInVim{Workspace: realWS, Path: outside},
			wantCmd:   true,
			wantToast: true,
		},
		{
			name:      "dotdot escape is refused",
			msg:       messages.OpenFileInVim{Workspace: realWS, Path: filepath.Join(wsRoot, "..", "..", "etc", "hostname")},
			wantCmd:   true,
			wantToast: true,
		},
		{
			name:      "missing file is refused",
			msg:       messages.OpenFileInVim{Workspace: realWS, Path: filepath.Join(wsRoot, "gone.go")},
			wantCmd:   true,
			wantToast: true,
		},
		{
			name:    "transcript under the amux home opens",
			msg:     messages.OpenFileInVim{Workspace: realWS, Path: transcript},
			wantCmd: true,
			setHome: true,
		},
		{
			name:      "file outside both roots is refused",
			msg:       messages.OpenFileInVim{Workspace: realWS, Path: filepath.Join(t.TempDir(), "elsewhere.txt")},
			wantCmd:   true,
			wantToast: true,
			setHome:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newDialogHarness(t)
			if tc.setHome {
				h.app.config.Paths.Home = amuxHome
			}
			beforeCenter := h.app.center
			beforeEvents := len(h.app.toast.Events())

			cmd := h.app.handleOpenFileInVim(tc.msg)
			events := h.app.toast.Events()

			if tc.wantToast {
				if len(events) != beforeEvents+1 || events[0].Type != common.ToastWarning {
					t.Fatalf("expected a refusal warning toast, events=%v", events)
				}
				if cmd == nil {
					t.Fatal("expected the refusal's toast command")
				}
				// A refusal must not hand the path to the center pane.
				if h.app.center != beforeCenter {
					t.Fatal("refused open must not touch the center model")
				}
				return
			}
			if len(events) != beforeEvents {
				t.Fatalf("unexpected toast on %s: %v", tc.name, events)
			}

			if tc.wantCmd {
				if cmd == nil {
					t.Fatal("expected a command for a valid open-file request")
				}
				// The center model is reassigned even when the work is deferred
				// into the returned closure. We deliberately do NOT execute the
				// returned cmd because it would exec tmux/vim.
				if h.app.center == nil {
					t.Fatal("expected center to remain set after dispatch")
				}
				return
			}
			if cmd != nil {
				t.Fatalf("expected nil command for noop request, got %T", cmd)
			}
			// A noop must not mutate the center model.
			if h.app.center != beforeCenter {
				t.Fatal("expected center to be untouched for a noop request")
			}
		})
	}
}
