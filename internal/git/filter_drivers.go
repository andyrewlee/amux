package git

import (
	"context"
	"fmt"
	"strings"
)

// filterDriverAttribution pairs a repo-local filter driver with a tracked
// path whose attributes select it — a live repo-delivered exec binding.
type filterDriverAttribution struct {
	driver string
	path   string
}

// filterDriverGate is the mutation-site filter-driver check — a var so
// tests that fake runGitCtx can stand it down alongside the fake (the gate
// execs git directly for its config/ls-files/check-attr enumeration and
// would otherwise escape the fake entirely).
var filterDriverGate = refuseRepoFilterDrivers

// refuseRepoFilterDrivers runs the filter-driver refusal shared by every
// mutation-capable git call site (worktree add, add -A, merge): a
// repo-local filter.<name>.* driver attributed by a tracked path executes
// its command during checkout or staging — arbitrary code execution under
// the user's identity from bytes shipped inside a delivered .git, before
// the .amux trust gate could even be presented. The AMUX_ALLOW_GIT_HOOKS=1
// opt-out stands the guard down along with the rest of the hardening —
// opting into repo hooks is opting into repo trust.
func refuseRepoFilterDrivers(ctx context.Context, repoPath, op string) error {
	if allowRepoGitHooks {
		return nil
	}
	live, err := liveFilterDrivers(ctx, repoPath)
	if err != nil {
		return fmt.Errorf("%s: checking filter drivers: %w", op, err)
	}
	if len(live) > 0 {
		return fmt.Errorf("%s: refusing: repo-configured filter driver %q is attributed to %s and would execute during checkout/write — review .git/config and .gitattributes, or set AMUX_ALLOW_GIT_HOOKS=1 to opt into repo trust", op, live[0].driver, live[0].path)
	}
	return nil
}

// liveFilterDrivers returns every filter driver configured in the repo's
// LOCAL config (.git/config — the deliverable surface) that at least one
// tracked path attributes via `filter=<name>` — the pairs a checkout or a
// stage could actually execute. Two enumerations bound the answer exactly,
// mirroring liveMergeDrivers: `git config --local --get-regexp` reads only
// the repo-local config (the user's own global config is deliberately out
// of scope — a legitimate `git lfs install` writes ~/.gitconfig and must
// not trip the gate), and `git check-attr` applies the real attribute stack
// (.gitattributes at every tree level plus info/attributes), so
// unattributed driver names and unconfigured filter= values are correctly
// ignored.
func liveFilterDrivers(ctx context.Context, repoPath string) ([]filterDriverAttribution, error) {
	out, err := RunGitAllowFailureCtx(ctx, repoPath, "config", "--local", "--get-regexp", `^filter\..*\.(clean|smudge|process|command|required)$`)
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

	pathsRaw, err := RunGitRawCtx(ctx, repoPath, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(string(pathsRaw), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}

	var live []filterDriverAttribution
	const checkAttrBatch = 200
	for i := 0; i < len(paths); i += checkAttrBatch {
		end := min(i+checkAttrBatch, len(paths))
		args := append([]string{"check-attr", "-z", "filter", "--"}, paths[i:end]...)
		raw, err := RunGitRawCtx(ctx, repoPath, args...)
		if err != nil {
			return nil, err
		}
		// -z output is path\0filter\0value\0 triples.
		fields := strings.Split(string(raw), "\x00")
		for j := 0; j+2 < len(fields); j += 3 {
			if _, ok := configured[fields[j+2]]; ok {
				live = append(live, filterDriverAttribution{driver: fields[j+2], path: fields[j]})
			}
		}
	}
	return live, nil
}
