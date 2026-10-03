package process

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// TestScriptTrust_ReadErrorPreservesState: a read failure that is not a
// missing file must refuse Trust — the registry may hold other repos'
// approvals that a blind empty-map write would erase. Covers
// permission/EIO/arbitrary errors, byte preservation, and fail-closed
// lookups.
func TestScriptTrust_ReadErrorPreservesState(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"permission", &os.PathError{Op: "read", Path: "trusted-scripts.json", Err: fs.ErrPermission}},
		{"EIO", &os.PathError{Op: "read", Path: "trusted-scripts.json", Err: syscall.EIO}},
		{"arbitrary", errors.New("injected reader failure")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			trust := NewScriptTrust(dir)
			repoA, repoB, repoC := t.TempDir(), t.TempDir(), t.TempDir()
			contentA := []byte(`{"setup-workspace":["init a"]}`)
			contentB := []byte(`{"setup-workspace":["init b"]}`)
			if err := trust.Trust(repoA, contentA); err != nil {
				t.Fatal(err)
			}
			if err := trust.Trust(repoB, contentB); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(trustedScriptsPath(t, dir))
			if err != nil {
				t.Fatal(err)
			}

			called := new(bool)
			trust.readFile = func(string) ([]byte, error) {
				*called = true
				return nil, tc.err
			}

			if err := trust.Trust(repoC, []byte(`{"setup-workspace":["init c"]}`)); !errors.Is(err, tc.err) {
				t.Fatalf("Trust() error = %v, want cause %v", err, tc.err)
			}
			if !*called {
				t.Fatal("injected reader was not invoked")
			}
			// Lookups stay fail-closed and must not mutate the file.
			if trust.IsTrusted(repoA, contentA) {
				t.Fatal("IsTrusted must fail closed under a read error")
			}
			after, err := os.ReadFile(trustedScriptsPath(t, dir))
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("read failure must preserve bytes\nbefore: %s\nafter:  %s", before, after)
			}
			trust.readFile = nil
			if !NewScriptTrust(dir).IsTrusted(repoA, contentA) || !NewScriptTrust(dir).IsTrusted(repoB, contentB) {
				t.Fatal("existing approvals must survive the refused write")
			}
		})
	}
}

// TestScriptTrust_StrictReadPolicy pins the write-path policy matrix:
// missing state initializes, corrupt JSON is still replaced by an explicit
// approval, a newer schema is refused byte-for-byte (including a malformed
// future payload), and unrelated approvals survive a new write.
func TestScriptTrust_StrictReadPolicy(t *testing.T) {
	content := []byte(`{"setup-workspace":["touch marker"]}`)
	repo := t.TempDir()

	t.Run("missing initializes", func(t *testing.T) {
		dir := t.TempDir()
		trust := NewScriptTrust(dir)
		if err := trust.Trust(repo, content); err != nil {
			t.Fatalf("Trust() on missing registry error = %v", err)
		}
		if !NewScriptTrust(dir).IsTrusted(repo, content) {
			t.Fatal("approval must be visible after reload")
		}
	})

	t.Run("corrupt syntax replaced", func(t *testing.T) {
		dir := t.TempDir()
		trust := NewScriptTrust(dir)
		if err := os.WriteFile(trustedScriptsPath(t, dir), []byte(`{not json`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := trust.Trust(repo, content); err != nil {
			t.Fatalf("Trust() on corrupt registry error = %v", err)
		}
		if !NewScriptTrust(dir).IsTrusted(repo, content) {
			t.Fatal("corrupt JSON must be replaced by the explicit approval")
		}
	})

	t.Run("corrupt type replaced", func(t *testing.T) {
		dir := t.TempDir()
		trust := NewScriptTrust(dir)
		if err := os.WriteFile(trustedScriptsPath(t, dir), []byte(`{"version":1,"trusted":123}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := trust.Trust(repo, content); err != nil {
			t.Fatalf("Trust() on ill-typed registry error = %v", err)
		}
		if !NewScriptTrust(dir).IsTrusted(repo, content) {
			t.Fatal("ill-typed JSON must be replaced by the explicit approval")
		}
	})

	t.Run("future schema refused", func(t *testing.T) {
		dir := t.TempDir()
		trust := NewScriptTrust(dir)
		fixture := `{"version":99,"trusted":{}}`
		if err := os.WriteFile(trustedScriptsPath(t, dir), []byte(fixture), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := trust.Trust(repo, content); !errors.Is(err, data.ErrUnsupportedSchemaVersion) {
			t.Fatalf("Trust() error = %v, want ErrUnsupportedSchemaVersion", err)
		}
		raw, err := os.ReadFile(trustedScriptsPath(t, dir))
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != fixture {
			t.Fatalf("refused write must preserve newer-schema bytes, got %s", raw)
		}
	})

	t.Run("malformed future payload refused", func(t *testing.T) {
		dir := t.TempDir()
		trust := NewScriptTrust(dir)
		// Version precedence: the future version integer must be rejected
		// before the ill-typed `trusted` field can make it look "corrupt".
		fixture := `{"version":99,"trusted":"not-a-map"}`
		if err := os.WriteFile(trustedScriptsPath(t, dir), []byte(fixture), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := trust.Trust(repo, content); !errors.Is(err, data.ErrUnsupportedSchemaVersion) {
			t.Fatalf("Trust() error = %v, want ErrUnsupportedSchemaVersion", err)
		}
		raw, err := os.ReadFile(trustedScriptsPath(t, dir))
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != fixture {
			t.Fatalf("refused write must preserve malformed future bytes, got %s", raw)
		}
	})

	t.Run("unrelated approvals survive", func(t *testing.T) {
		dir := t.TempDir()
		trust := NewScriptTrust(dir)
		other := t.TempDir()
		otherContent := []byte(`{"setup-workspace":["other init"]}`)
		if err := trust.Trust(other, otherContent); err != nil {
			t.Fatal(err)
		}
		if err := trust.Trust(repo, content); err != nil {
			t.Fatal(err)
		}
		reloaded := NewScriptTrust(dir)
		if !reloaded.IsTrusted(other, otherContent) {
			t.Fatal("unrelated approval must survive a new Trust write")
		}
		if !reloaded.IsTrusted(repo, content) {
			t.Fatal("new approval must be recorded")
		}
	})
}
