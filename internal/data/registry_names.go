package data

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/validation"
)

// Name law for the registry — the pure helpers that decide what a project is
// called and where it lives. The registry's Name is the authority for the
// workspaces-root segment a project's worktrees live under; these helpers
// keep every consumer (parse repair, save, add, rename) on one rule.

// collisionIn reports whether name (case-insensitive) is already taken.
func collisionIn(taken map[string]struct{}, name string) bool {
	_, ok := taken[strings.ToLower(name)]
	return ok
}

// mintProjectName derives a project name for path that no entry in taken
// holds: the basename when free, then basename-2, basename-3, and so on.
// Matching is case-insensitive because the workspaces root commonly lives on
// a case-insensitive filesystem, where "Checkfu" and "checkfu" are the same
// segment.
func mintProjectName(path string, taken map[string]struct{}) string {
	base := filepath.Base(filepath.Clean(path))
	if !isSafeProjectName(base) {
		base = "project"
	}
	name := base
	for i := 2; collisionIn(taken, name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}

// isSafeProjectName reports whether name is usable as a single filesystem
// segment under the workspaces root. The rule lives once, in
// validation.ValidateProjectName — the mint/repair paths below need the
// boolean form, the rename paths the error form, and both must agree.
func isSafeProjectName(name string) bool {
	return validation.ValidateProjectName(name) == nil
}

// canonicalProjectPath canonicalizes a registry path for dedupe and lookup:
// trim + clean + absolutize, but deliberately NO symlink resolution — the
// registry dedupes by the literal spelling the user registered, so two
// spellings that resolve to the same directory stay distinct entries. For
// resolve-aware matching (e.g. is this stored path the same place as that
// live path) use CanonicalPath instead.
func canonicalProjectPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	return filepath.Clean(cleaned)
}

// normalizeAndDedupeEntries canonicalizes entry paths, drops empty ones, and
// dedupes by path keeping the first occurrence (with its name). Names that
// collide after the dedupe (hand-edited files) are re-minted so the saved
// list never carries two projects on one workspaces-root segment.
func normalizeAndDedupeEntries(entries []RegistryEntry) []RegistryEntry {
	out := make([]RegistryEntry, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	taken := make(map[string]struct{}, len(entries))

	for _, entry := range entries {
		canonical := canonicalProjectPath(entry.Path)
		if canonical == "" {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		entry.Path = canonical
		name := strings.TrimSpace(entry.Name)
		if !isSafeProjectName(name) || collisionIn(taken, name) {
			name = mintProjectName(canonical, taken)
		}
		taken[strings.ToLower(name)] = struct{}{}
		entry.Name = name
		out = append(out, entry)
	}

	return out
}
