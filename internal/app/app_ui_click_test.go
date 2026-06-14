package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// newCenterClickApp builds an App wired with a sized layout, default styles and
// an empty config so the center-pane click handlers can run without a live
// Bubble Tea program or any external tmux/git process. The default 200x60
// geometry lands in three-pane mode (ShowCenter() == true).
func newCenterClickApp(t *testing.T) *App {
	t.Helper()
	cfg := &config.Config{}
	a := &App{
		layout: newLayout(200, 60),
		styles: common.DefaultStyles(),
		config: cfg,
		center: center.New(cfg),
	}
	if !a.layout.ShowCenter() {
		t.Fatal("test setup expects a layout that shows the center pane")
	}
	if a.center.HasTabs() {
		t.Fatal("test setup expects an empty center model (no tabs)")
	}
	return a
}

// welcomeHitPoint replays the production hit-region math from handleWelcomeClick
// to find the local (x, y) that lands inside the [Settings]/[Add project]
// button. Computing the coordinate the same way the handler does keeps the test
// resilient to layout/style drift instead of hard-coding a magic cell.
func welcomeHitPoint(t *testing.T, a *App, label string) (int, int, bool) {
	t.Helper()
	content := a.welcomeContent()
	lines := strings.Split(content, "\n")
	_, contentHeight := viewDimensions(content)

	placeWidth := a.layout.CenterWidth() - 4
	placeHeight := a.layout.Height() - 2
	offsetY := centerOffset(placeHeight, contentHeight)

	for i, line := range lines {
		stripped := ansi.Strip(line)
		lineWidth := lipgloss.Width(line)
		lineOffsetX := centerOffset(placeWidth, lineWidth)
		if idx := strings.Index(stripped, label); idx >= 0 {
			return idx + lineOffsetX, i + offsetY, true
		}
	}
	return 0, 0, false
}

// workspaceHitPoint replays the hit-region math from handleWorkspaceInfoClick
// for the [New agent] button (rendered at the stripped index with no centering
// offset).
func workspaceHitPoint(t *testing.T, a *App, label string) (int, int, bool) {
	t.Helper()
	content := a.renderWorkspaceInfo()
	for i, line := range strings.Split(content, "\n") {
		stripped := ansi.Strip(line)
		if idx := strings.Index(stripped, label); idx >= 0 {
			return idx, i, true
		}
	}
	return 0, 0, false
}

func TestHandleWelcomeClick(t *testing.T) {
	settingsX, settingsY, ok := welcomeHitPoint(t, newCenterClickApp(t), "[Settings]")
	if !ok {
		t.Fatal("expected to locate [Settings] hit region in welcome content")
	}
	addX, addY, ok := welcomeHitPoint(t, newCenterClickApp(t), "[Add project]")
	if !ok {
		t.Fatal("expected to locate [Add project] hit region in welcome content")
	}

	tests := []struct {
		name    string
		x, y    int
		wantCmd bool
		assert  func(t *testing.T, msg tea.Msg)
	}{
		{
			name:    "settings button opens settings dialog",
			x:       settingsX,
			y:       settingsY,
			wantCmd: true,
			assert: func(t *testing.T, msg tea.Msg) {
				if _, ok := msg.(messages.ShowSettingsDialog); !ok {
					t.Fatalf("want ShowSettingsDialog, got %T", msg)
				}
			},
		},
		{
			name:    "add-project button opens add-project dialog",
			x:       addX,
			y:       addY,
			wantCmd: true,
			assert: func(t *testing.T, msg tea.Msg) {
				if _, ok := msg.(messages.ShowAddProjectDialog); !ok {
					t.Fatalf("want ShowAddProjectDialog, got %T", msg)
				}
			},
		},
		{
			name:    "right edge of settings button (last cell) still hits",
			x:       settingsX + len("[Settings]") - 1,
			y:       settingsY,
			wantCmd: true,
			assert: func(t *testing.T, msg tea.Msg) {
				if _, ok := msg.(messages.ShowSettingsDialog); !ok {
					t.Fatalf("want ShowSettingsDialog at right edge, got %T", msg)
				}
			},
		},
		{
			name:    "one cell past the settings button misses",
			x:       settingsX + len("[Settings]"),
			y:       settingsY,
			wantCmd: false,
		},
		{
			name:    "one cell before the add-project button misses",
			x:       addX - 1,
			y:       addY,
			wantCmd: false,
		},
		{
			name:    "wrong row over the settings column misses",
			x:       settingsX,
			y:       settingsY + 1,
			wantCmd: false,
		},
		{
			name:    "origin click hits no button",
			x:       0,
			y:       0,
			wantCmd: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newCenterClickApp(t)
			cmd := a.handleWelcomeClick(tc.x, tc.y)
			if !tc.wantCmd {
				if cmd != nil {
					t.Fatalf("expected nil command at (%d,%d), got %T", tc.x, tc.y, cmd())
				}
				return
			}
			if cmd == nil {
				t.Fatalf("expected command at (%d,%d), got nil", tc.x, tc.y)
			}
			tc.assert(t, cmd())
		})
	}
}

func TestHandleWelcomeClick_ZeroPlacementBoxReturnsNil(t *testing.T) {
	// A degenerate layout collapses the placement box (placeWidth/placeHeight
	// <= 0), which must short-circuit before any hit testing.
	a := &App{
		layout: newLayout(10, 4),
		styles: common.DefaultStyles(),
		config: &config.Config{},
	}
	if cmd := a.handleWelcomeClick(0, 0); cmd != nil {
		t.Fatalf("expected nil command for collapsed placement box, got %T", cmd())
	}
}

func TestHandleWorkspaceInfoClick(t *testing.T) {
	base := func() *App {
		a := newCenterClickApp(t)
		a.activeWorkspace = &data.Workspace{Name: "ws", Branch: "b", Root: "/r"}
		return a
	}

	agentX, agentY, ok := workspaceHitPoint(t, base(), "[New agent]")
	if !ok {
		t.Fatal("expected to locate [New agent] hit region in workspace info")
	}

	tests := []struct {
		name    string
		x, y    int
		wantCmd bool
	}{
		{name: "new-agent button opens assistant dialog", x: agentX, y: agentY, wantCmd: true},
		{name: "right edge of new-agent button still hits", x: agentX + len("[New agent]") - 1, y: agentY, wantCmd: true},
		{name: "one cell past the new-agent button misses", x: agentX + len("[New agent]"), y: agentY, wantCmd: false},
		{name: "one cell before the new-agent button misses", x: agentX - 1, y: agentY, wantCmd: false},
		{name: "wrong row over the button column misses", x: agentX, y: agentY + 1, wantCmd: false},
		{name: "origin click hits no button", x: 0, y: 0, wantCmd: false},
		{name: "negative coordinates miss", x: -1, y: -1, wantCmd: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := base()
			cmd := a.handleWorkspaceInfoClick(tc.x, tc.y)
			if !tc.wantCmd {
				if cmd != nil {
					t.Fatalf("expected nil command at (%d,%d), got %T", tc.x, tc.y, cmd())
				}
				return
			}
			if cmd == nil {
				t.Fatalf("expected command at (%d,%d), got nil", tc.x, tc.y)
			}
			if _, ok := cmd().(messages.ShowSelectAssistantDialog); !ok {
				t.Fatalf("want ShowSelectAssistantDialog at (%d,%d), got %T", tc.x, tc.y, cmd())
			}
		})
	}
}

func TestHandleWorkspaceInfoClick_NilWorkspaceReturnsNil(t *testing.T) {
	a := newCenterClickApp(t)
	a.activeWorkspace = nil
	if cmd := a.handleWorkspaceInfoClick(0, 0); cmd != nil {
		t.Fatalf("expected nil command with no active workspace, got %T", cmd())
	}
}

func TestHandleCenterPaneClick_GatingAndDispatch(t *testing.T) {
	// originHit returns a MouseClickMsg whose translated local coordinates land
	// on (localX, localY) inside the center content area.
	originHit := func(a *App, localX, localY int) tea.MouseClickMsg {
		cx, cy := a.centerPaneContentOrigin()
		return tea.MouseClickMsg{Button: tea.MouseLeft, X: cx + localX, Y: cy + localY}
	}

	t.Run("non-left button is ignored", func(t *testing.T) {
		a := newCenterClickApp(t)
		a.showWelcome = true
		sx, sy, ok := welcomeHitPoint(t, a, "[Settings]")
		if !ok {
			t.Fatal("could not locate settings region")
		}
		msg := originHit(a, sx, sy)
		msg.Button = tea.MouseRight
		if cmd := a.handleCenterPaneClick(msg); cmd != nil {
			t.Fatalf("expected nil command for right click, got %T", cmd())
		}
	})

	t.Run("nil layout returns nil", func(t *testing.T) {
		a := &App{}
		if cmd := a.handleCenterPaneClick(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 5}); cmd != nil {
			t.Fatalf("expected nil command with nil layout, got %T", cmd())
		}
	})

	t.Run("click outside center horizontal band returns nil", func(t *testing.T) {
		a := newCenterClickApp(t)
		a.showWelcome = true
		// X=0 sits in the dashboard gutter, left of the center band.
		if cmd := a.handleCenterPaneClick(tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: 5}); cmd != nil {
			t.Fatalf("expected nil command for click left of center, got %T", cmd())
		}
	})

	t.Run("welcome screen routes settings click", func(t *testing.T) {
		a := newCenterClickApp(t)
		a.showWelcome = true
		sx, sy, ok := welcomeHitPoint(t, a, "[Settings]")
		if !ok {
			t.Fatal("could not locate settings region")
		}
		cmd := a.handleCenterPaneClick(originHit(a, sx, sy))
		if cmd == nil {
			t.Fatal("expected settings dialog command")
		}
		if _, ok := cmd().(messages.ShowSettingsDialog); !ok {
			t.Fatalf("want ShowSettingsDialog, got %T", cmd())
		}
	})

	t.Run("welcome screen routes add-project click", func(t *testing.T) {
		a := newCenterClickApp(t)
		a.showWelcome = true
		ax, ay, ok := welcomeHitPoint(t, a, "[Add project]")
		if !ok {
			t.Fatal("could not locate add-project region")
		}
		cmd := a.handleCenterPaneClick(originHit(a, ax, ay))
		if cmd == nil {
			t.Fatal("expected add-project dialog command")
		}
		if _, ok := cmd().(messages.ShowAddProjectDialog); !ok {
			t.Fatalf("want ShowAddProjectDialog, got %T", cmd())
		}
	})

	t.Run("active workspace routes new-agent click", func(t *testing.T) {
		a := newCenterClickApp(t)
		a.activeWorkspace = &data.Workspace{Name: "ws", Branch: "b", Root: "/r"}
		ax, ay, ok := workspaceHitPoint(t, a, "[New agent]")
		if !ok {
			t.Fatal("could not locate new-agent region")
		}
		cmd := a.handleCenterPaneClick(originHit(a, ax, ay))
		if cmd == nil {
			t.Fatal("expected new-agent dialog command")
		}
		if _, ok := cmd().(messages.ShowSelectAssistantDialog); !ok {
			t.Fatalf("want ShowSelectAssistantDialog, got %T", cmd())
		}
	})

	t.Run("welcome click missing every button returns nil", func(t *testing.T) {
		a := newCenterClickApp(t)
		a.showWelcome = true
		// Local origin (0,0) is above/left of the centered button row.
		if cmd := a.handleCenterPaneClick(originHit(a, 0, 0)); cmd != nil {
			t.Fatalf("expected nil command for empty-area welcome click, got %T", cmd())
		}
	})

	t.Run("no welcome and no workspace returns nil", func(t *testing.T) {
		a := newCenterClickApp(t)
		a.showWelcome = false
		a.activeWorkspace = nil
		if cmd := a.handleCenterPaneClick(originHit(a, 1, 1)); cmd != nil {
			t.Fatalf("expected nil command with neither welcome nor workspace, got %T", cmd())
		}
	})

	t.Run("click above content origin returns nil", func(t *testing.T) {
		// A Y above the content origin yields a negative localY which the handler
		// rejects before dispatching.
		a := newCenterClickApp(t)
		a.showWelcome = true
		cx, _ := a.centerPaneContentOrigin()
		if cmd := a.handleCenterPaneClick(tea.MouseClickMsg{Button: tea.MouseLeft, X: cx + 1, Y: 0}); cmd != nil {
			t.Fatalf("expected nil command for click above content origin, got %T", cmd())
		}
	})
}
