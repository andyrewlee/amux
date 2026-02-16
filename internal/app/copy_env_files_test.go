package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyEnvFiles(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Top-level .env files
	os.WriteFile(filepath.Join(src, ".env"), []byte("TOP=1"), 0o644)
	os.WriteFile(filepath.Join(src, ".env.local"), []byte("LOCAL=1"), 0o644)
	os.WriteFile(filepath.Join(src, ".env.production"), []byte("PROD=1"), 0o644)

	// Non-.env file at top level (should be ignored)
	os.WriteFile(filepath.Join(src, "README.md"), []byte("# hi"), 0o644)

	// Subdirectory with .env files
	os.MkdirAll(filepath.Join(src, "frontend"), 0o755)
	os.WriteFile(filepath.Join(src, "frontend", ".env"), []byte("FE=1"), 0o644)
	os.WriteFile(filepath.Join(src, "frontend", ".env.local"), []byte("FE_LOCAL=1"), 0o644)
	os.WriteFile(filepath.Join(src, "frontend", "package.json"), []byte("{}"), 0o644)

	// Subdirectory without .env files (should be skipped)
	os.MkdirAll(filepath.Join(src, "docs"), 0o755)
	os.WriteFile(filepath.Join(src, "docs", "README.md"), []byte("# docs"), 0o644)

	// Nested two levels deep (should NOT be copied)
	os.MkdirAll(filepath.Join(src, "services", "api"), 0o755)
	os.WriteFile(filepath.Join(src, "services", "api", ".env"), []byte("DEEP=1"), 0o644)
	os.WriteFile(filepath.Join(src, "services", ".env"), []byte("SVC=1"), 0o644)

	copyEnvFiles(src, dst)

	// Should exist
	for _, path := range []string{
		".env",
		".env.local",
		".env.production",
		filepath.Join("frontend", ".env"),
		filepath.Join("frontend", ".env.local"),
		filepath.Join("services", ".env"),
	} {
		full := filepath.Join(dst, path)
		if _, err := os.Stat(full); os.IsNotExist(err) {
			t.Errorf("expected %s to be copied, but it doesn't exist", path)
		}
	}

	// Should NOT exist
	for _, path := range []string{
		"README.md",
		filepath.Join("frontend", "package.json"),
		filepath.Join("docs", "README.md"),
		filepath.Join("services", "api", ".env"),
	} {
		full := filepath.Join(dst, path)
		if _, err := os.Stat(full); err == nil {
			t.Errorf("expected %s to NOT be copied, but it exists", path)
		}
	}

	// Verify content
	content, _ := os.ReadFile(filepath.Join(dst, ".env"))
	if string(content) != "TOP=1" {
		t.Errorf("expected .env content 'TOP=1', got %q", string(content))
	}
	content, _ = os.ReadFile(filepath.Join(dst, "frontend", ".env.local"))
	if string(content) != "FE_LOCAL=1" {
		t.Errorf("expected frontend/.env.local content 'FE_LOCAL=1', got %q", string(content))
	}
}
