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

	_, _ = tab.ptyTraceFile.Write([]byte(fmt.Sprintf("chunk offset=%d bytes=%d\n", tab.ptyTraceBytes, len(data))))
	_, _ = tab.ptyTraceFile.Write([]byte(hex.Dump(data)))
	tab.ptyTraceBytes += len(data)

	if tab.ptyTraceBytes >= ptyTraceLimit {
		_, _ = tab.ptyTraceFile.Write([]byte("TRACE TRUNCATED\n"))
		_ = tab.ptyTraceFile.Close()
		tab.ptyTraceClosed = true
	}
}
