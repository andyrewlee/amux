package common

// Icons used throughout the application
// Uses Unicode characters with fallbacks for broad terminal support
var Icons = struct {
	// Status indicators
	Clean   string
	Dirty   string
	Running string
	Idle    string
	Pending string

	// Actions
	Add    string
	Delete string
	Edit   string
	Close  string

	// Navigation
	Cursor      string
	CursorEmpty string
	ArrowRight  string
	ArrowDown   string

	// Objects
	Project   string
	Workspace string
	Agent     string
	Terminal  string
	Folder    string
	File      string
	Git       string
	Home      string
	DirOpen   string
	DirClosed string

	// Tab states
	TabActive   string
	TabInactive string

	// Spinner frames for loading animation
	Spinner []string
}{
	// Status indicators
	Clean:   "✓",
	Dirty:   "●",
	Running: "●",
	Idle:    "○",
	Pending: "◌",

	// Actions
	Add:    "+",
	Delete: "×",
	Edit:   "~",
	Close:  "×",

	// Navigation
	Cursor:      ">",
	CursorEmpty: " ",
	ArrowRight:  "→",
	ArrowDown:   "↓",

	// Objects
	Project:   "□",
	Workspace: "├",
	Agent:     "◇",
	Terminal:  "$",
	Folder:    "/",
	File:      "·",
	Git:       "*",
	Home:      "~",
	DirOpen:   "▼",
	DirClosed: "▶",

	// Tab states
	TabActive:   "●",
	TabInactive: "○",

	// Spinner frames (braille pattern animation)
	Spinner: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
}

// SpinnerFrame returns the spinner character for a given frame index
func SpinnerFrame(frame int) string {
	return Icons.Spinner[frame%len(Icons.Spinner)]
}

// FileStatusIcon returns an icon and description for git file status
func FileStatusIcon(status string) (icon, desc string) {
	switch status {
	case "M":
		return "M", "modified"
	case "A":
		return "A", "added"
	case "D":
		return "D", "deleted"
	case "R":
		return "R", "renamed"
	case "C":
		return "C", "copied"
	case "U":
		return "U", "unmerged"
	case "??":
		return "?", "untracked"
	case "!!":
		return "!", "ignored"
	default:
		return "?", "unknown"
	}
}
