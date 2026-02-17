package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/andyrewlee/amux/internal/logging"
)

type logsResult struct {
	Path  string   `json:"path"`
	Lines []string `json:"lines"`
	Count int      `json:"count"`
}

func cmdLogs(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux logs tail [--lines N] [--json]"
	if len(args) == 0 || args[0] != "tail" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	tailArgs := args[1:]

	fs := newFlagSet("logs tail")
	lines := fs.Int("lines", 50, "number of lines to tail")
	if err := fs.Parse(tailArgs); err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if *lines < 0 {
		if gf.JSON {
			ReturnError(w, "invalid_lines", "--lines must be >= 0", map[string]any{"lines": *lines}, version)
		} else {
			Errorf(wErr, "--lines must be >= 0")
		}
		return ExitUsage
	}

	logPath := logging.GetLogPath()
	if logPath == "" {
		// Logging not initialized in headless mode; find the latest log file.
		logPath = findLatestLogFile()
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		if gf.JSON {
			ReturnError(w, "log_not_found", fmt.Sprintf("cannot read log: %v", err), nil, version)
		} else {
			Errorf(wErr, "cannot read log file: %v", err)
		}
		return ExitNotFound
	}

	var allLines []string
	if len(content) > 0 {
		allLines = strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	}
	start := 0
	if len(allLines) > *lines {
		start = len(allLines) - *lines
	}
	tail := allLines[start:]

	result := logsResult{
		Path:  logPath,
		Lines: tail,
		Count: len(tail),
	}

	if gf.JSON {
		PrintJSON(w, result, version)
		return ExitOK
	}

	PrintHuman(w, func(w io.Writer) {
		for _, line := range tail {
			fmt.Fprintln(w, line)
		}
	})
	return ExitOK
}

// findLatestLogFile locates the most recent amux-*.log in ~/.amux/logs.
func findLatestLogFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	logDir := filepath.Join(home, ".amux", "logs")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return ""
	}
	var logs []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Match only date-stamped logs: amux-YYYY-MM-DD.log (len == 19)
		if strings.HasPrefix(name, "amux-") && strings.HasSuffix(name, ".log") && len(name) == 19 {
			logs = append(logs, name)
		}
	}
	if len(logs) == 0 {
		return ""
	}
	sort.Strings(logs) // date-stamped names sort chronologically
	return filepath.Join(logDir, logs[len(logs)-1])
}
