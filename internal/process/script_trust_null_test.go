package process

import (
	"os"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// TestScriptTrustLegacyNull_TrustRoundTrip proves a valid top-level `null`
// grants nothing but decodes to allocated empty state: the next explicit
// Trust assigns into it instead of panicking, persists the v1 envelope, and
// a fresh instance approves exactly that content while rejecting changed
// content.
func TestScriptTrustLegacyNull_TrustRoundTrip(t *testing.T) {
	dir := t.TempDir()
	trust := NewScriptTrust(dir)
	repo := t.TempDir()
	content := []byte(`{"setup-workspace":["touch marker"]}`)

	if err := os.WriteFile(trustedScriptsPath(t, dir), []byte(`null`), 0o600); err != nil {
		t.Fatal(err)
	}
	if trust.IsTrusted(repo, content) {
		t.Fatal("null registry must not grant trust")
	}
	if err := trust.Trust(repo, content); err != nil {
		t.Fatalf("Trust() after legacy null error = %v", err)
	}
	reloaded := NewScriptTrust(dir)
	if !reloaded.IsTrusted(repo, content) {
		t.Fatal("Trust() after null must approve the exact content after reload")
	}
	if reloaded.IsTrusted(repo, []byte(`{"setup-workspace":["touch other"]}`)) {
		t.Fatal("changed content must remain untrusted")
	}
}

// TestScriptTrustLegacyNull_ReadReturnsEmpty is the direct load assertion:
// legacy null loads as a non-nil empty map and the read leaves the file's
// bytes untouched.
func TestScriptTrustLegacyNull_ReadReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	trust := NewScriptTrust(dir)

	if err := os.WriteFile(trustedScriptsPath(t, dir), []byte(`null`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := trust.load(); got == nil || len(got) != 0 {
		t.Fatalf("load() = %v, want non-nil empty map", got)
	}
	raw, err := os.ReadFile(trustedScriptsPath(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `null` {
		t.Fatalf("read must not rewrite the registry, got %s", raw)
	}
}

// TestScriptTrustLegacyNull_FixtureMatrix walks the decode fixtures:
// missing file, legacy null, legacy empty object, v1 envelope with a null
// map, v1 empty map, and populated v0/v1. Every read must leave the
// original bytes alone and only a correctly hashed approval satisfies
// IsTrusted.
func TestScriptTrustLegacyNull_FixtureMatrix(t *testing.T) {
	repo := t.TempDir()
	key := data.NormalizePath(repo)
	content := []byte(`{"setup-workspace":["touch marker"]}`)
	hash := hashConfig(content)
	fixtures := []struct {
		name        string
		content     string
		exists      bool
		wantLen     int
		wantTrusted bool
	}{
		{name: "missing", exists: false, wantLen: 0},
		{name: "legacy null", content: `null`, exists: true, wantLen: 0},
		{name: "legacy empty object", content: `{}`, exists: true, wantLen: 0},
		{name: "v1 null trusted", content: `{"version":1,"trusted":null}`, exists: true, wantLen: 0},
		{name: "v1 empty trusted", content: `{"version":1,"trusted":{}}`, exists: true, wantLen: 0},
		{name: "populated v0", content: `{"` + key + `":"` + hash + `"}`, exists: true, wantLen: 1, wantTrusted: true},
		{name: "populated v1", content: `{"version":1,"trusted":{"` + key + `":"` + hash + `"}}`, exists: true, wantLen: 1, wantTrusted: true},
		{name: "corrupt", content: `{not json`, exists: true, wantLen: 0},
	}
	for _, tc := range fixtures {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			trust := NewScriptTrust(dir)
			if tc.exists {
				if err := os.WriteFile(trustedScriptsPath(t, dir), []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if got := trust.load(); got == nil || len(got) != tc.wantLen {
				t.Fatalf("load() = %v, want non-nil map len %d", got, tc.wantLen)
			}
			if got := trust.IsTrusted(repo, content); got != tc.wantTrusted {
				t.Fatalf("IsTrusted() = %v, want %v", got, tc.wantTrusted)
			}
			if tc.exists {
				raw, err := os.ReadFile(trustedScriptsPath(t, dir))
				if err != nil {
					t.Fatal(err)
				}
				if string(raw) != tc.content {
					t.Fatalf("read rewrote fixture %q → %q", tc.content, raw)
				}
			} else if _, err := os.Stat(trustedScriptsPath(t, dir)); !os.IsNotExist(err) {
				t.Fatal("read must not create the registry")
			}
		})
	}
}
