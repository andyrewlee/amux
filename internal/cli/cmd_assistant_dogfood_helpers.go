package cli

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"
)

func assistantDogfoodRenderChannelStatus(payload map[string]any, elapsedSeconds int) string {
	if payload == nil {
		return "command_error|non-json terminal output|latency=" + assistantDogfoodSecondsLabel(elapsedSeconds)
	}
	if text := assistantDogfoodFirstPayloadText(payload, "result", "payloads"); text != "" {
		var inner map[string]any
		if json.Unmarshal([]byte(text), &inner) == nil && inner != nil && assistantDogfoodNestedString(inner, "status") != "" {
			return assistantDogfoodStatusLine(
				assistantDogfoodNestedString(inner, "status"),
				assistantDogfoodNestedString(inner, "summary"),
				assistantDogfoodSecondsLabel(elapsedSeconds),
			)
		}
		return assistantDogfoodStatusLine(
			firstNonEmpty(assistantDogfoodNestedString(payload, "status"), "ok"),
			text,
			assistantDogfoodSecondsLabel(elapsedSeconds),
		)
	}
	if text := assistantDogfoodFirstPayloadText(payload, "payloads"); text != "" {
		return assistantDogfoodStatusLine("ok", text, assistantDogfoodSecondsLabel(elapsedSeconds))
	}
	return assistantDogfoodStatusLine(
		firstNonEmpty(assistantDogfoodNestedString(payload, "status"), "ok"),
		firstNonEmpty(assistantDogfoodNestedString(payload, "summary"), "assistant channel command completed"),
		assistantDogfoodSecondsLabel(elapsedSeconds),
	)
}

func assistantDogfoodFirstPayloadText(payload map[string]any, path ...string) string {
	if payload == nil {
		return ""
	}
	var lookup []string
	lookup = append(lookup, path...)
	lookup = append(lookup, "0", "text")
	return assistantDogfoodNestedString(payload, lookup...)
}

func assistantDogfoodStatusLine(status, summary, elapsed string) string {
	return status + "|" + assistantDogfoodSanitizeText(summary) + "|latency=" + elapsed
}

func assistantDogfoodSanitizeText(value string) string {
	parts := strings.Fields(strings.ReplaceAll(value, "\r", " "))
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func assistantDogfoodParseJSONObject(raw string) (map[string]any, string) {
	candidate := assistantDogfoodJSONCandidate(raw)
	if candidate == "" {
		return nil, ""
	}
	var payload map[string]any
	if json.Unmarshal([]byte(candidate), &payload) != nil {
		return nil, ""
	}
	return payload, candidate
}

func assistantDogfoodJSONCandidate(raw string) string {
	lines := strings.Split(raw, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "{") {
			start = i
			break
		}
	}
	if start >= 0 {
		candidate := strings.Join(lines[start:], "\n")
		var payload map[string]any
		if json.Unmarshal([]byte(candidate), &payload) == nil {
			return candidate
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if !strings.HasPrefix(strings.TrimLeft(line, " \t"), "{") {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(line), &payload) == nil {
			return line
		}
	}
	return ""
}

func assistantDogfoodWriteTextFile(path, content string) error {
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func assistantDogfoodAppendTextFile(path, content string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	_, err = file.WriteString(content)
	return err
}

func assistantDogfoodNestedString(value any, path ...string) string {
	current := assistantDogfoodValueAt(value, path...)
	text, _ := current.(string)
	return text
}

func assistantDogfoodValueAt(value any, path ...string) any {
	current := value
	for _, part := range path {
		switch node := current.(type) {
		case map[string]any:
			current = node[part]
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil
			}
			current = node[idx]
		default:
			return nil
		}
	}
	return current
}

func assistantDogfoodMissingMarkers(jsonText, nonceToken, expectedToken string) bool {
	if nonceToken != "" && !strings.Contains(jsonText, nonceToken) {
		return true
	}
	return expectedToken != "" && !strings.Contains(jsonText, expectedToken)
}

func assistantDogfoodElapsedSeconds(start time.Time) int {
	seconds := int(time.Since(start) / time.Second)
	if seconds < 0 {
		return 0
	}
	return seconds
}

func assistantDogfoodElapsedLabel(start time.Time) string {
	return assistantDogfoodSecondsLabel(assistantDogfoodElapsedSeconds(start))
}

func assistantDogfoodSecondsLabel(seconds int) string {
	return strconv.Itoa(max(seconds, 0)) + "s"
}

func assistantDogfoodFirstNonEmptyEnv(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func assistantDogfoodEnvBool(name string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func assistantDogfoodDXShellCommand(rt *assistantDogfoodRuntime, args ...string) string {
	parts := make([]string, 0, 1+len(rt.DXInvoker.PrefixArgs)+len(args))
	parts = append(parts, shellQuoteCommandValue(rt.DXInvoker.Path))
	for _, prefix := range rt.DXInvoker.PrefixArgs {
		parts = append(parts, shellQuoteCommandValue(prefix))
	}
	for _, arg := range args {
		parts = append(parts, shellQuoteCommandValue(arg))
	}
	return strings.Join(parts, " ")
}
