package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// trustedScriptsPath mirrors the store's backing filename for fixtures.
func trustedScriptsPath(t *testing.T, dir string) string {
	t.Helper()
	return filepath.Join(dir, "trusted-scripts.json")
}

func TestScriptTrust_V0FileReadsUnchanged(t *testing.T) {
	dir := t.TempDir()
	trust := NewScriptTrust(dir)
	repo := t.TempDir()
	content := []byte(`{"setup-workspace":["touch marker"]}`)

	// v0 fixture: the bare map shape keyed by normalized repo path.
	key := data.NormalizePath(repo)
	fixture := `{"` + key + `":"` + hashConfig(content) + `"}`
	if err := os.WriteFile(trustedScriptsPath(t, dir), []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if !trust.IsTrusted(repo, content) {
		t.Fatal("v0-format registry entry must still satisfy IsTrusted")
	}
}

func TestScriptTrust_V1RoundTrip(t *testing.T) {
	dir := t.TempDir()
	trust := NewScriptTrust(dir)
	repo := t.TempDir()
	content := []byte(`{"setup-workspace":["touch marker"]}`)

	if err := trust.Trust(repo, content); err != nil {
		t.Fatalf("Trust: %v", err)
	}
	raw, err := os.ReadFile(trustedScriptsPath(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	// v1 envelope: version + trusted wrapper.
	if got := string(raw); !strings.Contains(got, `"version"`) || !strings.Contains(got, `"trusted"`) {
		t.Fatalf("saved registry missing envelope: %s", got)
	}
	if !NewScriptTrust(dir).IsTrusted(repo, content) {
		t.Fatal("v1-format registry entry must satisfy IsTrusted after reload")
	}
}

func TestScriptTrust_UnknownVersionFailsClosed(t *testing.T) {
	dir := t.TempDir()
	trust := NewScriptTrust(dir)
	repo := t.TempDir()
	content := []byte(`{"setup-workspace":["touch marker"]}`)

	// A newer-format file must never be leniently parsed into trust.
	key := data.NormalizePath(repo)
	fixture := `{"version":99,"trusted":{"` + key + `":"` + hashConfig(content) + `"}}`
	if err := os.WriteFile(trustedScriptsPath(t, dir), []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if trust.IsTrusted(repo, content) {
		t.Fatal("IsTrusted must fail closed on an unknown schema version")
	}
}
