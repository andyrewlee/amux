package panelaunch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPaneLaunchPrepareFileOwnership(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pane launch is unix-only")
	}
	stubTempRoot(t, t.TempDir())
	p, err := Prepare("/tmp", "echo hi", []string{"K=V"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer func() { _ = p.Discard() }()

	dirInfo, err := os.Lstat(p.Dir())
	if err != nil {
		t.Fatalf("attempt dir: %v", err)
	}
	if !dirInfo.IsDir() || dirInfo.Mode().Perm() != 0o700 || dirInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("attempt dir mode = %v", dirInfo.Mode())
	}
	fileInfo, err := os.Lstat(p.Path())
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	if !fileInfo.Mode().IsRegular() || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("payload mode = %v", fileInfo.Mode())
	}
}

// TestPaneLaunchPrepareKeepsValuesOutOfFileText asserts the stored payload
// carries env values only in their byte-field encoding — a plaintext marker
// must not appear in the file.
func TestPaneLaunchPrepareKeepsValuesOutOfFileText(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pane launch is unix-only")
	}
	stubTempRoot(t, t.TempDir())
	marker := "amux-pl-prepare-marker-7f3a"
	p, err := Prepare("/tmp", "echo public", []string{"AMUX_PL=" + marker})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer func() { _ = p.Discard() }()
	raw, err := os.ReadFile(p.Path())
	if err != nil {
		t.Fatalf("read payload: %v", err)
	}
	if strings.Contains(string(raw), marker) {
		t.Fatal("payload file contains the plaintext env marker")
	}
	// And the values are present in their encoded form, decodable.
	var decoded payload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("payload is not decodable: %v", err)
	}
	found := false
	for _, e := range decoded.Environment {
		if strings.Contains(string(e), marker) {
			found = true
		}
	}
	if !found {
		t.Fatal("env value missing from stored payload")
	}
}

func TestPaneLaunchPrepareRejections(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pane launch is unix-only")
	}
	stubTempRoot(t, t.TempDir())
	for _, tc := range []struct {
		name             string
		workDir, command string
		env              []string
	}{
		{"empty workdir", "", "echo hi", nil},
		{"NUL workdir", "/tmp/\x00", "echo hi", nil},
		{"NUL command", "/tmp", "echo \x00", nil},
		{"env without equals", "/tmp", "echo hi", []string{"NOEQUALS"}},
		{"env empty name", "/tmp", "echo hi", []string{"=v"}},
		{"env NUL", "/tmp", "echo hi", []string{"K=\x00"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Prepare(tc.workDir, tc.command, tc.env); err == nil {
				t.Fatal("Prepare accepted invalid spec")
			}
		})
	}
	// Rejection must not leave an attempt behind.
	entries, err := os.ReadDir(tempRootFn())
	if err != nil {
		t.Fatalf("list temp root: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), attemptPrefix) {
			t.Fatalf("rejected Prepare left attempt %s", e.Name())
		}
	}
}

// TestPaneLaunchPrepareRefusesWorkspaceTempRoot pins the containment rule:
// when the temp root resolves inside the launching workspace, Prepare must
// refuse rather than drop a payload into the managed worktree.
func TestPaneLaunchPrepareRefusesWorkspaceTempRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pane launch is unix-only")
	}
	workDir := t.TempDir()
	stubTempRoot(t, filepath.Join(workDir, "tmp"))
	if err := os.MkdirAll(tempRootFn(), 0o700); err != nil {
		t.Fatalf("create workspace tmp: %v", err)
	}
	if _, err := Prepare(workDir, "echo hi", nil); err == nil {
		t.Fatal("Prepare wrote a payload inside the workspace")
	}
}

func TestPaneLaunchPrepareDiscardLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pane launch is unix-only")
	}
	stubTempRoot(t, t.TempDir())
	p, err := Prepare("/tmp", "echo hi", []string{"K=V", "K2=V2"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if err := p.Discard(); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if _, err := os.Lstat(p.Dir()); !os.IsNotExist(err) {
		t.Fatal("attempt dir survived Discard")
	}
	// Idempotent.
	if err := p.Discard(); err != nil {
		t.Fatalf("second Discard: %v", err)
	}
}

// TestPaneLaunchPrepareEmptyEntriesDropped keeps the old transport's
// behavior: empty env strings are skipped, not delivered.
func TestPaneLaunchPrepareEmptyEntriesDropped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pane launch is unix-only")
	}
	stubTempRoot(t, t.TempDir())
	p, err := Prepare("/tmp", "echo hi", []string{"", "K=V"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer func() { _ = p.Discard() }()
	raw, err := os.ReadFile(p.Path())
	if err != nil {
		t.Fatalf("read payload: %v", err)
	}
	var decoded payload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Environment) != 1 || string(decoded.Environment[0]) != "K=V" {
		t.Fatalf("env = %v, want [K=V]", decoded.Environment)
	}
}
