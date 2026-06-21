package process

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScriptTrustNotTrustedUntilApproved(t *testing.T) {
	trust := NewScriptTrust(t.TempDir())
	repo := t.TempDir()
	content := []byte(`{"setup-workspace":["touch marker"]}`)

	if trust.IsTrusted(repo, content) {
		t.Fatal("expected a never-trusted repo to report not trusted")
	}
}

func TestScriptTrustTrustThenTrusted(t *testing.T) {
	trust := NewScriptTrust(t.TempDir())
	repo := t.TempDir()
	content := []byte(`{"setup-workspace":["touch marker"]}`)

	if err := trust.Trust(repo, content); err != nil {
		t.Fatalf("Trust() error = %v", err)
	}
	if !trust.IsTrusted(repo, content) {
		t.Fatal("expected trusted after Trust() with identical content")
	}
}

func TestScriptTrustEmptyPathDoesNotWriteRegistry(t *testing.T) {
	dir := t.TempDir()
	trust := NewScriptTrust(dir)

	if err := trust.Trust("", []byte(`{"setup-workspace":["touch marker"]}`)); err != nil {
		t.Fatalf("Trust(empty) error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, trustRegistryFilename)); !os.IsNotExist(err) {
		t.Fatalf("expected no trust registry for empty path, stat err = %v", err)
	}
	if trust.IsTrusted("", []byte(`{"setup-workspace":["touch marker"]}`)) {
		t.Fatal("empty repo path must never be trusted")
	}
}

func TestScriptTrustInvalidatedWhenContentChanges(t *testing.T) {
	trust := NewScriptTrust(t.TempDir())
	repo := t.TempDir()
	original := []byte(`{"setup-workspace":["touch marker"]}`)

	if err := trust.Trust(repo, original); err != nil {
		t.Fatalf("Trust() error = %v", err)
	}
	changed := []byte(`{"setup-workspace":["rm -rf /"]}`)
	if trust.IsTrusted(repo, changed) {
		t.Fatal("expected trust to be invalidated once the config content changes")
	}
	// Original content still verifies as trusted.
	if !trust.IsTrusted(repo, original) {
		t.Fatal("expected original content to remain trusted")
	}
}

func TestScriptTrustCorruptRegistryFailsClosed(t *testing.T) {
	dir := t.TempDir()
	trust := NewScriptTrust(dir)
	repo := t.TempDir()
	content := []byte(`{"setup-workspace":["touch marker"]}`)

	// Write garbage to the registry file the trust struct reads from.
	if err := os.WriteFile(filepath.Join(dir, trustRegistryFilename), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed corrupt registry: %v", err)
	}

	// Must not panic and must fail closed.
	if trust.IsTrusted(repo, content) {
		t.Fatal("expected a corrupt registry to fail closed (not trusted)")
	}
}

func TestScriptTrustTwoReposIndependent(t *testing.T) {
	trust := NewScriptTrust(t.TempDir())
	repoA := t.TempDir()
	repoB := t.TempDir()
	contentA := []byte(`{"setup-workspace":["echo a"]}`)
	contentB := []byte(`{"setup-workspace":["echo b"]}`)

	if err := trust.Trust(repoA, contentA); err != nil {
		t.Fatalf("Trust(repoA) error = %v", err)
	}

	if !trust.IsTrusted(repoA, contentA) {
		t.Fatal("expected repoA to be trusted")
	}
	if trust.IsTrusted(repoB, contentB) {
		t.Fatal("expected repoB to remain untrusted (independent of repoA)")
	}

	// Trusting B must not disturb A.
	if err := trust.Trust(repoB, contentB); err != nil {
		t.Fatalf("Trust(repoB) error = %v", err)
	}
	if !trust.IsTrusted(repoA, contentA) || !trust.IsTrusted(repoB, contentB) {
		t.Fatal("expected both repos to be trusted independently")
	}
}
