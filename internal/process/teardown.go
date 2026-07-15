package process

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// teardownGracePeriod gives service stacks (dev servers with children of
// their own) longer than the 200ms treekill default to exit cleanly before
// SIGKILL.
const teardownGracePeriod = 2 * time.Second

// OrphanedGroups returns the process groups that qualify as orphaned service
// workloads for path: some member's command line references the path, no
// member is an interactive session process, and the group leader's ancestry
// reaches the root of the process tree (PID <= 1, covering both init and
// subreaper-managed orphans) without passing through a session process. A
// parent missing from the snapshot is ambiguous and disqualifies the group —
// never kill on doubt. The session guard is load-bearing: a user's shell,
// agent client, or editor must never qualify, even when it holds the path
// (see IsSessionCommand).
func OrphanedGroups(snap []ProcessInfo, path string) []ProcessInfo {
	referencing := ReferencingPath(snap, path)
	if len(referencing) == 0 {
		return nil
	}
	candidates := make(map[int]bool, len(referencing))
	for _, p := range referencing {
		candidates[p.PGID] = true
	}
	byPID := make(map[int]ProcessInfo, len(snap))
	for _, p := range snap {
		byPID[p.PID] = p
	}
	members := make(map[int][]ProcessInfo)
	for _, p := range snap {
		if candidates[p.PGID] {
			members[p.PGID] = append(members[p.PGID], p)
		}
	}
	var leaders []ProcessInfo
	for _, procs := range members {
		disqualified := false
		for _, p := range procs {
			if IsSessionCommand(p.Command) {
				disqualified = true
				break
			}
		}
		if disqualified {
			continue
		}
		leader := GroupLeaders(procs)[0]
		if !ancestryReachesRootWithoutSession(leader, byPID) {
			continue
		}
		leaders = append(leaders, leader)
	}
	sort.Slice(leaders, func(i, j int) bool { return leaders[i].PGID < leaders[j].PGID })
	return leaders
}

// ancestryReachesRootWithoutSession walks p's parent chain. True means the
// chain hits PID <= 1 (init, launchd, or a numbered subreaper's own root)
// with every intermediate ancestor being a non-session process — the group
// has no live session owning it. A missing ancestor or a cycle is ambiguous
// and returns false.
func ancestryReachesRootWithoutSession(p ProcessInfo, byPID map[int]ProcessInfo) bool {
	seen := map[int]bool{p.PID: true}
	cur := p
	for {
		if cur.PPID <= 1 {
			return true
		}
		parent, ok := byPID[cur.PPID]
		if !ok || seen[parent.PID] {
			return false
		}
		if IsSessionCommand(parent.Command) {
			return false
		}
		seen[parent.PID] = true
		cur = parent
	}
}

// TeardownResult reports what a workspace teardown did. A non-nil error from
// TeardownWorkspaceProcesses means qualifying groups survived; the error text
// carries their description.
type TeardownResult struct {
	// Killed are the group leaders that were terminated, one entry per group.
	Killed []ProcessInfo
}

// TeardownWorkspaceProcesses kills orphaned service process groups that
// reference the workspace root, then re-verifies. It returns an error when
// qualifying groups survive the kill passes, so callers can refuse to proceed
// with worktree removal instead of silently orphaning live writers. A nil
// error with empty Killed means there was nothing to tear down. Platforms
// without process enumeration (Windows) degrade to a no-op: session and
// script teardown still run, only the orphan sweep is skipped.
//
// It deliberately does NOT touch processes still attached to a live session
// (a tmux pane, a shell, an agent): those are the caller's to stop first —
// tmux.KillSession already kills pane process groups.
//
// Known limitation: attribution is by command line, so a process whose argv
// never names the workspace path (started with a relative command and only
// its cwd inside the worktree) is invisible to both the kill and the verify
// pass. The durable ServiceRegistry covers the managed-script case; fully
// closing the gap needs cwd-based attribution.
func TeardownWorkspaceProcesses(root string) (TeardownResult, error) {
	var res TeardownResult
	killedPGIDs := make(map[int]bool)
	// Two passes: killing a session's shell reparents its background jobs
	// asynchronously, so groups can become qualifying orphans between the
	// first snapshot and the verify snapshot. The second pass catches exactly
	// those; anything still alive after it is a genuine failure.
	for pass := 0; pass < 2; pass++ {
		snap, err := Snapshot()
		if err != nil {
			if errors.Is(err, errors.ErrUnsupported) {
				return res, nil
			}
			return res, fmt.Errorf("teardown snapshot: %w", err)
		}
		targets := OrphanedGroups(snap, root)
		if len(targets) == 0 {
			return res, nil
		}
		killGroups(targets, func(leader ProcessInfo) {
			if !killedPGIDs[leader.PGID] {
				killedPGIDs[leader.PGID] = true
				res.Killed = append(res.Killed, leader)
			}
		})
	}
	snap, err := Snapshot()
	if err != nil {
		return res, fmt.Errorf("teardown verify snapshot: %w", err)
	}
	if survivors := OrphanedGroups(snap, root); len(survivors) > 0 {
		return res, fmt.Errorf("workspace processes survived teardown: %s", DescribeGroups(survivors))
	}
	return res, nil
}

// killGroups terminates the given group leaders concurrently — each kill
// waits up to teardownGracePeriod before SIGKILL, so sequential kills would
// cost gracePeriod × N on the caller's (synchronous delete) path. onKilled
// runs serially for each leader that was actually signaled. Each kill
// re-verifies the leader still belongs to its snapshotted process group, so
// a PID recycled since the snapshot is never signaled.
func killGroups(targets []ProcessInfo, onKilled func(ProcessInfo)) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, leader := range targets {
		wg.Add(1)
		go func(leader ProcessInfo) {
			defer wg.Done()
			if !processGroupMatches(leader.PID, leader.PGID) {
				return
			}
			if err := KillProcessGroup(leader.PID, KillOptions{GracePeriod: teardownGracePeriod}); err != nil && !isBenignStopError(err) {
				return
			}
			mu.Lock()
			onKilled(leader)
			mu.Unlock()
		}(leader)
	}
	wg.Wait()
}

// KillOrphanedGroups terminates the given orphaned group leaders (as returned
// by OrphanedGroups/FindWorkspaceOrphans against a recent snapshot) and
// returns the ones that were actually signaled. Kills run concurrently and
// re-verify group identity first — see killGroups.
func KillOrphanedGroups(targets []ProcessInfo) []ProcessInfo {
	var killed []ProcessInfo
	killGroups(targets, func(leader ProcessInfo) {
		killed = append(killed, leader)
	})
	return killed
}

// WorkspaceStats aggregates the live workload attributable to one workspace.
type WorkspaceStats struct {
	// Procs is the number of processes in groups referencing the workspace.
	Procs int
	// CPU is those processes' summed instantaneous %CPU (100 = one core).
	CPU float64
}

// StatsByWorkspace sums, for every root, the process groups whose command
// lines reference it — the same attribution the teardown/reaper paths use,
// but without any orphan or session filtering: this is a resource ledger, not
// a kill list. One shared pass over the snapshot serves all roots.
func StatsByWorkspace(snap []ProcessInfo, roots []string) map[string]WorkspaceStats {
	stats := make(map[string]WorkspaceStats)
	if len(roots) == 0 {
		return stats
	}
	trimmed := make([]string, 0, len(roots))
	for _, root := range roots {
		if r := strings.TrimRight(root, "/"); r != "" {
			trimmed = append(trimmed, r)
		}
	}
	// pgid -> set of roots any member references.
	groupRoots := make(map[int]map[string]bool)
	for _, p := range snap {
		for _, root := range trimmed {
			if commandReferencesPath(p.Command, root) {
				if groupRoots[p.PGID] == nil {
					groupRoots[p.PGID] = make(map[string]bool, 1)
				}
				groupRoots[p.PGID][root] = true
			}
		}
	}
	if len(groupRoots) == 0 {
		return stats
	}
	for _, p := range snap {
		for root := range groupRoots[p.PGID] {
			s := stats[root]
			s.Procs++
			s.CPU += p.CPU
			stats[root] = s
		}
	}
	return stats
}

// StatsForWorkspace is the single-root form of StatsByWorkspace.
func StatsForWorkspace(snap []ProcessInfo, root string) WorkspaceStats {
	return StatsByWorkspace(snap, []string{root})[strings.TrimRight(root, "/")]
}

// DescribeGroups renders group leaders compactly for logs and error messages.
func DescribeGroups(leaders []ProcessInfo) string {
	parts := make([]string, 0, len(leaders))
	for _, l := range leaders {
		cmd := l.Command
		if len(cmd) > 80 {
			cmd = cmd[:77] + "..."
		}
		parts = append(parts, fmt.Sprintf("pgid %d (%s)", l.PGID, cmd))
	}
	return strings.Join(parts, ", ")
}
