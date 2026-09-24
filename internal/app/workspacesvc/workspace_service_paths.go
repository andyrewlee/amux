package workspacesvc

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
)

// shouldSurfaceWorkspace returns true for workspaces managed by amux for this
// project and for the primary checkout.
func (s *Service) shouldSurfaceWorkspace(_ string, ws *data.Workspace) bool {
	if ws == nil {
		return false
	}
	if ws.IsPrimaryCheckout() {
		return true
	}
	managedRoot := s.managedProjectRoot()
	wsRoot := lexicalWorkspacePath(ws.Root)
	if managedRoot == "" || wsRoot == "" {
		return false
	}
	return pathWithinAliases(workspacePathAliases(managedRoot), workspacePathAliases(wsRoot))
}

func (s *Service) managedProjectRoot() string {
	base := strings.TrimSpace(s.workspacesRoot)
	if base == "" {
		return ""
	}
	return lexicalWorkspacePath(base)
}

func (s *Service) pendingProjectRoot(project *data.Project) string {
	base := strings.TrimSpace(s.workspacesRoot)
	if base == "" {
		return ""
	}
	projectName := strings.TrimSpace(project.Name)
	if projectName == "" {
		projectName = filepath.Base(strings.TrimSpace(project.Path))
	}
	if projectName == "" {
		return ""
	}
	return filepath.Join(base, projectName)
}

// ResolveBase returns the base branch to use for a new workspace. If base is
// non-empty (after trimming) it is returned as-is; otherwise GetBaseBranch is
// consulted, falling back to "HEAD" on error.
func ResolveBase(projectPath, base string) string {
	base = strings.TrimSpace(base)
	if base != "" {
		return base
	}
	resolved, err := git.GetBaseBranch(projectPath)
	if err != nil {
		return "HEAD"
	}
	return resolved
}

func (s *Service) PendingWorkspace(project *data.Project, name, base string) *data.Workspace {
	if project == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	base = strings.TrimSpace(base)
	if base == "" {
		base = "HEAD"
	}
	projectRoot := s.pendingProjectRoot(project)
	if projectRoot == "" {
		return nil
	}
	return data.NewWorkspace(name, name, base, project.Path, filepath.Join(projectRoot, name))
}

func lexicalWorkspacePath(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	cleaned := filepath.Clean(value)
	if !filepath.IsAbs(cleaned) {
		if abs, err := filepath.Abs(cleaned); err == nil {
			cleaned = abs
		}
	}
	return cleaned
}

// pathWithin reports whether target is inside base, including base itself.
// Contract: inclusive (rel == "." counts), lexical only, fail-closed.
func pathWithin(base, target string) bool {
	_, inside := data.PathWithin(base, target)
	return inside
}

// pathWithinStrict reports whether target is strictly nested under base —
// the base itself does not count. Use for destructive checks so a root can
// never satisfy its own containment check.
func pathWithinStrict(base, target string) bool {
	return data.PathStrictlyWithin(base, target)
}

func pathWithinAliases(baseAliases, targetAliases []string) bool {
	for _, base := range baseAliases {
		for _, target := range targetAliases {
			if pathWithin(base, target) {
				return true
			}
		}
	}
	return false
}

func pathWithinAliasesStrict(baseAliases, targetAliases []string) bool {
	for _, base := range baseAliases {
		for _, target := range targetAliases {
			if pathWithinStrict(base, target) {
				return true
			}
		}
	}
	return false
}

func workspacePathAliases(path string) []string {
	canonical := lexicalWorkspacePath(path)
	if canonical == "" {
		return nil
	}
	unique := make(map[string]struct{}, 4)
	add := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		unique[trimmed] = struct{}{}
	}

	// Preserve lexical form for CWD-independent containment checks.
	add(canonical)
	// Add fully normalized form when the full path can be resolved.
	add(data.NormalizePath(canonical))
	// Add an alias built from resolving the deepest existing prefix; this
	// handles missing workspace roots under symlinked workspaces roots.
	if resolved, ok := resolveFromExistingPrefix(canonical); ok {
		add(resolved)
		add(data.NormalizePath(resolved))
	}

	aliases := make([]string, 0, len(unique))
	for value := range unique {
		aliases = append(aliases, value)
	}
	return aliases
}

// resolveFromExistingPrefix resolves symlinks through the deepest existing
// prefix and re-appends the unresolved tail — the containment-check sibling
// of data.resolveExistingAncestor. It differs on broken symlinks: Lstat still
// sees a dangling link, and resolvePrefixAlias falls back to readlink so a
// symlinked workspace root whose target is missing still resolves to its
// target spelling. Worktree containment needs that: a record's stored root
// may traverse a link that only exists during setup. Do not delegate to
// data.resolveExistingAncestor — its EvalSymlinks loop treats a dangling link
// as nonexistent and would strand the containment alias.
func resolveFromExistingPrefix(path string) (string, bool) {
	full := lexicalWorkspacePath(path)
	if full == "" {
		return "", false
	}
	for prefix := full; ; prefix = filepath.Dir(prefix) {
		if info, err := os.Lstat(prefix); err == nil {
			if resolved, ok := resolveFromPrefix(prefix, info, full); ok {
				return resolved, true
			}
		}
		parent := filepath.Dir(prefix)
		if parent == prefix {
			break
		}
	}
	return "", false
}

// resolveFromPrefix resolves full against the existing ancestor prefix: the
// prefix goes through resolvePrefixAlias (EvalSymlinks with a readlink
// fallback for dangling links) and the remainder of full is rejoined
// lexically. Returns false when the prefix itself won't resolve or full is
// not beneath it.
func resolveFromPrefix(prefix string, info os.FileInfo, full string) (string, bool) {
	resolvedPrefix, ok := resolvePrefixAlias(prefix, info)
	if !ok {
		return "", false
	}
	rel, err := filepath.Rel(prefix, full)
	if err != nil {
		return "", false
	}
	if rel == "." {
		return filepath.Clean(resolvedPrefix), true
	}
	return filepath.Clean(filepath.Join(resolvedPrefix, rel)), true
}

func resolvePrefixAlias(prefix string, info os.FileInfo) (string, bool) {
	if resolved, err := filepath.EvalSymlinks(prefix); err == nil {
		return filepath.Clean(resolved), true
	}
	// Fallback for broken symlinks: readlink works even when target is missing.
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(prefix)
		if err != nil {
			return "", false
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(prefix), target)
		}
		return filepath.Clean(target), true
	}
	return "", false
}
