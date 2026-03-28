package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/sandbox"
)

func TestCurrentCLIWorkingDirUsesInitCwdWhenWrapperChangesProcessCwd(t *testing.T) {
	initCwd := t.TempDir()
	t.Setenv("INIT_CWD", initCwd)
	t.Setenv("PWD", initCwd)
	chdirForTest(t, t.TempDir())

	got, err := currentCLIWorkingDir()
	if err != nil {
		t.Fatalf("currentCLIWorkingDir() error = %v", err)
	}
	if !sameCLIPath(got, initCwd) {
		t.Fatalf("currentCLIWorkingDir() = %q, want %q", got, initCwd)
	}
}

func TestCurrentCLIWorkingDirPreservesSymlinkedWrapperPath(t *testing.T) {
	parent := t.TempDir()
	realCwd := filepath.Join(parent, "real")
	if err := os.Mkdir(realCwd, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	linkCwd := filepath.Join(parent, "link")
	if err := os.Symlink(realCwd, linkCwd); err != nil {
		t.Skipf("Symlink() error = %v", err)
	}

	t.Setenv("INIT_CWD", linkCwd)
	t.Setenv("PWD", linkCwd)
	chdirForTest(t, realCwd)

	got, err := currentCLIWorkingDir()
	if err != nil {
		t.Fatalf("currentCLIWorkingDir() error = %v", err)
	}
	if got != linkCwd {
		t.Fatalf("currentCLIWorkingDir() = %q, want logical symlink path %q", got, linkCwd)
	}
	if sandbox.ComputeWorktreeID(got) == sandbox.ComputeWorktreeID(realCwd) {
		t.Fatalf("currentCLIWorkingDir() lost symlink identity: got %q real %q", got, realCwd)
	}
}

func TestCurrentCLIWorkingDirPrefersInitCwdWhenPackageManagerRewritesPWD(t *testing.T) {
	root := t.TempDir()
	initCwd := filepath.Join(root, "sub")
	if err := os.Mkdir(initCwd, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	t.Setenv("INIT_CWD", initCwd)
	t.Setenv("PWD", root)
	t.Setenv("npm_package_json", filepath.Join(root, "package.json"))
	chdirForTest(t, root)

	got, err := currentCLIWorkingDir()
	if err != nil {
		t.Fatalf("currentCLIWorkingDir() error = %v", err)
	}
	if got != initCwd {
		t.Fatalf("currentCLIWorkingDir() = %q, want INIT_CWD %q", got, initCwd)
	}
}

func TestCurrentCLIWorkingDirPrefersLogicalPWDOverCanonicalInitCwd(t *testing.T) {
	parent := t.TempDir()
	realRoot := filepath.Join(parent, "real")
	if err := os.Mkdir(realRoot, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	linkRoot := filepath.Join(parent, "link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("Symlink() error = %v", err)
	}

	t.Setenv("INIT_CWD", realRoot)
	t.Setenv("PWD", linkRoot)
	t.Setenv("npm_package_json", filepath.Join(realRoot, "package.json"))
	chdirForTest(t, realRoot)

	got, err := currentCLIWorkingDir()
	if err != nil {
		t.Fatalf("currentCLIWorkingDir() error = %v", err)
	}
	if got != linkRoot {
		t.Fatalf("currentCLIWorkingDir() = %q, want logical PWD %q", got, linkRoot)
	}
}

func TestCurrentCLIWorkingDirPrefersRealCwdWhenPWDAndInitCwdAreStale(t *testing.T) {
	root := t.TempDir()
	actualCwd := filepath.Join(root, "packages", "app")
	if err := os.MkdirAll(actualCwd, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	t.Setenv("INIT_CWD", root)
	t.Setenv("PWD", root)
	t.Setenv("npm_package_json", filepath.Join(root, "package.json"))
	chdirForTest(t, actualCwd)

	got, err := currentCLIWorkingDir()
	if err != nil {
		t.Fatalf("currentCLIWorkingDir() error = %v", err)
	}
	if !sameCLIPath(got, actualCwd) {
		t.Fatalf("currentCLIWorkingDir() = %q, want real cwd %q", got, actualCwd)
	}
}

func TestCurrentCLIWorkingDirPrefersProcessCwdWhenPWDMatchesIt(t *testing.T) {
	initCwd := t.TempDir()
	currentCwd := t.TempDir()
	t.Setenv("INIT_CWD", initCwd)
	t.Setenv("PWD", currentCwd)
	chdirForTest(t, currentCwd)

	got, err := currentCLIWorkingDir()
	if err != nil {
		t.Fatalf("currentCLIWorkingDir() error = %v", err)
	}
	if !sameCLIPath(got, currentCwd) {
		t.Fatalf("currentCLIWorkingDir() = %q, want %q", got, currentCwd)
	}
}

func TestCurrentCLIWorkingDirPrefersIntentionalCdInsidePackageManagerScript(t *testing.T) {
	root := t.TempDir()
	currentCwd := filepath.Join(root, "nested")
	if err := os.Mkdir(currentCwd, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	t.Setenv("INIT_CWD", root)
	t.Setenv("PWD", currentCwd)
	t.Setenv("npm_package_json", filepath.Join(root, "package.json"))
	chdirForTest(t, currentCwd)

	got, err := currentCLIWorkingDir()
	if err != nil {
		t.Fatalf("currentCLIWorkingDir() error = %v", err)
	}
	if got != currentCwd {
		t.Fatalf("currentCLIWorkingDir() = %q, want intentional cwd %q", got, currentCwd)
	}
}

func TestApplyRunGlobalsPreservesSymlinkTraversalSemanticsInCwdOverride(t *testing.T) {
	parent := t.TempDir()
	base := filepath.Join(parent, "base")
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	targetRoot := filepath.Join(parent, "target")
	linkTarget := filepath.Join(targetRoot, "child")
	if err := os.MkdirAll(linkTarget, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	wantCwd := filepath.Join(targetRoot, "repo")
	if err := os.MkdirAll(wantCwd, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	linkPath := filepath.Join(base, "link")
	if err := os.Symlink(linkTarget, linkPath); err != nil {
		t.Skipf("Symlink() error = %v", err)
	}

	chdirForTest(t, base)

	overridePath := strings.Join([]string{"link", "..", "repo"}, string(filepath.Separator))
	restore, err := applyRunGlobals(GlobalFlags{Cwd: overridePath})
	if err != nil {
		t.Fatalf("applyRunGlobals() error = %v", err)
	}
	t.Cleanup(restore)

	got, err := currentCLIWorkingDir()
	if err != nil {
		t.Fatalf("currentCLIWorkingDir() error = %v", err)
	}
	if !sameCLIPath(got, wantCwd) {
		t.Fatalf("currentCLIWorkingDir() = %q, want actual cwd %q", got, wantCwd)
	}
}

func TestResolveCLIWorkingDirOverrideUsesCurrentCLIWorkingDirUnderWrapper(t *testing.T) {
	root := t.TempDir()
	initCwd := filepath.Join(root, "sub")
	if err := os.Mkdir(initCwd, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	t.Setenv("INIT_CWD", initCwd)
	t.Setenv("PWD", root)
	t.Setenv("npm_package_json", filepath.Join(root, "package.json"))

	if got := resolveCLIWorkingDirOverride(root, "."); got != initCwd {
		t.Fatalf("resolveCLIWorkingDirOverride(.) = %q, want %q", got, initCwd)
	}

	wantSibling := filepath.Join(root, "other")
	if got := resolveCLIWorkingDirOverride(root, "../other"); got != wantSibling {
		t.Fatalf("resolveCLIWorkingDirOverride(../other) = %q, want %q", got, wantSibling)
	}
}

func TestCurrentCLIWorkingDirPrefersExplicitOverrideOverInitCwd(t *testing.T) {
	initCwd := t.TempDir()
	overrideCwd := t.TempDir()
	t.Setenv("INIT_CWD", initCwd)
	t.Setenv("PWD", initCwd)
	chdirForTest(t, overrideCwd)

	prevOverride := setCLIWorkingDirOverride(overrideCwd)
	t.Cleanup(func() {
		setCLIWorkingDirOverride(prevOverride)
	})

	got, err := currentCLIWorkingDir()
	if err != nil {
		t.Fatalf("currentCLIWorkingDir() error = %v", err)
	}
	if !sameCLIPath(got, overrideCwd) {
		t.Fatalf("currentCLIWorkingDir() = %q, want %q", got, overrideCwd)
	}
}

func TestApplyRunGlobalsPreservesSymlinkedCwdOverride(t *testing.T) {
	parent := t.TempDir()
	realCwd := filepath.Join(parent, "real")
	if err := os.Mkdir(realCwd, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	linkCwd := filepath.Join(parent, "link")
	if err := os.Symlink(realCwd, linkCwd); err != nil {
		t.Skipf("Symlink() error = %v", err)
	}

	restore, err := applyRunGlobals(GlobalFlags{Cwd: linkCwd})
	if err != nil {
		t.Fatalf("applyRunGlobals() error = %v", err)
	}
	t.Cleanup(restore)

	got, err := currentCLIWorkingDir()
	if err != nil {
		t.Fatalf("currentCLIWorkingDir() error = %v", err)
	}
	if got != linkCwd {
		t.Fatalf("currentCLIWorkingDir() = %q, want override path %q", got, linkCwd)
	}
}
