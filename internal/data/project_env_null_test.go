package data

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// TestProjectEnvStoreLegacyNull_SetRoundTrip proves a valid top-level `null`
// decodes to allocated empty state: the next Set assigns into it instead of
// panicking, persists the v1 envelope, and reloads the exact values.
func TestProjectEnvStoreLegacyNull_SetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectEnvStore(dir)
	repo := t.TempDir()

	if err := os.WriteFile(store.Path(), []byte(`null`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(repo, map[string]string{"A": "1", "B": "2"}); err != nil {
		t.Fatalf("Set() after legacy null error = %v", err)
	}
	if got := NewProjectEnvStore(dir).ForRepo(repo); got["A"] != "1" || got["B"] != "2" || len(got) != 2 {
		t.Fatalf("reloaded ForRepo() = %v, want exact values", got)
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"version"`) {
		t.Fatalf("Set() after null must write the v1 envelope, got %s", raw)
	}
}

// TestProjectEnvStoreLegacyNull_ReadReturnsEmpty is the direct load
// assertion: legacy null loads as a non-nil empty map and the read leaves
// the file's bytes untouched.
func TestProjectEnvStoreLegacyNull_ReadReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectEnvStore(dir)

	if err := os.WriteFile(store.Path(), []byte(`null`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := store.load()
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("load() = %v, want non-nil empty map", got)
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `null` {
		t.Fatalf("read must not rewrite the file, got %s", raw)
	}
}

// TestProjectEnvStoreLegacyNull_FixtureMatrix walks the decode fixtures:
// missing file, legacy null, legacy empty object, v1 envelope with a null
// map, v1 empty map, and populated v0/v1. Every read must leave the
// original bytes alone.
func TestProjectEnvStoreLegacyNull_FixtureMatrix(t *testing.T) {
	repo := t.TempDir()
	key := NormalizePath(repo)
	fixtures := []struct {
		name     string
		content  string
		exists   bool
		wantLen  int
		wantA    string
		wantFail bool
	}{
		{name: "missing", exists: false, wantLen: 0},
		{name: "legacy null", content: `null`, exists: true, wantLen: 0},
		{name: "legacy empty object", content: `{}`, exists: true, wantLen: 0},
		{name: "v1 null env", content: `{"version":1,"env":null}`, exists: true, wantLen: 0},
		{name: "v1 empty env", content: `{"version":1,"env":{}}`, exists: true, wantLen: 0},
		{name: "populated v0", content: `{"` + key + `":{"A":"1"}}`, exists: true, wantLen: 1, wantA: "1"},
		{name: "populated v1", content: `{"version":1,"env":{"` + key + `":{"A":"1"}}}`, exists: true, wantLen: 1, wantA: "1"},
		{name: "corrupt", content: `{not json`, exists: true, wantLen: 0, wantFail: true},
	}
	for _, tc := range fixtures {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			store := NewProjectEnvStore(dir)
			if tc.exists {
				if err := os.WriteFile(store.Path(), []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			all, err := store.load()
			if tc.wantFail {
				if err == nil {
					t.Fatalf("load() = %v, want corrupt error", all)
				}
				return
			}
			if err != nil {
				t.Fatalf("load() error = %v", err)
			}
			if all == nil || len(all) != tc.wantLen {
				t.Fatalf("load() = %v, want non-nil map len %d", all, tc.wantLen)
			}
			if tc.wantA != "" {
				if got := store.ForRepo(repo); got["A"] != tc.wantA {
					t.Fatalf("ForRepo() = %v, want A=%q", got, tc.wantA)
				}
			}
			if tc.exists {
				raw, err := os.ReadFile(store.Path())
				if err != nil {
					t.Fatal(err)
				}
				if string(raw) != tc.content {
					t.Fatalf("read rewrote fixture %q → %q", tc.content, raw)
				}
			} else if _, err := os.Stat(store.Path()); !os.IsNotExist(err) {
				t.Fatal("read must not create the file")
			}
		})
	}
}

// TestProjectEnvStoreLegacyNull_ClearAfterNull: an empty Set after legacy
// null removes nothing but still writes a valid v1 envelope (the
// authoritative-write path), and an independent repo entry survives a
// normal round trip alongside it.
func TestProjectEnvStoreLegacyNull_ClearAfterNull(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectEnvStore(dir)
	repoA, repoB := t.TempDir(), t.TempDir()

	if err := os.WriteFile(store.Path(), []byte(`null`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(repoA, map[string]string{}); err != nil {
		t.Fatalf("empty Set() after null error = %v", err)
	}
	if err := store.Set(repoB, map[string]string{"B": "2"}); err != nil {
		t.Fatalf("Set(repoB) error = %v", err)
	}
	reloaded := NewProjectEnvStore(dir)
	if got := reloaded.ForRepo(repoA); len(got) != 0 {
		t.Fatalf("ForRepo(repoA) = %v, want empty", got)
	}
	if got := reloaded.ForRepo(repoB); got["B"] != "2" {
		t.Fatalf("ForRepo(repoB) = %v, want B=2", got)
	}
}

// TestProjectEnvStoreWriteRefusesNewerSchema mirrors the trust registry's
// refusal: a newer-schema file is never clobbered, byte for byte.
func TestProjectEnvStoreWriteRefusesNewerSchema(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectEnvStore(dir)
	repo := t.TempDir()
	content := `{"version":99,"env":{}}`

	if err := os.WriteFile(store.Path(), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(repo, map[string]string{"A": "1"}); !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatalf("Set() error = %v, want ErrUnsupportedSchemaVersion", err)
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != content {
		t.Fatalf("refused write must preserve newer-schema bytes, got %s", raw)
	}
}
