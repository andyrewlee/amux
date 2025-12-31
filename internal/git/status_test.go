package git

import (
	"testing"
)

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name       string
		output     string
		wantClean  bool
		wantFiles  int
		wantCodes  []string
		wantPaths  []string
	}{
		{
			name:      "empty output",
			output:    "",
			wantClean: true,
			wantFiles: 0,
		},
		{
			name:      "single modified file",
			output:    " M file.txt",
			wantClean: false,
			wantFiles: 1,
			wantCodes: []string{" M"},
			wantPaths: []string{"file.txt"},
		},
		{
			name:      "multiple files",
			output:    " M file1.txt\n?? file2.txt\nA  file3.txt",
			wantClean: false,
			wantFiles: 3,
			wantCodes: []string{" M", "??", "A "},
			wantPaths: []string{"file1.txt", "file2.txt", "file3.txt"},
		},
		{
			name:      "staged and unstaged",
			output:    "MM both.txt",
			wantClean: false,
			wantFiles: 1,
			wantCodes: []string{"MM"},
			wantPaths: []string{"both.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseStatus(tt.output)

			if result.Clean != tt.wantClean {
				t.Errorf("Clean = %v, want %v", result.Clean, tt.wantClean)
			}
			if len(result.Files) != tt.wantFiles {
				t.Errorf("Files count = %d, want %d", len(result.Files), tt.wantFiles)
			}

			for i, code := range tt.wantCodes {
				if i >= len(result.Files) {
					break
				}
				if result.Files[i].Code != code {
					t.Errorf("Files[%d].Code = %v, want %v", i, result.Files[i].Code, code)
				}
			}

			for i, path := range tt.wantPaths {
				if i >= len(result.Files) {
					break
				}
				if result.Files[i].Path != path {
					t.Errorf("Files[%d].Path = %v, want %v", i, result.Files[i].Path, path)
				}
			}
		})
	}
}

func TestFileStatus_IsModified(t *testing.T) {
	tests := []struct {
		code string
		want bool
	}{
		{"M ", true},
		{" M", true},
		{"MM", true},
		{"A ", false},
		{"??", false},
		{"D ", false},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			f := FileStatus{Code: tt.code}
			if f.IsModified() != tt.want {
				t.Errorf("IsModified() = %v, want %v", f.IsModified(), tt.want)
			}
		})
	}
}

func TestFileStatus_IsAdded(t *testing.T) {
	tests := []struct {
		code string
		want bool
	}{
		{"A ", true},
		{" A", true},
		{"M ", false},
		{"??", false},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			f := FileStatus{Code: tt.code}
			if f.IsAdded() != tt.want {
				t.Errorf("IsAdded() = %v, want %v", f.IsAdded(), tt.want)
			}
		})
	}
}

func TestFileStatus_IsDeleted(t *testing.T) {
	tests := []struct {
		code string
		want bool
	}{
		{"D ", true},
		{" D", true},
		{"M ", false},
		{"??", false},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			f := FileStatus{Code: tt.code}
			if f.IsDeleted() != tt.want {
				t.Errorf("IsDeleted() = %v, want %v", f.IsDeleted(), tt.want)
			}
		})
	}
}

func TestFileStatus_IsUntracked(t *testing.T) {
	tests := []struct {
		code string
		want bool
	}{
		{"??", true},
		{"M ", false},
		{"A ", false},
		{" M", false},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			f := FileStatus{Code: tt.code}
			if f.IsUntracked() != tt.want {
				t.Errorf("IsUntracked() = %v, want %v", f.IsUntracked(), tt.want)
			}
		})
	}
}

func TestStatusResult_GetStatusSummary(t *testing.T) {
	tests := []struct {
		name   string
		status StatusResult
		want   string
	}{
		{
			name:   "clean",
			status: StatusResult{Clean: true},
			want:   "Clean",
		},
		{
			name: "dirty",
			status: StatusResult{
				Clean: false,
				Files: []FileStatus{{}, {}, {}},
			},
			want: "+3 changes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.GetStatusSummary(); got != tt.want {
				t.Errorf("GetStatusSummary() = %v, want %v", got, tt.want)
			}
		})
	}
}
