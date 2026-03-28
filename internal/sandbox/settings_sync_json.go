package sandbox

import (
	"encoding/json"
	"strings"
	"unicode"
)

// filterSensitiveJSON removes potentially sensitive keys from JSON config.
func filterSensitiveJSON(data []byte) ([]byte, error) {
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return data, nil // Not valid JSON, return as-is
	}

	sensitiveKeys := []string{
		"apiKey", "api_key", "apikey",
		"token", "auth_token", "authToken",
		"secret", "password", "credential",
		"privatekey", "private_key",
		"accesskey", "access_key",
		"secretkey", "secret_key",
	}

	filtered := filterMapKeys(obj, sensitiveKeys)
	return json.MarshalIndent(filtered, "", "  ")
}

// exactSensitiveKeys are filtered by exact (case-insensitive) match only.
// These are too short/common for substring matching (would false-positive on
// "hotkeyMode", "primaryKey", "keyboard", etc.) but should still be caught
// when used as bare key names like {"key": "sk-live-..."}.
var exactSensitiveKeys = map[string]struct{}{
	"key":     {},
	"private": {},
}

// compoundSensitiveJSONKeys catches credential-like *Key fields without
// blanket-filtering every "...key" name (for example "primaryKey").
var compoundSensitiveJSONKeys = map[string]struct{}{
	"accountkey":    {},
	"appkey":        {},
	"authkey":       {},
	"clientkey":     {},
	"consumerkey":   {},
	"credentialkey": {},
	"licensekey":    {},
	"servicekey":    {},
	"sessionkey":    {},
	"signingkey":    {},
	"sshkey":        {},
	"userkey":       {},
	"webhookkey":    {},
}

// filterMapKeys recursively removes sensitive keys from a map.
func filterMapKeys(obj map[string]any, sensitiveKeys []string) map[string]any {
	result := make(map[string]any)

	for k, v := range obj {
		if isSensitiveJSONKey(k, sensitiveKeys) {
			continue
		}

		result[k] = filterJSONValue(v, sensitiveKeys)
	}

	return result
}

func filterJSONValue(v any, sensitiveKeys []string) any {
	switch typed := v.(type) {
	case map[string]any:
		return filterMapKeys(typed, sensitiveKeys)
	case []any:
		filtered := make([]any, len(typed))
		for i, item := range typed {
			filtered[i] = filterJSONValue(item, sensitiveKeys)
		}
		return filtered
	default:
		return v
	}
}

func isSensitiveJSONKey(key string, sensitiveKeys []string) bool {
	lowerKey := strings.ToLower(key)
	if _, exact := exactSensitiveKeys[lowerKey]; exact {
		return true
	}

	if _, exact := compoundSensitiveJSONKeys[normalizeJSONKeyName(key)]; exact {
		return true
	}

	for _, sensitive := range sensitiveKeys {
		if strings.Contains(lowerKey, strings.ToLower(sensitive)) {
			return true
		}
	}

	return false
}

func normalizeJSONKeyName(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	for _, r := range key {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
