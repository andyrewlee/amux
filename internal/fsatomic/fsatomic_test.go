package fsatomic

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "one" {
		t.Fatalf("content = %q, want one", got)
	}
	if err := WriteFile(path, []byte("two"), 0o644); err != nil {
		t.Fatalf("WriteFile overwrite error = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "two" {
		t.Fatalf("content = %q, want two", got)
	}

	// No temp files survive a successful write.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected only the target file, found %v", names)
	}
}

func TestWriteFileKeepsPrivatePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Fatalf("expected no group/other permissions, got %03o", mode)
	}
}

func TestWriteFileWindowsBackupShuffle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileForGOOS("windows", path, []byte("new"), 0o644); err != nil {
		t.Fatalf("windows-path WriteFile error = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "new" {
		t.Fatalf("content = %q, want new", got)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("expected backup removed after successful replace, err=%v", err)
	}
}
