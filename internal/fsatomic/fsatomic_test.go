package fsatomic

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteJSONMarshalsIndentedAndReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	if err := WriteJSON(path, payload{Name: "amux", Count: 2}); err != nil {
		t.Fatalf("WriteJSON error = %v", err)
	}

	// Byte-for-byte parity with json.MarshalIndent(v, "", "  ") and no trailing
	// newline, so existing on-disk files are unchanged.
	want := "{\n  \"name\": \"amux\",\n  \"count\": 2\n}"
	if got, _ := os.ReadFile(path); string(got) != want {
		t.Fatalf("content = %q, want %q", got, want)
	}

	// Overwrite leaves no temp files behind (atomic replace).
	if err := WriteJSON(path, payload{Name: "x", Count: 9}); err != nil {
		t.Fatalf("WriteJSON overwrite error = %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected only the target file, found %d entries", len(entries))
	}
}

func TestWriteJSONReturnsMarshalError(t *testing.T) {
	// A channel cannot be marshaled; WriteJSON must surface the error and never
	// create the target file.
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := WriteJSON(path, make(chan int)); err == nil {
		t.Fatal("expected marshal error, got nil")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target should not exist after marshal failure, stat err = %v", err)
	}
}

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

// TestWriteFileUnixRenameFailureLeavesTargetUntouched drives the Unix rename
// failure: WriteFile must surface the error, leave the existing target intact,
// and clean up the temp file it created.
func TestWriteFileUnixRenameFailureLeavesTargetUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { renameFile = os.Rename })
	sentinel := errors.New("rename failed")
	renameFile = func(_, _ string) error { return sentinel }

	err := writeFileForGOOS("linux", path, []byte("new"), 0o644)
	if !errors.Is(err, sentinel) {
		t.Fatalf("WriteFile error = %v, want %v", err, sentinel)
	}

	// The original target survives untouched.
	if got, _ := os.ReadFile(path); string(got) != "old" {
		t.Fatalf("content = %q, want old (target must be untouched)", got)
	}

	// Only the original target remains; the temp file was cleaned up.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected only state.json to remain, found %v", names)
	}
}

// TestWriteFileWindowsRestoresBackupOnFinalRenameFailure drives the Windows
// shuffle's restore branch: the existing file is moved to .bak, the final
// rename of the temp into place fails, and the backup must be restored to the
// primary path so the previous contents survive.
func TestWriteFileWindowsRestoresBackupOnFinalRenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { renameFile = os.Rename })
	sentinel := errors.New("final rename failed")
	// Let the path->backup shuffle and the backup->path restore use the real
	// rename; only the temp->path move fails. tmp paths carry the ".tmp-"
	// marker that the real target/backup paths never do.
	realRename := os.Rename
	renameFile = func(oldPath, newPath string) error {
		if filepath.Base(newPath) == filepath.Base(path) &&
			containsTmpMarker(filepath.Base(oldPath)) {
			return sentinel
		}
		return realRename(oldPath, newPath)
	}

	err := writeFileForGOOS("windows", path, []byte("new"), 0o644)
	if !errors.Is(err, sentinel) {
		t.Fatalf("WriteFile error = %v, want %v", err, sentinel)
	}

	// The backup was restored to the primary path with the old contents.
	if got, _ := os.ReadFile(path); string(got) != "old" {
		t.Fatalf("content = %q, want old (backup must be restored)", got)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("expected backup consumed by restore, err=%v", err)
	}
}

func containsTmpMarker(name string) bool {
	for i := 0; i+5 <= len(name); i++ {
		if name[i:i+5] == ".tmp-" {
			return true
		}
	}
	return false
}
