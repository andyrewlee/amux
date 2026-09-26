package update

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/shellutil"
)

// TestInstallBinaryKeepsPathnamePresent proves the pre-replacement continuity
// contract: inside the staged→target rename seam the live pathname still holds
// the complete old binary, and afterwards it holds the complete new one. The
// old design renamed the live pathname away first, so this assertion failed
// there by construction.
func TestInstallBinaryKeepsPathnamePresent(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "new-amux")
	if err := os.WriteFile(srcPath, []byte("new binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(src): %v", err)
	}
	destPath := filepath.Join(tmpDir, "amux")
	if err := os.WriteFile(destPath, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(dest): %v", err)
	}

	t.Cleanup(func() { renameFile = os.Rename })
	renameFile = func(oldpath, newpath string) error {
		if newpath == destPath && isInstallStagedPath(tmpDir, oldpath) {
			content, err := os.ReadFile(destPath)
			if err != nil {
				t.Errorf("live pathname must exist immediately before replacement: %v", err)
			} else if string(content) != "old binary" {
				t.Errorf("pre-replacement pathname holds %q, want old binary", content)
			}
		}
		return os.Rename(oldpath, newpath)
	}

	if err := InstallBinary(srcPath, destPath); err != nil {
		t.Fatalf("InstallBinary() error = %v", err)
	}
	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("ReadFile(dest): %v", err)
	}
	if string(content) != "new binary" {
		t.Fatalf("post-install content = %q, want new binary", content)
	}
	info, err := os.Stat(destPath)
	if err != nil {
		t.Fatalf("Stat(dest): %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("installed mode = %03o, want executable bits", info.Mode().Perm())
	}
	assertNoInstallTemps(t, tmpDir, "amux")
}

// TestInstallBinaryPreCommitSyncFailurePreservesTarget: a directory-sync
// failure before the rename keeps the old binary live and removes both temps.
func TestInstallBinaryPreCommitSyncFailurePreservesTarget(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "new-amux")
	if err := os.WriteFile(srcPath, []byte("new"), 0o755); err != nil {
		t.Fatalf("WriteFile(src): %v", err)
	}
	destPath := filepath.Join(tmpDir, "amux")
	if err := os.WriteFile(destPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("WriteFile(dest): %v", err)
	}

	injected := errors.New("injected pre-commit sync failure")
	t.Cleanup(func() { syncInstallDir = syncInstallDirImpl })
	syncInstallDir = func(string) error { return injected }

	err := InstallBinary(srcPath, destPath)
	if !errors.Is(err, injected) {
		t.Fatalf("InstallBinary() error = %v, want wrapped sync cause", err)
	}
	if !strings.Contains(err.Error(), "before replacement") {
		t.Fatalf("error must identify the pre-commit phase, got: %v", err)
	}
	content, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("Current binary should still exist: %v", readErr)
	}
	if string(content) != "old" {
		t.Fatalf("pre-commit failure must leave 'old', got %q", content)
	}
	assertNoInstallTemps(t, tmpDir, "amux")
}

// TestInstallBinaryPostCommitSyncFailureKeepsBackup: a directory-sync failure
// after the rename must not roll back — the new binary stays installed, the
// complete old backup is retained, and the error names it with a quoted
// manual recovery hint.
func TestInstallBinaryPostCommitSyncFailureKeepsBackup(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "new-amux")
	if err := os.WriteFile(srcPath, []byte("new"), 0o755); err != nil {
		t.Fatalf("WriteFile(src): %v", err)
	}
	destPath := filepath.Join(tmpDir, "amux")
	if err := os.WriteFile(destPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("WriteFile(dest): %v", err)
	}

	injected := errors.New("injected post-commit sync failure")
	t.Cleanup(func() { syncInstallDir = syncInstallDirImpl })
	var calls int
	syncInstallDir = func(dir string) error {
		calls++
		if calls == 2 {
			return injected
		}
		return syncInstallDirImpl(dir)
	}

	err := InstallBinary(srcPath, destPath)
	if !errors.Is(err, injected) {
		t.Fatalf("InstallBinary() error = %v, want wrapped sync cause", err)
	}
	if !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("error must state the replacement already happened, got: %v", err)
	}

	// New bytes are live; nothing was rolled back.
	content, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("ReadFile(dest): %v", readErr)
	}
	if string(content) != "new" {
		t.Fatalf("post-commit failure must keep 'new', got %q", content)
	}

	// Exactly one retained backup file, holding the old bytes, named in the
	// error with an accurately quoted recovery hint.
	var backupPath string
	entries, readDirErr := os.ReadDir(tmpDir)
	if readDirErr != nil {
		t.Fatalf("ReadDir: %v", readDirErr)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".amux.bak-") {
			backupPath = filepath.Join(tmpDir, e.Name())
		}
	}
	if backupPath == "" {
		t.Fatal("post-commit sync failure must retain the backup")
	}
	backup, readErr := os.ReadFile(backupPath)
	if readErr != nil {
		t.Fatalf("ReadFile(backup): %v", readErr)
	}
	if string(backup) != "old" {
		t.Fatalf("backup content = %q, want old", backup)
	}
	if !strings.Contains(err.Error(), backupPath) {
		t.Errorf("error must name the backup path %q, got: %v", backupPath, err)
	}
	wantHint := "mv " + shellutil.ShellQuote(backupPath) + " " + shellutil.ShellQuote(destPath)
	if !strings.Contains(err.Error(), wantHint) {
		t.Errorf("error must include the quoted recovery hint %q, got: %v", wantHint, err)
	}
}

// phaseFailFile wraps a real destination file, injecting a failure at one
// phase so copyFile's partial-destination cleanup is exercised against a file
// that genuinely exists on disk.
type phaseFailFile struct {
	*os.File
	failAt  string
	injects error
}

func (f *phaseFailFile) Write(p []byte) (int, error) {
	if f.failAt == "write" {
		return 0, f.injects
	}
	return f.File.Write(p)
}

// ReadFrom is promoted from *os.File and would let io.Copy bypass the
// injected Write failure, so it must honor the same phase.
func (f *phaseFailFile) ReadFrom(r io.Reader) (int64, error) {
	if f.failAt == "write" {
		return 0, f.injects
	}
	return f.File.ReadFrom(r)
}

func (f *phaseFailFile) Sync() error {
	if f.failAt == "sync" {
		return f.injects
	}
	return f.File.Sync()
}

func (f *phaseFailFile) Close() error {
	if f.failAt == "close" {
		return f.injects
	}
	return f.File.Close()
}

// TestCopyFileRemovesPartialDestination: a write, sync, or close failure on a
// destination copyFile created must not leave the partial file behind.
func TestCopyFileRemovesPartialDestination(t *testing.T) {
	for _, phase := range []string{"write", "sync", "close"} {
		t.Run(phase, func(t *testing.T) {
			tmpDir := t.TempDir()
			src := filepath.Join(tmpDir, "src")
			dst := filepath.Join(tmpDir, "dst")
			if err := os.WriteFile(src, []byte("partial"), 0o600); err != nil {
				t.Fatalf("WriteFile(src): %v", err)
			}
			injected := errors.New("injected " + phase + " failure")

			originalDest := openCopyDestFile
			t.Cleanup(func() { openCopyDestFile = originalDest })
			openCopyDestFile = func(name string, flag int, perm os.FileMode) (syncWriteCloser, error) {
				f, err := openFileInParentRoot(name, flag, perm)
				if err != nil {
					return nil, err
				}
				return &phaseFailFile{File: f, failAt: phase, injects: injected}, nil
			}

			if err := copyFile(src, dst); !errors.Is(err, injected) {
				t.Fatalf("copyFile() error = %v, want injected %s failure", err, phase)
			}
			if _, err := os.Stat(dst); !os.IsNotExist(err) {
				t.Fatalf("partial destination must be removed after %s failure, stat err = %v", phase, err)
			}
		})
	}
}

// TestInstallHelperProcess is the re-exec'd child: it writes real fixture
// files, parks inside installPhaseHook at the requested phase after writing a
// marker file, and is SIGKILLed by the parent — so the parent's assertion is
// sequenced by the marker, never a timer. Never runs standalone.
func TestInstallHelperProcess(t *testing.T) {
	if os.Getenv("AMUX_INSTALL_HELPER") != "1" {
		t.Skip("helper process only")
	}
	src := os.Getenv("AMUX_INSTALL_HELPER_SRC")
	dest := os.Getenv("AMUX_INSTALL_HELPER_DEST")
	marker := os.Getenv("AMUX_INSTALL_HELPER_MARKER")
	phase := os.Getenv("AMUX_INSTALL_HELPER_PHASE")
	if err := os.WriteFile(src, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	installPhaseHook = func(got string) {
		if got != phase {
			return
		}
		if err := os.WriteFile(marker, []byte("reached"), 0o600); err != nil {
			t.Fatal(err)
		}
		select {}
	}
	_ = InstallBinary(src, dest)
}

// killHelperAtPhase runs the install helper, waits for the phase marker, then
// SIGKILLs it and returns the fixture paths.
func killHelperAtPhase(t *testing.T, phase string) (destPath, tmpDir string) {
	t.Helper()
	tmpDir = t.TempDir()
	srcPath := filepath.Join(tmpDir, "new-amux")
	destPath = filepath.Join(tmpDir, "amux")
	marker := filepath.Join(tmpDir, "reached")

	cmd := exec.Command(os.Args[0], "-test.run", "^TestInstallHelperProcess$")
	cmd.Env = append(os.Environ(),
		"AMUX_INSTALL_HELPER=1",
		"AMUX_INSTALL_HELPER_PHASE="+phase,
		"AMUX_INSTALL_HELPER_MARKER="+marker,
		"AMUX_INSTALL_HELPER_SRC="+srcPath,
		"AMUX_INSTALL_HELPER_DEST="+destPath,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("helper never reached phase %q", phase)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	_ = cmd.Wait()
	return destPath, tmpDir
}

// TestInstallBinaryKillBeforeRenameKeepsOld kills a helper parked after the
// backup copy and pre-commit sync but before the replacement rename: the live
// pathname must still contain the old binary.
func TestInstallBinaryKillBeforeRenameKeepsOld(t *testing.T) {
	if os.Getenv("AMUX_INSTALL_HELPER") == "1" {
		t.Skip("running inside helper")
	}
	destPath, _ := killHelperAtPhase(t, "prepared")

	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("live pathname must survive a pre-rename kill: %v", err)
	}
	if string(content) != "old" {
		t.Fatalf("pre-rename kill left %q at the live pathname, want old", content)
	}
}

// TestInstallBinaryKillAfterRenameKeepsNew kills a helper parked immediately
// after the replacement rename: the live pathname must already hold the new
// binary. (This proves the rename is atomic with respect to the killed
// process only — a real power loss could still lose an unsynced directory
// entry; durability is the directory sync's job.)
func TestInstallBinaryKillAfterRenameKeepsNew(t *testing.T) {
	if os.Getenv("AMUX_INSTALL_HELPER") == "1" {
		t.Skip("running inside helper")
	}
	destPath, _ := killHelperAtPhase(t, "replaced")

	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("live pathname must survive a post-rename kill: %v", err)
	}
	if string(content) != "new" {
		t.Fatalf("post-rename kill left %q at the live pathname, want new", content)
	}
}
