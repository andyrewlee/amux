package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/diff"
)

// newLifecycleModel builds a center Model with a single workspace already
// selected, returning the model and the workspace id key used in the tab maps.
func newLifecycleModel(t *testing.T) (*Model, string) {
	t.Helper()
	m := New(&config.Config{})
	ws := data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/repo/feature")
	m.SetWorkspace(ws)
	return m, string(ws.ID())
}

func TestInitReturnsNoCmd(t *testing.T) {
	m := New(&config.Config{})
	if cmd := m.Init(); cmd != nil {
		t.Fatalf("Init should return a nil tea.Cmd, got %T", cmd)
	}
}

func TestFocusedReflectsFocusState(t *testing.T) {
	m, _ := newLifecycleModel(t)

	if m.Focused() {
		t.Fatal("a freshly created model should not be focused")
	}

	m.Focus()
	if !m.Focused() {
		t.Fatal("Focused should report true after Focus()")
	}

	// Focus is idempotent: calling again keeps it focused.
	m.Focus()
	if !m.Focused() {
		t.Fatal("Focused should remain true after a second Focus()")
	}

	m.Blur()
	if m.Focused() {
		t.Fatal("Focused should report false after Blur()")
	}

	// Blur is idempotent.
	m.Blur()
	if m.Focused() {
		t.Fatal("Focused should remain false after a second Blur()")
	}
}

func TestHasTabs(t *testing.T) {
	t.Run("no workspace", func(t *testing.T) {
		m := New(&config.Config{})
		if m.HasTabs() {
			t.Fatal("a model with no workspace should report no tabs")
		}
	})

	t.Run("workspace with no tabs", func(t *testing.T) {
		m, _ := newLifecycleModel(t)
		if m.HasTabs() {
			t.Fatal("a workspace with no tabs should report HasTabs() == false")
		}
	})

	t.Run("workspace with empty tab slice", func(t *testing.T) {
		m, wsID := newLifecycleModel(t)
		m.tabs.ByWorkspace[wsID] = []*Tab{}
		if m.HasTabs() {
			t.Fatal("an explicitly empty tab slice should report HasTabs() == false")
		}
	})

	t.Run("workspace with one tab", func(t *testing.T) {
		m, wsID := newLifecycleModel(t)
		m.tabs.ByWorkspace[wsID] = []*Tab{{ID: generateTabID(), Name: "t"}}
		if !m.HasTabs() {
			t.Fatal("a workspace with one tab should report HasTabs() == true")
		}
	})

	t.Run("workspace with several tabs", func(t *testing.T) {
		m, wsID := newLifecycleModel(t)
		m.tabs.ByWorkspace[wsID] = []*Tab{
			{ID: generateTabID(), Name: "a"},
			{ID: generateTabID(), Name: "b"},
			{ID: generateTabID(), Name: "c"},
		}
		if !m.HasTabs() {
			t.Fatal("a workspace with several tabs should report HasTabs() == true")
		}
	})
}

func TestSetCanFocusRight(t *testing.T) {
	tests := []struct {
		name string
		set  bool
	}{
		{name: "enable", set: true},
		{name: "disable", set: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := New(&config.Config{})
			m.SetCanFocusRight(tc.set)
			if m.canFocusRight != tc.set {
				t.Fatalf("SetCanFocusRight(%v) -> canFocusRight=%v", tc.set, m.canFocusRight)
			}
			// Last write wins after toggling.
			m.SetCanFocusRight(!tc.set)
			if m.canFocusRight == tc.set {
				t.Fatalf("toggling SetCanFocusRight did not flip canFocusRight from %v", tc.set)
			}
		})
	}
}

func TestSetShowKeymapHints(t *testing.T) {
	tests := []struct {
		name string
		show bool
	}{
		{name: "show", show: true},
		{name: "hide", show: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := New(&config.Config{})
			m.SetShowKeymapHints(tc.show)
			if m.showKeymapHints != tc.show {
				t.Fatalf("SetShowKeymapHints(%v) -> showKeymapHints=%v", tc.show, m.showKeymapHints)
			}
			m.SetShowKeymapHints(!tc.show)
			if m.showKeymapHints == tc.show {
				t.Fatalf("toggling SetShowKeymapHints did not flip showKeymapHints from %v", tc.show)
			}
		})
	}
}

func TestSetStylesUpdatesModelAndPropagatesToDiffViewers(t *testing.T) {
	m, wsID := newLifecycleModel(t)

	// A tab with a real diff viewer (propagation target), a tab with a nil
	// DiffViewer (skipped without panic), and a nil tab entry (skipped).
	dv := &diff.Model{}
	m.tabs.ByWorkspace[wsID] = []*Tab{
		{ID: generateTabID(), Name: "with-viewer", DiffViewer: dv},
		{ID: generateTabID(), Name: "no-viewer"},
		nil,
	}

	custom := common.DefaultStyles()
	custom.Title = lipgloss.NewStyle().SetString("custom-title")

	m.SetStyles(custom)

	if got := m.styles.Title.Value(); got != "custom-title" {
		t.Fatalf("SetStyles did not update model styles: got Title %q, want %q", got, "custom-title")
	}

	// Overwriting with a new Styles replaces wholesale.
	replacement := common.DefaultStyles()
	replacement.Title = lipgloss.NewStyle().SetString("replacement")
	m.SetStyles(replacement)
	if got := m.styles.Title.Value(); got != "replacement" {
		t.Fatalf("SetStyles did not overwrite model styles: got Title %q, want %q", got, "replacement")
	}
}

func TestSetStylesWithNoTabsIsSafe(t *testing.T) {
	m := New(&config.Config{})
	custom := common.DefaultStyles()
	custom.Title = lipgloss.NewStyle().SetString("only-model")

	m.SetStyles(custom)

	if got := m.styles.Title.Value(); got != "only-model" {
		t.Fatalf("SetStyles on a tab-less model failed: got %q, want %q", got, "only-model")
	}
}

func TestSetMsgSink(t *testing.T) {
	m := New(&config.Config{})

	var got []tea.Msg
	m.SetMsgSink(func(msg tea.Msg) { got = append(got, msg) })

	if m.msgSink == nil {
		t.Fatal("SetMsgSink should install a msgSink callback")
	}
	if m.msgSinkTry != nil {
		t.Fatal("SetMsgSink should clear any msgSinkTry callback")
	}

	m.msgSink(tabActorRedraw{})
	if len(got) != 1 {
		t.Fatalf("installed msgSink not invoked: got %d messages, want 1", len(got))
	}
	if _, ok := got[0].(tabActorRedraw); !ok {
		t.Fatalf("msgSink received unexpected message type %T", got[0])
	}
}

func TestSetMsgSinkReplacesPriorTry(t *testing.T) {
	m := New(&config.Config{})
	// Install a try-sink first, then a plain sink should clear it.
	m.SetMsgSinkTry(func(tea.Msg) bool { return true })
	if m.msgSinkTry == nil {
		t.Fatal("precondition: SetMsgSinkTry should install a try-sink")
	}

	m.SetMsgSink(func(tea.Msg) {})
	if m.msgSinkTry != nil {
		t.Fatal("SetMsgSink should clear a previously installed msgSinkTry")
	}
	if m.msgSink == nil {
		t.Fatal("SetMsgSink should install the plain sink")
	}
}

func TestSetMsgSinkTry(t *testing.T) {
	t.Run("non-nil try sink", func(t *testing.T) {
		m := New(&config.Config{})

		var tryCalls int
		m.SetMsgSinkTry(func(tea.Msg) bool {
			tryCalls++
			return true
		})

		if m.msgSinkTry == nil {
			t.Fatal("SetMsgSinkTry should install msgSinkTry")
		}
		if m.msgSink == nil {
			t.Fatal("SetMsgSinkTry should install a bridging msgSink that delegates to the try-sink")
		}

		// The bridging msgSink must forward to the try-sink (ignoring its bool).
		m.msgSink(tabActorRedraw{})
		if tryCalls != 1 {
			t.Fatalf("bridging msgSink did not delegate to try-sink: tryCalls=%d, want 1", tryCalls)
		}

		// The try-sink itself reports its bool result back to the caller.
		if !m.msgSinkTry(tabActorRedraw{}) {
			t.Fatal("expected try-sink to report success")
		}
	})

	t.Run("nil try sink clears both sinks", func(t *testing.T) {
		m := New(&config.Config{})
		m.SetMsgSink(func(tea.Msg) {})

		m.SetMsgSinkTry(nil)

		if m.msgSinkTry != nil {
			t.Fatal("SetMsgSinkTry(nil) should leave msgSinkTry nil")
		}
		if m.msgSink != nil {
			t.Fatal("SetMsgSinkTry(nil) should also clear msgSink")
		}
	})
}

func TestTickSpinner(t *testing.T) {
	m := New(&config.Config{})
	if m.spinnerFrame != 0 {
		t.Fatalf("a fresh model should start at spinnerFrame 0, got %d", m.spinnerFrame)
	}

	for i := 1; i <= 5; i++ {
		m.TickSpinner()
		if m.spinnerFrame != i {
			t.Fatalf("after %d ticks spinnerFrame=%d, want %d", i, m.spinnerFrame, i)
		}
	}
}

func TestClose(t *testing.T) {
	m, wsID := newLifecycleModel(t)

	tabA := &Tab{
		ID:         generateTabID(),
		Name:       "a",
		Workspace:  m.workspace,
		DiffViewer: &diff.Model{},
		Running:    true,
	}
	tabB := &Tab{
		ID:        generateTabID(),
		Name:      "b",
		Workspace: m.workspace,
		Running:   true,
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tabA, tabB}

	m.Close()

	for _, tab := range []*Tab{tabA, tabB} {
		if !tab.isClosed() {
			t.Fatalf("Close should mark tab %q closed", tab.Name)
		}
		tab.mu.Lock()
		running := tab.Running
		viewer := tab.DiffViewer
		term := tab.Terminal
		snap := tab.CachedSnap
		traceClosed := tab.ptyTraceClosed
		ws := tab.Workspace
		tab.mu.Unlock()

		if running {
			t.Fatalf("Close should clear Running on tab %q", tab.Name)
		}
		if viewer != nil {
			t.Fatalf("Close should nil out DiffViewer on tab %q", tab.Name)
		}
		if term != nil {
			t.Fatalf("Close should nil out Terminal on tab %q", tab.Name)
		}
		if snap != nil {
			t.Fatalf("Close should nil out CachedSnap on tab %q", tab.Name)
		}
		if ws != nil {
			t.Fatalf("Close should nil out Workspace on tab %q", tab.Name)
		}
		// ptyTraceClosed is only flipped when there was a trace file; with no
		// trace file it stays false, which is the expected default here.
		if traceClosed {
			t.Fatalf("Close should not mark a (nonexistent) trace closed on tab %q", tab.Name)
		}
	}
}

func TestCloseWithNoTabsIsSafe(t *testing.T) {
	m := New(&config.Config{})
	// No workspace, no tabs, no agents: Close must be a safe no-op.
	m.Close()
}
