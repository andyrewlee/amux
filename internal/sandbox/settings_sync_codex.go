package sandbox

import "strings"

func ensureCodexFileStoreSetting(data []byte) []byte {
	const settingLine = `cli_auth_credentials_store = "file"`

	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	if strings.TrimSpace(normalized) == "" {
		return []byte(settingLine + "\n")
	}

	lines := strings.Split(normalized, "\n")
	filtered := make([]string, 0, len(lines)+1)
	insertAt := -1
	foundRootSetting := false
	beforeFirstTable := true

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if beforeFirstTable && isTOMLTableHeader(trimmed) {
			insertAt = len(filtered)
			beforeFirstTable = false
		}

		if strings.HasPrefix(trimmed, "cli_auth_credentials_store") {
			if beforeFirstTable && !foundRootSetting {
				filtered = append(filtered, settingLine)
				foundRootSetting = true
			}
			continue
		}

		filtered = append(filtered, line)
	}

	if !foundRootSetting {
		if insertAt < 0 {
			insertAt = len(filtered)
			if insertAt > 0 && filtered[insertAt-1] == "" {
				insertAt--
			}
		}
		filtered = insertStringAt(filtered, insertAt, settingLine)
	}

	content := strings.Join(filtered, "\n")
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return []byte(content)
}

func isTOMLTableHeader(line string) bool {
	if commentIdx := strings.Index(line, "#"); commentIdx >= 0 {
		line = strings.TrimSpace(line[:commentIdx])
	}
	return strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")
}

func insertStringAt(lines []string, idx int, line string) []string {
	lines = append(lines, "")
	copy(lines[idx+1:], lines[idx:])
	lines[idx] = line
	return lines
}
