package center

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/vterm"
)

// TestInitialTaskSentAfterModeSet proves the readiness gate: the queued launch
// task is not sent while the agent has emitted no private-mode set (pre-TUI),
// and is sent exactly once — task + "\r" through the binding's input FIFO — on
// the first write pass after a DECSET lands.
func TestInitialTaskSentAfterModeSet(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "sent.txt")
	term, err := appPty.NewWithSize("cat >"+out, dir, nil, 24, 80)
	if err != nil {
		t.Fatalf("expected test PTY terminal: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })

	tab := &Tab{
		ID:        "tab-task",
		Assistant: "claude",
		Terminal:  vterm.New(80, 24),
		Agent:     &appPty.Agent{Terminal: term},
		Running:   true,
	}
	tab.pendingInitialTask = "fix the bug"

	m := &Model{}
	m.msgSink = func(tea.Msg) {}

	write := func(chunk []byte) {
		m.handleWriteOutput(tabEvent{
			tab:            tab,
			tabID:          tab.ID,
			workspaceID:    "ws",
			output:         chunk,
			filteredOutput: chunk,
		})
	}

	// Plain output before any mode set: task must NOT send.
	write([]byte("still booting\r\n"))
	time.Sleep(200 * time.Millisecond)
	if b, _ := os.ReadFile(out); len(b) != 0 {
		t.Fatalf("task sent before readiness: %q", b)
	}

	// A DECSET (mouse reporting) marks the TUI input loop live.
	write([]byte("\x1b[?1000h"))
	testutil.Eventually(t, 3*time.Second, 20*time.Millisecond, func() bool {
		b, _ := os.ReadFile(out)
		return string(b) == "fix the bug\n"
	}, "task never sent after mode set")

	// Second write: task cleared, nothing re-sent.
	write([]byte("\x1b[?1006h more\r\n"))
	time.Sleep(150 * time.Millisecond)
	if b, _ := os.ReadFile(out); string(b) != "fix the bug\n" {
		t.Fatalf("task re-sent: %q", b)
	}
}

// TestInitialTaskClearedOnDetach proves a queued task cannot leak into a
// reattach: markDetachedLocked drops it before a new agent could see it.
func TestInitialTaskClearedOnDetach(t *testing.T) {
	tab := &Tab{Terminal: vterm.New(80, 24)}
	tab.pendingInitialTask = "stale task"
	tab.mu.Lock()
	tab.markDetachedLocked()
	tab.mu.Unlock()
	if tab.pendingInitialTask != "" {
		t.Fatal("detach left a pending initial task")
	}
}

// TestInitialTaskNotSentWithoutModes pins the conservative half: an agent that
// never emits a private-mode set never receives the task (no fake readiness).
func TestInitialTaskNotSentWithoutModes(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "sent.txt")
	term, err := appPty.NewWithSize("cat >"+out, dir, nil, 24, 80)
	if err != nil {
		t.Fatalf("expected test PTY terminal: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })

	tab := &Tab{
		ID:       "tab-nomodes",
		Terminal: vterm.New(80, 24),
		Agent:    &appPty.Agent{Terminal: term},
		Running:  true,
	}
	tab.pendingInitialTask = "never sent"

	m := &Model{}
	m.msgSink = func(tea.Msg) {}

	m.handleWriteOutput(tabEvent{
		tab:            tab,
		tabID:          tab.ID,
		workspaceID:    "ws",
		output:         []byte("plain line\r\n"),
		filteredOutput: []byte("plain line\r\n"),
	})
	time.Sleep(150 * time.Millisecond)
	if b, _ := os.ReadFile(out); len(b) != 0 {
		t.Fatalf("task sent with no readiness signal: %q", b)
	}
	if tab.pendingInitialTask != "never sent" {
		t.Fatal("task cleared without a send")
	}
}
