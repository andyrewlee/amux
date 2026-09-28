package process

import (
	"testing"
)

// TestRunSessionList_OrdersByNumericSuffix pins the picker's ordering
// contract: Find returns lexical order (run-10 before run-2), so the entry
// list must re-sort numerically — base first, then -2, -9, -10 — and carry
// per-session liveness and exit status.
func TestRunSessionList_OrdersByNumericSuffix(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "concurrent")
	base := "amux-ws-" + string(ws.ID()) + "-run"

	meta := RunSessionMeta{WorkspaceID: string(ws.ID())}
	host.sessions[base] = &fakeRunSession{alive: true, exitCode: -1, meta: meta}
	host.sessions[base+"-2"] = &fakeRunSession{alive: false, exitCode: 7, meta: meta}
	host.sessions[base+"-9"] = &fakeRunSession{alive: false, exitCode: 0, meta: meta}
	host.sessions[base+"-10"] = &fakeRunSession{alive: false, exitCode: 1, meta: meta}

	entries := runner.RunSessionList(ws)
	if len(entries) != 4 {
		t.Fatalf("entries = %d, want 4", len(entries))
	}
	wantNames := []string{base, base + "-2", base + "-9", base + "-10"}
	for i, want := range wantNames {
		if entries[i].Name != want {
			t.Fatalf("entries[%d].Name = %q, want %q (full list: %+v)", i, entries[i].Name, want, entries)
		}
		if entries[i].Ordinal != []int{1, 2, 9, 10}[i] {
			t.Fatalf("entries[%d].Ordinal = %d", i, entries[i].Ordinal)
		}
	}
	if !entries[0].Alive || entries[0].ExitCode != -1 {
		t.Fatalf("base entry = %+v, want alive exit=-1", entries[0])
	}
	if entries[1].Alive || entries[1].ExitCode != 7 {
		t.Fatalf("run-2 entry = %+v, want dead exit=7", entries[1])
	}
	if entries[2].Alive || entries[2].ExitCode != 0 {
		t.Fatalf("run-9 entry = %+v, want dead exit=0", entries[2])
	}
}

// TestRunSessionList_SkipsVanishedSession covers a session dying between the
// Find sweep and its Status read — the entry drops out rather than reporting
// a phantom row.
func TestRunSessionList_SkipsVanishedSession(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "concurrent")
	base := "amux-ws-" + string(ws.ID()) + "-run"
	meta := RunSessionMeta{WorkspaceID: string(ws.ID())}
	host.sessions[base] = &fakeRunSession{alive: true, exitCode: -1, meta: meta}
	host.sessions[base+"-2"] = &fakeRunSession{alive: false, exitCode: 0, meta: meta}
	host.findExtra = []string{base + "-3"} // Find reports it; Status says !exists

	entries := runner.RunSessionList(ws)
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want the 2 real sessions (phantom skipped)", entries)
	}
	for _, e := range entries {
		if e.Name == base+"-3" {
			t.Fatalf("vanished session %q survived enumeration", e.Name)
		}
	}
}

// TestRunSessionList_NoHostOrNil covers the unhosted/early-outs.
func TestRunSessionList_NoHostOrNil(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	if got := runner.RunSessionList(newHostedWorkspace(t, "concurrent")); got != nil {
		t.Fatalf("unhosted list = %v, want nil", got)
	}
	runner.SetRunHost(newFakeRunSessionHost())
	if got := runner.RunSessionList(nil); got != nil {
		t.Fatalf("nil workspace list = %v, want nil", got)
	}
}

// TestRunSessionTailAndAlive_NameAddressed pins the picker's pinned-session
// reads against the fake host.
func TestRunSessionTailAndAlive_NameAddressed(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "concurrent")
	base := "amux-ws-" + string(ws.ID()) + "-run"
	meta := RunSessionMeta{WorkspaceID: string(ws.ID())}
	host.sessions[base] = &fakeRunSession{alive: true, exitCode: -1, meta: meta, tail: "live-tail"}
	host.sessions[base+"-2"] = &fakeRunSession{alive: false, exitCode: 3, meta: meta, tail: "dead-tail"}

	if got := runner.RunSessionTail(base, 10); got != "live-tail" {
		t.Fatalf("RunSessionTail = %q, want live-tail", got)
	}
	if !runner.RunSessionAlive(base) {
		t.Fatal("RunSessionAlive = false for a live session")
	}
	if runner.RunSessionAlive(base + "-2") {
		t.Fatal("RunSessionAlive = true for a dead session")
	}
	if runner.RunSessionAlive(base + "-99") {
		t.Fatal("RunSessionAlive = true for a nonexistent session")
	}
}
