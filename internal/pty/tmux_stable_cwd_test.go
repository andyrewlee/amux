package pty

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestNewTmuxClientWithSizeRejectsInvalidWorkingDirectory(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing")
		term, err := NewTmuxClientWithSize("true", missing, nil, 24, 80)
		if err == nil {
			if term != nil {
				_ = term.Close()
			}
			t.Fatal("expected missing working directory to fail synchronously")
		}
		if term != nil {
			t.Fatal("expected no terminal for missing working directory")
		}
	})

	t.Run("not a directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		term, err := NewTmuxClientWithSize("true", path, nil, 24, 80)
		if err == nil {
			if term != nil {
				_ = term.Close()
			}
			t.Fatal("expected non-directory working path to fail synchronously")
		}
		if term != nil {
			t.Fatal("expected no terminal for non-directory working path")
		}
	})

	t.Run("not searchable", func(t *testing.T) {
		path := t.TempDir()
		if err := os.Chmod(path, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o700) })
		term, err := NewTmuxClientWithSize("true", path, nil, 24, 80)
		if err == nil {
			if term != nil {
				_ = term.Close()
			}
			t.Fatal("expected unsearchable working directory to fail synchronously")
		}
		if term != nil {
			t.Fatal("expected no terminal for unsearchable working directory")
		}
	})
}

// TestAgentManagerStartsTmuxServerOutsideWorkspace reproduces the mass-delete
// failure at the process boundary. The first viewer starts the shared server;
// its workspace is then deleted while the server stays alive. A later viewer
// must start without either shell's deleted-cwd diagnostics and must land in
// its own workspace.
func TestAgentManagerStartsTmuxServerOutsideWorkspace(t *testing.T) {
	if err := tmux.EnsureAvailable(); err != nil {
		t.Skipf("tmux unavailable: %v", err)
	}

	serverName := fmt.Sprintf("amux-stable-cwd-%d", time.Now().UnixNano())
	opts := tmux.Options{
		ServerName:     serverName,
		ConfigPath:     "/dev/null",
		CommandTimeout: 5 * time.Second,
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", serverName, "kill-server").Run()
	})

	m := NewAgentManager(testConfig())
	m.SetTmuxOptions(opts)
	firstParent := t.TempDir()
	firstRoot := filepath.Join(firstParent, "deleted-workspace")
	if err := os.Mkdir(firstRoot, 0o700); err != nil {
		t.Fatalf("create first workspace: %v", err)
	}
	firstWorkspace := data.NewWorkspace("first", "first", "main", firstParent, firstRoot)
	firstSession := "first-viewer"
	first, err := m.CreateViewer(firstWorkspace, "sleep 300", firstSession, 24, 80)
	if err != nil {
		t.Fatalf("create first viewer: %v", err)
	}
	t.Cleanup(func() { _ = m.CloseAgent(first) })
	waitForPTYTestSession(t, firstSession, opts)

	if err := os.Remove(firstRoot); err != nil {
		t.Fatalf("delete first workspace: %v", err)
	}

	secondRoot := t.TempDir()
	secondWorkspace := data.NewWorkspace("second", "second", "main", secondRoot, secondRoot)
	const marker = "AMUX_STABLE_CWD_READY"
	secondSession := "second-viewer"
	second, err := m.CreateViewer(
		secondWorkspace,
		"printf '"+marker+"\\n'; sleep 300",
		secondSession,
		24,
		80,
	)
	if err != nil {
		t.Fatalf("create second viewer: %v", err)
	}
	t.Cleanup(func() { _ = m.CloseAgent(second) })
	if got := second.Terminal.cmd.Dir; got != tmuxClientWorkingDirectory {
		t.Fatalf("second tmux client cwd = %q, want stable cwd %q", got, tmuxClientWorkingDirectory)
	}

	output := readTerminalThroughMarker(t, second.Terminal, marker, 5*time.Second)
	if strings.Contains(output, "getcwd") || strings.Contains(output, "retrieving current directory") {
		t.Fatalf("second viewer inherited deleted server cwd diagnostics:\n%s", output)
	}

	waitForPTYTestSession(t, secondSession, opts)
	cmd := exec.Command(
		"tmux", "-L", serverName, "-f", "/dev/null",
		"list-panes", "-t", "="+secondSession, "-F", "#{pane_current_path}",
	)
	panePath, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("read second pane cwd: %v\n%s", err, panePath)
	}
	got, err := filepath.EvalSymlinks(strings.TrimSpace(string(panePath)))
	if err != nil {
		t.Fatalf("resolve second pane cwd: %v", err)
	}
	want, err := filepath.EvalSymlinks(secondRoot)
	if err != nil {
		t.Fatalf("resolve second workspace cwd: %v", err)
	}
	if got != want {
		t.Fatalf("second pane cwd = %q, want %q", got, want)
	}
}

func waitForPTYTestSession(t *testing.T, sessionName string, opts tmux.Options) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, err := tmux.SessionStateFor(sessionName, opts)
		if err == nil && state.Exists && state.HasLivePane {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("tmux session %q did not become live", sessionName)
}

func readTerminalThroughMarker(t *testing.T, term *Terminal, marker string, timeout time.Duration) string {
	t.Helper()
	type result struct {
		output []byte
		err    error
	}
	done := make(chan result, 1)
	go func() {
		var output []byte
		buf := make([]byte, 4096)
		for {
			n, err := term.Read(buf)
			output = append(output, buf[:n]...)
			if bytes.Contains(output, []byte(marker)) || err != nil {
				done <- result{output: output, err: err}
				return
			}
		}
	}()

	select {
	case got := <-done:
		if !bytes.Contains(got.output, []byte(marker)) {
			t.Fatalf("terminal closed before marker %q: %v\n%s", marker, got.err, got.output)
		}
		return string(got.output)
	case <-time.After(timeout):
		_ = term.Close()
		got := <-done
		t.Fatalf("timeout waiting for marker %q:\n%s", marker, got.output)
		return ""
	}
}
