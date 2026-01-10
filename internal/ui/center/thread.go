package center

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/logging"
)

const threadsDirName = "threads"

func (m *Model) exportActiveThread() (string, error) {
	if m.worktree == nil {
		return "", errors.New("no active worktree")
	}

	tabs := m.getTabs()
	if len(tabs) == 0 {
		return "", errors.New("no active tab")
	}

	activeIdx := m.getActiveTabIdx()
	if activeIdx < 0 || activeIdx >= len(tabs) {
		return "", errors.New("no active tab")
	}

	tab := tabs[activeIdx]
	lines, err := m.threadLinesForTab(tab)
	if err != nil {
		return "", err
	}

	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}

	exportDir, err := m.threadExportDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return "", err
	}

	now := time.Now()
	filename := m.threadFilename(now, tab)
	path, err := uniqueThreadPath(exportDir, filename)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}

	logging.Info("Thread saved to: %s", path)

	return path, nil
}

func (m *Model) threadLinesForTab(tab *Tab) ([]string, error) {
	if tab == nil {
		return nil, errors.New("no active tab")
	}
	tab.mu.Lock()
	if tab.Terminal == nil {
		tab.mu.Unlock()
		return nil, errors.New("no active terminal")
	}
	lines := tab.Terminal.GetAllLines()
	tab.mu.Unlock()

	return trimTrailingEmptyLines(lines), nil
}

func (m *Model) threadExportDir() (string, error) {
	if m.config != nil && m.config.Paths != nil && m.config.Paths.Home != "" {
		return filepath.Join(m.config.Paths.Home, threadsDirName), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".amux", threadsDirName), nil
}

func (m *Model) threadFilename(now time.Time, tab *Tab) string {
	parts := []string{now.Format("20060102-150405")}

	if m.worktree != nil {
		if part := sanitizeFilenamePart(m.worktree.Name); part != "" {
			parts = append(parts, part)
		}
	}

	label := ""
	if tab != nil {
		label = strings.TrimSpace(tab.Name)
		if label == "" {
			label = strings.TrimSpace(tab.Assistant)
		}
	}
	if part := sanitizeFilenamePart(label); part != "" {
		parts = append(parts, part)
	}

	if len(parts) == 0 {
		parts = []string{"thread"}
	}

	return strings.Join(parts, "-") + ".txt"
}

func sanitizeFilenamePart(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	var b strings.Builder
	lastDash := false
	for _, r := range input {
		if r > 127 {
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
			continue
		}
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_':
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		default:
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}

	return strings.Trim(b.String(), "-")
}

func uniqueThreadPath(dir, filename string) (string, error) {
	path := filepath.Join(dir, filename)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return path, nil
		}
		return "", err
	}

	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	for i := 2; i < 10000; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
		if _, err := os.Stat(candidate); err != nil {
			if os.IsNotExist(err) {
				return candidate, nil
			}
			return "", err
		}
	}

	return "", errors.New("could not determine unique thread filename")
}

func trimTrailingEmptyLines(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}
