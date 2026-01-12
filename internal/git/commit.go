package git

import (
	"regexp"
	"strconv"
	"strings"
)

// graphPattern matches the git graph prefix (e.g., "* ", "| * ", "| | * ")
// Graph characters: * | / \ _ space
var graphPattern = regexp.MustCompile(`^([*|/\\_\s]*)`)

// Commit represents a parsed git commit with graph information
type Commit struct {
	ShortHash string // Abbreviated SHA (7 chars)
	FullHash  string // Full SHA (40 chars)
	Subject   string // First line of commit message
	Author    string // Author name
	Date      string // Relative date (e.g., "2 hours ago")
	GraphLine string // ASCII graph prefix (e.g., "* ", "| * ", etc.)
	Refs      string // Branch/tag names (e.g., "HEAD -> main, origin/main")
}

// CommitLog represents the result of parsing git log output
type CommitLog struct {
	Commits []Commit
}

// Delimiter used to separate fields in git log format
// Using a rare sequence to avoid conflicts with commit messages
const commitDelimiter = "|||"

// GetCommitLog returns parsed commits with graph information
// Default limit is 100 commits
func GetCommitLog(repoPath string, limit int) (*CommitLog, error) {
	if limit <= 0 {
		limit = 100
	}

	// Format: graph + delimiter-separated fields
	// %h = short hash, %H = full hash, %s = subject, %an = author name, %cr = relative date, %d = ref names
	format := "%h" + commitDelimiter + "%H" + commitDelimiter + "%s" + commitDelimiter + "%an" + commitDelimiter + "%cr" + commitDelimiter + "%d"

	output, err := RunGit(repoPath, "log",
		"--graph",
		"--all",
		"--decorate",
		"--color=never",
		"--format="+format,
		"-n", strconv.Itoa(limit),
	)
	if err != nil {
		return nil, err
	}

	return parseCommitLog(output), nil
}

// parseCommitLog parses the output of git log --graph
func parseCommitLog(output string) *CommitLog {
	if output == "" {
		return &CommitLog{Commits: []Commit{}}
	}

	lines := strings.Split(output, "\n")
	commits := make([]Commit, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}

		// Extract graph prefix
		match := graphPattern.FindStringSubmatch(line)
		graphPrefix := ""
		remaining := line
		if len(match) > 1 {
			graphPrefix = match[1]
			remaining = line[len(graphPrefix):]
		}

		// Skip lines that are pure graph (no commit data)
		if remaining == "" || !strings.Contains(remaining, commitDelimiter) {
			// This is a graph-only line (like merge lines)
			// We still want to show it for visual continuity
			if strings.TrimSpace(line) != "" {
				commits = append(commits, Commit{
					GraphLine: line,
				})
			}
			continue
		}

		// Parse the commit data (delimiter-separated)
		parts := strings.Split(remaining, commitDelimiter)
		if len(parts) < 6 {
			continue
		}

		commit := Commit{
			GraphLine: graphPrefix,
			ShortHash: strings.TrimSpace(parts[0]),
			FullHash:  strings.TrimSpace(parts[1]),
			Subject:   strings.TrimSpace(parts[2]),
			Author:    strings.TrimSpace(parts[3]),
			Date:      strings.TrimSpace(parts[4]),
			Refs:      strings.Trim(strings.TrimSpace(parts[5]), "()"),
		}

		commits = append(commits, commit)
	}

	return &CommitLog{Commits: commits}
}
