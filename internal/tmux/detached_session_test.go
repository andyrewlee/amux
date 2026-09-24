package tmux

import (
	"strings"
	"testing"
	"time"
)

// waitForSessionStatus polls RunSessionStatus until pred holds or the deadline
// passes — tmux reports pane_dead asynchronously after the command exits.
func waitForSessionStatus(t *testing.T, sessionName string, opts Options, pred func(exists, alive bool, exitCode int) bool) (bool, bool, int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		exists, alive, exitCode, err := RunSessionStatus(sessionName, opts)
		if err == nil && pred(exists, alive, exitCode) {
			return exists, alive, exitCode
		}
		if time.Now().After(deadline) {
			return exists, alive, exitCode
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestEnsureDetachedSessionCreatesAndTags(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)
	dir := t.TempDir()

	err := EnsureDetachedSession("run-basic", dir, "echo hello-from-run", nil, opts, SessionTags{
		WorkspaceID: "ws-1",
		Type:        "run",
		CreatedAt:   time.Now().Unix(),
		InstanceID:  "test-inst",
	})
	if err != nil {
		t.Fatalf("EnsureDetachedSession() error = %v", err)
	}

	// Idempotent: a second Ensure for the same name is a no-op.
	if err := EnsureDetachedSession("run-basic", dir, "echo other", nil, opts, SessionTags{}); err != nil {
		t.Fatalf("EnsureDetachedSession() second call error = %v", err)
	}

	exists, alive, _, err := RunSessionStatus("run-basic", opts)
	if err != nil || !exists {
		t.Fatalf("RunSessionStatus() = (%v, %v, err=%v), want exists", exists, alive, err)
	}

	// remain-on-exit keeps the dead pane inspectable after the echo finishes.
	exists, alive, exitCode := waitForSessionStatus(t, "run-basic", opts,
		func(exists, alive bool, _ int) bool { return exists && !alive })
	if !exists || alive {
		t.Fatalf("post-exit status = (exists=%v, alive=%v), want (true, false)", exists, alive)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0 for successful echo", exitCode)
	}

	out, ok := RunSessionTail("run-basic", 20, opts)
	if !ok || out == "" {
		t.Fatal("RunSessionTail() empty after remain-on-exit")
	}
	if !strings.Contains(out, "hello-from-run") {
		t.Fatalf("RunSessionTail() = %q, want the run's output", out)
	}
	if !strings.Contains(out, "Pane is dead") {
		t.Fatalf("RunSessionTail() = %q, want the remain-on-exit banner", out)
	}
	got, err := FindRunSessions("ws-1", "test-inst", opts)
	if err != nil {
		t.Fatalf("FindRunSessions() error = %v", err)
	}
	if len(got) != 1 || got[0] != "run-basic" {
		t.Fatalf("FindRunSessions() = %v, want [run-basic]", got)
	}
}

func TestRunSessionStatusReportsNonzeroExit(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	if err := EnsureDetachedSession("run-fail", t.TempDir(), "exit 7", nil, opts,
		SessionTags{WorkspaceID: "ws-1", Type: "run"}); err != nil {
		t.Fatalf("EnsureDetachedSession() error = %v", err)
	}
	_, alive, exitCode := waitForSessionStatus(t, "run-fail", opts,
		func(exists, alive bool, code int) bool { return exists && !alive && code == 7 })
	if alive || exitCode != 7 {
		t.Fatalf("status = (alive=%v, exit=%d), want dead with exit 7", alive, exitCode)
	}
}

func TestFindRunSessionsScopesTagsAndNamespace(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)
	dir := t.TempDir()
	tags := func(ws, typ, inst string) SessionTags {
		return SessionTags{WorkspaceID: ws, Type: typ, InstanceID: inst, CreatedAt: time.Now().Unix()}
	}
	for _, s := range []struct{ name, ws, typ, inst string }{
		{"find-a", "ws-a", "run", "ns1.aaa"},
		{"find-b", "ws-a", "run", "ns1.bbb"},   // same namespace, different launch
		{"find-c", "ws-b", "run", "ns1.aaa"},   // other workspace
		{"find-d", "ws-a", "agent", "ns1.aaa"}, // other session type
		{"find-e", "ws-a", "run", "ns2.aaa"},   // foreign state root
	} {
		if err := EnsureDetachedSession(s.name, dir, "sleep 300", nil, opts, tags(s.ws, s.typ, s.inst)); err != nil {
			t.Fatalf("EnsureDetachedSession(%s) error = %v", s.name, err)
		}
	}
	got, err := FindRunSessions("ws-a", "ns1.zzz", opts)
	if err != nil {
		t.Fatalf("FindRunSessions() error = %v", err)
	}
	if len(got) != 2 || got[0] != "find-a" || got[1] != "find-b" {
		t.Fatalf("FindRunSessions() = %v, want [find-a find-b]", got)
	}
}

func TestDetachedSessionKillRemovesFromFindAndStatus(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	if err := EnsureDetachedSession("run-kill", t.TempDir(), "sleep 300", nil, opts,
		SessionTags{WorkspaceID: "ws-9", Type: "run"}); err != nil {
		t.Fatalf("EnsureDetachedSession() error = %v", err)
	}
	if err := KillSession("run-kill", opts); err != nil {
		t.Fatalf("KillSession() error = %v", err)
	}
	exists, _, _, err := RunSessionStatus("run-kill", opts)
	if err != nil {
		t.Fatalf("RunSessionStatus() error = %v", err)
	}
	if exists {
		t.Fatal("session still exists after KillSession")
	}
	if got, _ := FindRunSessions("ws-9", "", opts); len(got) != 0 {
		t.Fatalf("FindRunSessions() = %v after kill, want empty", got)
	}
}
