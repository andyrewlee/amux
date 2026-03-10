package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/vterm"
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

func TestHandleTabEvent_WriteOutputEmitsPostWriteRedrawForChatAndActiveTabs(t *testing.T) {
	tests := []struct {
		name          string
		assistant     string
		visible       bool
		wantRefreshes int
	}{
		{name: "chat background", assistant: "codex", wantRefreshes: 1},
		{name: "non-chat visible", assistant: "bash", visible: true, wantRefreshes: 1},
		{name: "non-chat background", assistant: "bash", wantRefreshes: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			tab := &Tab{
				ID:        TabID("tab-write-output"),
				Assistant: tt.assistant,
				Terminal:  vterm.New(80, 24),
			}
			tab.setPostWriteVisible(tt.visible)

			refreshes := 0
			m.msgSink = func(msg tea.Msg) {
				if _, ok := msg.(PTYCursorRefresh); ok {
					refreshes++
				}
			}

			m.handleTabEvent(tabEvent{
				tab:         tab,
				workspaceID: "ws",
				tabID:       tab.ID,
				kind:        tabEventWriteOutput,
				output:      []byte("x"),
			})

			if refreshes != tt.wantRefreshes {
				t.Fatalf("expected %d cursor refresh messages, got %d", tt.wantRefreshes, refreshes)
			}
		})
	}
}

func TestShouldPostWriteRedraw(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")

	tests := []struct {
		name      string
		assistant string
		visible   bool
		want      bool
	}{
		{name: "chat background", assistant: "codex", want: true},
		{name: "non-chat visible", assistant: "bash", visible: true, want: true},
		{name: "non-chat background", assistant: "bash", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tab := &Tab{
				ID:        TabID("tab-redraw"),
				Assistant: tt.assistant,
				Workspace: ws,
			}
			tab.setPostWriteVisible(tt.visible)
			if got := m.shouldPostWriteRedraw(tab); got != tt.want {
				t.Fatalf("expected shouldPostWriteRedraw=%v, got %v", tt.want, got)
			}
		})
	}
}
