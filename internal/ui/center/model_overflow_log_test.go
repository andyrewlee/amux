package center

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

// TestNoteOverflowDropLocked_ThrottlesAndAggregates pins the throttle window and
// byte-aggregation contract independent of the log writer.
func TestNoteOverflowDropLocked_ThrottlesAndAggregates(t *testing.T) {
	tab := &Tab{}

	logNow, total := tab.NoteOverflowDropLocked(100)
	if !logNow || total != 100 {
		t.Fatalf("first drop should log immediately with its total, got logNow=%v total=%d", logNow, total)
	}
	if ln, _ := tab.NoteOverflowDropLocked(50); ln {
		t.Fatal("second drop within the throttle window should be suppressed")
	}
	if ln, _ := tab.NoteOverflowDropLocked(25); ln {
		t.Fatal("third drop within the throttle window should be suppressed")
	}
	// Simulate the throttle window elapsing.
	tab.LastOverflowLogAt = time.Now().Add(-2 * ptyio.OverflowLogThrottle)
	logNow, total = tab.NoteOverflowDropLocked(10)
	if !logNow {
		t.Fatal("a drop after the throttle window should log")
	}
	if total != 85 {
		t.Fatalf("expected aggregated 50+25+10=85 dropped bytes, got %d", total)
	}
}

// TestUpdatePTYOutput_OverflowEmitsThrottledWarn drives the real overflow path
// and asserts exactly one Warn is written at the default level even though two
// overflows occur back to back.
func TestUpdatePTYOutput_OverflowEmitsThrottledWarn(t *testing.T) {
	dir := t.TempDir()
	if err := logging.Initialize(dir, logging.LevelWarn); err != nil {
		t.Fatalf("logging init: %v", err)
	}
	defer logging.Close()

	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	m := New(cfg)
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo"}
	m.SetWorkspace(ws)
	wsID := string(ws.ID())
	tab := &Tab{ID: TabID("overflow-tab"), Workspace: ws, Terminal: vterm.New(80, 24), Running: true}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0

	overflowChunk := bytes.Repeat([]byte("x"), ptyMaxBufferedBytes+4096)
	_ = m.updatePTYOutput(PTYOutput{WorkspaceID: wsID, TabID: tab.ID, Data: overflowChunk})
	// A second overflow immediately after must be throttled (no second Warn).
	_ = m.updatePTYOutput(PTYOutput{WorkspaceID: wsID, TabID: tab.ID, Data: bytes.Repeat([]byte("y"), ptyMaxBufferedBytes+4096)})

	logging.Close()
	if got := countLogLines(t, dir, "PTY output overflow"); got != 1 {
		t.Fatalf("expected exactly one throttled overflow Warn, got %d", got)
	}
}

func TestUpdatePTYOutput_OverflowWarnReportsActualTrimmedBytes(t *testing.T) {
	dir := t.TempDir()
	if err := logging.Initialize(dir, logging.LevelWarn); err != nil {
		t.Fatalf("logging init: %v", err)
	}
	defer logging.Close()

	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	m := New(cfg)
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo"}
	m.SetWorkspace(ws)
	wsID := string(ws.ID())
	tab := &Tab{ID: TabID("overflow-tab"), Workspace: ws, Terminal: vterm.New(80, 24), Running: true}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0

	controlSeq := []byte("\x1b[>1;10;0c")
	chunk := append([]byte{}, controlSeq...)
	chunk = append(chunk, bytes.Repeat([]byte("x"), ptyMaxBufferedBytes+1-len(controlSeq))...)
	_ = m.updatePTYOutput(PTYOutput{WorkspaceID: wsID, TabID: tab.ID, Data: chunk})

	logging.Close()
	logText := readLogText(t, dir)
	if !strings.Contains(logText, "dropped 10 bytes") {
		t.Fatalf("expected overflow Warn to report dropped 10 bytes, log:\n%s", logText)
	}
}

func countLogLines(t *testing.T, dir, needle string) int {
	t.Helper()
	logText := readLogText(t, dir)
	count := 0
	for _, line := range strings.Split(logText, "\n") {
		if strings.Contains(line, needle) {
			count++
		}
	}
	return count
}

func readLogText(t *testing.T, dir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "amux-*.log"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no log file written in %s (err=%v)", dir, err)
	}
	var out strings.Builder
	for _, p := range matches {
		b, readErr := os.ReadFile(p)
		if readErr != nil {
			t.Fatalf("read log: %v", readErr)
		}
		out.Write(b)
	}
	return out.String()
}
