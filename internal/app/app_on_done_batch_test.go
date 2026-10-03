package app

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
)

// stubOnDoneHookMap swaps the runOnDoneHook seam for a per-session
// recorder + error map, and restores it on cleanup.
func stubOnDoneHookMap(t *testing.T, errs map[string]error) *[]recordedOnDoneFire {
	t.Helper()
	orig := runOnDoneHook
	var recorded []recordedOnDoneFire
	runOnDoneHook = func(ws *data.Workspace, sessionName string, _ *workspacesvc.Service) error {
		recorded = append(recorded, recordedOnDoneFire{ws.Root, sessionName})
		return errs[sessionName]
	}
	t.Cleanup(func() { runOnDoneHook = orig })
	return &recorded
}

func onDoneResults(t *testing.T, msgs []tea.Msg) []messages.WorkspaceOnDoneResult {
	t.Helper()
	var out []messages.WorkspaceOnDoneResult
	for _, m := range msgs {
		if r, ok := m.(messages.WorkspaceOnDoneResult); ok {
			out = append(out, r)
		}
	}
	return out
}

func doneEdge(session string) agentStateTagChange {
	return agentStateTagChange{sessionName: session, state: activity.StateDone, prev: activity.StateWorking}
}

// TestOnDoneHookBatch_ContinuesAfterError: a dispatch error on one captured
// fire must not starve the later eligible hooks in the same batch — each is
// attempted once and every failure emits its own result.
func TestOnDoneHookBatch_ContinuesAfterError(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	s1 := "amux-" + string(ws.ID()) + "-tab-1"
	s2 := "amux-" + string(ws.ID()) + "-tab-2"
	s3 := "amux-" + string(ws.ID()) + "-tab-3"
	errSpawn := errors.New("spawn failed")
	fires := stubOnDoneHookMap(t, map[string]error{s1: errSpawn})
	app := onDoneTestApp(ws)

	cmd := app.onDoneHookCmd([]agentStateTagChange{doneEdge(s1), doneEdge(s2), doneEdge(s3)})
	if cmd == nil {
		t.Fatal("expected a hook cmd")
	}
	msgs := drainOnDoneCmd(cmd)
	if len(*fires) != 3 {
		t.Fatalf("hook attempted %d times, want all 3 eligible fires", len(*fires))
	}
	for i, session := range []string{s1, s2, s3} {
		if (*fires)[i].sessionName != session {
			t.Fatalf("fire %d = %q, want order preserved (%q)", i, (*fires)[i].sessionName, session)
		}
	}
	results := onDoneResults(t, msgs)
	if len(results) != 1 {
		t.Fatalf("results = %d, want exactly the one failure", len(results))
	}
	if results[0].SessionName != s1 || !errors.Is(results[0].Err, errSpawn) || results[0].Workspace == nil || results[0].Workspace.ID() != ws.ID() {
		t.Fatalf("failure result misattributed: %+v", results[0])
	}
}

// TestOnDoneHookBatch_MultipleErrors: two distinct dispatch failures each
// produce their own WorkspaceOnDoneResult — errors are never joined or
// collapsed across sessions, including two sessions of one workspace.
func TestOnDoneHookBatch_MultipleErrors(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	s1 := "amux-" + string(ws.ID()) + "-tab-1"
	s2 := "amux-" + string(ws.ID()) + "-tab-2"
	errA, errB := errors.New("spawn failed"), errors.New("queue full")
	stubOnDoneHookMap(t, map[string]error{s1: errA, s2: errB})
	app := onDoneTestApp(ws)

	msgs := drainOnDoneCmd(app.onDoneHookCmd([]agentStateTagChange{doneEdge(s1), doneEdge(s2)}))
	results := onDoneResults(t, msgs)
	if len(results) != 2 {
		t.Fatalf("results = %d, want both failures reported", len(results))
	}
	bySession := map[string]error{}
	for _, r := range results {
		bySession[r.SessionName] = r.Err
	}
	if !errors.Is(bySession[s1], errA) || !errors.Is(bySession[s2], errB) {
		t.Fatalf("per-session errors collapsed or misattributed: %+v", results)
	}
}

// TestOnDoneHookBatch_AllSuccessReturnsNil: a batch with zero failures emits
// no message at all — the nil-msg convention the result handler relies on.
func TestOnDoneHookBatch_AllSuccessReturnsNil(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	s1 := "amux-" + string(ws.ID()) + "-tab-1"
	s2 := "amux-" + string(ws.ID()) + "-tab-2"
	fires := stubOnDoneHookMap(t, nil)
	app := onDoneTestApp(ws)

	msgs := drainOnDoneCmd(app.onDoneHookCmd([]agentStateTagChange{doneEdge(s1), doneEdge(s2)}))
	if len(*fires) != 2 {
		t.Fatalf("hook attempted %d times, want 2", len(*fires))
	}
	for _, m := range msgs {
		if m != nil {
			t.Fatalf("successful batch emitted %T, want nil", m)
		}
	}
}

// TestOnDoneHookBatch_SteadyDoneDoesNotReplay: after a mixed batch (one
// failure, one success) the same Done scan re-applied produces no further
// fires — completed edges are never retried, failed or not.
func TestOnDoneHookBatch_SteadyDoneDoesNotReplay(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	s1 := "amux-" + string(ws.ID()) + "-tab-1"
	s2 := "amux-" + string(ws.ID()) + "-tab-2"
	errSpawn := errors.New("spawn failed")
	fires := stubOnDoneHookMap(t, map[string]error{s1: errSpawn})
	app := onDoneActivityApp(ws)
	t0 := time.Now()

	ownerScan := func(now time.Time, states map[string]*activity.SessionState) tmuxActivityResult {
		return tmuxActivityResult{
			ScannerOwner:       true,
			Now:                now,
			ActiveWorkspaceIDs: map[string]bool{},
			AgentStates:        map[string]activity.AgentState{},
			SessionStates:      states,
		}
	}
	// Both sessions observed Working, then both observed Done in one scan.
	drainCmd(app.applyTmuxActivityPayload(ownerScan(t0, map[string]*activity.SessionState{
		s1: {Initialized: true, Score: activity.ScoreThreshold, LastWorkingAt: t0},
		s2: {Initialized: true, Score: activity.ScoreThreshold, LastWorkingAt: t0},
	})))
	t1 := t0.Add(time.Second)
	drainCmd(app.applyTmuxActivityPayload(ownerScan(t1, map[string]*activity.SessionState{
		s1: {Initialized: true, LastWorkingAt: t1},
		s2: {Initialized: true, LastWorkingAt: t1},
	})))
	if len(*fires) != 2 {
		t.Fatalf("working→done edges must each fire once, got %#v", *fires)
	}

	// Steady Done: the same classification again must not retry either edge.
	drainCmd(app.applyTmuxActivityPayload(ownerScan(t1.Add(time.Second), map[string]*activity.SessionState{
		s1: {Initialized: true, LastWorkingAt: t1},
		s2: {Initialized: true, LastWorkingAt: t1},
	})))
	if len(*fires) != 2 {
		t.Fatalf("steady done refired: %#v", *fires)
	}
}

// TestOnDoneHookBatch_MixedErrorRouting: a trust rejection and a genuine
// spawn failure keep their distinct result-handler paths per session.
func TestOnDoneHookBatch_MixedErrorRouting(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws"}
	s1 := "amux-" + string(ws.ID()) + "-tab-1"
	s2 := "amux-" + string(ws.ID()) + "-tab-2"
	errTrust := &process.ScriptsNotTrustedError{Repo: ws.Repo, Command: "notify done", ConfigHash: "abc123"}
	errSpawn := errors.New("spawn failed")
	fires := stubOnDoneHookMap(t, map[string]error{s1: errTrust, s2: errSpawn})
	app := onDoneTestApp(ws)

	msgs := drainOnDoneCmd(app.onDoneHookCmd([]agentStateTagChange{doneEdge(s1), doneEdge(s2)}))
	if len(*fires) != 2 {
		t.Fatalf("hook attempted %d times, want 2", len(*fires))
	}
	results := onDoneResults(t, msgs)
	if len(results) != 2 {
		t.Fatalf("results = %d, want both failures", len(results))
	}
	var trustRes, spawnRes *messages.WorkspaceOnDoneResult
	for i := range results {
		switch results[i].SessionName {
		case s1:
			trustRes = &results[i]
		case s2:
			spawnRes = &results[i]
		}
	}
	if trustRes == nil || spawnRes == nil {
		t.Fatalf("missing per-session results: %+v", results)
	}
	if !errors.Is(trustRes.Err, process.ErrScriptsNotTrusted) {
		t.Fatalf("trust session result carries wrong error: %v", trustRes.Err)
	}
	if !errors.Is(spawnRes.Err, errSpawn) {
		t.Fatalf("spawn session result carries wrong error: %v", spawnRes.Err)
	}
}
