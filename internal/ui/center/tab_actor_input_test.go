package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/common"
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

func TestShouldPostTabActorRedraw(t *testing.T) {
	tests := []struct {
		kind tabEventKind
		want bool
	}{
		{kind: tabEventSelectionClear, want: false},
		{kind: tabEventSelectionStart, want: true},
		{kind: tabEventSelectionUpdate, want: true},
		{kind: tabEventScrollBy, want: true},
		{kind: tabEventScrollPage, want: true},
		{kind: tabEventDiffInput, want: true},
		{kind: tabEventSendInput, want: false},
		{kind: tabEventPaste, want: false},
		{kind: tabEventWriteOutput, want: false},
	}

	for _, tt := range tests {
		if got := shouldPostTabActorRedraw(tt.kind); got != tt.want {
			t.Fatalf("kind %v: expected shouldPostTabActorRedraw=%v, got %v", tt.kind, tt.want, got)
		}
	}
}

func TestHandleTabEvent_SelectionClearEmitsRedrawOnlyWhenSelectionChanged(t *testing.T) {
	tests := []struct {
		name       string
		prepareTab func(*Tab)
		wantRedraw bool
	}{
		{
			name: "active selection",
			prepareTab: func(tab *Tab) {
				tab.Selection = common.SelectionState{Active: true, StartX: 1, StartLine: 1, EndX: 3, EndLine: 1}
				tab.Terminal.SetSelection(1, 1, 3, 1, true, false)
			},
			wantRedraw: true,
		},
		{
			name:       "no selection",
			prepareTab: func(tab *Tab) {},
			wantRedraw: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			tab := &Tab{Terminal: vterm.New(80, 24)}
			tt.prepareTab(tab)

			redraws := 0
			m.msgSink = func(msg tea.Msg) {
				if _, ok := msg.(tabActorRedraw); ok {
					redraws++
				}
			}

			m.handleTabEvent(tabEvent{tab: tab, kind: tabEventSelectionClear})

			if got := redraws > 0; got != tt.wantRedraw {
				t.Fatalf("expected redraw=%v, got redraws=%d", tt.wantRedraw, redraws)
			}
		})
	}
}
