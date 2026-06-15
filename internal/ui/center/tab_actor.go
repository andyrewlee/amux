// Tab-actor concurrency invariants. The tab actor (RunTabActor) serializes all
// mutations to a tab's Terminal so that PTY-reader goroutines, the Bubble Tea
// update loop, and selection/scroll handlers never touch the same terminal
// concurrently. When editing this package, preserve these rules:
//
//  1. Single write path. Every tab terminal write goes through RunTabActor via a
//     tabEvent (sendTabEvent -> handleTabEvent), or — only when the actor channel
//     cannot accept the event — through the synchronous fallback in
//     recoverFailedActorSend. Never call tab.WriteToTerminal or other terminal
//     mutators directly from Update; that bypasses the actor and races the reader.
//
//  2. Backpressure contract. shouldDropTabEvent sheds load only for the
//     selection/scroll class of events (selection-update, selection-scroll-tick,
//     scroll-by, scroll-page) once tabEvents is >=75% full; those are coalescible
//     UI gestures. tabEventWriteOutput is NEVER dropped — losing output corrupts
//     the terminal — and the closed-tab guard in sendTabEvent returns true (treat
//     as delivered) for write events so callers do not re-buffer them.
//
// See internal/app/MESSAGE_FLOW.md and internal/app/ARCHITECTURE.md (Invariants)
// for the broader External-message rules these constraints fit inside.
package center

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
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
	tabEventSendMouse
	tabEventPaste
	tabEventWriteOutput
)

type tabEvent struct {
	tab             *Tab
	workspaceID     string
	tabID           TabID
	kind            tabEventKind
	termX           int
	termY           int
	inBounds        bool
	delta           int
	gen             uint64
	notifyCopy      bool
	scrollPage      int
	diffMsg         tea.Msg
	input           []byte
	pasteText       string
	output          []byte
	writeEpoch      uint64
	catchUp         bool
	hasMoreBuffered bool
	visibleSeq      uint64
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

type tabActorRedraw struct{}

func (tabActorRedraw) MarkCriticalExternalMsg() {}

type tabDiffCmd struct{ cmd tea.Cmd }

type TabInputFailed struct {
	TabID       TabID
	WorkspaceID string
	Err         error
}

func (m *Model) shouldPostWriteRedraw(tab *Tab) bool {
	return tab != nil && (m.isChatTab(tab) || tab.postWriteVisible())
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
		return ev.kind != tabEventWriteOutput
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

func shouldPostTabActorRedraw(kind tabEventKind) bool {
	switch kind {
	case tabEventSelectionStart,
		tabEventSelectionUpdate,
		tabEventSelectionFinish,
		tabEventScrollBy,
		tabEventSelectionClearAndNotify,
		tabEventSelectionScrollTick,
		tabEventScrollToBottom,
		tabEventScrollPage,
		tabEventScrollToTop,
		tabEventDiffInput:
		return true
	default:
		return false
	}
}

func (m *Model) RunTabActor(ctx context.Context) error {
	if m == nil || m.tabEvents == nil {
		return nil
	}
	m.setTabActorReady()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-m.tabEvents:
			m.noteTabActorHeartbeat()
			m.handleTabEvent(ev)
			if shouldPostTabActorRedraw(ev.kind) {
				m.requestTabActorRedraw()
			}
		case <-ticker.C:
			m.noteTabActorHeartbeat()
		}
	}
}

func (m *Model) handleTabEvent(ev tabEvent) {
	if ev.tab == nil || ev.tab.isClosed() {
		perf.Count("tab_event_drop_missing", 1)
		return
	}
	switch ev.kind {
	case tabEventSelectionClear:
		m.handleSelectionClear(ev)
	case tabEventSelectionClearAndNotify:
		m.handleSelectionClearAndNotify(ev)
	case tabEventSelectionCopy:
		m.handleSelectionCopy(ev)
	case tabEventSelectionStart:
		m.handleSelectionStart(ev)
	case tabEventSelectionUpdate:
		m.handleSelectionUpdate(ev)
	case tabEventSelectionFinish:
		m.handleSelectionFinish(ev)
	case tabEventScrollBy:
		m.handleScrollBy(ev)
	case tabEventSelectionScrollTick:
		m.handleSelectionScrollTick(ev)
	case tabEventScrollToBottom:
		m.handleScrollToBottom(ev)
	case tabEventScrollPage:
		m.handleScrollPage(ev)
	case tabEventScrollToTop:
		m.handleScrollToTop(ev)
	case tabEventDiffInput:
		m.handleDiffInput(ev)
	case tabEventSendInput:
		m.handleSendInput(ev)
	case tabEventSendMouse:
		m.handleSendMouse(ev)
	case tabEventPaste:
		m.handlePaste(ev)
	case tabEventWriteOutput:
		m.handleWriteOutput(ev)
	default:
		logging.Debug("unknown tab event: %v", ev.kind)
	}
}

func (m *Model) handleScrollBy(ev tabEvent) {
	tab := ev.tab
	tab.mu.Lock()
	if tab.Terminal != nil && ev.delta != 0 {
		m.scrollTerminalViewLocked(tab, ev.delta)
	}
	tab.mu.Unlock()
}

func (m *Model) handleScrollToBottom(ev tabEvent) {
	tab := ev.tab
	tab.mu.Lock()
	if tab.Terminal != nil && tab.Terminal.IsScrolled() {
		m.scrollTerminalToBottomLocked(tab)
	}
	tab.mu.Unlock()
}

func (m *Model) handleScrollPage(ev tabEvent) {
	tab := ev.tab
	tab.mu.Lock()
	if tab.Terminal != nil && ev.scrollPage != 0 {
		delta := common.ScrollDeltaForHeight(tab.Terminal.Height, 4)
		m.scrollTerminalViewLocked(tab, delta*ev.scrollPage)
	}
	tab.mu.Unlock()
}

func (m *Model) handleScrollToTop(ev tabEvent) {
	tab := ev.tab
	tab.mu.Lock()
	if tab.Terminal != nil {
		m.scrollTerminalToTopLocked(tab)
	}
	tab.mu.Unlock()
}

func (m *Model) handleDiffInput(ev tabEvent) {
	tab := ev.tab
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
}

func (m *Model) handleSendInput(ev tabEvent) {
	m.sendToTerminal(ev.tab, string(ev.input), ev.tabID, ev.workspaceID, "Input")
}

func (m *Model) handleSendMouse(ev tabEvent) {
	m.sendMouseToTerminal(ev.tab, string(ev.input), ev.tabID, ev.workspaceID)
}

func (m *Model) handlePaste(ev tabEvent) {
	if ev.pasteText != "" {
		m.sendToTerminal(ev.tab, "\x1b[200~"+ev.pasteText+"\x1b[201~", ev.tabID, ev.workspaceID, "Paste")
	}
}

func (m *Model) sendMouseToTerminal(tab *Tab, data string, tabID TabID, workspaceID string) {
	if tab == nil || data == "" {
		return
	}
	tab.mu.Lock()
	agent := tab.Agent
	closed := tab.isClosed()
	tab.mu.Unlock()
	if closed || agent == nil || agent.Terminal == nil {
		return
	}
	if err := agent.Terminal.SendString(data); err != nil {
		logging.Warn("Mouse input failed for tab %s: %v", tab.ID, err)
		tab.mu.Lock()
		tab.markDetachedLocked()
		tab.mu.Unlock()
		if m.msgSink != nil {
			m.msgSink(TabInputFailed{TabID: tabID, WorkspaceID: workspaceID, Err: err})
		}
	}
}

func (m *Model) sendToTerminal(tab *Tab, data string, tabID TabID, workspaceID, label string) {
	if tab == nil || data == "" {
		return
	}
	tab.mu.Lock()
	agent := tab.Agent
	closed := tab.isClosed()
	tab.mu.Unlock()
	if closed || agent == nil || agent.Terminal == nil {
		return
	}
	if err := agent.Terminal.SendString(data); err != nil {
		logging.Warn("%s failed for tab %s: %v", label, tab.ID, err)
		tab.mu.Lock()
		tab.markDetachedLocked()
		tab.mu.Unlock()
		if m.msgSink != nil {
			m.msgSink(TabInputFailed{TabID: tabID, WorkspaceID: workspaceID, Err: err})
		}
		return
	}
	recordLocalInputEchoWindow(tab, data, time.Now())
	if m.msgSink != nil && m.isChatTab(tab) {
		m.msgSink(PTYCursorRefresh{WorkspaceID: workspaceID, TabID: tabID})
	}
}
