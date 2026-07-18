package process

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// pruneStagedMarker appears in the basename of worktrees staged for removal
// by internal/git's cleanup (".<base>.amux-prune-<ts>-<attempt>"). Processes
// still referencing such a path are running out of a directory amux is in the
// middle of deleting — definitively amux's garbage.
const pruneStagedMarker = ".amux-prune-"

// FindWorkspaceOrphans returns orphaned service group leaders whose command
// lines reference a workspace root under baseDir that is either staged for
// prune or no longer on disk (exists reports path liveness; pass os.Stat
// semantics in production, a fake in tests). Only definitively dead roots
// qualify — groups referencing a live workspace are never returned, so this
// can safely drive automatic reaping without violating the never-kill-
// ambiguous rule.
func FindWorkspaceOrphans(snap []ProcessInfo, baseDir string, exists func(string) bool) []ProcessInfo {
	baseDir = strings.TrimRight(baseDir, "/")
	if baseDir == "" {
		return nil
	}
	roots := make(map[string]bool)
	for _, p := range snap {
		for _, root := range referencedWorkspaceRoots(p.Command, baseDir) {
			roots[root] = true
		}
	}
	seen := make(map[int]bool)
	var out []ProcessInfo
	for root := range roots {
		staged := strings.Contains(filepath.Base(root), pruneStagedMarker)
		if !staged && exists(root) {
			continue
		}
		for _, leader := range OrphanedGroups(snap, root) {
			if seen[leader.PGID] {
				continue
			}
			seen[leader.PGID] = true
			out = append(out, leader)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PGID < out[j].PGID })
	return out
}

// PathExists is the production exists callback for FindWorkspaceOrphans.
func PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// referencedWorkspaceRoots extracts the workspace roots (baseDir/<project>/
// <workspace>) named anywhere in a command line. Deeper references (a script
// inside node_modules) resolve to their containing root.
func referencedWorkspaceRoots(command, baseDir string) []string {
	var roots []string
	prefix := baseDir + "/"
	for rest := command; ; {
		idx := strings.Index(rest, prefix)
		if idx < 0 {
			return roots
		}
		tail := rest[idx+len(prefix):]
		end := strings.IndexAny(tail, " \t\"';:")
		if end < 0 {
			end = len(tail)
		}
		parts := strings.SplitN(tail[:end], "/", 3)
		if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
			roots = append(roots, filepath.Join(baseDir, parts[0], parts[1]))
		}
		rest = tail
	}
}
