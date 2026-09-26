package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// readConfigForUpdate is the strict read-modify-write boundary both config
// save paths share. These tests pin its contract with an injected reader so
// I/O failures are deterministic.

func TestReadConfigForUpdate(t *testing.T) {
	t.Run("missing file reads as empty object", func(t *testing.T) {
		payload, err := readConfigForUpdate("x", func(string) ([]byte, error) {
			return nil, &fs.PathError{Op: "open", Path: "x", Err: fs.ErrNotExist}
		})
		if err != nil {
			t.Fatalf("error = %v, want nil for ENOENT", err)
		}
		if payload == nil || len(payload) != 0 {
			t.Fatalf("payload = %#v, want empty non-nil map", payload)
		}
	})

	t.Run("permission error propagates with identity", func(t *testing.T) {
		payload, err := readConfigForUpdate("x", func(string) ([]byte, error) {
			return nil, &fs.PathError{Op: "open", Path: "x", Err: syscall.EACCES}
		})
		if payload != nil {
			t.Fatalf("payload = %#v, want nil on read failure", payload)
		}
		if err == nil || !errors.Is(err, syscall.EACCES) {
			t.Fatalf("error = %v, want wrapped EACCES", err)
		}
	})

	t.Run("other I/O error propagates with identity", func(t *testing.T) {
		boom := errors.New("disk gone")
		payload, err := readConfigForUpdate("x", func(string) ([]byte, error) {
			return nil, boom
		})
		if payload != nil {
			t.Fatalf("payload = %#v, want nil on read failure", payload)
		}
		if err == nil || !errors.Is(err, boom) {
			t.Fatalf("error = %v, want wrapped injected error", err)
		}
	})

	t.Run("empty and whitespace-only read as empty object", func(t *testing.T) {
		for _, content := range [][]byte{{}, []byte("  \n\t ")} {
			payload, err := readConfigForUpdate("x", func(string) ([]byte, error) {
				return content, nil
			})
			if err != nil {
				t.Fatalf("error = %v for %q, want nil", err, content)
			}
			if payload == nil || len(payload) != 0 {
				t.Fatalf("payload = %#v for %q, want empty non-nil map", payload, content)
			}
		}
	})

	t.Run("valid object is returned with unknown keys", func(t *testing.T) {
		payload, err := readConfigForUpdate("x", func(string) ([]byte, error) {
			return []byte(`{"ui": {"theme": "x"}, "custom": 1}`), nil
		})
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if payload["custom"] != float64(1) {
			t.Fatalf("payload lost unknown key: %#v", payload)
		}
	})

	t.Run("non-object roots are refused", func(t *testing.T) {
		for _, content := range [][]byte{
			[]byte("null"),
			[]byte("[1,2]"),
			[]byte(`"str"`),
			[]byte("5"),
			[]byte("{bad json"),
		} {
			payload, err := readConfigForUpdate("x", func(string) ([]byte, error) {
				return content, nil
			})
			if err == nil {
				t.Fatalf("error = nil for %q, want refusal", content)
			}
			if payload != nil {
				t.Fatalf("payload = %#v for %q, want nil on refusal", payload, content)
			}
		}
	})
}

// Both public save paths must leave an existing non-object or unreadable file
// byte-identical — partial overwrite is exactly the data loss this boundary
// exists to prevent.

func TestSavePathsRefuseTopLevelNull(t *testing.T) {
	for _, save := range map[string]func(string) error{
		"ui": func(path string) error {
			return saveUISettings(path, UISettings{Theme: "gruvbox"})
		},
		"assistants": func(path string) error {
			return saveAssistants(path, map[string]AssistantConfig{"claude": {Command: "claude"}})
		},
	} {
		path := filepath.Join(t.TempDir(), "config.json")
		original := []byte("null")
		if err := os.WriteFile(path, original, 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if err := save(path); err == nil {
			t.Fatal("save() error = nil for top-level null, want refusal (no panic)")
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}
		if string(got) != string(original) {
			t.Fatalf("config file modified despite refusal: got %q want %q", got, original)
		}
	}
}

func TestSavePathsRefuseUnreadableExistingFile(t *testing.T) {
	// A symlink escaping the open-root confinement makes readConfigPath fail
	// without chmod games (root can read anything, so chmod is unreliable).
	for _, save := range map[string]func(string) error{
		"ui": func(path string) error {
			return saveUISettings(path, UISettings{Theme: "gruvbox"})
		},
		"assistants": func(path string) error {
			return saveAssistants(path, map[string]AssistantConfig{"claude": {Command: "claude"}})
		},
	} {
		dir := t.TempDir()
		outside := filepath.Join(t.TempDir(), "outside.json")
		if err := os.WriteFile(outside, []byte(`{"keep": true}`), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		path := filepath.Join(dir, "config.json")
		if err := os.Symlink(outside, path); err != nil {
			t.Fatalf("Symlink() error = %v", err)
		}

		if err := save(path); err == nil {
			t.Fatal("save() error = nil for unreadable (escaping-symlink) config, want refusal")
		}

		// The symlink itself and its target must be untouched.
		link, err := os.Readlink(path)
		if err != nil {
			t.Fatalf("Readlink() error = %v — symlink was replaced", err)
		}
		if link != outside {
			t.Fatalf("symlink retargeted: %q, want %q", link, outside)
		}
		got, err := os.ReadFile(outside)
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}
		if string(got) != `{"keep": true}` {
			t.Fatalf("symlink target modified despite refusal: %q", got)
		}
	}
}

func TestSavePathsSucceedOnMissingAndBlankFiles(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing", "config.json")
	blank := filepath.Join(dir, "blank.json")
	if err := os.WriteFile(blank, []byte("  \n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	for _, path := range []string{missing, blank} {
		if err := saveUISettings(path, UISettings{Theme: "gruvbox"}); err != nil {
			t.Fatalf("saveUISettings(%q) error = %v, want nil for missing/blank file", path, err)
		}
		if err := saveAssistants(path, map[string]AssistantConfig{"claude": {Command: "claude"}}); err != nil {
			t.Fatalf("saveAssistants(%q) error = %v, want nil for missing/blank file", path, err)
		}
	}
}
