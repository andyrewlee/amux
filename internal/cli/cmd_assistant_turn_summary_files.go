package cli

import (
	"path/filepath"
	"regexp"
	"strings"
)

var assistantTurnFilenameTokenRE = regexp.MustCompile(`([[:alnum:]_.-]+/)*[[:alnum:]_.-]+`)

func assistantTurnLineHasFilenameToken(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, candidate := range assistantTurnFilenameTokenRE.FindAllString(value, -1) {
		candidate = strings.TrimRight(candidate, ".,:;!?)]}\"'")
		if candidate == "" {
			continue
		}
		if strings.Contains(value, "://"+candidate) {
			continue
		}
		if strings.Contains(candidate, "/") {
			hostSegment := strings.SplitN(candidate, "/", 2)[0]
			if assistantTurnIsLikelyDomain(hostSegment) {
				continue
			}
			if assistantTurnIsSpecialFilename(candidate) || strings.Contains(filepath.Base(candidate), ".") {
				return true
			}
			continue
		}
		if assistantTurnIsSpecialFilename(candidate) {
			return true
		}
		if !strings.Contains(candidate, ".") || assistantTurnIsVersionLike(candidate) {
			continue
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(candidate)), ".")
		stem := strings.TrimSuffix(candidate, filepath.Ext(candidate))
		if stem == "" || ext == "" || !assistantTurnHasAlphaSignal(stem) && !assistantTurnHasAlphaSignal(ext) {
			continue
		}
		if assistantTurnIsLikelyDomain(candidate) && !assistantTurnIsEnvLike(candidate) {
			continue
		}
		if assistantTurnIsLikelyDomain(candidate) && !assistantTurnAllowedFileExt(ext) {
			continue
		}
		return true
	}
	return false
}

func assistantTurnLineHasFileSignal(value string) bool {
	switch {
	case strings.Contains(value, ".go"),
		strings.Contains(value, ".md"),
		strings.Contains(value, ".sh"),
		strings.Contains(value, "internal/"),
		strings.Contains(value, "cmd/"),
		strings.Contains(value, "skills/"),
		strings.Contains(value, "README."),
		strings.Contains(value, "Makefile"):
		return true
	default:
		return assistantTurnLineHasFilenameToken(value)
	}
}

func assistantTurnIsSpecialFilename(candidate string) bool {
	base := strings.ToLower(filepath.Base(candidate))
	return base == "dockerfile" || base == "makefile" || base == "readme" || strings.HasPrefix(base, "readme.")
}

func assistantTurnHasAlphaSignal(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == '-' {
			return true
		}
	}
	return false
}

func assistantTurnIsVersionLike(candidate string) bool {
	ok, _ := regexp.MatchString(`^v?[0-9]+(\.[0-9]+)+$`, candidate)
	return ok
}

func assistantTurnIsLikelyDomain(candidate string) bool {
	ok, _ := regexp.MatchString(`^[[:alnum:]-]+(\.[[:alnum:]-]+)+$`, candidate)
	if !ok {
		return false
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(candidate)), ".")
	switch ext {
	case "ai", "app", "biz", "ca", "co", "com", "de", "dev", "edu", "fr", "gov", "info", "in", "internal", "io", "jp", "local", "localhost", "ly", "me", "mil", "net", "org", "test", "tv", "uk", "us":
		return true
	default:
		return false
	}
}

func assistantTurnIsEnvLike(candidate string) bool {
	ok, _ := regexp.MatchString(`^(\.env|env)\.(dev|local|test)$`, strings.ToLower(candidate))
	return ok
}

func assistantTurnAllowedFileExt(ext string) bool {
	switch strings.ToLower(ext) {
	case "adoc", "bash", "bat", "c", "cc", "cfg", "conf", "cpp", "cjs", "cmd", "css", "csv",
		"cts", "cxx", "editorconfig", "env", "go", "gql", "graphql", "h", "hh", "hpp", "htm",
		"html", "ini", "java", "js", "json", "jsonc", "jsx", "kt", "kts", "less", "lock", "md",
		"mjs", "mod", "mts", "proto", "ps1", "py", "rb", "rs", "rst", "scala", "scss", "sh",
		"sql", "sum", "swift", "toml", "ts", "tsx", "tsv", "txt", "xml", "yaml", "yml", "zsh":
		return true
	default:
		return false
	}
}
