package update

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tarEntry describes a single record to write into a test tar.gz archive.
type tarEntry struct {
	name     string
	typeflag byte
	linkname string
	content  []byte
}

// writeTarGz builds a tar.gz archive at archivePath from the given entries.
// It lets tests construct adversarial archives (traversal names, absolute
// paths, symlinks) without any network access.
func writeTarGz(t *testing.T, archivePath string, entries []tarEntry) {
	t.Helper()

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("Failed to create archive file: %v", err)
	}
	defer f.Close()

	gzw := gzip.NewWriter(f)
	tw := tar.NewWriter(gzw)

	for _, e := range entries {
		typeflag := e.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     0o755,
			Typeflag: typeflag,
			Linkname: e.linkname,
		}
		if typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.content))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("Failed to write tar header for %q: %v", e.name, err)
		}
		if typeflag == tar.TypeReg {
			if _, err := tw.Write(e.content); err != nil {
				t.Fatalf("Failed to write tar content for %q: %v", e.name, err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("Failed to close tar writer: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("Failed to close gzip writer: %v", err)
	}
}

// TestExtractBinaryHostileArchives characterizes the traversal guard in
// ExtractBinary: filepath.Base(header.Name) collapses any directory component
// (including ".." and absolute paths) down to the basename, and the
// tar.TypeReg filter drops symlinks/dirs. These tests lock in that the
// extraction stays confined to destDir and never writes outside it.
func TestExtractBinaryHostileArchives(t *testing.T) {
	binaryContent := []byte("#!/bin/sh\necho hello\n")

	tests := []struct {
		name          string
		entries       []tarEntry
		wantExtracted bool
		wantErrSubstr string
	}{
		{
			name:          "relative traversal entry is confined to destDir",
			entries:       []tarEntry{{name: "../../../tmp/amux", content: binaryContent}},
			wantExtracted: true,
		},
		{
			name:          "absolute path entry is confined to destDir",
			entries:       []tarEntry{{name: "/tmp/amux", content: binaryContent}},
			wantExtracted: true,
		},
		{
			name:          "symlink entry fails the TypeReg check",
			entries:       []tarEntry{{name: "amux", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"}},
			wantErrSubstr: "amux binary not found in archive",
		},
		{
			name:          "normal nested entry extracts successfully",
			entries:       []tarEntry{{name: "amux_1.0.0_darwin_arm64/amux", content: binaryContent}},
			wantExtracted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			// Nest destDir several levels below tmpDir so that the locations
			// a naive raw-join extractor (one doing filepath.Join(destDir,
			// header.Name) without filepath.Base) would actually write to
			// resolve to real, statable paths under tmpDir. ExtractBinary
			// must NOT write to either of them.
			destDir := filepath.Join(tmpDir, "a", "b", "c", "extracted")
			if err := os.MkdirAll(destDir, 0o755); err != nil {
				t.Fatalf("Failed to create dest dir: %v", err)
			}

			// Relative-traversal target: filepath.Join(destDir,
			// "../../../tmp/amux") climbs three levels above destDir and lands
			// at tmpDir/a/tmp/amux — outside destDir but inside tmpDir.
			traversalTarget := filepath.Clean(filepath.Join(destDir, "../../../tmp/amux"))
			// Absolute-path target: filepath.Join(destDir, "/tmp/amux") drops
			// the leading slash and yields destDir/tmp/amux — outside the
			// "amux binary at destDir/amux" invariant.
			absJoinTarget := filepath.Join(destDir, "tmp", "amux")

			archivePath := filepath.Join(tmpDir, "test.tar.gz")
			writeTarGz(t, archivePath, tt.entries)

			extractedPath, err := ExtractBinary(archivePath, destDir)

			if tt.wantErrSubstr != "" {
				if err == nil {
					t.Fatalf("ExtractBinary() expected error containing %q, got nil", tt.wantErrSubstr)
				}
				if !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Fatalf("ExtractBinary() error = %v, want substring %q", err, tt.wantErrSubstr)
				}
				// No file should have been written into destDir for a
				// rejected entry.
				if _, statErr := os.Stat(filepath.Join(destDir, "amux")); !os.IsNotExist(statErr) {
					t.Errorf("symlink entry should not produce a file in destDir, stat err = %v", statErr)
				}
			}

			if tt.wantExtracted {
				if err != nil {
					t.Fatalf("ExtractBinary() error = %v", err)
				}
				wantPath := filepath.Join(destDir, "amux")
				if extractedPath != wantPath {
					t.Errorf("ExtractBinary() path = %q, want %q (must stay inside destDir)", extractedPath, wantPath)
				}
				content, readErr := os.ReadFile(extractedPath)
				if readErr != nil {
					t.Fatalf("Failed to read extracted file: %v", readErr)
				}
				if string(content) != string(binaryContent) {
					t.Errorf("Extracted content mismatch")
				}
			}

			// In every case, nothing must land at the locations a raw-join
			// extractor would have hit: the resolved dot-dot target above
			// destDir, or destDir/tmp/amux for the absolute-path case.
			if _, statErr := os.Stat(traversalTarget); !os.IsNotExist(statErr) {
				t.Errorf("file escaped destDir to %q, stat err = %v", traversalTarget, statErr)
			}
			if _, statErr := os.Stat(absJoinTarget); !os.IsNotExist(statErr) {
				t.Errorf("file escaped destDir to %q, stat err = %v", absJoinTarget, statErr)
			}
		})
	}
}
