package sidebar

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// flushTimingFor returns the flush timing for a specific terminal. Callers must
// pass the terminal they are flushing, not the selected one: background
// terminals keep streaming, and timing them by whatever happens
// to be on screen gives an alt-screen terminal the faster non-alt cadence — and
// the flush policy reads the quiet period to decide whether a caller asked for
// a slower cadence (see ptyio.FlushDelay).
func (m *TerminalModel) flushTimingFor(ts *TerminalState) (time.Duration, time.Duration) {
	if ts == nil {
		return ptyFlushQuiet, ptyFlushMaxInterval
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	return m.flushTimingForLocked(ts)
}

// flushTimingForLocked is flushTimingFor for callers already holding ts.mu —
// the flush handler keeps one hold across the whole ptyio.State critical
// section now that the writer goroutine also mutates State under ts.mu.
func (m *TerminalModel) flushTimingForLocked(ts *TerminalState) (time.Duration, time.Duration) {
	// Only use slower Alt timing for true AltScreen mode (full-screen TUIs).
	if ts.VTerm != nil && ts.VTerm.AltScreen {
		return ptyFlushQuietAlt, ptyFlushMaxAlt
	}
	return ptyFlushQuiet, ptyFlushMaxInterval
}

// Init initializes the terminal model
func (m *TerminalModel) Init() tea.Cmd {
	return nil
}

// sidebarClipboardCopy is the off-loop OS-clipboard seam — the actual exec
// stays outside every lock. Tests stub it so a completion never touches the
// real user clipboard.
var sidebarClipboardCopy = common.CopyToClipboardWithLog

// drainSidebarClipboard copies a captured OSC52 payload to the OS clipboard
// off the update path — the same drain the flush fallback and the
// stream-lifecycle completions use. The OSC52 opt-in check happens inside
// OSC52ClipboardText.
func (m *TerminalModel) drainSidebarClipboard(clip []byte) {
	if text, ok := common.OSC52ClipboardText(clip); ok {
		safego.Go("sidebar.osc52_clipboard", func() {
			sidebarClipboardCopy(text, "agent OSC52 (sidebar)")
		})
	}
}

func (m *TerminalModel) Update(msg tea.Msg) (*TerminalModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg)
	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg)
	case messages.SidebarSelectionScrollTick:
		if cmd := m.handleSelectionScrollTick(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)
	case tea.PasteMsg:
		return m.handlePaste(msg)
	case tea.KeyPressMsg:
		return m.handleKeyPress(msg)

	case messages.SidebarPTYOutput:
		if cmd := m.handlePTYOutput(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.SidebarPTYFlush:
		if cmd := m.handlePTYFlush(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.SidebarPTYStopped:
		if cmd := m.handlePTYStopped(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.SidebarPTYRestart:
		if cmd := m.handlePTYRestart(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case SidebarTabWritten:
		// The writer goroutine captured an OSC52 clipboard payload — drain it
		// off-loop exactly once per write, same as the pre-actor path, but
		// only while the completion's stream is still current: a completion
		// stamped under a replaced epoch must have no clipboard side effects.
		tab, _ := m.resolveTabForResult(msg.WorkspaceID, TerminalTabID(msg.TabID), "sidebar write result")
		if tab == nil || tab.State == nil {
			break
		}
		ts := tab.State
		ts.mu.Lock()
		current := msg.epoch != 0 && ts.writeEpoch == msg.epoch
		ts.mu.Unlock()
		if !current {
			break
		}
		m.drainSidebarClipboard(msg.clip)

	case SidebarInputFailed:
		m.handleSidebarInputFailed(msg)

	case SidebarTerminalCreated:
		if cmd := m.handleTerminalCreated(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case SidebarTerminalReattachResult:
		if cmd := m.handleReattachResult(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case SidebarTerminalReattachFailed:
		if cmd := m.handleReattachFailed(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case SidebarTerminalCreateFailed:
		if cmd := m.handleCreateFailed(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.WorkspaceDeleted:
		if cmd := m.handleWorkspaceDeleted(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.WorkspaceShelved:
		if cmd := m.handleWorkspaceShelved(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, common.SafeBatch(cmds...)
}

// handleMouseWheel scrolls the sidebar terminal viewport.
func (m *TerminalModel) handleMouseWheel(msg tea.MouseWheelMsg) (*TerminalModel, tea.Cmd) {
	if !m.focused {
		return m, nil
	}
	ts := m.getTerminal()
	if ts == nil || ts.VTerm == nil {
		return m, nil
	}
	ts.mu.Lock()
	delta := common.ScrollDeltaForHeight(ts.VTerm.Height, 8) // ~12.5% of viewport
	if msg.Button == tea.MouseWheelUp {
		ts.VTerm.ScrollViewAndNote(delta)
	} else if msg.Button == tea.MouseWheelDown {
		ts.VTerm.ScrollViewAndNote(-delta)
	}
	ts.mu.Unlock()
	return m, nil
}

// handlePaste forwards a bracketed paste to the sidebar terminal.
func (m *TerminalModel) handlePaste(msg tea.PasteMsg) (*TerminalModel, tea.Cmd) {
	if !m.focused {
		return m, nil
	}
	ts := m.getTerminal()
	if ts == nil || ts.Terminal == nil {
		return m, nil
	}

	// Handle bracketed paste - send entire content at once with escape sequences.
	// Admission only: the binding's writer goroutine owns SendString, so a
	// stalled PTY can never park Update on ptyFile.Write.
	text := msg.Content
	bracketedText := ansi.BracketedPasteStart + text + ansi.BracketedPasteEnd
	tab := m.getActiveTab()
	var tabID TerminalTabID
	if tab != nil {
		tabID = tab.ID
	}
	if res := m.admitSidebarInput(ts, tabID, bracketedText, "paste"); res == sidebarInputRejectedFull {
		m.surfaceSidebarInputRejection(ts, tabID, "input queue full")
	} else if res == sidebarInputAdmitted {
		logging.Debug("Sidebar terminal pasted %d bytes via bracketed paste", len(text))
	}
	return m, nil
}

// handleKeyPress handles Cmd+C copy, PgUp/PgDown scrollback, and forwards all
// other keys to the terminal. It always returns (m, nil) — the original case had
// no trailing return and fell through to an empty batch.
func (m *TerminalModel) handleKeyPress(msg tea.KeyPressMsg) (*TerminalModel, tea.Cmd) {
	if !m.focused {
		return m, nil
	}

	ts := m.getTerminal()
	if ts == nil || ts.Terminal == nil {
		return m, nil
	}

	// Check if this is Cmd+C (copy command)
	k := msg.Key()
	isCopyKey := k.Mod.Contains(tea.ModSuper) && k.Code == 'c'

	// Handle explicit Cmd+C to copy current selection
	if isCopyKey {
		ts.mu.Lock()
		text := ""
		if ts.VTerm != nil && ts.VTerm.HasSelection() {
			text = ts.VTerm.SelectedText()
		}
		ts.mu.Unlock()
		common.CopyToClipboardWithLog(text, "Cmd+C sidebar")
		return m, nil // Don't forward to terminal, don't clear selection
	}

	// PgUp/PgDown for scrollback (these don't conflict with embedded TUIs)
	switch msg.Key().Code {
	case tea.KeyPgUp:
		ts.mu.Lock()
		if ts.VTerm != nil {
			ts.VTerm.ScrollViewAndNote(common.ScrollDeltaForHeight(ts.VTerm.Height, 2))
		}
		ts.mu.Unlock()
		return m, nil

	case tea.KeyPgDown:
		ts.mu.Lock()
		if ts.VTerm != nil {
			ts.VTerm.ScrollViewAndNote(-common.ScrollDeltaForHeight(ts.VTerm.Height, 2))
		}
		ts.mu.Unlock()
		return m, nil
	}

	// If scrolled, any typing goes back to live and sends key
	ts.mu.Lock()
	if ts.VTerm != nil && ts.VTerm.IsScrolled() {
		ts.VTerm.ScrollViewToBottom()
		ts.VTerm.NoteSyncViewportInteraction()
	}
	ts.mu.Unlock()

	// Forward ALL keys to terminal (no Ctrl interceptions). Same admission
	// contract as paste: Update never writes to the PTY itself.
	input := common.KeyToBytes(msg)
	if len(input) > 0 {
		tab := m.getActiveTab()
		var tabID TerminalTabID
		if tab != nil {
			tabID = tab.ID
		}
		if res := m.admitSidebarInput(ts, tabID, string(input), "key"); res == sidebarInputRejectedFull {
			m.surfaceSidebarInputRejection(ts, tabID, "input queue full")
		}
	}
	return m, nil
}
