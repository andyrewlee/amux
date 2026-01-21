package git

import (
	"testing"
)

func TestParseStatusPorcelain(t *testing.T) {
	tests := []struct {
		name          string
		input         []byte
		wantClean     bool
		wantStaged    int
		wantUnstaged  int
		wantUntracked int
		checkFirst    func(t *testing.T, result *StatusResult)
	}{
		{
			name:      "empty output",
			input:     []byte{},
			wantClean: true,
		},
		{
			name:          "single staged modified file",
			input:         []byte("M  file.txt\x00"),
			wantClean:     false,
			wantStaged:    1,
			wantUnstaged:  0,
			wantUntracked: 0,
			checkFirst: func(t *testing.T, result *StatusResult) {
				if len(result.Staged) != 1 {
					return
				}
				if result.Staged[0].Path != "file.txt" {
					t.Errorf("expected path 'file.txt', got %q", result.Staged[0].Path)
				}
				if result.Staged[0].Kind != ChangeModified {
					t.Errorf("expected ChangeModified, got %d", result.Staged[0].Kind)
				}
			},
		},
		{
			name:          "single unstaged modified file",
			input:         []byte(" M file.txt\x00"),
			wantClean:     false,
			wantStaged:    0,
			wantUnstaged:  1,
			wantUntracked: 0,
			checkFirst: func(t *testing.T, result *StatusResult) {
				if len(result.Unstaged) != 1 {
					return
				}
				if result.Unstaged[0].Path != "file.txt" {
					t.Errorf("expected path 'file.txt', got %q", result.Unstaged[0].Path)
				}
				if result.Unstaged[0].Kind != ChangeModified {
					t.Errorf("expected ChangeModified, got %d", result.Unstaged[0].Kind)
				}
			},
		},
		{
			name:          "both staged and unstaged",
			input:         []byte("MM file.txt\x00"),
			wantClean:     false,
			wantStaged:    1,
			wantUnstaged:  1,
			wantUntracked: 0,
		},
		{
			name:          "staged added file",
			input:         []byte("A  new.txt\x00"),
			wantClean:     false,
			wantStaged:    1,
			wantUnstaged:  0,
			wantUntracked: 0,
			checkFirst: func(t *testing.T, result *StatusResult) {
				if len(result.Staged) != 1 {
					return
				}
				if result.Staged[0].Kind != ChangeAdded {
					t.Errorf("expected ChangeAdded, got %d", result.Staged[0].Kind)
				}
			},
		},
		{
			name:          "untracked file",
			input:         []byte("?? untracked.txt\x00"),
			wantClean:     false,
			wantStaged:    0,
			wantUnstaged:  0,
			wantUntracked: 1,
			checkFirst: func(t *testing.T, result *StatusResult) {
				if len(result.Untracked) != 1 {
					return
				}
				if result.Untracked[0].Kind != ChangeUntracked {
					t.Errorf("expected ChangeUntracked, got %d", result.Untracked[0].Kind)
				}
			},
		},
		{
			name:          "deleted file",
			input:         []byte("D  deleted.txt\x00"),
			wantClean:     false,
			wantStaged:    1,
			wantUnstaged:  0,
			wantUntracked: 0,
			checkFirst: func(t *testing.T, result *StatusResult) {
				if len(result.Staged) != 1 {
					return
				}
				if result.Staged[0].Kind != ChangeDeleted {
					t.Errorf("expected ChangeDeleted, got %d", result.Staged[0].Kind)
				}
			},
		},
		{
			name:          "renamed file",
			input:         []byte("R  old.txt\x00new.txt\x00"),
			wantClean:     false,
			wantStaged:    1,
			wantUnstaged:  0,
			wantUntracked: 0,
			checkFirst: func(t *testing.T, result *StatusResult) {
				if len(result.Staged) != 1 {
					return
				}
				if result.Staged[0].Kind != ChangeRenamed {
					t.Errorf("expected ChangeRenamed, got %d", result.Staged[0].Kind)
				}
				if result.Staged[0].OldPath != "old.txt" {
					t.Errorf("expected OldPath 'old.txt', got %q", result.Staged[0].OldPath)
				}
				if result.Staged[0].Path != "new.txt" {
					t.Errorf("expected Path 'new.txt', got %q", result.Staged[0].Path)
				}
			},
		},
		{
			name:          "path with spaces",
			input:         []byte(" M file with spaces.txt\x00"),
			wantClean:     false,
			wantStaged:    0,
			wantUnstaged:  1,
			wantUntracked: 0,
			checkFirst: func(t *testing.T, result *StatusResult) {
				if len(result.Unstaged) != 1 {
					return
				}
				if result.Unstaged[0].Path != "file with spaces.txt" {
					t.Errorf("expected path with spaces, got %q", result.Unstaged[0].Path)
				}
			},
		},
		{
			name:          "multiple files mixed",
			input:         []byte("M  staged.txt\x00 M unstaged.txt\x00?? new.txt\x00"),
			wantClean:     false,
			wantStaged:    1,
			wantUnstaged:  1,
			wantUntracked: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseStatusPorcelain(tt.input)

			if result.Clean != tt.wantClean {
				t.Errorf("Clean = %v, want %v", result.Clean, tt.wantClean)
			}
			if len(result.Staged) != tt.wantStaged {
				t.Errorf("Staged count = %d, want %d", len(result.Staged), tt.wantStaged)
			}
			if len(result.Unstaged) != tt.wantUnstaged {
				t.Errorf("Unstaged count = %d, want %d", len(result.Unstaged), tt.wantUnstaged)
			}
			if len(result.Untracked) != tt.wantUntracked {
				t.Errorf("Untracked count = %d, want %d", len(result.Untracked), tt.wantUntracked)
			}

			if tt.checkFirst != nil {
				tt.checkFirst(t, result)
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
			name: "dirty with staged",
			status: StatusResult{
				Clean:  false,
				Staged: []Change{{}, {}, {}},
			},
			want: "+3 changes",
		},
		{
			name: "dirty with mixed",
			status: StatusResult{
				Clean:     false,
				Staged:    []Change{{}},
				Unstaged:  []Change{{}},
				Untracked: []Change{{}},
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

func TestChange_KindString(t *testing.T) {
	tests := []struct {
		kind ChangeKind
		want string
	}{
		{ChangeModified, "M"},
		{ChangeAdded, "A"},
		{ChangeDeleted, "D"},
		{ChangeRenamed, "R"},
		{ChangeCopied, "C"},
		{ChangeUntracked, "?"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			c := Change{Kind: tt.kind}
			if got := c.KindString(); got != tt.want {
				t.Errorf("KindString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChange_DisplayCode(t *testing.T) {
	tests := []struct {
		name   string
		change Change
		want   string
	}{
		{
			name:   "staged modified",
			change: Change{Kind: ChangeModified, Staged: true},
			want:   "M ",
		},
		{
			name:   "unstaged modified",
			change: Change{Kind: ChangeModified, Staged: false},
			want:   " M",
		},
		{
			name:   "untracked",
			change: Change{Kind: ChangeUntracked, Staged: false},
			want:   "??",
		},
		{
			name:   "staged added",
			change: Change{Kind: ChangeAdded, Staged: true},
			want:   "A ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.change.DisplayCode(); got != tt.want {
				t.Errorf("DisplayCode() = %v, want %v", got, tt.want)
			}
		})
	}
}
