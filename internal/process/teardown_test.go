package process

import (
	"strings"
	"testing"
)

func TestOrphanedGroups(t *testing.T) {
	root := "/base/proj/ws"
	snap := []ProcessInfo{
		// Qualifying orphan: leader reparented to PID 1, member references root.
		{PID: 100, PGID: 100, PPID: 1, Command: "fnm exec pnpm dlx convex dev"},
		{PID: 101, PGID: 100, PPID: 100, Command: "node " + root + "/node_modules/.bin/convex dev"},
		// Live stack: leader still has a real parent (its shell) — not orphaned.
		{PID: 200, PGID: 200, PPID: 50, Command: "node " + root + "/node_modules/.bin/vite"},
		// Group containing a session command must never qualify.
		{PID: 300, PGID: 300, PPID: 1, Command: "-zsh"},
		{PID: 301, PGID: 300, PPID: 300, Command: "tail -f " + root + "/log.txt"},
		// Orphaned group referencing a different workspace.
		{PID: 400, PGID: 400, PPID: 1, Command: "node /base/proj/other/server.js"},
	}
	got := OrphanedGroups(snap, root)
	if len(got) != 1 || got[0].PGID != 100 {
		t.Fatalf("expected only pgid 100, got %+v", got)
	}
}

func TestOrphanedGroupsLeaderReferencesButChildDoesNot(t *testing.T) {
	root := "/base/proj/ws"
	snap := []ProcessInfo{
		{PID: 100, PGID: 100, PPID: 1, Command: "sh -c cd " + root + " && pnpm dev"},
		{PID: 101, PGID: 100, PPID: 100, Command: "node dev-server"},
	}
	got := OrphanedGroups(snap, root)
	if len(got) != 1 || got[0].PID != 100 {
		t.Fatalf("expected leader pid 100, got %+v", got)
	}
}

func TestOrphanedGroupsSubreaperAncestry(t *testing.T) {
	root := "/base/proj/ws"
	snap := []ProcessInfo{
		// Linux user-session subreaper: orphans reparent to systemd --user
		// (PPID != 1) — the group is still orphaned because the ancestry
		// reaches the tree root without passing a session process.
		{PID: 500, PGID: 500, PPID: 1, Command: "/usr/lib/systemd/systemd --user"},
		{PID: 510, PGID: 510, PPID: 500, Command: "node " + root + "/server.js"},
		// Same shape but the parent chain passes through an interactive
		// shell: a user's own foreground stack, never orphaned.
		{PID: 600, PGID: 600, PPID: 1, Command: "-zsh"},
		{PID: 610, PGID: 610, PPID: 600, Command: "node " + root + "/other.js"},
	}
	got := OrphanedGroups(snap, root)
	if len(got) != 1 || got[0].PGID != 510 {
		t.Fatalf("expected only the subreaper-parented pgid 510, got %+v", got)
	}
}

func TestOrphanedGroupsSparesAppBundles(t *testing.T) {
	root := "/base/proj/ws"
	snap := []ProcessInfo{
		// A Dock-launched editor (PPID 1 on macOS) with the workspace path in
		// argv must never qualify: .app/ bundles are session processes.
		{PID: 700, PGID: 700, PPID: 1, Command: "/Applications/Visual Studio Code.app/Contents/MacOS/Electron " + root},
	}
	if got := OrphanedGroups(snap, root); len(got) != 0 {
		t.Fatalf("app bundle must never be treated as an orphaned service, got %+v", got)
	}
}

func TestDescribeGroupsTruncates(t *testing.T) {
	long := strings.Repeat("x", 200)
	desc := DescribeGroups([]ProcessInfo{{PGID: 7, Command: long}})
	if len(desc) > 120 || !strings.Contains(desc, "pgid 7") {
		t.Errorf("unexpected description: %q", desc)
	}
}

func TestFindWorkspaceOrphans(t *testing.T) {
	base := "/base"
	deadRoot := base + "/proj/gone"
	stagedRoot := base + "/proj/.ws.amux-prune-123-0"
	liveRoot := base + "/proj/alive"
	snap := []ProcessInfo{
		// Orphan referencing a deleted workspace root → reap.
		{PID: 100, PGID: 100, PPID: 1, Command: "node " + deadRoot + "/server.js"},
		// Orphan referencing a prune-staged path → reap even though the staged dir exists.
		{PID: 200, PGID: 200, PPID: 1, Command: "node " + stagedRoot + "/node_modules/.bin/trigger dev"},
		// Orphan referencing a live workspace → never reaped automatically.
		{PID: 300, PGID: 300, PPID: 1, Command: "node " + liveRoot + "/server.js"},
		// Non-orphan referencing a dead root (still has a parent) → left alone.
		{PID: 400, PGID: 400, PPID: 50, Command: "node " + deadRoot + "/other.js"},
	}
	exists := func(path string) bool { return path == liveRoot || path == stagedRoot }
	got := FindWorkspaceOrphans(snap, base, exists)
	if len(got) != 2 {
		t.Fatalf("expected pgids 100 and 200, got %+v", got)
	}
	if got[0].PGID != 100 || got[1].PGID != 200 {
		t.Errorf("unexpected orphans: %+v", got)
	}
}

func TestStatsForWorkspace(t *testing.T) {
	root := "/base/proj/ws"
	snap := []ProcessInfo{
		// Group 100: leader doesn't reference root, child does — whole group counts.
		{PID: 100, PGID: 100, PPID: 1, CPU: 1.0, Command: "fnm exec pnpm run dev"},
		{PID: 101, PGID: 100, PPID: 100, CPU: 46.5, Command: "node " + root + "/node_modules/.bin/vite"},
		// Unrelated group.
		{PID: 200, PGID: 200, PPID: 1, CPU: 90.0, Command: "node /elsewhere/server.js"},
	}
	stats := StatsForWorkspace(snap, root)
	if stats.Procs != 2 {
		t.Errorf("expected 2 procs, got %d", stats.Procs)
	}
	if stats.CPU != 47.5 {
		t.Errorf("expected 47.5 CPU, got %v", stats.CPU)
	}
	if empty := StatsForWorkspace(snap, "/base/proj/none"); empty.Procs != 0 || empty.CPU != 0 {
		t.Errorf("expected zero stats, got %+v", empty)
	}
}

func TestReferencedWorkspaceRoots(t *testing.T) {
	cmd := "node /base/p1/ws1/node_modules/.bin/x --flag /base/p2/ws2 other"
	roots := referencedWorkspaceRoots(cmd, "/base")
	if len(roots) != 2 || roots[0] != "/base/p1/ws1" || roots[1] != "/base/p2/ws2" {
		t.Fatalf("unexpected roots: %v", roots)
	}
	if got := referencedWorkspaceRoots("no paths here", "/base"); len(got) != 0 {
		t.Errorf("expected no roots, got %v", got)
	}
}
