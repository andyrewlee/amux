package git

import (
	"bytes"
	"strconv"
	"strings"
)

// BranchFile represents a file changed on a branch relative to base
type BranchFile struct {
	Path         string     // File path
	OldPath      string     // Original path for renames
	Kind         ChangeKind // Type of change
	AddedLines   int        // Lines added
	DeletedLines int        // Lines deleted
}

// BranchDiff holds the diff summary for a branch
type BranchDiff struct {
	BaseBranch   string       // The base branch (main, master, etc.)
	Files        []BranchFile // Changed files
	TotalAdded   int          // Total lines added
	TotalDeleted int          // Total lines deleted
}

// GetBaseBranch returns the base branch (main, master, or the default branch)
func GetBaseBranch(repoPath string) (string, error) {
	// Try common base branch names in order of preference
	candidates := []string{"main", "master", "develop", "dev"}

	for _, branch := range candidates {
		// Check if branch exists
		_, err := RunGit(repoPath, "rev-parse", "--verify", branch)
		if err == nil {
			return branch, nil
		}
	}

	// Try to get the default branch from remote
	output, err := RunGit(repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		// Output is like "refs/remotes/origin/main"
		parts := strings.Split(output, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1], nil
		}
	}

	// Fall back to "main" if nothing else works
	return "main", nil
}

// GetBranchFiles returns the list of files changed on the current branch vs base
func GetBranchFiles(repoPath string) (*BranchDiff, error) {
	base, err := GetBaseBranch(repoPath)
	if err != nil {
		return nil, err
	}

	// Get list of changed files with stats
	// Using merge-base to find the common ancestor
	mergeBase, err := RunGit(repoPath, "merge-base", base, "HEAD")
	if err != nil {
		// If merge-base fails, try direct diff
		mergeBase = base
	}

	// Get files with numstat for line counts using -z for NUL separators (handles spaces in paths)
	// and -M to detect renames (avoids duplicate entries for renamed files)
	output, err := RunGitRaw(repoPath, "diff", "--numstat", "-M", "-z", mergeBase+"...HEAD")
	if err != nil {
		return nil, err
	}

	result := &BranchDiff{
		BaseBranch: base,
		Files:      []BranchFile{},
	}

	if len(output) == 0 {
		return result, nil
	}

	// Parse NUL-separated numstat format:
	// Normal: "ADDED\tDELETED\tPATH\0"
	// Rename: "ADDED\tDELETED\0OLDPATH\0NEWPATH\0" (only 2 tab fields, followed by NUL-separated paths)
	parts := bytes.Split(output, []byte{0})
	i := 0
	for i < len(parts) {
		part := string(parts[i])
		if part == "" {
			i++
			continue
		}

		fields := strings.Split(part, "\t")
		if len(fields) < 2 {
			i++
			continue
		}

		file := BranchFile{}

		// Parse line counts (can be "-" for binary files)
		if fields[0] != "-" {
			file.AddedLines, _ = strconv.Atoi(fields[0])
		}
		if fields[1] != "-" {
			file.DeletedLines, _ = strconv.Atoi(fields[1])
		}

		// Check for rename/copy: exactly 2 tab fields means paths come in next NUL-separated parts
		if len(fields) == 2 && i+2 < len(parts) {
			file.OldPath = string(parts[i+1])
			file.Path = string(parts[i+2])
			file.Kind = ChangeRenamed
			i += 3
		} else if len(fields) >= 3 {
			// Normal file: path is the third tab-separated field
			file.Path = fields[2]
			i++
		} else {
			i++
			continue
		}

		result.Files = append(result.Files, file)
		result.TotalAdded += file.AddedLines
		result.TotalDeleted += file.DeletedLines
	}

	// Get file status (A/M/D/R) for each file using -z for NUL separators
	statusOutput, err := RunGitRaw(repoPath, "diff", "--name-status", "-M", "-z", mergeBase+"...HEAD")
	if err == nil && len(statusOutput) > 0 {
		// Parse NUL-separated name-status format:
		// Normal: "STATUS\0PATH\0"
		// Rename: "Rstatus\0OLDPATH\0NEWPATH\0"
		statusParts := bytes.Split(statusOutput, []byte{0})
		j := 0
		for j < len(statusParts) {
			status := string(statusParts[j])
			if status == "" {
				j++
				continue
			}

			var path, oldPath string
			kind := ChangeModified

			switch {
			case status == "A":
				kind = ChangeAdded
				if j+1 < len(statusParts) {
					path = string(statusParts[j+1])
					j += 2
				} else {
					j++
				}
			case status == "D":
				kind = ChangeDeleted
				if j+1 < len(statusParts) {
					path = string(statusParts[j+1])
					j += 2
				} else {
					j++
				}
			case status == "M":
				kind = ChangeModified
				if j+1 < len(statusParts) {
					path = string(statusParts[j+1])
					j += 2
				} else {
					j++
				}
			case strings.HasPrefix(status, "R"):
				kind = ChangeRenamed
				if j+2 < len(statusParts) {
					oldPath = string(statusParts[j+1])
					path = string(statusParts[j+2])
					j += 3
				} else {
					j++
				}
			case strings.HasPrefix(status, "C"):
				kind = ChangeCopied
				if j+2 < len(statusParts) {
					oldPath = string(statusParts[j+1])
					path = string(statusParts[j+2])
					j += 3
				} else {
					j++
				}
			default:
				// Unknown status, try to get path
				if j+1 < len(statusParts) {
					path = string(statusParts[j+1])
					j += 2
				} else {
					j++
				}
			}

			// Update the file entry with kind and oldPath
			for k := range result.Files {
				if result.Files[k].Path == path {
					result.Files[k].Kind = kind
					if oldPath != "" {
						result.Files[k].OldPath = oldPath
					}
					break
				}
			}
		}
	}

	return result, nil
}

// GetBranchFileDiff returns the full diff for a single file on the branch
func GetBranchFileDiff(repoPath, path string) (*DiffResult, error) {
	base, err := GetBaseBranch(repoPath)
	if err != nil {
		return nil, err
	}

	mergeBase, err := RunGit(repoPath, "merge-base", base, "HEAD")
	if err != nil {
		mergeBase = base
	}

	args := []string{"diff", "--no-color", "--no-ext-diff", "-U3", mergeBase + "...HEAD", "--", path}
	output, err := RunGit(repoPath, args...)
	if err != nil {
		return &DiffResult{
			Path:  path,
			Error: err.Error(),
		}, nil
	}

	return parseDiff(path, output), nil
}

// FileCount returns the number of files changed
func (d *BranchDiff) FileCount() int {
	return len(d.Files)
}

// Summary returns a summary string like "12 files changed, +234 -56"
func (d *BranchDiff) Summary() string {
	files := len(d.Files)
	filesWord := "files"
	if files == 1 {
		filesWord = "file"
	}
	return strconv.Itoa(files) + " " + filesWord + " changed, +" + strconv.Itoa(d.TotalAdded) + " -" + strconv.Itoa(d.TotalDeleted)
}
