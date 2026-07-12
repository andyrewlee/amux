package process

import (
	"os"
	"path/filepath"
	"strings"
)

// scriptExtensions are file suffixes that make a bare token (one without a path
// separator) look like a script a command might run, e.g. "dev.sh".
var scriptExtensions = []string{".sh", ".bash", ".py", ".rb", ".js", ".ts"}

// unresolvableMetachars are shell constructs whose presence in a token means a
// static, best-effort scan cannot reliably know which file the token names:
// variable expansion ($), command substitution (backtick / $(...)), and glob
// wildcards (* and ?). A token containing any of these is not treated as a
// resolvable path candidate.
const unresolvableMetachars = "$`*?"

// ReferencesInRepoFiles best-effort detects paths inside repoRoot that a shell
// command string appears to reference (e.g. "bash ./scripts/dev.sh"). It is
// intentionally conservative: it reports likely references, never claims to be
// complete, and is used to WARN, not to authorize. It returns repo-relative
// paths that exist as regular files under repoRoot.
//
// It does NOT parse the shell. It splits on whitespace, strips surrounding
// quotes, skips tokens containing shell metacharacters that make resolution
// unreliable, and confirms each remaining path-like token resolves to an
// existing file that stays within repoRoot (no ".." escapes). An empty result
// means "no in-repo file reference was detected", which is NOT a guarantee that
// the command runs no repo files; use CommandIsUnresolvable to detect the
// constructs that hide references from this scan.
func ReferencesInRepoFiles(cmd, repoRoot string) []string {
	if repoRoot == "" {
		return nil
	}

	var found []string
	seen := make(map[string]struct{})

	for _, token := range strings.Fields(cmd) {
		token = stripQuotes(token)
		if token == "" {
			continue
		}
		// Tokens with expansion/substitution/glob constructs cannot be
		// statically resolved to a single file; skip them here (the whole
		// command is separately flagged by CommandIsUnresolvable).
		if strings.ContainsAny(token, unresolvableMetachars) {
			continue
		}
		if !looksLikePath(token) {
			continue
		}

		rel, ok := resolveWithinRepo(token, repoRoot)
		if !ok {
			continue
		}
		if _, dup := seen[rel]; dup {
			continue
		}
		seen[rel] = struct{}{}
		found = append(found, rel)
	}

	return found
}

// CommandIsUnresolvable reports whether cmd contains constructs (variable
// expansion, command substitution, globs) that prevent static reference
// detection, so callers can warn that the command may reference files this
// detector cannot see. It is a coarse signal, not a parser: any occurrence of
// $, backtick, *, or ? anywhere in the string trips it.
func CommandIsUnresolvable(cmd string) bool {
	return strings.ContainsAny(cmd, unresolvableMetachars)
}

// stripQuotes removes a single matching pair of surrounding single or double
// quotes from a token. It only strips a balanced outer pair; anything else is
// returned unchanged.
func stripQuotes(token string) string {
	if len(token) >= 2 {
		first := token[0]
		last := token[len(token)-1]
		if (first == '\'' || first == '"') && first == last {
			return token[1 : len(token)-1]
		}
	}
	return token
}

// looksLikePath reports whether a token is worth resolving as a possible in-repo
// file: it contains a path separator, starts with "./" or "../", or ends in a
// recognized script extension. This deliberately errs toward inspecting more
// tokens; resolveWithinRepo does the authoritative existence/containment check.
func looksLikePath(token string) bool {
	if strings.ContainsRune(token, '/') {
		return true
	}
	if strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") {
		return true
	}
	for _, ext := range scriptExtensions {
		if strings.HasSuffix(token, ext) {
			return true
		}
	}
	return false
}

// resolveWithinRepo resolves a path-like token against repoRoot and returns its
// cleaned repo-relative path if it stays inside repoRoot and names an existing
// regular file. It rejects tokens that escape repoRoot via "..". The returned
// path uses the OS separator (filepath, not slash-normalized).
func resolveWithinRepo(token, repoRoot string) (string, bool) {
	joined := filepath.Join(repoRoot, token)
	rel, err := filepath.Rel(repoRoot, joined)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	info, err := os.Stat(joined)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return rel, true
}
