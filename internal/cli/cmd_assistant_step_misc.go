package cli

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var (
	assistantStepLookPath = exec.LookPath
	assistantStepStat     = os.Stat
)

func assistantStepCaptureContent(amuxBin, sessionName string, lines int) (string, bool) {
	if strings.TrimSpace(sessionName) == "" {
		return "", false
	}
	if lines <= 0 {
		lines = 160
	}
	out, _ := exec.Command(amuxBin, "--json", "agent", "capture", sessionName, "--lines", strconv.Itoa(lines)).Output()
	var payload struct {
		OK   bool `json:"ok"`
		Data struct {
			Content string `json:"content"`
		} `json:"data"`
	}
	if json.Unmarshal(out, &payload) != nil || !payload.OK {
		return "", false
	}
	return payload.Data.Content, true
}

func assistantStepAMUXBin() string {
	if assistantStepShouldReuseSelfExecutable() {
		if value, err := os.Executable(); err == nil && strings.TrimSpace(value) != "" {
			return value
		}
	}
	if value := strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_NATIVE_BIN")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("AMUX_BIN")); value != "" {
		return value
	}
	if value, err := assistantStepLookPath("amux"); err == nil && strings.TrimSpace(value) != "" {
		return value
	}
	if info, err := assistantStepStat("/usr/local/bin/amux"); err == nil && !info.IsDir() {
		return "/usr/local/bin/amux"
	}
	if info, err := assistantStepStat("/opt/homebrew/bin/amux"); err == nil && !info.IsDir() {
		return "/opt/homebrew/bin/amux"
	}
	return "amux"
}

func assistantStepShouldReuseSelfExecutable() bool {
	return strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_REUSE_SELF_EXEC")) != ""
}

func assistantStepDurationToSeconds(value string, fallback int) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parseInt := func(raw string, mult int) (int, bool) {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return 0, false
		}
		return n * mult, true
	}
	switch {
	case regexp.MustCompile(`^[0-9]+$`).MatchString(value):
		if n, ok := parseInt(value, 1); ok {
			return n
		}
	case strings.HasSuffix(value, "s"):
		if n, ok := parseInt(strings.TrimSuffix(value, "s"), 1); ok {
			return n
		}
	case strings.HasSuffix(value, "m"):
		if n, ok := parseInt(strings.TrimSuffix(value, "m"), 60); ok {
			return n
		}
	case strings.HasSuffix(value, "h"):
		if n, ok := parseInt(strings.TrimSuffix(value, "h"), 3600); ok {
			return n
		}
	}
	return fallback
}

func assistantStepEnvDurationToSeconds(name string, fallback int) int {
	return assistantStepDurationToSeconds(os.Getenv(name), fallback)
}

func assistantStepEnvInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func assistantStepTextHasReplyOptionNumber(text, number string) bool {
	text = strings.TrimSpace(text)
	number = strings.TrimSpace(number)
	if text == "" || number == "" {
		return false
	}
	re := regexp.MustCompile(`(?im)(^|[[:space:]])` + regexp.QuoteMeta(number) + `[.)][[:space:]]+`)
	return re.MatchString(text)
}

func assistantStepTextHasReplyOptionLetter(text, letter string) bool {
	text = strings.TrimSpace(text)
	letter = strings.TrimSpace(letter)
	if text == "" || letter == "" {
		return false
	}
	upper := strings.ToUpper(letter)
	lower := strings.ToLower(letter)
	re := regexp.MustCompile(`(?im)(^|[[:space:]])(` + regexp.QuoteMeta(upper) + `|` + regexp.QuoteMeta(lower) + `)[.)][[:space:]]+`)
	return re.MatchString(text)
}

func assistantStepTextHasYesNoPrompt(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	switch {
	case strings.Contains(lower, "(y/n)"),
		strings.Contains(lower, "[y/n]"),
		strings.Contains(lower, "(yes/no)"),
		strings.Contains(lower, "[yes/no]"),
		strings.Contains(lower, "yes or no"),
		strings.Contains(lower, "reply yes"),
		strings.Contains(lower, "reply no"):
		return true
	default:
		return false
	}
}

func assistantStepTextHasPressEnterPrompt(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	switch {
	case strings.Contains(lower, "press enter"),
		strings.Contains(lower, "hit enter"),
		strings.Contains(lower, "press return"),
		strings.Contains(lower, "hit return"),
		strings.Contains(lower, "just press enter"),
		strings.Contains(lower, "enter to continue"):
		return true
	default:
		return false
	}
}

func assistantStepAnyNonEmpty(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func assistantStepWriteJSON(w io.Writer, payload any) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}
