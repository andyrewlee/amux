package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRunMigrationsLegacyToNew(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()

	// Create legacy directories with content
	legacyWorkspaces := filepath.Join(tmpDir, "worktrees")
	legacyMetadata := filepath.Join(tmpDir, "worktrees-metadata")

	if err := os.MkdirAll(filepath.Join(legacyWorkspaces, "project1"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyWorkspaces, "project1", "test.txt"), []byte("test content"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(legacyMetadata, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyMetadata, "metadata.json"), []byte(`{"key":"value"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create paths pointing to new locations
	paths := &Paths{
		Home:           tmpDir,
		WorkspacesRoot: filepath.Join(tmpDir, "workspaces"),
		MetadataRoot:   filepath.Join(tmpDir, "workspaces-metadata"),
	}

	// Run migrations
	result := paths.RunMigrations()

	// Verify migrations occurred
	if !result.MigratedWorkspacesRoot {
		t.Error("Expected MigratedWorkspacesRoot to be true")
	}
	if !result.MigratedMetadataRoot {
		t.Error("Expected MigratedMetadataRoot to be true")
	}
	if result.Error != nil {
		t.Errorf("Expected no error, got: %v", result.Error)
	}
	if !result.HasMigrations() {
		t.Error("Expected HasMigrations() to return true")
	}

	// Verify new directories exist with correct content
	content, err := os.ReadFile(filepath.Join(paths.WorkspacesRoot, "project1", "test.txt"))
	if err != nil {
		t.Errorf("Failed to read migrated file: %v", err)
	}
	if string(content) != "test content" {
		t.Errorf("Expected 'test content', got '%s'", string(content))
	}

	content, err = os.ReadFile(filepath.Join(paths.MetadataRoot, "metadata.json"))
	if err != nil {
		t.Errorf("Failed to read migrated metadata: %v", err)
	}
	if string(content) != `{"key":"value"}` {
		t.Errorf("Expected '{\"key\":\"value\"}', got '%s'", string(content))
	}

	// Verify legacy directories still exist (never deleted)
	if _, err := os.Stat(legacyWorkspaces); os.IsNotExist(err) {
		t.Error("Legacy workspaces directory should not be deleted")
	}
	if _, err := os.Stat(legacyMetadata); os.IsNotExist(err) {
		t.Error("Legacy metadata directory should not be deleted")
	}
}

func TestRunMigrationsIdempotent(t *testing.T) {
	tmpDir := t.TempDir()

	// Create both legacy and new directories
	legacyWorkspaces := filepath.Join(tmpDir, "worktrees")
	newWorkspaces := filepath.Join(tmpDir, "workspaces")

	if err := os.MkdirAll(legacyWorkspaces, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyWorkspaces, "legacy.txt"), []byte("legacy"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create new directory with different content (should NOT be overwritten)
	if err := os.MkdirAll(newWorkspaces, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newWorkspaces, "new.txt"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}

	paths := &Paths{
		Home:           tmpDir,
		WorkspacesRoot: newWorkspaces,
		MetadataRoot:   filepath.Join(tmpDir, "workspaces-metadata"),
	}

	// Run migrations
	result := paths.RunMigrations()

	// Migration should be skipped (destination exists)
	if result.MigratedWorkspacesRoot {
		t.Error("Expected MigratedWorkspacesRoot to be false (destination exists)")
	}
	if result.Error != nil {
		t.Errorf("Expected no error, got: %v", result.Error)
	}

	// Verify new directory was NOT overwritten
	content, err := os.ReadFile(filepath.Join(newWorkspaces, "new.txt"))
	if err != nil {
		t.Errorf("Failed to read new file: %v", err)
	}
	if string(content) != "new" {
		t.Errorf("New content was overwritten! Expected 'new', got '%s'", string(content))
	}

	// Verify legacy file was NOT copied
	if _, err := os.Stat(filepath.Join(newWorkspaces, "legacy.txt")); !os.IsNotExist(err) {
		t.Error("Legacy file should NOT have been copied to existing destination")
	}
}

func TestRunMigrationsNoLegacy(t *testing.T) {
	tmpDir := t.TempDir()

	// No legacy directories exist
	paths := &Paths{
		Home:           tmpDir,
		WorkspacesRoot: filepath.Join(tmpDir, "workspaces"),
		MetadataRoot:   filepath.Join(tmpDir, "workspaces-metadata"),
	}

	result := paths.RunMigrations()

	// No migrations should occur
	if result.MigratedWorkspacesRoot {
		t.Error("Expected MigratedWorkspacesRoot to be false")
	}
	if result.MigratedMetadataRoot {
		t.Error("Expected MigratedMetadataRoot to be false")
	}
	if result.Error != nil {
		t.Errorf("Expected no error, got: %v", result.Error)
	}
	if result.HasMigrations() {
		t.Error("Expected HasMigrations() to return false")
	}
}

func TestCopyFilePreservesPermissions(t *testing.T) {
	tmpDir := t.TempDir()

	srcFile := filepath.Join(tmpDir, "src.sh")
	dstFile := filepath.Join(tmpDir, "dst.sh")

	// Create source file with executable permissions
	if err := os.WriteFile(srcFile, []byte("#!/bin/bash\necho hello"), 0755); err != nil {
		t.Fatal(err)
	}

	// Copy the file
	if err := copyFile(srcFile, dstFile); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	// Verify permissions
	srcInfo, err := os.Stat(srcFile)
	if err != nil {
		t.Fatal(err)
	}
	dstInfo, err := os.Stat(dstFile)
	if err != nil {
		t.Fatal(err)
	}

	if srcInfo.Mode() != dstInfo.Mode() {
		t.Errorf("Permissions not preserved: src=%v, dst=%v", srcInfo.Mode(), dstInfo.Mode())
	}

	// Verify content
	content, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "#!/bin/bash\necho hello" {
		t.Errorf("Content not preserved: %s", content)
	}
}

func TestCopyDirIfNeededSourceNotExists(t *testing.T) {
	tmpDir := t.TempDir()

	migrated, err := copyDirIfNeeded(
		filepath.Join(tmpDir, "nonexistent"),
		filepath.Join(tmpDir, "dst"),
	)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if migrated {
		t.Error("Expected migrated to be false for nonexistent source")
	}
}

func TestCopyDirIfNeededDestExists(t *testing.T) {
	tmpDir := t.TempDir()

	src := filepath.Join(tmpDir, "src")
	dst := filepath.Join(tmpDir, "dst")

	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}

	migrated, err := copyDirIfNeeded(src, dst)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if migrated {
		t.Error("Expected migrated to be false when destination exists")
	}
}

func TestCopyDirRecursive(t *testing.T) {
	tmpDir := t.TempDir()

	src := filepath.Join(tmpDir, "src")
	dst := filepath.Join(tmpDir, "dst")

	// Create nested directory structure
	nested := filepath.Join(src, "level1", "level2", "level3")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "deep.txt"), []byte("deep content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "root.txt"), []byte("root content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Copy
	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir failed: %v", err)
	}

	// Verify nested file
	content, err := os.ReadFile(filepath.Join(dst, "level1", "level2", "level3", "deep.txt"))
	if err != nil {
		t.Errorf("Failed to read nested file: %v", err)
	}
	if string(content) != "deep content" {
		t.Errorf("Expected 'deep content', got '%s'", string(content))
	}

	// Verify root file
	content, err = os.ReadFile(filepath.Join(dst, "root.txt"))
	if err != nil {
		t.Errorf("Failed to read root file: %v", err)
	}
	if string(content) != "root content" {
		t.Errorf("Expected 'root content', got '%s'", string(content))
	}
}

func TestCopyDirIfNeededCleansUpOnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-based test not reliable on Windows")
	}

	tmpDir := t.TempDir()

	src := filepath.Join(tmpDir, "src")
	dst := filepath.Join(tmpDir, "dst")

	if err := os.MkdirAll(filepath.Join(src, "blocked"), 0755); err != nil {
		t.Fatal(err)
	}

	blockedPath := filepath.Join(src, "blocked")
	if err := os.Chmod(blockedPath, 0000); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chmod(blockedPath, 0755)
	}()

	migrated, err := copyDirIfNeeded(src, dst)

	if err == nil {
		t.Fatal("Expected error copying directory with unreadable subdir")
	}
	if migrated {
		t.Error("Expected migrated to be false on copy error")
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Errorf("Expected destination to be removed on error, got stat err: %v", statErr)
	}
}

func TestCopyDirPreservesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated permissions on Windows")
	}

	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src")
	dst := filepath.Join(tmpDir, "dst")

	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(src, "target.txt"), []byte("target"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(src, "dir"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("target.txt", filepath.Join(src, "link.txt")); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("dir", filepath.Join(src, "linkdir")); err != nil {
		t.Fatal(err)
	}

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir failed: %v", err)
	}

	linkInfo, err := os.Lstat(filepath.Join(dst, "link.txt"))
	if err != nil {
		t.Fatalf("Lstat link.txt failed: %v", err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("Expected link.txt to be symlink, got mode %v", linkInfo.Mode())
	}
	linkTarget, err := os.Readlink(filepath.Join(dst, "link.txt"))
	if err != nil {
		t.Fatalf("Readlink link.txt failed: %v", err)
	}
	if linkTarget != "target.txt" {
		t.Fatalf("Expected link.txt target to be target.txt, got %s", linkTarget)
	}

	linkDirInfo, err := os.Lstat(filepath.Join(dst, "linkdir"))
	if err != nil {
		t.Fatalf("Lstat linkdir failed: %v", err)
	}
	if linkDirInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("Expected linkdir to be symlink, got mode %v", linkDirInfo.Mode())
	}
	linkDirTarget, err := os.Readlink(filepath.Join(dst, "linkdir"))
	if err != nil {
		t.Fatalf("Readlink linkdir failed: %v", err)
	}
	if linkDirTarget != "dir" {
		t.Fatalf("Expected linkdir target to be dir, got %s", linkDirTarget)
	}
}
