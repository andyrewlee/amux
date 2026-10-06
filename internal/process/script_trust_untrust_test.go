package process

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// TestScriptTrust_UntrustRemovesGrant covers the happy path: a recorded
// approval is dropped, IsTrusted flips false, and a fresh instance agrees —
// the revoke is durably on disk, not just in-memory state.
func TestScriptTrust_UntrustRemovesGrant(t *testing.T) {
	dir := t.TempDir()
	trust := NewScriptTrust(dir)
	repo := t.TempDir()
	content := []byte(`{"setup-workspace":["touch marker"]}`)

	if err := trust.Trust(repo, content); err != nil {
		t.Fatalf("Trust() error = %v", err)
	}
	if err := trust.Untrust(repo); err != nil {
		t.Fatalf("Untrust() error = %v", err)
	}
	if trust.IsTrusted(repo, content) {
		t.Fatal("expected untrusted after Untrust()")
	}
	if NewScriptTrust(dir).IsTrusted(repo, content) {
		t.Fatal("revoke did not persist — a fresh instance still sees the grant")
	}
}

// TestScriptTrust_UntrustAbsentIsIdempotent: revoking a repo with no
// recorded grant succeeds and writes nothing.
func TestScriptTrust_UntrustAbsentIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	trust := NewScriptTrust(dir)

	if err := trust.Untrust(t.TempDir()); err != nil {
		t.Fatalf("Untrust(absent) error = %v", err)
	}
	if _, err := os.Stat(trustedScriptsPath(t, dir)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("revoking an absent grant must not create the registry, stat err = %v", err)
	}
}

// TestScriptTrust_UntrustKeepsOtherRepos: the delete is key-scoped — other
// repos' approvals survive a revoke.
func TestScriptTrust_UntrustKeepsOtherRepos(t *testing.T) {
	dir := t.TempDir()
	trust := NewScriptTrust(dir)
	repoA, repoB := t.TempDir(), t.TempDir()
	content := []byte(`{"setup-workspace":["init"]}`)

	if err := trust.Trust(repoA, content); err != nil {
		t.Fatal(err)
	}
	if err := trust.Trust(repoB, content); err != nil {
		t.Fatal(err)
	}
	if err := trust.Untrust(repoA); err != nil {
		t.Fatalf("Untrust() error = %v", err)
	}
	if trust.IsTrusted(repoA, content) {
		t.Fatal("revoked repo still trusted")
	}
	if !trust.IsTrusted(repoB, content) {
		t.Fatal("revoking one repo dropped another repo's grant")
	}
}

// TestScriptTrust_UntrustCorruptRefusesAndPreserves pins the fail-closed
// revoke contract: a corrupt registry cannot be inspected to know what a
// delete would drop, so Untrust refuses and preserves the bytes — the grant
// the file may carry stays in place.
func TestScriptTrust_UntrustCorruptRefusesAndPreserves(t *testing.T) {
	dir := t.TempDir()
	trust := NewScriptTrust(dir)
	repo := t.TempDir()
	garbage := []byte("{not json")

	if err := os.WriteFile(trustedScriptsPath(t, dir), garbage, 0o644); err != nil {
		t.Fatalf("seed corrupt registry: %v", err)
	}
	if err := trust.Untrust(repo); err == nil {
		t.Fatal("expected Untrust to refuse a corrupt registry")
	}
	after, err := os.ReadFile(trustedScriptsPath(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(garbage) {
		t.Fatalf("refused revoke must preserve bytes\nbefore: %s\nafter:  %s", garbage, after)
	}
}

// TestScriptTrust_UntrustReadErrorLeavesGrant covers the other fail-closed
// direction: an unreadable registry refuses the delete (error surfaces), the
// file is untouched, and the grant is still honored once reads recover.
func TestScriptTrust_UntrustReadErrorLeavesGrant(t *testing.T) {
	dir := t.TempDir()
	trust := NewScriptTrust(dir)
	repo := t.TempDir()
	content := []byte(`{"setup-workspace":["init"]}`)
	if err := trust.Trust(repo, content); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(trustedScriptsPath(t, dir))
	if err != nil {
		t.Fatal(err)
	}

	injected := &os.PathError{Op: "read", Path: "trusted-scripts.json", Err: syscall.EIO}
	trust.readFile = func(string) ([]byte, error) { return nil, injected }
	if err := trust.Untrust(repo); !errors.Is(err, injected) {
		t.Fatalf("Untrust() error = %v, want cause %v", err, injected)
	}
	after, err := os.ReadFile(trustedScriptsPath(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("failed revoke must not mutate the registry")
	}
	trust.readFile = nil
	if !trust.IsTrusted(repo, content) {
		t.Fatal("a failed revoke must leave the grant in place")
	}
}

// TestScriptRunner_UntrustRepoScriptsRegates covers the runner-level revoke
// end to end: trusted repo runs its setup, revoke flips it back to gated —
// the next run reports ErrScriptsNotTrusted and produces no side effect.
func TestScriptRunner_UntrustRepoScriptsRegates(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	marker := filepath.Join(wsRoot, "marker")
	writeWorkspaceConfig(t, repo, `{"setup-workspace": ["touch `+marker+`"]}`)

	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	ws := &data.Workspace{Repo: repo, Root: wsRoot}

	if err := runner.RunSetup(ws); err != nil {
		t.Fatalf("trusted RunSetup() error = %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("trusted setup did not run: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}

	if err := runner.UntrustRepoScripts(repo); err != nil {
		t.Fatalf("UntrustRepoScripts() error = %v", err)
	}
	if err := runner.RunSetup(ws); !errors.Is(err, ErrScriptsNotTrusted) {
		t.Fatalf("post-revoke RunSetup() error = %v, want ErrScriptsNotTrusted", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("revoked setup command executed: marker file was recreated")
	}
}
