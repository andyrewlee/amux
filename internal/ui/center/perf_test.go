package center

import (
	"bytes"
	"os"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/vterm"
)

func TestPerfScenario(t *testing.T) {
	if os.Getenv("AMUX_PERF_TEST") != "1" {
		t.Skip("set AMUX_PERF_TEST=1 to run perf scenario")
	}

	logDir := os.Getenv("AMUX_PERF_LOG_DIR")
	if logDir == "" {
		logDir = t.TempDir()
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("log dir: %v", err)
	}
	if err := logging.Initialize(logDir, logging.LevelInfo); err != nil {
		t.Fatalf("logging init: %v", err)
	}
	defer logging.Close()

	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}

	m := New(cfg)
	wt := &data.Workspace{
		Name: "perf",
		Repo: "/tmp/perf-repo",
		Root: "/tmp/perf-repo",
	}
	m.SetWorkspace(wt)
	wtID := string(wt.ID())
	tab := &Tab{
		ID:        TabID("perf-tab"),
		Workspace: wt,
		Terminal:  vterm.New(120, 40),
		Running:   true,
	}
	m.tabsByWorkspace[wtID] = []*Tab{tab}
	m.activeTabByWorkspace[wtID] = 0
	m.SetSize(120, 40)
	m.SetOffset(0)
	m.Focus()

	payload := bytes.Repeat([]byte("lorem ipsum dolor sit amet 0123456789\n"), 512)

	start := time.Now()
	for time.Since(start) < 2*time.Second {
		var cmd tea.Cmd
		m, cmd = m.Update(PTYOutput{WorkspaceID: wtID, TabID: tab.ID, Data: payload})
		_ = cmd
		m, cmd = m.Update(PTYFlush{WorkspaceID: wtID, TabID: tab.ID})
		_ = cmd
		_ = m.View()
	}

	// Simulate selection drag and wheel scroll.
	var cmd tea.Cmd
	m, cmd = m.Update(tea.MouseClickMsg{X: 10, Y: 10, Button: tea.MouseLeft})
	_ = cmd
	for i := 0; i < 30; i++ {
		m, cmd = m.Update(tea.MouseMotionMsg{X: 10 + i, Y: 10 + i, Button: tea.MouseLeft})
		_ = cmd
	}
	m, cmd = m.Update(tea.MouseReleaseMsg{X: 40, Y: 40, Button: tea.MouseLeft})
	_ = cmd
	m, cmd = m.Update(tea.MouseWheelMsg{X: 12, Y: 12, Button: tea.MouseWheelDown})
	_ = cmd
	_ = m.View()

	logPath := logging.GetLogPath()
	if logPath == "" {
		t.Fatalf("expected perf log path to be set")
	}
	t.Logf("perf log: %s", logPath)
}
