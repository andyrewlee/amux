package diff

import (
	"strings"
)

// LineType identifies diff line type.
type LineType string

const (
	LineHeader  LineType = "header"
	LineHunk    LineType = "hunk"
	LineAdd     LineType = "add"
	LineDel     LineType = "del"
	LineContext LineType = "context"
	LineComment LineType = "comment"
)

// Line represents a diff line with optional line numbers.
type Line struct {
	Text     string
	Type     LineType
	OldLine  int
	NewLine  int
	FilePath string
}

// File represents a file diff.
type File struct {
	Path    string
	Added   int
	Deleted int
	Lines   []Line
}

// Parse parses a unified diff into structured files.
func Parse(diff string) []File {
	lines := strings.Split(diff, "\n")
	var files []File
	var current *File
	oldLine := 0
	newLine := 0

	flush := func() {
		if current != nil {
			files = append(files, *current)
		}
		current = nil
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			current = &File{Lines: []Line{}}
			oldLine = 0
			newLine = 0
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(line, "+++ ") {
			path := strings.TrimPrefix(line, "+++ ")
			path = strings.TrimPrefix(path, "b/")
			current.Path = path
			current.Lines = append(current.Lines, Line{Text: line, Type: LineHeader, FilePath: path})
			continue
		}
		if strings.HasPrefix(line, "@@ ") {
			current.Lines = append(current.Lines, Line{Text: line, Type: LineHunk, FilePath: current.Path})
			oldLine, newLine = parseHunkHeader(line)
			continue
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			current.Added++
			current.Lines = append(current.Lines, Line{Text: line, Type: LineAdd, OldLine: 0, NewLine: newLine, FilePath: current.Path})
			newLine++
			continue
		}
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			current.Deleted++
			current.Lines = append(current.Lines, Line{Text: line, Type: LineDel, OldLine: oldLine, NewLine: 0, FilePath: current.Path})
			oldLine++
			continue
		}
		// context or other
		current.Lines = append(current.Lines, Line{Text: line, Type: LineContext, OldLine: oldLine, NewLine: newLine, FilePath: current.Path})
		if oldLine > 0 {
			oldLine++
		}
		if newLine > 0 {
			newLine++
		}
	}
	flush()
	return files
}

func parseHunkHeader(line string) (int, int) {
	// @@ -a,b +c,d @@
	parts := strings.Split(line, " ")
	if len(parts) < 3 {
		return 0, 0
	}
	oldPart := strings.TrimPrefix(parts[1], "-")
	newPart := strings.TrimPrefix(parts[2], "+")
	oldStart := parseHunkPos(oldPart)
	newStart := parseHunkPos(newPart)
	return oldStart, newStart
}

func parseHunkPos(part string) int {
	fields := strings.Split(part, ",")
	if len(fields) == 0 {
		return 0
	}
	return atoi(fields[0])
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}
