package center

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/vterm"
)

// setupScrollModel creates a center pane model with scrollback content.
// The tab actor path is exercised directly via handleTabEvent.
func setupScrollModel(t *testing.T) (*Model, *Tab, string) {
	t.Helper()
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	m := New(cfg)
	wt := &data.Workspace{
		Name: "wt",
		Repo: "/tmp/repo",
		Root: "/tmp/repo",
	}
	m.SetWorkspace(wt)
	wtID := string(wt.ID())

	vt := vterm.New(80, 24)
	vt.AllowAltScreenScrollback = true
	for i := 0; i < 100; i++ {
		vt.Write([]byte(fmt.Sprintf("line %d\r\n", i)))
	}

	tab := &Tab{
		ID:        TabID("tab-scroll-1"),
		Workspace: wt,
		Terminal:  vt,
	}
	m.tabsByWorkspace[wtID] = []*Tab{tab}
	m.activeTabByWorkspace[wtID] = 0
	m.SetSize(100, 40)
	m.SetOffset(0)
	m.Focus()

	// Capture messages from msgSink
	var sinkMsgs []tea.Msg
	m.msgSink = func(msg tea.Msg) {
		sinkMsgs = append(sinkMsgs, msg)
	}
	_ = sinkMsgs // used via closure

	return m, tab, wtID
}

func TestTabActor_SelectionScrollTick_ScrollsUp(t *testing.T) {
	m, tab, wtID := setupScrollModel(t)

	// Start selection via tab actor
	tab.mu.Lock()
	absLine := tab.Terminal.ScreenYToAbsoluteLine(10)
	tab.mu.Unlock()
	m.handleTabEvent(tabEvent{
		tab:         tab,
		workspaceID: wtID,
		tabID:       tab.ID,
		kind:        tabEventSelectionStart,
		termX:       10,
		termY:       10,
		inBounds:    true,
	})

	tab.mu.Lock()
	if !tab.Selection.Active {
		tab.mu.Unlock()
		t.Fatal("expected selection to be active")
	}
	if tab.Selection.StartLine != absLine {
		tab.mu.Unlock()
		t.Fatalf("expected StartLine=%d, got %d", absLine, tab.Selection.StartLine)
	}
	tab.mu.Unlock()

	// Drag above viewport (termY = -1)
	var sinkMsgs []tea.Msg
	m.msgSink = func(msg tea.Msg) { sinkMsgs = append(sinkMsgs, msg) }

	m.handleTabEvent(tabEvent{
		tab:         tab,
		workspaceID: wtID,
		tabID:       tab.ID,
		kind:        tabEventSelectionUpdate,
		termX:       10,
		termY:       -1,
	})

	tab.mu.Lock()
	if tab.selectionScroll.ScrollDir != 1 {
		tab.mu.Unlock()
		t.Fatalf("expected ScrollDir=1 (up), got %d", tab.selectionScroll.ScrollDir)
	}
	if !tab.selectionScroll.Active {
		tab.mu.Unlock()
		t.Fatal("expected scroll to be active")
	}
	gen := tab.selectionScroll.Gen
	tab.mu.Unlock()

	// Should have sent a selectionTickRequest
	if len(sinkMsgs) == 0 {
		t.Fatal("expected selectionTickRequest to be sent via msgSink")
	}
	tickReq, ok := sinkMsgs[0].(selectionTickRequest)
	if !ok {
		t.Fatalf("expected selectionTickRequest, got %T", sinkMsgs[0])
	}
	if tickReq.gen != gen {
		t.Fatalf("tick request gen=%d, expected %d", tickReq.gen, gen)
	}

	// Process scroll tick via tab actor
	tab.mu.Lock()
	scrollBefore, _ := tab.Terminal.GetScrollInfo()
	tab.mu.Unlock()

	sinkMsgs = nil
	m.handleTabEvent(tabEvent{
		tab:         tab,
		workspaceID: wtID,
		tabID:       tab.ID,
		kind:        tabEventSelectionScrollTick,
		gen:         gen,
	})

	tab.mu.Lock()
	scrollAfter, _ := tab.Terminal.GetScrollInfo()
	if scrollAfter <= scrollBefore {
		tab.mu.Unlock()
		t.Fatalf("expected scroll offset to increase, before=%d after=%d",
			scrollBefore, scrollAfter)
	}
	// Verify selection endpoint was updated to top edge
	topAbsLine := tab.Terminal.ScreenYToAbsoluteLine(0)
	if tab.Selection.EndLine != topAbsLine {
		tab.mu.Unlock()
		t.Fatalf("expected EndLine=%d (top edge), got %d", topAbsLine, tab.Selection.EndLine)
	}
	tab.mu.Unlock()

	// Should have requested another tick
	if len(sinkMsgs) == 0 {
		t.Fatal("expected next tick request after scroll tick")
	}
}

func TestTabActor_SelectionScrollTick_ScrollsDown(t *testing.T) {
	m, tab, wtID := setupScrollModel(t)

	// Scroll up first so there's room to scroll down
	tab.mu.Lock()
	tab.Terminal.ScrollView(50)
	tab.mu.Unlock()

	// Start selection
	m.handleTabEvent(tabEvent{
		tab:         tab,
		workspaceID: wtID,
		tabID:       tab.ID,
		kind:        tabEventSelectionStart,
		termX:       10,
		termY:       10,
		inBounds:    true,
	})

	// Drag below viewport
	var sinkMsgs []tea.Msg
	m.msgSink = func(msg tea.Msg) { sinkMsgs = append(sinkMsgs, msg) }

	tab.mu.Lock()
	termHeight := tab.Terminal.Height
	tab.mu.Unlock()

	m.handleTabEvent(tabEvent{
		tab:         tab,
		workspaceID: wtID,
		tabID:       tab.ID,
		kind:        tabEventSelectionUpdate,
		termX:       10,
		termY:       termHeight + 1, // below viewport
	})

	tab.mu.Lock()
	if tab.selectionScroll.ScrollDir != -1 {
		tab.mu.Unlock()
		t.Fatalf("expected ScrollDir=-1 (down), got %d", tab.selectionScroll.ScrollDir)
	}
	gen := tab.selectionScroll.Gen
	scrollBefore, _ := tab.Terminal.GetScrollInfo()
	tab.mu.Unlock()

	// Process tick
	sinkMsgs = nil
	m.handleTabEvent(tabEvent{
		tab:         tab,
		workspaceID: wtID,
		tabID:       tab.ID,
		kind:        tabEventSelectionScrollTick,
		gen:         gen,
	})

	tab.mu.Lock()
	scrollAfter, _ := tab.Terminal.GetScrollInfo()
	if scrollAfter >= scrollBefore {
		tab.mu.Unlock()
		t.Fatalf("expected scroll offset to decrease (down), before=%d after=%d",
			scrollBefore, scrollAfter)
	}
	// Verify endpoint at bottom edge
	bottomAbsLine := tab.Terminal.ScreenYToAbsoluteLine(termHeight - 1)
	if tab.Selection.EndLine != bottomAbsLine {
		tab.mu.Unlock()
		t.Fatalf("expected EndLine=%d (bottom edge), got %d", bottomAbsLine, tab.Selection.EndLine)
	}
	tab.mu.Unlock()
}

func TestTabActor_SelectionScrollTick_StopsAfterFinish(t *testing.T) {
	m, tab, wtID := setupScrollModel(t)

	// Start selection and drag above
	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionStart, termX: 10, termY: 10, inBounds: true,
	})

	var sinkMsgs []tea.Msg
	m.msgSink = func(msg tea.Msg) { sinkMsgs = append(sinkMsgs, msg) }

	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionUpdate, termX: 10, termY: -1,
	})

	tab.mu.Lock()
	gen := tab.selectionScroll.Gen
	tab.mu.Unlock()

	// Finish selection (mouse release)
	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionFinish,
	})

	// Tick with old gen should be rejected
	sinkMsgs = nil
	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionScrollTick, gen: gen,
	})

	// Should NOT have requested another tick
	for _, msg := range sinkMsgs {
		if _, ok := msg.(selectionTickRequest); ok {
			t.Fatal("expected no tick request after selection finish")
		}
	}
}

func TestTabActor_SelectionScrollTick_StopsAfterClear(t *testing.T) {
	m, tab, wtID := setupScrollModel(t)

	// Start selection and drag above
	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionStart, termX: 10, termY: 10, inBounds: true,
	})

	var sinkMsgs []tea.Msg
	m.msgSink = func(msg tea.Msg) { sinkMsgs = append(sinkMsgs, msg) }

	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionUpdate, termX: 10, termY: -1,
	})

	tab.mu.Lock()
	gen := tab.selectionScroll.Gen
	tab.mu.Unlock()

	// Clear selection (e.g., user types a key)
	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionClear,
	})

	// Tick with old gen should be rejected
	sinkMsgs = nil
	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionScrollTick, gen: gen,
	})

	for _, msg := range sinkMsgs {
		if _, ok := msg.(selectionTickRequest); ok {
			t.Fatal("expected no tick request after selection clear")
		}
	}
}

func TestTabActor_SelectionScrollTick_Continuous(t *testing.T) {
	m, tab, wtID := setupScrollModel(t)

	// Start selection and drag above
	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionStart, termX: 10, termY: 10, inBounds: true,
	})

	var sinkMsgs []tea.Msg
	m.msgSink = func(msg tea.Msg) { sinkMsgs = append(sinkMsgs, msg) }

	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionUpdate, termX: 10, termY: -1,
	})

	tab.mu.Lock()
	gen := tab.selectionScroll.Gen
	tab.mu.Unlock()

	// Simulate 5 consecutive ticks
	for i := 0; i < 5; i++ {
		sinkMsgs = nil
		m.handleTabEvent(tabEvent{
			tab: tab, workspaceID: wtID, tabID: tab.ID,
			kind: tabEventSelectionScrollTick, gen: gen,
		})

		hasTickReq := false
		for _, msg := range sinkMsgs {
			if _, ok := msg.(selectionTickRequest); ok {
				hasTickReq = true
				break
			}
		}
		if !hasTickReq {
			t.Fatalf("tick %d: expected continuation tick request", i)
		}
	}

	// Verify accumulated scrolling
	tab.mu.Lock()
	offset, _ := tab.Terminal.GetScrollInfo()
	tab.mu.Unlock()
	if offset < 5 {
		t.Fatalf("expected at least 5 lines scrolled, got offset=%d", offset)
	}
}

func TestTabActor_SelectionScrollTick_EndpointTracksX(t *testing.T) {
	m, tab, wtID := setupScrollModel(t)

	// Start selection
	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionStart, termX: 10, termY: 10, inBounds: true,
	})

	// Drag above at X=42
	var sinkMsgs []tea.Msg
	m.msgSink = func(msg tea.Msg) { sinkMsgs = append(sinkMsgs, msg) }

	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionUpdate, termX: 42, termY: -1,
	})

	tab.mu.Lock()
	gen := tab.selectionScroll.Gen
	tab.mu.Unlock()

	// Process tick
	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionScrollTick, gen: gen,
	})

	tab.mu.Lock()
	if tab.Selection.EndX != 42 {
		tab.mu.Unlock()
		t.Fatalf("expected EndX=42 (last drag X), got %d", tab.Selection.EndX)
	}
	tab.mu.Unlock()
}

func TestTabActor_SelectionScrollTick_NoTickInBounds(t *testing.T) {
	m, tab, wtID := setupScrollModel(t)

	// Start selection
	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionStart, termX: 10, termY: 10, inBounds: true,
	})

	// Drag within bounds
	var sinkMsgs []tea.Msg
	m.msgSink = func(msg tea.Msg) { sinkMsgs = append(sinkMsgs, msg) }

	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionUpdate, termX: 20, termY: 15,
	})

	// Should not have started a tick loop
	for _, msg := range sinkMsgs {
		if _, ok := msg.(selectionTickRequest); ok {
			t.Fatal("expected no tick request when dragging within viewport")
		}
	}

	tab.mu.Lock()
	if tab.selectionScroll.Active {
		tab.mu.Unlock()
		t.Fatal("expected scroll to be inactive when in bounds")
	}
	tab.mu.Unlock()
}
