package git

import (
	"context"
	"fmt"
	"strings"
)

// filterDriverAttribution pairs a repo-local filter driver with a path whose
// attributes select it — a live repo-delivered exec binding.
type filterDriverAttribution struct {
	driver string
	path   string
}

// filterDriverGate is the mutation-site filter-driver check — a var so
// tests that fake runGitCtx can stand it down alongside the fake (the gate
// execs git directly for its config/ls-files/check-attr enumeration and
// would otherwise escape the fake entirely). refs are additional tree-ish
// sources whose attribute stacks also gate the operation — the commit a
// `worktree add` checks out, the incoming commit of a merge — because the
// current worktree's .gitattributes say nothing about paths attributed only
// in the tree being written.
var filterDriverGate = refuseRepoFilterDrivers

// refuseRepoFilterDrivers runs the filter-driver refusal shared by every
// mutation-capable git call site (worktree add, add -A, merge, merge
// --abort): a repo-local filter.<name>.* driver attributed by a path
// executes its command during checkout or staging — arbitrary code
// execution under the user's identity from bytes shipped inside a
// delivered .git, before the .amux trust gate could even be presented.
// Attribution is checked against the current index AND every ref's tree:
// `worktree add <base>` and `git merge` apply the checked-out/incoming
// tree's .gitattributes, not the current one. The AMUX_ALLOW_GIT_HOOKS=1
// opt-out stands the guard down along with the rest of the hardening —
// opting into repo hooks is opting into repo trust.
func refuseRepoFilterDrivers(ctx context.Context, repoPath, op string, refs ...string) error {
	if allowRepoGitHooks {
		return nil
	}
	live, err := liveFilterDrivers(ctx, repoPath, refs...)
	if err != nil {
		return fmt.Errorf("%s: checking filter drivers: %w", op, err)
	}
	if len(live) > 0 {
		return fmt.Errorf("%s: refusing: repo-configured filter driver %q is attributed to %s and would execute during checkout/write — review .git/config and .gitattributes, or set AMUX_ALLOW_GIT_HOOKS=1 to opt into repo trust", op, live[0].driver, live[0].path)
	}
	return nil
}

// liveFilterDrivers returns every filter driver configured in the repo's
// local+worktree config (.git/config, its include.path/includeIf chain, and
// .git/config.worktree — the deliverable surfaces) that at least one
// tracked path attributes via `filter=<name>` — the pairs a checkout or a
// stage could actually execute. The scope flag comes from
// filterConfigScope — `--local`, upgraded to `--worktree` when the repo
// enables extensions.worktreeConfig (it additionally reads config.worktree;
// unconditional --worktree errors on multi-worktree repos without the
// extension) — and `--includes` resolves include chains a delivered .git
// could hide drivers behind; the user's own global
// config stays deliberately out of scope — a legitimate `git lfs install`
// writes ~/.gitconfig and must not trip the gate.
//
// Attribution is evaluated against the current index (`ls-files` +
// `check-attr` applies the real attribute stack — .gitattributes at every
// tree level plus info/attributes) AND against each refs tree via
// `check-attr --source=<ref>` — the op being gated may write paths that
// only the target tree attributes. Unattributed driver names and
// unconfigured filter= values are correctly ignored in both.
func liveFilterDrivers(ctx context.Context, repoPath string, refs ...string) ([]filterDriverAttribution, error) {
	scope, err := filterConfigScope(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	args := append([]string{"config"}, scope...)
	args = append(args, "--includes", "--get-regexp", `^filter\..*\.(clean|smudge|process|command|required)$`)
	out, err := RunGitAllowFailureCtx(ctx, repoPath, args...)
	if err != nil {
		return nil, err
	}
	configured := make(map[string]struct{})
	for _, line := range strings.Split(out, "\n") {
		key, _, _ := strings.Cut(line, " ")
		name, ok := strings.CutPrefix(key, "filter.")
		if !ok {
			continue
		}
		// Strip the tail key — clean/smudge/process/command/required all
		// mark the driver name configured; the last dot separates them even
		// for names containing dots (filter.lfs.clean → lfs).
		if idx := strings.LastIndexByte(name, '.'); idx > 0 && idx < len(name)-1 {
			if suffix := name[idx+1:]; suffix == "clean" || suffix == "smudge" || suffix == "process" || suffix == "command" || suffix == "required" {
				name = name[:idx]
			}
		}
		if name != "" {
			configured[name] = struct{}{}
		}
	}
	if len(configured) == 0 {
		return nil, nil
	}

	var live []filterDriverAttribution
	seen := make(map[filterDriverAttribution]struct{})
	collect := func(pairs []filterDriverAttribution) {
		for _, p := range pairs {
			if _, dup := seen[p]; !dup {
				seen[p] = struct{}{}
				live = append(live, p)
			}
		}
	}
	idx, err := attributedDriverPaths(ctx, repoPath, "", "filter", configured)
	if err != nil {
		return nil, err
	}
	collect(idx)
	for _, ref := range refs {
		if ref == "" {
			continue
		}
		pairs, err := attributedDriverPaths(ctx, repoPath, ref, "filter", configured)
		if err != nil {
			return nil, err
		}
		collect(pairs)
	}
	return live, nil
}

// attributedDriverPaths enumerates the paths attributing configured
// drivers for the named attribute ("filter"/"merge"). ref == "" reads the
// current index (`ls-files` + `check-attr`); a non-empty ref reads that
// tree (`ls-tree -r` + `check-attr --source`) — covering drivers
// attributed only by the tree an operation writes.
func attributedDriverPaths(ctx context.Context, repoPath, ref, attr string, configured map[string]struct{}) ([]filterDriverAttribution, error) {
	var paths []string
	if ref == "" {
		pathsRaw, err := RunGitRawCtx(ctx, repoPath, "ls-files", "-z")
		if err != nil {
			return nil, err
		}
		paths = splitZ(pathsRaw)
	} else {
		pathsRaw, err := RunGitRawCtx(ctx, repoPath, "ls-tree", "-r", "-z", "--name-only", ref)
		if err != nil {
			return nil, err
		}
		paths = splitZ(pathsRaw)
	}
	if len(paths) == 0 {
		return nil, nil
	}

	// One spawn for the whole path list: --stdin takes NUL-separated paths
	// and emits the same path\0<attr>\0value\0 triples argv mode does.
	args := []string{"check-attr", "-z", "--stdin"}
	if ref != "" {
		args = append(args, "--source="+ref)
	}
	args = append(args, attr)
	raw, err := RunGitRawStdinCtx(ctx, repoPath,
		[]byte(strings.Join(paths, "\x00")+"\x00"), args...)
	if err != nil {
		return nil, err
	}
	var live []filterDriverAttribution
	fields := strings.Split(string(raw), "\x00")
	for j := 0; j+2 < len(fields); j += 3 {
		if _, ok := configured[fields[j+2]]; ok {
			live = append(live, filterDriverAttribution{driver: fields[j+2], path: fields[j]})
		}
	}
	return live, nil
}

// splitZ parses NUL-separated output into non-empty entries.
func splitZ(raw []byte) []string {
	var out []string
	for _, p := range strings.Split(string(raw), "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
