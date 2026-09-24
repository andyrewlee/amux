package center

import (
	"sort"
	"strconv"
	"time"

	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/tmux"
)

// activityTagFlushDue is the internal message a scheduled flush posts back
// through msgSink once the throttle window has elapsed. Unknown to the app
// dispatcher, it reaches Update via the default forward-to-center case.
type activityTagFlushDue struct{}

// setSessionTagForSessions is a seam over tmux.SetSessionTagValueForSessions
// so tests can count batched flushes without a tmux server.
var setSessionTagForSessions = tmux.SetSessionTagValueForSessions

// markActivityTagForFlush records sessionName for the next batched
// @amux_last_output_at write and schedules a flush if none is pending. The
// per-tab throttle (activityTagThrottle, enforced upstream in
// noteVisibleActivity*) means each tab contributes at most one mark per
// second; the pending set turns N tab marks into ONE tmux invocation.
// The flush timestamp is unified at drain time — the 1s throttle window
// makes per-tab skew irrelevant to the freshness checks that read the tag.
func (m *Model) markActivityTagForFlush(sessionName string) {
	if sessionName == "" {
		return
	}
	m.activityTagsMu.Lock()
	if m.pendingActivityTags == nil {
		m.pendingActivityTags = make(map[string]struct{})
	}
	m.pendingActivityTags[sessionName] = struct{}{}
	schedule := !m.activityTagFlushPending
	m.activityTagFlushPending = true
	m.activityTagsMu.Unlock()
	if !schedule || m.msgSink == nil {
		return
	}
	sink := m.msgSink
	safego.Go("center.activity_tag_flush_due", func() {
		time.Sleep(activityTagThrottle)
		sink(activityTagFlushDue{})
	})
}

// handleActivityTagFlushDue drains the pending set into a single batched
// tag write, off the Update goroutine.
func (m *Model) handleActivityTagFlushDue() {
	sessions := m.drainActivityTags()
	if len(sessions) == 0 {
		return
	}
	opts := m.tmuxOpts
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	safego.Go("center.tmux_tag_flush", func() {
		_ = setSessionTagForSessions(sessions, tmux.TagLastOutputAt, timestamp, opts)
	})
}

// drainActivityTags takes the pending set and re-arms the scheduler. Marks
// arriving after the drain schedule a fresh flush, so a continuously
// streaming session writes at most one tag per throttle window.
func (m *Model) drainActivityTags() []string {
	m.activityTagsMu.Lock()
	defer m.activityTagsMu.Unlock()
	m.activityTagFlushPending = false
	if len(m.pendingActivityTags) == 0 {
		return nil
	}
	sessions := make([]string, 0, len(m.pendingActivityTags))
	for name := range m.pendingActivityTags {
		sessions = append(sessions, name)
	}
	sort.Strings(sessions) // deterministic batch contents for tests
	clear(m.pendingActivityTags)
	return sessions
}
