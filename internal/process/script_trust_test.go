package process

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
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

func TestScriptTrustCreatesPrivateRegistryDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "amux-state")
	trust := NewScriptTrust(dir)
	repo := t.TempDir()
	content := []byte(`{"setup-workspace":["touch marker"]}`)

	if err := trust.Trust(repo, content); err != nil {
		t.Fatalf("Trust() error = %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", dir, err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Fatalf("expected trust registry dir to be private, got mode %03o", mode)
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

func TestScriptTrustSentinelIgnoresCWDRegistry(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)

	repo := t.TempDir()
	content := []byte(`{"setup-workspace":["touch marker"]}`)
	entries := map[string]string{
		data.NormalizePath(repo): hashConfig(content),
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cwd, trustRegistryFilename), raw, 0o600); err != nil {
		t.Fatalf("write hostile cwd registry: %v", err)
	}

	trust := &ScriptTrust{path: ""}
	if trust.IsTrusted(repo, content) {
		t.Fatal("sentinel trust must ignore matching registries in the current directory")
	}
}

func TestScriptTrustSentinelTrustErrorsAndWritesNothing(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)

	trust := &ScriptTrust{path: ""}
	if err := trust.Trust(t.TempDir(), []byte(`{"setup-workspace":["touch marker"]}`)); err == nil {
		t.Fatal("expected sentinel Trust() to return an error")
	}
	if _, err := os.Stat(filepath.Join(cwd, trustRegistryFilename)); !os.IsNotExist(err) {
		t.Fatalf("expected sentinel Trust() to write no registry, stat err = %v", err)
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
