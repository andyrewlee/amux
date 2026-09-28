package e2e

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestFakeAgentBuildFailureRemovesOwnedDir: a failed build removes the
// directory it created — including a partial binary — and nothing else.
func TestFakeAgentBuildFailureRemovesOwnedDir(t *testing.T) {
	dest := t.TempDir()
	var ownedDir string
	_, err := buildFakeAgentBinary(dest, func(outPath string) error {
		ownedDir = filepath.Dir(outPath)
		// Simulate a partial build artifact before failing.
		if werr := os.WriteFile(outPath, []byte("partial"), 0o755); werr != nil {
			t.Fatalf("write partial: %v", werr)
		}
		return errors.New("build exploded")
	})
	if err == nil {
		t.Fatal("expected build error")
	}
	if ownedDir == "" || !strings.HasPrefix(filepath.Base(ownedDir), fakeAgentBuildDirPrefix) {
		t.Fatalf("helper did not allocate an owned dir under %q: %q", dest, ownedDir)
	}
	if _, serr := os.Stat(ownedDir); !os.IsNotExist(serr) {
		t.Fatalf("failed build left owned dir behind: %s", ownedDir)
	}
	// The destination root itself must survive.
	if entries, rerr := os.ReadDir(dest); rerr != nil || len(entries) != 0 {
		t.Fatalf("destination root disturbed: entries=%v err=%v", entries, rerr)
	}
}

// TestFakeAgentBuildSuccessKeepsOwnedDir: a successful build keeps its
// directory for the process-level cleanup to remove later.
func TestFakeAgentBuildSuccessKeepsOwnedDir(t *testing.T) {
	dest := t.TempDir()
	out, err := buildFakeAgentBinary(dest, func(outPath string) error {
		return os.WriteFile(outPath, []byte("bin"), 0o755)
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	dir := filepath.Dir(out)
	if _, serr := os.Stat(out); serr != nil {
		t.Fatalf("successful build lost its binary: %v", serr)
	}
	// Ownership transferred: cleanup removes it, and a second call is a no-op.
	if err := cleanupOwnedFakeAgentDir(out); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, serr := os.Stat(dir); !os.IsNotExist(serr) {
		t.Fatalf("cleanup left the owned dir: %s", dir)
	}
	if err := cleanupOwnedFakeAgentDir(out); err != nil {
		t.Fatalf("cleanup not idempotent: %v", err)
	}
}

// TestCleanupFakeAgentEmptyAndRefusal: empty path is a no-op; a directory
// outside the owned naming convention is refused and left intact.
func TestCleanupFakeAgentEmptyAndRefusal(t *testing.T) {
	if err := cleanupOwnedFakeAgentDir(""); err != nil {
		t.Fatalf("empty path should be a no-op, got %v", err)
	}

	unrelated := t.TempDir() // not an amux-fakeagent-* name
	sentinel := filepath.Join(unrelated, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	err := cleanupOwnedFakeAgentDir(filepath.Join(unrelated, "fakeagent"))
	if err == nil {
		t.Fatal("expected refusal for unowned directory")
	}
	if _, serr := os.Stat(sentinel); serr != nil {
		t.Fatalf("refusal deleted the unrelated dir anyway: %v", serr)
	}
}

// TestFakeAgentCleanupHelper runs only inside the subprocess spawned by
// TestFakeAgentCleanupAfterTestMain. It builds a fake binary through the real
// owned helper, records the directory for the parent, and relies on the
// process's TestMain to remove it after m.Run — proving process-lifetime
// cleanup, not per-test cleanup.
func TestFakeAgentCleanupHelper(t *testing.T) {
	if os.Getenv("AMUX_E2E_FAKEAGENT_CLEANUP_HELPER") != "1" {
		// Helper-process stub: return (not Skip) so STRICT_TMUX's skip
		// counter — which guards real tmux coverage — never counts it.
		return
	}
	outFile := os.Getenv("AMUX_E2E_FAKEAGENT_DIR_OUT")
	if outFile == "" {
		t.Fatal("AMUX_E2E_FAKEAGENT_DIR_OUT unset")
	}
	out, err := buildFakeAgentBinary(os.TempDir(), func(outPath string) error {
		return os.WriteFile(outPath, []byte("bin"), 0o755)
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	fakeAgentPath = out // this process's owned binary, as the real once-build sets it
	dir := filepath.Dir(out)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("owned dir missing before exit: %v", err)
	}
	if err := os.WriteFile(outFile, []byte(dir), 0o644); err != nil {
		t.Fatalf("record dir: %v", err)
	}
}

// TestFakeAgentCleanupAfterTestMain runs the test binary as a subprocess
// executing only the helper test, with a private TMPDIR, and asserts the
// owned fakeagent directory is gone after normal TestMain exit.
func TestFakeAgentCleanupAfterTestMain(t *testing.T) {
	helperTmp := t.TempDir()
	outFile := filepath.Join(t.TempDir(), "dir.txt")
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable: %v", err)
	}
	cmd := exec.Command(self, "-test.run=^TestFakeAgentCleanupHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"AMUX_E2E_FAKEAGENT_CLEANUP_HELPER=1",
		"AMUX_E2E_FAKEAGENT_DIR_OUT="+outFile,
		"TMPDIR="+helperTmp,
	)
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper subprocess failed: %v\n%s", err, combined)
	}
	dirBytes, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("helper did not record its dir: %v", err)
	}
	dir := strings.TrimSpace(string(dirBytes))
	if !strings.HasPrefix(filepath.Base(dir), fakeAgentBuildDirPrefix) {
		t.Fatalf("helper recorded unexpected dir %q", dir)
	}
	if filepath.Dir(dir) != helperTmp {
		t.Fatalf("helper built outside its private TMPDIR: %q", dir)
	}
	if _, serr := os.Stat(dir); !os.IsNotExist(serr) {
		t.Fatalf("owned fakeagent dir survived TestMain cleanup: %s", dir)
	}
}
