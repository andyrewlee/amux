package git

import (
	"testing"
)

func TestBranchDiff_FileCount(t *testing.T) {
	diff := &BranchDiff{
		Files: []BranchFile{
			{Path: "file1.go"},
			{Path: "file2.go"},
			{Path: "file3.go"},
		},
	}
	if diff.FileCount() != 3 {
		t.Errorf("FileCount() = %d, want 3", diff.FileCount())
	}
}

func TestBranchDiff_Summary(t *testing.T) {
	tests := []struct {
		name string
		diff BranchDiff
		want string
	}{
		{
			name: "single file",
			diff: BranchDiff{
				Files:        []BranchFile{{Path: "file.go"}},
				TotalAdded:   10,
				TotalDeleted: 5,
			},
			want: "1 file changed, +10 -5",
		},
		{
			name: "multiple files",
			diff: BranchDiff{
				Files: []BranchFile{
					{Path: "file1.go"},
					{Path: "file2.go"},
				},
				TotalAdded:   100,
				TotalDeleted: 50,
			},
			want: "2 files changed, +100 -50",
		},
		{
			name: "no files",
			diff: BranchDiff{
				Files:        []BranchFile{},
				TotalAdded:   0,
				TotalDeleted: 0,
			},
			want: "0 files changed, +0 -0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.diff.Summary(); got != tt.want {
				t.Errorf("Summary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBranchFile_ChangeKinds(t *testing.T) {
	tests := []struct {
		kind ChangeKind
		want string
	}{
		{ChangeModified, "M"},
		{ChangeAdded, "A"},
		{ChangeDeleted, "D"},
		{ChangeRenamed, "R"},
		{ChangeCopied, "C"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			file := BranchFile{Kind: tt.kind}
			change := Change{Kind: file.Kind}
			if got := change.KindString(); got != tt.want {
				t.Errorf("KindString() = %v, want %v", got, tt.want)
			}
		})
	}
}
