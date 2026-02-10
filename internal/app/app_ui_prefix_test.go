package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
)

func newPrefixTestApp(t *testing.T) (*App, *data.Workspace, *center.Model) {
	t.Helper()

	cfg := &config.Config{
		Assistants: map[string]config.AssistantConfig{
			"claude": {},
		},
	}
	ws := &data.Workspace{
		Name: "ws",
		Repo: "/repo/ws",
		Root: "/repo/ws",
	}
	centerModel := center.New(cfg)
	centerModel.SetWorkspace(ws)

	app := &App{
		center:      centerModel,
		keymap:      DefaultKeyMap(),
		focusedPane: messages.PaneCenter,
	}
	return app, ws, centerModel
}

func TestHandlePrefixNumericTabSelection_InvalidIndexNoOp(t *testing.T) {
	app, ws, centerModel := newPrefixTestApp(t)
	centerModel.AddTab(&center.Tab{
		ID:          center.TabID("tab-1"),
		Name:        "Claude",
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "sess-1",
		Detached:    true,
	})

	handled, cmd := app.handlePrefixCommand(tea.KeyPressMsg{Code: '9', Text: "9"})
	if !handled {
		t.Fatalf("expected numeric shortcut to be handled")
	}
	if cmd != nil {
		t.Fatalf("expected out-of-range numeric selection to return nil command")
	}
}

func TestHandlePrefixNumericTabSelection_ValidIndexTriggersReattach(t *testing.T) {
	app, ws, centerModel := newPrefixTestApp(t)
	centerModel.AddTab(&center.Tab{
		ID:          center.TabID("tab-1"),
		Name:        "Claude 1",
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "sess-1",
		Detached:    false,
		Running:     true,
	})
	centerModel.AddTab(&center.Tab{
		ID:          center.TabID("tab-2"),
		Name:        "Claude 2",
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "sess-2",
		Detached:    true,
	})

	handled, cmd := app.handlePrefixCommand(tea.KeyPressMsg{Code: '2', Text: "2"})
	if !handled {
		t.Fatalf("expected numeric shortcut to be handled")
	}
	if cmd == nil {
		t.Fatalf("expected valid numeric selection to trigger follow-up command")
	}
}

func TestHandlePrefixNextTab_SingleTabNoOp(t *testing.T) {
	app, ws, centerModel := newPrefixTestApp(t)
	centerModel.AddTab(&center.Tab{
		ID:          center.TabID("tab-1"),
		Name:        "Claude",
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "sess-1",
		Detached:    true,
	})

	handled, cmd := app.handlePrefixCommand(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !handled {
		t.Fatalf("expected next-tab shortcut to be handled")
	}
	if cmd != nil {
		t.Fatalf("expected single-tab next to be a no-op without reattach command")
	}
}

func TestHandlePrefixPrevTab_SingleTabNoOp(t *testing.T) {
	app, ws, centerModel := newPrefixTestApp(t)
	centerModel.AddTab(&center.Tab{
		ID:          center.TabID("tab-1"),
		Name:        "Claude",
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "sess-1",
		Detached:    true,
	})

	handled, cmd := app.handlePrefixCommand(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if !handled {
		t.Fatalf("expected prev-tab shortcut to be handled")
	}
	if cmd != nil {
		t.Fatalf("expected single-tab prev to be a no-op without reattach command")
	}
}
