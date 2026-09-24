package data

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizePath_ReResolvesAfterPathCreated(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(realRoot) error = %v", err)
	}

	linkRoot := filepath.Join(base, "link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	missingViaLink := filepath.Join(linkRoot, "new-workspace")

	first := NormalizePath(missingViaLink)
	if first != filepath.Clean(missingViaLink) {
		t.Fatalf("first normalization = %q, want %q", first, filepath.Clean(missingViaLink))
	}

	realPath := filepath.Join(realRoot, "new-workspace")
	if err := os.MkdirAll(realPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(realPath) error = %v", err)
	}

	second := NormalizePath(missingViaLink)
	resolvedReal, err := filepath.EvalSymlinks(realPath)
	if err != nil {
		t.Fatalf("EvalSymlinks(realPath) error = %v", err)
	}
	want := filepath.Clean(resolvedReal)
	if second != want {
		t.Fatalf("second normalization = %q, want %q", second, want)
	}
}

func TestNormalizePath_ReResolvesAfterSymlinkRetarget(t *testing.T) {
	base := t.TempDir()

	realA := filepath.Join(base, "real-a")
	realB := filepath.Join(base, "real-b")
	workspaceA := filepath.Join(realA, "workspace")
	workspaceB := filepath.Join(realB, "workspace")
	if err := os.MkdirAll(workspaceA, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspaceA) error = %v", err)
	}
	if err := os.MkdirAll(workspaceB, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspaceB) error = %v", err)
	}

	linkRoot := filepath.Join(base, "link-root")
	if err := os.Symlink(realA, linkRoot); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	pathViaLink := filepath.Join(linkRoot, "workspace")
	first := NormalizePath(pathViaLink)
	resolvedA, err := filepath.EvalSymlinks(workspaceA)
	if err != nil {
		t.Fatalf("EvalSymlinks(workspaceA) error = %v", err)
	}
	wantFirst := filepath.Clean(resolvedA)
	if first != wantFirst {
		t.Fatalf("first normalization = %q, want %q", first, wantFirst)
	}

	if err := os.Remove(linkRoot); err != nil {
		t.Fatalf("Remove(linkRoot) error = %v", err)
	}
	if err := os.Symlink(realB, linkRoot); err != nil {
		t.Fatalf("Symlink(realB) error = %v", err)
	}

	second := NormalizePath(pathViaLink)
	resolvedB, err := filepath.EvalSymlinks(workspaceB)
	if err != nil {
		t.Fatalf("EvalSymlinks(workspaceB) error = %v", err)
	}
	wantSecond := filepath.Clean(resolvedB)
	if second != wantSecond {
		t.Fatalf("second normalization = %q, want %q", second, wantSecond)
	}
}

// TestCanonicalPath_AbsolutizesRelativeInput pins the match contract's key
// difference from NormalizePath: a relative input absolutizes against CWD so
// it compares equal to its absolute spelling; NormalizePath must NOT do this
// (identity hashes stay CWD-independent).
func TestCanonicalPath_AbsolutizesRelativeInput(t *testing.T) {
	rel := filepath.Join("tmp", "canonicalpath-rel-probe")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("Abs error = %v", err)
	}
	got := CanonicalPath(rel)
	want := filepath.Clean(abs)
	if resolved, resolveErr := filepath.EvalSymlinks(want); resolveErr == nil {
		want = filepath.Clean(resolved)
	}
	if got != want {
		t.Fatalf("CanonicalPath(%q) = %q, want %q", rel, got, want)
	}
	if NormalizePath(rel) != filepath.Clean(rel) {
		t.Fatalf("NormalizePath must keep %q relative, got %q", rel, NormalizePath(rel))
	}
}

// TestCanonicalPath_EmptyAndDotYieldEmpty pins the degenerate inputs: "" and
// any spelling of "." return "" rather than resolving to the process CWD —
// a "." input must never match CWD in lookup comparisons.
func TestCanonicalPath_EmptyAndDotYieldEmpty(t *testing.T) {
	for _, in := range []string{"", "  ", ".", " . ", "./"} {
		if got := CanonicalPath(in); got != "" {
			t.Fatalf("CanonicalPath(%q) = %q, want empty", in, got)
		}
	}
}

// TestCanonicalPath_NonexistentTailFallsBackCleaned pins the resolve-or-
// fallback policy: an unresolvable (nonexistent) absolute path returns the
// cleaned absolute form, not "" and not an error.
func TestCanonicalPath_NonexistentTailFallsBackCleaned(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-dir", "leaf")
	got := CanonicalPath(missing + "  ")
	want := filepath.Clean(missing)
	if got != want {
		t.Fatalf("CanonicalPath(%q) = %q, want %q", missing, got, want)
	}
}

// TestCanonicalProjectPath_DoesNotResolveSymlinks pins the registry's
// lexical-only contract: a symlinked registered path dedupes by literal
// spelling, so canonicalProjectPath must NOT collapse it to the target —
// that's what CanonicalPath is for.
func TestCanonicalProjectPath_DoesNotResolveSymlinks(t *testing.T) {
	base := t.TempDir()
	realProj := filepath.Join(base, "real-proj")
	if err := os.MkdirAll(realProj, 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	link := filepath.Join(base, "link-proj")
	if err := os.Symlink(realProj, link); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	got := canonicalProjectPath(link)
	if got != filepath.Clean(link) {
		t.Fatalf("canonicalProjectPath(%q) = %q, want the lexical spelling %q", link, got, filepath.Clean(link))
	}
	resolved, err := filepath.EvalSymlinks(realProj)
	if err != nil {
		t.Fatalf("EvalSymlinks error = %v", err)
	}
	if CanonicalPath(link) != filepath.Clean(resolved) {
		t.Fatalf("CanonicalPath(%q) = %q, want resolved %q", link, CanonicalPath(link), filepath.Clean(resolved))
	}
}

func TestPathWithin_ContractMatrix(t *testing.T) {
	root := filepath.Join("repo", "ws")
	cases := []struct {
		name      string
		root      string
		candidate string
		wantRel   string
		inside    bool
		strict    bool
	}{
		{"same path is inside but not strict", root, root, ".", true, false},
		{"child", root, filepath.Join(root, "sub"), "sub", true, true},
		{"nested child", root, filepath.Join(root, "a", "b"), filepath.Join("a", "b"), true, true},
		{"escape via dotdot", root, filepath.Join(root, "..", "other"), "", false, false},
		{"escape above root", root, filepath.Join("repo"), "", false, false},
		{"sibling prefix is not inside", root, root + "2", "", false, false},
		{"empty root fails closed", "", root, "", false, false},
		{"empty candidate fails closed", root, "", "", false, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rel, inside := PathWithin(tt.root, tt.candidate)
			if inside != tt.inside {
				t.Fatalf("PathWithin(%q, %q) inside = %v, want %v", tt.root, tt.candidate, inside, tt.inside)
			}
			if inside && rel != tt.wantRel {
				t.Fatalf("PathWithin(%q, %q) rel = %q, want %q", tt.root, tt.candidate, rel, tt.wantRel)
			}
			if got := PathStrictlyWithin(tt.root, tt.candidate); got != tt.strict {
				t.Fatalf("PathStrictlyWithin(%q, %q) = %v, want %v", tt.root, tt.candidate, got, tt.strict)
			}
		})
	}
}

func TestPathWithin_IsLexicalOnly(t *testing.T) {
	// The kernel must not canonicalize: a symlinked candidate spelling stays
	// inside its lexical root — alias expansion is the caller's contract.
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	// Lexically, link/child is inside link — even though it resolves outside
	// the lexical tree only by symlink (into base/realDir, still under base).
	if _, inside := PathWithin(link, filepath.Join(link, "child")); !inside {
		t.Fatal("lexical child of a symlinked root should be inside")
	}
	// A nonexistent path still resolves lexically.
	if _, inside := PathWithin(base, filepath.Join(base, "missing", "deep")); !inside {
		t.Fatal("nonexistent nested path should be inside")
	}
}
