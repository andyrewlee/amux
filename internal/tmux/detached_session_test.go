package tmux

import (
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/testutil"
)

// waitForSessionStatus polls RunSessionStatus until pred holds or the deadline
// passes — tmux reports pane_dead asynchronously after the command exits.
// The 10s bound absorbs the slow pane_dead reporting observed on the apt CI
// lane under load (this helper's callers produced that lane's flakes).
func waitForSessionStatus(t *testing.T, sessionName string, opts Options, pred func(exists, alive bool, exitCode int) bool) (bool, bool, int) {
	t.Helper()
	type status struct {
		exists, alive bool
		exitCode      int
	}
	got := testutil.PollUntil(10*time.Second, 25*time.Millisecond, func() (status, bool) {
		exists, alive, exitCode, err := RunSessionStatus(sessionName, opts)
		s := status{exists: exists, alive: alive, exitCode: exitCode}
		return s, err == nil && pred(exists, alive, exitCode)
	})
	return got.exists, got.alive, got.exitCode
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

// TestCreateDetachedSessionCollides pins the create-only contract: the loser
// reports ErrSessionNameTaken and the winner's tags are never re-stamped —
// the Ensure path used to adopt the session and overwrite them.
func TestCreateDetachedSessionCollides(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)
	dir := t.TempDir()

	if err := CreateDetachedSession("run-create", dir, "sleep 300", nil, opts, SessionTags{
		WorkspaceID: "ws-1",
		Type:        "run",
		CreatedAt:   111,
		InstanceID:  "test-inst",
	}); err != nil {
		t.Fatalf("CreateDetachedSession() error = %v", err)
	}

	err := CreateDetachedSession("run-create", dir, "echo loser", nil, opts, SessionTags{
		WorkspaceID: "ws-1",
		Type:        "run",
		CreatedAt:   999,
		InstanceID:  "test-inst",
	})
	if !errors.Is(err, ErrSessionNameTaken) {
		t.Fatalf("second CreateDetachedSession() = %v, want ErrSessionNameTaken", err)
	}

	// The winner's tags survived: workspace + the original creation stamp.
	rows, err := FindRunSessionsDetailed("ws-1", "test-inst", []string{"@amux_created_at"}, opts)
	if err != nil {
		t.Fatalf("FindRunSessionsDetailed() error = %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "run-create" {
		t.Fatalf("FindRunSessionsDetailed() = %+v, want exactly [run-create]", rows)
	}
	if got := rows[0].Tags["@amux_created_at"]; got != "111" {
		t.Fatalf("winner's @amux_created_at = %q, want 111 — loser must not re-stamp", got)
	}
	exists, alive, _, err := RunSessionStatus("run-create", opts)
	if err != nil || !exists || !alive {
		t.Fatalf("winning session status = (exists=%v, alive=%v, err=%v), want alive", exists, alive, err)
	}
	// A fresh name still creates — collision is per-name, not per-caller.
	if err := CreateDetachedSession("run-create-2", dir, "sleep 300", nil, opts, SessionTags{
		WorkspaceID: "ws-1", Type: "run", CreatedAt: 222, InstanceID: "test-inst",
	}); err != nil {
		t.Fatalf("CreateDetachedSession(new name) error = %v", err)
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
	// Find order is tmux's — sort before comparing (the package convention).
	sort.Strings(got)
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

// TestParseDeadPaneBannerStatus pins the tmux <3.3 fallback: when a dead pane
// has no pane_dead_status format, the exit code is recovered from the
// remain-on-exit banner text.
func TestParseDeadPaneBannerStatus(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int
		ok   bool
	}{
		{"nonzero exit", "job output\nPane is dead (status 7)\n", 7, true},
		{"zero exit", "done\nPane is dead (status 0)\n", 0, true},
		{"banner without status stays unknown", "Pane is dead\n", 0, false},
		{"no banner", "some output\n", 0, false},
		{"last banner wins", "Pane is dead (status 3)\nPane is dead (status 9)\n", 9, true},
		{"nonnumeric status rejected", "Pane is dead (status x)\n", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseDeadPaneBannerStatus(tc.text)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("parseDeadPaneBannerStatus(%q) = (%d, %v), want (%d, %v)", tc.text, got, ok, tc.want, tc.ok)
			}
		})
	}
}
