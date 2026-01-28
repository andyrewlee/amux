package center

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/logging"
)

const ptyTraceLimit = 256 * 1024

func ptyTraceAllowed(assistant string) bool {
	value := strings.TrimSpace(os.Getenv("AMUX_PTY_TRACE"))
	if value == "" {
		return false
	}

	switch strings.ToLower(value) {
	case "0", "false", "no":
		return false
	case "1", "true", "yes", "all", "*":
		return true
	}

	target := strings.ToLower(strings.TrimSpace(assistant))
	if target == "" {
		return false
	}

	for _, part := range strings.Split(value, ",") {
		if strings.ToLower(strings.TrimSpace(part)) == target {
			return true
		}
	}

	return false
}

func ptyTraceDir() string {
	logPath := logging.GetLogPath()
	if logPath != "" {
		return filepath.Dir(logPath)
	}
	return os.TempDir()
}

func (m *Model) tracePTYOutput(tab *Tab, data []byte) {
	if tab == nil || !ptyTraceAllowed(tab.Assistant) {
		return
	}

	tab.mu.Lock()
	defer tab.mu.Unlock()

	if tab.ptyTraceClosed {
		return
	}

	if tab.ptyTraceFile == nil {
		dir := ptyTraceDir()
		name := fmt.Sprintf("amux-pty-claude-%s-%s.log", tab.ID, time.Now().Format("20060102-150405"))
		path := filepath.Join(dir, name)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			logging.Warn("PTY trace open failed: %v", err)
			tab.ptyTraceClosed = true
			return
		}
		tab.ptyTraceFile = file
		worktreeName := ""
		if tab.Workspace != nil {
			worktreeName = tab.Workspace.Name
		}
		_, _ = file.Write([]byte(fmt.Sprintf(
			"TRACE %s assistant=%s worktree=%s tab=%s\n",
			time.Now().Format(time.RFC3339Nano),
			tab.Assistant,
			worktreeName,
			tab.ID,
		)))
		logging.Info("PTY trace enabled: %s", path)
	}

	remaining := ptyTraceLimit - tab.ptyTraceBytes
	if remaining <= 0 {
		_ = tab.ptyTraceFile.Close()
		tab.ptyTraceClosed = true
		return
	}

	if len(data) > remaining {
		data = data[:remaining]
	}

	_, _ = tab.ptyTraceFile.Write([]byte(fmt.Sprintf("chunk offset=%d bytes=%d\n", tab.ptyTraceBytes, len(data))))
	_, _ = tab.ptyTraceFile.Write([]byte(hex.Dump(data)))
	tab.ptyTraceBytes += len(data)

	if tab.ptyTraceBytes >= ptyTraceLimit {
		_, _ = tab.ptyTraceFile.Write([]byte("TRACE TRUNCATED\n"))
		_ = tab.ptyTraceFile.Close()
		tab.ptyTraceClosed = true
	}
}

// AmuxLogMessage represents a parsed AMUX_LOG message
type AmuxLogMessage struct {
	Message string
}

// AmuxApprovalMessage represents a parsed AMUX_APPROVAL message
type AmuxApprovalMessage struct {
	ID   string
	JSON string
}

// AmuxMessages holds extracted AMUX protocol messages
type AmuxMessages struct {
	Logs      []AmuxLogMessage
	Approvals []AmuxApprovalMessage
}

// extractAmuxMessages scans data for AMUX_* protocol messages and returns
// filtered data (without protocol messages) plus extracted messages.
func (m *Model) extractAmuxMessages(tab *Tab, wtID string, tabID TabID, data []byte) ([]byte, AmuxMessages) {
	var msgs AmuxMessages

	// Prepend any buffered incomplete line from previous chunk
	if len(tab.amuxBuffer) > 0 {
		data = append(tab.amuxBuffer, data...)
		tab.amuxBuffer = nil
	}

	var out []byte
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			line := data[start : i+1]
			lineStr := string(line)

			// Check for AMUX_LOG protocol message
			if strings.HasPrefix(lineStr, "AMUX_LOG:") {
				msg := strings.TrimPrefix(lineStr, "AMUX_LOG:")
				msg = strings.TrimSpace(msg)
				msgs.Logs = append(msgs.Logs, AmuxLogMessage{Message: msg})
				start = i + 1
				continue
			}

			// Check for AMUX_APPROVAL protocol message
			if strings.HasPrefix(lineStr, "AMUX_APPROVAL:") {
				rest := strings.TrimPrefix(lineStr, "AMUX_APPROVAL:")
				rest = strings.TrimSpace(rest)
				parts := strings.SplitN(rest, ":", 2)
				if len(parts) == 2 {
					msgs.Approvals = append(msgs.Approvals, AmuxApprovalMessage{
						ID:   strings.TrimSpace(parts[0]),
						JSON: strings.TrimSpace(parts[1]),
					})
				}
				start = i + 1
				continue
			}

			// Regular line - keep it
			out = append(out, line...)
			start = i + 1
		}
	}

	// Handle remaining bytes (incomplete line)
	if start < len(data) {
		remaining := data[start:]
		// Check if it starts with AMUX_ prefix - buffer it
		if strings.HasPrefix(string(remaining), "AMUX_") {
			tab.amuxBuffer = make([]byte, len(remaining))
			copy(tab.amuxBuffer, remaining)
		} else {
			// Not an AMUX line, pass through
			out = append(out, remaining...)
		}
	}

	return out, msgs
}
