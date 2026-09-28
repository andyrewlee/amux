package dashboard

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// Rows in makeAttentionModel (two projects, one workspace each):
//
//	0 Home, 1 Spacer, 2 projA (main ws A), 3 ws-a, 4 Create, 5 Spacer,
//	6 projB (main ws B), 7 ws-b, 8 Create, 9 Spacer
func makeAttentionModel(t *testing.T) (*Model, *data.Workspace, *data.Workspace) {
	t.Helper()
	m := New()
	m.SetSize(30, 30)

	mkProject := func(name string) data.Project {
		main := data.Workspace{Name: name, Branch: "main", Repo: "/" + name, Root: "/" + name}
		feat := data.Workspace{
			Name: name + "-feat", Branch: "feat", Repo: "/" + name,
			Root: "/" + name + "/.amux/workspaces/feat",
		}
		return data.Project{
			Name:       name,
			Path:       "/" + name,
			Workspaces: []data.Workspace{main, feat},
		}
	}
	m.SetProjects([]data.Project{mkProject("projA"), mkProject("projB")})
	_ = m.View()

	wsA := &m.projects[0].Workspaces[1]
	wsB := &m.projects[1].Workspaces[1]
	return m, wsA, wsB
}

func rowTypeAt(m *Model, idx int) RowType { return m.rows[idx].Type }

func TestJumpToNextAttention_LandsOnNext(t *testing.T) {
	m, wsA, wsB := makeAttentionModel(t)
	// Cursor starts on row 0 (Home). ws-a's badge is live Done; ws-b's is the
	// latched Working→Done edge (donePending survives the state decaying).
	m.SetAgentStates(map[string]data.AgentState{
		string(wsA.ID()): data.StateDone,
		string(wsB.ID()): data.StateWorking,
	})
	m.SetAgentStates(map[string]data.AgentState{
		string(wsA.ID()): data.StateDone,
		string(wsB.ID()): data.StateDone,
	})

	if cmd := m.JumpToNextAttention(); cmd == nil {
		t.Fatal("expected a jump cmd")
	}
	if m.cursor != 3 {
		t.Fatalf("cursor = %d, want 3 (ws-a row)", m.cursor)
	}
	// Landing acks ws-a — a second jump must move to ws-b, not return.
	if cmd := m.JumpToNextAttention(); cmd == nil {
		t.Fatal("expected second jump cmd")
	}
	if m.cursor != 7 {
		t.Fatalf("cursor = %d, want 7 (ws-b row)", m.cursor)
	}
}

func TestJumpToNextAttention_Wraps(t *testing.T) {
	m, wsA, _ := makeAttentionModel(t)
	m.SetAgentStates(map[string]data.AgentState{string(wsA.ID()): data.StateDone})

	m.cursor = 7 // ws-b row, past the only attention row
	if cmd := m.JumpToNextAttention(); cmd == nil {
		t.Fatal("expected a jump cmd")
	}
	if m.cursor != 3 {
		t.Fatalf("cursor = %d, want 3 after wrap", m.cursor)
	}
}

func TestJumpToNextAttention_SkipsAckedAndNonSelectable(t *testing.T) {
	m, wsA, _ := makeAttentionModel(t)
	m.SetAgentStates(map[string]data.AgentState{string(wsA.ID()): data.StateDone})
	m.ackDone(string(wsA.ID()))

	if cmd := m.JumpToNextAttention(); cmd != nil {
		t.Fatal("acked row must not be a jump target")
	}
	if m.cursor != 0 {
		t.Fatalf("cursor moved to %d with nothing needing attention", m.cursor)
	}
}

func TestJumpToNextAttention_ProjectRowBadge(t *testing.T) {
	m, _, _ := makeAttentionModel(t)
	// The project row's ActivityWorkspaceID is its main workspace's ID —
	// a Done main branch makes the PROJECT row the jump target.
	mainWS := m.projects[0].Workspaces[0]
	m.SetAgentStates(map[string]data.AgentState{string(mainWS.ID()): data.StateDone})

	cmd := m.JumpToNextAttention()
	if cmd == nil {
		t.Fatal("expected a jump cmd")
	}
	if m.cursor != 2 || rowTypeAt(m, 2) != RowProject {
		t.Fatalf("cursor = %d (%v), want row 2 RowProject", m.cursor, rowTypeAt(m, m.cursor))
	}
	msg := cmd()
	if _, ok := msg.(messages.WorkspaceActivated); !ok {
		t.Fatalf("cmd emitted %T, want WorkspaceActivated (activation rides along)", msg)
	}
}

func TestJumpToNextAttention_NoopWhenClean(t *testing.T) {
	m, _, _ := makeAttentionModel(t)
	m.cursor = 3
	if cmd := m.JumpToNextAttention(); cmd != nil {
		t.Fatal("no badges → nil cmd")
	}
	if m.cursor != 3 {
		t.Fatalf("cursor moved to %d with no attention items", m.cursor)
	}
}

func TestJumpToNextAttention_SelfIsTarget(t *testing.T) {
	m, wsA, _ := makeAttentionModel(t)
	m.SetAgentStates(map[string]data.AgentState{string(wsA.ID()): data.StateDone})
	m.cursor = 3 // already on ws-a; wrap-around lands back and acks it
	if cmd := m.JumpToNextAttention(); cmd == nil {
		t.Fatal("sole attention row under cursor is still a valid target")
	}
	if m.cursor != 3 {
		t.Fatalf("cursor = %d, want to stay on 3", m.cursor)
	}
	if m.doneBadgeVisible(string(wsA.ID())) {
		t.Fatal("landing should ack the badge")
	}
}

func TestMarkAttention_BadgeAndJump(t *testing.T) {
	m, wsA, _ := makeAttentionModel(t)

	// A bell on an idle workspace raises the badge and makes it a jump target.
	m.MarkAttention(string(wsA.ID()))
	if !m.doneBadgeVisible(string(wsA.ID())) {
		t.Fatal("MarkAttention did not surface the badge")
	}
	if cmd := m.JumpToNextAttention(); cmd == nil {
		t.Fatal("marked workspace should be a jump target")
	}
	if m.cursor != 3 {
		t.Fatalf("cursor = %d, want 3 (ws-a row)", m.cursor)
	}
	// Landing acks — badge clears.
	if m.doneBadgeVisible(string(wsA.ID())) {
		t.Fatal("viewing the row should ack the attention latch")
	}
}

func TestMarkAttention_SurvivesWorkingPublish(t *testing.T) {
	m, wsA, _ := makeAttentionModel(t)

	// The headline case: a permission-prompt bell while the agent still reads
	// Working. SetAgentStates deletes donePending on Working — attentionPending
	// must not ride that clear.
	m.MarkAttention(string(wsA.ID()))
	m.SetAgentStates(map[string]data.AgentState{string(wsA.ID()): data.StateWorking})
	m.SetAgentStates(map[string]data.AgentState{string(wsA.ID()): data.StateWorking})
	if !m.doneBadgeVisible(string(wsA.ID())) {
		t.Fatal("Working publishes swallowed the attention latch")
	}
}

func TestMarkAttention_CoalescesAndAckClears(t *testing.T) {
	m, wsA, _ := makeAttentionModel(t)

	m.MarkAttention(string(wsA.ID()))
	m.MarkAttention(string(wsA.ID())) // second bell while flagged = no-op edge
	if !m.doneBadgeVisible(string(wsA.ID())) {
		t.Fatal("latch should hold across repeated marks")
	}
	m.ackDone(string(wsA.ID()))
	if m.doneBadgeVisible(string(wsA.ID())) {
		t.Fatal("ackDone must clear the attention latch")
	}
}

func TestMarkAttention_EmptyIDIgnored(t *testing.T) {
	m, _, _ := makeAttentionModel(t)
	m.MarkAttention("")
	if len(m.attentionPending) != 0 {
		t.Fatal("empty workspace ID must not latch")
	}
}

func TestMarkAttention_AfterAckStillFlags(t *testing.T) {
	m, wsA, _ := makeAttentionModel(t)

	// User views the row (acked), THEN the agent rings — the fresh bell must
	// surface despite the stale doneAcked latch.
	m.ackDone(string(wsA.ID()))
	m.MarkAttention(string(wsA.ID()))
	if !m.doneBadgeVisible(string(wsA.ID())) {
		t.Fatal("bell after ack must still surface attention")
	}
	if cmd := m.JumpToNextAttention(); cmd == nil {
		t.Fatal("post-ack bell must remain a jump target")
	}
}
