package data

import (
	"path/filepath"
	"strings"
)

// NormalizePath returns a cleaned path with symlinks resolved when possible.
func NormalizePath(path string) string {
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		// Keep relative paths relative so identity hashes do not depend on CWD.
		return cleaned
	}
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return cleaned
	}
	return filepath.Clean(resolved)
}

func workspaceIdentity(repo, root string) string {
	return NormalizePath(repo) + "\n" + NormalizePath(root)
}

// resolveExistingAncestor normalizes a path by resolving symlinks through the
// deepest ancestor that currently exists, then re-appending the unresolved
// tail. Unlike NormalizePath — whose EvalSymlinks is all-or-nothing, so a
// missing leaf flips the result wholesale — the output is stable across the
// leaf appearing or disappearing as long as the ancestors persist. It is used
// to recognize metadata keys born under either normalization state.
func resolveExistingAncestor(path string) string {
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		return cleaned
	}
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	dir, tail := cleaned, ""
	for {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Clean(filepath.Join(resolved, tail))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cleaned
		}
		tail = filepath.Join(filepath.Base(dir), tail)
		dir = parent
	}
}

// SamePath reports whether two paths identify the same location. Unlike
// NormalizePath (which deliberately preserves relative paths so identity
// hashes stay CWD-independent), SamePath absolutizes relative paths against
// the process working directory before comparing, so a relative path and its
// absolute equivalent compare equal. Use it when a path recorded in one form
// must be matched against the same path recorded in another — e.g. an async
// result carrying a workspace root that predates a path-normalization rebind.
func SamePath(a, b string) bool {
	ca, cb := CanonicalPath(a), CanonicalPath(b)
	return ca != "" && ca == cb
}

// CanonicalPath returns the match form of path: trimmed, cleaned, absolutized
// against the process working directory, and symlink-resolved when possible.
// Unlike NormalizePath it always absolutizes (a relative path and its
// absolute equivalent produce the same result) — use it to compare paths
// recorded in different forms, not to mint identity keys (identity needs
// NormalizePath's CWD-independent relative preservation). Nonexistent tails
// and unresolvable input fall back to the cleaned absolute form.
func CanonicalPath(path string) string {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "." || cleaned == "" {
		return ""
	}
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		cleaned = resolved
	}
	return filepath.Clean(cleaned)
}

// PathWithin returns candidate's path relative to root and whether it sits
// inside root, where "inside" includes root itself (rel == "."). It is purely
// lexical — the single filepath.Rel + ".."-escape kernel the destructive-path
// containment checks share. Both empty inputs and escaping candidates report
// inside == false. Callers needing strict nesting (the root itself excluded)
// should use PathStrictlyWithin or check rel != "." themselves when they also
// need the relative path.
//
// Alias expansion is deliberately the caller's job: which alias spellings to
// compare (canonicalized, deepest-existing-prefix, dangling-link tolerant) is
// a per-callsite contract.
func PathWithin(root, candidate string) (rel string, inside bool) {
	if root == "" || candidate == "" {
		return "", false
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel, false
	}
	return rel, true
}

// PathStrictlyWithin reports whether candidate is strictly nested under root
// — inside and not the root itself. Destructive-flow checks use this form so
// the root can never satisfy its own containment check.
func PathStrictlyWithin(root, candidate string) bool {
	rel, inside := PathWithin(root, candidate)
	return inside && rel != "."
}
