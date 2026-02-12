package git

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const diffNumstatTimeout = 10 * time.Second

// DiffMode specifies which changes to diff
type DiffMode int

const (
	DiffModeUnstaged DiffMode = iota // Working tree changes (not staged)
	DiffModeStaged                   // Index changes (staged)
	DiffModeBoth                     // Both staged and unstaged
	DiffModeBranch                   // Branch diff vs base
)

// ChangeKind represents the type of change
type ChangeKind int

const (
	ChangeModified  ChangeKind = iota // File content changed
	ChangeAdded                       // New file
	ChangeDeleted                     // File removed
	ChangeRenamed                     // File renamed
	ChangeCopied                      // File copied
	ChangeUntracked                   // Untracked file
)

// Change represents a single file change in git status
type Change struct {
	Path    string     // Current file path
	OldPath string     // Original path (for renames/copies)
	Kind    ChangeKind // Type of change
	Staged  bool       // Whether this change is staged
}

// StatusResult holds the parsed git status grouped by category
type StatusResult struct {
	Staged    []Change // Changes staged for commit
	Unstaged  []Change // Changes in working tree (not staged)
	Untracked []Change // Untracked files
	Clean     bool     // True if no changes

	// Aggregate line-level diff stats
	TotalAdded   int // Total lines added across all changes
	TotalDeleted int // Total lines deleted across all changes
}

// GetStatus returns the git status for a repository using porcelain v1 -z format
// This format handles spaces, unicode, and special characters in paths correctly
func GetStatus(repoPath string) (*StatusResult, error) {
	output, err := RunGitRawCtx(context.Background(), repoPath, "--no-optional-locks", "status", "--porcelain=v1", "-z", "-u")
	if err != nil {
		return nil, err
	}

	result := parseStatusPorcelain(output)

	// Populate aggregate line stats from git diff --numstat
	if !result.Clean {
		unstagedAdd, unstagedDel, _ := getDiffNumstat(repoPath, false)
		stagedAdd, stagedDel, _ := getDiffNumstat(repoPath, true)
		untrackedAdd := countUntrackedLines(repoPath, result.Untracked)
		result.TotalAdded = unstagedAdd + stagedAdd + untrackedAdd
		result.TotalDeleted = unstagedDel + stagedDel
	}

	return result, nil
}

// getDiffNumstat runs git diff --numstat and returns total added/deleted lines.
func getDiffNumstat(repoPath string, staged bool) (added, deleted int, err error) {
	args := []string{"--no-optional-locks", "diff", "--numstat"}
	if staged {
		args = []string{"--no-optional-locks", "diff", "--cached", "--numstat"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), diffNumstatTimeout)
	defer cancel()
	output, err := RunGitCtx(ctx, repoPath, args...)
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 || parts[0] == "-" {
			continue // skip binary files
		}
		a, _ := strconv.Atoi(parts[0])
		d, _ := strconv.Atoi(parts[1])
		added += a
		deleted += d
	}
	return added, deleted, nil
}

// countUntrackedLines counts the total number of lines across untracked files.
// Binary files (null byte in first 8KB) and files larger than 1MB are skipped.
func countUntrackedLines(repoPath string, untracked []Change) int {
	const maxSize = 1 << 20 // 1MB
	total := 0
	for _, c := range untracked {
		p := filepath.Join(repoPath, c.Path)
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxSize {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		// Read first 8KB to check for binary content
		head := make([]byte, 8192)
		n, _ := f.Read(head)
		if n == 0 {
			f.Close()
			continue
		}
		if bytes.ContainsRune(head[:n], 0) {
			f.Close()
			continue
		}
		// Count newlines in head
		lines := bytes.Count(head[:n], []byte{'\n'})
		lastByte := head[n-1]
		// Count remaining newlines
		buf := make([]byte, 32*1024)
		for {
			nr, err := f.Read(buf)
			if nr > 0 {
				lines += bytes.Count(buf[:nr], []byte{'\n'})
				lastByte = buf[nr-1]
			}
			if err != nil {
				break
			}
		}
		f.Close()
		// Count final line if file is non-empty and lacks trailing newline
		if n > 0 && lastByte != '\n' {
			lines++
		}
		total += lines
	}
	return total
}

// parseStatusPorcelain parses git status --porcelain=v1 -z output
// Format: XY PATH\0 or XY NEWPATH\0OLDPATH\0 (for renames/copies)
// Where X is index status, Y is work tree status
func parseStatusPorcelain(output []byte) *StatusResult {
	result := &StatusResult{
		Staged:    []Change{},
		Unstaged:  []Change{},
		Untracked: []Change{},
		Clean:     true,
	}

	if len(output) == 0 {
		return result
	}

	// Split on NUL bytes
	entries := bytes.Split(output, []byte{0})

	i := 0
	for i < len(entries) {
		entry := entries[i]
		if len(entry) < 3 {
			i++
			continue
		}

		result.Clean = false

		// First two bytes are status codes
		indexStatus := entry[0]
		workTreeStatus := entry[1]
		// Third byte should be space
		path := string(entry[3:])

		// Handle renames and copies which have two paths
		var oldPath string
		if indexStatus == 'R' || indexStatus == 'C' {
			// In porcelain -z, the entry path is the new name,
			// and the next NUL-delimited token is the old name.
			i++
			if i < len(entries) {
				oldPath = string(entries[i])
			}
		}

		// Process staged changes (index status)
		if indexStatus != ' ' && indexStatus != '?' {
			change := Change{
				Path:    path,
				OldPath: oldPath,
				Kind:    statusCodeToKind(indexStatus),
				Staged:  true,
			}
			result.Staged = append(result.Staged, change)
		}

		// Process unstaged changes (work tree status)
		if workTreeStatus != ' ' && workTreeStatus != '?' {
			change := Change{
				Path:    path,
				OldPath: "", // Unstaged changes don't have renames
				Kind:    statusCodeToKind(workTreeStatus),
				Staged:  false,
			}
			result.Unstaged = append(result.Unstaged, change)
		}

		// Process untracked files
		if indexStatus == '?' && workTreeStatus == '?' {
			change := Change{
				Path:   path,
				Kind:   ChangeUntracked,
				Staged: false,
			}
			result.Untracked = append(result.Untracked, change)
		}

		i++
	}

	// Sort each group lexicographically
	sortChanges(result.Staged)
	sortChanges(result.Unstaged)
	sortChanges(result.Untracked)

	return result
}

// statusCodeToKind converts a git status code to ChangeKind
func statusCodeToKind(code byte) ChangeKind {
	switch code {
	case 'M':
		return ChangeModified
	case 'A':
		return ChangeAdded
	case 'D':
		return ChangeDeleted
	case 'R':
		return ChangeRenamed
	case 'C':
		return ChangeCopied
	default:
		return ChangeModified
	}
}

// sortChanges sorts changes by path
func sortChanges(changes []Change) {
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Path < changes[j].Path
	})
}

// GetDirtyCount returns the number of unique changed files
func (s *StatusResult) GetDirtyCount() int {
	seen := make(map[string]struct{})
	for _, c := range s.Staged {
		seen[c.Path] = struct{}{}
	}
	for _, c := range s.Unstaged {
		seen[c.Path] = struct{}{}
	}
	for _, c := range s.Untracked {
		seen[c.Path] = struct{}{}
	}
	return len(seen)
}

// GetStatusSummary returns a summary string for the status
func (s *StatusResult) GetStatusSummary() string {
	if s.Clean {
		return "Clean"
	}
	return "+" + strconv.Itoa(s.GetDirtyCount()) + " changes"
}

// AllChanges returns all changes as a flat list for backwards compatibility
func (s *StatusResult) AllChanges() []Change {
	all := make([]Change, 0, len(s.Staged)+len(s.Unstaged)+len(s.Untracked))
	all = append(all, s.Staged...)
	all = append(all, s.Unstaged...)
	all = append(all, s.Untracked...)
	return all
}

// KindString returns a display string for the change kind
func (c *Change) KindString() string {
	switch c.Kind {
	case ChangeModified:
		return "M"
	case ChangeAdded:
		return "A"
	case ChangeDeleted:
		return "D"
	case ChangeRenamed:
		return "R"
	case ChangeCopied:
		return "C"
	case ChangeUntracked:
		return "?"
	default:
		return "?"
	}
}

// DisplayCode returns a two-character status code for display
// First char is staged status, second is unstaged status
func (c *Change) DisplayCode() string {
	if c.Staged {
		return c.KindString() + " "
	}
	if c.Kind == ChangeUntracked {
		return "??"
	}
	return " " + c.KindString()
}
