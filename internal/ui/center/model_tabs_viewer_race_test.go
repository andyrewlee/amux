package center

import (
	"runtime"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/git"
)

// reuseDiffTab mutates the tab's diff viewer, and the tab actor applies queued
// viewer updates via updateDiffViewer — both must serialize on tab.mu. Before
// the fix, reuse ran wrapDiffResults/ResetSource/Init after dropping tab.mu,
// so a concurrent actor update (e.g. a queued wheel event writing m.scroll)
// raced the reuse writes to the same fields. Run under -race.
func TestReuseDiffTab_SerializedAgainstActorUpdates(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	m.SetWorkspace(ws)
	wsID := string(ws.ID())

	tab := newDiffViewerTab(ws, "tab-diff", true)
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0

	change := &git.Change{Path: "pkg/bar.go", Kind: git.ChangeModified}
	wheel := tea.MouseWheelMsg{Button: tea.MouseWheelUp}

	// Hammer actor-path updates for the whole duration of the reuse loop so
	// every reuse window has a live competitor — not a barrier, continuous load.
	var done, started atomic.Bool
	go func() {
		started.Store(true)
		for !done.Load() {
			m.updateDiffViewer(tab, wheel)
			runtime.Gosched()
		}
	}()
	for !started.Load() {
		runtime.Gosched()
	}
	for i := 0; i < 300; i++ {
		m.reuseDiffTab(ws, 0, tab, change, git.DiffModeUnstaged)
	}
	done.Store(true)
}
