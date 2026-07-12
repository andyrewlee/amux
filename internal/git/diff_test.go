package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDiff(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantEmpty   bool
		wantBinary  bool
		wantHunks   int
		wantAdded   int
		wantDeleted int
	}{
		{
			name:      "empty diff",
			content:   "",
			wantEmpty: true,
		},
		{
			name:       "binary file",
			content:    "Binary files a/image.png and b/image.png differ",
			wantBinary: true,
		},
		{
			name: "single hunk",
			content: `diff --git a/file.txt b/file.txt
index abc123..def456 100644
--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,4 @@
 line 1
+added line
 line 2
 line 3`,
			wantHunks:   1,
			wantAdded:   1,
			wantDeleted: 0,
		},
		{
			name: "multiple hunks",
			content: `diff --git a/file.txt b/file.txt
index abc123..def456 100644
--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,4 @@
 line 1
+added line
 line 2
@@ -10,3 +11,2 @@
 line 10
-removed line
 line 12`,
			wantHunks:   2,
			wantAdded:   1,
			wantDeleted: 1,
		},
		{
			name: "with deletions only",
			content: `diff --git a/file.txt b/file.txt
index abc123..def456 100644
--- a/file.txt
+++ b/file.txt
@@ -1,5 +1,3 @@
 line 1
-line 2
-line 3
 line 4
 line 5`,
			wantHunks:   1,
			wantAdded:   0,
			wantDeleted: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDiff("test.txt", tt.content)

			if result.Empty != tt.wantEmpty {
				t.Errorf("Empty = %v, want %v", result.Empty, tt.wantEmpty)
			}
			if result.Binary != tt.wantBinary {
				t.Errorf("Binary = %v, want %v", result.Binary, tt.wantBinary)
			}
			if len(result.Hunks) != tt.wantHunks {
				t.Errorf("Hunks count = %d, want %d", len(result.Hunks), tt.wantHunks)
			}
			if result.AddedLines() != tt.wantAdded {
				t.Errorf("AddedLines = %d, want %d", result.AddedLines(), tt.wantAdded)
			}
			if result.DeletedLines() != tt.wantDeleted {
				t.Errorf("DeletedLines = %d, want %d", result.DeletedLines(), tt.wantDeleted)
			}
		})
	}
}

func TestHunkParsing(t *testing.T) {
	content := `diff --git a/file.txt b/file.txt
index abc123..def456 100644
--- a/file.txt
+++ b/file.txt
@@ -10,5 +10,6 @@ function context
 line 10
+added
 line 11`

	result := parseDiff("test.txt", content)

	if len(result.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(result.Hunks))
	}

	hunk := result.Hunks[0]
	if hunk.OldStart != 10 {
		t.Errorf("OldStart = %d, want 10", hunk.OldStart)
	}
	if hunk.OldCount != 5 {
		t.Errorf("OldCount = %d, want 5", hunk.OldCount)
	}
	if hunk.NewStart != 10 {
		t.Errorf("NewStart = %d, want 10", hunk.NewStart)
	}
	if hunk.NewCount != 6 {
		t.Errorf("NewCount = %d, want 6", hunk.NewCount)
	}
	if !strings.Contains(hunk.Header, "function context") {
		t.Errorf("Header should contain context, got %q", hunk.Header)
	}
}

func TestDiffLineKinds(t *testing.T) {
	content := `diff --git a/file.txt b/file.txt
--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,3 @@
 context
+added
-deleted`

	result := parseDiff("test.txt", content)

	// Check that we have the right line kinds
	hasContext := false
	hasAdd := false
	hasDelete := false
	hasHeader := false

	for _, line := range result.Lines {
		switch line.Kind {
		case DiffLineContext:
			hasContext = true
		case DiffLineAdd:
			hasAdd = true
		case DiffLineDelete:
			hasDelete = true
		case DiffLineHeader:
			hasHeader = true
		}
	}

	if !hasContext {
		t.Error("expected context lines")
	}
	if !hasAdd {
		t.Error("expected add lines")
	}
	if !hasDelete {
		t.Error("expected delete lines")
	}
	if !hasHeader {
		t.Error("expected header lines")
	}
}

func TestGetUntrackedFileContent_TextFile(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)

	content := "alpha\nbeta\ngamma\n"
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte(content), 0o600); err != nil {
		t.Fatalf("write file.txt: %v", err)
	}

	result, err := GetUntrackedFileContent(repo, "file.txt")
	if err != nil {
		t.Fatalf("GetUntrackedFileContent() error = %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Error = %q, want empty", result.Error)
	}
	if result.Binary {
		t.Error("Binary = true, want false for a plain text file")
	}
	if result.Path != "file.txt" {
		t.Errorf("Path = %q, want %q", result.Path, "file.txt")
	}
	if result.Empty {
		t.Error("Empty = true, want false")
	}

	added := map[string]bool{}
	for _, line := range result.Lines {
		if line.Kind == DiffLineAdd {
			added[strings.TrimPrefix(line.Content, "+")] = true
		}
	}
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !added[want] {
			t.Errorf("Lines missing added line %q; added lines = %v", want, added)
		}
	}
	if got := result.AddedLines(); got != 3 {
		t.Errorf("AddedLines() = %d, want 3", got)
	}
	if got := result.DeletedLines(); got != 0 {
		t.Errorf("DeletedLines() = %d, want 0", got)
	}
}

func TestGetUntrackedFileContent_BinaryFile(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)

	// NUL bytes make git report "Binary files ... differ".
	binary := []byte{0x00, 0x01, 0x02, 0xff, 0x00, 'P', 'N', 'G'}
	if err := os.WriteFile(filepath.Join(repo, "blob.bin"), binary, 0o600); err != nil {
		t.Fatalf("write blob.bin: %v", err)
	}

	result, err := GetUntrackedFileContent(repo, "blob.bin")
	if err != nil {
		t.Fatalf("GetUntrackedFileContent() error = %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Error = %q, want empty", result.Error)
	}
	if !result.Binary {
		t.Fatal("Binary = false, want true for a NUL-containing file")
	}
	if result.Path != "blob.bin" {
		t.Errorf("Path = %q, want %q", result.Path, "blob.bin")
	}
	if len(result.Lines) != 0 {
		t.Errorf("Lines = %d entries, want none for binary short-circuit", len(result.Lines))
	}
}

func TestDiffResult_HunkCount(t *testing.T) {
	result := &DiffResult{
		Hunks: []Hunk{{}, {}, {}},
	}
	if result.HunkCount() != 3 {
		t.Errorf("HunkCount() = %d, want 3", result.HunkCount())
	}
}
