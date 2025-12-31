package git

import (
	"strconv"
	"strings"
)

// FileStatus represents the status of a single file
type FileStatus struct {
	Code string // Two-character status code (e.g., "M ", " M", "??", "A ")
	Path string // File path relative to repo root
}

// StatusResult holds the parsed git status
type StatusResult struct {
	Files []FileStatus
	Clean bool
}

// GetStatus returns the git status for a repository
func GetStatus(repoPath string) (*StatusResult, error) {
	output, err := RunGit(repoPath, "status", "--short")
	if err != nil {
		return nil, err
	}

	return parseStatus(output), nil
}

// parseStatus parses the short format git status output
func parseStatus(output string) *StatusResult {
	result := &StatusResult{
		Files: []FileStatus{},
		Clean: true,
	}

	for _, line := range strings.Split(output, "\n") {
		if len(line) < 3 {
			continue
		}

		result.Clean = false
		status := FileStatus{
			Code: line[0:2],
			Path: strings.TrimSpace(line[3:]),
		}
		result.Files = append(result.Files, status)
	}

	return result
}

// GetStatusSummary returns a summary string for the status
func (s *StatusResult) GetStatusSummary() string {
	if s.Clean {
		return "Clean"
	}
	return "+" + strconv.Itoa(len(s.Files)) + " changes"
}

// GetDirtyCount returns the number of modified files
func (s *StatusResult) GetDirtyCount() int {
	return len(s.Files)
}

// IsModified checks if a file status represents a modification
func (f *FileStatus) IsModified() bool {
	return f.Code[0] == 'M' || f.Code[1] == 'M'
}

// IsAdded checks if a file status represents an addition
func (f *FileStatus) IsAdded() bool {
	return f.Code[0] == 'A' || f.Code[1] == 'A'
}

// IsDeleted checks if a file status represents a deletion
func (f *FileStatus) IsDeleted() bool {
	return f.Code[0] == 'D' || f.Code[1] == 'D'
}

// IsUntracked checks if a file is untracked
func (f *FileStatus) IsUntracked() bool {
	return f.Code == "??"
}
