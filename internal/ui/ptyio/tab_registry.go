package ptyio

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// ResolveKeyedTab finds the tab an async result belongs to: the stamped
// workspace key is tried first, then every bucket is scanned. Tab IDs are
// process-unique, so the ID alone identifies a tab; the workspace key is only
// a routing hint and goes stale when a rebind or identity change lands between
// dispatch and delivery — the (wsKey, id) pair misses while the tab is still
// very much alive. Dropping a result on that miss used to strand tabs (and, in
// the sidebar, leave the attach gate refusing retries forever), so lookups
// that must not miss use this scan.
//
// alive filters dead entries in both phases — the center skips closed tabs,
// the sidebar only nils — so each component keeps its own liveness rule. The
// returned key is where the tab is actually filed; follow-up messages must use
// it. A miss returns the zero value and "".
func ResolveKeyedTab[T any, K comparable](
	byWorkspace map[string][]T,
	wsKey string,
	id K,
	tabID func(T) K,
	alive func(T) bool,
	context, kind string,
) (T, string) {
	var zero T
	for _, tab := range byWorkspace[wsKey] {
		if alive(tab) && tabID(tab) == id {
			return tab, wsKey
		}
	}
	for actual, tabs := range byWorkspace {
		for _, tab := range tabs {
			if !alive(tab) || tabID(tab) != id {
				continue
			}
			// The fallback keeps the user unblocked, but the drift itself is
			// a bug worth seeing in the log.
			logging.Warn("%s: %s %v routed with workspace %s but filed under %s", context, kind, id, wsKey, actual)
			return tab, actual
		}
	}
	return zero, ""
}

// RebindHooks carries the component-specific pieces of RebindMigratedTabs.
type RebindHooks[T any] struct {
	// State returns the tab's shared ptyio.State; nil skips the tab entirely
	// (also how nil tabs and nil sub-state are filtered).
	State func(T) *State
	// Lock returns the mutex guarding the tab's fields — the center locks the
	// tab itself, the sidebar locks its TerminalState.
	Lock func(T) *sync.Mutex
	// ExamineLocked runs with the lock held: apply component writes (the
	// center rebinds its Workspace pointer) and report whether the tab still
	// owns a live terminal whose reader must be restarted.
	ExamineLocked func(T) (restart bool)
	// Restart stops and restarts the PTY reader under the new workspace key.
	// It runs unlocked and only when ExamineLocked reported true.
	Restart func(T) tea.Cmd
	// FlushTiming returns the quiet delay for the re-armed flush tick.
	FlushTiming func(T) time.Duration
	// FlushMsg builds the flush message stamped with the new workspace key.
	FlushMsg func(T) tea.Msg
}

// RebindMigratedTabs runs the per-tab side effects of a workspace rebind over
// the slice merged by common.RebindTabMaps: under the tab's lock it unlatches
// any flush armed under the old key (a tick stamped with it would orphan on
// the exact-key lookup and latch FlushScheduled forever), then — unlocked —
// restarts the reader when the tab still owns a live terminal and re-arms a
// flush under the new key so buffered output is not stranded.
func RebindMigratedTabs[T any](merged []T, h RebindHooks[T]) tea.Cmd {
	var cmds []tea.Cmd
	for _, tab := range merged {
		st := h.State(tab)
		if st == nil {
			continue
		}
		mu := h.Lock(tab)
		mu.Lock()
		restart := h.ExamineLocked(tab)
		hadPending := len(st.PendingOutput) > 0
		lastOutputAt := st.LastOutputAt
		if st.FlushScheduled {
			st.FlushScheduled = false
			st.FlushPendingSince = time.Time{}
		}
		mu.Unlock()

		if restart {
			if cmd := h.Restart(tab); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if hadPending {
			st.FlushScheduled = true
			st.FlushPendingSince = lastOutputAt
			cmds = append(cmds, common.SafeTick(h.FlushTiming(tab), func(time.Time) tea.Msg {
				return h.FlushMsg(tab)
			}))
		}
	}
	return common.SafeBatch(cmds...)
}
