package git

import (
	"regexp"
	"strconv"
	"strings"
)

const (
	// LargeFileSizeThreshold is the size above which files are considered "large"
	LargeFileSizeThreshold = 2 * 1024 * 1024 // 2MB
)

// DiffLineKind represents the type of a diff line
type DiffLineKind int

const (
	DiffLineContext DiffLineKind = iota // Unchanged context line
	DiffLineAdd                         // Added line (green)
	DiffLineDelete                      // Deleted line (red)
	DiffLineHeader                      // File/hunk header
)

// DiffLine represents a single line in a diff
type DiffLine struct {
	Kind    DiffLineKind
	Content string
}

// Hunk represents a single hunk in a diff
type Hunk struct {
	OldStart  int    // Starting line in old file
	OldCount  int    // Number of lines in old file
	NewStart  int    // Starting line in new file
	NewCount  int    // Number of lines in new file
	StartLine int    // Line index in rendered output (for navigation)
	Header    string // The full @@ line
}

// DiffResult holds parsed diff information for a single file
type DiffResult struct {
	Path    string     // File path
	Content string     // Raw diff content
	Hunks   []Hunk     // Parsed hunks for navigation
	Lines   []DiffLine // Parsed lines for rendering
	Binary  bool       // True if this is a binary file
	Large   bool       // True if the file is too large to display
	Empty   bool       // True if there are no changes
	Error   string     // Error message if diff failed
}

// hunkPattern matches unified diff hunk headers
// @@ -OLD_START,OLD_COUNT +NEW_START,NEW_COUNT @@ optional context
var hunkPattern = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$`)

// GetFileDiff returns the diff for a specific file
func GetFileDiff(repoPath, path string, mode DiffMode) (*DiffResult, error) {
	var args []string

	switch mode {
	case DiffModeStaged:
		args = []string{"diff", "--cached", "--no-color", "--no-ext-diff", "-U3", "--", path}
	case DiffModeUnstaged:
		args = []string{"diff", "--no-color", "--no-ext-diff", "-U3", "--", path}
	case DiffModeBranch:
		// For branch mode, caller should use GetBranchFileDiff instead
		base, _ := GetBaseBranch(repoPath)
		args = []string{"diff", base + "...HEAD", "--no-color", "--no-ext-diff", "-U3", "--", path}
	default:
		args = []string{"diff", "--no-color", "--no-ext-diff", "-U3", "--", path}
	}

	output, err := RunGit(repoPath, args...)
	if err != nil {
		return &DiffResult{
			Path:  path,
			Error: err.Error(),
		}, nil
	}

	return parseDiff(path, output), nil
}

// GetUntrackedFileContent returns the content of an untracked file formatted as a diff
func GetUntrackedFileContent(repoPath, path string) (*DiffResult, error) {
	fullPath := repoPath + "/" + path

	// Use RunGitAllowFailure since --no-index returns exit code 1 when differences exist
	output, _ := RunGitAllowFailure(repoPath, "diff", "--no-index", "--no-color", "--", "/dev/null", fullPath)

	if strings.Contains(output, "Binary files") {
		return &DiffResult{
			Path:   path,
			Binary: true,
		}, nil
	}

	return parseDiff(path, output), nil
}

// parseDiff parses unified diff output into a DiffResult
func parseDiff(path, content string) *DiffResult {
	result := &DiffResult{
		Path:    path,
		Content: content,
		Hunks:   []Hunk{},
		Lines:   []DiffLine{},
		Empty:   content == "",
	}

	if content == "" {
		return result
	}

	// Check for binary file indicator
	if strings.Contains(content, "Binary files") {
		result.Binary = true
		return result
	}

	// Check for large file
	if len(content) > LargeFileSizeThreshold {
		result.Large = true
		return result
	}

	lines := strings.Split(content, "\n")
	lineIdx := 0

	for _, line := range lines {
		diffLine := DiffLine{Content: line}

		// Determine line type
		if strings.HasPrefix(line, "@@") {
			diffLine.Kind = DiffLineHeader

			// Parse hunk header
			if matches := hunkPattern.FindStringSubmatch(line); matches != nil {
				hunk := Hunk{
					Header:    line,
					StartLine: lineIdx,
				}

				hunk.OldStart, _ = strconv.Atoi(matches[1])
				if matches[2] != "" {
					hunk.OldCount, _ = strconv.Atoi(matches[2])
				} else {
					hunk.OldCount = 1
				}
				hunk.NewStart, _ = strconv.Atoi(matches[3])
				if matches[4] != "" {
					hunk.NewCount, _ = strconv.Atoi(matches[4])
				} else {
					hunk.NewCount = 1
				}

				result.Hunks = append(result.Hunks, hunk)
			}
		} else if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			diffLine.Kind = DiffLineAdd
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			diffLine.Kind = DiffLineDelete
		} else if strings.HasPrefix(line, "diff ") ||
			strings.HasPrefix(line, "index ") ||
			strings.HasPrefix(line, "---") ||
			strings.HasPrefix(line, "+++") ||
			strings.HasPrefix(line, "new file") ||
			strings.HasPrefix(line, "deleted file") ||
			strings.HasPrefix(line, "old mode") ||
			strings.HasPrefix(line, "new mode") ||
			strings.HasPrefix(line, "similarity index") ||
			strings.HasPrefix(line, "rename from") ||
			strings.HasPrefix(line, "rename to") ||
			strings.HasPrefix(line, "copy from") ||
			strings.HasPrefix(line, "copy to") {
			diffLine.Kind = DiffLineHeader
		} else {
			diffLine.Kind = DiffLineContext
		}

		result.Lines = append(result.Lines, diffLine)
		lineIdx++
	}

	return result
}

// GetCombinedDiff returns the combined diff for a file that has both staged and unstaged changes
func GetCombinedDiff(repoPath, path string) (*DiffResult, error) {
	// Get staged changes
	staged, err := GetFileDiff(repoPath, path, DiffModeStaged)
	if err != nil {
		return nil, err
	}

	// Get unstaged changes
	unstaged, err := GetFileDiff(repoPath, path, DiffModeUnstaged)
	if err != nil {
		return nil, err
	}

	// If both have content, combine them
	if !staged.Empty && !unstaged.Empty {
		return &DiffResult{
			Path:    path,
			Content: "=== Staged Changes ===\n" + staged.Content + "\n\n=== Unstaged Changes ===\n" + unstaged.Content,
			Hunks:   append(staged.Hunks, unstaged.Hunks...),
			Binary:  staged.Binary || unstaged.Binary,
			Large:   staged.Large || unstaged.Large,
		}, nil
	}

	// Return whichever has content
	if !staged.Empty {
		return staged, nil
	}
	return unstaged, nil
}

// HunkCount returns the number of hunks in the diff
func (d *DiffResult) HunkCount() int {
	return len(d.Hunks)
}

// AddedLines returns the count of added lines
func (d *DiffResult) AddedLines() int {
	count := 0
	for _, line := range d.Lines {
		if line.Kind == DiffLineAdd {
			count++
		}
	}
	return count
}

// DeletedLines returns the count of deleted lines
func (d *DiffResult) DeletedLines() int {
	count := 0
	for _, line := range d.Lines {
		if line.Kind == DiffLineDelete {
			count++
		}
	}
	return count
}
