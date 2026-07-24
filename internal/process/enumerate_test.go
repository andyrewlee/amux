package process

import (
	"os"
	"runtime"
	"testing"
	"time"
)

func TestParsePSLines(t *testing.T) {
	out := "  100  100    1  0.0 Sun Jul 13 12:00:00 2026 sh -c pnpm run dev\n" +
		"  101  100  100 46.3 Sun Jul  6 07:05:09 2026 node /ws/ando/app/node_modules/.bin/ts-node-dev server.ts\n" +
		"garbage line\n" +
		"  102  102    1  1.5 Sun Jul 13 12:00:00 2026 5 numeric-argv0-command\n" +
		"\n"
	procs := parsePSLines(out)
	if len(procs) != 3 {
		t.Fatalf("expected 3 processes, got %d: %+v", len(procs), procs)
	}
	if procs[0].PID != 100 || procs[0].PGID != 100 || procs[0].PPID != 1 || procs[0].Command != "sh -c pnpm run dev" {
		t.Errorf("unexpected first process: %+v", procs[0])
	}
	want := time.Date(2026, 7, 13, 12, 0, 0, 0, time.Local)
	if !procs[0].StartedAt.Equal(want) {
		t.Errorf("expected start time %v, got %v", want, procs[0].StartedAt)
	}
	if procs[1].CPU != 46.3 {
		t.Errorf("expected CPU 46.3, got %v", procs[1].CPU)
	}
	// Space-padded single-digit day must parse too.
	if procs[1].StartedAt.IsZero() {
		t.Errorf("space-padded lstart failed to parse: %+v", procs[1])
	}
	if procs[2].Command != "5 numeric-argv0-command" {
		t.Errorf("numeric argv0 mis-parsed: %+v", procs[2])
	}
}

func TestDescendants(t *testing.T) {
	snap := []ProcessInfo{
		{PID: 1, PPID: 0},
		{PID: 10, PPID: 1},
		{PID: 20, PPID: 10},
		{PID: 21, PPID: 10},
		{PID: 30, PPID: 20},
		{PID: 40, PPID: 1},
	}
	got := Descendants(snap, 10)
	want := []int{20, 21, 30}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %+v", want, got)
	}
	for i, pid := range want {
		if got[i].PID != pid {
			t.Errorf("descendant %d: expected pid %d, got %d", i, pid, got[i].PID)
		}
	}
	if len(Descendants(snap, 999)) != 0 {
		t.Error("unknown root should have no descendants")
	}
}

func TestReferencingPathBoundaries(t *testing.T) {
	snap := []ProcessInfo{
		{PID: 1, Command: "node /ws/app/node_modules/.bin/vite"},
		{PID: 2, Command: "node /ws/app-other/server.js"},
		{PID: 3, Command: "vim /ws/app"},
		{PID: 4, Command: "sh -c cd /ws/app; pnpm dev"},
		{PID: 5, Command: "unrelated"},
	}
	got := ReferencingPath(snap, "/ws/app")
	if len(got) != 3 {
		t.Fatalf("expected pids 1,3,4 — got %+v", got)
	}
	for _, p := range got {
		if p.PID == 2 || p.PID == 5 {
			t.Errorf("pid %d should not match", p.PID)
		}
	}
	// Trailing slash on the query must not change matching.
	if len(ReferencingPath(snap, "/ws/app/")) != 3 {
		t.Error("trailing slash changed match results")
	}
}

func TestIsSessionCommand(t *testing.T) {
	session := []string{
		"-zsh", "/bin/zsh -l", "sh -lc unset TMUX; exec /bin/zsh -l",
		"tmux -L amux -f /dev/null new-session",
		"claude --dangerously-skip-permissions", "nvim .", "./amux",
		// macOS app bundles and editors are always sessions, even with a
		// workspace path in argv.
		"/Applications/Visual Studio Code.app/Contents/MacOS/Electron /ws/app",
		"code /ws/app",
	}
	for _, cmd := range session {
		if !IsSessionCommand(cmd) {
			t.Errorf("expected session command: %q", cmd)
		}
	}
	services := []string{
		"node /ws/app/node_modules/.bin/ts-node-dev server.ts",
		"/opt/homebrew/bin/esbuild --service=0.23.1 --ping",
		"pnpm dlx convex dev",
		"sh -c pnpm run dev", // the ScriptRunner wrapper shape
		"",
	}
	for _, cmd := range services {
		if IsSessionCommand(cmd) {
			t.Errorf("expected service command: %q", cmd)
		}
	}
}

func TestGroupLeaders(t *testing.T) {
	procs := []ProcessInfo{
		{PID: 12, PGID: 10},
		{PID: 10, PGID: 10}, // leader listed second on purpose
		{PID: 22, PGID: 20},
		{PID: 21, PGID: 20}, // no leader present: lowest PID wins
	}
	got := GroupLeaders(procs)
	if len(got) != 2 {
		t.Fatalf("expected 2 groups, got %+v", got)
	}
	if got[0].PID != 10 {
		t.Errorf("group 10: expected leader pid 10, got %d", got[0].PID)
	}
	if got[1].PID != 21 {
		t.Errorf("group 20: expected lowest pid 21, got %d", got[1].PID)
	}
}

func TestSnapshotSeesSelf(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Snapshot unsupported on windows")
	}
	snap, err := Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	for _, p := range snap {
		if p.PID == os.Getpid() {
			return
		}
	}
	t.Error("snapshot does not contain the current process")
}
