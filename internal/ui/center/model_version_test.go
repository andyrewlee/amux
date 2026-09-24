package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/vterm"
)

// TestTabBarVersion_MutationsBump pins the fingerprint coverage the
// compose-time gate relies on: every input that changes the rendered tab bar
// must move TabBarVersion, or the gate reuses a stale drawable.
func TestTabBarVersion_MutationsBump(t *testing.T) {
	newModel := func() *Model {
		m := newTestModel()
		m.SetSize(80, 24)
		addWorkspaceWithTabs(t, m, "ws",
			&Tab{ID: "t1", Name: "one", Assistant: "amp", Running: true, Terminal: vterm.New(80, 24)},
			&Tab{ID: "t2", Name: "two", Assistant: "amp", Running: true, Terminal: vterm.New(80, 24)},
		)
		return m
	}

	t.Run("tab add bumps", func(t *testing.T) {
		m := newModel()
		v := m.TabBarVersion()
		m.AddTab(&Tab{ID: "t3", Name: "three", Assistant: "amp", Running: true, Workspace: m.workspace})
		if m.TabBarVersion() == v {
			t.Fatal("AddTab did not move TabBarVersion")
		}
	})

	t.Run("active index bumps", func(t *testing.T) {
		m := newModel()
		v := m.TabBarVersion()
		m.setActiveTabIdx(1)
		if m.TabBarVersion() == v {
			t.Fatal("setActiveTabIdx did not move TabBarVersion")
		}
	})

	t.Run("tab disconnect flips bump", func(t *testing.T) {
		m := newModel()
		v := m.TabBarVersion()
		tab := m.getTabs()[0]
		tab.mu.Lock()
		tab.Detached = true
		tab.mu.Unlock()
		if m.TabBarVersion() == v {
			t.Fatal("Detached flip did not move TabBarVersion")
		}
	})

	t.Run("tab activity bit bumps", func(t *testing.T) {
		m := newModel()
		v := m.TabBarVersion()
		m.getTabs()[0].NoteVisibleOutput(time.Now())
		if m.TabBarVersion() == v {
			t.Fatal("visible-output mark did not move TabBarVersion")
		}
	})

	t.Run("styles bump", func(t *testing.T) {
		m := newModel()
		v := m.TabBarVersion()
		m.SetStyles(common.DefaultStyles())
		if m.TabBarVersion() == v {
			t.Fatal("SetStyles did not move TabBarVersion")
		}
	})

	t.Run("stable when nothing mutates", func(t *testing.T) {
		m := newModel()
		if v := m.TabBarVersion(); v != m.TabBarVersion() {
			t.Fatal("TabBarVersion not stable across identical reads")
		}
	})
}

// TestStatusLineVersion_MutationsBump pins fingerprint coverage for the
// status line: active tab identity, lifecycle flags, and scroll state.
func TestStatusLineVersion_MutationsBump(t *testing.T) {
	newModel := func() *Model {
		m := newTestModel()
		m.SetSize(80, 24)
		addWorkspaceWithTabs(t, m, "ws",
			&Tab{ID: "t1", Name: "one", Assistant: "amp", Running: true, Terminal: vterm.New(80, 24)},
			&Tab{ID: "t2", Name: "two", Assistant: "amp", Running: true, Terminal: vterm.New(80, 24)},
		)
		return m
	}

	t.Run("active tab switch bumps", func(t *testing.T) {
		m := newModel()
		v := m.StatusLineVersion()
		m.setActiveTabIdx(1)
		if m.StatusLineVersion() == v {
			t.Fatal("active tab switch did not move StatusLineVersion")
		}
	})

	t.Run("detach bumps", func(t *testing.T) {
		m := newModel()
		v := m.StatusLineVersion()
		tab := m.getTabs()[0]
		tab.mu.Lock()
		tab.Detached = true
		tab.mu.Unlock()
		if m.StatusLineVersion() == v {
			t.Fatal("Detached flip did not move StatusLineVersion")
		}
	})

	t.Run("scroll bumps", func(t *testing.T) {
		m := newModel()
		tab := m.getTabs()[0]
		// Fill scrollback so a scroll position exists.
		for i := 0; i < 100; i++ {
			tab.WriteToTerminal([]byte("line\n"))
		}
		v := m.StatusLineVersion()
		tab.Terminal.ScrollViewToTop()
		if m.StatusLineVersion() == v {
			t.Fatal("scroll did not move StatusLineVersion")
		}
	})

	t.Run("stable when nothing mutates", func(t *testing.T) {
		m := newModel()
		if v := m.StatusLineVersion(); v != m.StatusLineVersion() {
			t.Fatal("StatusLineVersion not stable across identical reads")
		}
	})
}
