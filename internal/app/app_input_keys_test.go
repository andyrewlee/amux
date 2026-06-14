package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestHandleKeyboardEnhancements_StoresMsgAndReportsCapabilities(t *testing.T) {
	tests := []struct {
		name             string
		flags            int
		wantDisambiguate bool
		wantEventTypes   bool
	}{
		{
			name:             "no enhancements",
			flags:            0,
			wantDisambiguate: false,
			wantEventTypes:   false,
		},
		{
			name:             "disambiguation only",
			flags:            int(ansi.KittyDisambiguateEscapeCodes),
			wantDisambiguate: true,
			wantEventTypes:   false,
		},
		{
			name:             "event types implies disambiguation",
			flags:            int(ansi.KittyReportEventTypes),
			wantDisambiguate: true,
			wantEventTypes:   true,
		},
		{
			name:             "all kitty flags",
			flags:            int(ansi.KittyAllFlags),
			wantDisambiguate: true,
			wantEventTypes:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &App{}
			msg := tea.KeyboardEnhancementsMsg{Flags: tt.flags}

			app.handleKeyboardEnhancements(msg)

			if app.keyboardEnhancements.Flags != tt.flags {
				t.Fatalf("expected stored flags %d, got %d", tt.flags, app.keyboardEnhancements.Flags)
			}
			if got := app.keyboardEnhancements.SupportsKeyDisambiguation(); got != tt.wantDisambiguate {
				t.Fatalf("disambiguation: got %t want %t", got, tt.wantDisambiguate)
			}
			if got := app.keyboardEnhancements.SupportsEventTypes(); got != tt.wantEventTypes {
				t.Fatalf("event types: got %t want %t", got, tt.wantEventTypes)
			}
		})
	}
}

func TestHandleKeyboardEnhancements_OverwritesPreviousMsg(t *testing.T) {
	app := &App{keyboardEnhancements: tea.KeyboardEnhancementsMsg{Flags: int(ansi.KittyAllFlags)}}

	app.handleKeyboardEnhancements(tea.KeyboardEnhancementsMsg{Flags: 0})

	if app.keyboardEnhancements.Flags != 0 {
		t.Fatalf("expected later msg to overwrite earlier flags, got %d", app.keyboardEnhancements.Flags)
	}
	if app.keyboardEnhancements.SupportsKeyDisambiguation() {
		t.Fatal("expected disambiguation false after overwrite with empty flags")
	}
}

func TestHandleWindowSize_UpdatesDimensionsReadyAndLayout(t *testing.T) {
	h := newCenterHarness(nil, HarnessOptions{Width: 80, Height: 24, Tabs: 1})
	app := h.app

	const newWidth, newHeight = 140, 50
	app.handleWindowSize(tea.WindowSizeMsg{Width: newWidth, Height: newHeight})

	if app.width != newWidth {
		t.Fatalf("expected width %d, got %d", newWidth, app.width)
	}
	if app.height != newHeight {
		t.Fatalf("expected height %d, got %d", newHeight, app.height)
	}
	if !app.ready {
		t.Fatal("expected ready=true after window size")
	}
	// Layout must reflect the new geometry. Resize subtracts gutters, so the
	// layout height should be positive and at most the requested height.
	if lh := app.layout.Height(); lh <= 0 || lh > newHeight {
		t.Fatalf("expected layout height in (0,%d], got %d", newHeight, lh)
	}
}

func TestHandleWindowSize_ZeroDimensionsDoNotPanic(t *testing.T) {
	h := newCenterHarness(nil, HarnessOptions{Width: 80, Height: 24, Tabs: 1})
	app := h.app

	app.handleWindowSize(tea.WindowSizeMsg{Width: 0, Height: 0})

	if app.width != 0 || app.height != 0 {
		t.Fatalf("expected zeroed dimensions, got %dx%d", app.width, app.height)
	}
	if !app.ready {
		t.Fatal("expected ready=true even with zero dimensions")
	}
	if lh := app.layout.Height(); lh < 0 {
		t.Fatalf("expected non-negative layout height, got %d", lh)
	}
}

func TestHandlePaste_RoutesByFocusedPane(t *testing.T) {
	tests := []struct {
		name       string
		focused    messages.PaneType
		wantRouted bool
	}{
		{name: "center focused routes paste", focused: messages.PaneCenter, wantRouted: true},
		{name: "sidebar terminal focused routes paste", focused: messages.PaneSidebarTerminal, wantRouted: true},
		{name: "dashboard focused drops paste", focused: messages.PaneDashboard, wantRouted: false},
		{name: "sidebar focused drops paste", focused: messages.PaneSidebar, wantRouted: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newCenterHarness(nil, HarnessOptions{Width: 120, Height: 40, Tabs: 1})
			app := h.app
			// Ensure a sidebar terminal exists so the route target is live.
			app.sidebarTerminal.AddTerminalForHarness(harnessWorkspace())
			app.focusedPane = tt.focused

			centerBefore := app.center
			sidebarTermBefore := app.sidebarTerminal

			// Must not panic for any focused pane. The returned tea.Cmd is
			// intentionally discarded: routed handlers return a nil cmd in this
			// harness (no live PTY / focus flag), so this is a no-panic /
			// no-nil-out guard, not a non-nil-cmd assertion.
			_ = app.handlePaste(tea.PasteMsg{Content: "hello"})

			// Routed panes get a (possibly identical) model assigned back; the
			// pointers must remain non-nil regardless of route.
			if app.center == nil {
				t.Fatal("center model became nil after paste")
			}
			if app.sidebarTerminal == nil {
				t.Fatal("sidebar terminal model became nil after paste")
			}
			// Non-routed panes must leave both component pointers untouched.
			if !tt.wantRouted {
				if app.center != centerBefore {
					t.Fatal("expected center model untouched when not focused")
				}
				if app.sidebarTerminal != sidebarTermBefore {
					t.Fatal("expected sidebar terminal untouched when not focused")
				}
			}
		})
	}
}

func TestHandlePaste_EmptyContentDoesNotPanic(t *testing.T) {
	h := newCenterHarness(nil, HarnessOptions{Width: 120, Height: 40, Tabs: 1})
	app := h.app
	app.focusedPane = messages.PaneCenter

	// Empty paste content must be handled gracefully.
	_ = app.handlePaste(tea.PasteMsg{Content: ""})

	if app.center == nil {
		t.Fatal("center model became nil after empty paste")
	}
}

func TestHandlePrefixTimeout_ExitsOnMatchingTokenWhileActive(t *testing.T) {
	app, _, _ := newPrefixTestApp(t)
	app.prefixActive = true
	app.prefixToken = 7
	app.prefixSequence = []string{"t"}

	app.handlePrefixTimeout(prefixTimeoutMsg{token: 7})

	if app.prefixActive {
		t.Fatal("expected prefix mode to exit on matching token")
	}
	if app.prefixSequence != nil {
		t.Fatalf("expected prefix sequence cleared on exit, got %v", app.prefixSequence)
	}
}

func TestHandlePrefixTimeout_StaleTokenIgnored(t *testing.T) {
	app, _, _ := newPrefixTestApp(t)
	app.prefixActive = true
	app.prefixToken = 9
	app.prefixSequence = []string{"t"}

	// A timeout fired for an earlier (superseded) token must be ignored.
	app.handlePrefixTimeout(prefixTimeoutMsg{token: 8})

	if !app.prefixActive {
		t.Fatal("expected prefix mode to stay active for stale token")
	}
	if len(app.prefixSequence) != 1 || app.prefixSequence[0] != "t" {
		t.Fatalf("expected prefix sequence preserved, got %v", app.prefixSequence)
	}
}

func TestHandlePrefixTimeout_InactivePrefixIgnored(t *testing.T) {
	app, _, _ := newPrefixTestApp(t)
	app.prefixActive = false
	app.prefixToken = 3

	// Even a matching token must be a no-op when prefix mode is already inactive.
	app.handlePrefixTimeout(prefixTimeoutMsg{token: 3})

	if app.prefixActive {
		t.Fatal("expected prefix mode to remain inactive")
	}
}

func TestCenterButtonCount(t *testing.T) {
	tests := []struct {
		name         string
		showWelcome  bool
		hasWorkspace bool
		want         int
	}{
		{name: "welcome screen shows add-project and settings", showWelcome: true, want: 2},
		{name: "welcome takes precedence over active workspace", showWelcome: true, hasWorkspace: true, want: 2},
		{name: "active workspace shows new-agent button", hasWorkspace: true, want: 1},
		{name: "no welcome and no workspace shows no buttons", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &App{showWelcome: tt.showWelcome}
			if tt.hasWorkspace {
				app.activeWorkspace = &data.Workspace{Name: "ws"}
			}

			if got := app.centerButtonCount(); got != tt.want {
				t.Fatalf("centerButtonCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestActivateCenterButton_Welcome(t *testing.T) {
	tests := []struct {
		name    string
		index   int
		wantCmd bool
		wantMsg tea.Msg
	}{
		{name: "index 0 opens add-project dialog", index: 0, wantCmd: true, wantMsg: messages.ShowAddProjectDialog{}},
		{name: "index 1 opens settings dialog", index: 1, wantCmd: true, wantMsg: messages.ShowSettingsDialog{}},
		{name: "out-of-range index returns nil", index: 2, wantCmd: false},
		{name: "negative index returns nil", index: -1, wantCmd: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &App{showWelcome: true, centerBtnIndex: tt.index}

			cmd := app.activateCenterButton()
			if !tt.wantCmd {
				if cmd != nil {
					t.Fatalf("expected nil command for index %d", tt.index)
				}
				return
			}
			if cmd == nil {
				t.Fatalf("expected command for index %d", tt.index)
			}
			got := cmd()
			if got != tt.wantMsg {
				t.Fatalf("activateCenterButton() msg = %T(%v), want %T(%v)", got, got, tt.wantMsg, tt.wantMsg)
			}
		})
	}
}

func TestActivateCenterButton_ActiveWorkspaceOpensAssistantDialog(t *testing.T) {
	app := &App{
		showWelcome:     false,
		activeWorkspace: &data.Workspace{Name: "ws"},
		// Index is irrelevant once a workspace is active: there is a single button.
		centerBtnIndex: 0,
	}

	cmd := app.activateCenterButton()
	if cmd == nil {
		t.Fatal("expected command for active workspace button")
	}
	if _, ok := cmd().(messages.ShowSelectAssistantDialog); !ok {
		t.Fatalf("expected ShowSelectAssistantDialog, got %T", cmd())
	}
}

func TestActivateCenterButton_NoWelcomeNoWorkspaceReturnsNil(t *testing.T) {
	app := &App{showWelcome: false, activeWorkspace: nil}

	if cmd := app.activateCenterButton(); cmd != nil {
		t.Fatalf("expected nil command with no welcome and no workspace, got %v", cmd())
	}
}
