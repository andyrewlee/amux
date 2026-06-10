package sidebar

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/vterm"
)

func TestUpdateKeyPgUpScrollsOneLineOnShortTerminal(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo/ws", Root: "/repo/ws"}
	term := vterm.New(80, 1)
	for i := 0; i < 10; i++ {
		term.Write([]byte("line\n"))
	}

	state := &TerminalState{
		VTerm:    term,
		Terminal: &appPty.Terminal{},
	}
	tab := &TerminalTab{
		ID:    generateTerminalTabID(),
		Name:  "Terminal 1",
		State: state,
	}

	m := NewTerminalModel()
	m.workspace = ws
	m.focused = true
	m.tabs.ByWorkspace[string(ws.ID())] = []*TerminalTab{tab}
	m.tabs.ActiveByWorkspace[string(ws.ID())] = 0

	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})

	state.mu.Lock()
	offset, _ := state.VTerm.GetScrollInfo()
	state.mu.Unlock()
	if offset != 1 {
		t.Fatalf("expected PgUp on a short sidebar terminal to scroll by 1 line, got %d", offset)
	}
}
