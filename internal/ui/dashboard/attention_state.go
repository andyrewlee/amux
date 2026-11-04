package dashboard

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/fsm"
)

// attentionState is the per-workspace badge machine: it folds the live agent
// publish, the donePending/doneAcked latches, and the BEL attention latch
// into one control state, so rendering is a plain enum lookup instead of
// three-map boolean algebra. One machine instance per workspace, held in
// Model.attention.
type attentionState int

const (
	// attQuiet: no agent activity and no latches — nothing renders.
	attQuiet attentionState = iota
	// attWorking: the last publish was Working; the done latches are clear,
	// so the next watched finish starts from a clean slate.
	attWorking
	// attDoneFresh: a watched Working→Done edge latched the done badge. It
	// survives the agent state decaying to Idle (the old donePending latch)
	// and only clears on ackDone or a new Working publish.
	attDoneFresh
	// attDoneLive: Done observed without a Working predecessor — the live
	// state renders the badge but nothing latches, so it decays with the
	// publish (the first observation is not a finish the user watched).
	attDoneLive
	// attAcked: the user viewed the workspace (ackDone); the done badge is
	// suppressed until the next Working publish re-arms it. The BEL latch
	// can still surface on top of it via MarkAttention.
	attAcked
	// attBell: the unacked attention latch (MarkAttention, agent-emitted
	// BEL). The badge renders regardless of the live agent state and
	// survives Working publishes — an agent ringing mid-task (e.g. a
	// permission prompt) still needs a look. Cleared only by ackDone.
	attBell
	// attBellAcked: the attention latch raised on top of an un-lifted ack
	// (ackDone during a Working window, then MarkAttention). The badge
	// renders exactly like attBell, but the done bell stays suppressed —
	// the ack is only lifted by a Working publish, matching the old
	// doneAcked flag's lifetime.
	attBellAcked
)

// attentionEvent is the reduction of one activity sync to a workspace's
// machine: the live publish (classified by whether a Working predecessor
// makes the Done a watched finish), plus the user-driven ack and the
// agent-driven attention mark.
type attentionEvent int

const (
	evPublishWorking attentionEvent = iota
	// evPublishDoneFresh: Done observed with a Working predecessor — the
	// watched-finish edge, and the only event that may ring the done bell.
	evPublishDoneFresh
	// evPublishDoneStale: Done observed without a Working predecessor.
	evPublishDoneStale
	// evPublishIdle covers both an explicit Idle publish and the workspace
	// dropping out of the state map (data.AgentState's zero value is Idle).
	evPublishIdle
	evMarkAttention
	evAck
)

// attentionMachine is the checked transition table. It is total over the
// event alphabet: re-observations are self-loops, and the two entries the
// live-publish reduction cannot reach (DoneFresh from Acked/DoneLive, where
// the table parks rather than latches) match the old latch semantics — a
// fresh edge while acked stays suppressed until Working re-arms.
var attentionMachine = fsm.MustCompile(fsm.Machine[attentionState, attentionEvent]{
	Name:    "dashboard-attention",
	Initial: attQuiet,
	Steps: []fsm.Step[attentionState, attentionEvent]{
		{From: attQuiet, Event: evPublishWorking, To: attWorking},
		{From: attQuiet, Event: evPublishDoneFresh, To: attDoneFresh},
		{From: attQuiet, Event: evPublishDoneStale, To: attDoneLive},
		{From: attQuiet, Event: evPublishIdle, To: attQuiet},
		{From: attQuiet, Event: evMarkAttention, To: attBell},
		{From: attQuiet, Event: evAck, To: attAcked},
		{From: attWorking, Event: evPublishWorking, To: attWorking},
		{From: attWorking, Event: evPublishDoneFresh, To: attDoneFresh},
		{From: attWorking, Event: evPublishDoneStale, To: attDoneLive},
		{From: attWorking, Event: evPublishIdle, To: attQuiet},
		{From: attWorking, Event: evMarkAttention, To: attBell},
		{From: attWorking, Event: evAck, To: attAcked},
		{From: attDoneFresh, Event: evPublishWorking, To: attWorking},
		{From: attDoneFresh, Event: evPublishDoneFresh, To: attDoneFresh},
		{From: attDoneFresh, Event: evPublishDoneStale, To: attDoneFresh},
		{From: attDoneFresh, Event: evPublishIdle, To: attDoneFresh},
		{From: attDoneFresh, Event: evMarkAttention, To: attBell},
		{From: attDoneFresh, Event: evAck, To: attAcked},
		{From: attDoneLive, Event: evPublishWorking, To: attWorking},
		{From: attDoneLive, Event: evPublishDoneFresh, To: attDoneFresh},
		{From: attDoneLive, Event: evPublishDoneStale, To: attDoneLive},
		{From: attDoneLive, Event: evPublishIdle, To: attQuiet},
		{From: attDoneLive, Event: evMarkAttention, To: attBell},
		{From: attDoneLive, Event: evAck, To: attAcked},
		{From: attAcked, Event: evPublishWorking, To: attWorking},
		{From: attAcked, Event: evPublishDoneFresh, To: attAcked},
		{From: attAcked, Event: evPublishDoneStale, To: attAcked},
		{From: attAcked, Event: evPublishIdle, To: attAcked},
		{From: attAcked, Event: evMarkAttention, To: attBellAcked},
		{From: attAcked, Event: evAck, To: attAcked},
		{From: attBell, Event: evPublishWorking, To: attBell},
		{From: attBell, Event: evPublishDoneFresh, To: attBell},
		{From: attBell, Event: evPublishDoneStale, To: attBell},
		{From: attBell, Event: evPublishIdle, To: attBell},
		{From: attBell, Event: evMarkAttention, To: attBell},
		{From: attBell, Event: evAck, To: attAcked},
		// BellAcked: every publish keeps the (acked) latch; only a Working
		// publish lifts the ack — re-arming the done bell — and only ack
		// clears the latch outright.
		{From: attBellAcked, Event: evPublishWorking, To: attBell},
		{From: attBellAcked, Event: evPublishDoneFresh, To: attBellAcked},
		{From: attBellAcked, Event: evPublishDoneStale, To: attBellAcked},
		{From: attBellAcked, Event: evPublishIdle, To: attBellAcked},
		{From: attBellAcked, Event: evMarkAttention, To: attBellAcked},
		{From: attBellAcked, Event: evAck, To: attAcked},
	},
})

// nextAttention applies an event through the checked table. The table is
// total, so the fallback is unreachable; it keeps a future table hole from
// silently latching the badge on.
func nextAttention(prev attentionState, ev attentionEvent) attentionState {
	if next, ok := attentionMachine.Next(prev, ev); ok {
		return next
	}
	return prev
}

// attentionEventFor reduces a live publish pair to the machine's event: a
// Done publish is a watched finish (evPublishDoneFresh) exactly when the
// previous publish was Working — the same de-duplication the old prev-map
// comparison performed for the bell.
func attentionEventFor(prevLive, newLive data.AgentState) attentionEvent {
	switch newLive {
	case data.StateWorking:
		return evPublishWorking
	case data.StateDone:
		if prevLive == data.StateWorking {
			return evPublishDoneFresh
		}
		return evPublishDoneStale
	default:
		return evPublishIdle
	}
}

// attentionFlagged reports whether the BEL attention latch is set — the
// render-side lookup for the "attention" badge.
func (s attentionState) attentionFlagged() bool {
	return s == attBell || s == attBellAcked
}

// badgeVisible reports whether any attention surface renders for the
// workspace: the latched or live done badge, or the attention latch. This is
// the old doneBadgeVisible boolean algebra as one enum test.
func (s attentionState) badgeVisible() bool {
	switch s {
	case attDoneFresh, attDoneLive, attBell, attBellAcked:
		return true
	default:
		return false
	}
}

// bellEligible reports whether a watched Working→Done edge in this state
// rings the done bell: Working and an unacked attention latch do; an ack
// suppresses it until a Working publish lifts it.
func (s attentionState) bellEligible() bool {
	return s == attWorking || s == attBell
}

// attentionMachineCheck formally verifies the badge table: every state
// reachable, the attention latch is raised only by MarkAttention and only
// cleared by ack, it survives every publish (the headline property the old
// donePending clear could not express), every watched Working→Done edge
// latches, and the acked suppression is lifted only by a Working publish.
func attentionMachineCheck() error {
	flagged := func(s attentionState) bool { return s.attentionFlagged() }
	return attentionMachine.Check(fsm.Checker[attentionState, attentionEvent]{
		Alphabet: []attentionEvent{
			evPublishWorking, evPublishDoneFresh, evPublishDoneStale,
			evPublishIdle, evMarkAttention, evAck,
		},
		AllDeclaredReachable: true,
		Edge: func(from attentionState, ev attentionEvent, to attentionState) error {
			if flagged(to) && !flagged(from) && ev != evMarkAttention {
				return errors.New("attention latch is raised only by MarkAttention")
			}
			if flagged(from) && !flagged(to) && ev != evAck {
				return fmt.Errorf("attention latch must survive event %v (got %v)", ev, to)
			}
			if from == attWorking && ev == evPublishDoneFresh && to != attDoneFresh {
				return errors.New("a watched Working→Done edge must latch DoneFresh")
			}
			if (from == attDoneFresh || from == attDoneLive || from == attAcked) &&
				ev == evPublishWorking && to != attWorking {
				return fmt.Errorf("a Working publish must re-arm from %v (got %v)", from, to)
			}
			if ev == evAck && to != attAcked {
				return errors.New("ack must always land Acked")
			}
			return nil
		},
	})
}

// attentionStateOf reads a workspace's badge state (absent = attQuiet).
func (m *Model) attentionStateOf(wsID string) attentionState {
	return m.attention[wsID]
}

// setAttentionState stores a workspace's badge state without dirtying —
// callers dirty only on real change.
func (m *Model) setAttentionState(wsID string, s attentionState) {
	if s == attQuiet {
		delete(m.attention, wsID)
		return
	}
	if m.attention == nil {
		m.attention = make(map[string]attentionState)
	}
	m.attention[wsID] = s
}

// stepAttention reduces one workspace's live publish to an event and steps
// its badge machine. It reports whether this was the watched Working→Done
// edge from a bell-eligible state (see bellEligible) — the only edge that
// may ring the done bell.
func (m *Model) stepAttention(wsID string, prevLive, newLive data.AgentState) bool {
	prev := m.attentionStateOf(wsID)
	ev := attentionEventFor(prevLive, newLive)
	next := nextAttention(prev, ev)
	if next != prev {
		m.setAttentionState(wsID, next)
	}
	return ev == evPublishDoneFresh && prev.bellEligible()
}

// ackDone marks a workspace's indicator as seen so it stops rendering — the
// live Done state, the latched done badge, and the BEL attention latch.
func (m *Model) ackDone(wsID string) {
	if wsID == "" {
		return
	}
	m.setAttentionState(wsID, nextAttention(m.attentionStateOf(wsID), evAck))
}

// doneBadgeVisible reports whether the attention badge renders for a
// workspace — a plain lookup on the badge machine's state.
func (m *Model) doneBadgeVisible(wsID string) bool {
	return m.attentionStateOf(wsID).badgeVisible()
}

// attentionFlagged reports whether the BEL attention latch is set for a
// workspace — the render-side lookup behind the "attention" badge.
func (m *Model) attentionFlagged(wsID string) bool {
	return m.attentionStateOf(wsID).attentionFlagged()
}

// MarkAttention raises the unacked attention latch for a workspace — the same
// surface the Working→Done edge uses (badge renders, `n` jumps to the row),
// cleared when the user views the row via ackDone. Agent-emitted BEL lands
// here: the agent escalated, so the workspace needs a look even though its
// activity state may still read Working. Repeated marks coalesce — the
// machine's Bell state is idempotent by design.
func (m *Model) MarkAttention(wsID string) {
	if wsID == "" || m.attentionStateOf(wsID) == attBell {
		return
	}
	m.setAttentionState(wsID, nextAttention(m.attentionStateOf(wsID), evMarkAttention))
	m.markContentDirty()
}
