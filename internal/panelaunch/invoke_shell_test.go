//go:build !windows

package panelaunch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveShellSkipsNonAbsolutePaths pins the untrusted-workdir contract:
// resolution runs after chdir into the payload dir, so empty and relative
// PATH elements — which would resolve `sh` against that dir — are skipped.
func TestResolveShellSkipsNonAbsolutePaths(t *testing.T) {
	workDir := t.TempDir()
	hostile := filepath.Join(workDir, "sh")
	if err := os.WriteFile(hostile, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	absBin := t.TempDir()
	good := filepath.Join(absBin, "sh")
	if err := os.WriteFile(good, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workDir)

	for _, tc := range []struct {
		name string
		path string
	}{
		{"dot element", ".:" + absBin},
		{"empty leading element", ":" + absBin},
		{"empty middle element", absBin + "::/nonexistent"},
		{"relative element", "reldir:" + absBin},
		{"trailing colon cwd", absBin + ":"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveShell([]string{"PATH=" + tc.path})
			if err != nil {
				t.Fatalf("resolveShell: %v", err)
			}
			if got != good {
				t.Fatalf("resolveShell = %q, want %q", got, good)
			}
		})
	}

	// A PATH with only non-absolute elements has no usable shell.
	if _, err := resolveShell([]string{"PATH=.:reldir:"}); err == nil {
		t.Fatal("resolveShell with only non-absolute elements should fail")
	}
}

// TestPaneLaunchRunNeverExecsWorkdirShadow drives the full invoke path: the
// payload's own PATH ships an empty element, and the payload workdir contains
// an executable `sh` — exec must receive an absolute-path resolution, never
// the workdir file.
func TestPaneLaunchRunNeverExecsWorkdirShadow(t *testing.T) {
	stubTempRoot(t, t.TempDir())
	workDir := t.TempDir()
	hostile := filepath.Join(workDir, "sh")
	if err := os.WriteFile(hostile, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	p, err := Prepare(workDir, "true", []string{"PATH=:/usr/bin:/bin"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	var gotPath string
	stubExec(t, func(path string, argv, env []string) error {
		gotPath = path
		return errUnreachable
	})
	cwdBefore, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwdBefore) })

	// execFn is stubbed, so runPayload reports stageExec — the assertion is
	// on the path it was asked to exec.
	_ = runPayload(p.Path())
	if !filepath.IsAbs(gotPath) || strings.HasPrefix(gotPath, workDir) {
		t.Fatalf("exec path = %q, want an absolute shell outside the payload dir", gotPath)
	}
}
