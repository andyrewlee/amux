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

// scrollableTermModel returns a model focused on a single tab whose VTerm has
// `lines` of scrollback already written. The viewport is one row tall so the
// wheel scroll delta is exactly one line, which keeps the offset assertions
// exact. No PTY/tmux process is involved.
func scrollableTermModel(t *testing.T, focused bool, lines int) (*TerminalModel, *TerminalState) {
	t.Helper()
	ws := &data.Workspace{Name: "ws", Repo: "/repo/ws", Root: "/repo/ws"}
	term := vterm.New(80, 1)
	for i := 0; i < lines; i++ {
		term.Write([]byte("line\n"))
	}
	state := &TerminalState{VTerm: term}
	tab := &TerminalTab{ID: generateTerminalTabID(), Name: "Terminal 1", State: state}

	m := NewTerminalModel()
	m.workspace = ws
	m.focused = focused
	m.tabs.ByWorkspace[string(ws.ID())] = []*TerminalTab{tab}
	m.tabs.ActiveByWorkspace[string(ws.ID())] = 0
	return m, state
}

func wheelMsg(button tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg{Button: button}
}

// --- Init ---------------------------------------------------------------

func TestInitReturnsNilCmd(t *testing.T) {
	// Init has no startup work to schedule; it must return a nil command and
	// must not mutate the model.
	m := NewTerminalModel()
	m.focused = true
	if cmd := m.Init(); cmd != nil {
		t.Fatalf("expected Init to return a nil command, got %T", cmd)
	}
	if !m.focused {
		t.Fatal("expected Init to leave model state untouched")
	}
}

func TestInitReturnsNilOnZeroValueModel(t *testing.T) {
	// Even a freshly zero-valued model (no workspace, no tabs) must yield a nil
	// command rather than panic.
	var m TerminalModel
	if cmd := m.Init(); cmd != nil {
		t.Fatalf("expected nil command from zero-value model Init, got %T", cmd)
	}
}

// --- handleMouseWheel ---------------------------------------------------

func TestHandleMouseWheelUnfocusedIsNoop(t *testing.T) {
	// When the sidebar terminal is not focused, wheel events must be ignored so
	// they don't steal scroll from the focused pane.
	m, state := scrollableTermModel(t, false, 10)

	gotM, cmd := m.handleMouseWheel(wheelMsg(tea.MouseWheelUp))

	if gotM != m {
		t.Fatal("expected the same model returned")
	}
	if cmd != nil {
		t.Fatal("expected nil command when unfocused")
	}
	state.mu.Lock()
	offset, _ := state.VTerm.GetScrollInfo()
	state.mu.Unlock()
	if offset != 0 {
		t.Fatalf("expected unfocused wheel to leave offset at 0, got %d", offset)
	}
}

func TestHandleMouseWheelNoTerminalIsNoop(t *testing.T) {
	// Focused but with no active terminal/VTerm: the nil guards must short-circuit
	// without panicking.
	tests := []struct {
		name  string
		setup func() *TerminalModel
	}{
		{
			name: "no tabs at all",
			setup: func() *TerminalModel {
				m := NewTerminalModel()
				m.workspace = &data.Workspace{Repo: "/repo", Root: "/repo/ws"}
				m.focused = true
				return m
			},
		},
		{
			name: "active tab has nil VTerm",
			setup: func() *TerminalModel {
				ws := &data.Workspace{Repo: "/repo", Root: "/repo/ws"}
				m := NewTerminalModel()
				m.workspace = ws
				m.focused = true
				m.tabs.ByWorkspace[string(ws.ID())] = []*TerminalTab{
					{ID: generateTerminalTabID(), Name: "Terminal 1", State: &TerminalState{}},
				}
				m.tabs.ActiveByWorkspace[string(ws.ID())] = 0
				return m
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setup()
			gotM, cmd := m.handleMouseWheel(wheelMsg(tea.MouseWheelUp))
			if gotM != m {
				t.Fatal("expected the same model returned")
			}
			if cmd != nil {
				t.Fatal("expected nil command when there is no terminal to scroll")
			}
		})
	}
}

func TestHandleMouseWheelUpScrollsIntoScrollback(t *testing.T) {
	// A wheel-up on a focused terminal with scrollback must increase the view
	// offset (scroll back in history). One-row viewport -> delta of 1 line.
	m, state := scrollableTermModel(t, true, 10)

	gotM, cmd := m.handleMouseWheel(wheelMsg(tea.MouseWheelUp))

	if gotM != m {
		t.Fatal("expected the same model returned")
	}
	if cmd != nil {
		t.Fatal("expected handleMouseWheel to return a nil command")
	}
	state.mu.Lock()
	offset, _ := state.VTerm.GetScrollInfo()
	scrolled := state.VTerm.IsScrolled()
	state.mu.Unlock()
	if offset != 1 {
		t.Fatalf("expected wheel-up to scroll by 1 line, got offset %d", offset)
	}
	if !scrolled {
		t.Fatal("expected IsScrolled() true after scrolling up")
	}
}

func TestHandleMouseWheelDownReturnsToLive(t *testing.T) {
	// After scrolling up, a wheel-down must decrease the offset back toward the
	// live (bottom) view. Two wheel-ups then one wheel-down -> offset 1.
	m, state := scrollableTermModel(t, true, 10)
	m.handleMouseWheel(wheelMsg(tea.MouseWheelUp))
	m.handleMouseWheel(wheelMsg(tea.MouseWheelUp))

	state.mu.Lock()
	before, _ := state.VTerm.GetScrollInfo()
	state.mu.Unlock()
	if before != 2 {
		t.Fatalf("precondition: expected offset 2 after two wheel-ups, got %d", before)
	}

	m.handleMouseWheel(wheelMsg(tea.MouseWheelDown))

	state.mu.Lock()
	after, _ := state.VTerm.GetScrollInfo()
	state.mu.Unlock()
	if after != 1 {
		t.Fatalf("expected wheel-down to drop offset to 1, got %d", after)
	}
}

func TestHandleMouseWheelDownClampsAtLive(t *testing.T) {
	// A wheel-down while already at the live view must not push the offset below
	// zero; ScrollView clamps it.
	m, state := scrollableTermModel(t, true, 10)

	m.handleMouseWheel(wheelMsg(tea.MouseWheelDown))

	state.mu.Lock()
	offset, _ := state.VTerm.GetScrollInfo()
	scrolled := state.VTerm.IsScrolled()
	state.mu.Unlock()
	if offset != 0 {
		t.Fatalf("expected offset clamped at 0 on wheel-down from live, got %d", offset)
	}
	if scrolled {
		t.Fatal("expected IsScrolled() false when clamped at the live view")
	}
}

func TestHandleMouseWheelUpClampsAtScrollbackTop(t *testing.T) {
	// Scrolling up past the top of scrollback must clamp at the maximum offset
	// rather than running away unboundedly.
	m, state := scrollableTermModel(t, true, 5)

	state.mu.Lock()
	_, maxOffset := state.VTerm.GetScrollInfo()
	state.mu.Unlock()

	// Many more wheel-ups than there are scrollback lines.
	for i := 0; i < 50; i++ {
		m.handleMouseWheel(wheelMsg(tea.MouseWheelUp))
	}

	state.mu.Lock()
	offset, _ := state.VTerm.GetScrollInfo()
	state.mu.Unlock()
	if offset != maxOffset {
		t.Fatalf("expected offset clamped at max %d, got %d", maxOffset, offset)
	}
}

func TestHandleMouseWheelIgnoresNonWheelButton(t *testing.T) {
	// Only the up/down wheel buttons move the viewport. A different button (e.g.
	// a middle/none button delivered as a wheel msg) must leave the offset put.
	m, state := scrollableTermModel(t, true, 10)

	gotM, cmd := m.handleMouseWheel(wheelMsg(tea.MouseNone))

	if gotM != m || cmd != nil {
		t.Fatal("expected (m, nil) for a non-up/down wheel button")
	}
	state.mu.Lock()
	offset, _ := state.VTerm.GetScrollInfo()
	state.mu.Unlock()
	if offset != 0 {
		t.Fatalf("expected offset unchanged for a non-wheel button, got %d", offset)
	}
}

func TestHandleMouseWheelRoutedThroughUpdate(t *testing.T) {
	// Update must dispatch MouseWheelMsg to handleMouseWheel; routing a wheel-up
	// through Update produces the same scroll effect as calling the handler.
	m, state := scrollableTermModel(t, true, 10)

	_, _ = m.Update(wheelMsg(tea.MouseWheelUp))

	state.mu.Lock()
	offset, _ := state.VTerm.GetScrollInfo()
	state.mu.Unlock()
	if offset != 1 {
		t.Fatalf("expected Update to route wheel-up into a 1-line scroll, got %d", offset)
	}
}

// --- handlePaste --------------------------------------------------------

func TestHandlePasteUnfocusedIsNoop(t *testing.T) {
	// Paste into an unfocused sidebar terminal must be ignored, leaving the
	// terminal attached.
	m, state := scrollableTermModel(t, false, 0)
	state.Terminal = &appPty.Terminal{}

	gotM, cmd := m.handlePaste(tea.PasteMsg{Content: "hello"})

	if gotM != m || cmd != nil {
		t.Fatal("expected (m, nil) when unfocused")
	}
	state.mu.Lock()
	detached := state.Detached
	term := state.Terminal
	state.mu.Unlock()
	if detached {
		t.Fatal("expected the terminal to stay attached when paste is ignored")
	}
	if term == nil {
		t.Fatal("expected the Terminal to remain set when paste is ignored")
	}
}

func TestHandlePasteNoTerminalIsNoop(t *testing.T) {
	// Focused but with no live Terminal: the nil guards short-circuit without
	// panicking and without detaching anything.
	tests := []struct {
		name  string
		setup func() *TerminalModel
	}{
		{
			name: "no tabs",
			setup: func() *TerminalModel {
				m := NewTerminalModel()
				m.workspace = &data.Workspace{Repo: "/repo", Root: "/repo/ws"}
				m.focused = true
				return m
			},
		},
		{
			name: "active tab has nil Terminal",
			setup: func() *TerminalModel {
				m, _ := scrollableTermModel(t, true, 0)
				return m
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setup()
			gotM, cmd := m.handlePaste(tea.PasteMsg{Content: "hello"})
			if gotM != m || cmd != nil {
				t.Fatal("expected (m, nil) when there is no terminal to paste into")
			}
		})
	}
}

func TestHandlePasteWriteFailureDetaches(t *testing.T) {
	// A zero-value *pty.Terminal has no underlying PTY file, so SendString
	// returns an error. handlePaste must react by detaching the terminal (the
	// not-user-initiated branch) rather than swallowing the failure.
	tests := []struct {
		name    string
		content string
	}{
		{name: "plain text", content: "echo hi"},
		{name: "empty paste", content: ""},
		{name: "multiline paste", content: "line1\nline2\nline3"},
		{name: "embedded paste markers", content: "a\x1b[200~b\x1b[201~c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, state := scrollableTermModel(t, true, 0)
			state.Terminal = &appPty.Terminal{}
			state.Running = true

			gotM, cmd := m.handlePaste(tea.PasteMsg{Content: tt.content})

			if gotM != m || cmd != nil {
				t.Fatal("expected (m, nil) from handlePaste")
			}
			state.mu.Lock()
			detached := state.Detached
			running := state.Running
			userDetached := state.UserDetached
			term := state.Terminal
			state.mu.Unlock()
			if !detached {
				t.Fatal("expected a write failure to mark the terminal detached")
			}
			if running {
				t.Fatal("expected Running cleared after a write failure")
			}
			if userDetached {
				t.Fatal("expected the failure detach to be non-user-initiated")
			}
			if term != nil {
				t.Fatal("expected the closed Terminal to be cleared on detach")
			}
		})
	}
}

func TestHandlePasteRoutedThroughUpdate(t *testing.T) {
	// Update must dispatch PasteMsg to handlePaste; routing a paste whose write
	// fails detaches the terminal exactly as the direct handler call does.
	m, state := scrollableTermModel(t, true, 0)
	state.Terminal = &appPty.Terminal{}
	state.Running = true

	_, _ = m.Update(tea.PasteMsg{Content: "payload"})

	state.mu.Lock()
	detached := state.Detached
	state.mu.Unlock()
	if !detached {
		t.Fatal("expected Update to route the paste and detach on write failure")
	}
}
