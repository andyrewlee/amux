package process

import (
	"errors"
	"testing"
)

// TestRunScriptHosted_CreateCollisionRetriesNextSuffix pins the allocation
// contract: when the candidate name is taken between the find and the create
// — the concurrent-start race Ensure used to paper over — the loser must not
// adopt and re-tag the winner's session. It retries the next free suffix and
// reports exactly one newly-created session.
func TestRunScriptHosted_CreateCollisionRetriesNextSuffix(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "concurrent")
	wsID := string(ws.MetadataID())
	base := "amux-ws-" + wsID + "-run"

	// A competing starter claims the candidate inside Create — after our
	// find, before our session exists — the exact gap where Ensure used to
	// adopt the winner's session and re-stamp its tags.
	sniped := false
	host.onCreate = func(h *fakeRunSessionHost, name string) {
		if sniped {
			return
		}
		sniped = true
		h.sessions[name] = &fakeRunSession{
			alive:    true,
			exitCode: -1,
			meta:     RunSessionMeta{WorkspaceID: wsID, CreatedAt: 111},
			cmd:      "winner cmd",
		}
	}

	if _, err := runner.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}

	// The winner's session is untouched — same cmd, same creation stamp —
	// and ours landed on the next free suffix.
	winner := host.sessions[base]
	if winner == nil || winner.cmd != "winner cmd" || winner.meta.CreatedAt != 111 {
		t.Fatalf("winning session was re-stamped by the loser: %+v", winner)
	}
	ours := host.sessions[base+"-2"]
	if ours == nil {
		t.Fatal("losing start did not retry the next free suffix")
	}
	if ours.cmd != "make dev" || ours.meta.CreatedAt == 111 {
		t.Fatalf("retried session = %+v, want our cmd and a fresh stamp", ours)
	}
	if len(host.ensured) != 1 || host.ensured[0] != base+"-2" {
		t.Fatalf("ensured = %v, want exactly [%s-2] (collision must not count)", host.ensured, base)
	}
}

// TestRunScriptHosted_ChronologicalNewest pins newest-session selection on
// creation time: a killed middle slot is reused by the next create, so the
// recreated -2 is NEWER than the surviving -3 even though its suffix sorts
// earlier. The output pick and the attach target must both take it.
func TestRunScriptHosted_ChronologicalNewest(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "concurrent")
	wsID := string(ws.MetadataID())
	base := "amux-ws-" + wsID + "-run"

	// Three sessions with explicit old stamps; -2 dies and is recreated.
	for i, name := range []string{base, base + "-2", base + "-3"} {
		host.sessions[name] = &fakeRunSession{
			alive:    true,
			exitCode: -1,
			meta:     RunSessionMeta{WorkspaceID: wsID, CreatedAt: int64(100 + i*100)},
			tail:     "tail-" + name,
		}
	}
	_ = host.Kill(base + "-2")

	if _, err := runner.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	recreated := host.sessions[base+"-2"]
	if recreated == nil {
		t.Fatal("recreate did not land on the freed -2 slot")
	}
	if recreated.meta.CreatedAt <= 300 {
		t.Fatalf("recreated session stamp = %d, want newer than the surviving -3", recreated.meta.CreatedAt)
	}
	host.sessions[base+"-2"].tail = "tail-recreated"

	if target, ok := runner.RunScriptAttachTarget(ws); !ok || target != base+"-2" {
		t.Fatalf("RunScriptAttachTarget() = %q (ok=%v), want %q — newest by creation, not suffix", target, ok, base+"-2")
	}
	if out := runner.RunScriptOutput(ws, 5); out != "tail-recreated" {
		t.Fatalf("RunScriptOutput() = %q, want the recreated session's tail", out)
	}
}

// TestRunSessionsHosted_PostRestartSweep pins the bounded discovery probe: a
// restarted runner has an empty seen-set, so a config-gone workspace with a
// surviving detached session still gets exactly one sweep — enough to
// rediscover it — instead of skipping forever.
func TestRunSessionsHosted_PostRestartSweep(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Scripts.Run = "" // config removed — the gate would skip unseen
	wsID := string(ws.MetadataID())

	// The detached session outlived the amux restart: the seen-set that used
	// to keep the sweep running was process-local and is gone.
	host.sessions["amux-ws-"+wsID+"-run"] = &fakeRunSession{
		alive:    true,
		exitCode: -1,
		meta:     RunSessionMeta{WorkspaceID: wsID, CreatedAt: 42},
	}

	sessions, alive, _ := runner.runSessionsHosted(ws)
	if len(sessions) != 1 {
		t.Fatalf("runSessionsHosted() found %d sessions, want the 1 surviving session rediscovered", len(sessions))
	}
	if !alive {
		t.Fatal("surviving session reported dead")
	}
	// Rediscovery populates the seen-set, so the sweep keeps running while
	// the session lives — the gate only ever bought one probe.
	if len(host.findCalls) == 0 {
		t.Fatal("bounded sweep never ran")
	}
	if _, alive, _ := runner.runSessionsHosted(ws); !alive {
		t.Fatal("session lost again after rediscovery — seen-set should keep the sweep open")
	}
}

// TestRunSessionsHosted_BatchedStatusQuery pins the sweep's O(1) query
// contract: a host offering RunSessionStatusBatcher gets exactly one
// StatusAll call regardless of session count — never a per-session Status
// probe — and a vanished name filters out the same way it did serially.
func TestRunSessionsHosted_BatchedStatusQuery(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := &batchRunSessionHost{fakeRunSessionHost: newFakeRunSessionHost()}
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "concurrent")
	wsID := string(ws.MetadataID())
	base := "amux-ws-" + wsID + "-run"

	for i, name := range []string{base, base + "-2"} {
		host.sessions[name] = &fakeRunSession{
			alive:    true,
			exitCode: -1,
			meta:     RunSessionMeta{WorkspaceID: wsID, CreatedAt: int64(100 + i)},
		}
	}
	host.sessions[base+"-3"] = &fakeRunSession{
		alive:    false,
		exitCode: 7,
		meta:     RunSessionMeta{WorkspaceID: wsID, CreatedAt: 300},
	}
	// A name the find returned that is gone by status time — the batch maps
	// absent names to missing entries, the same !exists skip the serial loop
	// made.
	host.findExtra = []string{base + "-vanished"}

	sessions, alive, lastExit := runner.runSessionsHosted(ws)
	if host.statusAllCalls != 1 {
		t.Fatalf("StatusAll calls = %d, want exactly one batched query", host.statusAllCalls)
	}
	if host.statusCalls != 0 {
		t.Fatalf("per-session Status ran %d times despite the batch capability", host.statusCalls)
	}
	if len(sessions) != 3 {
		t.Fatalf("sessions = %d, want 3 (the vanished name filtered)", len(sessions))
	}
	if !alive {
		t.Fatal("live sessions not reported")
	}
	if lastExit != 7 {
		t.Fatalf("lastExit = %d, want the dead session's 7", lastExit)
	}

	// A batch error surfaces as an empty result — consistent with the find
	// error path — not a silent serial retry.
	host.statusAllErr = errors.New("tmux down")
	sessions, alive, lastExit = runner.runSessionsHosted(ws)
	if sessions != nil || alive || lastExit != -1 {
		t.Fatalf("batch error got %v/%v/%d, want empty/false/-1", sessions, alive, lastExit)
	}
	if host.statusCalls != 0 {
		t.Fatalf("batch error fell back to per-session Status (%d calls)", host.statusCalls)
	}
}

// TestNewestRunSessionFirst covers the ordering helper's tiebreaks: creation
// stamp desc, name desc within the same second, find order for unstamped
// hosts.
func TestNewestRunSessionFirst(t *testing.T) {
	refs := []RunSessionRef{
		{Name: "a-run-2", CreatedAt: 50},
		{Name: "a-run-10", CreatedAt: 50},
		{Name: "a-run", CreatedAt: 90},
		{Name: "a-run-3", CreatedAt: 70},
	}
	got := newestRunSessionFirst(refs)
	want := []string{"a-run", "a-run-3", "a-run-10", "a-run-2"}
	for i, ref := range got {
		if ref.Name != want[i] {
			t.Fatalf("newestRunSessionFirst()[%d] = %q, want %q (full order %v)", i, ref.Name, want[i], got)
		}
	}

	// Unstamped hosts fall back to numeric-suffix order — the "largest -N is
	// newest" approximation the old find-order scan made, minus its lexical
	// defect (-10 correctly outranks -2).
	unstamped := []RunSessionRef{{Name: "a-run"}, {Name: "a-run-2"}, {Name: "a-run-10"}}
	got = newestRunSessionFirst(unstamped)
	wantFallback := []string{"a-run-10", "a-run-2", "a-run"}
	for i, ref := range got {
		if ref.Name != wantFallback[i] {
			t.Fatalf("unstamped order[%d] = %q, want %q", i, ref.Name, wantFallback[i])
		}
	}
}
