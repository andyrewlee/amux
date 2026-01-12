package git

import (
	"testing"
)

func TestParseCommitLog(t *testing.T) {
	tests := []struct {
		name           string
		output         string
		wantCommits    int
		wantHashes     []string
		wantFullHashes []string
		wantSubjects   []string
		wantAuthors    []string
		wantRefs       []string
	}{
		{
			name:        "empty output",
			output:      "",
			wantCommits: 0,
		},
		{
			name:           "single commit",
			output:         "* abc1234|||abc1234aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa|||Fix bug|||John Doe|||2 hours ago|||HEAD -> main",
			wantCommits:    1,
			wantHashes:     []string{"abc1234"},
			wantFullHashes: []string{"abc1234aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			wantSubjects:   []string{"Fix bug"},
			wantAuthors:    []string{"John Doe"},
			wantRefs:       []string{"HEAD -> main"},
		},
		{
			name:           "multiple commits",
			output:         "* abc1234|||abc1234aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa|||First commit|||Alice|||1 day ago|||\n* def5678|||def5678bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb|||Second commit|||Bob|||2 days ago|||origin/main",
			wantCommits:    2,
			wantHashes:     []string{"abc1234", "def5678"},
			wantFullHashes: []string{"abc1234aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "def5678bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			wantSubjects:   []string{"First commit", "Second commit"},
			wantAuthors:    []string{"Alice", "Bob"},
			wantRefs:       []string{"", "origin/main"},
		},
		{
			name:           "commit with graph lines",
			output:         "* abc1234|||abc1234aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa|||Commit on main|||Dev|||now|||\n| * def5678|||def5678bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb|||Commit on branch|||Dev2|||1 hour ago|||feature",
			wantCommits:    2,
			wantHashes:     []string{"abc1234", "def5678"},
			wantFullHashes: []string{"abc1234aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "def5678bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			wantSubjects:   []string{"Commit on main", "Commit on branch"},
		},
		{
			name:           "graph-only line (merge visualization)",
			output:         "* abc1234|||abc1234aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa|||Merge|||Dev|||now|||\n|\\  \n| * def5678|||def5678bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb|||Feature|||Dev2|||1 hour ago|||",
			wantCommits:    3,                                  // Includes graph-only line
			wantHashes:     []string{"abc1234", "", "def5678"}, // Empty hash for graph-only line
			wantFullHashes: []string{"abc1234aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "", "def5678bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		},
		{
			name:        "refs with parentheses stripped",
			output:      "* abc1234|||abc1234aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa|||Test|||Dev|||now|||(HEAD -> main, origin/main)",
			wantCommits: 1,
			wantRefs:    []string{"HEAD -> main, origin/main"}, // Parentheses stripped
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseCommitLog(tt.output)

			if len(result.Commits) != tt.wantCommits {
				t.Errorf("Commits count = %d, want %d", len(result.Commits), tt.wantCommits)
				return
			}

			for i, hash := range tt.wantHashes {
				if i >= len(result.Commits) {
					break
				}
				if result.Commits[i].ShortHash != hash {
					t.Errorf("Commit[%d].ShortHash = %q, want %q", i, result.Commits[i].ShortHash, hash)
				}
			}

			for i, fullHash := range tt.wantFullHashes {
				if i >= len(result.Commits) {
					break
				}
				if result.Commits[i].FullHash != fullHash {
					t.Errorf("Commit[%d].FullHash = %q, want %q", i, result.Commits[i].FullHash, fullHash)
				}
			}

			for i, subject := range tt.wantSubjects {
				if i >= len(result.Commits) {
					break
				}
				if result.Commits[i].Subject != subject {
					t.Errorf("Commit[%d].Subject = %q, want %q", i, result.Commits[i].Subject, subject)
				}
			}

			for i, author := range tt.wantAuthors {
				if i >= len(result.Commits) {
					break
				}
				if result.Commits[i].Author != author {
					t.Errorf("Commit[%d].Author = %q, want %q", i, result.Commits[i].Author, author)
				}
			}

			for i, refs := range tt.wantRefs {
				if i >= len(result.Commits) {
					break
				}
				if result.Commits[i].Refs != refs {
					t.Errorf("Commit[%d].Refs = %q, want %q", i, result.Commits[i].Refs, refs)
				}
			}
		})
	}
}

func TestParseCommitLogGraphPrefix(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		wantGraph string
	}{
		{
			name:      "simple star",
			output:    "* abc1234|||abc1234aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa|||Test|||Dev|||now|||",
			wantGraph: "* ",
		},
		{
			name:      "branch with pipe",
			output:    "| * abc1234|||abc1234aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa|||Test|||Dev|||now|||",
			wantGraph: "| * ",
		},
		{
			name:      "merge with backslash",
			output:    "|\\ abc1234|||abc1234aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa|||Test|||Dev|||now|||",
			wantGraph: "|\\ ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseCommitLog(tt.output)
			if len(result.Commits) == 0 {
				t.Fatal("Expected at least one commit")
			}
			if result.Commits[0].GraphLine != tt.wantGraph {
				t.Errorf("GraphLine = %q, want %q", result.Commits[0].GraphLine, tt.wantGraph)
			}
		})
	}
}

func TestParseCommitLogEmptyLines(t *testing.T) {
	// Empty lines should be skipped
	output := "* abc1234|||abc1234aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa|||Test|||Dev|||now|||\n\n* def5678|||def5678bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb|||Test2|||Dev2|||1h ago|||"
	result := parseCommitLog(output)
	if len(result.Commits) != 2 {
		t.Errorf("Expected 2 commits, got %d", len(result.Commits))
	}
}

func TestParseCommitLogIncompleteFields(t *testing.T) {
	// Lines with fewer than 6 delimiter-separated fields should be skipped or treated as graph-only
	output := "* abc1234|||Test"
	result := parseCommitLog(output)
	// This should either be 0 commits or treated as graph-only (hash empty)
	if len(result.Commits) > 0 && (result.Commits[0].ShortHash != "" || result.Commits[0].FullHash != "") {
		t.Errorf("Expected incomplete line to be skipped or have empty hashes")
	}
}
