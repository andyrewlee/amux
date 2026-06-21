package center

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/logging"
)

func TestPtyTraceFileName(t *testing.T) {
	cases := []struct {
		assistant  string
		wantPrefix string
	}{
		{"claude", "amux-pty-claude-"},
		{"codex", "amux-pty-codex-"},
		{"Cline", "amux-pty-cline-"},
		{"  gemini  ", "amux-pty-gemini-"},
		{"open code", "amux-pty-open-code-"},
		{"a/b\\c", "amux-pty-a-b-c-"},
		{"", "amux-pty-agent-"},
	}
	for _, c := range cases {
		got := ptyTraceFileName(c.assistant, "tab-1", "20060102-150405")
		if !strings.HasPrefix(got, c.wantPrefix) {
			t.Errorf("ptyTraceFileName(%q): got %q, want prefix %q", c.assistant, got, c.wantPrefix)
		}
		if !strings.HasSuffix(got, "-tab-1-20060102-150405.log") {
			t.Errorf("ptyTraceFileName(%q): unexpected suffix in %q", c.assistant, got)
		}
	}
}

// TestTracePTYBothDirections proves the trace records both the agent→amux
// (RECV) and amux→agent (SEND) directions in the same per-tab file, tagged so
// they are distinguishable.
func TestTracePTYBothDirections(t *testing.T) {
	t.Setenv("AMUX_PTY_TRACE", "1")
	// Pin the trace directory (ptyTraceDir resolves to the log dir) to a live
	// temp dir so the lazily-opened trace file lands somewhere we control and is
	// readable regardless of stale log paths left by sibling tests.
	dir := t.TempDir()
	if err := logging.Initialize(dir, logging.LevelDebug); err != nil {
		t.Fatalf("logging init: %v", err)
	}
	defer logging.Close()

	m := newTestModel()
	tab := &Tab{ID: TabID("trace-both"), Assistant: "claude"}
	t.Cleanup(func() {
		if tab.ptyTraceFile != nil {
			_ = tab.ptyTraceFile.Close()
		}
	})

	m.tracePTYInput(tab, []byte("hi\r"))
	m.tracePTYOutput(tab, []byte("ok"))

	if tab.ptyTraceFile == nil {
		t.Fatal("trace file was not opened")
	}
	path := tab.ptyTraceFile.Name()
	if filepath.Dir(path) != filepath.Clean(dir) {
		t.Fatalf("trace file %q not under expected dir %q", path, dir)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat trace: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("trace file permissions = %o, want 600", perm)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	got := string(data)

	if !strings.Contains(got, "SEND chunk ") {
		t.Errorf("trace missing SEND marker:\n%s", got)
	}
	if !strings.Contains(got, "RECV chunk ") {
		t.Errorf("trace missing RECV marker:\n%s", got)
	}
	// The 0D carriage return (the historically bug-prone Enter byte) must be
	// visible in the SEND hex dump so a dropped-Enter bug is debuggable.
	if !strings.Contains(got, "hi.") || !strings.Contains(got, "0d") {
		t.Errorf("SEND hex dump missing CR byte:\n%s", got)
	}
}

// TestTracePTYInputGated confirms the send-direction trace respects the
// AMUX_PTY_TRACE gate and the empty-data short-circuit.
func TestTracePTYInputGated(t *testing.T) {
	t.Setenv("AMUX_PTY_TRACE", "")

	m := newTestModel()
	tab := &Tab{ID: TabID("trace-gated"), Assistant: "claude"}
	m.tracePTYInput(tab, []byte("hi"))

	if tab.ptyTraceFile != nil {
		t.Fatal("trace file opened while AMUX_PTY_TRACE was disabled")
	}

	// Even when enabled, empty data must not open a file.
	t.Setenv("AMUX_PTY_TRACE", "1")
	m.tracePTYInput(tab, nil)
	if tab.ptyTraceFile != nil {
		t.Fatal("trace file opened for empty input data")
	}
}
