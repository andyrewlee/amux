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

// ptyTraceFileName builds the trace filename for an assistant. The assistant
// token is lowercased, trimmed, and reduced to [a-z0-9_-] (other runes become
// '-'), falling back to "agent" when empty — so a codex/cline/gemini trace is
// labeled correctly instead of the old hardcoded "claude".
func ptyTraceFileName(assistant, tabID, ts string) string {
	token := strings.ToLower(strings.TrimSpace(assistant))
	if token == "" {
		token = "agent"
	} else {
		token = strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
				return r
			default:
				return '-'
			}
		}, token)
	}
	return fmt.Sprintf("amux-pty-%s-%s-%s.log", token, tabID, ts)
}

// tracePTYOutput records bytes flowing FROM the agent TO amux (PTY output) into
// the per-tab trace file. The chunk line is tagged "RECV" so the direction is
// distinguishable from input (see tracePTYInput).
func (m *Model) tracePTYOutput(tab *Tab, data []byte) {
	m.tracePTY(tab, "RECV", data)
}

// tracePTYInput records bytes flowing FROM amux TO the agent (keystrokes,
// pastes, and the 50ms-delayed Enter / hex 0D carriage return) into the same
// per-tab trace file as the output direction. The chunk line is tagged "SEND"
// so a 'my Enter didn't register' / keystroke-forwarding bug can be debugged at
// the byte level. Both directions share ptyTraceAllowed, ptyTraceDir, and the
// ptyTraceLimit budget so the trace stays bounded.
func (m *Model) tracePTYInput(tab *Tab, data []byte) {
	m.tracePTY(tab, "SEND", data)
}

// tracePTY is the shared, direction-tagged writer behind tracePTYOutput and
// tracePTYInput. direction is a short marker ("RECV"/"SEND") that prefixes the
// chunk header so both dimensions of the PTY pipeline interleave in one trace
// file while remaining distinguishable.
func (m *Model) tracePTY(tab *Tab, direction string, data []byte) {
	if tab == nil || len(data) == 0 || !ptyTraceAllowed(tab.Assistant) {
		return
	}

	tab.mu.Lock()
	defer tab.mu.Unlock()

	if tab.ptyTraceClosed {
		return
	}

	if tab.ptyTraceFile == nil {
		dir := ptyTraceDir()
		name := ptyTraceFileName(tab.Assistant, string(tab.ID), time.Now().Format("20060102-150405"))
		path := filepath.Join(dir, name)
		file, err := openPTYTraceFile(dir, name)
		if err != nil {
			logging.Warn("PTY trace open failed: %v", err)
			tab.ptyTraceClosed = true
			return
		}
		if err := file.Chmod(0o600); err != nil {
			_ = file.Close()
			logging.Warn("PTY trace chmod failed: %v", err)
			tab.ptyTraceClosed = true
			return
		}
		tab.ptyTraceFile = file
		workspaceName := ""
		if tab.Workspace != nil {
			workspaceName = tab.Workspace.Name
		}
		_, _ = file.Write([]byte(fmt.Sprintf(
			"TRACE %s assistant=%s workspace=%s tab=%s\n",
			time.Now().Format(time.RFC3339Nano),
			tab.Assistant,
			workspaceName,
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

	_, _ = tab.ptyTraceFile.Write([]byte(fmt.Sprintf("%s chunk offset=%d bytes=%d\n", direction, tab.ptyTraceBytes, len(data))))
	_, _ = tab.ptyTraceFile.Write([]byte(hex.Dump(data)))
	tab.ptyTraceBytes += len(data)

	if tab.ptyTraceBytes >= ptyTraceLimit {
		_, _ = tab.ptyTraceFile.Write([]byte("TRACE TRUNCATED\n"))
		_ = tab.ptyTraceFile.Close()
		tab.ptyTraceClosed = true
	}
}

func openPTYTraceFile(dir, name string) (*os.File, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open trace directory: %w", err)
	}
	file, openErr := root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	closeErr := root.Close()
	if openErr != nil {
		if closeErr != nil {
			logging.Warn("PTY trace directory close failed after open error: %v", closeErr)
		}
		return nil, fmt.Errorf("open trace file: %w", openErr)
	}
	if closeErr != nil {
		_ = file.Close()
		return nil, fmt.Errorf("close trace directory: %w", closeErr)
	}
	return file, nil
}
