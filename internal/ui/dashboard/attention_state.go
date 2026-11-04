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
	// permission prompt) still needs a look. Cleared only by ackDone, or
	// superseded when the program itself reports blocked (the blocked
	// badge outranks the generic attention badge everywhere else, so it
	// outranks it here too).
	attBell
	// attBellAcked: the attention latch raised on top of an un-lifted ack
	// (ackDone during a Working window, then MarkAttention). The badge
	// renders exactly like attBell, but the done bell stays suppressed —
	// the ack is only lifted by a Working publish, matching the old
	// doneAcked flag's lifetime. A fresh blocked publish still latches
	// Blocked: the program's own needs-user signal outranks a bell ack.
	attBellAcked
	// attBlocked: a reported Blocked (or Error) publish latched the
	// needs-user badge. It outranks every other badge: the program itself
	// said it is waiting on a decision. Cleared by ackDone or by the
	// program moving on (a Working or Done publish); an Idle publish does
	// NOT clear it — the badge must survive state decay and the empty
	// re-publish an ownership handoff emits, exactly like the done latch.
	attBlocked
	// attBlockedAcked: the user viewed the blocked workspace; the badge is
	// suppressed until the state actually changes. A re-observation of the
	// same blocked publish parks here — the stored report is re-read
	// unchanged every scan, and re-raising it would re-bell every tick for
	// exactly as long as the user is reading the question. Only a fresh
	// blocked edge (a different live state in between) re-raises.
	attBlockedAcked
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
	// evPublishBlocked: a reported Blocked or Error live publish — the
	// Program Status Protocol's "waiting on the user" signal — observed as
	// an EDGE (the previous live state was neither). It shares one event
	// because both states carry the same badge semantics; the live enum
	// still distinguishes them for rendering.
	evPublishBlocked
	// evPublishBlockedHold: the live state is Blocked/Error and was
	// Blocked/Error in the previous publish too — a re-observation of the
	// same stored report, which every scan produces while the program
	// waits. It is not an edge: it never raises or re-raises a latch and
	// never rings.
	evPublishBlockedHold
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
		{From: attQuiet, Event: evPublishBlocked, To: attBlocked},
		{From: attQuiet, Event: evPublishBlockedHold, To: attQuiet},
		{From: attQuiet, Event: evMarkAttention, To: attBell},
		{From: attQuiet, Event: evAck, To: attAcked},
		{From: attWorking, Event: evPublishWorking, To: attWorking},
		{From: attWorking, Event: evPublishDoneFresh, To: attDoneFresh},
		{From: attWorking, Event: evPublishDoneStale, To: attDoneLive},
		{From: attWorking, Event: evPublishIdle, To: attQuiet},
		{From: attWorking, Event: evPublishBlocked, To: attBlocked},
		{From: attWorking, Event: evPublishBlockedHold, To: attWorking},
		{From: attWorking, Event: evMarkAttention, To: attBell},
		{From: attWorking, Event: evAck, To: attAcked},
		{From: attDoneFresh, Event: evPublishWorking, To: attWorking},
		{From: attDoneFresh, Event: evPublishDoneFresh, To: attDoneFresh},
		{From: attDoneFresh, Event: evPublishDoneStale, To: attDoneFresh},
		{From: attDoneFresh, Event: evPublishIdle, To: attDoneFresh},
		{From: attDoneFresh, Event: evPublishBlocked, To: attBlocked},
		{From: attDoneFresh, Event: evPublishBlockedHold, To: attDoneFresh},
		{From: attDoneFresh, Event: evMarkAttention, To: attBell},
		{From: attDoneFresh, Event: evAck, To: attAcked},
		{From: attDoneLive, Event: evPublishWorking, To: attWorking},
		{From: attDoneLive, Event: evPublishDoneFresh, To: attDoneFresh},
		{From: attDoneLive, Event: evPublishDoneStale, To: attDoneLive},
		{From: attDoneLive, Event: evPublishIdle, To: attQuiet},
		{From: attDoneLive, Event: evPublishBlocked, To: attBlocked},
		{From: attDoneLive, Event: evPublishBlockedHold, To: attDoneLive},
		{From: attDoneLive, Event: evMarkAttention, To: attBell},
		{From: attDoneLive, Event: evAck, To: attAcked},
		{From: attAcked, Event: evPublishWorking, To: attWorking},
		{From: attAcked, Event: evPublishDoneFresh, To: attAcked},
		{From: attAcked, Event: evPublishDoneStale, To: attAcked},
		{From: attAcked, Event: evPublishIdle, To: attAcked},
		{From: attAcked, Event: evPublishBlocked, To: attBlocked},
		{From: attAcked, Event: evPublishBlockedHold, To: attAcked},
		{From: attAcked, Event: evMarkAttention, To: attBellAcked},
		{From: attAcked, Event: evAck, To: attAcked},
		// Bell: every publish keeps the latch (an agent ringing mid-task
		// still needs a look); a fresh blocked publish supersedes it — the
		// program's own needs-user signal outranks the generic badge.
		{From: attBell, Event: evPublishWorking, To: attBell},
		{From: attBell, Event: evPublishDoneFresh, To: attBell},
		{From: attBell, Event: evPublishDoneStale, To: attBell},
		{From: attBell, Event: evPublishIdle, To: attBell},
		{From: attBell, Event: evPublishBlocked, To: attBlocked},
		{From: attBell, Event: evPublishBlockedHold, To: attBell},
		{From: attBell, Event: evMarkAttention, To: attBell},
		{From: attBell, Event: evAck, To: attAcked},
		// BellAcked: every publish keeps the (acked) latch; only a Working
		// publish lifts the ack — re-arming the done bell — and only ack
		// clears the latch outright. A fresh blocked publish latches
		// Blocked (the ack was for the bell, not the program's new
		// condition); a blocked re-observation changes nothing.
		{From: attBellAcked, Event: evPublishWorking, To: attBell},
		{From: attBellAcked, Event: evPublishDoneFresh, To: attBellAcked},
		{From: attBellAcked, Event: evPublishDoneStale, To: attBellAcked},
		{From: attBellAcked, Event: evPublishIdle, To: attBellAcked},
		{From: attBellAcked, Event: evPublishBlocked, To: attBlocked},
		{From: attBellAcked, Event: evPublishBlockedHold, To: attBellAcked},
		{From: attBellAcked, Event: evMarkAttention, To: attBellAcked},
		{From: attBellAcked, Event: evAck, To: attAcked},
		// Blocked: the program reported it is waiting on the user. A
		// re-observation stays latched; a Working or Done publish means the
		// program moved on and clears the latch; an Idle publish does NOT
		// clear it — the badge survives state decay and the empty
		// re-publish an ownership handoff emits (same survival the done
		// latch has); a manual attention mark is redundant (already the
		// strongest badge).
		{From: attBlocked, Event: evPublishWorking, To: attWorking},
		{From: attBlocked, Event: evPublishDoneFresh, To: attDoneFresh},
		{From: attBlocked, Event: evPublishDoneStale, To: attDoneLive},
		{From: attBlocked, Event: evPublishIdle, To: attBlocked},
		{From: attBlocked, Event: evPublishBlocked, To: attBlocked},
		{From: attBlocked, Event: evPublishBlockedHold, To: attBlocked},
		{From: attBlocked, Event: evMarkAttention, To: attBlocked},
		{From: attBlocked, Event: evAck, To: attBlockedAcked},
		// BlockedAcked: the user looked. A re-observation of the same
		// blocked publish parks here — the stored report is re-read
		// unchanged every scan, and re-raising it would re-bell every
		// tick; only a fresh blocked edge (a different live state in
		// between) re-raises. Working re-arms normally; Idle keeps the ack
		// suppression (the done bell stays muted until the next Working
		// window, matching attAcked).
		{From: attBlockedAcked, Event: evPublishWorking, To: attWorking},
		{From: attBlockedAcked, Event: evPublishDoneFresh, To: attDoneFresh},
		{From: attBlockedAcked, Event: evPublishDoneStale, To: attDoneLive},
		{From: attBlockedAcked, Event: evPublishIdle, To: attAcked},
		{From: attBlockedAcked, Event: evPublishBlocked, To: attBlocked},
		{From: attBlockedAcked, Event: evPublishBlockedHold, To: attBlockedAcked},
		{From: attBlockedAcked, Event: evMarkAttention, To: attBlocked},
		{From: attBlockedAcked, Event: evAck, To: attBlockedAcked},
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
// comparison performed for the bell. A reported Blocked or Error publish is
// the needs-user event, split by whether it is an edge (the previous live
// state was neither) or a re-observation of the same condition: the stored
// report is re-read unchanged on every scan, and only a genuine edge may
// raise or re-raise the latch or ring.
func attentionEventFor(prevLive, newLive data.AgentState) attentionEvent {
	switch newLive {
	case data.StateWorking:
		return evPublishWorking
	case data.StateDone:
		if prevLive == data.StateWorking {
			return evPublishDoneFresh
		}
		return evPublishDoneStale
	case data.StateBlocked, data.StateError:
		if prevLive == data.StateBlocked || prevLive == data.StateError {
			return evPublishBlockedHold
		}
		return evPublishBlocked
	default:
		return evPublishIdle
	}
}

// attentionFlagged reports whether the BEL attention latch is set — the
// render-side lookup for the "attention" badge.
func (s attentionState) attentionFlagged() bool {
	return s == attBell || s == attBellAcked
}

// blockedFlagged reports whether the reported-blocked latch is set and
// un-acked — the render-side lookup for the "blocked"/"error" badge.
func (s attentionState) blockedFlagged() bool {
	return s == attBlocked
}

// badgeVisible reports whether any attention surface renders for the
// workspace: the latched or live done badge, the attention latch, or the
// un-acked blocked latch. This is the old doneBadgeVisible boolean algebra
// as one enum test plus the blocked tier.
func (s attentionState) badgeVisible() bool {
	switch s {
	case attDoneFresh, attDoneLive, attBell, attBellAcked, attBlocked:
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
// cleared by ack or superseded by a fresh blocked latch (the stronger
// badge absorbs it — blocked outranks attention everywhere else, so it
// outranks it here), it survives every other publish (the headline
// property the old donePending clear could not express), every watched
// Working→Done edge latches, the acked suppression is lifted only by a
// Working publish, the blocked latch is raised only by a fresh blocked
// publish (or a redundant manual mark while already latched), is cleared
// only by ack or the program moving on (Working/Done — an Idle decay
// keeps it, like the done latch), every fresh blocked publish re-raises
// the badge even after an ack, and a blocked re-observation neither raises
// nor re-raises anything anywhere.
func attentionMachineCheck() error {
	flagged := func(s attentionState) bool { return s.attentionFlagged() }
	blocked := func(s attentionState) bool { return s.blockedFlagged() }
	return attentionMachine.Check(fsm.Checker[attentionState, attentionEvent]{
		Alphabet: []attentionEvent{
			evPublishWorking, evPublishDoneFresh, evPublishDoneStale,
			evPublishIdle, evPublishBlocked, evPublishBlockedHold,
			evMarkAttention, evAck,
		},
		AllDeclaredReachable: true,
		Edge: func(from attentionState, ev attentionEvent, to attentionState) error {
			if flagged(to) && !flagged(from) && ev != evMarkAttention {
				return errors.New("attention latch is raised only by MarkAttention")
			}
			if flagged(from) && !flagged(to) && ev != evAck && ev != evPublishBlocked {
				return fmt.Errorf("attention latch must survive event %v (got %v)", ev, to)
			}
			if from == attWorking && ev == evPublishDoneFresh && to != attDoneFresh {
				return errors.New("a watched Working→Done edge must latch DoneFresh")
			}
			if (from == attDoneFresh || from == attDoneLive || from == attAcked) &&
				ev == evPublishWorking && to != attWorking {
				return fmt.Errorf("a Working publish must re-arm from %v (got %v)", from, to)
			}
			if ev == evAck && to != attAcked && to != attBlockedAcked {
				return errors.New("ack must land Acked (or BlockedAcked from Blocked)")
			}
			if blocked(to) && !blocked(from) && ev != evPublishBlocked && ev != evMarkAttention {
				return errors.New("blocked latch is raised only by a fresh blocked publish or an explicit attention mark")
			}
			if blocked(from) && !blocked(to) && ev != evAck &&
				ev != evPublishWorking &&
				ev != evPublishDoneFresh && ev != evPublishDoneStale {
				return fmt.Errorf("blocked latch must survive event %v (got %v)", ev, to)
			}
			if ev == evPublishBlocked && to != attBlocked {
				return fmt.Errorf("a fresh blocked publish must latch Blocked, got %v", to)
			}
			if from == attBlockedAcked && ev == evPublishBlocked && to != attBlocked {
				return errors.New("a fresh blocked publish must re-raise after an ack")
			}
			if ev == evPublishBlockedHold &&
				((blocked(to) && !blocked(from)) || flagged(to) != flagged(from)) {
				return errors.New("a blocked re-observation must be inert (no raise, no re-raise)")
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
// may ring the done bell — and whether a blocked latch was newly raised
// (entered attBlocked), the edge that always rings the needs-user bell: the
// program itself reported it is waiting, so no user preference gates it.
func (m *Model) stepAttention(wsID string, prevLive, newLive data.AgentState) (doneEdge, blockedEdge bool) {
	prev := m.attentionStateOf(wsID)
	ev := attentionEventFor(prevLive, newLive)
	next := nextAttention(prev, ev)
	if next != prev {
		m.setAttentionState(wsID, next)
	}
	return ev == evPublishDoneFresh && prev.bellEligible(),
		ev == evPublishBlocked && next == attBlocked && !prev.blockedFlagged()
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

// blockedBadgeVisible reports whether the un-acked reported-blocked latch
// renders for a workspace — the "needs you" badge from the Program Status
// Protocol.
func (m *Model) blockedBadgeVisible(wsID string) bool {
	return m.attentionStateOf(wsID).blockedFlagged()
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
