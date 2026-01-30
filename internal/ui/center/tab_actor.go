package center

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/perf"
)

type tabEventKind int

const (
	tabEventSelectionClear tabEventKind = iota
	tabEventSelectionStart
	tabEventSelectionUpdate
	tabEventSelectionFinish
	tabEventScrollBy
	tabEventSelectionClearAndNotify
	tabEventSelectionScrollTick
	tabEventSelectionCopy
	tabEventScrollToBottom
	tabEventScrollPage
	tabEventScrollToTop
	tabEventDiffInput
	tabEventSendInput
	tabEventPaste
	tabEventSendResponse
	tabEventWriteOutput
)

type tabEvent struct {
	tab         *Tab
	workspaceID string
	tabID       TabID
	kind        tabEventKind
	termX       int
	termY       int
	inBounds    bool
	delta       int
	gen         uint64
	notifyCopy  bool
	scrollPage  int
	diffMsg     tea.Msg
	input       []byte
	pasteText   string
	response    []byte
	output      []byte
}

type tabSelectionResult struct {
	workspaceID string
	tabID       TabID
	clipboard   string
}

type selectionTickRequest struct {
	workspaceID string
	tabID       TabID
	gen         uint64
}

type tabActorReady struct{}

type tabActorHeartbeat struct{}

type tabDiffCmd struct {
	cmd tea.Cmd
}

// TabInputFailed is sent when input to the PTY fails (e.g., after sleep)
type TabInputFailed struct {
	TabID       TabID
	WorkspaceID string
	Err         error
}

func (m *Model) sendTabEvent(ev tabEvent) bool {
	if m == nil || m.tabEvents == nil {
		return false
	}
	if ev.tab == nil {
		perf.Count("tab_event_drop_missing", 1)
		return false
	}
	if ev.tab != nil && ev.tab.isClosed() {
		perf.Count("tab_event_drop_closed", 1)
		return true
	}
	if shouldDropTabEvent(m.tabEvents, ev.kind) {
		perf.Count("tab_event_drop_backpressure", 1)
		return false
	}
	select {
	case m.tabEvents <- ev:
		return true
	default:
		perf.Count("tab_event_drop", 1)
	}
	return false
}

func shouldDropTabEvent(ch chan tabEvent, kind tabEventKind) bool {
	if ch == nil {
		return true
	}
	switch kind {
	case tabEventSelectionUpdate, tabEventSelectionScrollTick, tabEventScrollBy, tabEventScrollPage:
	default:
		return false
	}
	capacity := cap(ch)
	if capacity == 0 {
		return false
	}
	return len(ch) >= (capacity*3)/4
}

func (m *Model) RunTabActor(ctx context.Context) error {
	if m == nil || m.tabEvents == nil {
		return nil
	}
	if m.msgSink != nil {
		m.msgSink(tabActorReady{})
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	if m.msgSink != nil {
		m.msgSink(tabActorHeartbeat{})
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-m.tabEvents:
			if m.msgSink != nil {
				m.msgSink(tabActorHeartbeat{})
			}
			m.handleTabEvent(ev)
		case <-ticker.C:
			if m.msgSink != nil {
				m.msgSink(tabActorHeartbeat{})
			}
		}
	}
}

func (m *Model) handleTabEvent(ev tabEvent) {
	if ev.tab == nil || ev.tab.isClosed() {
		perf.Count("tab_event_drop_missing", 1)
		return
	}
	tab := ev.tab

	switch ev.kind {
	case tabEventSelectionClear:
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ClearSelection()
		}
		tab.Selection = SelectionState{}
		tab.selectionScrollDir = 0
		tab.selectionScrollActive = false
		tab.selectionGen++
		tab.mu.Unlock()
	case tabEventSelectionClearAndNotify:
		tab.mu.Lock()
		text := ""
		if ev.notifyCopy && tab.Terminal != nil && tab.Terminal.HasSelection() {
			text = tab.Terminal.GetSelectedText(
				tab.Terminal.SelStartX(), tab.Terminal.SelStartLine(),
				tab.Terminal.SelEndX(), tab.Terminal.SelEndLine(),
			)
		}
		if tab.Terminal != nil {
			tab.Terminal.ClearSelection()
		}
		tab.Selection = SelectionState{}
		tab.selectionScrollDir = 0
		tab.selectionScrollActive = false
		tab.selectionGen++
		tab.mu.Unlock()
		if ev.notifyCopy && text != "" && m.msgSink != nil {
			m.msgSink(tabSelectionResult{workspaceID: ev.workspaceID, tabID: ev.tabID, clipboard: text})
		}
	case tabEventSelectionCopy:
		tab.mu.Lock()
		text := ""
		if ev.notifyCopy && tab.Terminal != nil && tab.Terminal.HasSelection() {
			text = tab.Terminal.GetSelectedText(
				tab.Terminal.SelStartX(), tab.Terminal.SelStartLine(),
				tab.Terminal.SelEndX(), tab.Terminal.SelEndLine(),
			)
		}
		tab.mu.Unlock()
		if ev.notifyCopy && text != "" && m.msgSink != nil {
			m.msgSink(tabSelectionResult{workspaceID: ev.workspaceID, tabID: ev.tabID, clipboard: text})
		}
	case tabEventSelectionStart:
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ClearSelection()
		}
		tab.Selection = SelectionState{}
		tab.selectionGen++
		tab.selectionScrollDir = 0
		tab.selectionScrollActive = false
		if ev.inBounds && tab.Terminal != nil {
			absLine := tab.Terminal.ScreenYToAbsoluteLine(ev.termY)
			tab.Selection = SelectionState{
				Active:    true,
				StartX:    ev.termX,
				StartLine: absLine,
				EndX:      ev.termX,
				EndLine:   absLine,
			}
			tab.Terminal.SetSelection(ev.termX, absLine, ev.termX, absLine, true, false)
		}
		tab.mu.Unlock()
	case tabEventSelectionUpdate:
		tab.mu.Lock()
		defer tab.mu.Unlock()
		if !tab.Selection.Active || tab.Terminal == nil {
			return
		}
		termWidth := tab.Terminal.Width
		termHeight := tab.Terminal.Height
		termX := ev.termX
		termY := ev.termY

		if termX < 0 {
			termX = 0
		}
		if termX >= termWidth {
			termX = termWidth - 1
		}

		scrollDir := 0
		if termY < 0 {
			tab.Terminal.ScrollView(1)
			scrollDir = 1
			termY = 0
		} else if termY >= termHeight {
			tab.Terminal.ScrollView(-1)
			scrollDir = -1
			termY = termHeight - 1
		}
		tab.selectionScrollDir = scrollDir

		absLine := tab.Terminal.ScreenYToAbsoluteLine(termY)
		startX := tab.Terminal.SelStartX()
		startLine := tab.Terminal.SelStartLine()
		if !tab.Terminal.HasSelection() {
			startX = tab.Selection.StartX
			startLine = tab.Selection.StartLine
		}
		tab.Selection.EndX = termX
		tab.Selection.EndLine = absLine
		tab.Terminal.SetSelection(startX, startLine, termX, absLine, true, false)
		tab.Selection.StartX = startX
		tab.Selection.StartLine = startLine
		tab.selectionGen++
		if tab.Selection.Active && tab.selectionScrollDir != 0 && !tab.selectionScrollActive && m.msgSink != nil {
			tab.selectionScrollActive = true
			m.msgSink(selectionTickRequest{
				workspaceID: ev.workspaceID,
				tabID:       ev.tabID,
				gen:         tab.selectionGen,
			})
		}
	case tabEventSelectionFinish:
		tab.mu.Lock()
		defer tab.mu.Unlock()
		if !tab.Selection.Active {
			return
		}
		tab.Selection.Active = false
		tab.selectionScrollDir = 0
		tab.selectionScrollActive = false
		tab.selectionGen++
		if tab.Terminal != nil &&
			(tab.Selection.StartX != tab.Selection.EndX ||
				tab.Selection.StartLine != tab.Selection.EndLine) {
			text := tab.Terminal.GetSelectedText(
				tab.Terminal.SelStartX(), tab.Terminal.SelStartLine(),
				tab.Terminal.SelEndX(), tab.Terminal.SelEndLine(),
			)
			if text != "" && m.msgSink != nil {
				m.msgSink(tabSelectionResult{workspaceID: ev.workspaceID, tabID: ev.tabID, clipboard: text})
			}
		}
	case tabEventScrollBy:
		tab.mu.Lock()
		if tab.Terminal != nil && ev.delta != 0 {
			tab.Terminal.ScrollView(ev.delta)
			tab.monitorDirty = true
		}
		tab.mu.Unlock()
	case tabEventSelectionScrollTick:
		tab.mu.Lock()
		if !tab.Selection.Active || tab.Terminal == nil || tab.selectionGen != ev.gen || tab.selectionScrollDir == 0 || !tab.selectionScrollActive {
			tab.selectionScrollActive = false
			tab.mu.Unlock()
			return
		}
		tab.Terminal.ScrollView(tab.selectionScrollDir)
		tab.monitorDirty = true
		tab.mu.Unlock()
		if m.msgSink != nil {
			m.msgSink(selectionTickRequest{
				workspaceID: ev.workspaceID,
				tabID:       ev.tabID,
				gen:         ev.gen,
			})
		}
	case tabEventScrollToBottom:
		tab.mu.Lock()
		if tab.Terminal != nil && tab.Terminal.IsScrolled() {
			tab.Terminal.ScrollViewToBottom()
			tab.monitorDirty = true
		}
		tab.mu.Unlock()
	case tabEventScrollPage:
		tab.mu.Lock()
		if tab.Terminal != nil && ev.scrollPage != 0 {
			delta := tab.Terminal.Height / 4
			if delta < 1 {
				delta = 1
			}
			tab.Terminal.ScrollView(delta * ev.scrollPage)
			tab.monitorDirty = true
		}
		tab.mu.Unlock()
	case tabEventScrollToTop:
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollViewToTop()
			tab.monitorDirty = true
		}
		tab.mu.Unlock()
	case tabEventDiffInput:
		tab.mu.Lock()
		dv := tab.DiffViewer
		if dv == nil {
			tab.mu.Unlock()
			return
		}
		newDV, cmd := dv.Update(ev.diffMsg)
		tab.DiffViewer = newDV
		tab.mu.Unlock()
		if cmd != nil && m.msgSink != nil {
			m.msgSink(tabDiffCmd{cmd: cmd})
		}
	case tabEventSendInput:
		if tab == nil || tab.Agent == nil || tab.Agent.Terminal == nil {
			return
		}
		if len(ev.input) == 0 {
			return
		}
		if err := tab.Agent.Terminal.SendString(string(ev.input)); err != nil {
			logging.Warn("Input failed for tab %s: %v", tab.ID, err)
			tab.mu.Lock()
			tab.Running = false
			tab.Detached = true
			tab.mu.Unlock()
			if m.msgSink != nil {
				m.msgSink(TabInputFailed{TabID: ev.tabID, WorkspaceID: ev.workspaceID, Err: err})
			}
		}
	case tabEventPaste:
		if tab == nil || tab.Agent == nil || tab.Agent.Terminal == nil {
			return
		}
		if ev.pasteText == "" {
			return
		}
		bracketedText := "\x1b[200~" + ev.pasteText + "\x1b[201~"
		if err := tab.Agent.Terminal.SendString(bracketedText); err != nil {
			logging.Warn("Paste failed for tab %s: %v", tab.ID, err)
			tab.mu.Lock()
			tab.Running = false
			tab.Detached = true
			tab.mu.Unlock()
			if m.msgSink != nil {
				m.msgSink(TabInputFailed{TabID: ev.tabID, WorkspaceID: ev.workspaceID, Err: err})
			}
		}
	case tabEventSendResponse:
		if tab == nil || tab.Agent == nil || tab.Agent.Terminal == nil {
			return
		}
		if len(ev.response) == 0 {
			return
		}
		if err := tab.Agent.Terminal.SendString(string(ev.response)); err != nil {
			logging.Warn("Response send failed for tab %s: %v", tab.ID, err)
			tab.mu.Lock()
			tab.Running = false
			tab.Detached = true
			tab.mu.Unlock()
			if m.msgSink != nil {
				m.msgSink(TabInputFailed{TabID: ev.tabID, WorkspaceID: ev.workspaceID, Err: err})
			}
		}
	case tabEventWriteOutput:
		if len(ev.output) == 0 {
			return
		}
		tab.mu.Lock()
		if tab.Terminal != nil {
			flushDone := perf.Time("pty_flush")
			tab.Terminal.Write(ev.output)
			flushDone()
			perf.Count("pty_flush_bytes", int64(len(ev.output)))
			tab.monitorDirty = true
		}
		tab.mu.Unlock()
	default:
		logging.Debug("unknown tab event: %v", ev.kind)
	}
}
