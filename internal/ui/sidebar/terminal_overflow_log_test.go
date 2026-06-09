package sidebar

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/vterm"
)

// TestNoteOverflowDropLocked_ThrottlesAndAggregates pins the throttle window and
// byte-aggregation contract independent of the log writer.
func TestNoteOverflowDropLocked_ThrottlesAndAggregates(t *testing.T) {
	ts := &TerminalState{}

	logNow, total := ts.noteOverflowDropLocked(100)
	if !logNow || total != 100 {
		t.Fatalf("first drop should log immediately with its total, got logNow=%v total=%d", logNow, total)
	}
	if ln, _ := ts.noteOverflowDropLocked(50); ln {
		t.Fatal("second drop within the throttle window should be suppressed")
	}
	if ln, _ := ts.noteOverflowDropLocked(25); ln {
		t.Fatal("third drop within the throttle window should be suppressed")
	}
	ts.lastOverflowLogAt = time.Now().Add(-2 * overflowLogThrottle)
	logNow, total = ts.noteOverflowDropLocked(10)
	if !logNow {
		t.Fatal("a drop after the throttle window should log")
	}
	if total != 85 {
		t.Fatalf("expected aggregated 50+25+10=85 dropped bytes, got %d", total)
	}
}

// TestHandlePTYOutput_OverflowEmitsThrottledWarn drives the real sidebar overflow
// path and asserts exactly one Warn is written even though two overflows occur.
func TestHandlePTYOutput_OverflowEmitsThrottledWarn(t *testing.T) {
	dir := t.TempDir()
	if err := logging.Initialize(dir, logging.LevelWarn); err != nil {
		t.Fatalf("logging init: %v", err)
	}
	defer logging.Close()

	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := TerminalTabID("term-overflow")
	state := &TerminalState{VTerm: vterm.New(80, 24)}
	m.tabsByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: state}}

	overflowChunk := bytes.Repeat([]byte("x"), ptyMaxBufferedBytes+4096)
	_ = m.handlePTYOutput(messages.SidebarPTYOutput{WorkspaceID: wsID, TabID: string(tabID), Data: overflowChunk})
	_ = m.handlePTYOutput(messages.SidebarPTYOutput{WorkspaceID: wsID, TabID: string(tabID), Data: bytes.Repeat([]byte("y"), ptyMaxBufferedBytes+4096)})

	logging.Close()
	if got := countLogLines(t, dir, "Sidebar PTY output overflow"); got != 1 {
		t.Fatalf("expected exactly one throttled overflow Warn, got %d", got)
	}
}

func countLogLines(t *testing.T, dir, needle string) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "amux-*.log"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no log file written in %s (err=%v)", dir, err)
	}
	count := 0
	for _, p := range matches {
		b, readErr := os.ReadFile(p)
		if readErr != nil {
			t.Fatalf("read log: %v", readErr)
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, needle) {
				count++
			}
		}
	}
	return count
}
