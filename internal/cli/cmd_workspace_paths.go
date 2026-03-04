package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

func shouldSurfaceWorkspaceForCLI(workspacesRoot string, ws *data.Workspace, cache *symlinkCache) bool {
	if ws == nil {
		return false
	}
	if ws.IsPrimaryCheckout() {
		return true
	}
	managedRoot := lexicalWorkspacePathCLI(workspacesRoot)
	wsRoot := lexicalWorkspacePathCLI(ws.Root)
	if managedRoot == "" || wsRoot == "" {
		return false
	}
	return pathWithinAliasesCLI(workspacePathAliasesCLI(managedRoot, cache), workspacePathAliasesCLI(wsRoot, cache))
}

func lexicalWorkspacePathCLI(path string) string {
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

func pathWithinCLI(base, target string) bool {
	if base == "" || target == "" {
		return false
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func pathWithinAliasesCLI(baseAliases, targetAliases []string) bool {
	for _, base := range baseAliases {
		for _, target := range targetAliases {
			if pathWithinCLI(base, target) {
				return true
			}
		}
	}
	return false
}

func workspacePathAliasesCLI(path string, cache *symlinkCache) []string {
	canonical := lexicalWorkspacePathCLI(path)
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

	add(canonical)
	add(data.NormalizePath(canonical))
	if resolved, ok := resolveFromExistingPrefixCLI(canonical, cache); ok {
		add(resolved)
		add(data.NormalizePath(resolved))
	}

	aliases := make([]string, 0, len(unique))
	for value := range unique {
		aliases = append(aliases, value)
	}
	return aliases
}

func resolveFromExistingPrefixCLI(path string, cache *symlinkCache) (string, bool) {
	full := lexicalWorkspacePathCLI(path)
	if full == "" {
		return "", false
	}
	for prefix := full; ; prefix = filepath.Dir(prefix) {
		if info, err := os.Lstat(prefix); err == nil {
			resolvedPrefix, ok := resolvePrefixAliasCLI(prefix, info, cache)
			if ok {
				rel, relErr := filepath.Rel(prefix, full)
				if relErr == nil {
					if rel == "." {
						return filepath.Clean(resolvedPrefix), true
					}
					return filepath.Clean(filepath.Join(resolvedPrefix, rel)), true
				}
			}
		}
		parent := filepath.Dir(prefix)
		if parent == prefix {
			break
		}
	}
	return "", false
}

type symlinkCache struct {
	resolved map[string]string
}

func newSymlinkCache() *symlinkCache {
	return &symlinkCache{resolved: make(map[string]string)}
}

func (c *symlinkCache) evalSymlinks(path string) (string, error) {
	if r, ok := c.resolved[path]; ok {
		return r, nil
	}
	r, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	c.resolved[path] = r
	return r, nil
}

func resolvePrefixAliasCLI(prefix string, info os.FileInfo, cache *symlinkCache) (string, bool) {
	if resolved, err := cache.evalSymlinks(prefix); err == nil {
		return filepath.Clean(resolved), true
	}
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
