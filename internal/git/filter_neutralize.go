package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Read-path safety: `git status`/`git diff` run repo-configured
// filter.<name>.clean/process/command drivers on stat-dirty attributed
// files — and a delivered .git/index's stat metadata (inode/ctime/dev) can
// never match the recipient's filesystem, so every attributed file is
// stat-dirty on first refresh. The mutation-site refusal in
// filter_drivers.go cannot help here: status runs on a timer before any
// trust prompt, and refusing reads would break the product. So every git
// invocation on a repo with configured local/worktree filter drivers
// carries `-c filter.<n>.clean= -c filter.<n>.smudge= -c filter.<n>.process=
// -c filter.<n>.command= -c filter.<n>.required=false` overrides — an
// empty command disables the driver (the documented LFS-disable recipe),
// and required=false keeps a surviving-but-disabled driver non-fatal.
// Globally configured drivers (a real `git lfs install`) are untouched —
// only repo-local, included, and worktree-scope config is enumerated.

// fileStamp identifies one config input by mtime+size so the cache
// invalidates when any contributing file is rewritten.
type fileStamp struct {
	path   string
	mtime  time.Time
	size   int64
	exists bool
}

// neutralCacheEntry records the neutralizer argv for one repo, keyed by the
// stamps of every file that contributed to the enumeration.
type neutralCacheEntry struct {
	key  []fileStamp
	args []string
}

var filterNeutralCache = struct {
	mu   sync.Mutex
	ents map[string]neutralCacheEntry
}{ents: make(map[string]neutralCacheEntry)}

// neutralizeGitArgs returns args with neutralizing `-c filter.<n>.*=`
// overrides prepended when dir's repo config carries filter drivers. It
// fails closed: an enumeration error is returned rather than silently
// running the operation unprotected. `config` subcommands are never
// neutralized — config reads execute no filters, and the enumeration below
// goes through the same runners (the skip is the recursion guard).
func neutralizeGitArgs(ctx context.Context, dir string, args []string) ([]string, error) {
	if ops.allowRepoGitHooks {
		return args, nil
	}
	if err := forgedCommondirError(dir); err != nil {
		return nil, err
	}
	if len(args) == 0 || args[0] == "config" {
		return args, nil
	}
	overrides, err := neutralizingFilterArgs(ctx, dir)
	if err != nil {
		return nil, err
	}
	if len(overrides) == 0 {
		return args, nil
	}
	return append(overrides, args...), nil
}

// neutralizingFilterArgs enumerates dir's configured local/worktree filter
// driver names (with mtime-keyed caching) and returns the `-c` argv pairs
// that disable each one. The enumeration runs on its own bounded context —
// a caller ctx about to expire must not mislabel the real command's error
// (and a 5s local config read can't hold a canceled caller hostage).
func neutralizingFilterArgs(ctx context.Context, dir string) ([]string, error) {
	gitdir, commondir := repoGitDirs(dir)
	if gitdir == "" {
		return nil, nil // not a repository — nothing repo-configured exists
	}
	base := configInputPaths(gitdir, commondir)

	filterNeutralCache.mu.Lock()
	entry, ok := filterNeutralCache.ents[dir]
	filterNeutralCache.mu.Unlock()
	if ok && stampsMatch(entry.key, base) {
		return entry.args, nil
	}

	enumCtx, cancel := context.WithTimeout(context.Background(), filterEnumTimeout)
	defer cancel()
	scope, err := filterConfigScope(enumCtx, dir)
	if err != nil {
		return nil, err
	}
	enumArgs := append([]string{"config"}, scope...)
	enumArgs = append(enumArgs, "--includes", "--show-origin", "--get-regexp",
		`^filter\..*\.(clean|smudge|process|command|required)$`)
	out, err := RunGitAllowFailureCtx(enumCtx, dir, enumArgs...)
	if err != nil {
		return nil, err
	}
	names := make(map[string]struct{})
	var allPaths []string
	for _, line := range strings.Split(out, "\n") {
		// --show-origin lines are `file:<path>\t<key> <value>`.
		origin, kv, _ := strings.Cut(line, "\t")
		if p, ok := strings.CutPrefix(origin, "file:"); ok && p != "" {
			if !filepath.IsAbs(p) {
				p = filepath.Join(dir, p)
			}
			allPaths = append(allPaths, p)
		}
		key, _, _ := strings.Cut(kv, " ")
		if name, ok := strings.CutPrefix(key, "filter."); ok {
			if idx := strings.LastIndexByte(name, '.'); idx > 0 && idx < len(name)-1 {
				names[name[:idx]] = struct{}{}
			}
		}
	}

	var overrides []string
	for _, n := range sortedKeys(names) {
		overrides = append(overrides,
			"-c", "filter."+n+".clean=",
			"-c", "filter."+n+".smudge=",
			"-c", "filter."+n+".process=",
			"-c", "filter."+n+".command=",
			"-c", "filter."+n+".required=false",
		)
	}

	filterNeutralCache.mu.Lock()
	filterNeutralCache.ents[dir] = neutralCacheEntry{key: stampFiles(append(base, allPaths...)), args: overrides}
	filterNeutralCache.mu.Unlock()
	return overrides, nil
}

// repoGitDirs resolves dir's gitdir and common dir without spawning git:
// `.git` as a directory → both are it; `.git` as a file → the worktree
// gitdir it names, plus its `commondir` file's target. Bare-repo layout
// (dir itself contains HEAD+config) resolves to dir for both.
func repoGitDirs(dir string) (gitdir, commondir string) {
	dotgit := filepath.Join(dir, ".git")
	if fi, err := os.Stat(dotgit); err == nil && fi.IsDir() {
		return dotgit, dotgit
	}
	if raw, err := os.ReadFile(dotgit); err == nil {
		return resolveWorktreeGitdir(dir, string(raw))
	}
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err == nil {
		if _, err := os.Stat(filepath.Join(dir, "config")); err == nil {
			return dir, dir // bare-repo layout
		}
	}
	return "", ""
}

// resolveWorktreeGitdir follows a `.git` file's `gitdir:` line to the
// linked worktree's gitdir, then its `commondir` file to the shared dir.
func resolveWorktreeGitdir(dir, contents string) (gitdir, commondir string) {
	p, ok := strings.CutPrefix(strings.TrimSpace(contents), "gitdir:")
	if !ok {
		return "", ""
	}
	gitdir = strings.TrimSpace(p)
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(dir, gitdir)
	}
	if raw, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil {
		commondir = strings.TrimSpace(string(raw))
		if !filepath.IsAbs(commondir) {
			commondir = filepath.Join(gitdir, commondir)
		}
		return gitdir, filepath.Clean(commondir)
	}
	return gitdir, gitdir
}

// configInputPaths lists the files whose content can change the
// enumeration: the shared local config, the per-worktree config, and HEAD
// (includeIf onbranch conditions re-evaluate on HEAD moves).
func configInputPaths(gitdir, commondir string) []string {
	paths := []string{
		filepath.Join(commondir, "config"),
		filepath.Join(gitdir, "config.worktree"),
		filepath.Join(gitdir, "HEAD"),
	}
	return paths
}

// forgedCommondirError refuses to run git against a checkout whose `.git`
// is a directory carrying a `commondir` file. Git only ever writes
// commondir inside the per-worktree gitdirs under a shared dir
// (<common>/worktrees/<id>/commondir) — a primary checkout's `.git` dir
// never has one, and linked worktrees reach the shared dir through a
// `gitdir:` .git FILE instead. So a commondir file sitting in a
// checkout's .git directory is forged state: git honors it and resolves
// refs/objects — and `worktree add` metadata writes — against the
// attacker-chosen common dir. Refusing every operation fails closed
// (repo reads/writes error out) rather than letting the redirect stand.
func forgedCommondirError(dir string) error {
	dotgit := filepath.Join(dir, ".git")
	if fi, err := os.Stat(dotgit); err != nil || !fi.IsDir() {
		return nil // .git file (linked worktree) or no repo at dir — nothing to check
	}
	if _, err := os.Stat(filepath.Join(dotgit, "commondir")); err == nil {
		return fmt.Errorf("refusing to run git in %s: .git/commondir present in a primary checkout is forged state that redirects the common dir", dir)
	}
	return nil
}

// stampsMatch reports whether the cached key still describes the live
// inputs: stat the stored paths; a new file appearing (config created
// since) is caught because the stored key's absent-stamp no longer
// matches. include-origin files are recorded in the stored key, so edits
// to included files invalidate too.
func stampsMatch(key []fileStamp, base []string) bool {
	for _, want := range key {
		fi, err := os.Stat(want.path)
		if (err == nil) != want.exists {
			return false
		}
		if err == nil && (fi.Size() != want.size || !fi.ModTime().Equal(want.mtime)) {
			return false
		}
	}
	// A config file that did not exist at enumeration time but does now is
	// covered above (exists flag flips). A *new* config input outside the
	// recorded set (e.g. config.worktree appeared where never recorded) —
	// check the base set explicitly.
	for _, p := range base {
		_, err := os.Stat(p)
		present := err == nil
		found := false
		for _, want := range key {
			if want.path == p {
				found = true
				break
			}
		}
		if !found && present {
			return false
		}
	}
	return true
}

func stampFiles(paths []string) []fileStamp {
	stamps := make([]fileStamp, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		p = filepath.Clean(p)
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		st := fileStamp{path: p}
		if fi, err := os.Stat(p); err == nil {
			st.exists, st.mtime, st.size = true, fi.ModTime(), fi.Size()
		}
		stamps = append(stamps, st)
	}
	return stamps
}

func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// filterEnumTimeout bounds the sidecar config enumerations — they run on
// their own context so an expiring caller ctx doesn't mislabel errors.
const filterEnumTimeout = 5 * time.Second

// filterConfigScope returns the config-scope flag pair for the filter
// enumeration: `--local` unless the repo enables extensions.worktreeConfig,
// in which case `--worktree` (a strict superset that also reads
// config.worktree). `--worktree` cannot be used unconditionally — on a
// multi-worktree repo without the extension git errors out.
func filterConfigScope(ctx context.Context, dir string) ([]string, error) {
	out, err := RunGitAllowFailureCtx(ctx, dir, "config", "--local", "--includes",
		"--get", "extensions.worktreeConfig")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "true" {
		return []string{"--worktree"}, nil
	}
	return []string{"--local"}, nil
}
