package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	appPty "github.com/andyrewlee/amux/internal/pty"
)

func TestSendToTerminal_EmitsCursorRefreshOnlyForChatTabs(t *testing.T) {
	dir := t.TempDir()
	term, err := appPty.NewWithSize("cat >/dev/null", dir, nil, 24, 80)
	if err != nil {
		t.Fatalf("expected test PTY terminal: %v", err)
	}
	defer func() { _ = term.Close() }()

	ws := newTestWorkspace("ws", dir)
	tabID := TabID("tab-send-input")
	workspaceID := string(ws.ID())

	tests := []struct {
		name          string
		assistant     string
		wantRefreshes int
	}{
		{name: "chat", assistant: "codex", wantRefreshes: 1},
		{name: "non-chat", assistant: "bash", wantRefreshes: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			tab := &Tab{
				ID:        tabID,
				Assistant: tt.assistant,
				Workspace: ws,
				Agent:     &appPty.Agent{Terminal: term},
			}

			refreshes := 0
			m.msgSink = func(msg tea.Msg) {
				if _, ok := msg.(PTYCursorRefresh); ok {
					refreshes++
				}
			}

			m.sendToTerminal(tab, "a", tabID, workspaceID, "Input")

			if refreshes != tt.wantRefreshes {
				t.Fatalf("expected %d cursor refresh messages, got %d", tt.wantRefreshes, refreshes)
			}
		})
	}
}
