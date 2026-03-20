package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type assistantCompatEvent struct {
	Type        string `json:"type"`
	IdleSeconds int    `json:"idle_seconds,omitempty"`
	Timestamp   string `json:"ts"`
}

var (
	assistantCompatStat               = os.Stat
	assistantCompatRepoScriptPathFunc = assistantCompatRepoScriptPath
)

func assistantCompatWriteEvent(w io.Writer, event assistantCompatEvent) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(event)
}

func assistantCompatMarshalJSON(value any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func assistantParseFlexibleDuration(raw string) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, errors.New("duration is required")
	}
	if d, err := time.ParseDuration(value); err == nil {
		return d, nil
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", raw)
	}
	return time.Duration(seconds) * time.Second, nil
}

func assistantWriteContent(w io.Writer, content string) error {
	_, err := io.WriteString(w, content)
	if err != nil {
		return err
	}
	if strings.HasSuffix(content, "\n") {
		return nil
	}
	_, err = io.WriteString(w, "\n")
	return err
}

func assistantTimeoutLabel(raw string, duration time.Duration) string {
	value := strings.TrimSpace(raw)
	if value != "" {
		if _, err := strconv.Atoi(value); err == nil {
			return value + "s"
		}
		return value
	}
	seconds := int(duration / time.Second)
	if seconds <= 0 {
		seconds = 1
	}
	return strconv.Itoa(seconds) + "s"
}

func assistantCompatDefaultScriptRef(scriptName string) string {
	relative := filepath.Join("skills", "amux", "scripts", scriptName)
	if info, err := assistantCompatStat(relative); err == nil && !info.IsDir() {
		return relative
	}
	if absolute := assistantCompatRepoScriptPathFunc(scriptName); absolute != "" {
		return absolute
	}
	return assistantCompatNativeCommandRef(scriptName)
}

func assistantCompatScriptPath(scriptName, envPathName, envDirName string) string {
	if value := strings.TrimSpace(os.Getenv(envPathName)); value != "" {
		return value
	}
	if dir := strings.TrimSpace(os.Getenv(envDirName)); dir != "" {
		return filepath.Join(dir, scriptName)
	}
	return assistantCompatDefaultScriptRef(scriptName)
}

func assistantCompatRepoScriptPath(scriptName string) string {
	repoRoot := assistantCompatRepoRoot()
	if repoRoot == "" {
		return ""
	}
	candidate := filepath.Join(repoRoot, "skills", "amux", "scripts", scriptName)
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return ""
	}
	return candidate
}

func assistantCompatRepoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func assistantCompatNativeCommandRef(scriptName string) string {
	switch scriptName {
	case "assistant-dx.sh":
		return "amux assistant dx"
	case "assistant-step.sh":
		return "amux assistant step"
	case "assistant-turn.sh":
		return "amux assistant turn"
	case "assistant-present.sh":
		return "amux assistant present"
	case "assistant-dogfood.sh":
		return "amux assistant dogfood"
	case "poll-agent.sh":
		return "amux assistant poll-agent"
	case "format-capture.sh":
		return "amux assistant format-capture"
	case "wait-for-idle.sh":
		return "amux assistant wait-for-idle"
	default:
		return filepath.Join("skills", "amux", "scripts", scriptName)
	}
}

func assistantCompatShellCommandRef(value string) string {
	if strings.HasPrefix(strings.TrimSpace(value), "amux assistant ") {
		return strings.TrimSpace(value)
	}
	return shellQuoteCommandValue(value)
}
